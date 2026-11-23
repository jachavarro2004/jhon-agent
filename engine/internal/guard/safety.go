package guard

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/grant"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lddswitch"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
	"github.com/jachavarro2004/jhon-agent/engine/internal/workspace"
)

// Ctx is the context a call is judged in (the safety guard's).
type Ctx struct {
	Cwd, Home, Root                                     string
	Projects, Roots, Worktrees, Tmp, Scratch, Deletable []string
	roots                                               map[string]string
	// scratch is the variables the line proves directories mktemp -d makes
	// (scratchVars), by the path each reads as; mktempRm, that the whole
	// line is `rm $(mktemp -d)` (T64c).
	scratch  map[string]string
	mktempRm bool
	// targets, when set, collects each program the walk judges, with the
	// root it acts on (Targets, T140).
	targets *[]Target
}

// State is what a command line changes as it is read: the directory ("" when
// it cannot be told), the search roots of the last find, and the links it made.
// Lost says a cd or a chdir lost the directory: a question asked there names
// a repository the guard cannot tell (UnknownRoot). Each subshell keeps its
// own directory in scoped, by its innermost number (sh.Command.Scope).
type State struct {
	Dir       string
	Lost      bool
	FindPaths []string
	Links     *Links
	scoped    map[int64]dirState
	// written is the files the line, or a script it runs, writes (T34b).
	written map[string]bool
}

// noteWrites records the files text writes, read from the directory st is in.
// The map is made here, before a script's copy of st is taken, so a script
// a shell runs shares it with the rest of the line.
func (st *State) noteWrites(text string) {
	if st.written == nil {
		st.written = map[string]bool{}
	}
	if st.Dir == "" {
		return
	}
	for _, f := range WrittenFiles(text, st.Dir) {
		st.written[f] = true
	}
}

// assignedIn is the variables a line's assignments set, as classify reads them.
func assignedIn(cmds []*sh.Command) map[string]string {
	vars := map[string]string{}
	for _, c := range cmds {
		words := c.Words
		if len(words) > 0 && slices.Contains([]string{"export", "declare", "typeset", "readonly", "local"}, words[0]) {
			words = words[1:]
		} else if slices.ContainsFunc(words, func(w string) bool { return !sh.Assignment.MatchString(w) }) {
			continue
		}
		for _, w := range words {
			if sh.Assignment.MatchString(w) {
				n, v, _ := strings.Cut(w, "=")
				vars[n] = v
			}
		}
	}
	// A value naming another variable (Y=$X) resolves to a fixed point.
	for range 8 {
		changed := false
		for n, v := range vars {
			if e := os.Expand(v, func(k string) string {
				if w, ok := vars[k]; ok && k != n {
					return w
				}
				return "$" + k
			}); e != v {
				vars[n], changed = e, true
			}
		}
		if !changed {
			break
		}
	}
	return vars
}

// wrote is whether the line, or a script it runs, writes the file w names.
func (st *State) wrote(w string, ctx *Ctx, vars map[string]string) bool {
	a := pathOf(w, st.Dir, ctx, vars)
	return a != "" && st.written[a]
}

// dirState is a subshell's directory, and whether a cd lost it.
type dirState struct {
	dir  string
	lost bool
}

// enter moves st into the subshells scope names, from the directory the
// innermost one already has or, on its first program, from the one it was
// started in; leave moves it back out and keeps the subshell's directory.
func (st *State) enter(scope []int64) (leave func()) {
	if len(scope) == 0 {
		return func() {}
	}
	if st.scoped == nil {
		st.scoped = map[int64]dirState{}
	}
	outer := dirState{st.Dir, st.Lost}
	in := outer
	for i := len(scope) - 1; i >= 0; i-- {
		if d, ok := st.scoped[scope[i]]; ok {
			in = d
			break
		}
	}
	st.Dir, st.Lost = in.dir, in.lost
	return func() {
		st.scoped[scope[len(scope)-1]] = dirState{st.Dir, st.Lost}
		st.Dir, st.Lost = outer.dir, outer.lost
	}
}

// rootOf is the project root of a directory, memoized; "" for none.
func (ctx *Ctx) rootOf(d string) string {
	if d == "" {
		return ""
	}
	if r, ok := ctx.roots[d]; ok {
		return r
	}
	r := project.Root(d)
	ctx.roots[d] = r
	return r
}

// ContextOf is the context of a call made in cwd.
func ContextOf(cwd string) *Ctx {
	home := project.Realish(project.Home())
	root := project.Root(cwd)
	// A workspace's primary and joined members are the project too (T57 slice 5).
	roots := workspace.ProjectRoots(cwd)
	var projects []string
	for _, r := range roots {
		if !strings.HasSuffix(r, "-worktrees") {
			projects = append(projects, r)
		}
	}
	var worktrees []string
	if root != "" {
		listed, _ := readGit(root, "worktree", "list", "--porcelain")
		for _, l := range strings.Split(listed, "\n") {
			if !strings.HasPrefix(l, "worktree ") {
				continue
			}
			if w := project.Realish(l[9:]); !slices.Contains(projects, w) && w != "/" && w != home {
				worktrees = append(worktrees, w)
			}
		}
	}
	tmp := project.TmpRoots()
	scratch := append(slices.Clone(tmp), filepath.Join(home, ".claude", "projects"), filepath.Join(home, ".claude", "plans"), filepath.Join(home, ".cache"))
	// Deletion stays inside the temporary roots and ~/.cache; the transcripts
	// and plans under ~/.claude are scratch for writes, never for deletion.
	deletable := append(slices.Clone(tmp), filepath.Join(home, ".cache"))
	return &Ctx{Cwd: project.Realish(cwd), Home: home, Root: root, Projects: projects, Roots: roots, Worktrees: worktrees,
		Tmp: tmp, Scratch: scratch, Deletable: deletable, roots: map[string]string{}}
}

func unreadableAsk(err error) Finding {
	return ask(unreadable, "the command cannot be read safely ("+err.Error()+"); Bash runs a POSIX shell (zsh or bash), never fish: no `(cmd)` substitution or `string`")
}

// Classify is the findings of a command line run in ctx.Cwd.
func Classify(command string, ctx *Ctx) []Finding {
	ctx.scratch, ctx.mktempRm = scratchVars(command, ctx), mktempRm(command)
	st := &State{Dir: ctx.Cwd, Links: &Links{}}
	out := classify(command, ctx, 0, st)
	ctx.scratch, ctx.mktempRm = nil, false
	// A file the write reader finds the line writing (curl -o, go build -o) is
	// refused when it is a credentials file, as judgeWrite's targets are (L1368).
	if !slices.ContainsFunc(out, func(f Finding) bool { return f.Level == "deny" && strings.Contains(f.Why, "which the owner handles") }) {
		for _, f := range slices.Sorted(maps.Keys(st.written)) {
			if CredentialFile(f) {
				out = append(out, deny("a write of "+f, CredentialsWhy(filepath.Base(f))))
			}
		}
	}
	return out
}

