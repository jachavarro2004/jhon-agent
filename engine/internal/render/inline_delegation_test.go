package render

import (
	"path/filepath"
	"strings"
	"testing"
)

// T218: inline mode delegates by size and risk. The organic rules (each
// runtime's full context, between the delegation markers), the LDD
// contract, the delegation contract and the lean text say the same thing:
// a change that is not small is applied by builders, slices sharing files
// one builder at a time; exploration past one read batch goes to the
// scout; delegating also when it is cheaper, more efficient or parallel.

// inlineForbidden are the inline rules T218 removed: each kept the work in
// the orchestrator's thread against S1-S3.
var inlineForbidden = []string{
	"never on price or size alone",
	"Work directly:",
	"Parts sharing files stay with one writer.",
	"small changes stay inline even when independent",
}

// organicRegion is the region between the delegation markers in text.
func organicRegion(t *testing.T, text string) string {
	t.Helper()
	if got := delegationRegionProblems(text); got != nil {
		t.Fatalf("delegation region: %v", got)
	}
	return text[strings.Index(text, delegationBegin) : strings.Index(text, delegationEnd)+len(delegationEnd)]
}

// S1-S3: in both runtimes' inline mode, the apply of a change that is not
// small goes to builders, slices sharing files one builder at a time,
// exploration past one read batch to the scout, and delegating also when it
// pays; no rule says to delegate never on price or size alone.
func TestInlineModeDelegatesBySizeAndRisk(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	for _, rt := range []string{"claude-code", "pi"} {
		full := e.fullText(rt)
		region := flatText(organicRegion(t, full))
		for _, w := range []string{
			"Plan here; build here only a small change (`jhon status` says so). Delegate:",
			"**Not small.** When `jhon status` or the change's route calls a change not small, builders apply it, a slice each; only a small change stays here.",
			"Slices that share files go to one builder at a time, in order; shared files are no reason to build here.",
			"**Exploration.** Reading past one bounded read batch goes to the scout.",
			"**When it pays.** Delegate also when it is cheaper, more efficient, or lets work run in parallel:",
			"**Context backstop.**",
			"**Independence.**",
		} {
			if !strings.Contains(region, w) {
				t.Errorf("%s's organic rules lack %q", rt, w)
			}
		}
		for _, text := range []string{flatText(full), flatText(e.leanText(rt))} {
			for _, gone := range inlineForbidden {
				if strings.Contains(text, gone) {
					t.Errorf("%s still says %q", rt, gone)
				}
			}
		}
	}
}

// S1-S3: the LDD contract's routes and the delegation contract say what the
// organic rules say.
func TestContractsDelegateBySizeAndRisk(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	ldd := flatText(mustReadText(filepath.Join(e.prose, "contracts", "ldd.md")))
	for _, w := range []string{
		"| explore, research | the `jhon-scout` agent, or inline within one bounded read batch |",
		"| apply | the `jhon-builder` agent, a slice each (slices that share files one at a time, in order); inline only for a small change",
	} {
		if !strings.Contains(ldd, w) {
			t.Errorf("contracts/ldd.md lacks %q", w)
		}
	}
	delegation := flatText(mustReadText(filepath.Join(e.prose, "contracts", "delegation.md")))
	for _, w := range []string{
		"**When to delegate.** A change `jhon status` (or its route) calls not small is applied by builders, a slice each; only a small change stays with the orchestrator.",
		"Slices that share files go to one builder at a time, in order; shared files are no reason to work inline.",
		"Exploration past one bounded read batch goes to the scout.",
		"Delegate also when it is cheaper, more efficient, or lets work run in parallel.",
		"In writer mode, its own rules bind instead.",
	} {
		if !strings.Contains(delegation, w) {
			t.Errorf("contracts/delegation.md lacks %q", w)
		}
	}
	for name, text := range map[string]string{"contracts/ldd.md": ldd, "contracts/delegation.md": delegation} {
		for _, gone := range inlineForbidden {
			if strings.Contains(text, gone) {
				t.Errorf("%s still says %q", name, gone)
			}
		}
	}
}

// S4: the always-on lean text says it in one line, in both runtimes.
func TestLeanTextSendsNotSmallWorkToBuilders(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	const line = "Once a change is not small, its slices go to builders and its exploration to the scout."
	for _, rt := range []string{"claude-code", "pi"} {
		if lean := flatText(e.leanText(rt)); strings.Count(lean, line) != 1 {
			t.Errorf("%s's lean text holds %q %d times, want once", rt, line, strings.Count(lean, line))
		}
	}
}

// writerRulesBeforeT218 is pi's writer-mode file as v0.4.0 (26bcb2b)
// renders it, byte for byte.
const writerRulesBeforeT218 = "**Writer mode.** You plan; builders write the code. Edit inline only a\n" +
	"change of at most ~20 lines, cheaper than starting a builder. The guard\n" +
	"sums changed lines across calls since the owner's last message. Reading\n" +
	"beyond one read batch goes into the builder's brief. Every other unit\n" +
	"goes to a builder with a self-contained brief: the spec lines, the exact\n" +
	"files and line ranges, the decisions already taken, and how to verify,\n" +
	"so it finishes in as few turns and attempts as possible.\n" +
	"A fix after a builder returns: continue it with `subagent_continue` and the id `subagent` returned; each slice gets a fresh\n" +
	"one. Never edit files a running sub-agent is working on. Units with\n" +
	"disjoint files launch together in one message; a writer beside another\n" +
	"is launched with `worktree: true`, a git worktree of its own beside the repository, on a new branch from HEAD. The plan doc, the Log and `odd/**` stay yours.\n" +
	"Every sub-agent and long job runs in the background.\n"

// S5: writer mode is unchanged: the rules the shell splices over the
// organic region are v0.4.0's, byte for byte.
func TestWriterModeRulesAreUnchanged(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	p := pi{e}
	if got := promptOf(t, p, p.piWriterFile()); got != writerRulesBeforeT218 {
		t.Errorf("pi's writer rules changed:\n%s", got)
	}
}
