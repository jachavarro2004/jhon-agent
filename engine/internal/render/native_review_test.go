package render

import (
	"strings"
	"testing"
)

// F3 (L1159): Claude Code's orchestrator runs `jhon review run` in the
// background, so the Bash tool's 2-minute default never cuts a review short,
// and acts on its notification; pi's names neither.
func TestNativeReviewRunsInTheBackgroundInClaudeCode(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	cc := flatText(e.bodyFor("claude-code", e.mainAgent(piMain)))
	for _, want := range []string{
		"one call to the native review (`jhon review run` as a Bash call with `run_in_background: true`",
		"the Bash tool's 2-minute default",
		"its notification",
	} {
		if !strings.Contains(cc, want) {
			t.Errorf("claude-code's orchestrator lacks %q", want)
		}
	}
	pi := flatText(e.bodyFor("pi", e.mainAgent(piMain)))
	from := strings.Index(pi, "A 4R is one call to the native review")
	to := strings.Index(pi[max(from, 0):], "only when it cannot run")
	if from < 0 || to < 0 {
		t.Fatal("pi's orchestrator has no native review sentence")
	}
	sentence := pi[from : from+to]
	for _, foreign := range []string{"run_in_background", "jhon review run", "Bash", "notification"} {
		if strings.Contains(sentence, foreign) {
			t.Errorf("pi's native review sentence names %q: %s", foreign, sentence)
		}
	}
}
