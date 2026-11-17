package guard

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

// A glob whose name could match a credentials file is refused (CredentialGlob),
// save where the guard proves it reads none (T28, L217-L223, L242): an
// allow-list, so anything it cannot prove keeps that rule. The line is one
// simple command (simpleWords) of a read-only program whose glob operands
// the shell expands as file operands (allowedArgs), or `node`/`python3`/
// `bun` running a script file whose text can name no directory but its own
// relative strings (scriptGlobs); it runs inside its project; every glob is
// relative; and none reaches a credentials file (credential_walk.go). A
// find that only lists names (findListing) is proven the same way, its
// -name and -iname patterns expanded below its search roots. The line may
// send an output to /dev/null (`2>/dev/null`, `2>&1`), and a find's names
// may be piped into filters that read no file (filterStage); a reader's
// content is never piped (T64, L508-L509).

// globProof is what the allow-list proves of a line: every credentials glob
// safe, or the globs it found reaching a credentials file.
type globProof struct {
	proven  bool
	reached []string
}

// readers are the programs whose operands are files they only read, and
// which never search on their own (no grep -r, rg, find, tar, git).
var readers = set("cat", "head", "tail", "wc", "jq", "yq")

// scriptRunners run a script file named as their first word.
var scriptRunners = set("node", "python3", "bun")

// proveGlobs reads command, run in ctx.Cwd, against the allow-list. A list
// (`a; b`, `a && b`, `a || b`) is proven element by element when every
// element is: one the allow-list proves (proveLine, no script), or a
// read-only one whose words name no credentials glob (readOnlyElement).
// No element changes directory or writes, so each globs from ctx.Cwd (T64c, L519).
func proveGlobs(command string, ctx *Ctx) globProof {
	elems, ok := listElements(command)
	if !ok || len(elems) < 2 {
		return proveLine(command, ctx, true)
	}
	var p globProof
	for _, e := range elems {
		q := proveLine(e, ctx, false)
		if !q.proven && len(q.reached) == 0 {
			if !readOnlyElement(e) {
				return globProof{}
			}
			continue
		}
		for _, g := range q.reached {
			if !slices.Contains(p.reached, g) {
				p.reached = append(p.reached, g)
			}
		}
	}
	p.proven = len(p.reached) == 0 && gitProject(ctx.Cwd, ctx.Root)
	return p
}

// listElements is a line split at its top-level `;`, `&&` and `||`; ok is
// false for a background `&`, a newline, or an empty element.
func listElements(line string) ([]string, bool) {
	var out []string
	in, start := byte(0), 0
	for i := 0; i <= len(line); i++ {
		op := 0
		if i < len(line) {
			c := line[i]
			switch {
			case in != 0:
				if c == in {
					in = 0
				}
				continue
			case c == '\'' || c == '"':
				in = c
				continue
			case c == '\n' || c == '\r':
				return nil, false
			case c == ';':
				op = 1
			case strings.HasPrefix(line[i:], "&&"), strings.HasPrefix(line[i:], "||"):
				op = 2
			case c == '&' && !(i > 0 && line[i-1] == '>') && !strings.HasPrefix(line[i:], "&>"):
				return nil, false
			default:
				continue
			}
		}
		e := strings.TrimSpace(line[start:i])
		if e == "" {
			return nil, false
		}
		out = append(out, e)
		if op == 0 {
			break
		}
		start = i + op
		i += op - 1
	}
	return out, in == 0
}

var (
	// listReaders are the read-only programs a list element may run when
	// none of its words is a credentials glob; ls and echo name files only.
	listReaders = set("ls", "echo", "pwd", "git", "grep", "sort", "uniq", "wc", "head", "tail", "cat", "jq", "jhon")
	grepFlags   = lazyre.New(`^(?:-[vinclhHoEFwxqsLZ]+|-[ABCm][0-9]+|--color(?:=[a-z]+)?)$`)
	gitReads    = set("status", "log", "diff", "show", "branch")
	branchFlags = set("-a", "-r", "-v", "-vv", "-l", "--list", "--all", "--remotes", "--show-current", "--no-color")
)

