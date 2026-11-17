package render

import (
	"slices"
	"testing"
)

// T134 H7a (L1019, L1021): worker, explore and verify hold pi's todo tool
// through the `todo` capability; Claude Code's allowlists do not change
// (its Task* list is the session's, which a sub-agent would share).
func TestExecutorsHoldTodoInPiOnly(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	seen := 0
	for _, a := range e.m.Agents {
		want := a.Name == "worker" || a.Name == "explore" || a.Name == "verify"
		if got := slices.Contains(e.piTools(a), "todo"); got != want {
			t.Errorf("%s: pi todo %v, want %v", a.Name, got, want)
		}
		if want {
			seen++
		}
		for _, tool := range e.claudeTools(a) {
			if tool == "todo" || tool == "TodoWrite" || (a.Mode != "primary" && slices.Contains([]string{"TaskCreate", "TaskUpdate", "TaskList"}, tool)) {
				t.Errorf("%s: Claude Code holds %s", a.Name, tool)
			}
		}
	}
	if seen != 3 {
		t.Fatalf("found %d of worker, explore and verify", seen)
	}
}
