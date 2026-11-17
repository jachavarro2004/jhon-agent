package ldd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// deliver check reads the same root as status (L786, L853): in a plain
// repository under a folder holding openspec/, the folder's change is not
// the repository's; a linked worktree inside a checkout whose openspec/ is
// untracked carries no layout of its own and sees no change either.
func TestDeliverAndWorktreeIgnoreOpenSpecAboveTheRepository(t *testing.T) {
	parent := temp(t)
	support.Write(t, parent, map[string]string{"openspec/config.yaml": "delivery: slice\n", "openspec/changes/c1/tasks.md": "## Intent\nDelivers: X\n"})
	inner := filepath.Join(parent, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	support.GitRepo(t, inner)
	env := support.Env("XDG_STATE_HOME=" + temp(t))
	r := support.Jhon(t, inner, env, "deliver", "check", "c1")
	if r.Code != 2 || !strings.Contains(r.Stderr, "is not an active change under "+inner) {
		t.Errorf("deliver check c1 in the inner repository: exit %d, want 2\n%s%s", r.Code, r.Stdout, r.Stderr)
	}
	main := filepath.Join(temp(t), "main")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	support.GitRepo(t, main)
	support.Write(t, main, map[string]string{"openspec/config.yaml": "delivery: slice\n", "openspec/changes/c1/tasks.md": "## Intent\nDelivers: X\n"})
	wt := filepath.Join(main, ".claude", "worktrees", "agent-x")
	support.Git(t, main, "worktree", "add", "-q", "-b", "c1", wt)
	if s := support.Jhon(t, wt, env, "status", "--json"); strings.Contains(s.Stdout, `"change": "c1"`) {
		t.Errorf("a worktree inside a checkout with an untracked openspec/ found the checkout's change: %s", s.Stdout)
	}
	if s := support.Jhon(t, main, env, "status", "--json"); !strings.Contains(s.Stdout, `"change": "c1"`) {
		t.Errorf("the checkout itself lost its change: %s%s", s.Stdout, s.Stderr)
	}
}