// readOnlyElement is whether a list element is a pipeline of listReaders
// that writes nothing and names no credentials glob: jhon only status or
// risk, with --json at most; git only status, log,
// diff, show or branch (listing), without --output, --ext-diff or
// --textconv; grep without recursion or a file of patterns; sort without
// -o; uniq with no operand.
func readOnlyElement(e string) bool {
	stages, ok := lineStages(e)
	if !ok {
		return false
	}
	for _, words := range stages {
		name, args := words[0], words[1:]
		if !listReaders[name] {
			return false
		}
		if name != "ls" && name != "echo" && slices.ContainsFunc(args, credentialGlobWord) {
			return false
		}
		switch name {
		case "git":
			if len(args) == 0 || !gitReads[args[0]] || slices.ContainsFunc(args, func(a string) bool {
				return strings.HasPrefix(a, "--output") || strings.HasPrefix(a, "--ext-diff") || strings.HasPrefix(a, "--textconv")
			}) {
				return false
			}
			if args[0] == "branch" && slices.ContainsFunc(args[1:], func(a string) bool { return !branchFlags[a] }) {
				return false
			}
		case "jhon":
			// Only `jhon status` and `jhon risk`, with --json at most.
			if len(args) == 0 || (args[0] != "status" && args[0] != "risk") || slices.ContainsFunc(args[1:], func(a string) bool { return a != "--json" }) {
				return false
			}
		case "grep":
			for k := 0; k < len(args); k++ {
				a := args[k]
				switch {
				case a == "-e" || a == "-A" || a == "-B" || a == "-C" || a == "-m":
					k++
				case strings.HasPrefix(a, "-") && !grepFlags.MatchString(a):
					return false
				}
			}
		case "sort", "uniq":
			if slices.ContainsFunc(args, func(a string) bool {
				return (strings.HasPrefix(a, "-") && !filterFlags[name].MatchString(a)) || (name == "uniq" && !strings.HasPrefix(a, "-"))
			}) {
				return false
			}
		case "cat", "head", "tail", "wc", "jq":
			if _, ok := allowedArgs(name, args); !ok {
				return false
			}
		}
	}
	return true
}

// proveLine reads one list element against the allow-list; scripts says
// whether a script runner may be proven (only as the whole line).
func proveLine(command string, ctx *Ctx, scripts bool) globProof {
	stages, ok := lineStages(command)
	if !ok || !gitProject(ctx.Cwd, ctx.Root) {
		return globProof{}
	}
	words := stages[0]
	name, args := words[0], words[1:]
	if len(stages) > 1 && name != "find" {
		return globProof{}
	}
	for _, f := range stages[1:] {
		if !filterStage(f) {
			return globProof{}
		}
	}
	var globs []string
	anywhere := false
	switch {
	case name == "find":
		roots, patterns, ok := findListing(args)
		if !ok {
			return globProof{}
		}
		var p globProof
		for _, g := range patterns {
			if !credentialGlobWord(g) {
				continue
			}
			if strings.Contains(g, "/") {
				return globProof{}
			}
			for _, r := range roots {
				// find matches a name at any depth below a root, as globReachesFrom's last part does.
				found, ok := globReachesFrom(ctx.Cwd, r+"/"+g)
				if !ok {
					return globProof{}
				}
				if found && !slices.Contains(p.reached, g) {
					p.reached = append(p.reached, g)
				}
			}
		}
		p.proven = len(p.reached) == 0
		return p
	case readers[name]:
		ops, ok := allowedArgs(name, args)
		if !ok {
			return globProof{}
		}
		globs = ops
	case scriptRunners[name]:
		if !scripts || len(args) == 0 || strings.HasPrefix(args[0], "-") {
			return globProof{}
		}
		inScript, ok := scriptGlobs(name, args[0], ctx)
		if !ok {
			return globProof{}
		}
		globs, anywhere = append(inScript, args[1:]...), true
	default:
		return globProof{}
	}
	var p globProof
	for _, g := range globs {
		if !credentialGlobWord(g) {
			continue
		}
		if !relativeGlob(g) {
			return globProof{}
		}
		found, ok := globReachesFrom(ctx.Cwd, g)
		if anywhere {
			// A script may join a glob to any directory it names: every depth from the cwd.
			found, ok = globReachesBelow(ctx.Cwd, strings.TrimPrefix(g, "./"))
		}
		if !ok {
			return globProof{}
		}
		if found && !slices.Contains(p.reached, g) {
			p.reached = append(p.reached, g)
		}
	}
	p.proven = len(p.reached) == 0
	return p
}

// credentialGlobWord is a glob today's rule reads as naming a credentials
// file: by CredentialGlob, or by CredentialFile (`x*.pem`).
func credentialGlobWord(w string) bool {
	return isGlob(w) && (CredentialGlob(w) || CredentialFile(w))
}

// nullRedirects are a stage's trailing redirections that send an output
// nowhere a program reads: to /dev/null, or stderr to stdout.
var nullRedirects = lazyre.New(`(?:\s+(?:[12&]?>>?\s*/dev/null|[12]>&[12]))+\s*$`)

