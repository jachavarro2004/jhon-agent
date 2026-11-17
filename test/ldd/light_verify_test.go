package ldd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// A light verify at slice close (T31.5, L252 C, L311 (1)): the verify agent
// reads the diff and the tests, runs the focused tests, and records its
// verdict with `jhon executions verify <change> --verdict pass|fail`, a note
// beside the change's RED and GREEN runs, with or without a folder.

// redGreenRuns runs change's tests failing (RED), then passing (GREEN).
func redGreenRuns(t *testing.T, root, change string) {
	t.Helper()
	support.Write(t, root, map[string]string{"red": ""})
	equal(t, jhonGit(t, root, "executions", "run", change, "--force").Code, 1, "RED")
	os.Remove(filepath.Join(root, "red"))
	r := jhonGit(t, root, "executions", "run", change, "--force")
	equal(t, r.Code, 0, "GREEN", r.Stderr)
}

func lightVerifyNotes(t *testing.T, recordPath string) []any {
	t.Helper()
	notes, _ := object(t, support.Read(t, recordPath))["light_verify"].([]any)
	return notes
}

func TestExecutionsVerifyWritesTheNote(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, change, record string }{
		{"folder", "c1", record},
		{"folderless", smallChange, smallRecord},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := smallRepo(t)
			if c.change == "c1" {
				support.Write(t, root, map[string]string{"openspec/changes/c1/tasks.md": "## Intent\nDelivers: B is 1\n"})
			}
			redGreenRuns(t, root, c.change)
			r := jhonGit(t, root, "executions", "verify", c.change, "--verdict", "pass", "--note", "read the diff; focused tests pass")
			equal(t, r.Code, 0, r.Stderr)
			match(t, r.Stdout, `^light verify pass recorded for `+c.change+` \(tree [0-9a-f]{40}\) in `+c.record+"\n$")
			notes := lightVerifyNotes(t, filepath.Join(root, c.record))
			if len(notes) != 1 {
				t.Fatalf("one note: %v", notes)
			}
			note := notes[0].(map[string]any)
			equal(t, note["verdict"], "pass")
			equal(t, note["note"], "read the diff; focused tests pass")
			match(t, note["tree"].(string), `^[0-9a-f]{40}$`)
			match(t, note["timestamp"].(string), `^\d{4}-\d\d-\d\dT`)
			k := keys(t, []byte(support.Read(t, filepath.Join(root, c.record))))
			equal(t, k[len(k)-1], "light_verify", "additive, at the end of the record")

			raw := []byte(jhonGit(t, root, "status", c.change, "--json").Stdout)
			tests := field(t, raw, "tests")
			tk := keys(t, tests)
			equal(t, tk[len(tk)-1], "light_verify", "last in the tests object")
			lv := object(t, string(field(t, tests, "light_verify")))
			equal(t, lv["verdict"], "pass")
			equal(t, lv["tree"], note["tree"])

			// --json prints the note; a later run keeps the notes.
			j := jhonGit(t, root, "executions", "verify", c.change, "--verdict", "fail", "--json")
			equal(t, j.Code, 0, j.Stderr)
			equal(t, object(t, j.Stdout)["verdict"], "fail")
			equal(t, jhonGit(t, root, "executions", "run", c.change, "--force").Code, 0)
			equal(t, len(lightVerifyNotes(t, filepath.Join(root, c.record))), 2, "a run keeps the notes")
		})
	}
}

func TestExecutionsVerifyRefusesBadInput(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	r := jhonGit(t, root, "executions", "verify", smallChange, "--verdict", "pass")
	equal(t, r.Code, 2, "no run on record")
	equal(t, r.Stderr, "jhon executions: no run on record for "+smallChange+": a light verify judges a tested change; run `jhon executions run "+smallChange+"` first\n")
	equal(t, exists(filepath.Join(root, smallRecord)), false, "nothing written")
	redGreenRuns(t, root, smallChange)
	before := support.Read(t, filepath.Join(root, smallRecord))
	for _, args := range [][]string{
		{"executions", "verify", smallChange},
		{"executions", "verify", smallChange, "--verdict", "maybe"},
		{"executions", "verify", smallChange, "--verdict"},
		{"executions", "verify", "--verdict", "pass"},
	} {
		r := jhonGit(t, root, args...)
		equal(t, r.Code, 2, args)
		equal(t, r.Stdout, "", args)
		match(t, r.Stderr, `usage: jhon executions verify <change> --verdict pass\|fail \[--note <text>\] \[--json\] \[--root <project>\]`, args)
	}
	equal(t, support.Read(t, filepath.Join(root, smallRecord)), before, "a refused verify writes nothing")
	// `run` is unchanged.
	r = jhonGit(t, root, "executions", "nope", smallChange)
	equal(t, r.Code, 2)
	match(t, r.Stderr, `usage: jhon executions run <change>`)
}

const lightVerifyDue = `^light verify due for feat-small-fix: its GREEN run is newer than its last light verify; launch the ` + "`verify`" + ` agent in light mode \(read the diff and the tests, run the focused tests\), which records ` + "`jhon executions verify feat-small-fix --verdict pass\\|fail`$"

// Status asks for the light verify when the RED before GREEN run is newer
// than the last one (or there is none), and only for a change that alters
// behaviour; next is never moved by it.
func TestStatusAsksForTheLightVerify(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	redGreenRuns(t, root, smallChange)
	s := statusJSON(t, root, smallChange)
	equal(t, anyMatch(s["notes"], lightVerifyDue), true, s["notes"])
	next := s["next"]
	equal(t, jhonGit(t, root, "executions", "verify", smallChange, "--verdict", "pass").Code, 0)
	s = statusJSON(t, root, smallChange)
	equal(t, anyMatch(s["notes"], `^light verify due`), false, s["notes"])
	equal(t, s["next"], next, "next stays as computed")
	// A newer run asks again.
	equal(t, jhonGit(t, root, "executions", "run", smallChange, "--force").Code, 0)
	equal(t, anyMatch(statusJSON(t, root, smallChange)["notes"], lightVerifyDue), true, "a newer run")

	// A change to tests and documents alone is never asked.
	passive := smallRepo(t)
	os.Remove(filepath.Join(passive, "a/b.go"))
	redGreenRuns(t, passive, smallChange)
	equal(t, anyMatch(statusJSON(t, passive, smallChange)["notes"], `^light verify due`), false, "a passive change")
}
