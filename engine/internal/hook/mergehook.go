package hook

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// The merge gate in git itself (T219 S5-S8): in a repository under LDD git
// runs `jhon hook merge-gate` as a reference-transaction hook named in the
// repository's local configuration (hook.jhon-merge-gate.command and
// .event, git's config-based hooks: a project's .git/hooks and
// core.hooksPath keep working and do not turn it off). On the "prepared"
// state it reads the transaction's updates (`<old> <new> <ref>` per line)
// and refuses one that moves refs/heads/<default> (ldd.DefaultBranches) to
// a commit no ready merge check covers, by the same rule as route-guard's
// line reading (mergeGate): the commit the branch is at, one it already
// holds, its own upstream's tip, or a commit ldd.MergeChecked covers. A
// deletion is the safety guard's; a symbolic ref at the default branch is
// refused. git prints the refusal and aborts the transaction, so every way
// of moving the branch (merge, pull, fetch, push to `.`, reset, rebase,
// update-ref, branch, worktree, aliases, scripts, `rebase -x`) is judged
// when git makes it.

// MergeGateName is the hook's name in git's configuration.
const MergeGateName = "jhon-merge-gate"

// MergeGate is `jhon hook merge-gate <state>`, run by git with the
// transaction on stdin; `jhon hook merge-gate --install` installs it in the
// repository of the working directory.
func MergeGate(args []string, io IO) int {
	cwd, _ := os.Getwd()
	if len(args) > 0 && args[0] == "--install" {
		root := project.Root(cwd)
		switch {
		case root == "" || !gatedRoot(root):
			fmt.Fprintf(io.Err, "jhon hook merge-gate: %s is in no repository under LDD; nothing installed\n", cwd)
			return 1
		case !InstallMergeGate(root):
			fmt.Fprintf(io.Err, "jhon hook merge-gate: could not write %s's git configuration\n", root)
			return 1
		}
		fmt.Fprintf(io.Out, "merge gate installed in %s (hook.%s)\n", root, MergeGateName)
		return 0
	}
	// Only the agent's git is judged (S9): the owner's never carries the
	// marker, and is never refused.
	if len(args) == 0 || args[0] != "prepared" || os.Getenv(AgentVariable) != "1" {
		return 0
	}
	trustedGit()
	var updates [][3]string
	sc := bufio.NewScanner(io.In)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 3 {
			updates = append(updates, [3]string{f[0], f[1], f[2]})
		}
	}
	root := hookRoot(cwd)
	if root == "" || !hookGated(root) {
		return 0
	}
	defaults := ldd.DefaultBranches(root)
	for _, u := range updates {
		branch, ok := strings.CutPrefix(u[2], "refs/heads/")
		if !ok || !contains(defaults, branch) || zeroSha.MatchString(u[1]) {
			continue
		}
		if why := gateUpdate(root, branch, u[1], updates); why != "" {
			fmt.Fprintln(io.Err, "jhon merge gate: "+why)
			return 2
		}
	}
	return 0
}

var zeroSha = regexp.MustCompile(`^0+$`)

// AgentVariable marks a command run on behalf of an agent session (S9):
// `jhon sandbox run` sets it for what it runs, pi's shell for its bash tool,
// and pre-tool for Claude Code's Bash with the sandbox off. The owner's own
// commands never carry it, so git's merge gate never refuses the owner.
const AgentVariable = "JHON_AGENT"

// hookRoot is the root of the repository git runs the hook in, as git
// itself gives it (whatever HOME is, L12 B3): the work tree it runs in, or,
// for a push into it (receive-pack runs in the git directory), the main
// work tree of its git common dir, else that dir.
func hookRoot(cwd string) string {
	// A git directory named in the environment is the repository git
	// updates, wherever the work tree is.
	if dir := os.Getenv("GIT_DIR"); dir != "" {
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(cwd, dir)
		}
		cwd = dir
		if common, ok := project.ReadGit(dir, "rev-parse", "--path-format=absolute", "--git-common-dir"); ok && filepath.Base(common) == ".git" {
			return project.Realish(filepath.Dir(common))
		}
	}
	if top, ok := project.ReadGit(cwd, "rev-parse", "--show-toplevel"); ok {
		return project.Realish(top)
	}
	common, ok := project.ReadGit(cwd, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if !ok {
		return ""
	}
	if filepath.Base(common) == ".git" {
		return project.Realish(filepath.Dir(common))
	}
	return common
}

