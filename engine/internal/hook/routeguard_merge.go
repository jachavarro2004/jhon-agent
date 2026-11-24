package hook

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

// mergeGate is why command, run from cwd in a session rooted at top, may
// not run, "" when it may (T219): it moves the default branch of a
// repository under LDD (ldd.DefaultBranches: main, master, origin/HEAD's)
// to a commit no ready `jhon deliver check <change> --action merge` on
// record covers (ldd.MergeChecked). The line is read before it runs, so a
// check on the same line never counts. A move is:
//   - a fetch or a push to `.` or a local path whose refspec writes the
//     default branch (`+`, refs/heads/, several refspecs, `--`), and a fetch
//     of it from a remote that is not its upstream's;
//   - `update-ref` of refs/heads/<default>, or of HEAD while on it, given
//     as arguments or as `--stdin` lines; `symbolic-ref refs/heads/<default>`;
//   - `branch` creating, forcing (-f), renaming (-m, -M) or copying (-c, -C)
//     onto it; `checkout -b|-B`, `switch -c|-C` and `worktree add -b|-B` of it;
//   - `merge`, `pull`, `reset <rev>` or `rebase` while on it (or after a
//     checkout or a switch to it earlier in the line), and `rebase <x> <it>`.
//
// Each program is read where it runs, as the safety guard reads the line
// (guard.Targets: a cd, git -C, sh -c, env, a variable the line assigns),
// with `jhon sandbox run -- <command>` and a git alias read as what they
// run. It allows a move to a commit the branch already holds and an update
// from its own upstream (the tip of refs/remotes/<its remote>/<it>, @{u},
// a pull or a fetch from its remote, FETCH_HEAD while the line's last fetch
// was of it). It fails closed: a refspec, a destination, a revision or
// input it cannot read is refused, and a program it cannot place runs at
// top with its branch unknown, as on the default. A repository without
// LDD's layout is not judged.
func mergeGate(command, cwd, top string) string {
	w := &mergeWalk{top: top, line: command, on: map[string]string{}, fetchHead: map[string]*fetched{},
		defaults: map[string][]string{}, gated: map[string]bool{}}
	ps, err := sh.JudgeString(command, nil, nil, sh.Opts{})
	if err != nil {
		if top != "" && w.isGated(top) && w.mentionsMove(top, command) {
			return w.unknown(top, command, w.defaultOf(top), "the line cannot be read", "")
		}
		return ""
	}
	want := 0
	for _, p := range ps {
		if mayMove(p) {
			want++
		}
	}
	if want == 0 {
		return ""
	}
	placed := 0
	for _, t := range guard.Targets(command, cwd) {
		if mayMove(t.Program) {
			placed++
		}
		if why := w.placed(t, 0); why != "" {
			return why
		}
	}
	// A program the walk did not reach runs at the session's root, unplaced.
	if placed < want {
		for _, p := range ps {
			if why := w.program(p, top, true, 0); why != "" {
				return why
			}
		}
	}
	return ""
}

// mayMove is whether p may move a branch: git, a program the reader cannot
// name, `jhon sandbox run`, or code and input it cannot see.
func mayMove(p sh.Program) bool {
	if len(p.Argv) == 0 {
		return false
	}
	return p.Name == "git" || p.Name == "jhon" || unnamed.MatchString(p.Argv[0]) || p.Code != nil || p.StdinOpaque != ""
}

// mergeWalk is what the line has done so far, per repository root: the
// branch a checkout or a switch left it on ("?" when unknown), and what
// the line's last fetch left in FETCH_HEAD.
type mergeWalk struct {
	top, line string
	on        map[string]string
	fetchHead map[string]*fetched
	defaults  map[string][]string
	gated     map[string]bool
}

// fetched is what a fetch left in FETCH_HEAD for a merge: the default
// branch's own upstream, or revisions read in dir; nil when the guard
// cannot tell.
type fetched struct {
	upstream bool
	revs     []string
	dir      string
}

// placed judges t, a program the walk placed in its repository.
func (w *mergeWalk) placed(t guard.Target, depth int) string {
	root, lost := t.Root, t.Root == guard.UnknownRoot
	if lost {
		root = w.top
	}
	return w.program(t.Program, root, lost, depth)
}

// program judges p, run in the repository at root; lost is whether the
// walk lost where it runs.
func (w *mergeWalk) program(p sh.Program, root string, lost bool, depth int) string {
	if len(p.Argv) == 0 || root == "" || depth > 8 || !w.isGated(root) {
		return ""
	}
	form := strings.Join(p.Argv, " ")
	if p.Code != nil || p.StdinOpaque != "" {
		text := form
		if p.Code != nil {
			text += "\n" + *p.Code
		}
		if p.Raw != nil {
			text += "\n" + *p.Raw
		}
		if w.mentionsMove(root, text) {
			return w.unknown(root, form, w.defaultOf(root), "its code or input is not read", "")
		}
		return ""
	}
	switch {
	case unnamed.MatchString(p.Argv[0]) || p.Name == "git":
		// A program the reader cannot name may be git.
		return w.git(p, p.Argv[1:], form, root, lost, depth)
	case p.Name == "jhon":
		inner, ok := sandboxCommand(p.Argv[1:])
		if !ok {
			return ""
		}
		line := w.line
		w.line = inner
		defer func() { w.line = line }()
		for _, t := range guard.Targets(inner, root) {
			if lost && t.Root != guard.UnknownRoot {
				t.Root = guard.UnknownRoot
			}
			if why := w.placed(t, depth+1); why != "" {
				return why
			}
		}
	}
	return ""
}

