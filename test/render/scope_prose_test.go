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
	// T146 FA3 (L1217): the cleanup rule is read on demand, on return.
	d := flat(support.Read(t, treePath(repoRoot, "contracts/delegation.md")))
	for file, ws := range map[string][]string{
		"orchestrator.md": {"Never migrate committed data or edit repo config the request did not name; report them.",
			"On return, read `contracts/delegation.md` before merging."},
		"contracts/delegation.md": {"remove each worktree (`git worktree remove <path>`), then its branch (`git branch -d <branch>`), one command each, never `&&`."},
	} {
		text := s
		if file != "orchestrator.md" {
			text = d
		}
		for _, w := range ws {
			if !strings.Contains(text, w) {
				t.Errorf("%s lacks %q", file, w)
			}
		}
	}
}