// classify reads a command line; depth bounds the re-reading of command strings found in interpreter code.
func classify(command string, ctx *Ctx, depth int, st *State) []Finding {
	cmds, err := sh.Parse(command)
	if err != nil {
		return []Finding{unreadableAsk(err)}
	}
	st.noteWrites(command)
	// A file named by a variable the line assigns is written as well (T34b).
	if pre := assignedIn(cmds); len(pre) > 0 {
		st.noteWrites(os.Expand(command, func(n string) string {
			if v, ok := pre[n]; ok {
				return v
			}
			return "$" + n
		}))
	}
	var out []Finding
	vars := map[string]string{}
	assign := func(w string) {
		n, v, _ := strings.Cut(w, "=")
		vars[n] = v
	}
	for _, c := range cmds {
		if len(c.Words) > 0 && !slices.ContainsFunc(c.Words, func(w string) bool { return !sh.Assignment.MatchString(w) }) {
			for _, w := range c.Words {
				assign(w)
			}
			continue
		}
		if len(c.Words) > 0 && slices.Contains([]string{"export", "declare", "typeset", "readonly", "local"}, c.Words[0]) {
			for _, w := range c.Words[1:] {
				if sh.Assignment.MatchString(w) {
					assign(w)
				}
			}
			continue
		}
		// A command inside literal `for` loops is judged once per value of
		// their variables, as the loop would run it.
		for _, bound := range loopBindings(c.Loops, vars) {
			out = append(out, judgeCommand(c, ctx, bound, st, depth)...)
		}
	}
	return out
}

// judgeProgram judges one program; an ask with no target repository takes
// the one of the directory it runs in.
func judgeProgram(p sh.Program, redirects []*sh.Redirect, ctx *Ctx, vars map[string]string, st *State, depth int) []Finding {
	defer st.enter(p.Scope)()
	pst := st
	if len(p.Chdir) > 0 {
		// env -C: the program runs in that directory; the command's own does not move.
		d := st.Dir
		for _, c := range p.Chdir {
			if d != "" {
				d = pathOf(c, d, ctx, vars)
			}
		}
		cp := *st
		cp.Dir, cp.Lost = d, st.Lost || d == ""
		pst = &cp
	}
	dir, lost := pst.Dir, pst.Lost
	out := judgeOne(p, redirects, ctx, vars, pst, depth)
	for i := range out {
		if out[i].Level == "ask" && !out[i].RootSet {
			out[i].RootSet = true
			out[i].Root = askRoot(dir, ctx)
			if dir == "" && lost {
				out[i].Root = UnknownRoot
			}
		}
	}
	return out
}

func inRepo(d string) bool {
	if d == "" {
		return false
	}
	_, ok := readGit(d, "rev-parse", "--show-toplevel")
	return ok
}

// askRoot is the project whose grant an operation run in dir needs; a
// scratch directory outside any repository runs on behalf of the session's project.
func askRoot(dir string, ctx *Ctx) string {
	if dir != "" && !inRepo(dir) && slices.ContainsFunc(ctx.Scratch, func(s string) bool { return project.Within(project.Realish(dir), s) }) &&
		ctx.Root != "" && inRepo(ctx.Root) {
		return ctx.Root
	}
	return ctx.rootOf(dir)
}

var (
	rmLike      = set("rm", "rmdir", "unlink", "shred", "rimraf", "srm")
	disk        = set("mkfs", "mke2fs", "wipefs", "fdisk", "sfdisk", "parted", "sgdisk", "mkswap", "blkdiscard")
	rootReads   = set("ls", "cat", "head", "tail", "stat", "id", "whoami", "journalctl", "dmesg", "df", "du", "lsblk", "lsof", "ss", "ps", "true", "test")
	escalate    = set("sudo", "doas")
	deviceOK    = set("/dev/null", "/dev/stdout", "/dev/stderr")
	codeDelete  = lazyre.New(`unlink|\brm(?:Sync|dir|dirSync)?\s*\(|rmtree|os\.remove|\bremove\s*\(|FileUtils\.rm|File\.delete`)
	grantScript = lazyre.New(`(?:^|/)src/grant\.ts$`)
	tsRunners   = set("node", "nodejs", "bun", "deno", "tsx", "ts-node")
	// xargsDestroy are the programs that delete, overwrite or re-permission
	// their operands: run by xargs on input the guard cannot see, they are asked.
	xargsDestroy = set("rm", "rmdir", "unlink", "shred", "srm", "rimraf", "mv", "cp", "tee", "ln", "install", "truncate", "dd", "chmod", "chown")
	// findWriters are the programs whose -exec on a find's results deletes or writes them.
	findWriters = func() map[string]bool {
		m := maps.Clone(xargsDestroy)
		m["touch"], m["mkdir"] = true, true
		for k := range sh.Shells {
			m[k] = true
		}
		m["env"], m["xargs"], m["sudo"] = true, true, true
		return m
	}()
	pythonName = lazyre.New(`^python\d?(\.\d+)?$`)
)

// The owner's acts, refused from an agent whatever the program is called.
const (
	grantAct = "a grant is the owner's act; no agent creates or revokes one"
	modeAct  = "a mode is the owner's act; no agent sets one"
	lddAct   = "the LDD switch is the owner's act; no agent turns LDD on or off"
	// T57 W2 (L1299): a sibling's pointer to its workspace's primary.
	workspaceAct = "a workspace pointer is the owner's act; no agent joins or leaves a workspace"
)

