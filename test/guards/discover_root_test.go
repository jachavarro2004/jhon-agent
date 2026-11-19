// Project discovery stops at the git toplevel (L477, owner L786/L853): an
// openspec/ or ldd/ in a folder above a repository is not that
// repository's. route-guard and scope-guard judge the repository by its own
// layout; status sees no project there (test/ldd/native_layout_test.go).

package guards

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// innerRepo is a git repository holding src/app.py inside a folder holding
// files, and the guards' environment.
func innerRepo(t *testing.T, files map[string]string) (parent, inner string, env []string) {
	t.Helper()
	parent = realTemp(t)
	support.Write(t, parent, files)
	inner = mkdir(t, filepath.Join(parent, "inner"))
	support.Write(t, inner, map[string]string{"src/app.py": "X = 1\n"})
	support.GitInit(t, inner)
	support.Git(t, inner, "add", "-A")
	support.Git(t, inner, "commit", "-q", "-m", "base")
	return parent, inner, support.Env("XDG_STATE_HOME="+realTemp(t), "JHON_ROOT="+support.Root())
}

func TestRouteGuardIgnoresALayoutAboveTheRepository(t *testing.T) {
	t.Parallel()
	for name, files := range map[string]map[string]string{
		"openspec": {"openspec/config.yaml": "delivery: slice\n", "openspec/changes/c1/tasks.md": "## Intent\nDelivers: X\n"},
		"ldd":      {"ldd/config.yaml": "delivery: slice\n", "ldd/changes/c1/tasks.md": "## Intent\nDelivers: X\n"},
	} {
		t.Run(name, func(t *testing.T) {
			_, inner, env := innerRepo(t, files)
			commit := hook(t, env, "route-guard", nil, map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash",
				"tool_input": map[string]any{"command": "git commit -qm x"}, "cwd": inner, "session_id": "s"})
			if commit.Code != 2 || !strings.Contains(commit.Stderr, "LDD is not set up in this project") {
				t.Errorf("a commit in a plain repository under %s/: exit %d %s", name, commit.Code, commit.Stderr)
			}
			edit := hook(t, env, "route-guard", nil, map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Write",
				"tool_input": map[string]any{"file_path": filepath.Join(inner, "src", "app.py"), "content": "X = 2\n"}, "cwd": inner, "session_id": "s"})
			if edit.Code != 0 || adviceOf(t, edit.Stdout) != setUpNote() {
				t.Errorf("a code edit in a plain repository under %s/ runs with the set-up note: exit %d %s %s", name, edit.Code, edit.Stdout, edit.Stderr)
			}
		})
	}
}

// openspec/ at the repository's own root is its project: a PR without
// RED→GREEN is refused by the LDD rules, not by the set-up refusal.
func TestRouteGuardFindsTheLayoutAtTheGitRoot(t *testing.T) {
	t.Parallel()
	root := realTemp(t)
	support.Write(t, root, map[string]string{"openspec/config.yaml": "delivery: slice\n", "openspec/changes/c1/tasks.md": "## Intent\nDelivers: X is 2\n", "src/app.py": "X = 1\n"})
	support.GitInit(t, root)
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "base")
	support.Write(t, root, map[string]string{"src/app.py": "X = 2\n"})
	env := support.Env("XDG_STATE_HOME="+realTemp(t), "JHON_ROOT="+support.Root())
	pr := hook(t, env, "route-guard", nil, map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash",
		"tool_input": map[string]any{"command": "gh pr create --title x --body y"}, "cwd": filepath.Join(root, "src"), "session_id": "s"})
	if pr.Code != 2 || !strings.HasPrefix(pr.Stderr, "route-guard: ") || strings.Contains(pr.Stderr, "LDD is not set up") {
		t.Errorf("a PR without RED→GREEN in an openspec repository: exit %d %s", pr.Code, pr.Stderr)
	}
}

// A writer in a plain repository under a folder holding ldd/ is held to the
// repository's own scope: the folder's ldd/changes/ lends it no change
// directory.
func TestScopeGuardIgnoresALayoutAboveTheRepository(t *testing.T) {
	t.Parallel()
	_, inner, env := innerRepo(t, map[string]string{"ldd/config.yaml": "delivery: slice\n", "ldd/changes/c1/tasks.md": "## Intent\nDelivers: X\n"})
	prompt := "Apply task 1.1.\n\n## Allowed edit surfaces\n- src/a.py\n"
	write := func(rel string) support.Result {
		return hook(t, env, "scope-guard", []string{"--agent", "worker"}, map[string]any{"tool_name": "Write",
			"tool_input": map[string]any{"file_path": filepath.Join(inner, rel), "content": "x\n"}, "cwd": inner, "agent_type": "worker", "launch_prompt": prompt})
	}
	code(t, write("src/a.py"), 0, "a listed file")
	w := write("src/app.py")
	code(t, w, 2, "an unlisted file")
	matches(t, w.Stderr, `^scope-guard: Write .*inner/src/app\.py is outside the allowed edit surfaces \(src/a\.py\)`, "an unlisted file")
	code(t, write("ldd/changes/c1/tasks.md"), 2, "ldd/changes/ of the folder above is no change directory of the repository")
	code(t, write("openspec/changes/c1/tasks.md"), 0, "the repository's own change directory")
}