// sandboxCommand is the command `jhon sandbox run [options] -- <command>`
// runs, its words quoted back into one line.
func sandboxCommand(args []string) (string, bool) {
	var subs []string
	for i, a := range args {
		if a == "--" {
			if len(subs) < 2 || subs[0] != "sandbox" || subs[1] != "run" || i+1 >= len(args) {
				return "", false
			}
			return strings.Join(quoteAll(args[i+1:]), " "), true
		}
		if !strings.HasPrefix(a, "-") && len(subs) < 2 {
			subs = append(subs, a)
		}
	}
	return "", false
}

// feedOf is what p reads on its standard input, as the line it came from
// gives it (sh.StdinOf: a literal echo or printf it is piped from, a
// here-string, a heredoc); nil when it reads none the reader can name.
func (w *mergeWalk) feedOf(p sh.Program) *sh.Feed {
	if p.Raw == nil {
		return nil
	}
	src := w.line
	if p.Script != nil {
		src = *p.Script
	}
	cmds, err := sh.Parse(src)
	if err != nil {
		return nil
	}
	for _, c := range cmds {
		if c.Raw == *p.Raw {
			return sh.StdinOf(c)
		}
	}
	return nil
}

// gitBuiltins are git commands a repository alias cannot shadow, read
// without asking git for one.
var gitBuiltins = map[string]bool{"status": true, "log": true, "diff": true, "show": true, "add": true, "commit": true, "rev-parse": true,
	"ls-files": true, "grep": true, "blame": true, "stash": true, "tag": true, "remote": true, "config": true, "worktree": true, "reset": true,
	"restore": true, "rebase": true, "cherry-pick": true, "revert": true, "init": true, "clone": true, "mv": true, "rm": true, "describe": true,
	"shortlog": true, "for-each-ref": true, "cat-file": true, "ls-tree": true, "symbolic-ref": true, "merge-base": true, "clean": true,
	"apply": true, "am": true, "format-patch": true, "notes": true, "reflog": true, "gc": true, "bisect": true, "submodule": true,
	"sparse-checkout": true, "version": true, "help": true, "rev-list": true, "show-ref": true, "branch": true, "checkout": true,
	"switch": true, "merge": true, "pull": true, "fetch": true, "push": true, "update-ref": true}

// git judges a git command line, args after `git`; p is the program.
func (w *mergeWalk) git(p sh.Program, args []string, form, root string, lost bool, depth int) string {
	g := sh.GitParts(args)
	if g.Sub == "" || slices.Contains(g.Globals, "--help") || slices.Contains(g.Globals, "-h") || slices.Contains(g.Rest, "--help") {
		return ""
	}
	if unnamed.MatchString(g.Sub) {
		if w.mentionsMove(root, "git "+strings.Join(g.Rest, " ")) || slices.ContainsFunc(g.Rest, func(a string) bool { return w.isDefault(root, a) }) {
			return w.unknown(root, form, w.defaultOf(root), "its subcommand is not written out", "")
		}
		return ""
	}
	// An alias the line defines (-c alias.X=...) or the repository's, read as what it runs.
	def, aliased := "", false
	for _, c := range g.Configs {
		if name, value, _ := strings.Cut(c, "="); strings.EqualFold(name, "alias."+g.Sub) {
			def, aliased = value, true
		}
	}
	if !aliased && !gitBuiltins[g.Sub] {
		def, aliased = project.ReadGit(root, "config", "--get", "alias."+g.Sub)
	}
	if aliased {
		if unnamed.MatchString(def) || depth > 8 {
			if w.mentionsMove(root, "git "+def+" "+strings.Join(g.Rest, " ")) {
				return w.unknown(root, form, w.defaultOf(root), "its alias is not written out", "")
			}
			return ""
		}
		if body, ok := strings.CutPrefix(strings.TrimSpace(def), "!"); ok {
			ps, err := sh.JudgeString(body+" "+strings.Join(quoteAll(g.Rest), " "), nil, nil, sh.Opts{})
			if err != nil {
				return w.unknown(root, form, w.defaultOf(root), "its alias cannot be read", "")
			}
			for _, q := range ps {
				if why := w.program(q, root, lost, depth+1); why != "" {
					return why
				}
			}
			return ""
		}
		expanded := append(append(slices.Clone(g.Globals), sh.Words(def)...), g.Rest...)
		return w.git(p, expanded, form, root, lost, depth+1)
	}
	switch g.Sub {
	case "fetch", "pull":
		return w.fetch(g, form, root, lost)
	case "push":
		return w.push(g, form, root)
	case "update-ref":
		return w.updateRef(p, g, form, root, lost)
	case "symbolic-ref":
		return w.symbolicRef(g, form, root, lost)
	case "branch":
		return w.branch(g, form, root, lost)
	case "checkout", "switch":
		return w.checkout(g, form, root, lost)
	case "worktree":
		return w.worktreeAdd(g, form, root, lost)
	case "merge":
		return w.merge(g, form, root, lost)
	case "reset":
		return w.reset(g, form, root, lost)
	case "rebase":
		return w.rebase(g, form, root, lost)
	}
	return ""
}

