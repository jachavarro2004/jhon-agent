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

	"mvdan.cc/sh/v3/syntax"

	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

// shells are the shells a runtime starts a line with: `jhon sandbox run
// --profile unsandboxed` runs `<shell> -c <line>` only with one of these.
var shells = []string{"sh", "bash", "zsh", "dash"}

// ProfileAt is ProfileFor for a line started in cwd in a project with
// roots (Input.Roots), stat as Input.Stat (T128g, L1306): a line
// ProfileFor leaves unsandboxed stays so only when each git on it works in
// a folder inside the roots (cwd, after its -C folders) and a git clone
// writes its new folder inside them; anything else gives work. A leading
// `cd <folder> &&` (leadingCd) is taken as -C (T181, L1503): the line
// stays unsandboxed only when the folder is inside the roots, and its
// programs then work there. tools are sandbox.deploy_tools (Input.DeployTools):
// programs the owner lists, which a line runs as it runs docker. It knows
// no sandbox.allow_write folder: ProfileIn also counts a git repository
// inside one (T251).
func ProfileAt(command, cwd string, roots []string, stat func(string) (string, bool, bool), tools []string) string {
	return profileAt(Input{Cwd: cwd, Stat: stat, DeployTools: tools}, command, roots, false)
}

// ProfileIn is ProfileAt for a line started in in.Cwd in the project of in
// (its Roots, Stat and DeployTools), where a git repository inside one of
// in.AllowWrite counts as a project too (T251, L1722): a line whose every
// git works in one such repository, which the sandbox opens for writing
// and the line names with no link on the way (placeOf), stays unsandboxed
// as a line of the project does. A line that works in the project and in
// such a repository, or in two of them, gives work. `jhon sandbox profile`
// and Claude Code's wrap ask it; `jhon sandbox run --profile unsandboxed`
// asks again (Outside).
func ProfileIn(in Input, command string) string {
	return profileAt(in, command, in.Roots, false)
}

