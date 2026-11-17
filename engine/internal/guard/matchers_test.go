package guard

import (
	"slices"
	"testing"
)

// route-guard acts on a Bash call (a commit, a PR, a whole suite, a write),
// an Agent launch (a bounded writer's scope, a reviewer), and a file write,
// which it refuses only as the first code edit in a repository with no LDD
// layout (S28, T89).
func TestRouteGuardJudgesWritesBashAndAgent(t *testing.T) {
	if got := ToolsOf("route-guard"); !slices.Equal(got, []string{"Edit", "MultiEdit", "Write", "NotebookEdit", "Bash", "Agent"}) {
		t.Errorf("route-guard tools: %q", got)
	}
	if got := MatcherOf("route-guard"); got != "Edit|MultiEdit|Write|NotebookEdit|Bash|Agent" {
		t.Errorf("route-guard matcher: %q", got)
	}
}
