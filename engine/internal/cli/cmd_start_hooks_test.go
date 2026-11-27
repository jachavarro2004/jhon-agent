package cli

import (
	"strings"
	"testing"
)

// T266: `jhon start --help` names --approve-hooks on both rows and says
// what the check does before a launch.
func TestStartHelpNamesTheLaunchCheck(t *testing.T) {
	_, out := capture(t, []string{"start", "--help"})
	text := strings.Join(strings.Fields(out), " ")
	for _, want := range []string{
		"jhon start [--approve-hooks] [<claude args>]",
		"jhon start pi [--approve-hooks] [<pi args>]",
		"before it launches, the hooks, extensions and MCP servers here are compared with what you last approved, and a change is shown and asked about (no terminal: nothing starts, exit 1); --approve-hooks approves it without asking",
		"the same check of what runs here, and the same --approve-hooks, as jhon start",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("jhon start --help lacks %q:\n%s", want, out)
		}
	}
}
