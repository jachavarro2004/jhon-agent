package render

import (
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T106 (L578): the orchestrator never migrates committed data or edits repo
// config the request did not name, and removes a sub-agent's worktree and
// branch with single commands (a compound one is denied by Claude Code's
// permission prompt in headless runs, so `.claude/worktrees/agent-*` stayed).
func TestScopeAndWorktreeCleanupProse(t *testing.T) {
	s := flat(support.Read(t, treePath(repoRoot, "orchestrator.md")))
	for _, w := range []string{
		"Never migrate committed data or edit repo config the request did not name; report them.",
		"remove each worktree (`git worktree remove <path>`), then its branch (`git branch -d <branch>`), one command each, never `&&`.",
	} {
		if !strings.Contains(s, w) {
			t.Errorf("orchestrator.md lacks %q", w)
		}
	}
}
