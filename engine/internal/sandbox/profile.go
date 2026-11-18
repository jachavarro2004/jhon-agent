package sandbox

// The classifier of a command line the runtimes run with sandbox.mode on
// (T128g, L1303 G1): the allowlist proven on the delivery profile's branch
// (T128d-T128d3, 1ac9872), now answering whether a line runs outside the
// sandbox. git over SSH cannot run inside it (ssh refuses its system
// drop-ins, owned by nobody in the user namespace, L1220) and gh needs
// ~/.config/gh, which the work profile hides (L1302), so a line of git
// network commands and gh runs as the owner's shell would run it; the
// guards judge it as they judge any line.

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

// shells are the shells a runtime starts a line with: `jhon sandbox run
// --profile unsandboxed` runs `<shell> -c <line>` only with one of these.
var shells = []string{"sh", "bash", "zsh", "dash"}

// ProfileAt is ProfileFor for a line started in cwd in a project with
// roots (Input.Roots), stat as Input.Stat (T128g, L1306): a line
// ProfileFor leaves unsandboxed stays so only when each git on it works in
// a folder inside the roots (cwd, after its -C folders) and a git clone
// writes its new folder inside them; anything else gives work.
func ProfileAt(command, cwd string, roots []string, stat func(string) (string, bool, bool)) string {
	if ProfileFor(command) != Unsandboxed || !filepath.IsAbs(cwd) {
		return Work
	}
	programs, err := sh.JudgeString(command, nil, nil, sh.Opts{})
	if err != nil {
		return Work
	}
	var real []string
	for _, r := range roots {
		if filepath.IsAbs(r) {
			real = append(real, realPath(r, stat))
		}
	}
	inside := func(p string) bool { return within(realPath(p, stat), real) }
	for _, p := range programs {
		if p.Argv[0] == "git" && !gitInside(p.Argv[1:], cwd, inside) {
			return Work
		}
	}
	return Unsandboxed
}

// realPath is p, absolute and clean, with its longest existing head
// replaced by its real path (stat), so a folder not made yet is judged by
// where its parent is.
func realPath(p string, stat func(string) (string, bool, bool)) string {
	p = filepath.Clean(p)
	var tail []string
	for head := p; ; head = filepath.Dir(head) {
		if r, _, ok := stat(head); ok {
			return filepath.Join(append([]string{r}, tail...)...)
		}
		if filepath.Dir(head) == head {
			return p
		}
		tail = append([]string{filepath.Base(head)}, tail...)
	}
}

// gitInside is whether git's arguments, run in cwd, work in a folder
// inside the project (each -C taken in turn from the one before) and, for
// a clone, write the new folder inside it: the folder named after the
// repository, or the one given.
func gitInside(args []string, cwd string, inside func(string) bool) bool {
	dir, k := cwd, 0
	for ; k < len(args) && strings.HasPrefix(args[k], "-"); k++ {
		if args[k] == "-C" && k+1 < len(args) {
			k++
			if filepath.IsAbs(args[k]) {
				dir = args[k]
			} else {
				dir = filepath.Join(dir, args[k])
			}
		}
	}
	if !inside(dir) {
		return false
	}
	if k >= len(args) || args[k] != "clone" {
		return true
	}
	target := cloneTarget(args[k+1:])
	if target == "" {
		return false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(dir, target)
	}
	return inside(target)
}

// cloneTarget is the folder git clone's arguments make: the second
// operand, else the repository's name (its last part, without .git; with
// .git for --bare and --mirror); "" when there is none to judge.
func cloneTarget(args []string) string {
	var operands []string
	bare := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			operands = append(operands, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			operands = append(operands, a)
			continue
		}
		if a == "--bare" || a == "--mirror" {
			bare = true
		}
		if o, ok := gitOptions["clone"][a]; ok && o.kind == value {
			i++
		}
	}
	switch {
	case len(operands) >= 2:
		return operands[1]
	case len(operands) == 0:
		return ""
	}
	name := strings.TrimRight(operands[0], "/")
	name = strings.TrimSuffix(strings.TrimSuffix(name, "/.git"), ".git")
	name = name[strings.LastIndexAny(name, "/:")+1:]
	if name == "" || name == "." || name == ".." {
		return ""
	}
	if bare {
		name += ".git"
	}
	return name
}

