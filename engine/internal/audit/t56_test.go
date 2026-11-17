package audit

import (
	"slices"
	"strings"
	"testing"
)

// T56: four checks with evidence after the fact, each pending-real-fixture
// until real clean and violation fixtures back it: a would-be violation.

func pendingAt(t *testing.T, fs []Finding, anchor, says string) {
	t.Helper()
	wouldBe(t, fs)
	if fs[0].ValidationStatus != "pending-real-fixture" || fs[0].Anchor != anchor || !strings.Contains(fs[0].Message, says) {
		t.Fatalf("want a pending %s finding saying %q, got %+v", anchor, says, fs[0])
	}
}

func pwrite(path string) pcall {
	return pcall{name: "write", args: obj("path", path, "content", "x"), result: "ok"}
}

// pi isolation: a pi worker or verify child writes outside the cwd its session header records.
func TestPiChildWritingOutsideItsOwnTree(t *testing.T) {
	for _, agent := range []string{"worker", "verify"} {
		fs := piFindings(t, "isolation-bypass", puser("go"), psub(agent, "Fix.", "done", pwrite("a.txt"), pwrite("/elsewhere/p/a.txt")))
		pendingAt(t, fs, "isolation/no-other-tree", "outside its own tree "+testRoot)
		if fs[0].AgentType != agent || !strings.Contains(fs[0].Evidence, "/elsewhere/p/a.txt") {
			t.Errorf("%s: %+v", agent, fs[0])
		}
	}
	// The worktree the launch prompt hands the child is its own tree too.
	handed := psub("worker", "Fix it in the worktree /work/trees/p-wt-1.", "done", pwrite("/work/trees/p-wt-1/src/a.go"))
	if fs := piFindings(t, "isolation-bypass", puser("go"), handed); len(fs) != 0 {
		t.Fatalf("handed worktree: %+v", fs)
	}
	// Clean: inside the tree, relative paths, the system temp directory, and an explore child.
	clean := psub("worker", "Fix.", "done", pwrite("a.txt"), pwrite(testRoot+"/src/b.go"), pwrite("/tmp/scratch-1/out.txt"))
	if fs := piFindings(t, "isolation-bypass", puser("go"), clean, psub("explore", "Look.", "done", pwrite("/elsewhere/notes.md"))); len(fs) != 0 {
		t.Fatalf("clean: %+v", fs)
	}
}

// Claude Code keeps its own isolation reading: a write under another root
// without an isolation refusal is no finding.
func TestClaudeCodeWorkerOutsideItsRootIsNotPiIsolation(t *testing.T) {
	write := tcall{name: "Write", input: obj("file_path", "/elsewhere/p/a.txt", "content", "x")}
	if fs := findingsOf(t, "isolation-bypass", user("go"), delegate("w01", "worker", chg, write)); len(fs) != 0 {
		t.Fatalf("%+v", fs)
	}
}

func edit(file, old, new string) tcall {
	return tcall{name: "Edit", input: obj("file_path", file, "old_string", old, "new_string", new)}
}

const testHead = "func TestTiming(t *testing.T) {\n"

// test-weakened: after a failing run, an edit to a test file adds a skip
// marker or removes a test block before the next pass.
func TestTestWeakened(t *testing.T) {
	red := bash("go test ./...", "--- FAIL: TestTiming (0.00s)\nFAIL", true)
	green := bash("go test ./...", "ok  \tx\t0.1s")
	skip := edit("timing_test.go", testHead, testHead+"\tt.Skip(\"later\")\n")
	pendingAt(t, findingsOf(t, "test-weakened", user("go"), delegate("w01", "worker", chg, red, skip, green)), "tdd/no-weaken", "adds a skip marker")
	removed := edit("timing_test.go", testHead+"\tif x() != 1 {\n\t\tt.Fatal(\"x\")\n\t}\n}\n", "")
	pendingAt(t, findingsOf(t, "test-weakened", user("go"), delegate("w01", "worker", chg, red, removed, green)), "tdd/no-weaken", "removes a test")
	py := edit("tests/test_api.py", "def test_api():\n", "@pytest.mark.skip\ndef test_api():\n")
	pendingAt(t, findingsOf(t, "test-weakened", user("go"), delegate("w01", "worker", chg, bash("pytest", "1 failed", true), py)), "tdd/no-weaken", "adds a skip marker")

	for name, calls := range map[string][]tcall{
		"a skip added with no failing run":    {green, skip},
		"a skip added after the pass":         {red, green, skip},
		"a stronger test while failing":       {red, edit("timing_test.go", testHead, testHead+"\tt.Parallel()\n"), green},
		"a skip in a code file while failing": {red, edit("timing.go", "x := 1", "x := 1 // t.Skip("), green},
	} {
		if fs := findingsOf(t, "test-weakened", user("go"), delegate("w01", "worker", chg, calls...)); len(fs) != 0 {
			t.Errorf("%s: %+v", name, fs)
		}
	}
}