// hookGated is whether the hook judges root: its own entry in the
// repository's local configuration, which Jhon writes only in a repository
// under LDD, or the LDD layout there; never the work tree's files alone,
// which a line may move away (L12 B2).
func hookGated(root string) bool {
	if v, ok := project.ReadGit(root, "config", "--local", "--get", "hook."+MergeGateName+".event"); ok && v == "reference-transaction" {
		return true
	}
	return gatedRoot(root)
}

// trustedGit makes the hook's own git calls run a git of the system's, not
// one a line put on PATH or in GIT_EXEC_PATH (L12 B7): PATH becomes that
// git's folder and the system's, and GIT_EXEC_PATH is dropped.
func trustedGit() {
	os.Unsetenv("GIT_EXEC_PATH")
	dirs := []string{"/usr/bin", "/bin", "/usr/local/bin", "/opt/homebrew/bin", "/opt/local/bin"}
	for _, d := range dirs {
		if st, err := os.Stat(filepath.Join(d, "git")); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			os.Setenv("PATH", d+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
			return
		}
	}
	// No git of the system's: the git that runs the hook is the one on the
	// owner's PATH as the session started it.
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// gatedRoot is whether root is judged: under LDD, or a joined member of a
// workspace whose primary is.
func gatedRoot(root string) bool { return project.UsesLdd(root) || memberPrimary(root) != "" }

// gateUpdate is why moving branch at root to value (a commit, or
// `ref:<ref>` for a symbolic ref) is refused, "" when it may move; updates
// are the whole transaction's.
func gateUpdate(root, branch, value string, updates [][3]string) string {
	if target, ok := strings.CutPrefix(value, "ref:"); ok {
		return "this makes the default branch " + branch + " a symbolic ref to " + target + ", which moves it with that ref and no merge check: " + s4Instruction("<change>")
	}
	commit := value
	cur, _ := project.ReadGit(root, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch+"^{commit}")
	if cur == "" {
		// The first commit of a repository creates it: one that holds no
		// commit at all, in any ref or reflog (L12 B5).
		if any, _ := project.ReadGit(root, "rev-list", "-n", "1", "--all", "--reflog"); any == "" {
			return ""
		}
	}
	remote, _ := project.ReadGit(root, "config", "--get", "branch."+branch+".remote")
	if remote == "" {
		remote = "origin"
	}
	tracking := "refs/remotes/" + remote + "/" + branch
	switch {
	case commit == cur:
		return ""
	case cur != "" && project.ReadGitCmd(root, "merge-base", "--is-ancestor", commit, cur).Run() == nil:
		return ""
	case remote != "." && upstreamAt(root, tracking, commit, updates):
		return ""
	case ldd.MergeChecked(root, commit):
		return ""
	}
	change := "<change>"
	if names, _ := project.ReadGit(root, "for-each-ref", "--points-at", commit, "--format=%(refname:short)", "refs/heads/"); names != "" {
		for _, n := range strings.Split(names, "\n") {
			if n != branch {
				change = execution.ChangeOfBranch(n)
				break
			}
		}
	}
	return "this moves the default branch " + branch + " to " + commit[:min(12, len(commit))] +
		", and no ready merge check of that commit is on record: " + s4Instruction(change)
}

// s4Instruction is what to do, in route-guard's words (S4).
func s4Instruction(change string) string {
	return "run `jhon deliver check " + change + " --action merge` on its own line, fix what it reports, then merge."
}

// upstreamAt is whether commit is tracking's tip, or what the same
// transaction moves tracking to (a fetch that updates the remote-tracking
// branch and the branch at once).
func upstreamAt(root, tracking, commit string, updates [][3]string) bool {
	for _, u := range updates {
		if u[2] == tracking && u[1] == commit {
			return true
		}
	}
	tip, ok := project.ReadGit(root, "rev-parse", "--verify", "--quiet", tracking+"^{commit}")
	return ok && tip == commit
}

// mergeGateJhon is the jhon git runs: the stable ~/.local/bin/jhon when it
// leads to an executable, else this binary.
func mergeGateJhon() string {
	link := filepath.Join(project.Home(), ".local", "bin", "jhon")
	if st, err := os.Stat(link); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
		return link
	}
	self, err := os.Executable()
	if err != nil {
		return "jhon"
	}
	return self
}