// RunsOutside is whether `jhon sandbox run --profile unsandboxed` may run
// argv with no sandbox (T128g, L1303 G1): argv is `<shell> -c <line>`, the
// shell named by an absolute path, the line one ProfileAt leaves
// unsandboxed in in's working directory and project, and none of the
// programs it may start is one a sandboxed command could have written: the
// shell's file, and the git, gh and ssh the shell finds first on PATH, lie
// outside every writable area of in, and
// PATH has no relative folder (L888, F1, as for bubblewrap). Anything else
// runs with the work profile.
func RunsOutside(h Host, in Input, argv []string) bool {
	if len(argv) != 3 || argv[1] != "-c" || !filepath.IsAbs(argv[0]) || !slices.Contains(shells, filepath.Base(argv[0])) || ProfileAt(argv[2], in.Cwd, in.Roots, in.Stat) != Unsandboxed {
		return false
	}
	open := writable(in)
	trusted := func(file string) bool {
		real, isDir, ok := in.Stat(file)
		if !ok || isDir {
			return false
		}
		folder, _, _ := in.Stat(filepath.Dir(file))
		return !within(real, open) && !within(folder, open)
	}
	if !trusted(argv[0]) {
		return false
	}
	for _, dir := range in.Path {
		if !filepath.IsAbs(dir) {
			return false
		}
	}
	for _, name := range []string{"git", "gh", "ssh"} {
		for _, dir := range in.Path {
			file := filepath.Join(dir, name)
			if _, isDir, ok := in.Stat(file); !ok || isDir {
				continue
			}
			if fi, err := h.Stat(file); err != nil || fi.Mode()&0o111 == 0 {
				continue
			}
			if !trusted(file) {
				return false
			}
			break
		}
	}
	return true
}

// Unsandboxed is the answer for a line that runs outside the sandbox: not
// a profile of bubblewrap's, which Profiles lists, but `jhon sandbox run
// --profile unsandboxed` runs such a line with none (ProfileFor).
const Unsandboxed = "unsandboxed"

// An option's kind in the allowlist: a flag; one that takes a value, as
// --name=<value> or as the next word; one whose value is optional and only
// ever attached (--name or --name=<value>).
type optKind int

const (
	flag optKind = iota
	value
	optional
)

// opt is an allowed option: its kind and, for an optional value, the values
// it may take (nil for any).
type opt struct {
	kind   optKind
	values []string
}

// options are the options a git subcommand may take on an unsandboxed line, by
// their exact names: git takes an unambiguous abbreviation of a long option
// (--upl= for --upload-pack=) and a cluster of short ones (-nu<cmd>), so a
// word that is not one of these names exactly gives work (T128d2, L1250
// A1). Each one names neither a program, nor a configuration, nor a folder
// git takes hooks or objects from; none works in a submodule, whose own
// configuration Jhon never judges (T128d3, L1254 R1).
type options map[string]opt

var (
	isFlag   = opt{kind: flag}
	takesArg = opt{kind: value}
	mayArg   = opt{kind: optional}
)

func oneOf(values ...string) opt { return opt{kind: optional, values: values} }

// with is o and more.
func (o options) with(more options) options {
	out := options{}
	for k, x := range o {
		out[k] = x
	}
	for k, x := range more {
		out[k] = x
	}
	return out
}

// common are the options every network subcommand of git but ls-remote takes.
var common = options{
	"--quiet": isFlag, "-q": isFlag, "--verbose": isFlag, "-v": isFlag, "--progress": isFlag, "--no-progress": isFlag,
	"--ipv4": isFlag, "-4": isFlag, "--ipv6": isFlag, "-6": isFlag,
}

// fetchOptions are git fetch's; pull takes them too.
var fetchOptions = common.with(options{
	"--all": isFlag, "--append": isFlag, "-a": isFlag, "--atomic": isFlag, "--dry-run": isFlag, "--force": isFlag, "-f": isFlag, "--keep": isFlag, "-k": isFlag,
	"--depth": takesArg, "--deepen": takesArg, "--shallow-since": takesArg, "--shallow-exclude": takesArg, "--unshallow": isFlag, "--update-shallow": isFlag,
	"--prune": isFlag, "-p": isFlag, "--no-prune": isFlag, "--prune-tags": isFlag, "-P": isFlag, "--tags": isFlag, "-t": isFlag, "--no-tags": isFlag, "-n": isFlag,
	"--jobs": takesArg, "-j": takesArg,
	"--set-upstream": isFlag, "--write-fetch-head": isFlag, "--no-write-fetch-head": isFlag,
	"--show-forced-updates": isFlag, "--no-show-forced-updates": isFlag, "--porcelain": isFlag, "--refetch": isFlag,
})