// ownerAct is "mode" when words hold the token `mode` followed by a mode,
// "grant" when they hold `grant` followed by a class or --revoke,
// "workspace" when `workspace` is followed by join or leave, and ""
// otherwise; jhon's own options (--root <p>, --for <d>, --json, --list) may
// stand between. It reads arguments, not the program's name: `go run
// ./engine/cmd/jhon mode yolo`, a copied, built or linked binary, and a
// subprocess's literal argv are each the jhon CLI as far as the guard can
// tell. A program that computes its arguments or a state path at run time
// is beyond any reading of its text; the modes' history records every
// change as the owner's (by "owner"), which jhon audit reads.
func ownerAct(words []string) string {
	for i, w := range words {
		if w != "mode" && w != "grant" && w != "ldd" && w != "workspace" {
			continue
		}
	next:
		for j := i + 1; j < len(words); j++ {
			v := words[j]
			switch {
			case v == "--root" || v == "--for":
				j++
			case v == "--json" || (w == "grant" && v == "--list"):
			case w == "mode" && slices.Contains(grant.Modes, v):
				return "mode"
			case w == "ldd" && (v == lddswitch.On || v == lddswitch.Off):
				return "ldd"
			case w == "workspace" && (v == "join" || v == "leave"):
				return "workspace"
			case w == "grant" && (slices.Contains(grant.Classes, v) || v == "--revoke"):
				return "grant"
			default:
				break next
			}
		}
	}
	return ""
}

// ownerActFinding is the refusal of an owner's act named by words, if any.
func ownerActFinding(name string, words []string) []Finding {
	switch ownerAct(words) {
	case "mode":
		return []Finding{deny(name+" mode", modeAct)}
	case "grant":
		return []Finding{deny(name+" grant", grantAct)}
	case "ldd":
		return []Finding{deny(name+" ldd", lddAct)}
	case "workspace":
		return []Finding{deny(name+" workspace", workspaceAct)}
	}
	return nil
}

// modeRead is whether `jhon mode <args>` only reads the mode: no mode named.
func modeRead(args []string) bool {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json", "--help", "-h":
		case "--root":
			i++
		default:
			return false
		}
	}
	return true
}

// workspaceRead is whether `jhon workspace <args>` only reads: `list`
// (--json), or the help; anything else may join or leave.
func workspaceRead(args []string) bool {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		return true
	}
	return len(args) > 0 && args[0] == "list" && (len(args) == 1 || (len(args) == 2 && args[1] == "--json"))
}

// runsGrant is whether a program runs grant.ts: as the program itself, or as a runner's file.
func runsGrant(name string, argv []string) bool {
	return grantScript.MatchString(at(argv, 0)) || (tsRunners[name] && len(argv) > 1 && slices.ContainsFunc(argv[1:], grantScript.MatchString))
}

func hasInPlace(args []string) bool { return slices.ContainsFunc(args, sh.InPlace) }

