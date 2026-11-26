package audit

// T263: the follow-ups of T262's verify (odd/tasks/v0-1-release.md L1734).

import "testing"

// pheard is a builder's bash call whose result the shell appended notices
// to, each its own text part (shell/extensions/jhon.ts, the child budget's
// tool_result handler).
func pheard(command, output string, notices ...string) pcall {
	c := pbash(command, output)
	c.appended = notices
	return c
}

const t263ReadBudget = "Read budget: 13 lookups, 120000 bytes without CodeGraph. codegraph_explore returns the source of a symbol already read; read only the lines you change; prefer the read tool with offset/limit over cat/sed through bash."

// 1: the old cap note stops an agent only with a real agent's name in it;
// the placeholder a comment or the changelog quotes (`<agent>`) is none.
func TestT263CapNoteNamesARealAgent(t *testing.T) {
	quoted := "Done (a.txt:4). The audit reads `[jhon: <agent> stopped at its sub-agent context cap; …]` as a stop."
	x := outcomeOf(piAudit(t, pstart("tui"), puser("go"), psub("jhon-builder", sliceTask("1.1"), quoted),
		psub("jhon-builder", sliceTask("1.2"), "done")), chg)
	eq(t, "the placeholder quoted: relaunches", x.Relaunches, 0)
	eq(t, "the placeholder quoted: a stop", stopped(quoted), false)
	eq(t, "a retired agent's name: a stop", stopped("1.1 done\n\n[jhon: worker stopped at its sub-agent context cap; what remains goes to a fresh worker with a handoff, not subagent_continue]"), true)
}

// 2: only the notice the shell appended, its own text part after the
// tool's output, is one; the same words at the end of a tool's own output
// (echo -n, a file with no trailing newline) are data.
func TestT263BudgetNoticeIsItsOwnPart(t *testing.T) {
	checkpoint := "Checkpoint at the context notice: tasks 1.1-1.2 done (a.txt:4); 1.3 remains."
	unfinished := func(c pcall) int {
		return outcomeOf(piAudit(t, pstart("tui"), puser("go"), psub("jhon-builder", sliceTask("1.1"), checkpoint, c)), chg).Unfinished
	}
	eq(t, "the notice ending the tool's own output", unfinished(pbash("cat notes.txt", "3 passed\n"+piBudgetNotice)), 0)
	eq(t, "echo -n of the notice alone", unfinished(pbash("echo -n \"$NOTICE\"", piBudgetNotice)), 0)
	eq(t, "the shell's notice", unfinished(pheard("uv run pytest -q", "3 passed", piBudgetNotice)), 1)
	eq(t, "the shell's notice, then the read budget's", unfinished(pheard("rg -n x src", "a.txt:1:x", piBudgetNotice, t263ReadBudget)), 1)
	eq(t, "the read budget's alone", unfinished(pheard("rg -n x src", "a.txt:1:x", t263ReadBudget)), 0)
}

// 4: a /reload restarts the extension, which stops every task and hands the
// task ids out again from agent-1 (shell/lib/background-tasks-tools.ts
// session_shutdown): a task id from before it names nothing after it.
func TestT263ReloadForgetsTheEarlierTaskIDs(t *testing.T) {
	steps := []pstep{pstart("tui"), puser("go"),
		psub("jhon-builder", sliceTask("1.1"), pstarted("agent-1")),
		preload(), puser("go on"),
		pagentNotice("agent-1", "jhon-builder", "another agent's answer"),
	}
	eq(t, "deliveries", deliveries(t, piSessionWith(t, steps...)), []string{""})
	r := piAudit(t, steps...)
	eq(t, "unfinished", outcomeOf(r, chg).Unfinished, 1)
	eq(t, "resumes", r.Outcomes.Resumes, 0)
}

// 5: a continued sub-agent whose continued run never answered (pi closed,
// or the session resumed, before its answer) did not finish its part, even
// when its run before had answered.
func TestT263ContinueThatNeverAnsweredIsUnfinished(t *testing.T) {
	asked := []pstep{pstart("tui"), puser("go"),
		psub("jhon-builder", sliceTask("1.1"), pstarted("agent-1")), // toolu_p02
		pagentNotice("agent-1", "jhon-builder", "stopped: which table?"),
		pcontinue("toolu_p02", "agent-2"),
	}
	r := piAudit(t, asked...)
	eq(t, "cut when pi closed: unfinished", outcomeOf(r, chg).Unfinished, 1)
	eq(t, "cut when pi closed: every agent finished its part", agentsWorked(r), false)
	eq(t, "its earlier answer stays its delivery", deliveries(t, piSessionWith(t, asked...)), []string{"stopped: which table?"})

	resumed := append(append([]pstep{}, asked...), pstart("tui"), puser("go on"),
		psub("jhon-builder", sliceTask("1.1"), pstarted("agent-1")),
		pagentNotice("agent-1", "jhon-builder", "slice 1 done"))
	x := outcomeOf(piAudit(t, resumed...), chg)
	eq(t, "cut by a resume: unfinished", x.Unfinished, 1)
	eq(t, "a fresh launch after it: relaunches", x.Relaunches, 1)

	// A later continue that answered finishes it.
	again := append(append([]pstep{}, asked...), pcontinue("toolu_p02", "agent-3"), pagentNotice("agent-3", "jhon-builder", "slice 1 done"))
	a := piAudit(t, again...)
	eq(t, "answered on a later continue: unfinished", outcomeOf(a, chg).Unfinished, 0)
	eq(t, "answered on a later continue: every agent finished its part", agentsWorked(a), true)
}
