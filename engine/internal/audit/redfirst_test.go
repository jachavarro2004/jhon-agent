package audit

import (
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
	f := findingsOf(t, "edit-before-red", user("go"), delegate("d01", "worker", acme, codeEdit("svca/src/totals.py"), red(acme), green(acme)))
	wouldBe(t, f)
	if !strings.Contains(f[0].Message, "Edit "+testRoot+"/svca/src/totals.py changes code of acme-totals before its RED") {
		t.Errorf("message: %s", f[0].Message)
	}
	eq(t, "status", f[0].ValidationStatus, "pending-real-fixture")

	eq(t, "RED before the edit", len(findingsOf(t, "edit-before-red", user("go"), delegate("d01", "worker", acme, red(acme), codeEdit("svca/src/totals.py"), green(acme)))), 0)
	for _, p := range []string{"svca/tests/test_totals.py", "svca/src/totals_test.go", "web/src/a.test.ts", "docs/guide.md", "openspec/changes/acme-totals/specs/a/spec.md"} {
		eq(t, p+" before RED", len(findingsOf(t, "edit-before-red", user("go"), delegate("d01", "worker", acme, codeEdit(p), red(acme)))), 0)
	}
	// A record written after the first code edit: the edit came before it.
	f = findingsOf(t, "edit-before-red", user("go"), delegate("d01", "worker", acme, red(acme), codeEdit("svca/src/totals.py"), recordWrite(acme)))
	wouldBe(t, f)
	if !strings.Contains(f[0].Message, "before its record openspec/changes/acme-totals/tasks.md") {
		t.Errorf("message: %s", f[0].Message)
	}
	eq(t, "record before the edit", len(findingsOf(t, "edit-before-red", user("go"), delegate("d01", "worker", acme, recordWrite(acme), red(acme), codeEdit("svca/src/totals.py")))), 0)
	// The main thread too.
	wouldBe(t, findingsOf(t, "edit-before-red", user("go"), step{kind: "tool", tool: "Edit", input: obj("file_path", testRoot+"/svca/src/a.py", "old_string", "a", "new_string", "b"), result: "ok"},
		step{kind: "bash", text: "jhon executions run " + acme, result: "1 failed", isError: true}))
}

func TestEditBeforeRedFindingJSON(t *testing.T) {
	f := findingsOf(t, "edit-before-red", user("go"), delegate("d01", "worker", acme, codeEdit("svca/src/totals.py"), red(acme)))
	wouldBe(t, f)
	o := f[0].value()
	eq(t, "keys", o.Keys(), []string{"check", "severity", "transcript", "line", "agent", "agentType", "evidence", "message", "validationStatus", "kind"})
	s := js.StringifyValueCompact(o)
	for _, want := range []string{`"check":"edit-before-red"`, `"severity":"suspect"`, `"evidence":"Edit /work/p/svca/src/totals.py"`, `"kind":"would-be-violation"`, `"agentType":"worker"`} {
		if !strings.Contains(s, want) {
			t.Errorf("%s not in %s", want, s)
		}
	}
}