// lineStages is a command line as the words of each stage of one pipeline,
// each stage a simple command (simpleWords) once its nullRedirects are
// dropped; ok is false for anything else.
func lineStages(line string) ([][]string, bool) {
	var stages [][]string
	in, start := byte(0), 0
	for i := 0; i <= len(line); i++ {
		if i < len(line) {
			c := line[i]
			switch {
			case in != 0:
				if c == in {
					in = 0
				}
				continue
			case c == '\'' || c == '"':
				in = c
				continue
			case c != '|':
				continue
			}
		}
		words, ok := simpleWords(nullRedirects.ReplaceAllString(line[start:i], ""))
		if !ok || len(words) == 0 {
			return nil, false
		}
		stages = append(stages, words)
		start = i + 1
	}
	return stages, true
}

// filters read their input and no file: a find's names piped into them
// hand over nothing but names.
var filterFlags = map[string]*lazyre.Regexp{
	"head": readerFlags["head"], "tail": readerFlags["tail"], "wc": readerFlags["wc"],
	"sort": lazyre.New(`^-[bdfghinMrRsuVz]+$`), "uniq": lazyre.New(`^-[cdiuz]+$`),
}

// filterStage is whether words are a filter that names no file: only
// options the allow-list knows, no operand.
func filterStage(words []string) bool {
	name, args := words[0], words[1:]
	if filterFlags[name] == nil {
		return false
	}
	if readers[name] {
		ops, ok := allowedArgs(name, args)
		return ok && len(ops) == 0
	}
	return !slices.ContainsFunc(args, func(a string) bool { return !filterFlags[name].MatchString(a) })
}

var (
	findDepth = lazyre.New(`^[0-9]+$`)
	findType  = lazyre.New(`^[fdl]$`)
	// findListOps are the words of a listing find's expression that take no value.
	findListOps = set("-print", "-not", "!", "-o", "-or", "-a", "-and")
)

// findListing is a find that only lists names: its search roots (`.` when
// none) and its -name and -iname patterns; ok is false for an option before
// the roots (-L), a root that is not a plain relative path (relativeGlob,
// no glob), a -path or -ipath test that is no exclusion (`-not -path`) or
// whose pattern could name a credentials file,
// or any other word of the expression: an action (-exec, -delete, -fprint),
// a group.
func findListing(args []string) (roots, patterns []string, ok bool) {
	k := 0
	for ; k < len(args) && !findOperandEnd.MatchString(args[k]); k++ {
		if !relativeGlob(args[k]) || isGlob(args[k]) {
			return nil, nil, false
		}
		roots = append(roots, strings.TrimRight(args[k], "/"))
	}
	if len(roots) == 0 {
		roots = []string{"."}
	}
	for ; k < len(args); k++ {
		a := args[k]
		switch {
		case findListOps[a]:
		case a == "-path" || a == "-ipath":
			// Only as an exclusion (`-not -path`, `! -path`) a path test narrows the listing.
			if k == 0 || (args[k-1] != "-not" && args[k-1] != "!") {
				return nil, nil, false
			}
			if k++; k >= len(args) || credentialGlobWord(args[k]) || CredentialFile(args[k]) {
				return nil, nil, false
			}
		case a == "-name" || a == "-iname" || a == "-type" || a == "-maxdepth" || a == "-mindepth":
			if k++; k >= len(args) {
				return nil, nil, false
			}
			v := args[k]
			if (a == "-type" && !findType.MatchString(v)) || (strings.HasSuffix(a, "depth") && !findDepth.MatchString(v)) {
				return nil, nil, false
			}
			if strings.HasSuffix(a, "name") {
				patterns = append(patterns, v)
			}
		default:
			return nil, nil, false
		}
	}
	return roots, patterns, true
}

// simpleWords is a command line as one simple command's words, quotes
// removed; ok is false for anything else: a pipe, a list, a subshell or
// group, a substitution of any kind, a redirection or heredoc, a variable,
// an escape, a comment, a leading `~`, or more than one line.
func simpleWords(line string) ([]string, bool) {
	var words []string
	var cur strings.Builder
	in, started := byte(0), false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case in == '\'':
			if c == '\'' {
				in = 0
			} else {
				cur.WriteByte(c)
			}
		case in == '"':
			if c == '"' {
				in = 0
			} else if strings.IndexByte("$`\\!", c) >= 0 {
				return nil, false
			} else {
				cur.WriteByte(c)
			}
		case c == ' ' || c == '\t':
			if started {
				words = append(words, cur.String())
				cur.Reset()
				started = false
			}
		case strings.IndexByte("|;&(){}<>$`\\#!\n\r", c) >= 0, c == '~' && !started:
			return nil, false
		default:
			if c == '\'' || c == '"' {
				in = c
			} else {
				cur.WriteByte(c)
			}
			started = true
		}
	}
	if in != 0 {
		return nil, false
	}
	if started {
		words = append(words, cur.String())
	}
	return words, true
}

