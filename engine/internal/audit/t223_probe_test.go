package audit

import "testing"

// T223 L4 (verify A1): archive-without-red reads RED and GREEN wherever the
// session ran them, the main thread or any sub-agent, and a run whose
// summary says pass but that tested nothing (`0 of 0 passed`, an affected
// run that selected no project) is no GREEN.

func TestArchiveWithoutRedSubAgentEdges(t *testing.T) {
	archive := "jhon archive " + chg + " 2026-09-30"
	red := "jhon executions run " + chg + " --affected main 2>&1 | tail -n 2"
	green := "jhon executions run " + chg + " --affected 2>&1 | tail -n 1"
	// RED in one builder, GREEN in another, the archive in the main thread.
	if fs := findingsOf(t, "archive-without-red", user("go"),
		delegate("w01", "jhon-builder", chg, bash(red, "<== p: exit 1\n"+redSummary)),
		delegate("w02", "jhon-builder", chg, bash(green, greenSummary)),
		mainBash(archive, "readback: empty")); len(fs) != 0 {
		t.Errorf("RED and GREEN in two builders: %+v", fs)
	}
	// RED in the main thread, GREEN in a builder.
	if fs := findingsOf(t, "archive-without-red", user("go"),
		mainBash(red, "<== p: exit 1\n"+redSummary),
		delegate("w01", "jhon-builder", chg, bash(green, greenSummary)),
		mainBash(archive, "readback: empty")); len(fs) != 0 {
		t.Errorf("RED in the main thread, GREEN in a builder: %+v", fs)
	}
	// RED in a builder, GREEN in the main thread after it.
	if fs := findingsOf(t, "archive-without-red", user("go"),
		delegate("w01", "jhon-builder", chg, bash(red, "<== p: exit 1\n"+redSummary)),
		mainBash(green, greenSummary),
		mainBash(archive, "readback: empty")); len(fs) != 0 {
		t.Errorf("RED in a builder, GREEN in the main thread: %+v", fs)
	}
	// A builder's RED with no GREEN anywhere before the archive.
	pendingAt(t, findingsOf(t, "archive-without-red", user("go"),
		delegate("w01", "jhon-builder", chg, bash(red, "<== p: exit 1\n"+redSummary)),
		mainBash(archive, "readback: empty")), "ldd/red-first", "no GREEN")
	// A builder's GREEN after the archive does not close a RED before it.
	pendingAt(t, findingsOf(t, "archive-without-red", user("go"),
		delegate("w01", "jhon-builder", chg, bash(red, "<== p: exit 1\n"+redSummary)),
		mainBash(archive, "readback: empty"),
		delegate("w02", "jhon-builder", chg, bash(green, greenSummary))), "ldd/red-first", "no GREEN")
	// Another change's runs in a builder are not this change's.
	fs := findingsOf(t, "archive-without-red", user("go"),
		delegate("w01", "jhon-builder", "other", bash("jhon executions run other 2>&1 | tail -n 1", "<== p: exit 1\n"+redSummary),
			bash("jhon executions run other 2>&1 | tail -n 1", greenSummary)),
		mainBash(archive, "readback: empty"))
	if len(fs) != 1 || fs[0].Kind != "indeterminate" {
		t.Errorf("another change's runs: %+v", fs)
	}
	// A builder's own `jhon archive --help` is no archive.
	if fs := findingsOf(t, "archive-without-red", user("go"),
		delegate("w01", "jhon-builder", chg, bash("jhon archive --help 2>&1 | head -5", "jhon archive — move a change into the archive"))); len(fs) != 0 {
		t.Errorf("a builder's archive help: %+v", fs)
	}
}

func TestArchiveZeroOfZeroIsNoGreen(t *testing.T) {
	archive := "jhon archive " + chg + " 2026-09-30"
	nothing := "jhon executions: pass, exit 0 (0 of 0 passed; record ldd/changes/" + chg + "/execution-record.json)"
	pendingAt(t, findingsOf(t, "archive-without-red", user("go"),
		delegate("w01", "jhon-builder", chg, bash("jhon executions run "+chg+" --affected main 2>&1 | tail -n 2", "<== p: exit 1\n"+redSummary),
			bash("jhon executions run "+chg+" --affected 2>&1 | tail -n 1", nothing)),
		mainBash(archive, "readback: empty")), "ldd/red-first", "no GREEN")
	if fs := piFindings(t, "archive-without-red", puser("go"),
		psub("jhon-builder", "Build slice 1.", "done", pbash("jhon executions run "+chg+" --affected main 2>&1 | tail -n 2", "<== p: exit 1\n"+redSummary),
			pbash("jhon executions run "+chg+" --affected 2>&1 | tail -n 1", nothing)),
		pmain(pbash(archive, "readback: empty"))); len(fs) != 1 || fs[0].Kind != "would-be-violation" {
		t.Errorf("pi: a RED, then a pass that tested nothing: %+v", fs)
	}
	// PRESERVE: a pass that tested something is a GREEN.
	if fs := findingsOf(t, "archive-without-red", user("go"),
		delegate("w01", "jhon-builder", chg, bash("jhon executions run "+chg+" --affected main 2>&1 | tail -n 2", "<== p: exit 1\n"+redSummary),
			bash("jhon executions run "+chg+" --affected 2>&1 | tail -n 1", greenSummary)),
		mainBash(archive, "readback: empty")); len(fs) != 0 {
		t.Errorf("a RED, then a pass of 2 of 2: %+v", fs)
	}
}
