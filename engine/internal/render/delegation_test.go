package render

import (
	"path/filepath"
	"strings"
	"testing"
)

// T117b (L720): pi's prompt marks the organic delegation rules once, begin
// before end, and pi renders the writer-mode rules beside the prompt in
// pi's words, which the shell splices over that region.
func TestWriterModeRulesReplaceAMarkedRegion(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	p := pi{e}
	prompt := promptOf(t, p, p.piPromptFile())
	if got := delegationRegionProblems(prompt); got != nil {
		t.Fatalf("pi's prompt: %v", got)
	}
	begin, end := strings.Index(prompt, delegationBegin), strings.Index(prompt, delegationEnd)
	organic := prompt[begin : end+len(delegationEnd)]
	for _, w := range []string{"Work directly:", "**Parallel work.**", "A fix needed after a builder returns goes to a builder"} {
		if !strings.Contains(organic, w) {
			t.Errorf("the organic region lacks %q", w)
		}
	}
	if strings.Contains(organic, "**Background sub-agents.**") {
		t.Error("the background rules sit inside the region writer mode replaces")
	}

	writer := promptOf(t, p, p.piWriterFile())
	if p.piWriterFile() != filepath.Join(p.piHome(), "jhon-orchestrator-writer.md") {
		t.Errorf("writer file at %s", p.piWriterFile())
	}
	flat := flatText(writer)
	for _, w := range []string{
		"Edit inline only a change of at most ~20 lines, cheaper than starting a builder.",
		"a self-contained brief: the spec lines, the exact files and line ranges, the decisions already taken, and how to verify, so it finishes in as few turns and attempts as possible.",
		"A fix after a builder returns: continue it with `subagent_continue`",
		"Never edit files a running sub-agent is working on.",
		"Units with disjoint files launch together in one message;",
		"The plan doc, the Log and `odd/**` stay yours.",
		"Every sub-agent and long job runs in the background.",
	} {
		if !strings.Contains(flat, w) {
			t.Errorf("the writer rules lack %q", w)
		}
	}
	if strings.Contains(writer, "{{runtime:") || strings.Contains(writer, "<!-- delegation:") {
		t.Errorf("the writer rules carry a placeholder or a marker:\n%s", writer)
	}
	if len(writer) > len(organic) {
		t.Errorf("the writer rules are %d bytes, the organic region %d", len(writer), len(organic))
	}
	t.Logf("organic region %d bytes, writer rules %d bytes", len(organic), len(writer))
}

func TestDelegationRegionProblems(t *testing.T) {
	for text, want := range map[string]string{
		delegationBegin + "x" + delegationEnd:                 "",
		"x" + delegationEnd:                                   delegationBegin + " appears 0 times, want 1",
		delegationBegin + delegationBegin + delegationEnd:     delegationBegin + " appears 2 times, want 1",
		delegationEnd + "x" + delegationBegin:                 delegationEnd + " comes before " + delegationBegin,
		delegationBegin + "x" + delegationEnd + delegationEnd: delegationEnd + " appears 2 times, want 1",
	} {
		if got := strings.Join(delegationRegionProblems(text), "; "); got != want {
			t.Errorf("%q: got %q, want %q", text, got, want)
		}
	}
}