func quoteAll(words []string) []string {
	out := make([]string, len(words))
	for i, x := range words {
		out[i] = shellQuote(x)
	}
	return out
}

// gitArgs are a subcommand's arguments split: its operands, each option
// with the values it was given, and where `--` stood among the operands
// (-1 when it did not).
type gitArgs struct {
	ops    []string
	opts   map[string][]string
	dashed int
}

func (a gitArgs) has(names ...string) bool {
	return slices.ContainsFunc(names, func(n string) bool { _, ok := a.opts[n]; return ok })
}

// value is the last value given to any of names.
func (a gitArgs) value(names ...string) (string, bool) {
	v, found := "", false
	for _, n := range names {
		if vs, ok := a.opts[n]; ok && len(vs) > 0 {
			v, found = vs[len(vs)-1], true
		}
	}
	return v, found
}

// parseGit splits args: short options in letters and long ones in long
// take a value, attached or as the next word.
func parseGit(args []string, letters string, long ...string) gitArgs {
	a := gitArgs{opts: map[string][]string{}, dashed: -1}
	for k := 0; k < len(args); k++ {
		x := args[k]
		switch {
		case x == "--":
			a.dashed = len(a.ops)
			a.ops = append(a.ops, args[k+1:]...)
			return a
		case strings.HasPrefix(x, "--"):
			name, value, attached := strings.Cut(x, "=")
			if slices.Contains(long, name) && !attached && k+1 < len(args) {
				k++
				value = args[k]
			}
			a.opts[name] = append(a.opts[name], value)
		case strings.HasPrefix(x, "-") && len(x) > 1:
			for i := 1; i < len(x); i++ {
				c := string(x[i])
				if !strings.Contains(letters, c) {
					a.opts["-"+c] = append(a.opts["-"+c], "")
					continue
				}
				value := x[i+1:]
				if value == "" && k+1 < len(args) {
					k++
					value = args[k]
				}
				a.opts["-"+c] = append(a.opts["-"+c], value)
				break
			}
		default:
			a.ops = append(a.ops, x)
		}
	}
	return a
}

// fetch judges `git fetch` and `git pull`: a refspec that writes the
// default branch, what the fetch leaves in FETCH_HEAD, and for a pull what
// it merges into the branch it is on.
func (w *mergeWalk) fetch(g sh.Git, form, root string, lost bool) string {
	a := parseGit(g.Rest, "jos", "--depth", "--deepen", "--shallow-since", "--shallow-exclude", "--jobs", "--upload-pack",
		"--refmap", "--server-option", "--negotiation-tip", "--filter", "--recurse-submodules-default", "--submodule-prefix",
		"--strategy", "--strategy-option", "--cleanup")
	if a.has("--stdin") {
		return w.unknown(root, form, w.defaultOf(root), "it reads its refspecs from its input", "")
	}
	if a.has("--all", "--multiple") {
		w.fetchHead[root] = nil
		return ""
	}
	repo := ""
	if len(a.ops) > 0 {
		repo = a.ops[0]
	}
	specs := slices.Clone(a.ops[min(1, len(a.ops)):])
	if refmaps, ok := a.opts["--refmap"]; ok {
		specs = append(specs, refmaps...)
	}
	for _, c := range g.Configs {
		if name, value, _ := strings.Cut(c, "="); strings.HasPrefix(strings.ToLower(name), "remote.") && strings.HasSuffix(strings.ToLower(name), ".fetch") {
			specs = append(specs, value)
		}
	}
	// A pull merges into the branch it is on: is it the default one?
	cur, known := w.current(root, lost)
	onDefault := !known || w.isDefault(root, cur)
	branch := cur
	if !known {
		branch = w.defaultOf(root)
	}
	if unnamed.MatchString(repo) {
		w.fetchHead[root] = nil
		if g.Sub == "pull" && onDefault || slices.ContainsFunc(specs, func(s string) bool { return strings.Contains(s, ":") || unnamed.MatchString(s) }) {
			return w.unknown(root, form, branch, "the repository it fetches from is not written out", "")
		}
		return ""
	}
	dir, local := w.localRepo(root, repo)
	var srcs []string
	for k := 0; k < len(specs); k++ {
		if specs[k] == "tag" {
			k++
			continue
		}
		if unnamed.MatchString(specs[k]) {
			return w.unknown(root, form, branch, "its refspec `"+specs[k]+"` is not written out", "")
		}
		src, dst, mapped := strings.Cut(strings.TrimPrefix(specs[k], "+"), ":")
		if src == "" {
			src = "HEAD"
		}
		srcs = append(srcs, src)
		if !mapped {
			continue
		}
		to, why := w.defaultDest(root, dst)
		switch {
		case to == "":
		case why != "":
			return w.unknown(root, form, to, why, src)
		case local:
			if why := w.judge(root, form, to, src, dir, moveHeld); why != "" {
				return why
			}
		case !w.ownUpstream(root, to, repo, src):
			return w.unknown(root, form, to, "it fetches `"+src+"` from "+repo+", which the guard does not read before it runs and which is not "+to+"'s upstream", src)
		}
	}
	// What the fetch leaves in FETCH_HEAD for a merge.
	switch {
	case local && len(srcs) == 0:
		w.fetchHead[root] = &fetched{revs: []string{"HEAD"}, dir: dir}
	case local:
		w.fetchHead[root] = &fetched{revs: srcs, dir: dir}
	case len(srcs) > 0:
		w.fetchHead[root] = nil
		if !slices.ContainsFunc(srcs, func(s string) bool { return !w.ownUpstream(root, defaultNamed(s), repo, s) }) {
			w.fetchHead[root] = &fetched{upstream: true}
		}
	default:
		// No refspec: the branch's own upstream, when it fetches from its remote.
		w.fetchHead[root] = nil
		if remote, merge := w.upstreamOf(root, cur); known && remote != "" && (repo == "" || repo == remote) && w.ownUpstream(root, defaultNamed(merge), remote, merge) {
			w.fetchHead[root] = &fetched{upstream: true}
		}
	}
	if g.Sub == "fetch" || !onDefault || repo == "" {
		return ""
	}
	if local {
		if len(srcs) == 0 {
			srcs = []string{"HEAD"}
		}
		for _, src := range srcs {
			if why := w.judge(root, form, branch, src, dir, moveHeld); why != "" {
				return why
			}
		}
		return ""
	}
	for _, src := range srcs {
		if !w.ownUpstream(root, branch, repo, src) {
			return w.unknown(root, form, branch, "it pulls `"+src+"` from "+repo+", which the guard does not read before it runs and which is not "+branch+"'s upstream", src)
		}
	}
	return ""
}

