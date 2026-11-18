package render

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// renderedOrchestrators is the orchestrator each runtime renders, flattened.
func renderedOrchestrators(t *testing.T, e *engine) map[string]string {
	t.Helper()
	// T146h: the orchestrator of a change that is not small is each
	// runtime's full file; pi's main prompt is still that text.
	return map[string]string{"pi": flatText(promptOf(t, pi{e}, pi{e}.piPromptFile())), "claude-code": flatText(e.fullText("claude-code"))}
}

// T146 FA3b (L1217, L1218): every rule moved out of the fixed context is in
// its contract, out of the fixed context, and pointed to from the rendered
// orchestrator of both runtimes; a contract read on demand names no
// runtime term.
func TestOnDemandContractsHoldTheMovedRules(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	if p := e.onDemandProblems(); len(p) > 0 {
		t.Errorf("on-demand problems:\n%v", p)
	}
	rendered := renderedOrchestrators(t, e)
	for _, c := range onDemandContracts {
		if len(c.pointers) == 0 || len(c.rules) == 0 {
			t.Errorf("%s lists no pointer or no rule", c.file)
		}
		for _, p := range c.pointers {
			for _, rt := range []string{"claude-code", "pi"} {
				if !strings.Contains(rendered[rt], e.atInstall(p.text)) {
					t.Errorf("%s's rendered orchestrator lacks the pointer to %s: %q", rt, c.file, p.text)
				}
			}
			if !strings.Contains(p.text, c.file) && !strings.Contains(p.text, "contracts/<name>.md") {
				t.Errorf("pointer %q does not name %s", p.text, c.file)
			}
			if p.in != "orchestrator.md" && !slices.Contains(alwaysOn(), p.in) {
				t.Errorf("pointer %q lives in %s, neither the orchestrator nor an always-on contract", p.text, p.in)
			}
		}
	}
}

// T146c K3 (L1222): a pointer names the contract at the install's real
// path, the renderer's `current`, on both runtimes; never the literal
// `~/.local/share/jhon/current`.
func TestOnDemandPointersNameTheInstallPath(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	root := filepath.Join(e.current(), ProseDir, "contracts") + "/"
	for rt, got := range renderedOrchestrators(t, e) {
		for _, w := range []string{
			"`contracts/<name>.md` here is `" + root + "<name>.md`, binding once its trigger applies.",
			"read `" + root + "asked-operations.md`.",
			"read `" + root + "witness.md`.",
		} {
			if !strings.Contains(got, w) {
				t.Errorf("%s's orchestrator does not name the install's path: want %q", rt, w)
			}
		}
		if strings.Contains(got, contractsRoot) {
			t.Errorf("%s's orchestrator still names %s", rt, contractsRoot)
		}
	}
}

// T146c K2 (L1222): the ambient-credential and owner-act rules are always
// on, for the orchestrator and every sub-agent of both runtimes.
func TestAmbientCredentialsAreAlwaysOn(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	for _, rt := range []string{"claude-code", "pi"} {
		for _, primary := range []bool{true, false} {
			got := flatText(e.alwaysOnSection(rt, primary))
			for _, w := range []string{
				"Ambient SSH agents, sockets, credentials, and sessions are not permission;",
				"no wrapper or delegate widens the authorized scope;",
				"grants and modes are the owner's act alone.",
			} {
				if !strings.Contains(got, w) {
					t.Errorf("%s's always-on layer (primary %v) lacks %q", rt, primary, w)
				}
			}
		}
	}
}

// onDemandProblem reports whether problems hold one against file (relative
// to prose/) whose message contains msg.
func hasOnDemandProblem(e *engine, problems []problem, file, msg string) bool {
	return slices.ContainsFunc(problems, func(p problem) bool {
		return p.file == filepath.Join(e.prose, file) && p.rule == "on-demand" && strings.Contains(p.msg, msg)
	})
}

// A moved rule its contract no longer states, or one back in the fixed
// context, is a validation problem against the prose file at fault; a
// missing pointer names where it lives.
func TestOnDemandProblemsReportALostOrDuplicatedRule(t *testing.T) {
	saved := onDemandContracts
	defer func() { onDemandContracts = saved }()
	e := engineAt(t, repo, "--validate")
	onDemandContracts = []onDemandContract{{
		file:     "contracts/delivery.md",
		pointers: []pointer{{"orchestrator.md", "read `contracts/nowhere.md`"}, {"contracts/remote-auth.md", "read `" + contractsRoot + "nowhere.md`"}},
		rules:    []string{"a sentence no contract states", "**Continue.** Never end a turn announcing a next step that is already authorized"},
	}}
	got := e.onDemandProblems()
	for _, want := range []struct{ file, msg string }{
		{"contracts/delivery.md", `contracts/delivery.md does not state "a sentence no contract states"`},
		{"orchestrator.md", "orchestrator.md does not hold the pointer to contracts/delivery.md"},
		{"orchestrator.md", "claude-code's orchestrator (orchestrator.md) does not point to contracts/delivery.md"},
		{"orchestrator.md", "pi's orchestrator (orchestrator.md) does not point to contracts/delivery.md"},
		{"contracts/remote-auth.md", "pi's always-on contracts/remote-auth.md does not point to contracts/delivery.md"},
		{"orchestrator.md", `claude-code's fixed context still states "**Continue.**`},
		{"orchestrator.md", `pi's fixed context still states "**Continue.**`},
	} {
		if !hasOnDemandProblem(e, got, want.file, want.msg) {
			t.Errorf("want against %s %q among:\n%v", want.file, want.msg, got)
		}
	}
	// Rule 31d reports them against that file, not the registry.
	if v := e.validate(); !slices.ContainsFunc(v, func(p problem) bool {
		return p.file == "contracts/delivery.md" && p.rule == "on-demand" && strings.Contains(p.msg, "a sentence no contract states")
	}) {
		t.Errorf("validate does not report the lost rule against contracts/delivery.md:\n%v", v)
	}
}

// proseCopy is a copy of the repository's prose/ the test may change.
func proseCopy(t *testing.T, e *engine) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ProseDir)
	if err := os.CopyFS(dir, os.DirFS(e.prose)); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A contract the registry names that is gone is reported against it.
func TestOnDemandProblemsReportAMissingContract(t *testing.T) {
	saved := onDemandContracts
	defer func() { onDemandContracts = saved }()
	e := engineAt(t, repo, "--validate")
	onDemandContracts = []onDemandContract{{
		file:     "contracts/nowhere.md",
		pointers: []pointer{{"orchestrator.md", "read `contracts/delivery.md`"}},
		rules:    []string{"anything"},
	}}
	if got := e.onDemandProblems(); !hasOnDemandProblem(e, got, "contracts/nowhere.md", "contracts/nowhere.md is missing") {
		t.Errorf("want contracts/nowhere.md is missing among:\n%v", got)
	}
}

// A contract read on demand is read raw: a runtime term in it is reported
// against it.
func TestOnDemandProblemsReportARuntimeTermInAContract(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	e.prose = proseCopy(t, e)
	path := filepath.Join(e.prose, "contracts/witness.md")
	if err := os.WriteFile(path, []byte(mustReadText(path)+"\nAsk {{runtime:grant-how}}.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := e.onDemandProblems(); !hasOnDemandProblem(e, got, "contracts/witness.md", "contracts/witness.md is read raw, yet names runtime term {{runtime:grant-how}}") {
		t.Errorf("want the runtime term reported against contracts/witness.md among:\n%v", got)
	}
}
