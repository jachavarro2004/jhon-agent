package guard

import (
	"regexp"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
	"mvdan.cc/sh/v3/syntax"
)

// Read-only by construction (T271): an inspecting agent's line is judged by
// what it runs. A line passes when every construct in it and every program
// it runs (in a pipe, a list, a loop, a subshell, a substitution, find's
// -exec, xargs) is on a closed list and keeps that program's rule: it
// cannot write a file, run a program the guard does not read, or reach the
// network. Anything not listed is refused, with the program named and a
// fix. It is a filter, not a container: the sandbox contains
// (docs/safety-guard.md).

// readOnlyFix is the fix of a program or a construct the list does not hold.
const readOnlyFix = "read files with the Read, Grep or Glob tool, run only read-only programs (cat, grep, rg, find, git log, show, diff, grep, blame, …), or hand the line to the agent that launched you"

// refusal is one refused part of a line: what, why, and the fix.
func refusal(what, why, fix string) string { return what + ": " + why + ". Instead: " + fix }

// ReadOnly is why a Bash line is not read-only by construction, "" when it
// is. inEnv says whether a variable is in the environment the line runs in;
// forms are Jhon's own readers an inspector is given (`codegraph callers`),
// each read as the leading words of one program of the line.
func ReadOnly(command string, inEnv func(string) bool, forms [][]string) string {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
	if err != nil {
		return refusal("the line", "it does not parse as a bash line ("+err.Error()+")", "quote what the shell would read as syntax, such as a `(` or a `|` inside a pattern")
	}
	why := ""
	syntax.Walk(f, func(n syntax.Node) bool {
		if why != "" {
			return false
		}
		if n != nil {
			why = roNode(n, command, inEnv, forms)
		}
		return why == ""
	})
	if why != "" {
		return why
	}
	cmds, err := sh.Parse(command)
	if err != nil {
		return refusal("the line", "the guard cannot read what it runs ("+err.Error()+")", readOnlyFix)
	}
	for _, c := range cmds {
		ps, err := sh.CommandPrograms(c, nil, nil, sh.Opts{})
		if err != nil {
			return refusal("`"+c.Raw+"`", "the guard cannot read what it runs ("+err.Error()+")", readOnlyFix)
		}
		for _, p := range ps {
			if why := roProgram(p, inEnv, forms); why != "" {
				return why
			}
		}
	}
	return ""
}

// assignable is the rule of every name a line assigns (X=v, read, for,
// printf -v): lowercase and absent from the environment, so no assignment
// changes what a program reads or which program runs.
var lowerName = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

func assignable(name string, inEnv func(string) bool) string {
	if lowerName.MatchString(name) && !inEnv(name) {
		return ""
	}
	return refusal("the assignment of "+name, "a name in the environment, or an uppercase shell variable, changes what the next program reads or which program runs",
		"use a lowercase name that is not set in the environment")
}