func judgeOne(p sh.Program, redirects []*sh.Redirect, ctx *Ctx, vars map[string]string, st *State, depth int) []Finding {
	// `builtin x` runs the shell's own x: it is judged as x.
	if (p.Name == "builtin" || (p.Name == "" && sh.Base(at(p.Argv, 0)) == "builtin")) && len(p.Argv) > 1 {
		q := p
		q.Argv, q.Name, q.Via = p.Argv[1:], sh.Base(p.Argv[1]), append(slices.Clone(p.Via), "builtin")
		return judgeOne(q, redirects, ctx, vars, st, depth)
	}
	var out []Finding
	dir := st.Dir
	pathAt := func(w string) string { return pathOf(w, dir, ctx, vars) }
	env := maps.Clone(vars)
	maps.Copy(env, p.Envv)

	for _, r := range redirects {
		if !strings.Contains(r.Op, ">") || (strings.HasSuffix(r.Op, "&") && descriptor.MatchString(r.Target)) {
			continue // a descriptor, not a file
		}
		out = append(out, judgeWrite(r.Target, r.Op+" "+r.Target, ctx, vars, st)...)
	}

	argv := p.Argv
	name := p.Name
	if name == "" {
		name = sh.Base(at(argv, 0))
	}
	var args []string
	for _, a := range argv[min(1, len(argv)):] {
		if a != "<xargs input>" {
			args = append(args, a)
		}
	}
	if ctx.targets != nil {
		q := p
		q.Name = name
		*ctx.targets = append(*ctx.targets, Target{Program: q, Root: targetRoot(name, args, st, ctx, vars, env)})
	}
	via := make([]string, len(p.Via))
	for i, v := range p.Via {
		via[i] = sh.Base(v)
	}
	if i := slices.IndexFunc(via, func(v string) bool { return escalate[v] }); i >= 0 && !rootReads[name] &&
		!(name == "systemctl" && slices.Contains([]string{"status", "is-active", "show", "list-units"}, at(args, 0))) {
		out = append(out, deny(via[i]+" "+name, "it runs a program as root, which may write anywhere"))
	}
	if name == "su" || name == "pkexec" || name == "run0" {
		out = append(out, deny(name, "it runs a program as another user"))
	}
	// `mode <a mode>` and `grant <a class>` are the owner's, by any program.
	// A program the guard cannot see (read from stdin, a preload, a
	// variable) is judged by its visible words alone (L1311).
	out = append(out, ownerActFinding(name, args)...)
	// xargs deleting or overwriting operands the guard cannot see (`find … |
	// xargs rm -rf`) is the destructive core: asked, as in v0.3.7.
	if slices.Contains(via, sh.XargsUnseen) && (xargsDestroy[name] || ((name == "sed" || name == "perl") && hasInPlace(args))) {
		out = append(out, askAt(unknown, "xargs "+name+": its operands come from input the guard cannot see", ""))
	}
	// A program eval, a -c string, or a sourced script runs (its Script) runs its script file as well.
	if p.Code == nil {
		out = append(out, judgeScriptFile(name, argv, ctx, vars, st, depth, p)...)
	}
	if p.Code != nil {
		out = append(out, judgeCode(p, name, ctx, vars, dir, st.Lost, depth)...)
		if (name == "perl" || name == "ruby") && hasInPlace(args) {
			pieces := strings.Split(*p.Code, "\n")
			for _, f := range operands(args, nil, "eEIMm") {
				if !slices.Contains(pieces, f) {
					out = append(out, judgeWrite(f, name+" -i "+f, ctx, vars, st)...)
				}
			}
		}
		return out
	}
	// The jhon CLI, by any spelling: grant is the owner's; publish is outward.
	var jhonArgs []string
	isJhon := false
	if name == "jhon" {
		jhonArgs, isJhon = args, true
	} else if sh.Shells[name] {
		if s := slices.IndexFunc(args, func(a string) bool { return !strings.HasPrefix(a, "-") }); s >= 0 && sh.Base(args[s]) == "jhon" {
			jhonArgs, isJhon = args[s+1:], true
		}
	}
	jsub := ""
	if isJhon {
		jsub = at(jhonArgs, 0)
	}
	grantHelp := jsub == "grant" && len(jhonArgs) == 2 && (jhonArgs[1] == "--help" || jhonArgs[1] == "-h")
	// Listing the grants changes none: an agent reads what the owner gave.
	grantList := jsub == "grant" && len(jhonArgs) >= 2 && jhonArgs[1] == "--list" &&
		(len(jhonArgs) == 2 || (len(jhonArgs) == 4 && jhonArgs[2] == "--root"))
	if (jsub == "grant" && !grantHelp && !grantList) || runsGrant(name, argv) {
		out = append(out, deny("jhon grant", grantAct))
	}
	// Reading the mode changes none; setting one is the owner's.
	if jsub == "mode" && !modeRead(jhonArgs[1:]) {
		out = append(out, deny("jhon mode", modeAct))
	}
	// So with the LDD switch: reading it changes nothing.
	if jsub == "ldd" && !modeRead(jhonArgs[1:]) {
		out = append(out, deny("jhon ldd", lddAct))
	}
	// So with a workspace pointer: listing the pointers changes none.
	if jsub == "workspace" && !workspaceRead(jhonArgs[1:]) {
		out = append(out, deny("jhon workspace", workspaceAct))
	}
	if jsub == "publish" {
		out = append(out, ask("publish", "jhon publish"))
	}
	route := append(slices.Clone(p.Via), name)
	// jhon mutate runs its test command, after `--`, as argv: judge it as one.
	if jsub == "mutate" {
		if dd := slices.Index(jhonArgs, "--"); dd >= 0 && dd+1 < len(jhonArgs) {
			out = append(out, judgeProgramWords(jhonArgs[dd+1:], redirects, ctx, vars, st, depth, route, p.Envv)...)
		}
	}

	// Programs that run the program they name.
	sub := func(words []string) []Finding {
		return judgeProgramWords(words, redirects, ctx, vars, st, depth, route, p.Envv)
	}
	if name == "rtk" {
		rest := args
		if at(args, 0) == "proxy" {
			rest = args[1:]
		}
		if len(rest) > 0 {
			out = append(out, sub(rest)...)
		}
		return out
	}
	if paneMultiplexers[name] {
		script, words := paneRuns(args)
		if script != "" {
			own := *st
			own.Links = st.Links.clone()
			out = append(out, judgeScript(script, ctx, vars, &own, depth, route, p.Envv)...)
		}
		if words != nil {
			out = append(out, sub(words)...)
		}
		return out
	}
	if words, script, isScript, ok := runnerTarget(name, args); ok {
		if isScript {
			return append(out, judgeScript(script, ctx, vars, st, depth, route, p.Envv)...)
		}
		return append(out, sub(words)...)
	}
	if pythonName.MatchString(name) && at(args, 0) == "-m" && at(args, 1) != "" {
		return append(out, sub(args[1:])...)
	}
	if name == "cd" || name == "pushd" {
		// Where a delivery runs (Targets): a cd a child process runs (a
		// shell's -c string, a wrapper) moves that process, not the line,
		// and the walk keeps no scope of its own for it: the directory is
		// lost, so what follows is placed nowhere (fail closed).
		if ctx.targets != nil && slices.ContainsFunc(via, func(v string) bool { return v != "eval" && v != "command" && v != "builtin" }) {
			st.Dir, st.Lost = "", true
			return out
		}
		i := slices.IndexFunc(args, func(a string) bool { return !strings.HasPrefix(a, "-") || a == "-" })
		switch {
		case i < 0:
			st.Dir = ctx.Home
		case args[i] == "-":
			st.Dir = ""
		default:
			// A link this command created leads where it points.
			if st.Dir = pathAt(args[i]); st.Dir != "" {
				st.Dir = followLinks(st.Dir, st)
			}
		}
		// A directory a cd lost is no repository the guard can tell.
		st.Lost = st.Dir == ""
		return out
	}

	// A command run inside a container or a pod: its destructive operations
	// are refused; its writes land in the container, not here.
	inner := func(words []string) []Finding {
		cst := &State{Links: &Links{}}
		var kept []Finding
		for _, f := range judgeProgramWords(words, nil, ctx, map[string]string{}, cst, depth, route, map[string]string{}) {
			if !f.Write {
				kept = append(kept, f)
			}
		}
		return kept
	}

	if rmLike[name] || name == "find" {
		out = append(out, judgeDelete(name, args, p, ctx, vars, st)...)
	}
	switch {
	case name == "git":
		out = append(out, judgeGit(args, dir, ctx, vars, env, 0)...)
	case name == "gh":
		out = append(out, judgeGh(args)...)
	case slices.Contains([]string{"docker", "podman", "docker-compose", "podman-compose"}, name):
		out = append(out, judgeDocker(name, args, env, inner)...)
	case (name == "kubectl" || name == "oc") && at(operands(args, kubeValued, "ncs"), 0) == "exec":
		var cmd []string
		if dd := slices.Index(args, "--"); dd >= 0 {
			cmd = args[dd+1:]
		} else if ops := operands(args, kubeValued, "ncs"); len(ops) > 2 {
			cmd = ops[2:]
		}
		if len(cmd) > 0 {
			out = append(out, inner(cmd)...)
		}
	default:
		out = append(out, judgeOutward(name, args, env)...)
	}

	if (name == "chmod" || name == "chown" || name == "chgrp") && (hasFlag(shortFlags(args, ""), "R") || slices.Contains(longs(args), "--recursive")) {
		ops := operands(args, nil, "")
		if !slices.Contains(longs(args), "--reference") && len(ops) > 0 {
			ops = ops[1:]
		}
		for _, o := range ops {
			why := "its target cannot be resolved"
			if a := pathAt(o); a != "" {
				why = writeWhy(a, ctx)
			}
			if why != "" {
				out = append(out, deny(name+" -R "+o, why))
			}
		}
	}
	if name == "dd" {
		if i := slices.IndexFunc(args, func(a string) bool { return strings.HasPrefix(a, "of=") }); i >= 0 {
			of := args[i][3:]
			if a := pathAt(of); a == "" {
				out = append(out, deny("dd of="+of, "its target cannot be resolved"))
			} else if t := followLinks(a, st); strings.HasPrefix(t, "/dev/") {
				if !deviceOK[t] {
					out = append(out, deny("dd of="+of, "it writes a device"))
				}
			} else {
				why := protectedWhy(t, ctx)
				if why == "" {
					why = credentialWhy(of, t)
				}
				if why == "" {
					why = writeWhy(t, ctx)
				}
				if why != "" {
					out = append(out, deny("dd of="+of, why))
				}
			}
		}
	}
	if disk[name] || strings.HasPrefix(name, "mkfs.") {
		out = append(out, deny(name, "it overwrites a disk or a filesystem"))
	}

	// Shell writes: a device or a protected path is refused (judgeWrite).
	// A link is judged where it is created, and only later writes go through it.
	targets, links := writeTargets(name, args, ctx, vars, st)
	for _, t := range targets {
		out = append(out, judgeWrite(t.word, t.label, ctx, vars, st)...)
	}
	for _, l := range links {
		st.Links.set(l.written, l.source)
	}
	return out
}

