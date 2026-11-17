package render

import (
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T115g (L705): no orchestrator turn exists only for bookkeeping. Memory is
// saved once per completed request, batched with other tool calls; a todo
// update, a status check, or a Log append rides with the work call it
// serves; background work is never polled (L716: every job, sub-agent and
// sleep goes in the background, and the orchestrator never waits idly).
func TestBookkeepingRidesWithWork(t *testing.T) {
	read := func(p string) string { return flat(support.Read(t, treePath(repoRoot, p))) }
	wants := map[string][]string{
		"orchestrator.md": {
			"**No bookkeeping turns.** A memory call, todo update, status check, or Log append rides with the work call it serves, never alone.",
			"`mem_review` (`list`) when available, with the first reads;",
			// L716: every job, sub-agent and sleep goes in the background.
			"Every job that may outlast a few seconds runs in the background too (`bash_background` or `monitor`; Bash with `run_in_background: true` or `Monitor`), its wait condition inside the command; never `sleep` in the foreground.",
			"Never wait idly: do other in-scope work, or end the turn and resume on the notice.",
		},
		"contracts/engram.md": {
			"Save once per completed request, batched with other tool calls, never per fix or event, and before the final answer;",
		},
		"contracts/routing.md": {
			"Update it only with the work it describes.",
			// T134 H7c (L1019): the todo list is the agent's own, never the change's tasks.
			"For multi-step work keep a todo list of your own next steps for this request, started before the first edit; a change's tasks stay in `jhon status` and its tasks.md, never copied into it.",
		},
	}
	gone := map[string][]string{
		"contracts/routing.md": {"The todo list may still hold quick sub-steps.", "that file is the list", "never copy its tasks into the todo list"},
		"contracts/engram.md":  {"Save before writing the final answer"},
		"orchestrator.md":      {"then `mem_context`, and"},
	}
	for p, ws := range wants {
		s := read(p)
		for _, w := range ws {
			if !strings.Contains(s, w) {
				t.Errorf("%s lacks %q", p, w)
			}
		}
	}
	for p, ws := range gone {
		s := read(p)
		for _, w := range ws {
			if strings.Contains(s, w) {
				t.Errorf("%s still says %q", p, w)
			}
		}
	}
}