// defaultNamed is the branch a remote's ref names: refs/heads/ and heads/
// taken off.
func defaultNamed(src string) string {
	return strings.TrimPrefix(strings.TrimPrefix(src, "refs/heads/"), "heads/")
}

// ownUpstream is whether src, fetched from remote, is branch's own
// upstream: branch is a default one, remote is its upstream's remote
// (branch.<branch>.remote), and src names branch (or the remote's HEAD).
func (w *mergeWalk) ownUpstream(root, branch, remote, src string) bool {
	if !w.isDefault(root, branch) || remote == "" {
		return false
	}
	own, _ := project.ReadGit(root, "config", "--get", "branch."+branch+".remote")
	if own == "" {
		own = "origin"
	}
	return own == remote && (src == "HEAD" || defaultNamed(src) == branch)
}

// upstreamOf is the remote and the ref branch's upstream names.
func (w *mergeWalk) upstreamOf(root, branch string) (remote, merge string) {
	if branch == "" {
		return "", ""
	}
	remote, _ = project.ReadGit(root, "config", "--get", "branch."+branch+".remote")
	merge, _ = project.ReadGit(root, "config", "--get", "branch."+branch+".merge")
	return remote, merge
}

// push judges `git push` to `.` or a local path; a push to a remote is the
// safety guard's to ask.
func (w *mergeWalk) push(g sh.Git, form, root string) string {
	a := parseGit(g.Rest, "o", "--repo", "--push-option", "--receive-pack", "--exec")
	if len(a.ops) < 2 {
		return ""
	}
	repo := a.ops[0]
	// A push to a repository the guard cannot name may be a local one.
	somewhere := unnamed.MatchString(repo)
	if _, local := w.localRepo(root, repo); !local && !somewhere {
		return ""
	}
	for _, spec := range a.ops[1:] {
		if unnamed.MatchString(spec) {
			return w.unknown(root, form, w.defaultOf(root), "its refspec `"+spec+"` is not written out", "")
		}
		src, dst, mapped := strings.Cut(strings.TrimPrefix(spec, "+"), ":")
		if !mapped {
			dst = src
		}
		if src == "" {
			continue
		}
		branch, why := w.defaultDest(root, dst)
		if branch == "" {
			continue
		}
		if somewhere {
			why = "the repository it pushes to is not written out"
		}
		if why != "" {
			return w.unknown(root, form, branch, why, src)
		}
		if why := w.judge(root, form, branch, src, root, moveHeld); why != "" {
			return why
		}
	}
	return ""
}

// mentionsDefault is whether s names one of root's default branches.
func (w *mergeWalk) mentionsDefault(root, s string) bool {
	return slices.ContainsFunc(w.defaultsOf(root), func(d string) bool { return strings.Contains(s, d) })
}