// profileAt is ProfileIn with the project's roots given, for a line zsh
// runs when zsh is set: a relative cd folder gives work, as zsh may find it
// through a cdpath ~/.zshenv sets (bash and sh take CDPATH only from the
// environment, which an unsandboxed line runs without, UnsandboxedEnv).
func profileAt(in Input, command string, roots []string, zsh bool) string {
	if classify(command, zsh, in.DeployTools) != Unsandboxed || !filepath.IsAbs(in.Cwd) {
		return Work
	}
	if _, p := in.placeOf(command, roots, zsh); p != nil {
		return Work
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

// gitFolder is the folder git's arguments, run in cwd, work in (each -C
// taken in turn from the one before), and the index of the first word after
// git's own options.
func gitFolder(args []string, cwd string) (string, int) {
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
	return dir, k
}

// GitFolders are the folders whose git configuration an unsandboxed line
// started in cwd may read (T249 A2): the folder it works in (cwd, or the
// one its leading `cd` names, where gh, docker and a deploy tool may run
// git too) and the one each git on it works in (gitFolder), each once.
// nil for a line the reader cannot read.
func GitFolders(command, cwd string) []string {
	programs, err := sh.JudgeString(command, nil, nil, sh.Opts{})
	if err != nil || !filepath.IsAbs(cwd) {
		return nil
	}
	if dir, ok := leadingCd(command); ok {
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(cwd, dir)
		}
		cwd = dir
	}
	out := []string{filepath.Clean(cwd)}
	for _, p := range programs {
		if len(p.Argv) == 0 || p.Argv[0] != "git" {
			continue
		}
		if dir, _ := gitFolder(p.Argv[1:], cwd); !slices.Contains(out, filepath.Clean(dir)) {
			out = append(out, filepath.Clean(dir))
		}
	}
	return out
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
// shell's file, and the git, gh, ssh, docker, docker-compose and each of
// in.DeployTools the shell finds first on PATH, lie outside every writable
// area of in, and
// PATH has no relative folder (L888, F1, as for bubblewrap). Anything else
// runs with the work profile. The project's roots are the ones that are no
// link (ownRoots, T249). The line then runs with OutsidePath.
//
// A git repository inside a sandbox.allow_write folder counts as the
// project (T251, ProfileIn, Outside).
func RunsOutside(h Host, in Input, argv []string) bool {
	_, _, ok := Outside(h, in, argv)
	return ok
}

// Outside is RunsOutside with where the line works and why it does not run
// outside the sandbox (T251, L1722): ok when it runs outside, with top the
// git repository inside a sandbox.allow_write folder its git works in, ""
// for the project (placeOf; the run then checks that repository as it
// checks the project: its folders, LineFoldersProblem, its git keys and
// folders, LineGitFolderProblem, and the registry, OutsideAreas). Not ok,
// kept is the reason a delivery line's note names: where its git works
// (placeOf) or a program a sandboxed command may have written; nil for a
// line that is no `<shell> -c <line>` of programs that run outside, whose
// note is DeliveryNote's.
func Outside(h Host, in Input, argv []string) (top string, kept *Problem, ok bool) {
	if len(argv) != 3 || argv[1] != "-c" || !filepath.IsAbs(argv[0]) || !slices.Contains(shells, filepath.Base(argv[0])) {
		return "", nil, false
	}
	zsh := filepath.Base(argv[0]) == "zsh"
	if classify(argv[2], zsh, in.DeployTools) != Unsandboxed {
		return "", nil, false
	}
	if !filepath.IsAbs(in.Cwd) {
		// A folder removed under the process has no name (getcwd fails):
		// say so, not that the line holds something (L1731).
		return "", &Problem{Reason: "the folder this line runs in cannot be named (it was removed, or is not an absolute path)", Fix: "run the line from a folder that exists"}, false
	}
	top, p := in.placeOf(argv[2], in.ownRoots(), zsh)
	if p == nil {
		p = trustProblem(h, in, argv[0])
	}
	if p != nil {
		return "", p, false
	}
	return top, nil, true
}

// trustProblem is why a line run by shell may not run outside the sandbox
// of in for the programs it would start (L888, F1), nil when it may: the
// shell's file, and the git, gh, ssh, docker, docker-compose and each of
// in.DeployTools the shell finds first on PATH, lie outside every writable
// area of in, and PATH has no relative folder.
func trustProblem(h Host, in Input, shell string) *Problem {
	open := writable(in)
	trusted := func(file string) bool {
		real, isDir, ok := in.Stat(file)
		if !ok || isDir {
			return false
		}
		folder, _, _ := in.Stat(filepath.Dir(file))
		return !within(real, open) && !within(folder, open)
	}
	if !trusted(shell) {
		return &Problem{Reason: "the shell " + shell + " is not a file outside the folders a sandboxed command may write", Fix: "run the line with a shell installed for the whole system, such as /usr/bin/bash"}
	}
	for _, dir := range in.Path {
		if !filepath.IsAbs(dir) {
			return &Problem{Reason: "PATH holds the relative folder " + dir + ", which names another folder wherever a program runs", Fix: "take " + dir + " off PATH, or name it by its absolute path"}
		}
	}
	for _, name := range slices.Concat([]string{"git", "gh", "ssh", "docker", "docker-compose"}, in.DeployTools) {
		for _, dir := range in.Path {
			file := filepath.Join(dir, name)
			if _, isDir, ok := in.Stat(file); !ok || isDir {
				continue
			}
			if fi, err := h.Stat(file); err != nil || fi.Mode()&0o111 == 0 {
				continue
			}
			if !trusted(file) {
				return &Problem{Reason: name + " on PATH is " + file + ", a file a sandboxed command may write", Fix: "take " + dir + " off PATH, or put a folder that holds " + name + " and that no sandboxed command may write before it"}
			}
			break
		}
	}
	return nil
}

// OutsidePath is PATH for a line that runs outside the sandbox (RunsOutside,
// T249 A1): PATH's folders, in their order, but a relative one and one a
// sandboxed command may change (mayChange: a project folder, the caches,
// $TMPDIR, sandbox.allow_write, any folder on the way to one of them), so
// no program a command planted there is what git, gh, docker or a deploy
// tool runs by its name outside the sandbox. A folder of the project's
// roots is left out whether or not the sandbox binds it.
func OutsidePath(in Input) string {
	mounts, _ := layout(in, false)
	var roots []string
	for _, r := range slices.Concat(in.Roots, []string{in.CommonDir}, in.Worktrees) {
		if filepath.IsAbs(r) {
			roots = append(roots, filepath.Clean(r), realPath(r, in.Stat))
		}
	}
	var keep []string
	for _, dir := range in.Path {
		if !filepath.IsAbs(dir) {
			continue
		}
		if within(filepath.Clean(dir), roots) || within(realPath(dir, in.Stat), roots) || changeable(mounts, dir) {
			continue
		}
		keep = append(keep, dir)
	}
	return strings.Join(keep, string(filepath.ListSeparator))
}

// Unsandboxed is the answer for a line that runs outside the sandbox: not
// a profile of bubblewrap's, but `jhon sandbox run --profile unsandboxed`
// runs such a line with none (ProfileFor).
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

// dockerValues are docker's global options that take a value (as the next
// word or after =), and dockerFlags the ones that take none: any other
// word before the command gives work, as its value is not known.
var (
	dockerValues = []string{"--config", "-c", "--context", "-H", "--host", "-l", "--log-level", "--tlscacert", "--tlscert", "--tlskey"}
	dockerFlags  = []string{"-D", "--debug", "--tls", "--tlsverify", "-v", "--version"}
)

// dockerContexts are the docker context commands that only read the
// owner's contexts; create, use, rm, update, import and export change
// them or write a file.
var dockerContexts = []string{"ls", "list", "inspect", "show"}

// dockerDelivers is whether docker's arguments run a command that leaves
// the owner's credentials and contexts as they are (T128h, L1332 K1):
// Docker is the owner's deployment tool (L1331), so any command runs
// outside the sandbox, its volume mounts unjudged, but login, logout and a
// change of contexts.
func dockerDelivers(args []string) bool {
	k, version := 0, false
	for ; k < len(args) && strings.HasPrefix(args[k], "-"); k++ {
		name, _, attached := strings.Cut(args[k], "=")
		version = version || name == "-v" || name == "--version"
		switch {
		case slices.Contains(dockerValues, name):
			if !attached {
				k++
			}
		case !slices.Contains(dockerFlags, args[k]):
			return false
		}
	}
	if k >= len(args) {
		// docker --version prints the client's version (T128h2, L1338).
		return version
	}
	switch args[k] {
	case "login", "logout":
		return false
	case "context":
		return k+1 < len(args) && slices.Contains(dockerContexts, args[k+1])
	}
	return true
}

// ProfileFor is how a runtime runs a command line with sandbox.mode on
// (T128g, L1303 G1; the classifier of T128d, L1245 K1): Unsandboxed when
// every program the shell reader finds on it is git with a network
// subcommand (fetch, pull, push, ls-remote, clone, remote update) or gh with
// one of ghSubcommands that runs no git and reads no local file
// (ghDelivers), or docker or docker-compose with any command but docker's
// login, logout and a change of contexts (dockerDelivers, T128h, L1332
// K1), or a program the owner lists in sandbox.deploy_tools (aws, cdk:
// a deploy tool, run as docker is, ProfileAt's tools), and the line writes
// no file through the shell; Work for any other line. The pi shell asks it through `jhon sandbox profile` and
// Claude Code's wrap calls it, so both runtimes take the same answer; `jhon
// sandbox run --profile unsandboxed` asks it again before it runs a line
// with no sandbox.
//
// The line may open with `cd <folder> &&` (leadingCd, T181, L1503): the
// folder is judged where the line runs (ProfileAt), as -C is.
//
// It is an allowlist and fails closed (T128d2, L1250 A1): a line the reader
// cannot read, an empty one, one that defines a function or an alias, and
// one with a word the shell would still rewrite get work. That is a brace
// ({a,b} expands after the check; a lone {word} with only letters, digits,
// _ and - does not, so gh's {owner} and {repo} are text, T181), a
// parameter, a command substitution, a
// glob, a $ or a backtick, unquoted (literal); a single-quoted word, and a
// double-quoted one with no $ or backtick, reaches its program as written
// and is judged by its text (T128h2, L1338). So does any git global option but -C <folder>, --no-pager, -P and
// --no-optional-locks; any option of the subcommand not in its list, by its
// exact name (gitOptions); a word after `--` that starts with `-`, which
// git pull and git remote update hand to fetch as an option (T128d3, L1254
// R1); a remote helper's <transport>::<address>; a gh global option before
// the subcommand, or `--`, after which gh passes words to git; a program
// reached through a path, a wrapper, a shell; a variable set on the line.
// So does a redirect but a descriptor's copy (2>&1) and a write to
// /dev/null (quiet): outside the sandbox, `gh api ... > <file>` would write
// what gh fetched anywhere (T128g).
func ProfileFor(command string) string { return classify(command, false, nil) }

// classify is ProfileFor for a line zsh runs when zsh is set: any unquoted
// brace gives work, as zsh expands a lone {word} under BRACE_CCL, an
// option ~/.zshenv may set for zsh -c. tools are the deploy tools
// (ProfileAt).
func classify(command string, zsh bool, tools []string) string {
	cmds, err := sh.Parse(command)
	if err != nil || sh.DefinesFunction(command) || !literal(command, zsh) || !foreground(command) {
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
	}
	programs, err := sh.JudgeString(command, nil, nil, sh.Opts{})
	if err != nil {
		return Work
	}
	if _, ok := leadingCd(command); ok && len(programs) > 0 && programs[0].Argv[0] == "cd" {
		programs = programs[1:]
	}
	if len(programs) == 0 {
		return Work
	}
	for _, p := range programs {
		if !networkProgram(p, tools) {
			return Work
		}
	}
	return Unsandboxed
}

// foreground is whether every command of the line runs in the foreground
// (T249, L36 B1): no statement put in the background (&, also inside a
// subshell or a group) and no coprocess, so the line's programs end before
// the shell does and no program it started outlives the run's record in
// the registry. A line the parser cannot read is not.
func foreground(command string) bool {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
	if err != nil {
		return false
	}
	ok := true
	syntax.Walk(f, func(n syntax.Node) bool {
		switch x := n.(type) {
		case *syntax.Stmt:
			if x.Background || x.Coprocess {
				ok = false
			}
		case *syntax.CoprocClause:
			ok = false
		}
		return ok
	})
	return ok
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
// listed options, gh with a listed subcommand, docker or docker-compose
// (dockerDelivers), or one of tools, the owner's deploy tools, run as its
// bare name. git, gh and docker keep their own rules when tools name them.
func networkProgram(p sh.Program, tools []string) bool {
	if len(p.Via) > 0 || len(p.Env) > 0 || p.Chdir != nil || p.Code != nil || p.StdinOpaque != "" || len(p.Argv) < 2 {
		return false
	}
	switch p.Argv[0] {
	case "gh":
		return slices.Contains(ghSubcommands, p.Argv[1]) && !slices.Contains(p.Argv, "--") && ghDelivers(p.Argv[1], p.Argv[2:])
	case "git":
		return gitDelivers(p.Argv[1:])
	case "docker":
		return dockerDelivers(p.Argv[1:])
	case "docker-compose":
		return true
	}
	return slices.Contains(tools, p.Argv[0])
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

// leadingCd is the folder a line opens with `cd <folder> &&`, and whether
// it does (T181, L1503): the line is one command, every && on the way to
// its first command, which is `cd` with one literal folder (quotes
// removed), not an option, not -, with no .. part (a logical cd through a
// link and a physical one differ there), no backslash and no brace, and
// with no redirect, no assignment, no negation and nothing in the
// background. What follows the cd runs only once it succeeded, in that
// folder.
func leadingCd(command string) (string, bool) {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
	if err != nil || len(f.Stmts) != 1 {
		return "", false
	}
	plain := func(s *syntax.Stmt) bool {
		return s != nil && !s.Background && !s.Negated && !s.Coprocess && len(s.Redirs) == 0
	}
	s, joined := f.Stmts[0], false
	for plain(s) {
		b, isBinary := s.Cmd.(*syntax.BinaryCmd)
		if !isBinary {
			break
		}
		if b.Op != syntax.AndStmt {
			return "", false
		}
		s, joined = b.X, true
	}
	if !joined || !plain(s) {
		return "", false
	}
	c, isCall := s.Cmd.(*syntax.CallExpr)
	if !isCall || len(c.Assigns) != 0 || len(c.Args) != 2 || c.Args[0].Lit() != "cd" {
		return "", false
	}
	var dir strings.Builder
	for _, p := range c.Args[1].Parts {
		switch x := p.(type) {
		case *syntax.Lit:
			dir.WriteString(x.Value)
		case *syntax.SglQuoted:
			if x.Dollar {
				return "", false
			}
			dir.WriteString(x.Value)
		case *syntax.DblQuoted:
			for _, q := range x.Parts {
				l, isLit := q.(*syntax.Lit)
				if !isLit || x.Dollar {
					return "", false
				}
				dir.WriteString(l.Value)
			}
		default:
			return "", false
		}
	}
	folder := dir.String()
	if !literalWord(c.Args[1], true) || folder == "" || strings.HasPrefix(folder, "-") || strings.ContainsAny(folder, `\{}`) ||
		slices.Contains(strings.Split(folder, "/"), "..") {
		return "", false
	}
	return folder, true
}

// literal is whether the shell passes every word of command as written
// (T128h2, L1338): the words the reader gives have their quotes removed,
// so this reads the line's own words. A part outside quotes with a $, a
// backtick, a glob character or a brace ({--upload-pack=x,repo} becomes
// other words after the check) is not, but a lone {word} (loneBraces) for
// a shell that is not zsh; nor is a parameter, a substitution
// or an extended glob, quoted or not; a single-quoted part ('…', $'…') is,
// and so is a double-quoted one with no $ or backtick.
func literal(command string, zsh bool) bool {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
	if err != nil {
		return false
	}
	ok := true
	syntax.Walk(f, func(n syntax.Node) bool {
		if w, isWord := n.(*syntax.Word); isWord && !literalWord(w, zsh) {
			ok = false
		}
		return ok
	})
	return ok
}

// literalWord is whether w is literal, as literal reads a word. A word
// with an unquoted ~ at its start, or after = or :, is not (T128h3, L1350): the shell may put a home folder there, outside the
// project (git clone <url> ~/y, docker run -v ~/x:/x); after : it gives
// work though bash expands it only in an assignment, to fail closed. With
// zsh set any unquoted brace gives work.
func literalWord(w *syntax.Word, zsh bool) bool {
	for i, p := range w.Parts {
		switch x := p.(type) {
		case *syntax.Lit:
			if strings.ContainsAny(x.Value, "$`*?[") || i == 0 && strings.HasPrefix(x.Value, "~") || strings.Contains(x.Value, "=~") || strings.Contains(x.Value, ":~") {
				return false
			}
			if strings.ContainsAny(x.Value, "{}") && (zsh || !loneBraces(x.Value)) {
				return false
			}
		case *syntax.SglQuoted:
		case *syntax.DblQuoted:
			if x.Dollar {
				return false
			}
			for _, q := range x.Parts {
				if l, isLit := q.(*syntax.Lit); !isLit || strings.ContainsAny(l.Value, "$`") {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}

// loneBraces is whether every brace of an unquoted part s opens a lone
// {word} that bash and sh leave as it is (T181, L1503): one or more
// letters, digits, _ or -, so no , and no .. a brace expansion needs, and
// closes it; any other brace gives false.
func loneBraces(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '}':
			return false
		case '{':
			j := strings.IndexByte(s[i+1:], '}')
			if j <= 0 || strings.IndexFunc(s[i+1:i+1+j], func(r rune) bool {
				return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-')
			}) >= 0 {
				return false
			}
			i += j + 1
		}
	}
	return true
}

// DeliveryNote is the note a work run of argv ends with when it fails and
// holds git with a network subcommand or gh (T181, L1503): argv itself, or
// the line of `<shell> -c <line>` and the words after its $0 (the Claude
// Code wrap passes the command there). "" when it holds none. It is the
// note of a line kept inside for its shape: DeliveryNoteAt gives the real
// reason of one kept inside for another.
func DeliveryNote(argv []string) string {
	if slices.ContainsFunc(linePrograms(argv), delivers) {
		return deliveryNote
	}
	return ""
}

// DeliveryNoteAt is DeliveryNote for a work run of argv in in, which names
// the real reason its line ran in the sandbox (T251, L1721): kept's, the
// reason the run found when it was asked to run the line outside (a check
// that refused it), when it is not nil; else, for `<shell> -c <line>` (or
// the command the Claude Code wrap passes after the shell's $0), the reason
// Outside gives for that line (where its git works, a planted program).
// Only a line kept inside for its shape (a pipe, another command) gets
// DeliveryNote's "run it alone". "" when argv holds no git network
// command and no gh.
func DeliveryNoteAt(h Host, in Input, argv []string, kept *Problem) string {
	if DeliveryNote(argv) == "" {
		return ""
	}
	if kept != nil {
		return keptNote + kept.Line()
	}
	if len(argv) >= 3 && argv[1] == "-c" {
		lines := []string{argv[2]}
		if len(argv) > 4 {
			lines = append(lines, argv[4:]...)
		}
		for _, line := range lines {
			if _, p, _ := Outside(h, in, []string{argv[0], "-c", line}); p != nil {
				return keptNote + p.Line()
			}
		}
		// A line that is alone already is kept inside for what it holds,
		// not for its company (L1729).
		for _, line := range lines {
			if DeliveryNote([]string{argv[0], "-c", line}) != "" && alone(line) {
				return aloneNote
			}
		}
	}
	return deliveryNote
}

// alone is whether line runs one program, after a leading `cd <folder> &&`
// (leadingCd): one command, no pipe, no list, no substitution, nothing in
// the background. What git or gh runs from its own words (an option's
// program such as --upload-pack, a configured command, an alias; the guard
// reads them since T228) is something the line holds, not a command beside
// it, so it is not counted.
func alone(line string) bool {
	cmds, err := sh.Parse(line)
	programs, err2 := sh.JudgeString(line, nil, nil, sh.Opts{})
	if err != nil || err2 != nil || !foreground(line) {
		return false
	}
	programs = slices.DeleteFunc(programs, func(p sh.Program) bool {
		return slices.ContainsFunc(p.Via, func(v string) bool { b := sh.Base(v); return b == "git" || b == "gh" })
	})
	n := 0
	for _, c := range cmds {
		if len(c.Words) > 0 {
			n++
		}
	}
	if _, ok := leadingCd(line); ok && len(programs) > 0 && programs[0].Argv[0] == "cd" {
		n, programs = n-1, programs[1:]
	}
	return n == 1 && len(programs) == 1
}

// aloneNote is DeliveryNoteAt's note of a line that is alone but kept
// inside for what it holds.
const aloneNote = keptNote + "the line is alone, but holds an option, a word the shell would still rewrite ($, a backtick, ~, a glob or brace), a redirect to a file, or a subcommand that never runs outside the sandbox; fix: run git's fetch, pull, push, ls-remote, clone or remote update, or gh's own commands, with only their usual options and literal words, or run the line yourself in a terminal"

// keptNote opens DeliveryNoteAt's note of a line kept inside for a reason
// other than its shape.
const keptNote = "git over SSH and gh ran in the sandbox, not outside it: "

// DeployNote is the note a work run of argv ends with, whatever its exit
// but a signal's, when its line holds a program tools (sandbox.deploy_tools)
// lists (T217): argv read as DeliveryNote reads it, each program matched by
// its base name. It names the listed programs found, in the order of tools,
// and gives the first as the example of a line of its own. "" when it holds
// none, or tools is empty.
func DeployNote(argv, tools []string) string {
	found := deployFound(argv, tools)
	const rule = " outside the sandbox (sandbox.deploy_tools) only on a line of "
	const rest = ", with no pipes, ;, $(…), ~, $VAR or VAR=value in front, or other commands: "
	const options = " <arguments>, and pass settings as options (--region, --profile), not variables"
	switch len(found) {
	case 0:
		return ""
	case 1:
		return found[0] + " runs" + rule + "its own, called by its name" + rest + "run it alone, as in " + found[0] + options
	}
	names := strings.Join(found[:len(found)-1], ", ") + " and " + found[len(found)-1]
	return names + " run" + rule + "their own, called by their names" + rest + "run each alone, as in " + found[0] + options
}

// deployFound are the programs of tools argv holds, in the order of
// tools, each matched by its base name (DeployNote).
func deployFound(argv, tools []string) []string {
	var found []string
	programs := linePrograms(argv)
	for _, tool := range tools {
		if slices.ContainsFunc(programs, func(p sh.Program) bool { return len(p.Argv) > 0 && filepath.Base(p.Argv[0]) == tool }) {
			found = append(found, tool)
		}
	}
	return found
}

// DeployNoteAt is DeployNote for a run of argv in in that names the real
// reason its line ran in the sandbox, as DeliveryNoteAt does (L1731): kept,
// else the reason Outside gives for its line (cdk run in a repository of
// sandbox.allow_write, where only git and gh run outside); DeployNote's
// "run it alone" only for a line kept inside for its shape.
func DeployNoteAt(h Host, in Input, argv []string, kept *Problem) string {
	note := DeployNote(argv, in.DeployTools)
	if note == "" {
		return ""
	}
	p := kept
	if p == nil && len(argv) >= 3 && argv[1] == "-c" {
		lines := []string{argv[2]}
		if len(argv) > 4 {
			lines = append(lines, argv[4:]...)
		}
		for _, line := range lines {
			if _, q, _ := Outside(h, in, []string{argv[0], "-c", line}); q != nil {
				p = q
				break
			}
		}
	}
	if p == nil {
		return note
	}
	found := deployFound(argv, in.DeployTools)
	names := found[0]
	if len(found) > 1 {
		names = strings.Join(found[:len(found)-1], ", ") + " and " + found[len(found)-1]
	}
	return names + " ran in the sandbox, not outside it: " + p.Line()
}

// linePrograms are the programs of argv, as the notes read it: argv itself,
// and for `<shell> -c <line>` the programs of the line and of the words
// after its $0 (the Claude Code wrap passes the command there), each by
// the reader or, where it cannot judge a line, by its commands' words.
func linePrograms(argv []string) []sh.Program {
	if len(argv) == 0 {
		return nil
	}
	programs := []sh.Program{{Argv: argv}}
	if len(argv) >= 3 && argv[1] == "-c" && slices.Contains(shells, filepath.Base(argv[0])) {
		lines := []string{argv[2]}
		if len(argv) > 4 {
			lines = append(lines, argv[4:]...)
		}
		for _, line := range lines {
			if ps, err := sh.JudgeString(line, nil, nil, sh.Opts{}); err == nil {
				programs = append(programs, ps...)
			} else if cmds, err := sh.Parse(line); err == nil {
				for _, c := range cmds {
					programs = append(programs, sh.Program{Argv: c.Words})
				}
			}
		}
	}
	return programs
}

// deliveryNote is DeliveryNote's text.
const deliveryNote = "git over SSH and gh run outside the sandbox only on a line of their own, with no pipes, $(…) or other commands: run it alone, as in git -C <dir> fetch origin main"

// delivers is whether p is gh, or git with a network subcommand (remote
// only as remote update, after the options beforeSecond lists), by its
// name or a path to it, whatever its options.
func delivers(p sh.Program) bool {
	if len(p.Argv) == 0 {
		return false
	}
	switch filepath.Base(p.Argv[0]) {
	case "gh":
		return true
	case "git":
		args := p.Argv[1:]
		for k := 0; k < len(args); k++ {
			switch {
			case args[k] == "-C" || args[k] == "-c":
				k++
			case !strings.HasPrefix(args[k], "-"):
				if before, ok := beforeSecond[args[k]]; ok {
					rest := args[k+1:]
					j := 0
					for ; j < len(rest); j++ {
						if _, ok := before[rest[j]]; !ok {
							break
						}
					}
					return j < len(rest) && rest[j] == "update"
				}
				_, network := gitOptions[args[k]]
				return network
			}
		}
	}
	return false
}

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
			// the word, or the next one. As pflag reads it, a rest that
			// opens with = drops it: -F=b=@file is -F b=@file (L1318).
			if k := strings.IndexByte(a, 'F'); k > 0 {
				if slices.Contains(ghFileShort, sub) {
					return false
				}
				field := strings.TrimPrefix(a[k+1:], "=")
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