var (
	number      = lazyre.New(`^\+?[0-9]+$`)
	formatName  = lazyre.New(`^[a-z]+$`)
	readerFlags = map[string]*lazyre.Regexp{
		"cat":  lazyre.New(`^-[AbeEnstTuv]+$`),
		"head": lazyre.New(`^-(?:[qvz]+|[nc]\+?[0-9]+|[0-9]+)$`),
		"tail": lazyre.New(`^-(?:[qvzfF]+|[nc]\+?[0-9]+|[0-9]+)$`),
		"wc":   lazyre.New(`^-[clmwL]+$`),
		"jq":   lazyre.New(`^(?:-[rcsnjSeMCa]+|--(?:raw-output|compact-output|slurp|null-input|join-output|sort-keys|exit-status|monochrome-output|color-output|ascii-output|tab))$`),
		"yq":   lazyre.New(`^(?:-[PeNCMr]+|-o=[a-z]+|--output-format=[a-z]+)$`),
	}
	// queryReads are the words of a jq or yq query that read another file.
	queryReads = lazyre.New(`\b(?:import|include|load\w*|env|strenv)\b|\$ENV|\$__`)
)

// allowedArgs is the file operands of a reader's arguments; ok is false
// for an option the allow-list does not know (one may name a file of
// names: wc --files0-from, jq --from-file), an operand that is an `@file`,
// or a jq or yq query that reads another file.
func allowedArgs(name string, args []string) ([]string, bool) {
	var ops []string
	query := name == "jq" || name == "yq"
	for k := 0; k < len(args); k++ {
		a := args[k]
		switch {
		case a == "--":
			ops = append(ops, args[k+1:]...)
			k = len(args)
		case (name == "head" || name == "tail") && (a == "-n" || a == "-c"):
			if k++; k >= len(args) || !number.MatchString(args[k]) {
				return nil, false
			}
		case name == "yq" && (a == "-o" || a == "--output-format"):
			if k++; k >= len(args) || !formatName.MatchString(args[k]) {
				return nil, false
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			if !readerFlags[name].MatchString(a) {
				return nil, false
			}
		case strings.HasPrefix(a, "@"):
			return nil, false
		case query:
			if name == "yq" && (a == "e" || a == "eval" || a == "ea" || a == "eval-all") {
				continue
			}
			if queryReads.MatchString(a) {
				return nil, false
			}
			if isGlob(a) {
				ops = append(ops, a) // a query the shell may expand into file names
			}
			query = false
		default:
			ops = append(ops, a)
		}
	}
	return ops, !query || name != "jq"
}

// scriptGlobs is the glob words a script file holds, named by w from
// ctx.Cwd; ok is false when the allow-list cannot read the file or its text
// could name another directory (scriptCan).
func scriptGlobs(name, w string, ctx *Ctx) ([]string, bool) {
	if !relativeGlob(w) || isGlob(w) {
		return nil, false
	}
	a := filepath.Join(ctx.Cwd, w)
	info, err := os.Lstat(a)
	if err != nil || !info.Mode().IsRegular() || info.Size() > scriptMaxBytes || !project.Within(project.Realish(a), project.Realish(ctx.Root)) {
		return nil, false
	}
	b, err := os.ReadFile(a)
	python := strings.HasSuffix(w, ".py") || name == "python3"
	if err != nil || !scriptCan(string(b), python) || (python && shadowed(string(b), filepath.Dir(a))) {
		return nil, false
	}
	var out []string
	for _, piece := range stringWords(string(b)) {
		if searchers[piece] {
			return nil, false
		}
		if isGlob(piece) {
			out = append(out, piece)
		}
	}
	return out, true
}

// stringWords is the words of the string literals code holds, its
// concatenated literals joined too, split at blanks, quotes, `=` and `,`.
func stringWords(code string) []string {
	var out []string
	for _, s := range append(sh.CodeStrings(code), sh.CodeStrings(joinConcat(code))...) {
		out = append(out, strings.FieldsFunc(s, func(r rune) bool {
			return r == ' ' || r == '\t' || r == '\n' || r == '\'' || r == '"' || r == '=' || r == ','
		})...)
	}
	return out
}
