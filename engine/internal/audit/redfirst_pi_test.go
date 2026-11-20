package audit

import "testing"

// edit-before-red reads pi transcripts too, in the main session and in a
// sub-agent's, and a RED before the edit finds nothing (T31.6).
func TestEditBeforeRedInPi(t *testing.T) {
	ed := pcall{name: "edit", args: obj("path", testRoot+"/svca/src/totals.py", "edits", []any{obj("oldText", "a", "newText", "b")}), result: "ok"}
	red := pcall{name: "bash", args: obj("command", "jhon executions run "+acme, "timeout", 120.0), result: "1 failed", isError: true}
	green := pbash("jhon executions run "+acme, "all passed")
	if got := piFindings(t, "edit-before-red", puser("go"), pmain(ed), pmain(red), pmain(green)); len(got) != 1 {
		t.Errorf("pi main, edit before RED: want one finding, got %d %+v", len(got), got)
	}
	if got := piFindings(t, "edit-before-red", puser("go"), pmain(red), pmain(ed), pmain(green)); len(got) != 0 {
		t.Errorf("pi main, RED first: want none, got %d %+v", len(got), got)
	}
	if got := piFindings(t, "edit-before-red", puser("go"), psub("jhon-builder", "do "+acme, "done", ed, red, green)); len(got) != 1 {
		t.Errorf("pi sub-agent, edit before RED: want one finding, got %d %+v", len(got), got)
	}
}
