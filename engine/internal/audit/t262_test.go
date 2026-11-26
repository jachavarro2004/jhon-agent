package audit

// T262: a pi session's background sub-agents across a resume and a
// subagent_continue, and a sub-agent that stopped at its context budget.

import (
	"slices"
	"strings"
	"testing"
)

const t262Started = " Its answer arrives as a message when it finishes; task_result reads it, task_stop cancels it."

func pstarted(task string) string {
	return "Started background sub-agent " + task + " (jhon-builder)." + t262Started
}

// pcontinue is the main session's subagent_continue of the child saved as
// child, which the shell ran in the background as task
// (shell/extensions/jhon.ts).
func pcontinue(child, task string) pstep {
	return pmain(pcall{name: "subagent_continue", args: obj("id", child, "message", "Go on where you stopped."),
		result: "Continued sub-agent " + child + " (jhon-builder) in the background as " + task + "." + t262Started})
}

// pcontinued is a continue's message in a child's session: its run before
// ended, and the message starts the next one.
func pcontinued(message string) pcall { return pcall{result: message} }

func sliceTask(n string) string {
	return "Apply openspec/changes/" + chg + "/ task " + n + ".\n\n## Allowed edit surfaces\n- a.txt"
}

// deliveries is each sub-agent's delivered result, "" for none, in launch order.
func deliveries(t *testing.T, path string) []string {
	t.Helper()
	s, errs := LoadSession(path)
	if len(errs) > 0 {
		t.Fatalf("audit-error: %+v", errs)
	}
	var out []string
	for _, a := range s.Subagents {
		_, result := completion(s, a)
		out = append(out, result)
	}
	return out
}

func agentsWorked(r *Result) bool {
	return slices.ContainsFunc(r.Worked, func(w Worked) bool { return w.Kind == "agents" && w.Change == chg })
}

// S1: the shell hands task ids out again after a resume, and a
// subagent_continue's answer arrives under the task id the continue
// started; it delivers the continued child's launch, never the earlier
// launch that had the same task id.
func TestPiContinueAfterAResumeDeliversTheContinuedLaunch(t *testing.T) {
	steps := []pstep{pstart("tui"), puser("go"),
		psub("jhon-builder", sliceTask("1.1"), pstarted("agent-1")), // toolu_p02
		pagentNotice("agent-1", "jhon-builder", "slice 1 done"),
		psub("jhon-builder", sliceTask("1.2"), pstarted("agent-2")), // toolu_p04, cut when pi closed
		pstart("tui"), puser("go on"),
		pcontinue("toolu_p04", "agent-1"),
		pagentNotice("agent-1", "jhon-builder", "slice 2 done"),
	}
	eq(t, "deliveries", deliveries(t, piSessionWith(t, steps...)), []string{"slice 1 done", "slice 2 done"})
	r := piAudit(t, steps...)
	eq(t, "unfinished", outcomeOf(r, chg).Unfinished, 0)
	eq(t, "every agent finished its part", agentsWorked(r), true)
}

// S1: a continue in the same sitting delivers its child's launch too.
func TestPiContinueDeliversTheContinuedLaunch(t *testing.T) {
	path := piSessionWith(t, pstart("tui"), puser("go"),
		psub("jhon-builder", sliceTask("1.1"), pstarted("agent-1")), // toolu_p02
		pagentNotice("agent-1", "jhon-builder", "stopped: which table?"),
		pcontinue("toolu_p02", "agent-2"),
		pagentNotice("agent-2", "jhon-builder", "slice 1 done"))
	eq(t, "deliveries", deliveries(t, path), []string{"slice 1 done"})
}

// S1: a task id from before a resume names nothing after it: a notice of
// that id with no launch in the new sitting delivers no earlier launch.
func TestPiResumeForgetsTheEarlierTaskIDs(t *testing.T) {
	steps := []pstep{pstart("tui"), puser("go"),
		psub("jhon-builder", sliceTask("1.1"), pstarted("agent-1")),
		pstart("tui"), puser("go on"),
		pagentNotice("agent-1", "jhon-builder", "another agent's answer"),
	}
	eq(t, "deliveries", deliveries(t, piSessionWith(t, steps...)), []string{""})
	eq(t, "unfinished", outcomeOf(piAudit(t, steps...), chg).Unfinished, 1)
	// A /reload is no resume: the same sitting's ids stand.
	reload := []pstep{pstart("tui"), puser("go"), psub("jhon-builder", sliceTask("1.1"), pstarted("agent-1")),
		preload(), pagentNotice("agent-1", "jhon-builder", "slice 1 done")}
	eq(t, "after a reload", deliveries(t, piSessionWith(t, reload...)), []string{"slice 1 done"})
}

// piBudgetNotice is the shell's notice a pi child hears past its context
// budget (shell/lib/child-budget.ts childBudgetNotice), riding a tool
// result's content as its own text part, which the reader joins to the output.
const piBudgetNotice = "Context budget: this sub-agent's context is at 91K tokens, past its 80K-token budget. Wrap up: finish the step in hand, then return what you found as path:line evidence and decisions, not raw output, and what remains."