// verify-wrote: a verify agent writes outside its verify record.
func TestVerifyWrote(t *testing.T) {
	report := tcall{name: "Write", input: obj("file_path", testRoot+"/openspec/changes/"+chg+"/verify-report.md", "content", "x")}
	fix := edit(testRoot+"/src/timing.go", "a", "b")
	pendingAt(t, findingsOf(t, "verify-wrote", user("go"), delegate("v01", "verify", chg, report, fix)), "ldd/independent-verify", "src/timing.go")
	if fs := findingsOf(t, "verify-wrote", user("go"), delegate("v01", "verify", chg, report, tcall{name: "Write", input: obj("file_path", "/tmp/v/out.txt", "content", "x")}),
		delegate("w01", "worker", chg, fix)); len(fs) != 0 {
		t.Fatalf("clean: %+v", fs)
	}
}

func run(change string, exit string) step {
	return mainBash("jhon executions run "+change, "==> p: go test\n<== p: exit "+exit)
}

// archive-without-red: `jhon archive <c>` with no RED then GREEN of c in the
// session; a change the session never ran is indeterminate (its RED may be
// in another session).
func TestArchiveWithoutRed(t *testing.T) {
	archive := mainBash("jhon archive "+chg+" 2026-09-30", "readback: empty")
	if fs := findingsOf(t, "archive-without-red", user("go"), run(chg, "1"), run(chg, "0"), archive); len(fs) != 0 {
		t.Fatalf("RED then GREEN: %+v", fs)
	}
	reused := mainBash("jhon executions run "+chg, "==> p: go test  (reused: it passed on this same working tree in the last run)")
	if fs := findingsOf(t, "archive-without-red", user("go"), run(chg, "1"), reused, archive); len(fs) != 0 {
		t.Fatalf("RED then a reused GREEN: %+v", fs)
	}
	pendingAt(t, findingsOf(t, "archive-without-red", user("go"), run(chg, "1"), archive), "ldd/red-first", "no GREEN")
	pendingAt(t, findingsOf(t, "archive-without-red", user("go"), run(chg, "1"), archive, run(chg, "0")), "ldd/red-first", "no GREEN")
	// No RED in the session, passes only, or no run, or runs with no verdict:
	// the RED may be another session's.
	for name, steps := range map[string][]step{
		"passes only":   {run(chg, "0"), archive},
		"never ran":     {run("other", "1"), archive},
		"no verdict":    {mainBash("jhon executions run "+chg, "started"), archive},
		"GREEN unknown": {run(chg, "1"), mainBash("jhon executions run "+chg+" | tail -1", "3 passed"), archive},
	} {
		fs := findingsOf(t, "archive-without-red", append([]step{user("go")}, steps...)...)
		if len(fs) != 1 || fs[0].Kind != "indeterminate" || fs[0].Severity != "suspect" {
			t.Fatalf("%s: want one indeterminate, got %+v", name, fs)
		}
	}
	// A commit message naming the archive is no archive.
	if fs := findingsOf(t, "archive-without-red", user("go"), mainBash("git commit -m 'jhon archive "+chg+"'")); len(fs) != 0 {
		t.Fatalf("quoted: %+v", fs)
	}
}