var (
	descriptor  = lazyre.New(`^\d+-?$|^-$`)
	sedScript   = lazyre.New(`^-[a-zA-Z]*[ef]$`)
	sedScriptLn = lazyre.New(`^--(?:expression|file)(?:=|$)`)
	rsyncHost   = lazyre.New(`^[^/]*:`)
)

type target struct{ word, label string }

func labelled(ws []string, prefix string) []target {
	var out []target
	for _, w := range ws {
		out = append(out, target{w, prefix + w})
	}
	return out
}

// writeTargets is the words a writing program writes, each with its label,
// and the links ln creates (link, target; absolute).
func writeTargets(name string, args []string, ctx *Ctx, vars map[string]string, st *State) ([]target, []pair) {
	ws := func(valued []string, letters string) []string { return operands(args, set(valued...), letters) }
	switch name {
	case "tee":
		return labelled(ws([]string{"--output-error"}, ""), "tee "), nil
	case "touch":
		return labelled(ws([]string{"-r", "-d", "-t", "--reference", "--date"}, "rdt"), "touch "), nil
	case "mkdir":
		return labelled(ws([]string{"-m", "--mode", "--context"}, "m"), "mkdir "), nil
	case "truncate":
		return labelled(ws([]string{"-s", "--size", "-r", "--reference"}, "sr"), "truncate "), nil
	case "sed":
		if !hasInPlace(args) {
			return nil, nil
		}
		script := slices.ContainsFunc(args, func(a string) bool { return sedScript.MatchString(a) || sedScriptLn.MatchString(a) })
		ops := ws([]string{"-e", "--expression", "-f", "--file", "-l", "--line-length"}, "efl")
		if !script && len(ops) > 0 {
			ops = ops[1:]
		}
		return labelled(ops, "sed -i "), nil
	case "perl", "ruby":
		if !hasInPlace(args) {
			return nil, nil
		}
		ops := ws(nil, "IMm")
		if len(ops) > 0 {
			ops = ops[1:]
		}
		return labelled(ops, name+" -i "), nil
	case "rsync":
		ops := ws([]string{"-e", "--rsh", "--exclude", "--include", "--filter", "--files-from", "--chmod", "--chown", "--rsync-path", "--log-file", "--partial-dir", "--temp-dir", "--backup-dir", "--suffix"}, "ef")
		if len(ops) >= 2 && !rsyncHost.MatchString(ops[len(ops)-1]) {
			dest := ops[len(ops)-1]
			return []target{{dest, "rsync " + dest}}, nil
		}
		return nil, nil
	case "cp", "mv", "ln", "install":
		writes, lns, sources := destinations(name, args, st, ctx, vars)
		var links []pair
		if name == "ln" {
			sym := hasFlag(shortFlags(args, "St"), "s") || slices.Contains(longs(args), "--symbolic")
			for _, lt := range lns {
				l := pathOf(lt.written, st.Dir, ctx, vars)
				if l == "" {
					continue
				}
				t := ""
				if sym && !strings.HasPrefix(lt.source, "/") && !strings.HasPrefix(lt.source, "~") {
					t = project.Resolve(filepath.Dir(l), lt.source)
				} else {
					t = pathOf(lt.source, st.Dir, ctx, vars)
				}
				if t != "" {
					links = append(links, pair{followLinks(l, st), followLinks(t, st)})
				}
			}
		}
		return append(labelled(writes, name+" "), labelled(sources, "mv (removes) ")...), links
	}
	return nil, nil
}

// judgeProgramWords judges a word list as a program of its own.
func judgeProgramWords(words []string, redirects []*sh.Redirect, ctx *Ctx, vars map[string]string, st *State, depth int, via []string, envv map[string]string) []Finding {
	found, err := sh.Programs(words, via, nil, sh.Opts{Envv: envv})
	if err != nil {
		return []Finding{unreadableAsk(err)}
	}
	var out []Finding
	for _, q := range found {
		r := q.Redirects
		if r == nil {
			r = redirects
		}
		out = append(out, judgeProgram(q, r, ctx, vars, st, depth)...)
	}
	return out
}

// judgeScript judges a shell string a program runs.
func judgeScript(s string, ctx *Ctx, vars map[string]string, st *State, depth int, via []string, envv map[string]string) []Finding {
	found, err := sh.JudgeString(s, via, nil, sh.Opts{Envv: envv})
	if err != nil {
		return []Finding{unreadableAsk(err)}
	}
	st.noteWrites(s)
	var out []Finding
	for _, q := range found {
		out = append(out, judgeProgram(q, q.Redirects, ctx, vars, st, depth)...)
	}
	return out
}

var (
	findOperandEnd = lazyre.New(`^[-(!]`)
	findVia        = lazyre.New(`^find -`)
	gitNames       = []string{".git", "config", "HEAD", "index", "description", "packed-refs", "ORIG_HEAD", "FETCH_HEAD", "MERGE_HEAD", "REBASE_HEAD", "AUTO_MERGE",
		"COMMIT_EDITMSG", "exclude", "pre-commit", "pre-commit.sample", "objects", "refs", "hooks", "info", "logs", "heads", "tags", "remotes", "main", "master",
		"origin", "pack", "pack-0a1b2c3d.pack", "pack-0a1b2c3d.idx", "pack-0a1b2c3d.rev", "0a", "1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d", "index.lock",
		"shallow", "modules", "worktrees", "gitdir", "commondir", "sharedindex.0a1b2c3d", "attributes", "alternates"}
	gitSubpaths = []string{".git", ".git/config", ".git/HEAD", ".git/index", ".git/hooks/pre-commit", ".git/objects/0a/1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d",
		".git/refs/heads/main", ".git/info/exclude", ".git/logs/HEAD", ".git/packed-refs"}
	findTests = []string{"-name", "-iname", "-path", "-ipath", "-wholename", "-iwholename"}
	findRegex = lazyre.New(`^-i?regex$`)
)

func findGlob(p string, ci bool) *regexp.Regexp { return globRegex(p, ".*", true, ci) }

