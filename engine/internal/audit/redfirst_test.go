package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// edit-before-red (ldd/red-first, T31.6): a change's first behaviour-changing
// edit of a code file comes after its RED, the first failing
// `jhon executions run <change>`, and after its tasks.md when it has one.

func codeEdit(path string) tcall {
	return tcall{name: "Edit", input: obj("file_path", testRoot+"/"+path, "old_string", "a", "new_string", "b")}
}

func recordWrite(change string) tcall {
	return tcall{name: "Write", input: obj("file_path", testRoot+"/openspec/changes/"+change+"/tasks.md", "content", "## Intent\nDelivers: x\n")}
}

func red(change string) tcall   { return bash("jhon executions run "+change, "1 failed", true) }
func green(change string) tcall { return bash("jhon executions run "+change, "all passed") }

const acme = "acme-totals"

func TestEditBeforeRed(t *testing.T) {
	f := findingsOf(t, "edit-before-red", user("go"), delegate("d01", "jhon-builder", acme, codeEdit("svca/src/totals.py"), red(acme), green(acme)))
	wouldBe(t, f)
	if !strings.Contains(f[0].Message, "Edit "+testRoot+"/svca/src/totals.py changes code of acme-totals before its RED") {
		t.Errorf("message: %s", f[0].Message)
	}
	eq(t, "status", f[0].ValidationStatus, "pending-real-fixture")

	eq(t, "RED before the edit", len(findingsOf(t, "edit-before-red", user("go"), delegate("d01", "jhon-builder", acme, red(acme), codeEdit("svca/src/totals.py"), green(acme)))), 0)
	for _, p := range []string{"svca/tests/test_totals.py", "svca/src/totals_test.go", "web/src/a.test.ts", "docs/guide.md", "openspec/changes/acme-totals/specs/a/spec.md"} {
		eq(t, p+" before RED", len(findingsOf(t, "edit-before-red", user("go"), delegate("d01", "jhon-builder", acme, codeEdit(p), red(acme)))), 0)
	}
	// A record written after the first code edit: the edit came before it.
	f = findingsOf(t, "edit-before-red", user("go"), delegate("d01", "jhon-builder", acme, red(acme), codeEdit("svca/src/totals.py"), recordWrite(acme)))
	wouldBe(t, f)
	if !strings.Contains(f[0].Message, "before its record openspec/changes/acme-totals/tasks.md") {
		t.Errorf("message: %s", f[0].Message)
	}
	eq(t, "record before the edit", len(findingsOf(t, "edit-before-red", user("go"), delegate("d01", "jhon-builder", acme, recordWrite(acme), red(acme), codeEdit("svca/src/totals.py")))), 0)
	// The main thread too.
	wouldBe(t, findingsOf(t, "edit-before-red", user("go"), step{kind: "tool", tool: "Edit", input: obj("file_path", testRoot+"/svca/src/a.py", "old_string", "a", "new_string", "b"), result: "ok"},
		step{kind: "bash", text: "jhon executions run " + acme, result: "1 failed", isError: true}))
}

// jhon-studio's working output (.jhon/studio/, T8g) is no code edit: a
// video's files written before the change's RED are no finding; a code
// edit after them still is.
func TestEditBeforeRedLeavesStudioOutput(t *testing.T) {
	studio := []string{".jhon/studio/tour/script.json", ".jhon/studio/tour/state.json", "web/.jhon/studio/tour/scene.js"}
	for _, p := range studio {
		eq(t, p+" before RED", len(findingsOf(t, "edit-before-red", user("go"), delegate("d01", "jhon-builder", acme, codeEdit(p), red(acme)))), 0)
	}
	f := findingsOf(t, "edit-before-red", user("go"), delegate("d01", "jhon-builder", acme, codeEdit(studio[0]), codeEdit("svca/src/totals.py"), red(acme)))
	wouldBe(t, f)
	if !strings.Contains(f[0].Message, "Edit "+testRoot+"/svca/src/totals.py changes code of acme-totals before its RED") {
		t.Errorf("message: %s", f[0].Message)
	}
}