// updateRef judges `git update-ref <ref> <new>`, and each `update` or
// `create` line `--stdin` reads; input the guard cannot read is refused.
func (w *mergeWalk) updateRef(p sh.Program, g sh.Git, form, root string, lost bool) string {
	a := parseGit(g.Rest, "m")
	if a.has("--stdin") {
		feed := w.feedOf(p)
		if feed == nil || feed.Text == nil {
			return w.unknown(root, form, w.defaultOf(root), "it reads its updates from input the guard cannot read", "")
		}
		if a.has("-z") {
			if w.mentionsDefault(root, *feed.Text) || strings.Contains(*feed.Text, "HEAD") {
				return w.unknown(root, form, w.defaultOf(root), "it reads its updates NUL-separated", "")
			}
			return ""
		}
		for _, line := range strings.Split(*feed.Text, "\n") {
			f := strings.Fields(line)
			if len(f) < 3 || f[0] != "update" && f[0] != "create" {
				continue
			}
			if why := w.refWrite(form, root, lost, f[1], f[2], a.has("--no-deref")); why != "" {
				return why
			}
		}
		return ""
	}
	if a.has("-d") || len(a.ops) < 2 {
		return ""
	}
	return w.refWrite(form, root, lost, a.ops[0], a.ops[1], a.has("--no-deref"))
}

// refWrite judges writing ref, refs/heads/<default> or HEAD while on it,
// to rev.
func (w *mergeWalk) refWrite(form, root string, lost bool, ref, rev string, noDeref bool) string {
	branch := ""
	switch name, ok := strings.CutPrefix(ref, "refs/heads/"); {
	case unnamed.MatchString(ref):
		return w.unknown(root, form, w.defaultOf(root), "the ref it writes is not written out", rev)
	case ok && w.isDefault(root, name):
		branch = name
	case ref == "HEAD" && !noDeref:
		if cur, known := w.current(root, lost); !known {
			branch = w.defaultOf(root)
		} else if w.isDefault(root, cur) {
			branch = cur
		}
	}
	if branch == "" {
		return ""
	}
	return w.judge(root, form, branch, rev, root, moveHeld)
}

// symbolicRef judges `git symbolic-ref refs/heads/<default> <ref>`, which
// makes the default branch read as ref; `symbolic-ref HEAD <ref>` moves
// HEAD to ref, as a checkout does.
func (w *mergeWalk) symbolicRef(g sh.Git, form, root string, lost bool) string {
	a := parseGit(g.Rest, "m")
	if a.has("-d", "--delete") || len(a.ops) < 2 {
		return ""
	}
	name, ref := a.ops[0], a.ops[1]
	if unnamed.MatchString(name) || unnamed.MatchString(ref) {
		return w.unknown(root, form, w.defaultOf(root), "the ref it writes is not written out", "")
	}
	if name == "HEAD" {
		if !lost {
			w.on[root] = strings.TrimPrefix(ref, "refs/heads/")
		}
		return ""
	}
	if b, ok := strings.CutPrefix(name, "refs/heads/"); ok && w.isDefault(root, b) {
		return w.judge(root, form, b, ref, root, moveHeld)
	}
	return ""
}

// branch judges `git branch` creating, forcing, renaming or copying onto
// the default branch.
func (w *mergeWalk) branch(g sh.Git, form, root string, lost bool) string {
	a := parseGit(g.Rest, "u", "--set-upstream-to", "--contains", "--no-contains", "--merged", "--no-merged", "--points-at", "--format", "--sort")
	if a.has("-d", "-D", "--delete", "-l", "--list", "-a", "--all", "-r", "--remotes", "--show-current", "-v", "--verbose",
		"--edit-description", "--unset-upstream", "-u", "--set-upstream-to", "--contains", "--no-contains", "--merged", "--no-merged", "--points-at") {
		return ""
	}
	if a.has("-m", "-M", "--move", "-c", "-C", "--copy") {
		old, name := w.headRev(root, lost), ""
		switch len(a.ops) {
		case 1:
			name = a.ops[0]
		case 2:
			old, name = "refs/heads/"+a.ops[0], a.ops[1]
		default:
			return ""
		}
		if unnamed.MatchString(name) {
			return w.unknown(root, form, w.defaultOf(root), "the branch it names is not written out", old)
		}
		if !w.isDefault(root, name) {
			return ""
		}
		return w.judge(root, form, name, old, root, moveHeld)
	}
	if len(a.ops) == 0 {
		return ""
	}
	name, start := a.ops[0], w.headRev(root, lost)
	if len(a.ops) > 1 {
		start = a.ops[1]
	}
	return w.create(form, root, name, start, a.has("-f", "--force"))
}

// create judges creating branch name at start, or forcing it there: a
// default branch that exists is moved only when forced (git refuses
// otherwise); a name the guard cannot read is refused when it could be.
func (w *mergeWalk) create(form, root, name, start string, force bool) string {
	if unnamed.MatchString(name) {
		if force || !refExists(root, "refs/heads/"+w.defaultOf(root)) {
			return w.unknown(root, form, w.defaultOf(root), "the branch it names is not written out", start)
		}
		return ""
	}
	if !w.isDefault(root, name) || !force && refExists(root, "refs/heads/"+name) {
		return ""
	}
	return w.judge(root, form, name, start, root, moveHeld)
}

