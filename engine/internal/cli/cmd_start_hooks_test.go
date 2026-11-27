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
		"a folder never approved where something runs asks once at its first start, unless the seed of jhon sync, run before, recorded it",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("jhon start --help lacks %q:\n%s", want, out)
		}
	}
}

// T266, L1768: `jhon sync --help` says what the seed does, when it asks
// and when it does not.
func TestSyncHelpNamesTheHookSeed(t *testing.T) {
	_, out := capture(t, []string{"sync", "--help"})
	text := strings.Join(strings.Fields(out), " ")
	want := "Before it builds, on your terminal, it offers once to record what runs outside the sandbox in each folder jhon start never saw (each repository under sandbox.allow_write, with its worktrees, and each project with Jhon state), so their first start does not ask; it shows each folder and how many files run there, asks [y/N], and never asks again for a folder already recorded; with no terminal it skips that in one line, and inside the sandbox it never records"
	if !strings.Contains(text, want) {
		t.Errorf("jhon sync --help lacks %q:\n%s", want, out)
	}
}
