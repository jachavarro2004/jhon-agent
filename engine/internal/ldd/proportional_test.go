package ldd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The record is proportional (T31, L252): a change owes only what NeedsOf
// asks for, TDD always; status never suggests archive before the change's
// RED before GREEN, nor for a change with no folder, which has nothing to
// archive.

// ran writes change's record at file: its last run passed, after a failing
// one when red.
func ran(t *testing.T, file string, red bool) {
	t.Helper()
	history := `[]`
	if red {
		history = `[{"timestamp":"2026-01-01T00:00:00.000Z","passed":false}]`
	}
	write(t, file, `{"timestamp":"2026-01-01T00:01:00.000Z","complete":true,"scope":"full","executions":[{"project":".","command":"true","status":"pass","exit_code":0}],"history":`+history+"}\n")
}

// lightVerified is ran with its RED, then a passing light verify of the
// tree it tested (T31b): a folder change on the standard route is archived
// only then.
func lightVerified(t *testing.T, file string) {
	t.Helper()
	ran(t, file, true)
	b, _ := os.ReadFile(file)
	write(t, file, strings.TrimSuffix(string(b), "}\n")+`,"light_verify":[{"timestamp":"2026-01-01T00:02:00.000Z","verdict":"pass","commit":"","tree":""}]}`+"\n")
}

func shownNames(st Status) []string {
	var out []string
	for _, a := range shownArtifacts(st) {
		out = append(out, a.Artifact)
	}
	return out
}

// A change owes tasks.md and a spec delta only when its needs ask for them.
func TestOwedFollowsNeeds(t *testing.T) {
	jhonRoot(t)
	root := configuredRepo(t, "", "src/app.py", "tests/test_app.py")
	write(t, filepath.Join(root, "openspec/changes/c/exploration.md"), "# x\n")
	if got := shownNames(statusOf(t, root, s("c"))); !slices.Equal(got, []string{"exploration.md"}) {
		t.Errorf("needs nothing: %v, want only what is written", got)
	}
	write(t, filepath.Join(root, "openspec/specs/a/spec.md"), "# a\n")
	write(t, filepath.Join(root, "src/big.py"), strings.Repeat("x = 1\n", 401))
	if got := shownNames(statusOf(t, root, s("c"))); !slices.Equal(got, []string{"exploration.md", "specs/", "tasks.md"}) {
		t.Errorf("needs tasks and a spec: %v", got)
	}
}

// A change that needs no tasks.md and no spec is suggested apply until its
// RED before GREEN is on record, with a note saying how; then verify, until
// a light verify passes (T31b); then archive.
func TestNextWaitsForRedBeforeGreen(t *testing.T) {
	jhonRoot(t)
	root := configuredRepo(t, "", "src/app.py", "tests/test_app.py")
	// Its folder holds no artifact: an exploration alone would make it an umbrella (T37).
	write(t, filepath.Join(root, "openspec/changes/c/.openspec.yaml"), "schema: spec-driven\n")
	st := statusOf(t, root, s("c"))
	if st.Next != "apply" || !strings.Contains(notes(st), "no RED before GREEN on record for c: run `jhon executions run c --affected <base>` while its new test fails (RED), a unit's run (`--affected`) between, and `--affected <base>` again once it passes (GREEN), where <base> is the commit the slice started from (with none, a full `jhon executions run c`)") {
		t.Errorf("no runs: next %s, notes %q", st.Next, st.Notes)
	}
	record := filepath.Join(root, "openspec/changes/c/execution-record.json")
	ran(t, record, false)
	if st := statusOf(t, root, s("c")); st.Next != "apply" {
		t.Errorf("a GREEN with no RED: next %s", st.Next)
	}
	ran(t, record, true)
	if st := statusOf(t, root, s("c")); st.Next != "verify" || strings.Contains(notes(st), "RED before GREEN") {
		t.Errorf("RED before GREEN, light verify due: next %s, notes %q", st.Next, st.Notes)
	}
	lightVerified(t, record)
	if st := statusOf(t, root, s("c")); st.Next != "archive" || strings.Contains(notes(st), "RED before GREEN") {
		t.Errorf("RED before GREEN: next %s, notes %q", st.Next, st.Notes)
	}
}

// A change whose tasks are done is never suggested archive before its RED
// before GREEN either.
func TestArchiveWaitsForRedBeforeGreen(t *testing.T) {
	jhonRoot(t)
	root := configuredRepo(t, "", "src/app.py", "tests/test_app.py")
	write(t, filepath.Join(root, "openspec/changes/c/tasks.md"), "## Intent\nDelivers: the totals\n\n- [x] 1.1 totals\n")
	if st := statusOf(t, root, s("c")); st.Next != "apply" || !strings.Contains(notes(st), "no RED before GREEN on record for c") {
		t.Errorf("tasks done, no runs: next %s, notes %q", st.Next, st.Notes)
	}
	ran(t, filepath.Join(root, "openspec/changes/c/execution-record.json"), true)
	if st := statusOf(t, root, s("c")); st.Next != "verify" {
		t.Errorf("tasks done, RED before GREEN, light verify due: next %s", st.Next)
	}
	lightVerified(t, filepath.Join(root, "openspec/changes/c/execution-record.json"))
	if st := statusOf(t, root, s("c")); st.Next != "archive" {
		t.Errorf("tasks done, RED before GREEN, light verified: next %s", st.Next)
	}
}

// A change with no folder has nothing to archive: once its RED before
// GREEN is on record it is delivered, its PR or commit saying what was asked.
func TestFolderlessNeverArchives(t *testing.T) {
	jhonRoot(t)
	root := configuredRepo(t, "", "src/app.py", "tests/test_app.py")
	st := statusOf(t, root, nil)
	if st.Change == nil || *st.Change != "feature" || st.Next != "apply" {
		t.Fatalf("no runs: %+v", st)
	}
	if got := shownNames(st); len(got) != 0 {
		t.Errorf("a folderless change that needs nothing owes no artifact: %v", got)
	}
	ran(t, filepath.Join(root, ".git/jhon/changes/feature/execution-record.json"), true)
	st = statusOf(t, root, nil)
	if st.Next != "merge" || !strings.Contains(notes(st), "nothing to archive: feature has no folder; deliver it with a PR or commit whose text says what was asked") {
		t.Errorf("RED before GREEN: next %s, notes %q", st.Next, st.Notes)
	}
}

// A change that needs neither tasks.md nor a spec is recorded by its RED
// before GREEN; any other by Documented, as before.
func TestRecorded(t *testing.T) {
	jhonRoot(t)
	root := configuredRepo(t, "", "src/app.py", "tests/test_app.py")
	dir := filepath.Join(root, "openspec/changes/c")
	write(t, filepath.Join(dir, "exploration.md"), "# x\n")
	small, big := Needs{}, Needs{Tasks: true}
	r := Route{Tier: Medium}
	if Recorded(root, "c", dir, r, small) {
		t.Error("needs nothing, no runs: recorded")
	}
	ran(t, filepath.Join(dir, "execution-record.json"), true)
	if !Recorded(root, "c", dir, r, small) {
		t.Error("needs nothing, RED before GREEN: not recorded")
	}
	if Recorded(root, "c", dir, r, big) {
		t.Error("needs tasks.md, none written: recorded")
	}
	write(t, filepath.Join(dir, "tasks.md"), "## Intent\nDelivers: the totals\n")
	if !Recorded(root, "c", dir, r, big) {
		t.Error("needs tasks.md, a one-line record: not recorded")
	}
}
