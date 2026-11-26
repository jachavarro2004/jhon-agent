package audit

import "testing"

// T263 verify (mwy4oq8v) item 2: every genuine stop still counts; glued words are data.
func TestT263ProbeGenuineStops(t *testing.T) {
	ck := "Checkpoint: 1.1 done (a.txt:4); 1.2 remains."
	u := func(agent string, c pcall) int {
		return outcomeOf(piAudit(t, pstart("tui"), puser("go"), psub(agent, sliceTask("1.1"), ck, c)), chg).Unfinished
	}
	scoutRead := "Read budget: 13 lookups, 120000 bytes without a CodeGraph call. codegraph_explore returns the source of the symbols it finds, already read: prefer it to more grep and read, and return path:line evidence, not raw output."
	eq(t, "empty output, then the notice", u("jhon-builder", pheard("true", "", piBudgetNotice)), 1)
	eq(t, "after a newline", u("jhon-builder", pheard("pytest", "3 passed\n", piBudgetNotice)), 1)
	eq(t, "on an error result", u("jhon-builder", pcall{name: "bash", args: obj("command", "false"), result: "exit 1", isError: true, appended: []string{piBudgetNotice}}), 1)
	eq(t, "then the scout's reminder", u("jhon-builder", pheard("rg x", "a:1:x", piBudgetNotice, scoutRead)), 1)
	eq(t, "a -worker", u("gentle-ai-worker", pheard("true", "ok", piBudgetNotice)), 1)
	eq(t, "data then the real notice", u("jhon-builder", pheard("cat n", piBudgetNotice, piBudgetNotice)), 1)
	eq(t, "glued, then the reminder", u("jhon-builder", pheard("cat n", "x\n"+piBudgetNotice, t263ReadBudget)), 0)
	eq(t, "notice then another note", u("jhon-builder", pheard("true", "ok", piBudgetNotice, "other")), 0)
	eq(t, "jd-fix-agent is no builder", u("jd-fix-agent", pheard("true", "ok", piBudgetNotice)), 0)
}

// T263 verify (mwy4oq8v) items 4 and 5: the last continue decides; a reload cuts a continue.
func TestT263ProbeLastContinueDecides(t *testing.T) {
	asked := []pstep{pstart("tui"), puser("go"), psub("jhon-builder", sliceTask("1.1"), pstarted("agent-1")),
		pagentNotice("agent-1", "jhon-builder", "stopped: which table?"), pcontinue("toolu_p02", "agent-2")}
	u := func(xs ...pstep) int {
		return outcomeOf(piAudit(t, append(append([]pstep{}, asked...), xs...)...), chg).Unfinished
	}
	eq(t, "answered, then cut", u(pagentNotice("agent-2", "jhon-builder", "partial"), pcontinue("toolu_p02", "agent-3")), 1)
	eq(t, "answered via task_result", u(pmain(pcall{name: "task_result", args: obj("id", "agent-2"), result: "Background sub-agent agent-2 (jhon-builder) completed after 1m.\n\nslice 1 done"})), 0)
	eq(t, "a reload cuts it", u(preload(), puser("go on"), pagentNotice("agent-2", "jhon-builder", "late")), 1)
}

// T263 verify advisory 1: the shell accepted agent names with capitals and
// dots (^[A-Za-z0-9][A-Za-z0-9_.-]*$), so the cap note matches them too;
// the <agent> placeholder still does not.
func TestT263CapNoteTakesEveryAgentNameTheShellAccepted(t *testing.T) {
	note := func(name string) string {
		return "[jhon: " + name + " stopped at its sub-agent context cap; relaunch it]"
	}
	for _, name := range []string{"jhon-builder", "Jhon_Builder2", "my.worker", "gentle-ai-worker"} {
		if !stoppedShort.MatchString(note(name)) {
			t.Errorf("the cap note of %q is not read as a stop", name)
		}
	}
	for _, name := range []string{"<agent>", "<worker>", ""} {
		if stoppedShort.MatchString(note(name)) {
			t.Errorf("the cap note of %q is read as a stop", name)
		}
	}
}
