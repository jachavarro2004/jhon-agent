package audit

import (
	"math"
	"testing"
)

// What went right, each with its evidence (worked.go).

func TestWhatWorkedNamesEachSuccessWithItsEvidence(t *testing.T) {
	ev := evidence{checks: []readyCheck{
		{change: "c1", at: "T1", action: "merge", commit: "0123456789abcdef", tree: "t0", testedTree: "t9"},
		{change: "c1", at: "T2", action: "merge", commit: "fedcba9876543210", tree: "t1", testedTree: "t1"},
		{change: "c2", at: "T3", action: "deploy"},
	}}
	o := &Outcomes{Changes: []*ChangeOutcome{
		{Change: "c1", Agents: 3, Relaunches: 1, VerifyRuns: 2, Verdicts: [3]int{1, 1, 0},
			TestRuns: TestRuns{Count: 4, Failed: 2}, Record: &Record{Timestamp: "T4", Passed: true, Executions: 12, DurationMs: 90000}},
		{Change: "", Agents: 2},
	}}
	ldd := []ChangeLDD{{Change: "c1", Commits: 2}}
	guards := []GuardRefusal{{Guard: "safety-guard", LostMs: 2000}, {Guard: "safety-guard", LostMs: 5000}, {Guard: "route-guard", LostMs: math.NaN()}}
	got := whatWorked(ev, o, ldd, guards)
	want := []Worked{
		{"delivery", "c1", "merge ready", "jhon deliver check at T2 on fedcba98, the tested tree is HEAD's"},
		{"tests", "c1", "12 execution(s) passed on a committed tree", "execution record at T4, 2m"},
		{"tests", "c1", "2 failing run(s) were fixed before the last one passed", "4 run(s) in the session"},
		{"verify", "c1", "verify caught 1 failure(s), then passed", "1 of 2 verify run(s) passed"},
		{"agents", "c1", "every agent finished its part", "3 agent(s), none stopped before returning, 1 relaunch(es)"},
		{"records", "c1", "the tests ran only through jhon executions", "4 run(s), none by hand"},
		{"guards", "", "safety-guard stopped 2 call(s) and the agent went on", "the next tool call succeeded within 5s"},
	}
	if len(got) != len(want) {
		t.Fatalf("%d items, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("item %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

func TestWhatWorkedClaimsNothingWithoutEvidence(t *testing.T) {
	o := &Outcomes{Changes: []*ChangeOutcome{
		// A record that did not pass, a verify that never passed, an agent at its cap, tests by hand.
		{Change: "c1", Agents: 2, Unfinished: 1, VerifyRuns: 1, Verdicts: [3]int{0, 1, 0},
			TestRuns: TestRuns{Count: 1, Failed: 1}, Record: &Record{Passed: false}},
	}}
	ldd := []ChangeLDD{{Change: "c1", Commits: 2, ByHand: 1}}
	guards := []GuardRefusal{{Guard: "safety-guard", LostMs: math.NaN()}}
	if got := whatWorked(evidence{}, o, ldd, guards); len(got) != 0 {
		t.Errorf("claimed %+v", got)
	}
	if got := whatWorked(evidence{}, nil, nil, nil); got == nil || len(got) != 0 {
		t.Errorf("no outcomes: %#v, want an empty list", got)
	}
}

// claims is whether r claims what for change, and its evidence.
func claims(r *Result, change, what string) (string, bool) {
	for _, w := range r.Worked {
		if w.Change == change && w.What == what {
			return w.Evidence, true
		}
	}
	return "", false
}

// "The tests ran only through jhon executions" is said only when no whole
// suite ran by hand in any thread (T133): a sub-agent's hand run withdraws
// it, and a session with none keeps the line as it was.
func TestWhatWorkedRecordsLineCountsHandRunsInSubagents(t *testing.T) {
	const line = "the tests ran only through jhon executions"
	run := bash("jhon executions run "+chg, executionsOut(c1, t1, []string{"pass"}))
	handRun := auditOf(t, user("go"), phased("apply", delegate("o01", "jhon-builder", chg, run, bash("uv run pytest -q", "3 passed"))))
	if evidence, said := claims(handRun, chg, line); said {
		t.Errorf("a jhon-builder ran the whole suite by hand, yet the audit says %q (%s)", line, evidence)
	}
	clean := auditOf(t, user("go"), phased("apply", delegate("o01", "jhon-builder", chg, run, bash("uv run pytest tests/test_a.py -q", "1 passed"))))
	evidence, said := claims(clean, chg, line)
	eq(t, "no hand run in any thread: the line is said", said, true)
	eq(t, "its evidence, as it was", evidence, "1 run(s), none by hand")
}