func TestEditBeforeRedFindingJSON(t *testing.T) {
	f := findingsOf(t, "edit-before-red", user("go"), delegate("d01", "jhon-builder", acme, codeEdit("svca/src/totals.py"), red(acme)))
	wouldBe(t, f)
	o := f[0].value()
	eq(t, "keys", o.Keys(), []string{"check", "severity", "transcript", "line", "agent", "agentType", "evidence", "message", "validationStatus", "kind"})
	s := js.StringifyValueCompact(o)
	for _, want := range []string{`"check":"edit-before-red"`, `"severity":"suspect"`, `"evidence":"Edit /work/p/svca/src/totals.py"`, `"kind":"would-be-violation"`, `"agentType":"jhon-builder"`} {
		if !strings.Contains(s, want) {
			t.Errorf("%s not in %s", want, s)
		}
	}
}

// A write outside the change's project and its worktrees is not change code
// (T254, L1721): a helper the main agent writes under /tmp, under $TMPDIR, or
// in another folder before the change's record and RED is no edit before it.
func TestEditBeforeRedOutsideTheProject(t *testing.T) {
	write := func(file string) step {
		return step{kind: "tool", tool: "Write", input: obj("file_path", file, "content", "print(1)\n"), result: "ok"}
	}
	record := step{kind: "tool", tool: "Write", input: obj("file_path", testRoot+"/openspec/changes/"+acme+"/tasks.md", "content", "## Intent\nDelivers: x\n"), result: "ok"}
	runRed := step{kind: "bash", text: "jhon executions run " + acme, result: "1 failed", isError: true}
	tmpdir := t.TempDir()
	for _, p := range []string{"/tmp/helper.py", "/tmp/jhon-1/scripts/helper.sh", tmpdir + "/helper.py", "/srv/other/tools/helper.py", "/work/p-other/src/helper.py"} {
		eq(t, p+" before the record and RED", len(findingsOf(t, "edit-before-red", user("go"), write(p), record, runRed)), 0)
		eq(t, p+" before RED", len(findingsOf(t, "edit-before-red", user("go"), write(p), runRed)), 0)
	}
	// A helper outside, then the project's code before RED: that code is the edit.
	f := findingsOf(t, "edit-before-red", user("go"), write("/tmp/helper.py"), write(testRoot+"/svca/src/totals.py"), runRed)
	wouldBe(t, f)
	if !strings.Contains(f[0].Message, "Write "+testRoot+"/svca/src/totals.py changes code of acme-totals before its RED") {
		t.Errorf("message: %s", f[0].Message)
	}
	// A write inside the project before the record is still reported.
	f = findingsOf(t, "edit-before-red", user("go"), write(testRoot+"/svca/src/helper.py"), record, runRed)
	wouldBe(t, f)
	if !strings.Contains(f[0].Message, "Write "+testRoot+"/svca/src/helper.py changes code of acme-totals before its record openspec/changes/acme-totals/tasks.md") {
		t.Errorf("message: %s", f[0].Message)
	}
}

// A write in a linked worktree of the change's project still counts; one in
// another repository beside it does not (T254).
func TestEditBeforeRedInAWorktree(t *testing.T) {
	repo, lane := repoWithWorktree(t)
	other := filepath.Join(filepath.Dir(repo), "other")
	must(t, os.MkdirAll(other, 0o755))
	gitT(t, other, "init", "-q")
	d := delegate("d01", "jhon-builder", acme, writeCall(lane+"/svca/src/totals.py"), red(acme))
	d.root = repo
	f := findingsOf(t, "edit-before-red", user("go"), d)
	wouldBe(t, f)
	if !strings.Contains(f[0].Message, "Write "+lane+"/svca/src/totals.py changes code of acme-totals before its RED") {
		t.Errorf("message: %s", f[0].Message)
	}
	d = delegate("d01", "jhon-builder", acme, writeCall(other+"/src/totals.py"), red(acme))
	d.root = repo
	eq(t, "another repository", len(findingsOf(t, "edit-before-red", user("go"), d)), 0)
}