func TestT56ChecksAreRegistered(t *testing.T) {
	for _, c := range []string{"test-weakened", "verify-wrote", "archive-without-red"} {
		if !slices.Contains(Checks, c) || ValidationStatuses()[c] != "pending-real-fixture" {
			t.Errorf("%s: registered %v, status %s", c, slices.Contains(Checks, c), ValidationStatuses()[c])
		}
	}
	if !slices.Contains(PendingOn()["isolation/no-other-tree"], "pi") || ValidationStatuses()["isolation-bypass"] != "verified" {
		t.Errorf("isolation: pending on %v, status %s", PendingOn()["isolation/no-other-tree"], ValidationStatuses()["isolation-bypass"])
	}
}

// explore-wrote (T123): an explore agent writes only its change's
// exploration.md or research.md, in either layout.
func TestExploreWrote(t *testing.T) {
	rec := func(name string) string { return testRoot + "/openspec/changes/" + chg + "/" + name }
	tasks := tcall{name: "Write", input: obj("file_path", rec("tasks.md"), "content", "x")}
	ok := tcall{name: "Write", input: obj("file_path", rec("exploration.md"), "content", "x")}
	pendingAt(t, findingsOf(t, "explore-wrote", user("go"), delegate("e01", "explore", chg, ok, tasks)), "ldd/explore-scope", "tasks.md")
	fs := piFindings(t, "explore-wrote", puser("go"), psub("explore", "Explore "+chg+".", "done", pwrite(rec("research.md")), pwrite(testRoot+"/src/a.go")))
	pendingAt(t, fs, "ldd/explore-scope", "src/a.go")
	if len(fs) != 1 || fs[0].AgentType != "explore" {
		t.Fatalf("once per agent: %+v", fs)
	}
	clean := []tcall{ok, tcall{name: "Write", input: obj("file_path", testRoot+"/ldd/changes/"+chg+"/research.md", "content", "x")},
		tcall{name: "Write", input: obj("file_path", "/tmp/e/scratch.md", "content", "x")}}
	if fs := findingsOf(t, "explore-wrote", user("go"), delegate("e01", "explore", chg, clean...), delegate("w01", "worker", chg, tasks)); len(fs) != 0 {
		t.Fatalf("clean: %+v", fs)
	}
	if fs := piFindings(t, "explore-wrote", puser("go"), psub("worker", "Fix.", "done", pwrite(testRoot+"/src/a.go"))); len(fs) != 0 {
		t.Fatalf("worker: %+v", fs)
	}
}

// A refused write is no finding; the same file's later successful edit is, once.
func TestExploreWroteAfterARefusal(t *testing.T) {
	refused := pwrite(testRoot + "/openspec/changes/" + chg + "/tasks.md")
	refused.isError, refused.result = true, "vcs-guard: refused"
	fs := piFindings(t, "explore-wrote", puser("go"), psub("explore", "Explore "+chg+".", "done", refused, pedit("openspec/changes/"+chg+"/tasks.md")))
	if len(fs) != 1 || !strings.HasPrefix(fs[0].Evidence, "Edit ") || !strings.Contains(fs[0].Evidence, "tasks.md") {
		t.Fatalf("want one finding on the edit: %+v", fs)
	}
	// Nested layouts are not the project's: no record there.
	fs = piFindings(t, "explore-wrote", puser("go"), psub("explore", "Explore "+chg+".", "done", pwrite(testRoot+"/src/openspec/changes/"+chg+"/exploration.md")))
	if len(fs) != 1 {
		t.Fatalf("nested layout: %+v", fs)
	}
}

// The real violation (L762 F3): a pi explore child wrote tasks.md and a spec delta.
func TestExploreWroteRealFixture(t *testing.T) {
	r := Audit(fixture(t, "explore-wrote-real"))
	fs := of(r.Findings, "explore-wrote")
	if len(fs) != 1 || fs[0].Kind != "would-be-violation" || fs[0].AgentType != "explore" || !strings.Contains(fs[0].Evidence, "openspec/changes/example-change/tasks.md") {
		t.Fatalf("%+v (errors %+v)", r.Findings, r.Errors)
	}
}