// MergeGateCommand is hook.jhon-merge-gate.command for root: a shell line
// (git appends the hook's arguments, "$@") that starts jhon only on the
// "prepared" state of a transaction that updates one of root's default
// branches, reading the updates with the shell's builtins, so every other
// state and ref update costs one sh and no other process, and the owner's
// git (no marker) stops at its first word; a jhon that no longer exists
// lets git go on.
func MergeGateCommand(root string) string {
	var cases []string
	for _, d := range ldd.DefaultBranches(root) {
		if c := "*" + shellQuote(" refs/heads/"+d); !contains(cases, c) {
			cases = append(cases, c)
		}
	}
	jhon := shellQuote(mergeGateJhon())
	// Keywords (case, while) and special builtins (exit, shift, set, unset,
	// exec), which no function replaces in sh's POSIX mode; the regular
	// builtins it uses (read, printf, command) have any function of that
	// name taken away first (L14 c8).
	return `case "$` + AgentVariable + `" in 1) ;; *) exit 0 ;; esac; unset -f read printf command; case "$1" in prepared) ;; *) exit 0 ;; esac; shift; hit=; ` +
		`while IFS= read -r l; do set -- "$@" "$l"; case "$l" in ` + strings.Join(cases, "|") + `) hit=1 ;; esac; done; ` +
		`case "$hit" in 1) ;; *) exit 0 ;; esac; command -v ` + jhon + ` >/dev/null 2>&1 || exit 0; printf '%s\n' "$@" | exec ` + jhon + ` hook merge-gate prepared`
}

// MergeGateInstalled is whether root's local configuration runs the merge
// gate as MergeGateCommand gives it, on reference-transaction, enabled.
func MergeGateInstalled(root string) bool {
	got, _ := project.ReadGit(root, "config", "--local", "--get-regexp", `^hook\.`+regexp.QuoteMeta(MergeGateName)+`\.`)
	want := mergeGateEntries(root)
	have := map[string]string{}
	for _, l := range strings.Split(got, "\n") {
		k, v, _ := strings.Cut(l, " ")
		have[strings.ToLower(k)] = v
	}
	for _, e := range want {
		if have[strings.ToLower(e[0])] != e[1] {
			return false
		}
	}
	return true
}

// mergeGateEntries are the merge gate's configuration entries for root.
func mergeGateEntries(root string) [][2]string {
	p := "hook." + MergeGateName + "."
	return [][2]string{{p + "event", "reference-transaction"}, {p + "command", MergeGateCommand(root)}, {p + "enabled", "true"}}
}

// InstallMergeGate writes the merge gate into root's local configuration
// when it is missing or differs, touching no other hook entry; false when
// git cannot write it.
func InstallMergeGate(root string) bool {
	if MergeGateInstalled(root) {
		return true
	}
	for _, e := range mergeGateEntries(root) {
		if project.ReadGitCmd(root, "config", "--local", "--replace-all", e[0], e[1]).Run() != nil {
			return false
		}
	}
	return true
}

// MergeGateUnsandboxed are the configuration entries an unsandboxed git
// line in root runs with for reference-transaction (sandbox.UnsandboxedEnv):
// in a repository under LDD, every other hook the configuration names for
// the event disabled and the merge gate forced to its own values, so it
// alone runs there; nil elsewhere (the event stays off).
func MergeGateUnsandboxed(root string) [][2]string {
	if root == "" || !gatedRoot(root) {
		return nil
	}
	var out [][2]string
	listed, _ := project.ReadGit(root, "config", "--get-regexp", `^hook\..*\.event$`)
	for _, l := range strings.Split(listed, "\n") {
		k, v, _ := strings.Cut(l, " ")
		name, ok := strings.CutSuffix(strings.TrimPrefix(k, "hook."), ".event")
		if !ok || v != "reference-transaction" || name == MergeGateName {
			continue
		}
		out = append(out, [2]string{"hook." + name + ".enabled", "false"})
	}
	p := "hook." + MergeGateName + "."
	return append(out, [2]string{p + "event", ""}, [2]string{p + "event", "reference-transaction"},
		[2]string{p + "command", MergeGateCommand(root)}, [2]string{p + "enabled", "true"}, [2]string{"hook.reference-transaction.enabled", "true"})
}
