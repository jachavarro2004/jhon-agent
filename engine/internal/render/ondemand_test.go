package render

import (
	"path/filepath"
	"strings"
	"testing"
)

// T146 FA3b (L1217, L1218): every rule moved out of the fixed context is in
// its contract, out of the fixed context, and pointed to from the rendered
// orchestrator of both runtimes; a contract read on demand names no
// runtime term.
func TestOnDemandContractsHoldTheMovedRules(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	if p := e.onDemandProblems(); len(p) > 0 {
		t.Errorf("on-demand problems:\n%s", strings.Join(p, "\n"))
	}
	rendered := map[string]string{"pi": flatText(promptOf(t, pi{e}, pi{e}.piPromptFile()))}
	for _, r := range e.renderers() {
		if r.Runtime() == "claude-code" {
			rendered["claude-code"] = flatText(promptOf(t, r, filepath.Join(e.claudeAgents(), "jhon-orchestrator.md")))
		}
	}
	for _, c := range onDemandContracts {
		if len(c.pointers) == 0 || len(c.rules) == 0 {
			t.Errorf("%s lists no pointer or no rule", c.file)
		}
		for _, rt := range []string{"claude-code", "pi"} {
			for _, p := range c.pointers {
				if !strings.Contains(rendered[rt], p) {
					t.Errorf("%s's rendered orchestrator lacks the pointer to %s: %q", rt, c.file, p)
				}
			}
		}
		for _, p := range c.pointers {
			if !strings.Contains(p, c.file) && !strings.Contains(p, "contracts/<name>.md") {
				t.Errorf("pointer %q does not name %s", p, c.file)
			}
		}
	}
}

// A moved rule its contract no longer states, or one back in the fixed
// context, is a validation problem.
func TestOnDemandProblemsReportALostOrDuplicatedRule(t *testing.T) {
	saved := onDemandContracts
	defer func() { onDemandContracts = saved }()
	e := engineAt(t, repo, "--validate")
	onDemandContracts = []onDemandContract{{
		file:     "contracts/delivery.md",
		pointers: []string{"read `contracts/nowhere.md`"},
		rules:    []string{"a sentence no contract states", "**Continue.** Never end a turn announcing a next step that is already authorized"},
	}}
	got := strings.Join(e.onDemandProblems(), "\n")
	for _, want := range []string{
		`contracts/delivery.md does not state "a sentence no contract states"`,
		"claude-code's orchestrator does not point to contracts/delivery.md",
		"pi's orchestrator does not point to contracts/delivery.md",
		`claude-code's fixed context still states "**Continue.**`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q among:\n%s", want, got)
		}
	}
}