// findCanMatchVcs is whether a find expression can select an entry inside a
// VCS directory under the search root as written.
func findCanMatchVcs(expr []string, root string) bool {
	r := strings.TrimRight(root, "/")
	if r == "" {
		r = "/"
	}
	paths := make([]string, len(gitSubpaths))
	for i, s := range gitSubpaths {
		if r == "/" {
			paths[i] = "/" + s
		} else {
			paths[i] = r + "/" + s
		}
	}
	byName := func(op string) bool { return strings.HasSuffix(op, "name") }
	test := func(op, pat string) bool {
		re := findGlob(pat, strings.HasPrefix(op, "-i"))
		if byName(op) {
			return slices.ContainsFunc(gitNames, re.MatchString)
		}
		return slices.ContainsFunc(paths, re.MatchString)
	}
	for k := 0; k < len(expr); k++ {
		if !slices.Contains(findTests, expr[k]) {
			continue
		}
		pat := at(expr, k+1)
		neg := at(expr, k-1) == "!" || at(expr, k-1) == "-not"
		ci := strings.HasPrefix(expr[k], "-i")
		vcs := false
		if byName(expr[k]) {
			vcs = findGlob(pat, true).MatchString(".git")
		} else {
			vcs = slices.ContainsFunc(paths[:2], findGlob(pat, ci).MatchString)
		}
		if vcs && slices.Contains(expr[min(k+2, len(expr)):min(k+4, len(expr))], "-prune") {
			return false
		}
		if neg && !slices.ContainsFunc(paths[1:], func(p string) bool { return !findGlob(pat, ci).MatchString(p) }) {
			return false
		}
	}
	branches := [][]string{{}}
	for _, a := range expr {
		if a == "-o" || a == "-or" {
			branches = append(branches, []string{})
		} else {
			branches[len(branches)-1] = append(branches[len(branches)-1], a)
		}
	}
	return slices.ContainsFunc(branches, func(b []string) bool {
		if slices.ContainsFunc(b, findRegex.MatchString) {
			return true
		}
		for k := 0; k < len(b); k++ {
			if slices.Contains(findTests, b[k]) && at(b, k-1) != "!" && at(b, k-1) != "-not" && !test(b[k], at(b, k+1)) {
				return false
			}
		}
		return true
	})
}

// holdsVcs is whether a search root holds a VCS directory: it has one, or it holds the project.
func holdsVcs(abs string, ctx *Ctx) bool {
	return slices.ContainsFunc(VCSDirs, func(d string) bool { return project.Exists(filepath.Join(abs, d)) }) ||
		slices.ContainsFunc(ctx.Projects, func(r string) bool { return project.Within(r, abs) })
}

func judgeDelete(name string, args []string, p sh.Program, ctx *Ctx, vars map[string]string, st *State) []Finding {
	var out []Finding
	dir := st.Dir
	if name == "find" {
		k := 0
		var paths []string
		for k < len(args) && !findOperandEnd.MatchString(args[k]) {
			paths = append(paths, args[k])
			k++
		}
		if len(paths) == 0 {
			paths = []string{"."}
		}
		st.FindPaths = paths
		// The expression without its exec bodies, and the programs those run.
		var expr []string
		var execs [][]string
		for j := k; j < len(args); j++ {
			if !sh.FindExec[args[j]] {
				expr = append(expr, args[j])
				continue
			}
			e := j + 1
			for e < len(args) && args[e] != ";" && args[e] != "+" {
				e++
			}
			execs = append(execs, args[j+1:e])
			j = e
		}
		how := ""
		if slices.Contains(expr, "-delete") {
			how = "-delete"
		} else if i := slices.IndexFunc(execs, func(x []string) bool {
			n := sh.Base(at(x, 0))
			return findWriters[n] || ((n == "sed" || n == "perl") && len(x) > 0 && hasInPlace(x[1:]))
		}); i >= 0 {
			how = "-exec " + sh.Base(at(execs[i], 0))
		}
		if how != "" {
			for _, w := range paths {
				a := pathOf(w, dir, ctx, vars)
				if a == "" {
					out = append(out, deny("find "+w+" "+how, "its search root cannot be resolved"))
					continue
				}
				if why := outsideWhy(filepath.Join(a, child), ctx, false); why != "" {
					out = append(out, deleteFinding("find "+w+" "+how, why, a, ctx))
					continue
				}
				if holdsVcs(a, ctx) && findCanMatchVcs(expr, w) {
					out = append(out, deny("find "+w+" "+how, "it searches a tree that holds a VCS directory, and its expression can select entries inside it"))
				}
			}
		}
		return out
	}
	flags := shortFlags(args, "")
	recursive := name == "rimraf" || hasFlag(flags, "rR") || slices.Contains(longs(args), "--recursive")
	fromFind := slices.ContainsFunc(p.Via, findVia.MatchString)
	r := ""
	if recursive {
		r = " -r"
	}
	for _, t := range operands(args, nil, "") {
		if t == "<xargs input>" {
			continue
		}
		type entry struct {
			w     string
			child bool
		}
		ws := []entry{{t, false}}
		if t == "{}" && fromFind {
			ws = nil
			fp := st.FindPaths
			if fp == nil {
				fp = []string{"."}
			}
			for _, f := range fp {
				ws = append(ws, entry{f, true})
			}
		}
		for _, e := range ws {
			a := pathOf(e.w, dir, ctx, vars)
			if e.w == "$(…)" && ctx.mktempRm && name == "rm" && len(p.Via) == 0 {
				a, _ = mktempDir("-d", ctx)
			}
			if a != "" && e.child {
				a = filepath.Join(a, child)
			}
			if a != "" {
				a = followLinks(a, st)
			}
			if why := credentialWhy(e.w, a); why != "" {
				out = append(out, deny(name+r+" "+t, why))
				continue
			}
			if a == "" && !recursive {
				out = append(out, writeAsk(unknown, name+" "+t+": its target cannot be resolved"))
				continue
			}
			why := "its target cannot be resolved"
			if a != "" {
				why = outsideWhy(a, ctx, false)
			}
			if why != "" && a != "" {
				out = append(out, deleteFinding(name+r+" "+t, why, a, ctx))
			} else if why != "" {
				out = append(out, deny(name+r+" "+t, why))
			}
		}
	}
	return out
}