// gitOptions are the options of each network subcommand (L1245 K1).
var gitOptions = map[string]options{
	"fetch": fetchOptions.with(options{"--multiple": isFlag}),
	"pull": fetchOptions.with(options{
		"--rebase": oneOf("true", "false", "merges"), "-r": isFlag, "--no-rebase": isFlag,
		"--ff": isFlag, "--no-ff": isFlag, "--ff-only": isFlag, "--squash": isFlag, "--no-squash": isFlag, "--commit": isFlag, "--no-commit": isFlag,
		"--no-edit": isFlag, "--stat": isFlag, "--no-stat": isFlag, "--log": isFlag, "--no-log": isFlag, "--autostash": isFlag, "--no-autostash": isFlag,
		"--allow-unrelated-histories": isFlag, "--verify": isFlag, "--no-verify": isFlag,
	}),
	"push": common.with(options{
		"--all": isFlag, "--branches": isFlag, "--mirror": isFlag, "--tags": isFlag, "--follow-tags": isFlag, "--no-follow-tags": isFlag,
		"--atomic": isFlag, "--no-atomic": isFlag, "--dry-run": isFlag, "-n": isFlag, "--porcelain": isFlag, "--delete": isFlag, "-d": isFlag, "--prune": isFlag,
		"--force": isFlag, "-f": isFlag, "--force-with-lease": mayArg, "--no-force-with-lease": isFlag, "--force-if-includes": isFlag, "--no-force-if-includes": isFlag,
		"--set-upstream": isFlag, "-u": isFlag,
		"--verify": isFlag, "--no-verify": isFlag, "--thin": isFlag, "--no-thin": isFlag, "--push-option": takesArg, "-o": takesArg, "--no-signed": isFlag,
	}),
	"ls-remote": {
		"--heads": isFlag, "--branches": isFlag, "--tags": isFlag, "-t": isFlag, "--refs": isFlag, "--quiet": isFlag, "-q": isFlag,
		"--exit-code": isFlag, "--get-url": isFlag, "--symref": isFlag,
	},
	"clone": common.with(options{
		"--no-checkout": isFlag, "-n": isFlag, "--bare": isFlag, "--mirror": isFlag, "--origin": takesArg, "-o": takesArg, "--branch": takesArg, "-b": takesArg,
		"--depth": takesArg, "--shallow-since": takesArg, "--shallow-exclude": takesArg, "--single-branch": isFlag, "--no-single-branch": isFlag,
		"--tags": isFlag, "--no-tags": isFlag, "--jobs": takesArg, "-j": takesArg, "--filter": takesArg, "--sparse": isFlag,
		"--reject-shallow": isFlag, "--no-reject-shallow": isFlag, "--no-hardlinks": isFlag,
	}),
	// The words after `remote update`.
	"remote update": {"--prune": isFlag, "-p": isFlag},
}

// beforeSecond are the options git remote takes before its own subcommand.
var beforeSecond = map[string]options{
	"remote": {"--verbose": isFlag, "-v": isFlag},
}

// gitGlobals are the global options an unsandboxed line may give git, besides
// -C <folder>: none of them names a program, a configuration or a folder.
var gitGlobals = []string{"--no-pager", "-P", "--no-optional-locks"}

// ghSubcommands are the gh commands an unsandboxed line may run: gh's own,
// none of which prints a token or installs or runs other code. Any other
// first word (auth, extension, alias, config, and any name gh does not
// know, which an extension may hold) gives work.
var ghSubcommands = []string{"pr", "issue", "run", "workflow", "release", "repo", "api", "search", "label", "status", "browse", "cache"}