// checkout judges `git checkout -b|-B` and `git switch -c|-C` of the
// default branch, and keeps the branch a checkout or a switch moves to.
func (w *mergeWalk) checkout(g sh.Git, form, root string, lost bool) string {
	var a gitArgs
	var create, force []string
	if g.Sub == "checkout" {
		a, create, force = parseGit(g.Rest, "bB", "--orphan", "--conflict", "--pathspec-from-file"), []string{"-b"}, []string{"-B"}
	} else {
		a, create, force = parseGit(g.Rest, "cC", "--create", "--force-create", "--orphan", "--conflict"), []string{"-c", "--create"}, []string{"-C", "--force-create"}
	}
	set := func(b string) {
		if !lost {
			w.on[root] = b
		}
	}
	name, forced := a.value(force...)
	if !forced {
		name, _ = a.value(create...)
	}
	if name != "" || a.has(append(create, force...)...) {
		start := w.headRev(root, lost)
		if len(a.ops) > 0 {
			start = a.ops[0]
		}
		why := w.create(form, root, name, start, forced)
		set(name)
		return why
	}
	if b, ok := a.value("--orphan"); ok {
		set(b)
		return ""
	}
	if a.has("--detach") || g.Sub == "switch" && a.has("-d") {
		set("")
		return ""
	}
	if len(a.ops) == 0 || a.dashed == 0 {
		return ""
	}
	switch op := a.ops[0]; {
	case op == "-" || unnamed.MatchString(op):
		set("?")
	case w.isDefault(root, op) || refExists(root, "refs/heads/"+op) || g.Sub == "switch":
		set(op)
	case resolves(root, op):
		set("")
	}
	return ""
}

// worktreeAdd judges `git worktree add -b|-B <default>`.
func (w *mergeWalk) worktreeAdd(g sh.Git, form, root string, lost bool) string {
	if len(g.Rest) == 0 || g.Rest[0] != "add" {
		return ""
	}
	a := parseGit(g.Rest[1:], "bB", "--reason")
	name, forced := a.value("-B")
	if !forced {
		name, _ = a.value("-b")
	}
	if !a.has("-b", "-B") {
		return ""
	}
	start := w.headRev(root, lost)
	if len(a.ops) > 1 {
		start = a.ops[1]
	}
	return w.create(form, root, name, start, forced)
}

// onDefault is the default branch a command run at root works on, "" when
// it is on another; unknown is the default one, failing closed.
func (w *mergeWalk) onDefault(root string, lost bool) string {
	cur, known := w.current(root, lost)
	switch {
	case !known:
		return w.defaultOf(root)
	case w.isDefault(root, cur):
		return cur
	}
	return ""
}

// merge judges `git merge` of another commit into the default branch.
func (w *mergeWalk) merge(g sh.Git, form, root string, lost bool) string {
	a := parseGit(g.Rest, "mFsX", "--file", "--strategy", "--strategy-option", "--into-name", "--cleanup", "--message")
	if a.has("--abort", "--continue", "--quit") || len(a.ops) == 0 {
		return ""
	}
	branch := w.onDefault(root, lost)
	if branch == "" {
		return ""
	}
	for _, op := range a.ops {
		if op == "FETCH_HEAD" {
			if f, ok := w.fetchHead[root]; ok {
				if f == nil {
					return w.unknown(root, form, branch, "FETCH_HEAD holds what an earlier fetch on the line wrote, which the guard cannot read", "")
				}
				if f.upstream {
					continue
				}
				for _, rev := range f.revs {
					if why := w.judge(root, form, branch, rev, f.dir, moveHeld); why != "" {
						return why
					}
				}
				continue
			}
		}
		if op == "-" {
			op = "@{-1}"
		}
		if why := w.judge(root, form, branch, op, root, moveHeld); why != "" {
			return why
		}
	}
	return ""
}

// reset judges `git reset [<mode>] <rev>` on the default branch; a reset
// of paths, or with no revision, moves no branch.
func (w *mergeWalk) reset(g sh.Git, form, root string, lost bool) string {
	a := parseGit(g.Rest, "", "--pathspec-from-file")
	if a.has("-p", "--patch", "--pathspec-from-file") {
		return ""
	}
	before := a.ops
	if a.dashed >= 0 {
		if a.dashed < len(a.ops) {
			return ""
		}
		before = a.ops[:a.dashed]
	}
	if len(before) != 1 {
		return ""
	}
	rev := before[0]
	branch := w.onDefault(root, lost)
	if branch == "" {
		return ""
	}
	if !unnamed.MatchString(rev) && !resolves(root, w.inLine(root, rev)) {
		// A path, not a revision.
		return ""
	}
	return w.judge(root, form, branch, rev, root, moveHeld)
}