// roNode is the refusal of one syntax node a line holds, "" when the list holds it.
func roNode(n syntax.Node, src string, inEnv func(string) bool, forms [][]string) string {
	text := func(n syntax.Node) string { return src[n.Pos().Offset():n.End().Offset()] }
	switch x := n.(type) {
	case *syntax.File, *syntax.Word, *syntax.Lit, *syntax.SglQuoted, *syntax.DblQuoted, *syntax.Subshell, *syntax.Block,
		*syntax.IfClause, *syntax.WhileClause, *syntax.CaseClause, *syntax.CaseItem, *syntax.ProcSubst, *syntax.ExtGlob,
		*syntax.Comment, *syntax.ArrayExpr:
		return ""
	case *syntax.Stmt:
		if x.Background || x.Coprocess {
			return refusal("`"+text(x)+"`", "it runs in the background, past the call", "run it in the foreground")
		}
		return ""
	case *syntax.BinaryCmd:
		return ""
	case *syntax.ForClause:
		if _, ok := x.Loop.(*syntax.WordIter); !ok || x.Select {
			return refusal("`"+text(x)+"`", "only `for <name> in <words>` loops are read", "loop over words: for f in a b; do …; done")
		}
		return ""
	case *syntax.WordIter:
		return assignable(x.Name.Value, inEnv)
	case *syntax.ArrayElem:
		if x.Index != nil {
			return refusal("`"+text(x)+"`", "an array subscript is arithmetic, which may run a substitution", "list the values in order")
		}
		return ""
	case *syntax.Assign:
		if x.Name == nil || x.Index != nil || x.Naked {
			return refusal("`"+text(x)+"`", "only `name=value` is read", "assign a plain lowercase name")
		}
		return assignable(x.Name.Value, inEnv)
	case *syntax.CmdSubst:
		if x.TempFile || x.ReplyVar {
			return refusal("`"+text(x)+"`", "it runs in the current shell", "use $(…)")
		}
		return ""
	case *syntax.ParamExp:
		if x.Excl || x.Index != nil || x.Slice != nil || x.Names != 0 || x.Flags != nil || x.NestedParam != nil || len(x.Modifiers) > 0 ||
			x.Exp != nil && (x.Exp.Op == syntax.AssignUnset || x.Exp.Op == syntax.AssignUnsetOrNull || x.Exp.Op == syntax.OtherParamOps) {
			return refusal("`"+text(x)+"`", "an indirect, indexed, assigning or transforming expansion may run code or change a variable", "expand the variable plainly: $name, ${name:-default}")
		}
		return ""
	case *syntax.Redirect:
		return roRedirect(x, text(x))
	case *syntax.CallExpr:
		if len(x.Args) == 0 {
			return ""
		}
		name, ok := literalWord(x.Args[0])
		if !ok || !programName.MatchString(name) {
			return refusal("`"+text(x.Args[0])+"`", "a program word that is a path, a variable or a substitution may name any program", "call a listed program by its name")
		}
		if _, ruled := roRules[name]; !plainReaders[name] && !ruled && !slices.ContainsFunc(forms, func(f []string) bool { return len(f) > 0 && f[0] == name }) {
			return refusal(name, "it is not on the read-only list: it may write, run another program or reach the network", readOnlyFix)
		}
		if optionPrograms[name] {
			return injection(name, x.Args[1:], text)
		}
		return ""
	}
	return refusal("`"+text(n)+"`", "this shell construct is not one the guard reads ("+constructName(n)+")", readOnlyFix)
}

// constructName is how a refusal names a construct the list does not hold.
func constructName(n syntax.Node) string {
	switch x := n.(type) {
	case *syntax.TestClause:
		return "[[ … ]]: use test or ["
	case *syntax.ArithmExp, *syntax.ArithmCmd, *syntax.LetClause:
		return "arithmetic"
	case *syntax.DeclClause:
		return x.Variant.Value
	case *syntax.FuncDecl:
		return "a function"
	case *syntax.TimeClause:
		return "time"
	case *syntax.CoprocClause:
		return "coproc"
	}
	return "it is outside the read-only list"
}

// programName is a program called by its name: no path, variable or glob.
var programName = regexp.MustCompile(`^(?:[A-Za-z0-9_][A-Za-z0-9_.+-]*|\[)$`)

// literalWord is a word's text when it is literal: plain, quoted, or both.
func literalWord(w *syntax.Word) (string, bool) {
	var b strings.Builder
	for _, p := range w.Parts {
		switch x := p.(type) {
		case *syntax.Lit:
			b.WriteString(x.Value)
		case *syntax.SglQuoted:
			b.WriteString(x.Value)
		case *syntax.DblQuoted:
			for _, q := range x.Parts {
				l, ok := q.(*syntax.Lit)
				if !ok {
					return "", false
				}
				b.WriteString(l.Value)
			}
		default:
			return "", false
		}
	}
	return b.String(), true
}

// optionPrograms are the listed programs that have an option that writes a
// file or runs a program: a word the shell builds before `--` may become
// one of their options.
var optionPrograms = set("rg", "find", "fd", "sort", "git", "tree", "file", "uniq", "xxd", "date", "base64")