// ProfileFor is how a runtime runs a command line with sandbox.mode on
// (T128g, L1303 G1; the classifier of T128d, L1245 K1): Unsandboxed when
// every program the shell reader finds on it is git with a network
// subcommand (fetch, pull, push, ls-remote, clone, remote update) or gh with
// one of ghSubcommands that runs no git and reads no local file
// (ghDelivers), and the line writes no file through the shell; Work for any
// other line. The pi shell asks it through `jhon sandbox profile` and
// Claude Code's wrap calls it, so both runtimes take the same answer; `jhon
// sandbox run --profile unsandboxed` asks it again before it runs a line
// with no sandbox.
//
// It is an allowlist and fails closed (T128d2, L1250 A1): a line the reader
// cannot read, an empty one, one that defines a function or an alias, and
// one with a word the shell would still rewrite get work. That is a brace
// ({a,b} expands after the check), a parameter, a command substitution, a
// glob. So does any git global option but -C <folder>, --no-pager, -P and
// --no-optional-locks; any option of the subcommand not in its list, by its
// exact name (gitOptions); a word after `--` that starts with `-`, which
// git pull and git remote update hand to fetch as an option (T128d3, L1254
// R1); a remote helper's <transport>::<address>; a gh global option before
// the subcommand, or `--`, after which gh passes words to git; a program
// reached through a path, a wrapper, a shell; a variable set on the line.
// So does a redirect but a descriptor's copy (2>&1) and a write to
// /dev/null (quiet): outside the sandbox, `gh api ... > <file>` would write
// what gh fetched anywhere (T128g).
func ProfileFor(command string) string {
	cmds, err := sh.Parse(command)
	if err != nil || sh.DefinesFunction(command) {
		return Work
	}
	for _, c := range cmds {
		if slices.ContainsFunc(c.Redirects, func(r *sh.Redirect) bool { return !quiet(r) }) {
			return Work
		}
		if len(c.Words) == 0 {
			continue
		}
		// A variable set by a line of its own reaches the programs after it
		// when the environment exports it: the reader keeps no program for
		// that line. An alias replaces the program it names.
		if sh.Assignment.MatchString(c.Words[0]) || c.Words[0] == "alias" {
			return Work
		}
		if slices.ContainsFunc(c.Words, braced) {
			return Work
		}
	}
	programs, err := sh.JudgeString(command, nil, nil, sh.Opts{})
	if err != nil || len(programs) == 0 {
		return Work
	}
	for _, p := range programs {
		if !networkProgram(p) {
			return Work
		}
	}
	return Unsandboxed
}

// quiet is whether a redirect writes no file: a copy or a close of a
// descriptor (2>&1, >&2, <&-), or output to /dev/null.
func quiet(r *sh.Redirect) bool {
	switch r.Op {
	case ">&", "<&":
		return r.Target == "-" || r.Target != "" && strings.Trim(r.Target, "0123456789") == ""
	case ">", ">>", "&>", "&>>", ">|":
		return r.Target == "/dev/null"
	}
	return false
}

// networkProgram is whether p is git with a network subcommand and only
// listed options, or gh with a listed subcommand, run as its bare name.
func networkProgram(p sh.Program) bool {
	if len(p.Via) > 0 || len(p.Env) > 0 || p.Chdir != nil || p.Code != nil || p.StdinOpaque != "" || len(p.Argv) < 2 {
		return false
	}
	if slices.ContainsFunc(p.Argv, expands) || slices.ContainsFunc(p.Argv, braced) {
		return false
	}
	switch p.Argv[0] {
	case "gh":
		return slices.Contains(ghSubcommands, p.Argv[1]) && !slices.Contains(p.Argv, "--") && ghDelivers(p.Argv[1], p.Argv[2:])
	case "git":
		return gitDelivers(p.Argv[1:])
	}
	return false
}

// gitDelivers is whether git's arguments are allowed globals, a network
// subcommand, and only that subcommand's listed options.
func gitDelivers(args []string) bool {
	if slices.ContainsFunc(args, func(a string) bool { return strings.Contains(a, "::") }) {
		return false // ext::, fd:: and any remote helper's <transport>::<address>
	}
	k := 0
	for ; k < len(args) && strings.HasPrefix(args[k], "-"); k++ {
		if args[k] == "-C" {
			k++
			// A folder the shell would rewrite (~) is not the one read.
			if k >= len(args) || args[k] == "" || strings.HasPrefix(args[k], "~") {
				return false
			}
			continue
		}
		if !slices.Contains(gitGlobals, args[k]) {
			return false
		}
	}
	if k >= len(args) {
		return false
	}
	sub, rest := args[k], args[k+1:]
	if before, ok := beforeSecond[sub]; ok {
		j := 0
		for ; j < len(rest) && strings.HasPrefix(rest[j], "-"); j++ {
			if _, ok := before[rest[j]]; !ok {
				return false
			}
		}
		if j >= len(rest) || rest[j] != "update" {
			return false
		}
		sub, rest = sub+" update", rest[j+1:]
	}
	allowed, ok := gitOptions[sub]
	if !ok {
		return false
	}
	return onlyListed(rest, allowed)
}

// onlyListed is whether every option of args is one of allowed by its exact
// name, with a value only where it takes one, and no word after a `--`
// starts with `-`: git pull and git remote update hand those words to
// fetch, which reads them as options (T128d3, L1253, L1254 R1).
func onlyListed(args []string, allowed options) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return !slices.ContainsFunc(args[i+1:], func(w string) bool { return strings.HasPrefix(w, "-") })
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			continue
		}
		name, val, attached := strings.Cut(a, "=")
		o, ok := allowed[name]
		if !ok {
			return false
		}
		switch {
		case attached && !strings.HasPrefix(name, "--"):
			return false // a short option takes its value as the next word only
		case o.kind == flag && attached:
			return false
		case o.kind == value && !attached:
			i++ // the value
			if i >= len(args) {
				return false
			}
		case o.kind == optional && attached && o.values != nil && !slices.Contains(o.values, val):
			return false
		}
	}
	return true
}

