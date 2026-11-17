package render

import (
	"slices"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
)

// The agent vcs-guard bounds (guard.ExploreAgent, T123) is agents.yaml's,
// and it carries the write tools the guard judges in both runtimes.
func TestExploreAgentCarriesWriteTools(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	for _, a := range e.m.Agents {
		if a.Name != guard.ExploreAgent {
			continue
		}
		cc, pi := e.claudeTools(a), e.piTools(a)
		if !slices.Contains(cc, "Edit") || !slices.Contains(cc, "Write") || !slices.Contains(pi, "edit") || !slices.Contains(pi, "write") {
			t.Fatalf("%s: Claude Code %v, pi %v", a.Name, cc, pi)
		}
		return
	}
	t.Fatalf("prose/agents.yaml declares no agent %q", guard.ExploreAgent)
}