// rebase judges `git rebase [--onto <new>] [<upstream> [<branch>]]` of the
// default branch: onto its own upstream, a commit it already holds (but
// interactively), or by fast-forward to a checked commit.
func (w *mergeWalk) rebase(g sh.Git, form, root string, lost bool) string {
	a := parseGit(g.Rest, "sXx", "--onto", "--strategy", "--strategy-option", "--exec", "--empty")
	if a.has("--continue", "--abort", "--skip", "--quit", "--edit-todo", "--show-current-patch") {
		return ""
	}
	branch := ""
	if len(a.ops) >= 2 {
		branch = a.ops[1]
		if unnamed.MatchString(branch) {
			return w.unknown(root, form, w.defaultOf(root), "the branch it rebases is not written out", "")
		}
		if !lost {
			w.on[root] = branch
		}
		if !w.isDefault(root, branch) {
			return ""
		}
	} else if branch = w.onDefault(root, lost); branch == "" {
		return ""
	}
	base, onto := "", false
	if v, ok := a.value("--onto"); ok {
		base, onto = v, true
	} else if len(a.ops) > 0 {
		base = a.ops[0]
	}
	if base == "" {
		if a.has("--root") {
			return w.unknown(root, form, branch, "it rewrites the whole branch", "")
		}
		return "" // onto its upstream
	}
	rewrites := onto || a.has("-i", "--interactive", "--root", "-x", "--exec")
	return w.judgeRebase(root, form, branch, base, rewrites)
}

// Moves: moveHeld allows a commit the branch already holds.
const moveHeld = 1