// expands is whether the shell would still expand a word: a parameter, a
// command substitution or a glob, whose value the reader does not know.
func expands(word string) bool { return strings.ContainsAny(word, "$`*?[") }

// braced is whether a word holds a brace, which the shell's brace expansion
// may turn into other words after the check ({--upload-pack=x,repo}).
func braced(word string) bool { return strings.ContainsAny(word, "{}") }

// ghFileFlags are gh's options that read a local file: their value is a
// path (or - for stdin), as --name <file> or --name=<file>.
var ghFileFlags = []string{"--input", "--body-file", "--notes-file"}

// ghFileShort are the gh commands whose -F reads a local file (--body-file
// for issue and pr, --notes-file for release); elsewhere -F is --field.
var ghFileShort = []string{"issue", "pr", "release"}

// ghReleaseValues are the options of gh release create that take a value as
// the next word: the operands after the tag are files it uploads.
var ghReleaseValues = []string{"-t", "--title", "-n", "--notes", "--target", "--discussion-category", "--notes-start-tag", "-R", "--repo"}

// ghDelivers is whether gh sub, with the words after it, runs no git and
// reads no local file (T128d3, L1254 R1): outside the sandbox gh sees
// every file, the keys included, and may send what it reads. pr checkout
// runs git; --input,
// --body-file and --notes-file read a file; so does -F in issue, pr and
// release, and a -F or --field value that starts with @ (the part after
// the first =, as gh reads key=@file) elsewhere; release upload sends files,
// and so does release create given more than the tag. A word is matched
// wherever it is on the line, so an option before the second word hides
// nothing.
func ghDelivers(sub string, args []string) bool {
	for i, a := range args {
		name, val, attached := strings.Cut(a, "=")
		next := func() string {
			if attached {
				return val
			}
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		switch {
		case strings.HasPrefix(a, "--"):
			if slices.Contains(ghFileFlags, name) || name == "--field" && readsFile(next()) {
				return false
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			// A cluster of short flags (-dF): the value of -F is the rest of
			// the word, or the next one.
			if k := strings.IndexByte(a, 'F'); k > 0 {
				if slices.Contains(ghFileShort, sub) {
					return false
				}
				field := a[k+1:]
				if field == "" && i+1 < len(args) {
					field = args[i+1]
				}
				if readsFile(field) {
					return false
				}
			}
		}
	}
	// What writes files where the line names (T128g): outside the sandbox
	// a download or a clone could write anywhere. repo clone and sync, and
	// repo fork, repo create and issue develop asked to clone or check out,
	// run git in a folder Jhon does not judge.
	clones := slices.Contains(args, "--clone") || slices.Contains(args, "-c") || slices.Contains(args, "--checkout") || slices.ContainsFunc(args, func(a string) bool {
		return strings.HasPrefix(a, "--clone=") || strings.HasPrefix(a, "--checkout=")
	})
	switch sub {
	case "repo":
		if slices.Contains(args, "clone") || slices.Contains(args, "sync") || (slices.Contains(args, "fork") || slices.Contains(args, "create")) && clones {
			return false
		}
	case "run":
		return !slices.Contains(args, "download")
	case "issue":
		return !(slices.Contains(args, "develop") && clones)
	case "pr":
		return !slices.Contains(args, "checkout")
	case "release":
		if slices.Contains(args, "upload") || slices.Contains(args, "download") {
			return false
		}
		if k := slices.Index(args, "create"); k >= 0 {
			return ghOperands(args[k+1:], ghReleaseValues) <= 1
		}
	}
	return true
}

// readsFile is whether a gh field (key=value) reads its value from a file:
// the value, the part after the first =, starts with @.
func readsFile(field string) bool {
	if _, v, ok := strings.Cut(field, "="); ok {
		return strings.HasPrefix(v, "@")
	}
	return strings.HasPrefix(field, "@")
}

// ghOperands is how many words of args are operands: not an option, and not
// the value of one of values given as the next word. An option this list
// does not know is taken as a flag, so its value counts as an operand.
func ghOperands(args, values []string) int {
	n := 0
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case strings.HasPrefix(a, "-") && len(a) > 1:
			if slices.Contains(values, a) {
				i++
			}
		default:
			n++
		}
	}
	return n
}
