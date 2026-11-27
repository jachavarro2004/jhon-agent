package guard

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

// ── git ─────────────────────────────────────────────────────────────────────

func readGit(dir string, args ...string) (string, bool) {
	cmd := project.ReadGitCmd(dir, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

var defaultRefs = []string{"refs/heads/main", "refs/heads/master", "refs/remotes/origin/main", "refs/remotes/origin/master"}

const firstParentDepth = 50

// forcedDeleteRefusal is why `git branch -D <branch>…` would lose work, or
// "" when every named branch is safe in every directory the command may run in.
func forcedDeleteRefusal(globals, rest []string, ctx *Ctx, dir string) string {
	names := operands(rest, nil, "")
	if len(names) == 0 || hasFlag(shortFlags(rest, ""), "r") || slices.Contains(longs(rest), "--remotes") {
		return "it deletes a branch whether or not its work is elsewhere"
	}
	var dirs []string
	for _, start := range unique([]string{dir, ctx.Cwd}) {
		d := start
		for k := 0; k < len(globals); k++ {
			if globals[k] == "-C" {
				k++
				if d != "" {
					d = project.Resolve(d, at(globals, k))
				}
				continue
			}
			if sh.GitValueGlobals[globals[k]] {
				return "the global option " + globals[k] + " hides the repository from the check that nothing is lost"
			}
		}
		if d == "" {
			return "the directory it runs in cannot be told"
		}
		dirs = append(dirs, d)
	}
	for _, d := range unique(dirs) {
		for _, b := range names {
			tip, ok := readGit(d, "rev-parse", "--verify", "--quiet", "refs/heads/"+b+"^{commit}")
			if !ok {
				if d == dirs[0] {
					return b + " is not a local branch of " + d
				}
				continue
			}
			upstream, _ := readGit(d, "for-each-ref", "--format=%(upstream)", "refs/heads/"+b)
			var bases []string
			for _, r := range defaultRefs {
				if _, ok := readGit(d, "rev-parse", "--verify", "--quiet", r); ok {
					bases = append(bases, r)
				}
			}
			reachable := false
			for _, r := range append([]string{upstream}, bases...) {
				if r == "" {
					continue
				}
				if _, ok := readGit(d, "merge-base", "--is-ancestor", tip, r); ok {
					reachable = true
					break
				}
			}
			if reachable {
				continue
			}
			tree, treeOK := readGit(d, "rev-parse", "--verify", "--quiet", tip+"^{tree}")
			squashed := false
			for _, r := range bases {
				log, _ := readGit(d, "log", "--first-parent", "-n", strconv.Itoa(firstParentDepth), "--format=%T", r)
				if treeOK && slices.Contains(strings.Split(log, "\n"), tree) {
					squashed = true
					break
				}
			}
			if squashed {
				continue
			}
			return b + " in " + d + " is not reachable from its upstream or the default branch, and its tree is not on the default branch: its work would be lost"
		}
	}
	return ""
}

var (
	pushValued = set("--push-option", "--repo", "--receive-pack", "--exec", "-o")
	// runsConfig is the configuration keys whose value git runs, or that move what git runs.
	runsConfig = lazyre.New(`(?i)^(?:alias\..+|core\.(?:hookspath|sshcommand|fsmonitor|gitproxy|pager|editor|askpass|worktree)|credential\..*helper|include\.path|includeif\..+|sequence\.editor|diff\.external|diff\..+\.(?:textconv|command)|filter\..+|merge\..+\.driver|gpg\..*program|uploadpack\.packobjectshook|pager\..+|url\..+\.insteadof|remote\..+\.(?:uploadpack|receivepack|proxy|vcs)|interactive\.difffilter|ssh\.variant|protocol\..+)$`)
	aliasKey   = lazyre.New(`(?i)^alias\.`)

	gitCommandsOnce sync.Once
	gitCommands     map[string]bool
)

// isGitCommand is whether sub is one of git's own commands or a git-<name> program on PATH.
func isGitCommand(sub string) bool {
	gitCommandsOnce.Do(func() {
		listed, _ := readGit("/", "--list-cmds=main,others")
		gitCommands = map[string]bool{}
		for _, c := range strings.Split(listed, "\n") {
			if c != "" {
				gitCommands[c] = true
			}
		}
		for _, c := range []string{"add", "am", "apply", "bisect", "blame", "branch", "checkout", "cherry-pick", "clean", "clone", "commit", "config", "describe",
			"diff", "fetch", "gc", "grep", "init", "log", "merge", "mv", "notes", "pull", "push", "rebase", "reflog", "remote", "reset", "restore", "revert",
			"rm", "show", "stash", "status", "submodule", "switch", "tag", "worktree", "rev-parse", "rev-list", "ls-files", "cat-file", "for-each-ref",
			"update-ref", "filter-branch", "help", "version", "archive", "bundle", "format-patch", "shortlog", "sparse-checkout", "maintenance"} {
			gitCommands[c] = true
		}
	})
	return gitCommands[sub]
}

// gitConfigWhy is why a `git config` invocation is asked, or "" for a read or an ordinary write.
func gitConfigWhy(rest []string) string {
	L := longs(rest)
	S := shortFlags(rest, "f")
	ops := operands(rest, set("-f", "--file", "--blob", "--type", "--default", "--comment", "--value"), "f")
	reads := []string{"--get", "--get-all", "--get-regexp", "--get-urlmatch", "--list", "--get-color", "--get-colorbool"}
	writes := []string{"--add", "--replace-all", "--unset", "--unset-all", "--rename-section", "--remove-section", "--edit"}
	any := func(names []string) bool {
		return slices.ContainsFunc(L, func(l string) bool { return slices.Contains(names, l) })
	}
	const editor = "it opens the configuration in an editor, which can set anything"
	var key, value string
	switch op := at(ops, 0); {
	case op == "get" || op == "list":
		return ""
	case slices.Contains([]string{"set", "unset", "rename-section", "remove-section", "edit"}, op):
		if op == "edit" {
			return editor
		}
		key, value = at(ops, 1), at(ops, 2)
	case slices.Contains(L, "--edit") || hasFlag(S, "e"):
		return editor
	case any(writes):
		key, value = at(ops, 0), at(ops, 1)
	case any(reads) || hasFlag(S, "l") || len(ops) < 2:
		return ""
	default:
		key, value = at(ops, 0), at(ops, 1)
	}
	if slices.Contains(L, "--global") || slices.Contains(L, "--system") {
		scope := "global"
		if slices.Contains(L, "--system") {
			scope = "system"
		}
		return "it writes the " + scope + " configuration, which every repository reads"
	}
	if aliasKey.MatchString(key) {
		if strings.HasPrefix(strings.TrimLeft(value, " \t\n\r"), "!") {
			return "the alias " + key + " runs a shell command"
		}
		return ""
	}
	if runsConfig.MatchString(key) {
		return key + " changes what git runs"
	}
	return ""
}

// gitTargetRoot is the project root of the repository a git command targets,
// or UnknownRoot when that cannot be told or the ways it is named disagree.
func gitTargetRoot(globals []string, dir string, ctx *Ctx, vars, envv map[string]string) string {
	d := dir
	gd, hasGD := envv["GIT_DIR"]
	if !hasGD {
		gd, hasGD = vars["GIT_DIR"]
	}
	wt, hasWT := envv["GIT_WORK_TREE"]
	if !hasWT {
		wt, hasWT = vars["GIT_WORK_TREE"]
	}
	for k := 0; k < len(globals); k++ {
		g := globals[k]
		if !sh.GitValueGlobals[g] {
			continue
		}
		k++
		v := at(globals, k)
		switch g {
		case "-C":
			if d != "" {
				d = pathOf(v, d, ctx, vars)
			}
		case "--git-dir":
			gd, hasGD = v, true
		case "--work-tree":
			wt, hasWT = v, true
		}
	}
	if d == "" {
		return UnknownRoot
	}
	var roots []string
	if hasGD {
		a := pathOf(gd, d, ctx, vars)
		if a == "" || !slices.Contains(VCSDirs, strings.ToLower(sh.Base(a))) {
			return UnknownRoot
		}
		roots = append(roots, ctx.rootOf(filepath.Dir(a)))
	}
	if hasWT {
		a := pathOf(wt, d, ctx, vars)
		r := ""
		if a != "" {
			r = ctx.rootOf(a)
		}
		roots = append(roots, r)
	}
	if len(roots) == 0 {
		roots = append(roots, ctx.rootOf(d))
	}
	for _, r := range roots {
		if r == "" || r != roots[0] {
			return UnknownRoot
		}
	}
	return roots[0]
}

// A Target is a program a command line runs and the project root of the
// repository it acts on (targetRoot).
type Target struct {
	Program sh.Program
	Root    string
}

// Targets is every program command runs from cwd, as the safety guard reads
// the line (a cd, a pushd, a subshell, env -C, the variables it assigns),
// each with the project root of the repository it acts on (targetRoot):
// route-guard judges a commit or a PR there, not at the session's root
// (T140, L1085). The rule is the safety guard's own walk; no second reading
// of cd or -C.
func Targets(command, cwd string) []Target {
	ctx := ContextOf(cwd)
	var out []Target
	ctx.targets = &out
	Classify(command, ctx)
	return out
}

// targetRoot is the project root of the repository a program run in st's
// directory acts on: a git command's, after -C, --git-dir, --work-tree and
// GIT_DIR (gitTargetRoot), any other's the directory's; UnknownRoot when a cd
// lost the directory, "" outside any repository.
func targetRoot(name string, args []string, st *State, ctx *Ctx, vars, envv map[string]string) string {
	if name == "git" {
		return gitTargetRoot(sh.GitParts(args).Globals, st.Dir, ctx, vars, envv)
	}
	if st.Dir == "" {
		return UnknownRoot
	}
	return ctx.rootOf(st.Dir)
}

// gitChdirRoot is the project root of the directory git -C moves a git
// command to, as a cd there would give it, or UnknownRoot when that
// directory cannot be told; ok is false when the command has no -C.
func gitChdirRoot(globals []string, dir string, ctx *Ctx, vars map[string]string) (root string, ok bool) {
	d := dir
	for k := 0; k < len(globals); k++ {
		if !sh.GitValueGlobals[globals[k]] {
			continue
		}
		k++
		if globals[k-1] == "-C" {
			ok = true
			if d != "" {
				d = pathOf(at(globals, k), d, ctx, vars)
			}
		}
	}
	if !ok {
		return "", false
	}
	if d == "" {
		return UnknownRoot, true
	}
	return askRoot(d, ctx), true
}

// gitNamesRepo is whether a git command names its repository by GIT_DIR or
// GIT_WORK_TREE (the environment, a variable, --git-dir or --work-tree)
// rather than by the directory it runs in.
func gitNamesRepo(globals []string, vars, envv map[string]string) bool {
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE"} {
		if _, ok := envv[k]; ok {
			return true
		}
		if _, ok := vars[k]; ok {
			return true
		}
	}
	return slices.Contains(globals, "--git-dir") || slices.Contains(globals, "--work-tree")
}

// skipsHooks is the advisory of a commit or a push that skips the
// repository's hooks: it runs, with a note (L1394).
const skipsHooks = "it skips the repository's hooks; run them unless the owner asked to skip them"

// judgeGit is the findings of a git command; a commit or a push without its
// hooks that nothing refuses carries the advisory beside them.
func judgeGit(args []string, dir string, ctx *Ctx, vars, envv map[string]string, depth int) []Finding {
	out := judgeGitOps(args, dir, ctx, vars, envv, depth)
	g := sh.GitParts(args)
	switch {
	case slices.ContainsFunc(out, func(f Finding) bool { return f.Level == "deny" }):
	case g.Sub == "commit" && (hasLong(g.Rest, "--no-verify") || hasFlag(shortFlags(g.Rest, "mFCcSt"), "n")):
		out = append(out, advise("git commit --no-verify", skipsHooks))
	case g.Sub == "push" && hasLong(g.Rest, "--no-verify"):
		out = append(out, advise("git push --no-verify", skipsHooks))
	}
	return out
}

func judgeGitOps(args []string, dir string, ctx *Ctx, vars, envv map[string]string, depth int) []Finding {
	g := sh.GitParts(args)
	gitDir := dir
	for k := 0; k < len(g.Globals); k++ {
		if g.Globals[k] == "-C" {
			k++
			if gitDir != "" {
				gitDir = project.Resolve(gitDir, at(g.Globals, k))
			}
		}
	}
	rest := g.Rest
	has := func(n ...string) bool { return hasLong(rest, n...) }
	D := func(what, why string) []Finding { return []Finding{deny(what, why)} }
	// A deletion, or a command the guard cannot read, in a repository
	// GIT_DIR or GIT_WORK_TREE names is that repository's, and one git -C
	// moves to is the directory's it names, not the directory's it runs in.
	rooted := func(f Finding) []Finding {
		if gitNamesRepo(g.Globals, vars, envv) {
			f.RootSet, f.Root = true, gitTargetRoot(g.Globals, dir, ctx, vars, envv)
		} else if r, ok := gitChdirRoot(g.Globals, dir, ctx, vars); ok {
			f.RootSet, f.Root = true, r
		}
		return []Finding{f}
	}
	// X is a deletion the owner approves: it loses work, and nothing else.
	X := func(what, why string) []Finding { return rooted(destroy(what, why)) }
	// E is an overwrite of working-tree files, which an edit does instead.
	E := func(what, why string) []Finding { return rooted(destroyAlt(what, why, editInstead)) }
	switch g.Sub {
	case "clean":
		if letters := shortFlags(rest, ""); hasFlag(letters, "f") || has("--force") {
			if hasFlag(letters, "xX") {
				return D("git clean -x", "it deletes ignored files too, where credentials such as .env live; the owner handles credentials")
			}
			return X("git clean -f", "it deletes untracked files for good")
		}
	case "reset":
		if i := slices.IndexFunc(rest, func(a string) bool { return a == "--hard" || a == "--merge" || a == "--keep" }); i >= 0 {
			return E("git reset "+rest[i], "it discards uncommitted work")
		} else if has("--hard", "--merge", "--keep") {
			return E("git reset undefined", "it discards uncommitted work")
		}
	case "checkout":
		letters := shortFlags(rest, "bB")
		if hasFlag(letters, "f") || has("--force") {
			return E("git checkout --force", "it discards uncommitted work")
		}
		if slices.Contains(rest, "--") {
			return E("git checkout -- <paths>", "it overwrites the working tree's paths")
		}
		ops := operands(rest, set("--orphan", "--conflict"), "bB")
		if slices.ContainsFunc(ops, func(o string) bool { return o == "." || strings.HasPrefix(o, ":/") }) {
			return E("git checkout .", "it overwrites the working tree")
		}
		if len(ops) >= 2 {
			return E("git checkout <ref> <paths>", "it overwrites the working tree's paths")
		}
		if len(ops) == 1 && !hasFlag(letters, "bB") && gitDir != "" {
			if project.Exists(project.Resolve(gitDir, ops[0])) {
				if _, ok := readGit(gitDir, "rev-parse", "--verify", "--quiet", ops[0]+"^{commit}"); !ok {
					return E("git checkout "+ops[0], "it names a path, and overwrites it in the working tree")
				}
			}
		}
	case "switch":
		if hasFlag(shortFlags(rest, "cC"), "f") || has("--force", "--discard-changes") {
			return X("git switch --discard-changes", "it discards uncommitted work")
		}
	case "restore":
		letters := shortFlags(rest, "s")
		staged := hasFlag(letters, "S") || has("--staged")
		worktree := hasFlag(letters, "W") || has("--worktree")
		if worktree || !staged {
			return E("git restore (worktree)", "it overwrites the working tree's files")
		}
	case "stash":
		if op := at(operands(rest, nil, ""), 0); op == "drop" || op == "clear" {
			return X("git stash "+op, "it deletes stashed work")
		}
	case "branch":
		letters := shortFlags(rest, "")
		del := hasFlag(letters, "d") || has("--delete")
		force := hasFlag(letters, "f") || has("--force")
		if hasFlag(letters, "D") || (del && force) {
			if why := forcedDeleteRefusal(g.Globals, rest, ctx, dir); why != "" {
				return X("git branch -D", why)
			}
		}
	case "worktree":
		if at(rest, 0) == "remove" && (hasFlag(shortFlags(rest[1:], ""), "f") || slices.Contains(longs(rest[1:]), "--force")) {
			return X("git worktree remove --force", "it discards the worktree's uncommitted work")
		}
	case "gc":
		if has("--prune") {
			return X("git gc --prune", "it deletes unreachable objects for good")
		}
	case "reflog":
		if op := at(rest, 0); op == "expire" || op == "delete" {
			return X("git reflog "+op, "it deletes the history recovery depends on")
		}
	case "filter-branch", "filter-repo":
		return D("git "+g.Sub, "it rewrites history")
	case "update-ref":
		if hasFlag(shortFlags(rest, ""), "d") || has("--stdin") {
			return X("git update-ref -d", "it deletes a ref")
		}
	case "push":
		letters := shortFlags(rest, "o")
		switch {
		case hasFlag(letters, "f") || has("--force", "--force-with-lease", "--force-if-includes"):
			return D("git push --force", "it overwrites remote history")
		case has("--mirror"):
			return D("git push --mirror", "it overwrites or deletes every remote ref")
		case has("--prune"):
			return D("git push --prune", "it deletes every remote ref with no local counterpart")
		}
		ops := operands(rest, pushValued, "o")
		if slices.ContainsFunc(ops, func(o string) bool { return strings.HasPrefix(o, "+") }) {
			return D("git push +<refspec>", "a + refspec forces the push")
		}
		target := gitTargetRoot(g.Globals, dir, ctx, vars, envv)
		// Deleting a named remote branch is outward-facing, not destructive in bulk.
		if hasFlag(letters, "d") || has("--delete") || (len(ops) > 1 && slices.ContainsFunc(ops[1:], func(o string) bool { return strings.HasPrefix(o, ":") })) {
			return []Finding{askAt("push", "git push --delete", target)}
		}
		// A push that updates only branches other than the default is a
		// change's review, not its delivery: it passes (S33).
		if target != UnknownRoot && featurePush(g, rest, ops, gitDir, vars, envv) {
			return nil
		}
		return []Finding{askAt("push", "git push", target)}
	case "config":
		if why := gitConfigWhy(rest); why != "" {
			return []Finding{askAt(gitConfig, "git config: "+why, "")}
		}
	default:
		if g.Sub == "" || strings.HasPrefix(g.Sub, "-") || isGitCommand(g.Sub) {
			return nil
		}
		// Not a git command: an alias of the repository's configuration, judged
		// as the git command it expands to. A shell alias (`!…`), or one the
		// guard cannot read, is judged by its visible words alone (L1311).
		def, ok := "", false
		if gitDir != "" {
			def, ok = readGit(gitDir, "config", "--get", "alias."+g.Sub)
		}
		if !ok || strings.HasPrefix(strings.TrimLeft(def, " \t\n\r"), "!") || depth >= 4 {
			return nil
		}
		cmds, err := sh.Parse(def)
		if err != nil || len(cmds) == 0 {
			return nil
		}
		i := slices.Index(args, g.Sub)
		expanded := append(append(slices.Clone(args[:i]), cmds[0].Words...), rest...)
		return judgeGit(expanded, dir, ctx, vars, envv, depth+1)
	}
	return nil
}

var (
	// pushGlobals are the git options before `push` that leave what it pushes as read.
	pushGlobals = set("--no-pager", "-P", "--no-optional-locks", "--no-replace-objects", "--literal-pathspecs", "--no-advice")
	// pushFlags are the push options that leave what it updates as its operands say.
	pushFlags = set("--set-upstream", "--quiet", "--verbose", "--progress", "--no-progress", "--dry-run", "--porcelain",
		"--atomic", "--no-atomic", "--signed", "--no-signed", "--push-option", "--ipv4", "--ipv6", "--no-follow-tags", "--no-recurse-submodules", "--no-thin", "--thin")
	pushLetters = "uqvn46"
	// pushConfig is the configuration that moves what a push updates.
	pushConfig = lazyre.New(`(?i)^(?:push\.(?:default|followtags|recursesubmodules)|remote\.pushdefault|remote\..+\.(?:push|mirror)|branch\..+\.(?:pushremote|remote|merge))$`)
)

// featurePush is whether a `git push` (neither forced, a mirror, nor a
// deletion) updates only branches that are not the default branch of the
// remote it pushes to: the remote's HEAD (origin/HEAD), else main and
// master. The current branch counts when it names no branch. A push of
// every branch, of a tag (one tag can start a release), through a
// configuration that moves what it updates, to a URL or a remote that is not
// configured, or one it cannot tell, is not.
func featurePush(g sh.Git, rest, ops []string, dir string, vars, envv map[string]string) bool {
	if dir == "" || len(g.Configs) > 0 {
		return false
	}
	for _, n := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_CONFIG", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS"} {
		if _, ok := envv[n]; ok {
			return false
		}
		if _, ok := vars[n]; ok {
			return false
		}
	}
	for k := 0; k < len(g.Globals); k++ {
		switch {
		case g.Globals[k] == "-C":
			k++
		case !pushGlobals[g.Globals[k]]:
			return false
		}
	}
	for k := 0; k < len(rest) && rest[k] != "--"; k++ {
		a := rest[k]
		switch {
		case strings.HasPrefix(a, "--"):
			n, v, attached := strings.Cut(a, "=")
			if n == "--recurse-submodules" && (v == "check" || v == "no") {
				continue
			}
			if !pushFlags[n] {
				return false
			}
			if n == "--push-option" && !attached {
				k++
			}
		case shortCluster.MatchString(a):
			for i := 1; i < len(a); i++ {
				if a[i] == 'o' {
					if i == len(a)-1 {
						k++
					}
					break
				}
				if strings.IndexByte(pushLetters, a[i]) < 0 {
					return false
				}
			}
		}
	}
	config := map[string]string{}
	listed, _ := readGit(dir, "config", "--get-regexp", `^(push|remote|branch)\.`)
	for _, l := range strings.Split(listed, "\n") {
		if k, v, _ := strings.Cut(l, " "); pushConfig.MatchString(k) {
			config[k] = v
		}
	}
	// on is a configured boolean that holds: git reads a key with no value as true.
	on := func(k string) bool {
		v, ok := config[k]
		return ok && !slices.Contains([]string{"false", "no", "off", "0"}, strings.ToLower(v))
	}
	current, onBranch := readGit(dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	branchConf := func(b, key string) string { return config["branch."+b+"."+key] }
	remote := at(ops, 0)
	if remote == "" {
		if !onBranch {
			return false
		}
		for _, r := range []string{branchConf(current, "pushremote"), config["remote.pushdefault"], branchConf(current, "remote"), "origin"} {
			if r != "" {
				remote = r
				break
			}
		}
	}
	if _, ok := readGit(dir, "config", "--get", "remote."+remote+".url"); !ok {
		return false
	}
	_, mapped := config["remote."+remote+".push"]
	if mapped || on("remote."+remote+".mirror") || on("push.followtags") ||
		on("push.recursesubmodules") && !slices.Contains([]string{"check", "no"}, config["push.recursesubmodules"]) {
		return false
	}
	pushDefault := config["push.default"]
	if pushDefault == "" {
		pushDefault = "simple"
	}
	// upstreamOf is where push.default upstream sends branch b: its upstream's name.
	upstreamOf := func(b string) string {
		if pushDefault != "upstream" && pushDefault != "tracking" {
			return ""
		}
		return strings.TrimPrefix(branchConf(b, "merge"), "refs/heads/")
	}
	branch := func(name string) bool {
		_, ok := readGit(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+name)
		return ok
	}
	tag := func(name string) bool {
		_, ok := readGit(dir, "rev-parse", "--verify", "--quiet", "refs/tags/"+name)
		return ok
	}
	var dests []string
	if len(ops) < 2 {
		if !onBranch || !slices.Contains([]string{"simple", "current", "upstream", "tracking"}, pushDefault) {
			return false
		}
		dests = append(dests, current, upstreamOf(current))
	}
	for _, spec := range ops[min(1, len(ops)):] {
		if spec == "tag" || strings.Contains(spec, "*") {
			return false
		}
		src, dst, mapped := strings.Cut(spec, ":")
		if !mapped {
			name := strings.TrimPrefix(src, "refs/heads/")
			if src == "HEAD" {
				if !onBranch {
					return false
				}
				name = current
			} else if strings.HasPrefix(name, "refs/") || !branch(name) || tag(name) {
				return false
			}
			dests = append(dests, name, upstreamOf(name))
			continue
		}
		name := strings.TrimPrefix(dst, "refs/heads/")
		if name == "" || strings.HasPrefix(name, "refs/") || name == "HEAD" {
			return false
		}
		// An unqualified destination takes the kind of its source, or of the ref it names.
		if name == dst && (tag(name) || strings.HasPrefix(src, "refs/tags/") || !strings.HasPrefix(src, "refs/heads/") && src != "HEAD" && tag(src) && !branch(src)) {
			return false
		}
		dests = append(dests, name)
	}
	// main and master always count as default, plus whatever the remote's
	// HEAD names: a push to main asks even where origin/HEAD is develop (L130).
	defaults := []string{"main", "master"}
	if head, ok := readGit(dir, "symbolic-ref", "--quiet", "refs/remotes/"+remote+"/HEAD"); ok {
		defaults = append(defaults, strings.TrimPrefix(head, "refs/remotes/"+remote+"/"))
	}
	return !slices.ContainsFunc(dests, func(d string) bool { return slices.Contains(defaults, d) })
}

// ── gh ──────────────────────────────────────────────────────────────────────

var ghWrites = map[string][]string{
	"pr":       {"create", "merge", "close", "edit", "reopen", "ready", "comment", "review", "lock", "unlock", "update-branch"},
	"issue":    {"create", "close", "comment", "delete", "edit", "reopen", "transfer", "lock", "unlock", "pin", "unpin", "develop"},
	"release":  {"create", "delete", "edit", "upload", "delete-asset"},
	"repo":     {"create", "edit", "fork", "rename", "archive", "unarchive", "sync", "set-default", "deploy-key"},
	"workflow": {"run", "enable", "disable"}, "run": {"rerun", "cancel", "delete"}, "secret": {"set", "delete"}, "variable": {"set", "delete"},
	"label": {"create", "edit", "delete", "clone"}, "gist": {"create", "edit", "delete"}, "cache": {"delete"},
}

// ghCollaboration is the pull request's own work, which passes (S33): opening
// it, commenting, marking it ready, and editing it. Its merge, close, review
// and every other write in ghWrites still ask.
var ghCollaboration = map[string][]string{"pr": {"create", "comment", "ready", "edit"}, "issue": {"comment"}}

var (
	ghField     = lazyre.New(`^-[fF]`)
	ghFieldLong = lazyre.New(`^--(?:field|raw-field|input)(?:=|$)`)
)

func ghAPIMethod(rest []string) string {
	for k, a := range rest {
		switch {
		case a == "-X" || a == "--method":
			return strings.ToUpper(at(rest, k+1))
		case strings.HasPrefix(a, "--method="):
			return strings.ToUpper(a[9:])
		case len(a) > 2 && strings.HasPrefix(a, "-X"):
			return strings.ToUpper(a[2:])
		}
	}
	if slices.ContainsFunc(rest, func(a string) bool { return ghField.MatchString(a) || ghFieldLong.MatchString(a) }) {
		return "POST"
	}
	return "GET"
}

var (
	ghAPIValued = set("-X", "--method", "-f", "-F", "--field", "--raw-field", "-H", "--header", "--hostname", "--input", "-q", "--jq", "-t", "--template", "--cache", "-p", "--preview")
	// ghPRWrite is the API endpoints of a pull request's collaboration, by method:
	// opening one, editing it, and commenting on it.
	ghPRWrite = map[string]*lazyre.Regexp{
		"POST":  lazyre.New(`^repos/[^/]+/[^/]+/(?:pulls|pulls/\d+/comments|pulls/\d+/comments/\d+/replies|issues/\d+/comments)$`),
		"PATCH": lazyre.New(`^repos/[^/]+/[^/]+/(?:pulls/\d+|pulls/comments/\d+|issues/comments/\d+)$`),
	}
)

// ghAPICollaboration is whether a `gh api` write of method m is a pull
// request's collaboration: an endpoint in ghPRWrite, whose fields do not set
// its state (closing it is not) and whose body is not read from a file.
func ghAPICollaboration(m string, rest []string) bool {
	re, ok := ghPRWrite[m]
	if !ok {
		return false
	}
	endpoint := ""
	for k := 0; k < len(rest); k++ {
		a := rest[k]
		n, v, attached := strings.Cut(a, "=")
		field := ""
		switch {
		case n == "--input":
			return false
		case n == "--field" || n == "--raw-field":
			field = v
			if !attached {
				k++
				field = at(rest, k)
			}
		case a == "-f" || a == "-F":
			k++
			field = at(rest, k)
		case strings.HasPrefix(a, "-f") || strings.HasPrefix(a, "-F"):
			field = a[2:]
		case ghAPIValued[n] && !attached:
			k++
		case strings.HasPrefix(a, "-"):
		case endpoint == "":
			endpoint = a
		}
		if strings.HasPrefix(field, "state=") {
			return false
		}
	}
	endpoint, _, _ = strings.Cut(strings.TrimPrefix(endpoint, "/"), "?")
	return re.MatchString(endpoint)
}

func judgeGh(args []string) []Finding {
	group, sub := at(args, 0), at(args, 1)
	var rest []string
	if len(args) > 2 {
		rest = args[2:]
	}
	if group == "pr" && sub == "merge" && slices.ContainsFunc(rest, func(a string) bool { n, _, _ := strings.Cut(a, "="); return n == "--admin" }) {
		return []Finding{deny("gh pr merge --admin", "it bypasses branch protection")}
	}
	if group == "repo" && sub == "delete" {
		return []Finding{deny("gh repo delete", "it deletes a repository for good")}
	}
	if group == "api" {
		if m := ghAPIMethod(args[1:]); m != "GET" && m != "HEAD" && !ghAPICollaboration(m, args[1:]) {
			return []Finding{ask("gh-write", "gh api ("+m+")")}
		}
		return nil
	}
	if slices.Contains(ghWrites[group], sub) && !slices.Contains(ghCollaboration[group], sub) && len(args) > 1 {
		return []Finding{ask("gh-write", "gh "+group+" "+sub)}
	}
	return nil
}

// ── containers ──────────────────────────────────────────────────────────────

var (
	dockerGlobals  = set("--context", "-c", "-H", "--host", "--config", "-l", "--log-level", "--tlscacert", "--tlscert", "--tlskey")
	composeGlobals = set("-f", "--file", "-p", "--project-name", "--env-file", "--profile", "--project-directory", "--ansi", "--parallel", "--progress")
	execValued     = set("-e", "--env", "--env-file", "-u", "--user", "-w", "--workdir", "--detach-keys", "--index")
	runValued      = func() map[string]bool {
		m := set("-v", "--volume", "-p", "--publish", "--name", "--entrypoint", "--network", "--net", "--mount", "-l", "--label",
			"--platform", "--pull", "--restart", "--cpus", "-m", "--memory", "-h", "--hostname", "--add-host", "--cap-add", "--cap-drop", "--device", "--gpus",
			"--link", "--log-driver", "--log-opt", "--tmpfs", "--ulimit", "--shm-size", "--security-opt", "--dns", "--expose", "--health-cmd", "--ipc", "--pid",
			"--runtime", "--stop-signal", "--cidfile", "--group-add", "--label-file", "--network-alias", "--userns", "--uts", "--volumes-from", "--workdir",
			"--cgroupns", "--cpu-shares", "-c", "--memory-swap", "--stop-timeout", "--env-from-file")
		for k := range execValued {
			m[k] = true
		}
		return m
	}()
	dockerReads = set("ps", "images", "logs", "inspect", "version", "info", "context", "stats", "top", "events", "history", "port", "diff", "search")
	localSocket = lazyre.New(`^(?:unix|npipe)://`)
	portSuffix  = lazyre.New(`:\d+$`)
)

func remoteDockerHost(h string) bool {
	if localSocket.MatchString(h) {
		return false
	}
	u, ok := urlHost(h)
	if !ok {
		u = portSuffix.ReplaceAllString(h, "")
	}
	return remoteHost(u)
}

// containerWords is a container command's words after its options: the command it runs.
func containerWords(args []string, valued map[string]bool) []string {
	k := 0
	entrypoint := ""
	for k < len(args) && strings.HasPrefix(args[k], "-") {
		a := args[k]
		if a == "--" {
			k++
			break
		}
		n, v, attached := strings.Cut(a, "=")
		if !attached && valued[n] {
			k++
			v = at(args, k)
		}
		if n == "--entrypoint" {
			entrypoint = v
		}
		k++
	}
	var cmd []string
	if k+1 < len(args) {
		cmd = args[k+1:]
	}
	if entrypoint != "" {
		return append([]string{entrypoint}, cmd...)
	}
	return cmd
}

// judgeDocker is the findings of docker, podman, or compose; inner judges a command run inside a container.
func judgeDocker(name string, all []string, envv map[string]string, inner func([]string) []Finding) []Finding {
	k := 0
	context, hasContext := envv["DOCKER_CONTEXT"]
	host, hasHost := envv["DOCKER_HOST"]
	if !strings.HasSuffix(name, "-compose") {
		for k < len(all) && strings.HasPrefix(all[k], "-") {
			n, v, attached := strings.Cut(all[k], "=")
			if !attached && dockerGlobals[n] {
				k++
				v = at(all, k)
			}
			if n == "--context" || n == "-c" {
				context, hasContext = v, attached || dockerGlobals[n]
			}
			if n == "-H" || n == "--host" {
				host, hasHost = v, attached || dockerGlobals[n]
			}
			k++
		}
	}
	args := all[min(k, len(all)):]
	ops := nonOptions(args)
	var out []Finding
	remote := ""
	if hasContext && context != "" && context != "default" {
		remote = "the context " + context
	} else if hasHost && host != "" && remoteDockerHost(host) {
		remote = "the host " + host
	}
	if remote != "" && !dockerReads[at(ops, 0)] {
		out = append(out, ask("deploy", name+" on "+remote))
	}
	return append(out, judgeDockerCommand(name, args, ops, inner)...)
}

func judgeDockerCommand(name string, args, ops []string, inner func([]string) []Finding) []Finding {
	var compose []string
	isCompose := false
	if strings.HasSuffix(name, "-compose") {
		compose, isCompose = args, true
	} else if at(ops, 0) == "compose" {
		compose, isCompose = args[slices.Index(args, "compose")+1:], true
	}
	if isCompose {
		c := 0
		for c < len(compose) && strings.HasPrefix(compose[c], "-") {
			if composeGlobals[compose[c]] {
				c += 2
			} else {
				c++
			}
		}
		if s := at(compose, c); s == "exec" || s == "run" {
			if cmd := containerWords(compose[c+1:], runValued); len(cmd) > 0 {
				return inner(cmd)
			}
			return nil
		}
		if down := slices.Index(compose, "down"); down >= 0 {
			after := compose[down+1:]
			if hasFlag(shortFlags(after, ""), "v") || slices.Contains(longs(after), "--volumes") {
				return []Finding{destroy(name+" compose down -v", "it deletes the volumes and their data")}
			}
		}
		return nil
	}
	sub, sub2 := at(ops, 0), at(ops, 1)
	runSub := ""
	if sub == "container" && (sub2 == "exec" || sub2 == "run") {
		runSub = sub2
	} else if sub == "exec" || sub == "run" {
		runSub = sub
	}
	if runSub != "" {
		valued := runValued
		if runSub == "exec" {
			valued = execValued
		}
		if cmd := containerWords(args[slices.Index(args, runSub)+1:], valued); len(cmd) > 0 {
			return inner(cmd)
		}
		return nil
	}
	if sub == "volume" && (sub2 == "rm" || sub2 == "remove" || sub2 == "prune") {
		return []Finding{destroy(name+" volume "+sub2, "it deletes volumes and their data")}
	}
	if sub == "system" && sub2 == "prune" {
		return []Finding{destroy(name+" system prune", "it deletes containers, images, and volumes")}
	}
	if sub == "rm" || (sub == "container" && (sub2 == "rm" || sub2 == "remove")) {
		word := sub2
		if sub == "rm" {
			word = "rm"
		}
		after := args[slices.Index(args, word)+1:]
		if hasFlag(shortFlags(after, ""), "v") || slices.Contains(longs(after), "--volumes") {
			return []Finding{destroy(name+" rm -v", "it deletes the container's volumes")}
		}
		if hasFlag(shortFlags(after, ""), "f") || slices.Contains(longs(after), "--force") {
			return []Finding{destroy(name+" rm -f", "it kills a running container and deletes it with the state it holds")}
		}
	}
	if sub == "push" || (sub == "image" && sub2 == "push") {
		return []Finding{ask("publish", name+" push")}
	}
	if sub == "buildx" && slices.Contains(args, "--push") {
		return []Finding{ask("publish", name+" buildx --push")}
	}
	return nil
}

// ── outward-facing programs ─────────────────────────────────────────────────

var (
	localHosts = set("", "localhost", "127.0.0.1", "::1", "[::1]", "0.0.0.0")
	urlHostRe  = lazyre.New(`(?i)^[a-z][a-z0-9+.-]*://(?:[^@/]*@)?(\[[^\]]*\]|[^:/?]+)`)
	dbClients  = []string{"psql", "mysql", "mariadb", "mongosh", "mongo", "redis-cli", "pg_restore", "sqlcmd", "cqlsh", "clickhouse-client"}
	dbEnv      = []string{"PGHOST", "PGHOSTADDR", "MYSQL_HOST", "DATABASE_URL", "REDIS_URL", "REDIS_HOST", "MONGODB_URI", "MONGO_URL", "POSTGRES_HOST", "DB_HOST"}
	dbTools    = set(append(slices.Clone(dbClients), "prisma", "alembic", "migrate", "dbmate", "goose", "flyway", "liquibase", "knex", "sequelize", "drizzle-kit", "typeorm", "atlas", "sqitch")...)
	hostKV     = lazyre.New(`(?:^|\s)host=([^\s]+)`)
)

func urlHost(s string) (string, bool) {
	if m := urlHostRe.FindStringSubmatch(s); m != nil {
		return m[1], true
	}
	return "", false
}

func remoteHost(h string) bool { return !strings.HasPrefix(h, "/") && !localHosts[strings.ToLower(h)] }

// dbHost is the host a database client connects to, when it names a non-local one.
func dbHost(args []string) string {
	for k, a := range args {
		if a == "-h" || a == "--host" || a == "-H" {
			if k+1 < len(args) && remoteHost(args[k+1]) {
				return args[k+1]
			}
			continue
		}
		if strings.HasPrefix(a, "--host=") && remoteHost(a[7:]) {
			return a[7:]
		}
		if len(a) > 2 && strings.HasPrefix(a, "-h") && remoteHost(a[2:]) {
			return a[2:]
		}
		if u, ok := urlHost(a); ok && remoteHost(u) {
			return u
		}
		if m := hostKV.FindStringSubmatch(a); m != nil && remoteHost(m[1]) {
			return m[1]
		}
	}
	return ""
}

// envDbHost is the non-local database host the command's environment names, or "".
func envDbHost(env map[string]string) string {
	for _, k := range dbEnv {
		v := env[k]
		if v == "" {
			continue
		}
		if strings.Contains(v, "$") || strings.Contains(v, "…") {
			return k + "=" + v
		}
		h := ""
		if strings.Contains(v, "://") {
			h, _ = urlHost(v)
		} else {
			h = portSuffix.ReplaceAllString(strings.Split(v, ",")[0], "")
		}
		if remoteHost(h) {
			return k + "=" + h
		}
	}
	return ""
}

var (
	curlValue       = "AbcCdDeEFHKmoOPQrTuUwxXYyz"
	curlLongValued  = set("--data", "--data-binary", "--data-raw", "--data-urlencode", "--data-ascii", "--json", "--output", "--header", "--user", "--user-agent", "--referer", "--request", "--cookie", "--cookie-jar", "--form", "--form-string", "--upload-file", "--url", "--max-time", "--connect-timeout", "--retry", "--cert", "--key", "--cacert", "--proxy", "--resolve", "--output-dir", "--write-out", "--config", "--limit-rate", "--range", "--interface", "--dns-servers", "--unix-socket", "--abstract-unix-socket", "--aws-sigv4", "--oauth2-bearer", "--connect-to", "--retry-delay", "--retry-max-time", "--max-filesize", "--speed-limit", "--speed-time", "--trace", "--trace-ascii", "--stderr", "--netrc-file", "--variable", "--expand-url")
	curlData        = lazyre.New(`^--(?:data|json)`)
	urlSplit        = lazyre.New(`[/?#]`)
	userInfo        = lazyre.New(`^[^@]*@`)
	curlUploadLong  = lazyre.New(`^--(?:upload-file|form|form-string)=`)
	curlDataAt      = lazyre.New(`^--(?:data|data-binary|data-urlencode|json|data-ascii)=.*@`)
	curlUploadFlags = set("-T", "--upload-file", "-F", "--form", "--form-string")
	curlDataFlags   = set("-d", "--data", "--data-binary", "--data-urlencode", "--json", "--data-ascii")
)

// curlRequest is the method a curl call sends, and whether any URL it names may be remote.
func curlRequest(args []string) (string, bool) {
	method, data, get, local := "", false, false, false
	var urls []string
	for k := 0; k < len(args); k++ {
		a := args[k]
		if strings.HasPrefix(a, "--") {
			n, v, attached := strings.Cut(a, "=")
			if !attached && curlLongValued[n] {
				k++
				v = at(args, k)
			}
			switch {
			case n == "--request":
				method = strings.ToUpper(v)
			case n == "--url":
				urls = append(urls, v)
			case n == "--get":
				get = true
			case n == "--head":
				if method == "" {
					method = "HEAD"
				}
			case curlData.MatchString(n):
				data = true
			case n == "--unix-socket" || n == "--abstract-unix-socket":
				local = true
			}
			continue
		}
		if shortCluster.MatchString(a) {
			for i := 1; i < len(a); i++ {
				ch := a[i]
				if ch == 'G' {
					get = true
				}
				if ch == 'I' && method == "" {
					method = "HEAD"
				}
				if strings.IndexByte(curlValue, ch) < 0 {
					continue
				}
				v := a[i+1:]
				if v == "" {
					k++
					v = at(args, k)
				}
				if ch == 'X' {
					method = strings.ToUpper(v)
				}
				if ch == 'd' {
					data = true
				}
				break
			}
			continue
		}
		urls = append(urls, a)
	}
	if method == "" {
		method = "GET"
		if !get && data {
			method = "POST"
		}
	}
	remote := !local && (len(urls) == 0 || slices.ContainsFunc(urls, func(u string) bool {
		if strings.Contains(u, "$") || strings.Contains(u, "…") {
			return true
		}
		h, ok := urlHost(u)
		if !ok {
			h = portSuffix.ReplaceAllString(userInfo.ReplaceAllString(urlSplit.Split(u, 2)[0], ""), "")
		}
		return remoteHost(h)
	}))
	return method, remote
}

func curlUpload(args []string) bool {
	for k := 0; k < len(args); k++ {
		a := args[k]
		if curlUploadFlags[a] || curlUploadLong.MatchString(a) {
			return true
		}
		if curlDataFlags[a] {
			if strings.Contains(at(args, k+1), "@") {
				return true
			}
			k++
			continue
		}
		if curlDataAt.MatchString(a) {
			return true
		}
		if shortCluster.MatchString(a) && !strings.HasPrefix(a, "--") {
			letters := a[1:]
			for i := 0; i < len(letters); i++ {
				ch := letters[i]
				if ch == 'T' || ch == 'F' {
					return true
				}
				if ch == 'd' {
					v := letters[i+1:]
					if v == "" {
						v = at(args, k+1)
					}
					if strings.Contains(v, "@") {
						return true
					}
					break
				}
				if strings.IndexByte(curlValue, ch) >= 0 {
					break
				}
			}
		}
	}
	return false
}

var (
	kubeWrites = set("apply", "delete", "create", "replace", "patch", "scale", "rollout", "set", "edit", "drain", "cordon", "uncordon", "label", "annotate", "expose", "run", "taint", "autoscale")
	kubeValued = set("-n", "--namespace", "--context", "--kubeconfig", "--cluster", "--user", "-s", "--server")
	awsWriteOp = lazyre.New(`^(create|delete|update|put|deploy|terminate|run|start|stop|modify|attach|detach|remove|invoke|publish|send|reboot|restore|replace|associate|disassociate|register|deregister|import|execute)-`)
)

// paneMultiplexers are the terminal multiplexers whose pane-run command the
// guard reads: what they run in another pane is judged like a direct call.
var paneMultiplexers = set("herdr")

// The pane multiplexer's commands that run or type something in a pane,
// from its own --help, version 0.9.5 (L491; its source left the repository
// in T24): for each,
// the long flags that take a value. Global flags come before the group:
// --session, --machine, --remote and --remote-keybindings take a value,
// --handoff is bare. What each one runs is read:
//   - pane run <pane> <command…>: the command, as a shell line;
//   - pane send-text <pane> <text>: the text, its line ends as typed;
//   - pane send-keys / agent send-keys <target> <key…>: the keys that are
//     not key names, as typed text;
//   - agent prompt <target> <text>: each line a coding agent runs as a
//     shell command (`!…`); the rest is a prompt, which the agent's own
//     guards judge;
//   - agent start <name> --kind <kind> -- <args…>: the agent's program.
var paneCommands = map[string][]string{
	"pane run":        nil,
	"pane send-text":  nil,
	"pane send-keys":  nil,
	"agent send-keys": nil,
	"agent prompt":    {"--until", "--timeout"},
	"agent start":     {"--kind", "--pane", "--timeout"},
}

var paneGlobalValued = set("--session", "--machine", "--remote", "--remote-keybindings")

// paneKey is a key name send-keys presses rather than types.
var paneKey = lazyre.New(`(?i)^(?:enter|return|esc|escape|tab|space|backspace|bspace|delete|up|down|left|right|home|end|pageup|pagedown|(?:c|ctrl|m|alt|s|shift)-.+|f[0-9]+)$`)

// paneRuns is what a multiplexer's command runs in another pane: shell
// text (a command line, or typed lines), or an agent's words.
func paneRuns(all []string) (script string, words []string) {
	k := 0
	for k < len(all) && strings.HasPrefix(all[k], "-") {
		name, _, inline := strings.Cut(all[k], "=")
		if paneGlobalValued[name] && !inline {
			k++
		}
		k++
	}
	cmd := at(all, k) + " " + at(all, k+1)
	valued, ok := paneCommands[cmd]
	if !ok {
		return "", nil
	}
	// Its operands, with its flags (anywhere, as clap reads them) taken
	// out; a command, text or keys after the target are taken whole, flags
	// and all, as the command's own flags.
	var ops []string
	flags := map[string]string{}
	rest := all[min(k+2, len(all)):]
	trailing := cmd == "pane run" || cmd == "pane send-text" || strings.HasSuffix(cmd, "send-keys")
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if trailing && len(ops) == 1 {
			if a == "--" {
				i++
			}
			ops = append(ops, rest[min(i, len(rest)):]...)
			break
		}
		if a == "--" {
			ops = append(ops, rest[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "--") {
			name, v, inline := strings.Cut(a, "=")
			if slices.Contains(valued, name) && !inline {
				i++
				v = at(rest, i)
			}
			flags[name] = v
			continue
		}
		ops = append(ops, a)
	}
	switch cmd {
	case "pane run":
		if len(ops) > 1 {
			return strings.Join(ops[1:], " "), nil
		}
	case "pane send-text":
		if len(ops) > 1 {
			return sh.TypedLines(strings.Join(ops[1:], " ")), nil
		}
	case "pane send-keys", "agent send-keys":
		var typed []string
		for _, a := range ops[min(1, len(ops)):] {
			if !paneKey.MatchString(a) {
				typed = append(typed, a)
			}
		}
		return sh.TypedLines(strings.Join(typed, " ")), nil
	case "agent prompt":
		var lines []string
		for _, l := range strings.Split(sh.TypedLines(strings.Join(ops[min(1, len(ops)):], " ")), "\n") {
			if t := strings.TrimSpace(l); strings.HasPrefix(t, "!") {
				lines = append(lines, strings.TrimPrefix(t, "!"))
			}
		}
		return strings.Join(lines, "\n"), nil
	case "agent start":
		if kind := flags["--kind"]; kind != "" {
			words = []string{kind}
			if dd := slices.Index(rest, "--"); dd >= 0 {
				words = append(words, rest[dd+1:]...)
			}
			return "", words
		}
	}
	return "", nil
}

// bypassFlag is the flag with which claude starts a session that never asks for permission, or "".
func bypassFlag(name string, args []string) string {
	if name == "claude" {
		if slices.Contains(args, "--dangerously-skip-permissions") {
			return "--dangerously-skip-permissions"
		}
		i := slices.IndexFunc(args, func(a string) bool { return a == "--permission-mode" || strings.HasPrefix(a, "--permission-mode=") })
		mode := ""
		if i >= 0 {
			if strings.Contains(args[i], "=") {
				mode = strings.Split(args[i], "=")[1]
			} else {
				mode = at(args, i+1)
			}
		}
		if mode == "bypassPermissions" {
			return "--permission-mode bypassPermissions"
		}
		return ""
	}
	return ""
}

// approveHooksWhat is what an agent's `jhon start --approve-hooks` does.
const approveHooksWhat = "jhon start --approve-hooks: it approves, for the owner, the hooks, extensions and MCP servers a session runs outside the sandbox"

// judgeApproveHooks is the settings question of a `jhon start` that
// approves what runs outside the sandbox (T266): `jhon start` checks the
// hooks, extensions and MCP servers of the roots it starts in against the
// owner's approval, and --approve-hooks records a change as approved
// without asking, so an agent that planted one could approve it itself.
// The question is the settings one, which no grant and no mode answers:
// asked at the top level, refused to a sub-agent. The flag counts wherever
// it stands among the words, an argument the runtime would get included
// (fail closed); a prompt that only mentions it is no flag.
//
// `jhon sync` is asked the same way (L1768): from a terminal it seeds the
// approval of every root the owner never started in, so an agent that got
// one (a pane run) could approve what it planted. Its help (-h, --help
// anywhere, which runs nothing) keeps its verdict.
func judgeApproveHooks(name string, args []string) []Finding {
	if name != "jhon" {
		return nil
	}
	switch at(args, 0) {
	case "start":
		if slices.ContainsFunc(args[1:], func(a string) bool { return a == "--approve-hooks" || strings.HasPrefix(a, "--approve-hooks=") }) {
			return []Finding{ask("settings", approveHooksWhat)}
		}
	case "sync":
		if !slices.ContainsFunc(args[1:], func(a string) bool { return a == "-h" || a == "--help" }) {
			return []Finding{ask("settings", seedHooksWhat)}
		}
	}
	return nil
}

// seedHooksWhat is what an agent's `jhon sync` does.
const seedHooksWhat = "jhon sync: from a terminal it approves, for the owner, the hooks, extensions and MCP servers of every folder he never started a session in"

// hookFlags are claude's flags that can leave out the plugin that carries
// Jhon's hooks, or turn its hooks off.
var hookFlags = []string{"--settings", "--setting-sources", "--bare"}

// judgeBypassLaunch judges a session started that never asks for
// permission; ok is false for any other program. Jhon's hooks reach a
// Claude Code session only through the plugin `jhon start` passes: a
// plain `claude` loads none (~/.claude/settings.json carries no Jhon guard,
// and 2.1.310 runs no agent frontmatter hooks), so nothing would guard it:
// it runs, with a note naming the guarded form (L1394). Under `jhon start`
// the plugin's guards refuse what nobody can be asked
// (test/testdata/spikes/claude-code-plugin-agent-hooks, a push refused
// under bypassPermissions), and it passes with none, unless a flag or
// another program can leave the plugin's hooks out.
func judgeBypassLaunch(name string, args []string, env map[string]string) ([]Finding, bool) {
	unguarded := func(what, how string) []Finding {
		return []Finding{advise(what, "it starts a session that never asks for permission and runs none of Jhon's guards: "+how+
			"; the guarded form is `jhon start --permission-mode bypassPermissions`")}
	}
	if name == "jhon" && at(args, 0) == "start" {
		b := bypassFlag("claude", args[1:])
		if b == "" {
			return nil, false
		}
		if _, other := env["JHON_CLAUDE"]; other {
			return unguarded("JHON_CLAUDE jhon start "+b, "JHON_CLAUDE runs another program in claude's place"), true
		}
		for _, a := range args[1:] {
			if flag, _, _ := strings.Cut(a, "="); slices.Contains(hookFlags, flag) {
				return unguarded("jhon start "+b+" "+flag, flag+" can leave out the plugin that carries them, or turn its hooks off"), true
			}
		}
		return nil, true
	}
	if b := bypassFlag(name, args); b != "" {
		return unguarded(name+" "+b, "a plain `claude` loads none of Jhon's hooks; only `jhon start` passes the plugin that carries them"), true
	}
	return nil, false
}

var (
	rsyncRemote  = lazyre.New(`^[^/]+:`)
	wgetUpload   = lazyre.New(`^--(?:post-file|body-file)(?:=|$)`)
	wgetPostData = lazyre.New(`^--(?:post-data|body-data)(?:=|$)`)
	publishTask  = lazyre.New(`^publish`)
)

func undefinedOr(s string, ok bool) string {
	if ok {
		return s
	}
	return "undefined"
}

// judgeOutward is the ask finding of an outward-facing program; env holds the assignments the command makes.
func judgeOutward(name string, args []string, env map[string]string) []Finding {
	ops := nonOptions(args)
	first, hasFirst := at(ops, 0), len(ops) > 0
	in := func(s string, xs ...string) bool { return slices.Contains(xs, s) }
	A := func(cls, what string) []Finding { return []Finding{ask(cls, what)} }
	approve := judgeApproveHooks(name, args)
	if f, ok := judgeBypassLaunch(name, args, env); ok {
		return append(approve, f...)
	}
	if approve != nil {
		return approve
	}
	if in(name, "npm", "pnpm", "yarn", "bun") && !in(first, "run", "exec", "dlx", "x", "run-script") && slices.Contains(ops[:min(3, len(ops))], "publish") {
		return A("publish", name+" publish")
	}
	if (name == "cargo" && first == "publish") || (name == "twine" && first == "upload") || (name == "gem" && first == "push") ||
		(in(name, "poetry", "uv", "flit", "hatch") && first == "publish") || (name == "dotnet" && first == "nuget" && at(ops, 1) == "push") ||
		(name == "mvn" && slices.Contains(ops, "deploy")) || (name == "gradle" && slices.ContainsFunc(ops, publishTask.MatchString)) {
		return A("publish", name+" "+undefinedOr(first, hasFirst))
	}
	// autossh is ssh, restarted (T228 L14).
	if in(name, "ssh", "autossh", "sftp", "mosh", "scp", "sshpass", "telnet") {
		return A("remote", name)
	}
	if name == "rsync" && slices.ContainsFunc(ops, func(o string) bool { return rsyncRemote.MatchString(o) || strings.HasPrefix(o, "rsync://") }) {
		return A("remote", "rsync to or from a host")
	}
	if name == "rclone" && in(first, "copy", "sync", "move", "delete", "purge", "copyto", "moveto") &&
		slices.ContainsFunc(ops[1:], func(o string) bool { return strings.Contains(o, ":") }) {
		return A("remote", "rclone "+first)
	}
	if name == "curl" && curlUpload(args) {
		return A("remote", "curl upload")
	}
	if name == "curl" {
		if m, remote := curlRequest(args); remote && !in(m, "GET", "HEAD", "OPTIONS") {
			return A("remote", "curl "+m+" to a remote host")
		}
	}
	if name == "wget" && slices.ContainsFunc(args, wgetUpload.MatchString) {
		return A("remote", "wget upload")
	}
	if name == "wget" {
		mi := slices.IndexFunc(args, func(a string) bool { return a == "--method" || strings.HasPrefix(a, "--method=") })
		m := "GET"
		if mi < 0 {
			if slices.ContainsFunc(args, wgetPostData.MatchString) {
				m = "POST"
			}
		} else if strings.Contains(args[mi], "=") {
			m = strings.ToUpper(strings.Split(args[mi], "=")[1])
		} else {
			m = strings.ToUpper(at(args, mi+1))
		}
		var urls []string
		for _, a := range nonOptions(args) {
			if strings.Contains(a, "://") {
				urls = append(urls, a)
			}
		}
		if m != "GET" && m != "HEAD" && (len(urls) == 0 || slices.ContainsFunc(urls, func(u string) bool {
			h, ok := urlHost(u)
			return ok && remoteHost(h)
		})) {
			return A("remote", "wget "+m+" to a remote host")
		}
	}
	last := ""
	if len(ops) > 0 {
		last = ops[len(ops)-1]
	}
	if name == "aws" {
		if first == "s3" {
			if in(at(ops, 1), "cp", "sync", "mv") && strings.HasPrefix(last, "s3://") {
				return A("remote", "aws s3 "+ops[1])
			}
			if in(at(ops, 1), "rm", "rb", "mv") {
				return A("deploy", "aws s3 "+ops[1])
			}
			return nil
		}
		// An SSM session with no document, the SSH one or a port forward is a
		// connection to the host, as ssh is, not a change to the account
		// (T228 L12, L14); a session document that runs a command, a custom
		// one, send-command and every other write stay deploy.
		if first == "ssm" && at(ops, 1) == "start-session" && sh.SSMConnection(args) {
			return A("remote", "aws ssm start-session")
		}
		if first == "deploy" || (len(ops) > 1 && slices.ContainsFunc(ops[1:], func(o string) bool { return awsWriteOp.MatchString(o) || o == "deploy" })) {
			return A("deploy", "aws "+undefinedOr(first, hasFirst))
		}
		return nil
	}
	if name == "gsutil" && in(first, "cp", "rsync", "mv") && strings.HasPrefix(last, "gs://") {
		return A("remote", "gsutil "+first)
	}
	if name == "gsutil" && in(first, "rm", "rb") {
		return A("deploy", "gsutil "+first)
	}
	if (name == "gcloud" || name == "az") && slices.ContainsFunc(ops, func(o string) bool { return in(o, "deploy", "create", "delete", "update", "apply", "set") }) {
		return A("deploy", name)
	}
	if name == "kubectl" || name == "oc" {
		kops := operands(args, kubeValued, "ns")
		if kubeWrites[at(kops, 0)] && !(at(kops, 0) == "rollout" && in(at(kops, 1), "status", "history")) {
			return A("deploy", name+" "+kops[0])
		}
		return nil
	}
	if in(name, "terraform", "tofu", "terragrunt") {
		if in(first, "apply", "destroy", "import", "taint", "untaint") || (first == "state" && in(at(ops, 1), "rm", "mv", "push")) {
			return A("deploy", name+" "+first)
		}
		return nil
	}
	if name == "helm" && in(first, "install", "upgrade", "uninstall", "delete", "rollback") {
		return A("deploy", "helm "+first)
	}
	if name == "pulumi" && in(first, "up", "destroy", "update") {
		return A("deploy", "pulumi "+first)
	}
	if in(name, "cdk", "sam", "serverless", "sls", "fly", "flyctl", "netlify", "firebase", "wrangler", "railway", "eb") &&
		in(first, "deploy", "destroy", "remove", "publish", "up", "down", "terminate", "delete") {
		return A("deploy", name+" "+first)
	}
	if name == "vercel" && (in(first, "deploy", "remove", "rm", "promote", "rollback", "alias") || slices.Contains(args, "--prod")) {
		return A("deploy", "vercel")
	}
	if name == "ansible-playbook" {
		return A("deploy", "ansible-playbook")
	}
	if name == "prisma" && first == "migrate" && at(ops, 1) == "deploy" {
		return A("deploy", "prisma migrate deploy")
	}
	if dbTools[name] {
		h := envDbHost(env)
		if h == "" {
			h = dbHost(args)
		}
		if h != "" {
			return A("deploy", name+" against "+h)
		}
	}
	return nil
}

// ── package runners ─────────────────────────────────────────────────────────

type runner struct {
	globals, subs, valued, shell []string
}

var (
	npx      = runner{valued: []string{"-p", "--package", "--cache", "--userconfig", "-w", "--workspace"}, shell: []string{"-c", "--call"}}
	uvValued = []string{"--from", "--with", "--with-requirements", "--with-editable", "-p", "--python", "--index", "--index-url", "--extra-index-url",
		"--project", "--directory", "--package", "--group", "--extra", "--env-file", "--config-file"}
	runners = map[string]runner{
		"npx": npx, "pnpx": npx, "bunx": {valued: []string{"-p", "--package"}}, "uvx": {valued: uvValued},
		"pnpm":   {globals: []string{"-C", "--dir", "--filter", "-F", "--workspace-dir", "--reporter", "--loglevel"}, subs: []string{"exec", "dlx"}, valued: []string{"--package", "--filter", "-F"}, shell: []string{"-c", "--shell-mode"}},
		"yarn":   {globals: []string{"--cwd"}, subs: []string{"exec", "dlx"}, valued: []string{"-p", "--package"}},
		"npm":    {globals: []string{"--prefix", "-w", "--workspace"}, subs: []string{"exec", "x"}, valued: []string{"--package", "-w", "--workspace"}, shell: []string{"-c", "--call"}},
		"bun":    {globals: []string{"--cwd"}, subs: []string{"x"}},
		"pipx":   {subs: []string{"run"}, valued: []string{"--spec", "--python", "--index-url", "--pip-args"}},
		"uv":     {globals: []string{"--directory", "--project", "--config-file", "--cache-dir"}, subs: []string{"run"}, valued: uvValued},
		"poetry": {globals: []string{"-C", "--directory", "-P", "--project"}, subs: []string{"run"}},
		"pipenv": {subs: []string{"run"}}, "pdm": {subs: []string{"run"}}, "hatch": {subs: []string{"run"}}, "bundle": {subs: []string{"exec"}},
	}
)

// runnerTarget is what a package runner runs: words, or a shell string; ok is false when it runs no program.
func runnerTarget(name string, args []string) (words []string, script string, isScript, ok bool) {
	r, known := runners[name]
	if !known {
		return nil, "", false, false
	}
	k := 0
	if r.subs != nil {
		for k < len(args) && strings.HasPrefix(args[k], "-") {
			if slices.Contains(r.globals, args[k]) {
				k += 2
			} else {
				k++
			}
		}
		if name == "uv" && at(args, k) == "tool" && at(args, k+1) == "run" {
			k += 2
		} else if k < len(args) && slices.Contains(r.subs, args[k]) {
			k++
		} else {
			return nil, "", false, false
		}
	}
	shellMode := false
	for k < len(args) && strings.HasPrefix(args[k], "-") {
		a := args[k]
		if a == "--" {
			k++
			break
		}
		n, v, attached := strings.Cut(a, "=")
		if slices.Contains(r.shell, n) {
			if name == "pnpm" {
				shellMode = true
				k++
				continue
			}
			if !attached {
				v = at(args, k+1)
			}
			return nil, v, true, true
		}
		if slices.Contains(r.valued, n) && !attached {
			k += 2
		} else {
			k++
		}
	}
	if k >= len(args) {
		return nil, "", false, false
	}
	if shellMode {
		return nil, strings.Join(args[k:], " "), true, true
	}
	return args[k:], "", false, true
}