// injection is the refusal of a word before `--` whose first character the
// shell decides at run time (a leading glob, a variable, a substitution):
// a file named `--pre=sh` or a value `-o f` would become an option.
func injection(name string, args []*syntax.Word, text func(syntax.Node) string) string {
	for _, w := range args {
		if l, ok := literalWord(w); ok && l == "--" {
			return ""
		}
		if len(w.Parts) == 0 {
			continue
		}
		lead := false
		switch x := w.Parts[0].(type) {
		case *syntax.Lit:
			lead = x.Value != "" && strings.ContainsRune("*?[", rune(x.Value[0]))
		case *syntax.SglQuoted:
			lead = x.Value == "" && len(w.Parts) > 1
		case *syntax.DblQuoted:
			lead = len(x.Parts) > 0
			if l, ok := x.Parts[0].(*syntax.Lit); ok && l.Value != "" {
				lead = false
			}
		default:
			lead = true
		}
		if lead {
			return refusal(name+" `"+text(w)+"`", "the shell decides its first character, and a file or a value starting with `-` would be one of "+name+"'s options, some of which write or run a program",
				"write it after `--`, or start it with a literal such as ./ or a name")
		}
	}
	return ""
}

// roRedirect is the refusal of a redirection that writes a file.
func roRedirect(r *syntax.Redirect, raw string) string {
	target, _ := literalWord(r.Word)
	switch r.Op {
	case syntax.RdrIn, syntax.Hdoc, syntax.DashHdoc, syntax.WordHdoc, syntax.DplIn:
		return ""
	case syntax.DplOut:
		if regexp.MustCompile(`^(?:\d+|-)$`).MatchString(target) {
			return ""
		}
	case syntax.RdrOut, syntax.AppOut, syntax.RdrClob, syntax.RdrAll, syntax.AppAll:
		if target == "/dev/null" || target == "/dev/stdout" || target == "/dev/stderr" {
			return ""
		}
	}
	return refusal("`"+raw+"`", "it writes a file", "drop the redirection: the output comes back to you, and 2>/dev/null hides errors")
}

// The programs of the list, by their rule.
var (
	// plainReaders have no option that writes a file or runs a program,
	// whatever words follow: find's -exec and xargs with operands the
	// guard does not see run only one of them.
	plainReaders = set("cat", "tac", "head", "tail", "wc", "grep", "egrep", "fgrep", "ls", "stat", "du", "df", "cut", "tr", "paste",
		"column", "nl", "fold", "rev", "comm", "join", "cmp", "diff", "od", "hexdump", "strings", "md5sum", "sha1sum", "sha224sum",
		"sha256sum", "sha384sum", "sha512sum", "b2sum", "cksum", "basename", "dirname", "realpath", "readlink", "pwd", "echo", "true",
		"false", "test", "[", "seq", "sleep", "id", "whoami", "uname", "nproc", "which", "type", "jq", ":", "cd", "pushd", "popd",
		"shift", "exit", "break", "continue")
	// execRoutes are the routes a program runs on that the list reads:
	// find's actions and xargs.
	execRoutes = set("find -exec", "find -execdir", "find -ok", "find -okdir", "xargs", sh.XargsUnseen)
)

