// Package project says where a call stands: the project root of a
// directory, and the roots a write may land in without asking.
package project

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"os"
	"os/exec"
	"path/filepath"
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
// status or diff never takes .git/index.lock to refresh the index (L320).
func GitReadEnv() []string { return append(os.Environ(), "GIT_OPTIONAL_LOCKS=0") }

// ReadGitCmd is the command ReadGit runs: git in dir, without inherited
// GIT_* variables, with optional locks off.
func ReadGitCmd(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	env := []string{"GIT_OPTIONAL_LOCKS=0"}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	cmd.Env = env
	return cmd
}
