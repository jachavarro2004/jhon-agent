// Package project says where a call stands: the project root of a
// directory, and the roots a write may land in without asking.
package project

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Resolve is path.resolve(base, p): p made absolute against base, cleaned.
func Resolve(base, p string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}

// Exists is whether p names something, through symlinks.
func Exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Realish is p with its deepest existing ancestor replaced by that ancestor's real path.
func Realish(p string) string {
	abs := Resolve("", p)
	head := abs
	var tail []string
	for !Exists(head) {
		up := filepath.Dir(head)
		if up == head {
			return abs
		}
		tail = append([]string{filepath.Base(head)}, tail...)
		head = up
	}
	real, err := filepath.EvalSymlinks(head)
	if err != nil {
		return abs
	}
	return filepath.Join(append([]string{real}, tail...)...)
}

// Within is whether p is r or inside it.
func Within(p, r string) bool {
	if p == r {
		return true
	}
	if !strings.HasSuffix(r, string(filepath.Separator)) {
		r += string(filepath.Separator)
	}
	return strings.HasPrefix(p, r)
}

// Home is the user's home directory: $HOME, as node's homedir() reads it.
func Home() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

// ReadGit runs read-only git in dir, without inherited GIT_* variables; ok is false on failure.
func ReadGit(dir string, args ...string) (string, bool) {
	out, err := ReadGitCmd(dir, args...).Output()
	if err != nil {
		return "", false
	}
	s := strings.TrimSpace(string(out))
	return s, s != ""
}

// Root is the project root of dir: the git toplevel of its nearest
// existing ancestor; "" for a folder in no git repository, which is no
// project (T57a), and for HOME and /, which are never a project's.
func Root(dir string) string {
	d := Realish(dir)
	for !Exists(d) && filepath.Dir(d) != d {
		d = filepath.Dir(d)
	}
	top, ok := ReadGit(d, "rev-parse", "--show-toplevel")
	if !ok {
		return ""
	}
	root := Realish(top)
	if root == string(filepath.Separator) || root == Realish(Home()) {
		return ""
	}
	return root
}

var gitDir = lazyre.New(`[\\/]\.git$`)