// roRules are the listed programs that keep a rule on their words.
var roRules = map[string]func(args []string, inEnv func(string) bool) string{
	"rg": func(args []string, _ func(string) bool) string {
		for _, a := range args {
			if a == "--" {
				break
			}
			if rgRuns.MatchString(a) {
				return refusal("rg "+a, "it runs a program on each file", "search without it")
			}
			// -z / --search-zip starts a decompression program per file.
			if a == "--search-zip" || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "z")) {
				return refusal("rg "+a, "it runs a decompression program on each file", "search without it")
			}
		}
		return ""
	},
	"fd":     clusterRule("fd", "xX", []string{"--exec", "--exec-batch"}, "it runs a program on each result", "list the files, then read them"),
	"sort":   clusterRule("sort", "oT", []string{"--output", "--compress-program", "--temporary-directory"}, "it writes a file or runs a program", "sort to standard output"),
	"tree":   clusterRule("tree", "oR", []string{"--output"}, "it writes a file", "print the tree to standard output"),
	"file":   clusterRule("file", "C", []string{"--compile"}, "it writes a compiled magic file", "name the files to identify"),
	"base64": clusterRule("base64", "o", []string{"--output"}, "it writes a file", "encode to standard output"),
	"date": func(args []string, _ func(string) bool) string {
		for _, a := range args {
			if strings.HasPrefix(a, "-s") || strings.HasPrefix(a, "--se") {
				return refusal("date "+a, "it sets the clock", "print the date with a +format")
			}
		}
		for _, a := range operands(args, set("-d", "--date", "-f", "--file", "-r", "--reference"), "dfr") {
			if !strings.HasPrefix(a, "+") {
				return refusal("date "+a, "an operand that is not a +format sets the clock", "print the date with a +format")
			}
		}
		return ""
	},
	"uniq": maxOperands("uniq", set(), "fsw", 1),
	"xxd": func(args []string, inEnv func(string) bool) string {
		for _, a := range args {
			if len(a) > 1 && strings.HasPrefix("-revert", a) {
				return refusal("xxd "+a, "it writes binary back", "dump to standard output")
			}
		}
		return maxOperands("xxd", set(), "cglosn", 1)(args, inEnv)
	},
	"tee": func(args []string, _ func(string) bool) string {
		if len(operands(args, set(), "")) > 0 {
			return refusal("tee", "it writes the files it names", "drop it: the output comes back to you")
		}
		return ""
	},
	"printf": func(args []string, inEnv func(string) bool) string {
		if at(args, 0) == "-v" {
			return assignable(at(args, 1), inEnv)
		}
		return ""
	},
	"read": func(args []string, inEnv func(string) bool) string {
		for i := 0; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "-a":
				if why := assignable(at(args, i+1), inEnv); why != "" {
					return why
				}
				i++
			case a == "-d" || a == "-n" || a == "-N" || a == "-p" || a == "-t" || a == "-u" || a == "-i":
				i++
			case strings.HasPrefix(a, "-"):
			default:
				if why := assignable(a, inEnv); why != "" {
					return why
				}
			}
		}
		return ""
	},
	"set": func(args []string, _ func(string) bool) string {
		for i := 0; i < len(args); i++ {
			a := args[i]
			if (a == "-o" || a == "+o") && slices.Contains([]string{"pipefail", "errexit", "nounset", "xtrace", "verbose", "noglob"}, at(args, i+1)) {
				i++
				continue
			}
			if len(a) > 1 && (a[0] == '-' || a[0] == '+') && strings.Trim(a[1:], "euxvf") == "" {
				continue
			}
			return refusal("set "+a, "only -e, -u, -x, -v, -f and -o pipefail are read", "set only those options")
		}
		return ""
	},
	"find": func(args []string, _ func(string) bool) string {
		for _, a := range args {
			if slices.Contains([]string{"-delete", "-fprint", "-fprint0", "-fprintf", "-fls"}, a) {
				return refusal("find "+a, "it deletes or writes files", "print the results (-print, -ls) and read them")
			}
		}
		return ""
	},
	"xargs": func([]string, func(string) bool) string { return "" },
	"git":   gitReadOnly,
}

// clusterRule is a rule that refuses the short option letters (alone or in
// a cluster) and the long options (or any abbreviation of them) why says.
func clusterRule(name, letters string, long []string, why, fix string) func([]string, func(string) bool) string {
	return func(args []string, _ func(string) bool) string {
		for _, a := range args {
			if a == "--" {
				break
			}
			if strings.HasPrefix(a, "--") {
				n, _, _ := strings.Cut(a, "=")
				for _, l := range long {
					if len(n) > 2 && strings.HasPrefix(l, n) {
						return refusal(name+" "+a, why, fix)
					}
				}
				continue
			}
			if strings.HasPrefix(a, "-") && strings.ContainsAny(a[1:], letters) {
				return refusal(name+" "+a, why, fix)
			}
		}
		return ""
	}
}

