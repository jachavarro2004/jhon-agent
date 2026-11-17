package audit

import (
	"math"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// Each successful commit of the change counts (ldd.go commitsOf).
func TestCommitsOf(t *testing.T) {
	commit := pbash("git -C /repo commit -qm 'feat: slice'", "[feat 1a2b3c] feat: slice")
	for _, c := range []struct {
		what    string
		steps   []pstep
		commits int
	}{
		{"two slice commits", []pstep{pmain(commit), pmain(commit)}, 2},
		{"a refused commit is no commit", []pstep{pmain(pblocked("bash", obj("command", "git commit -m x"), psafety("git commit")))}, 0},
		{"a sub-agent's commit counts", []pstep{psub("worker", "slice 1", "done", commit)}, 1},
	} {
		s, errs := LoadSession(piSessionWith(t, c.steps...))
		if len(errs) > 0 {
			t.Fatalf("%s: %+v", c.what, errs)
		}
		if got := commitsOf(s, math.Inf(-1), math.Inf(1)); got != c.commits {
			t.Errorf("%s: %d commits, want %d", c.what, got, c.commits)
		}
	}
}

func TestTestsByHand(t *testing.T) {
	for c, want := range map[string]bool{
		"cd x && timeout 900 uv run --extra dev pytest tests/integration -m \"not migration\" -q": true,
		"uv run pytest":  true,
		"go test ./...":  true,
		"npx vitest run": true,
		// A focused TDD run: named files, a node, a filter.
		"cd x && timeout 600 uv run --extra dev pytest tests/integration/test_flow_schedules_a.py -q": false,
		"cd x && timeout 1500 uv run --extra dev pytest -q tests/a.py":                                false,
		"pytest tests/test_a.py::test_b":                                                              false,
		"python3 -m pytest -q --lf":                                                                   false,
		"pytest -k x":                                                                                 false,
		"go test ./internal/audit/ -run TestX":                                                        false,
		"npx vitest run src/a.test.ts":                                                                false,
		"jhon executions run c1 --affected origin/main 2>&1 | rg pass":                                false,
		"uv run ruff check src":                                                                       false,
		"rg -n pytest pyproject.toml":                                                                 false,
		"git commit -m 'run pytest'":                                                                  false,
	} {
		if got := testsByHand(c); got != want {
			t.Errorf("testsByHand(%q) = %v, want %v", c, got, want)
		}
	}
}

// byHand is the change's hand runs in r's LDD row.
func byHand(t *testing.T, r *Result, change string) int {
	t.Helper()
	l := lddOf(r.LDD, change)
	if l == nil {
		t.Fatalf("no LDD row for %s: %+v", change, r.LDD)
	}
	return l.ByHand
}

// A whole project's suite run by hand counts in whichever thread ran it
// (T133, guard.WholeSuiteByHand): a sub-agent's as the main session's. A
// sub-agent's focused run, or a directory's, while it iterates is none.
func TestByHandCountsAWholeSuiteRunInASubagent(t *testing.T) {
	run := bash("jhon executions run "+chg, executionsOut(c1, t1, []string{"pass"}))
	for _, c := range []struct {
		what   string
		calls  []tcall
		byHand int
	}{
		{"a worker's whole suite, three times", []tcall{run, bash("uv run pytest -q", "3 passed"), bash("cd svc && uv run pytest -m unit -q", "3 passed"), bash("go test ./...", "ok")}, 3},
		{"a worker's focused runs and a directory's", []tcall{run, bash("uv run pytest tests/test_a.py -q", "1 passed"), bash("uv run pytest tests/unit -q", "3 passed"), bash("go test ./internal/audit/ -run TestX", "ok")}, 0},
		{"only through jhon executions", []tcall{run}, 0},
	} {
		r := auditOf(t, user("go"), phased("apply", delegate("o01", "worker", chg, c.calls...)))
		eq(t, c.what, byHand(t, r, chg), c.byHand)
	}
	// Another change's worker running at the same time is not this change's.
	other := phased("apply", delegate("o02", "worker", "other-change", bash("uv run pytest -q", "3 passed")))
	r := auditOf(t, user("go"), mainBash("jhon status "+chg), other, mainBash("jhon executions run "+chg, executionsOut(c1, t1, []string{"pass"})))
	eq(t, "another change's worker", byHand(t, r, chg), 0)
}

// The main session's own count keeps its wider rule: a directory's suite
// counts there (guard.TestsByHand), a focused run does not.
func TestByHandKeepsTheMainSessionsRule(t *testing.T) {
	r := auditOf(t, user("go"), mainBash("jhon status "+chg), mainBash("uv run pytest tests/integration -q", "3 passed"),
		mainBash("uv run pytest tests/test_a.py -q", "1 passed"), mainBash("jhon executions run "+chg, executionsOut(c1, t1, []string{"pass"})))
	eq(t, "the main session's directory run", byHand(t, r, chg), 1)
}

// The count reaches the per-change numbers: the report's `tests_by_hand`,
// and the human line, which says it only when a suite ran by hand.
func TestReportStatesTestsByHand(t *testing.T) {
	run := bash("jhon executions run "+chg, executionsOut(c1, t1, []string{"pass"}))
	r := auditOf(t, user("go"), phased("apply", delegate("o01", "worker", chg, run, bash("uv run pytest -q", "3 passed"), bash("go test ./...", "ok"))))
	eq(t, "the report", js.Prop(changeValue(t, r, chg), "tests_by_hand"), any(2.0))
	if out := Human(r, "s.jsonl", false); !strings.Contains(out, "\n  ldd  "+chg+"  slices 0  owner messages 0  per slice -  tests by hand 2\n") {
		t.Errorf("the human output states no hand runs:\n%s", out)
	}
	none := auditOf(t, user("go"), phased("apply", delegate("o01", "worker", chg, run)))
	eq(t, "none: the report", js.Prop(changeValue(t, none, chg), "tests_by_hand"), any(0.0))
	if out := Human(none, "s.jsonl", false); !strings.Contains(out, "\n  ldd  "+chg+"  slices 0  owner messages 0  per slice -\n") {
		t.Errorf("none by hand: the human line changed:\n%s", out)
	}
}