// S2: a sub-agent that stopped at its context budget did not finish its
// part: it is unfinished, and a fresh launch after it is a relaunch.
func TestPiAgentAtItsContextBudgetIsUnfinished(t *testing.T) {
	heard := pbash("uv run pytest -q", "3 passed"+piBudgetNotice)
	checkpoint := "Checkpoint at the context notice: tasks 1.1-1.2 done (a.txt:4); 1.3 remains."
	x := outcomeOf(piAudit(t, pstart("tui"), puser("go"), psub("jhon-builder", sliceTask("1.1"), checkpoint, heard),
		psub("jhon-builder", sliceTask("1.3"), "done")), chg)
	eq(t, "unfinished", x.Unfinished, 1)
	eq(t, "relaunches", x.Relaunches, 1)

	// The notice's source quoted in a tool result is no notice.
	quoted := pbash("rg -n 'Context budget' shell/lib", "child-budget.ts:73:  return `Context budget: this sub-agent's context is at ${k(tokens)}K tokens, past its ${k(threshold)}K-token budget.`")
	q := piAudit(t, pstart("tui"), puser("go"), psub("jhon-builder", sliceTask("1.1"), "done", quoted))
	eq(t, "a quoted notice: unfinished", outcomeOf(q, chg).Unfinished, 0)
	eq(t, "a quoted notice: worked", agentsWorked(q), true)

	// A continue after the notice is a new run: one that then finished is no stop.
	c := outcomeOf(piAudit(t, pstart("tui"), puser("go"),
		psub("jhon-builder", sliceTask("1.1"), "done", heard, pcontinued("Go on with 1.3."), pbash("uv run pytest -q", "4 passed"))), chg)
	eq(t, "continued after the notice: unfinished", c.Unfinished, 0)
}

// S2: before T155 (shell/lib/child-budget.ts at 3118fbc) a child past its
// context cap had its calls refused, `jhon: your context is at …K tokens,
// past the …K-token sub-agent context cap. Call no more tools: …`, and the
// parent's result ended `[jhon: <agent> stopped at its sub-agent context
// cap; …]`: a stop as the turn cap's is.
func TestPiAgentAtItsOldContextCapIsUnfinished(t *testing.T) {
	refused := pblocked("bash", obj("command", "uv run pytest"), "jhon: your context is at 151K tokens, past the 150K-token sub-agent context cap. Call no more tools: reply now with what is done (path:line evidence), what remains, and the decisions the next agent needs; a fresh agent continues from there")
	line := "\n\n[jhon: jhon-builder stopped at its sub-agent context cap; what remains goes to a fresh jhon-builder with a handoff, not subagent_continue]"
	x := outcomeOf(piAudit(t, pstart("tui"), puser("go"), psub("jhon-builder", sliceTask("1.1"), "1.1 done (a.txt:4)"+line, refused),
		psub("jhon-builder", sliceTask("1.2"), "done")), chg)
	eq(t, "unfinished", x.Unfinished, 1)
	eq(t, "relaunches", x.Relaunches, 1)
	eq(t, "the result's line is a stop", stopped("1.1 done"+line), true)

	report := testRoot + "/openspec/changes/" + chg + "/verify-report.md"
	write := pcall{name: "write", args: obj("path", report, "content", "# Verify\n\nverdict: fail\n\nChecking."), result: "written"}
	v := outcomeOf(piAudit(t, pstart("tui"), puser("go"), psub("verify", "Verify openspec/changes/"+chg+"/ against its specs", "cut"+line, write, refused)), chg)
	eq(t, "a verify cut at the cap: no verdict", v.Verdicts, [3]int{0, 0, 1})
}

// The fixture test/testdata/audit/pi-resume-continue: a synthetic pi session
// shaped as the owner's (no private content). Sitting 1 launches builders
// A (agent-1, delivered) and B (agent-2, cut when pi closed); after the
// resume, B is continued in the background as agent-1, and builder D runs
// as agent-2 and stops at its context budget. Each call id holds a `|`,
// which the child's session id cannot, so B is found through its meta
// file's toolUseId.
func TestPiResumeAndContinueFixture(t *testing.T) {
	path := fixture(t, "pi-resume-continue")
	got := deliveries(t, path)
	if len(got) != 3 {
		t.Fatalf("deliveries = %q", got)
	}
	eq(t, "A's answer", strings.HasPrefix(got[0], "Slice 1 done"), true)
	eq(t, "B's continued answer", strings.HasPrefix(got[1], "Slice 2 done"), true)
	eq(t, "D's checkpoint", strings.HasPrefix(got[2], "Checkpoint at the context notice"), true)
	r := Audit(path)
	if len(r.Errors) > 0 {
		t.Fatalf("audit-error: %+v", r.Errors)
	}
	o := outcomeOf(r, "monthly-report")
	eq(t, "agents", o.Agents, 3)
	eq(t, "unfinished: D, at its budget", o.Unfinished, 1)
	eq(t, "relaunches", o.Relaunches, 0)
	eq(t, "resumes", r.Outcomes.Resumes, 1)
}