// maxOperands is a rule that refuses more than n operands: the next is a file written.
func maxOperands(name string, valued map[string]bool, letters string, n int) func([]string, func(string) bool) string {
	return func(args []string, _ func(string) bool) string {
		if len(operands(args, valued, letters)) > n {
			return refusal(name, "an operand after its input is the file it writes", "give it one input and read its output")
		}
		return ""
	}
}

// roProgram is the refusal of one program a line runs, "" when the list holds it.
func roProgram(p sh.Program, inEnv func(string) bool, forms [][]string) string {
	if len(p.Argv) == 0 {
		return ""
	}
	name := p.Name
	if strings.Contains(p.Argv[0], "/") || !programName.MatchString(p.Argv[0]) {
		return refusal("`"+p.Argv[0]+"`", "a path or a built word may name any program", "call a listed program by its name")
	}
	blind := false
	for _, v := range p.Via {
		if !execRoutes[v] {
			return refusal(v+" "+name, v+" runs another program, which the list does not read through it", "run the program itself")
		}
		blind = blind || v != "xargs"
	}
	if p.Code != nil || p.StdinOpaque != "" || p.ScriptFile != "" {
		return refusal(name, "it runs code", readOnlyFix)
	}
	var args []string
	for _, a := range p.Argv[1:] {
		if a != "<xargs input>" {
			args = append(args, a)
		}
	}
	if blind {
		if !plainReaders[name] {
			return refusal(strings.Join(p.Via, " ")+" "+name, "it hands "+name+" words the guard does not read, and "+name+" is not a program with no option that writes or runs one",
				"run a program such as cat, grep or wc there, or list the files first")
		}
		return ""
	}
	if plainReaders[name] {
		return ""
	}
	// A listed program keeps its rule whatever form names it.
	if rule, ok := roRules[name]; ok {
		return rule(args, inEnv)
	}
	if len(p.Via) == 0 && slices.ContainsFunc(forms, func(f []string) bool { return len(f) > 0 && len(f) <= len(p.Argv) && slices.Equal(f, p.Argv[:len(f)]) }) {
		return ""
	}
	return refusal(name, "it is not on the read-only list: it may write, run another program or reach the network", readOnlyFix)
}

// gitGlobals are git's global options a read keeps; gitValued, those of them that take a value.
var (
	gitGlobals = set("-C", "--git-dir", "--work-tree", "--no-pager", "-P", "--literal-pathspecs", "--glob-pathspecs",
		"--noglob-pathspecs", "--icase-pathspecs", "--no-optional-locks", "--bare", "--no-replace-objects", "--version")
	// gitReads are the subcommands that only read the repository.
	gitReads = set("log", "show", "diff", "diff-tree", "diff-index", "diff-files", "whatchanged", "shortlog", "blame", "annotate",
		"grep", "ls-files", "ls-tree", "cat-file", "rev-parse", "rev-list", "show-ref", "describe", "merge-base", "name-rev",
		"for-each-ref", "status", "count-objects", "check-ignore", "check-attr", "range-diff", "cherry", "version", "var")
	// gitWrites are the options of a read that write a file or run a program.
	gitWrites = []string{"--output", "--ext-diff", "--textconv", "--filters", "--open-files-in-pager", "--exec", "--upload-pack"}
	// gitListing are the subcommands that read only in their listing forms:
	// their allowed subcommands (none: options only), their options, and
	// whether an operand needs --list.
	gitListing = map[string]struct {
		subs, opts []string
		listOnly   bool
	}{
		"branch":   {nil, []string{"-a", "-r", "-l", "--list", "-v", "-vv", "--verbose", "--all", "--remotes", "--contains", "--no-contains", "--merged", "--no-merged", "--points-at", "--sort", "--format", "--color", "--no-color", "--column", "--no-column", "--show-current", "-i", "--ignore-case", "--abbrev", "--no-abbrev"}, true},
		"tag":      {nil, []string{"-l", "--list", "-n", "--contains", "--no-contains", "--merged", "--no-merged", "--points-at", "--sort", "--format", "--color", "--column", "--no-column", "-i", "--ignore-case"}, true},
		"remote":   {[]string{"get-url"}, []string{"-v", "--verbose", "--push", "--all"}, false},
		"stash":    {[]string{"list", "show"}, nil, false},
		"worktree": {[]string{"list"}, nil, false},
		"reflog":   {[]string{"show", "exists"}, nil, false},
		"notes":    {[]string{"list", "show"}, nil, false},
		"config":   {[]string{"get", "list"}, []string{"--get", "--get-all", "--get-regexp", "--get-urlmatch", "-l", "--list", "--show-origin", "--show-scope", "--name-only", "-z", "--null", "--type", "--bool", "--int", "--path", "--default", "--global", "--system", "--local", "--worktree", "--includes", "--no-includes", "--all", "--regexp", "--url", "--value", "--fixed-value"}, false},
	}
)