// MainWorktree is the main worktree of the repository root belongs to, or "" when git cannot tell.
func MainWorktree(root string) string {
	if !Exists(root) {
		return ""
	}
	common, ok := ReadGit(root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if !ok {
		return ""
	}
	return mainOf(common)
}

// mainOf is the main worktree of the repository whose common dir is common:
// the folder that holds it when it is a .git, else "".
func mainOf(common string) string {
	if !gitDir.MatchString(common) {
		return ""
	}
	return Realish(filepath.Dir(common))
}

// RepoMain is r's main worktree, read from the disk as r is: the folder
// that holds its common dir when that is a .git, else its top; "" for no
// project.
func RepoMain(r Repo) string {
	if r.Top == "" {
		return ""
	}
	if main := mainOf(r.Common); main != "" {
		return main
	}
	return r.Top
}

// Roots are the project roots of dir: the root, the main worktree when the
// root is a linked one, and each one's sibling <root>-worktrees/.
func Roots(dir string) []string {
	root := Root(dir)
	if root == "" {
		return nil
	}
	return rootsOf(root, MainWorktree(root))
}

// rootsOf is Roots for a project root and its repository's main worktree.
func rootsOf(root, main string) []string {
	roots := []string{root}
	if main != "" && main != root {
		roots = append(roots, main)
	}
	out := append([]string{}, roots...)
	for _, r := range roots {
		out = append(out, r+"-worktrees")
	}
	return out
}

// TmpRoots are the scratch roots any write may land in: /tmp and $TMPDIR.
func TmpRoots() []string {
	out := []string{Realish("/tmp")}
	if t := os.Getenv("TMPDIR"); t != "" && filepath.IsAbs(t) {
		if r := Realish(t); r != out[0] {
			out = append(out, r)
		}
	}
	return out
}

var claudeSettings = lazyre.New(`(?i)(?:^|[\\/])\.claude[\\/]settings[^\\/]*\.json$`)

// IsClaudeSettings is whether abs is a Claude Code settings file, in HOME or a project.
func IsClaudeSettings(abs string) bool {
	return claudeSettings.MatchString(abs) || claudeSettings.MatchString(Realish(abs))
}

// GitReadEnv is the environment of a read-only git run that keeps the
// caller's GIT_* variables: this process's, with optional locks off, so a
// status or diff never takes .git/index.lock to refresh the index (L320),
// and with the keys that make git run a program neutralised (gitNeutral,
// T250): jhon's own git runs outside the sandbox, and a sandboxed command
// can write the repository's configuration. It knows no folder to read the
// filters' names in, so a command that may run a filter (filterCommands: a
// status, a diff, an archive) runs through ReadGitCmd instead.
func GitReadEnv() []string {
	return withGitConfig(append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat"), neutralGit())
}

// ReadGitCmd is the command ReadGit runs: git in dir, without inherited
// GIT_* variables, with optional locks off, and with the keys that make git
// run a program neutralised (gitNeutral, T250), the repository's filters
// among them for a command that may run one (gitFilters), and an external
// diff and a textconv off for a diff, log or show. A `git config` runs as
// written: it runs no program, and a command-line value would stand in for
// the one it reads (core.hooksPath).
func ReadGitCmd(dir string, args ...string) *exec.Cmd {
	if len(args) > 0 && (args[0] == "diff" || args[0] == "log" || args[0] == "show") {
		args = append([]string{args[0], "--no-ext-diff", "--no-textconv"}, args[1:]...)
	}
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	env := []string{"GIT_OPTIONAL_LOCKS=0"}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	if len(args) == 0 || args[0] != "config" {
		env = withGitConfig(append(env, "GIT_PAGER=cat"), append(neutralGit(), gitFilters(dir, args)...))
	}
	cmd.Env = env
	return cmd
}

// gitNeutral are the keys jhon's own git, run outside the sandbox, runs
// with whatever the repository's configuration says (T250, L1732: a
// sandboxed command can write .git/config, and a core.fsmonitor there ran
// under jhon's own `git status`): no fsmonitor, hook, pager, editor, ssh
// command but ssh, askpass, ext:: transport, or automatic gc or maintenance
// in the background. Given through GIT_CONFIG_COUNT, after every
// configuration file, they win. An alias never stands for a command git
// has (jhon runs only those), so no alias needs one.
var gitNeutral = [][2]string{
	{"core.fsmonitor", "false"},
	{"core.hooksPath", "/dev/null"},
	{"core.pager", "cat"},
	{"core.editor", "true"},
	{"sequence.editor", "true"},
	{"core.sshCommand", "ssh"},
	{"core.askPass", ""},
	{"protocol.ext.allow", "never"},
	{"gc.auto", "0"},
	{"gc.autoDetach", "false"},
	{"maintenance.auto", "false"},
}

// GitHookEvents are the hook events of githooks(5) (git 2.56). Since git
// 2.54 a hook may be named in the configuration (hook.<name>.command with
// hook.<name>.event), which core.hooksPath does not turn off, so a git run
// that must run no hook sets hook.<event>.enabled=false for each (L1318 b);
// an older git ignores the key.
var GitHookEvents = []string{
	"applypatch-msg", "pre-applypatch", "post-applypatch", "pre-commit", "pre-merge-commit", "prepare-commit-msg", "commit-msg", "post-commit",
	"pre-rebase", "post-checkout", "post-merge", "pre-push", "pre-receive", "update", "proc-receive", "post-receive", "post-update",
	"reference-transaction", "push-to-checkout", "pre-auto-gc", "post-rewrite", "sendemail-validate", "fsmonitor-watchman", "post-index-change",
}

// neutralGit is gitNeutral and every hook event turned off.
func neutralGit() [][2]string {
	out := slices.Clone(gitNeutral)
	for _, e := range GitHookEvents {
		out = append(out, [2]string{"hook." + e + ".enabled", "false"})
	}
	return out
}

// filterCommands are the git commands that may run a filter driver's
// clean, smudge or process program, checked against git 2.5x (T250 B4): a
// status compares a changed file through its clean filter, mv, rm (with
// --cached too), add and commit clean, checkout-index, read-tree -u and
// worktree add smudge.
var filterCommands = []string{"mv", "rm", "checkout-index", "read-tree", "worktree", "update-index", "status", "diff", "log", "show", "blame", "grep", "add", "checkout", "switch", "restore", "reset", "stash", "merge", "cherry-pick", "rebase", "commit", "archive", "apply", "am", "ls-files"}

// gitFilters are the filter drivers' programs the configuration in dir
// names, each emptied, for a command that may run one (filterCommands):
// git then filters nothing. The names are read with `git config
// --name-only --get-regexp`, which runs no program; nil when there are
// none or they cannot be read.
func gitFilters(dir string, args []string) [][2]string {
	if len(args) == 0 || !slices.Contains(filterCommands, args[0]) {
		return nil
	}
	out, err := ReadGitCmd(dir, "config", "--name-only", "-z", "--get-regexp", `^filter\..*\.(clean|smudge|process)$`).Output()
	if err != nil {
		return nil
	}
	var keys [][2]string
	for _, k := range strings.Split(string(out), "\x00") {
		if k = strings.TrimSpace(k); k != "" {
			keys = append(keys, [2]string{k, ""})
		}
	}
	return keys
}

// withGitConfig is env with entries given as GIT_CONFIG_KEY_<n> and
// GIT_CONFIG_VALUE_<n> after the env's own GIT_CONFIG_COUNT entries (none
// when the count is not a number), and the count raised to cover them.
func withGitConfig(env []string, entries [][2]string) []string {
	n := 0
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "GIT_CONFIG_COUNT="); ok {
			if c, err := strconv.Atoi(v); err == nil && c > 0 {
				n = c
			}
		}
	}
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if k == "GIT_CONFIG_COUNT" {
			continue
		}
		if i, ok := gitConfigIndex(k); ok && i >= n {
			continue
		}
		out = append(out, kv)
	}
	for i, e := range entries {
		out = append(out, "GIT_CONFIG_KEY_"+strconv.Itoa(n+i)+"="+e[0], "GIT_CONFIG_VALUE_"+strconv.Itoa(n+i)+"="+e[1])
	}
	return append(out, "GIT_CONFIG_COUNT="+strconv.Itoa(n+len(entries)))
}

// gitConfigIndex is the n of GIT_CONFIG_KEY_<n> or GIT_CONFIG_VALUE_<n>.
func gitConfigIndex(key string) (int, bool) {
	for _, prefix := range []string{"GIT_CONFIG_KEY_", "GIT_CONFIG_VALUE_"} {
		if v, ok := strings.CutPrefix(key, prefix); ok {
			i, err := strconv.Atoi(v)
			return i, err == nil
		}
	}
	return 0, false
}