var (
	// multilineString is a string literal that may span lines: text the code
	// writes or prints, not a path it targets, when it does.
	multilineString = lazyre.New("'''[\\s\\S]*?'''|\"\"\"[\\s\\S]*?\"\"\"|`(?:[^`\\\\]|\\\\.)*`")
	codePathLike    = lazyre.New(`[^\s'"` + "`" + `(),;=<>{}[\]]+`)
	pathLead        = lazyre.New(`^(?:/|~|\.\.?(?:/|$))`)
	// jsModuleSpec and pyModuleSpec are modules the code imports, not files
	// it deletes (T210, L1657). Each is one statement on one line, its
	// specifier one quoted string: an `import … from` or `export … from` (or
	// the `} from` closing a multi-line one), a bare `import '…'`, and an
	// `import('…')` or `require('…')` that opens a statement or is assigned
	// or awaited, never a call's argument; in Python, the module of a
	// `from … import`.
	jsModuleSpec = lazyre.New(`(?m:^[ \t]*(?:(?:import|export)\b[^'"\n;]*?|\}[ \t]*)\bfrom[ \t]*(?:'[^'"\n]*'|"[^'"\n]*"))|` +
		`(?m:^[ \t]*import[ \t]*(?:'[^'"\n]*'|"[^'"\n]*"))|` +
		`(?m:(?:^|[=:]|\bawait)[ \t]*(?:import|require)\([ \t]*(?:'[^'"\n]*'|"[^'"\n]*")[ \t]*\))`)
	pyModuleSpec = lazyre.New(`(?m:^[ \t]*from[ \t]+[\w.]+[ \t]+import\b)`)
	// codeGlob is code that expands a pattern into file names: only there is
	// a `.*` a path that can name `.git`.
	codeGlob = lazyre.New(`\b(?:i?glob|rglob|fnmatch|globSync|fast-glob|wildcard)\b|\bglob\(`)
)

func withoutTextBlocks(code string) string {
	return multilineString.ReplaceAllStringFunc(code, func(m string) string {
		if strings.Contains(m, "\n") {
			return "''"
		}
		return m
	})
}

// withoutModules is code without the modules it imports, read in the
// language of interp, the program that runs it: a JS runtime's or Python's.
func withoutModules(code, interp string) string {
	switch {
	case tsRunners[interp]:
		return jsModuleSpec.ReplaceAllString(code, " ")
	case pythonName.MatchString(interp):
		return pyModuleSpec.ReplaceAllString(code, " ")
	}
	return code
}

// judgeCode is the findings of code that interp, the program, runs.
func judgeCode(p sh.Program, interp string, ctx *Ctx, vars map[string]string, dir string, lost bool, depth int) []Finding {
	var out []Finding
	code := ""
	if p.Code != nil {
		code = *p.Code
	}
	targets := withoutTextBlocks(code)
	globs := codeGlob.MatchString(targets)
	isProtected := func(t string) bool {
		a := pathOf(t, dir, ctx, vars)
		return a != "" && (vcsSegment(a, globs) != "" || grant.InGrants(a) || project.IsClaudeSettings(a))
	}
	if sh.CodeWrites(targets, isProtected) {
		out = append(out, deny(p.Name+" code", "it writes or deletes a VCS directory, the grants directory, or a Claude Code settings file"))
	}
	if codeDelete.MatchString(targets) {
		for _, t := range codePathLike.FindAllString(withoutModules(targets, interp), -1) {
			if !pathLead.MatchString(t) {
				continue
			}
			if a := pathOf(t, dir, ctx, vars); a != "" {
				if why := outsideWhy(a, ctx, false); why != "" {
					out = append(out, deny(p.Name+" code deleting "+t, why))
					break
				}
			}
		}
	}
	if depth < 2 && sh.CodeExec.MatchString(code) {
		strs := sh.CodeStrings(code)
		if len(strs) > 40 {
			strs = strs[:40]
		}
		seen := map[string]bool{}
		inner := *ctx
		inner.Cwd = dir
		for i := range strs {
			for _, candidate := range []string{strs[i], strings.Join(strs[i:], " ")} {
				if strings.TrimSpace(candidate) == "" || seen[candidate] {
					continue
				}
				seen[candidate] = true
				for _, f := range classify(candidate, &inner, depth+1, &State{Dir: dir, Lost: lost, Links: &Links{}}) {
					if f.Level == "ask" && (f.Cls == unreadable || f.Write) {
						continue // a literal that is not a command
					}
					if f.Level == "deny" {
						g := deny(p.Name+" code running "+f.What, f.Why)
						g.Cls = f.Cls
						f = g
					}
					out = append(out, f)
				}
			}
		}
	}
	return out
}

// ── script files ────────────────────────────────────────────────────────────

const scriptMaxBytes = 512 * 1024

var (
	shebang     = lazyre.New(`^#!\s*(\S+)(?:[ \t]+(\S+))?`)
	shebangLine = lazyre.New(`^#!.*\n`)
)

// shebangProgram is the interpreter a shebang line names, or "".
func shebangProgram(text string) string {
	m := shebang.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	prog := sh.Base(m[1])
	if prog != "env" {
		return prog
	}
	if m[2] != "" && !strings.HasPrefix(m[2], "-") {
		return sh.Base(m[2])
	}
	return ""
}

// scriptText is the text of the script file w names, and its path; ok is false when it cannot be read.
func scriptText(w string, st *State, ctx *Ctx, vars map[string]string) (string, string, bool) {
	a := pathOf(w, st.Dir, ctx, vars)
	if a == "" {
		return "", "", false
	}
	info, err := os.Stat(a)
	if err != nil || !info.Mode().IsRegular() || info.Size() > scriptMaxBytes {
		return "", "", false
	}
	b, err := os.ReadFile(a)
	return string(b), a, err == nil
}