// gitReadOnly is git's rule: read-only globals, a reading subcommand or a
// listing form, and no option of it that writes a file or runs a program.
func gitReadOnly(args []string, _ func(string) bool) string {
	g := sh.GitParts(args)
	for i := 0; i < len(g.Globals); i++ {
		n := g.Globals[i]
		if !gitGlobals[n] {
			return refusal("git "+n, "a global option that sets configuration, the program path or a pager changes what git runs", "drop it")
		}
		if sh.GitValueGlobals[n] {
			i++
		}
	}
	if g.Sub == "" {
		return ""
	}
	for _, a := range g.Rest {
		if a == "--" {
			break
		}
		n, _, _ := strings.Cut(a, "=")
		for _, w := range gitWrites {
			// git takes any unambiguous abbreviation of a long option.
			if len(n) > 2 && strings.HasPrefix(n, "--") && strings.HasPrefix(w, n) {
				return refusal("git "+g.Sub+" "+n, "it writes a file or runs a program", "drop the option")
			}
		}
		if g.Sub == "grep" && strings.HasPrefix(a, "-O") {
			return refusal("git grep -O", "it runs a pager on the matches", "drop the option")
		}
	}
	if gitReads[g.Sub] {
		return ""
	}
	l, ok := gitListing[g.Sub]
	if !ok {
		return refusal("git "+g.Sub, "it writes the repository, runs a program or reaches the network", "read with git log, show, diff, grep, blame, ls-files or ls-tree")
	}
	rest := g.Rest
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") && l.subs != nil {
		if !slices.Contains(l.subs, rest[0]) {
			return refusal("git "+g.Sub+" "+rest[0], "only its listing forms read ("+strings.Join(l.subs, ", ")+")", "list with git "+g.Sub+" "+l.subs[0])
		}
		if g.Sub != "config" && g.Sub != "remote" {
			return ""
		}
		rest = rest[1:]
	}
	listed, mode := false, false
	var ops []string
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		n, _, eq := strings.Cut(a, "=")
		if !strings.HasPrefix(a, "-") || a == "-" {
			ops = append(ops, a)
			continue
		}
		short := len(a) > 2 && a[1] != '-' && slices.Contains(l.opts, a[:2])
		if !slices.Contains(l.opts, n) && !short {
			return refusal("git "+g.Sub+" "+a, "only its listing options read", "list with git "+g.Sub+" "+strings.Join(l.opts[:min(2, len(l.opts))], " or "))
		}
		listed = listed || n == "-l" || n == "--list"
		mode = mode || strings.HasPrefix(n, "--get") || n == "-l" || n == "--list"
		if !eq && slices.Contains([]string{"--contains", "--no-contains", "--merged", "--no-merged", "--points-at", "--sort", "--format", "--type", "--default"}, n) &&
			i+1 < len(rest) && !strings.HasPrefix(rest[i+1], "-") {
			i++
		}
	}
	switch {
	case g.Sub == "config" && !mode && len(ops) > 1, l.listOnly && !listed && len(ops) > 0:
		return refusal("git "+g.Sub+" "+strings.Join(ops, " "), "an operand there creates or sets one", "list with git "+g.Sub+" --list")
	case g.Sub == "remote" && len(ops) > 0 && (len(g.Rest) == 0 || g.Rest[0] != "get-url"):
		return refusal("git remote "+ops[0], "only git remote, -v and get-url read", "list with git remote -v")
	}
	return ""
}