// judge is why moving branch at root to rev (read in dir) is refused: it
// moves it to a commit no ready merge check covers. The commit it is at,
// one it already holds, and its own upstream's tip move nothing to
// deliver.
func (w *mergeWalk) judge(root, form, branch, rev, dir string, mode int) string {
	if unnamed.MatchString(rev) {
		return w.unknown(root, form, branch, "the revision it moves to is not written out", "")
	}
	if dir == root {
		rev = w.inLine(root, rev)
	}
	commit, ok := project.ReadGit(dir, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if !ok {
		return w.unknown(root, form, branch, "`"+rev+"` names no commit in "+dir, "")
	}
	cur, _ := project.ReadGit(root, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch+"^{commit}")
	switch {
	case commit == cur:
		return ""
	case mode == moveHeld && cur != "" && isAncestor(root, commit, cur):
		return ""
	case w.upstreamTip(root, branch, commit):
		return ""
	case ldd.MergeChecked(root, commit):
		return ""
	}
	return w.refusal(form, branch, commit, changeNamed(dir, rev))
}

// judgeRebase is why rebasing branch onto base is refused: allowed onto
// its own upstream's tip, onto a commit it holds when nothing is rewritten
// (a no-op), and by fast-forward to a checked commit.
func (w *mergeWalk) judgeRebase(root, form, branch, base string, rewrites bool) string {
	if unnamed.MatchString(base) {
		return w.unknown(root, form, branch, "the revision it rebases onto is not written out", "")
	}
	base = w.inLine(root, base)
	commit, ok := project.ReadGit(root, "rev-parse", "--verify", "--quiet", "--end-of-options", base+"^{commit}")
	if !ok {
		return w.unknown(root, form, branch, "`"+base+"` names no commit in "+root, "")
	}
	cur, _ := project.ReadGit(root, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch+"^{commit}")
	switch {
	case commit == cur && !rewrites:
		return ""
	case w.upstreamTip(root, branch, commit):
		return ""
	case cur != "" && isAncestor(root, commit, cur) && !rewrites:
		return ""
	case cur != "" && isAncestor(root, cur, commit) && ldd.MergeChecked(root, commit):
		return ""
	}
	return w.refusal(form, branch, commit, changeNamed(root, base))
}

func (w *mergeWalk) refusal(form, branch, commit, change string) string {
	return "`" + clipForm(form) + "` moves the default branch " + branch + " to " + commit[:min(12, len(commit))] +
		", and no ready merge check of that commit is on record (a check on the same line never counts: the guard reads the line before it runs): run `jhon deliver check " +
		change + " --action merge` on its own line, fix what it reports, then merge."
}

// inLine is rev as the line reads it so far: HEAD is the branch a
// checkout or a switch earlier in the line moved to.
func (w *mergeWalk) inLine(root, rev string) string {
	b, ok := w.on[root]
	if !ok || b == "" || b == "?" {
		return rev
	}
	if rest, ok := strings.CutPrefix(rev, "HEAD"); ok && (rest == "" || strings.ContainsAny(rest[:1], "~^@")) {
		return "refs/heads/" + b + rest
	}
	return rev
}

func isAncestor(root, a, b string) bool {
	return project.ReadGitCmd(root, "merge-base", "--is-ancestor", a, b).Run() == nil
}

// unknown is the refusal of a move whose commit the guard cannot read.
func (w *mergeWalk) unknown(root, form, branch, why, rev string) string {
	change := "<change>"
	if rev != "" && root != "" {
		change = changeNamed(root, rev)
	}
	return "`" + clipForm(form) + "` may move the default branch " + branch + ", and the guard cannot read to which commit: " + why +
		". Name the branch and the commit plainly: run `jhon deliver check " + change + " --action merge` on its own line, fix what it reports, then merge."
}

func clipForm(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 160 {
		return s[:159] + "…"
	}
	return s
}

// changeNamed is the change rev names when it is a local branch
// (execution.ChangeOfBranch), else "<change>".
func changeNamed(dir, rev string) string {
	name := strings.TrimPrefix(rev, "refs/heads/")
	if name != "" && !unnamed.MatchString(name) && refExists(dir, "refs/heads/"+name) {
		return execution.ChangeOfBranch(name)
	}
	return "<change>"
}

func refExists(dir, ref string) bool {
	return project.ReadGitCmd(dir, "show-ref", "--verify", "--quiet", ref).Run() == nil
}

func resolves(dir, rev string) bool {
	_, ok := project.ReadGit(dir, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	return ok
}

// upstreamTip is whether commit is the tip of branch's own upstream: the
// remote-tracking branch of its remote (branch.<branch>.remote, origin
// when none is set).
func (w *mergeWalk) upstreamTip(root, branch, commit string) bool {
	remote, _ := project.ReadGit(root, "config", "--get", "branch."+branch+".remote")
	if remote == "" {
		remote = "origin"
	}
	if remote == "." || unnamed.MatchString(remote) {
		return false
	}
	tip, ok := project.ReadGit(root, "rev-parse", "--verify", "--quiet", "refs/remotes/"+remote+"/"+branch+"^{commit}")
	return ok && tip == commit
}

// current is the branch the line leaves root on so far; known is false when
// the walk lost where the program runs or a checkout it cannot read moved
// it. A detached HEAD is "".
func (w *mergeWalk) current(root string, lost bool) (string, bool) {
	if lost {
		return "", false
	}
	if b, ok := w.on[root]; ok {
		return b, b != "?"
	}
	b, _ := project.ReadGit(root, "symbolic-ref", "--quiet", "--short", "HEAD")
	return b, true
}

// headRev is HEAD as the line leaves root so far.
func (w *mergeWalk) headRev(root string, lost bool) string {
	if b, known := w.current(root, lost); known && b != "" {
		return "refs/heads/" + b
	}
	return "HEAD"
}

// localRepo is the directory repo, a fetch's or a push's repository, names
// when it is `.` or a local path rather than a remote.
func (w *mergeWalk) localRepo(root, repo string) (string, bool) {
	switch {
	case repo == "" || unnamed.MatchString(repo):
		return "", false
	case repo == ".":
		return root, true
	}
	if _, remote := project.ReadGit(root, "config", "--get", "remote."+repo+".url"); remote {
		return "", false
	}
	p := strings.TrimPrefix(repo, "file://")
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		p = filepath.Join(project.Home(), rest)
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		return p, true
	}
	return "", false
}

// defaultDest is the default branch dst, a refspec's destination, writes,
// "" for another; why says when the guard cannot read it.
func (w *mergeWalk) defaultDest(root, dst string) (branch, why string) {
	switch {
	case dst == "":
		return "", ""
	case unnamed.MatchString(dst):
		return w.defaultOf(root), "its destination is not written out"
	case strings.Contains(dst, "*"):
		re, err := regexp.Compile("^" + strings.ReplaceAll(regexp.QuoteMeta(dst), `\*`, ".*") + "$")
		for _, d := range w.defaultsOf(root) {
			if err != nil || re.MatchString("refs/heads/"+d) || re.MatchString(d) {
				return d, "its destination is a pattern"
			}
		}
		return "", ""
	case strings.HasPrefix(dst, "refs/") && !strings.HasPrefix(dst, "refs/heads/"):
		return "", ""
	}
	if name := defaultNamed(dst); w.isDefault(root, name) {
		return name, ""
	}
	return "", ""
}

func (w *mergeWalk) isDefault(root, name string) bool {
	return slices.Contains(w.defaultsOf(root), name)
}

func (w *mergeWalk) defaultsOf(root string) []string {
	d, ok := w.defaults[root]
	if !ok {
		d = ldd.DefaultBranches(root)
		w.defaults[root] = d
	}
	return d
}

// defaultOf is the default branch root has, the first of its names that is
// a branch.
func (w *mergeWalk) defaultOf(root string) string {
	ds := w.defaultsOf(root)
	for _, d := range ds {
		if refExists(root, "refs/heads/"+d) {
			return d
		}
	}
	return ds[0]
}

// isGated is whether root is judged: a repository under LDD, or a joined
// member of a workspace whose primary is.
func (w *mergeWalk) isGated(root string) bool {
	g, ok := w.gated[root]
	if !ok {
		g = project.UsesLdd(root) || memberPrimary(root) != ""
		w.gated[root] = g
	}
	return g
}

var gitWord = regexp.MustCompile(`\bgit\b`)

// mentionsMove is whether text, code or a line the reader cannot read,
// names git and a write of root's default branch.
func (w *mergeWalk) mentionsMove(root, text string) bool {
	if !gitWord.MatchString(text) {
		return false
	}
	var names []string
	for _, d := range w.defaultsOf(root) {
		names = append(names, regexp.QuoteMeta(d))
	}
	d := "(?:" + strings.Join(names, "|") + ")"
	return regexp.MustCompile(`:\+?(?:refs/heads/)?(?:heads/)?` + d + `\b|refs/heads/` + d + `\b|(?:-[BCMfc]|--force|--force-create)\s+` + d + `\b`).MatchString(text)
}
