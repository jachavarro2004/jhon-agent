package audit

import (
	"fmt"
	"math"
	"slices"
)

// Worked is one thing the session did right, with what shows it: the
// audit's other half beside its findings. Kind is delivery, tests, verify,
// agents, records, or guards; Change is "" for the session.
type Worked struct {
	Kind     string
	Change   string
	What     string
	Evidence string
}

// whatWorked reads what went right from what the audit already measured:
// each change's ready delivery check, its passing execution record, its
// verify verdicts, its agents, and its records; then the guards an agent
// got past. The owner's authorizations have their own list (Result's).
// Nothing here is a judgement the findings do not also rest on: each item
// names its evidence.
func whatWorked(ev evidence, o *Outcomes, ldd []ChangeLDD, guards []GuardRefusal) []Worked {
	out := []Worked{}
	if o != nil {
		for _, c := range o.Changes {
			if c.Change != "" {
				out = append(out, changeWorked(ev, c, ldd)...)
			}
		}
	}
	type got struct {
		n      int
		lostMs float64
	}
	byGuard := map[string]*got{}
	var names []string
	for _, g := range guards {
		if math.IsNaN(g.LostMs) {
			continue
		}
		if byGuard[g.Guard] == nil {
			byGuard[g.Guard] = &got{}
			names = append(names, g.Guard)
		}
		byGuard[g.Guard].n++
		byGuard[g.Guard].lostMs = max(byGuard[g.Guard].lostMs, g.LostMs)
	}
	for _, name := range names {
		g := byGuard[name]
		out = append(out, Worked{"guards", "", fmt.Sprintf("%s stopped %d call(s) and the agent went on", name, g.n),
			"the next tool call succeeded within " + Duration(g.lostMs)})
	}
	return out
}

func changeWorked(ev evidence, c *ChangeOutcome, ldd []ChangeLDD) []Worked {
	var out []Worked
	add := func(kind, what, evidence string) { out = append(out, Worked{kind, c.Change, what, evidence}) }
	if i := lastIndex(ev.checks, func(k readyCheck) bool { return k.change == c.Change }); i >= 0 {
		k := ev.checks[i]
		evidence := "jhon deliver check at " + k.at
		if k.commit != "" {
			evidence += " on " + k.commit[:min(8, len(k.commit))]
		}
		if k.testedTree != "" && k.testedTree == k.tree {
			evidence += ", the tested tree is HEAD's"
		}
		add("delivery", k.action+" ready", evidence)
	}
	if r := c.Record; r != nil && r.Passed {
		add("tests", fmt.Sprintf("%d execution(s) passed on a committed tree", r.Executions),
			fmt.Sprintf("execution record at %s, %s", r.Timestamp, Duration(r.DurationMs)))
	}
	if t := c.TestRuns; t.Failed > 0 && c.Record != nil && c.Record.Passed {
		add("tests", fmt.Sprintf("%d failing run(s) were fixed before the last one passed", t.Failed), fmt.Sprintf("%d run(s) in the session", t.Count))
	}
	if pass, fail := c.Verdicts[0], c.Verdicts[1]; pass > 0 {
		what := "verify passed"
		if fail > 0 {
			what = fmt.Sprintf("verify caught %d failure(s), then passed", fail)
		}
		add("verify", what, fmt.Sprintf("%d of %d verify run(s) passed", pass, c.VerifyRuns))
	}
	if c.Agents > 0 && c.Unfinished == 0 {
		evidence := fmt.Sprintf("%d agent(s), none stopped before returning", c.Agents)
		if c.Relaunches > 0 {
			evidence += fmt.Sprintf(", %d relaunch(es)", c.Relaunches)
		}
		add("agents", "every agent finished its part", evidence)
	}
	if i := slices.IndexFunc(ldd, func(l ChangeLDD) bool { return l.Change == c.Change }); i >= 0 {
		l := ldd[i]
		if l.ByHand == 0 && c.TestRuns.Count > 0 {
			add("records", "the tests ran only through jhon executions", fmt.Sprintf("%d run(s), none by hand", c.TestRuns.Count))
		}
	}
	return out
}

// lastIndex is the index of the last x in xs that f holds for, or -1.
func lastIndex[T any](xs []T, f func(T) bool) int {
	for i := len(xs) - 1; i >= 0; i-- {
		if f(xs[i]) {
			return i
		}
	}
	return -1
}