// judgeScriptFile is the findings of the script file a program runs, labelled with the file.
func judgeScriptFile(name string, argv []string, ctx *Ctx, vars map[string]string, st *State, depth int, p sh.Program) []Finding {
	sourced := name == "source" || name == "."
	if depth >= 2 && !sourced {
		return nil
	}
	// A script the guard cannot read (one the line writes first, a
	// substitution, a descriptor, a parameter it cannot resolve, one sourced
	// past the bound) is judged by the line's visible words alone (L1311).
	kind, word, text, path, ok, interp := "", "", "", "", false, name
	if sourced {
		// A sourced script runs in the shell itself, as `bash <script>` would
		// run it; the scripts it sources are judged in turn, up to a bound.
		if word = positional(at(nonOptions(argv[min(1, len(argv)):]), 0), p.Envv); word != "" && depth < maxSourceDepth {
			text, path, ok = scriptText(word, st, ctx, vars)
		}
		kind = "shell"
	} else if sh.Shells[name] || sh.CodeInterpreter.MatchString(name) || tsRunners[name] {
		ops := scriptOperands(name, argv[min(1, len(argv)):])
		hasWord := len(ops) > 0
		word = at(ops, 0)
		if (name == "deno" || name == "bun") && at(ops, 0) == "run" {
			word, hasWord = at(ops, 1), len(ops) > 1
		}
		word = positional(word, p.Envv)
		if hasWord {
			text, path, ok = scriptText(word, st, ctx, vars)
		}
		kind = "code"
		if sh.Shells[name] {
			kind = "shell"
		}
	} else if strings.Contains(at(argv, 0), "/") {
		word = argv[0]
		text, path, ok = scriptText(word, st, ctx, vars)
		if prog := shebangProgram(text); ok && prog != "" {
			if sh.Shells[prog] {
				kind = "shell"
			} else if sh.CodeInterpreter.MatchString(prog) || tsRunners[prog] {
				kind, interp = "code", prog
			}
		}
	}
	if !ok || kind == "" || st.wrote(word, ctx, vars) {
		return nil
	}
	label := name + " " + word
	via := append(slices.Clone(p.Via), name)
	body := shebangLine.ReplaceAllString(text, "")
	var found []Finding
	if kind == "shell" && sourced {
		// Its cd and links outlive it: the rest of the line runs where it left.
		found = judgeScript(body, ctx, vars, st, depth+1, via, p.Envv)
	} else if kind == "shell" {
		own := *st
		own.Links = st.Links.clone()
		// A script a shell runs has positional parameters of its own.
		found = judgeScript(body, ctx, map[string]string{}, &own, depth+1, via, sh.Positional(p.Envv, nil))
	} else {
		q := p
		q.Name, q.Code = label, &body
		found = judgeCode(q, interp, ctx, vars, st.Dir, st.Lost, depth+1)
	}
	// A question the script asks where it was started is its own
	// repository's when it lives in another one: a session lends another
	// project's script nothing (L185). Started in another project, it stays
	// that project's as well, so the session's own script lends that
	// project nothing either (L187).
	here, mine := askRoot(st.Dir, ctx), ctx.rootOf(filepath.Dir(path))
	if st.Dir == "" && st.Lost {
		here = UnknownRoot
	}
	var out []Finding
	for _, f := range found {
		if f.Level == "ask" && f.Cls == unreadable {
			continue
		}
		// A refusal or a question names the script it found the effect in.
		if !strings.HasPrefix(f.What, label) {
			f.What = label + ": " + f.What
		}
		if f.Level == "ask" && here != UnknownRoot && mine != "" && mine != here && (!f.RootSet || f.Root == here) {
			if here != "" && here != ctx.Root {
				f.RootSet, f.Root = true, here
				out = append(out, f)
			}
			f.RootSet, f.Root = true, mine
		}
		out = append(out, f)
	}
	return out
}

// maxSourceDepth bounds how deep scripts sourced by sourced scripts are read.
const maxSourceDepth = 6

// positionalRef is a reference to a shell positional parameter: $1, ${1}.
var positionalRef = lazyre.New(`\$(\d)|\$\{(\d+)\}`)

// positional is w with the positional parameters envv binds put in.
func positional(w string, envv map[string]string) string {
	return positionalRef.ReplaceAllStringFunc(w, func(m string) string {
		g := positionalRef.FindStringSubmatch(m)
		if v, ok := envv[g[1]+g[2]]; ok {
			return v
		}
		return m
	})
}

var (
	preloadRunners = set("node", "nodejs", "bun", "bunx", "tsx", "deno")
	// nodeValueOptions are node's, bun's and tsx's options whose value may be
	// the next word, which is not the script (T34b).
	nodeValueOptions = set("-C", "--conditions", "--title", "-r", "--require", "--import", "--loader", "--experimental-loader",
		"--env-file", "--env-file-if-exists", "--input-type", "--inspect-port", "--redirect-warnings", "--report-dir",
		"--diagnostic-dir", "--icu-data-dir", "--openssl-config", "--unhandled-rejections", "--watch-path",
		"--cwd", "--preload", "--config", "--tsconfig-override", "--tsconfig")
)

// scriptOperands is the words of an interpreter's arguments that are not
// options; for node and its kin, an option's separate value is skipped.
func scriptOperands(name string, args []string) []string {
	if !preloadRunners[name] {
		return nonOptions(args)
	}
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if nodeValueOptions[a] {
				i++
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

// maxLoopRuns bounds how many times a command inside literal loops is judged.
const maxLoopRuns = 64

// loopBindings are vars with each combination of the loops' values bound,
// or vars alone for a command in no literal loop or in too many runs of one.
func loopBindings(loops []sh.Loop, vars map[string]string) []map[string]string {
	runs := []map[string]string{vars}
	for _, l := range loops {
		if len(runs)*len(l.Values) > maxLoopRuns {
			return []map[string]string{vars}
		}
		var next []map[string]string
		for _, r := range runs {
			for _, v := range l.Values {
				b := maps.Clone(r)
				b[l.Var] = v
				next = append(next, b)
			}
		}
		runs = next
	}
	return runs
}

// judgeCommand is the findings of one simple command under vars.
func judgeCommand(c *sh.Command, ctx *Ctx, vars map[string]string, st *State, depth int) []Finding {
	words := make([]string, len(c.Words))
	// The line's own mktemp -d directory, named whole by an rm or rmdir, reads as the path it makes (scratchVars).
	scratchRm := depth == 0 && len(c.Scope) == 0 && len(c.Words) > 0 && (c.Words[0] == "rm" || c.Words[0] == "rmdir")
	for i, w := range c.Words {
		if m := scratchWord.FindStringSubmatch(w); scratchRm && i > 0 && m != nil && ctx.scratch[m[1]+m[2]] != "" {
			words[i] = ctx.scratch[m[1]+m[2]]
			continue
		}
		words[i] = varRef.ReplaceAllStringFunc(w, func(m string) string {
			name := varRef.FindStringSubmatch(m)[1]
			// An IFS expansion with an operation (${IFS%?}) stays as written: the reader finds it unreadable (L784).
			if name == "IFS" && strings.HasPrefix(m, "${") && !strings.HasSuffix(m, "}") {
				return m
			}
			if v, ok := vars[name]; ok {
				return v
			}
			return m
		})
	}
	cc := *c
	cc.Words = words
	found, err := sh.CommandPrograms(&cc, nil, nil, sh.Opts{})
	if err != nil {
		return []Finding{unreadableAsk(err)}
	}
	var out []Finding
	for _, p := range found {
		redirects := p.Redirects
		if redirects == nil {
			redirects = c.Redirects
		}
		out = append(out, judgeProgram(p, redirects, ctx, vars, st, depth)...)
	}
	return out
}
