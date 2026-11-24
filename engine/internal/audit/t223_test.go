package audit

import "testing"

// T223 S4: archive-without-red reads the RED and GREEN runs a builder
// sub-agent made, as a session shows them. A builder that pipes its run
// through tail or grep shows the run's summary line, `jhon executions:
// <pass|fail>, exit N (...)`, and few or none of its per-project verdict
// lines: the summary is the run's verdict, so such a GREEN is a GREEN, and
// a summary that says fail is a RED even when the lines left show only
// passing projects. `jhon archive --help` archives nothing.

const (
	redSummary   = "jhon executions: fail, exit 1 (1 of 2 passed; record ldd/changes/" + chg + "/execution-record.json)"
	greenSummary = "jhon executions: pass, exit 0 (2 of 2 passed; record ldd/changes/" + chg + "/execution-record.json)"
)

func TestArchiveReadsPipedSubAgentRuns(t *testing.T) {
	archive := "jhon archive " + chg + " 2026-09-30"
	// A RED cut to its passing projects and its summary, then GREENs shown
	// by their summary alone: the builder piped every run.
	cutRed := "jhon executions run " + chg + " --affected main 2>&1 | tail -n 3"
	cutRedOut := "<== p: exit 0\n<== p (lint): exit 0\n" + redSummary
	grepped := "jhon executions run " + chg + " --affected 2>&1 | grep -E '^(pass|fail|jhon)'"
	greenOut := "pass       p\npass       p (lint)\n" + greenSummary

	if fs := piFindings(t, "archive-without-red", puser("go"),
		psub("jhon-builder", "Build slice 1.", "done", pbash(cutRed, cutRedOut), pbash(grepped, greenOut)),
		pmain(pbash(archive, "readback: empty"))); len(fs) != 0 {
		t.Errorf("pi: a piped RED then GREEN in the builder: %+v", fs)
	}
	if fs := findingsOf(t, "archive-without-red", user("go"),
		delegate("w01", "jhon-builder", chg, bash(cutRed, cutRedOut), bash(grepped, greenOut)),
		mainBash(archive, "readback: empty")); len(fs) != 0 {
		t.Errorf("claude code: a piped RED then GREEN in the builder: %+v", fs)
	}
	// The session's shape: a later builder's summary-only GREEN closes the
	// RED an earlier builder made, as one more slice.
	if fs := piFindings(t, "archive-without-red", puser("go"),
		psub("jhon-builder", "Build slice 1.", "done", pbash("jhon executions run "+chg+" --affected main 2>&1 | tail -n 60", "<== p: exit 1\n"+redSummary)),
		psub("jhon-builder", "Build slice 2.", "done", pbash(grepped, greenSummary)),
		pmain(pbash(archive, "readback: empty"))); len(fs) != 0 {
		t.Errorf("pi: a RED in one builder, a summary-only GREEN in the next: %+v", fs)
	}
}

// PRESERVE: a summary that says fail after the RED is no GREEN, as a
// failing verdict line was; with no run passing, the archive is still the
// would-be violation it was.
func TestArchiveSummaryFailIsNoGreen(t *testing.T) {
	archive := mainBash("jhon archive "+chg+" 2026-09-30", "readback: empty")
	pendingAt(t, findingsOf(t, "archive-without-red", user("go"), run(chg, "1"),
		mainBash("jhon executions run "+chg+" | tail -n 1", redSummary), archive), "ldd/red-first", "no GREEN")
}

// `jhon archive --help` (and -h) prints help and archives nothing, so it is
// no archive the check reads; a real archive in the same session still is.
func TestArchiveHelpIsNoArchive(t *testing.T) {
	for _, help := range []string{"jhon archive --help 2>&1 | head -15", "jhon archive -h"} {
		if fs := findingsOf(t, "archive-without-red", user("go"), mainBash(help, "jhon archive — move a change into the archive")); len(fs) != 0 {
			t.Errorf("%s: %+v", help, fs)
		}
	}
	fs := findingsOf(t, "archive-without-red", user("go"), mainBash("jhon archive --help", "usage"), mainBash("jhon archive "+chg+" 2026-09-30", "readback: empty"))
	if len(fs) != 1 || fs[0].Evidence != "jhon archive "+chg {
		t.Errorf("the real archive after the help: %+v", fs)
	}
}
