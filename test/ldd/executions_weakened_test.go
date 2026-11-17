package ldd

import (
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// A weakened test (T137, L1064 KB1-KB3): a test a recorded run of the
// change names as failed, which a later recorded run passes while only its
// own test file changed in its project, is marked in the record as turned
// green by editing only its test, with both runs' identifiers; `jhon
// context --judge` carries it as `tests_to_judge`. Nothing is refused. An
// edit to a non-test file of that project between the two runs means no
// mark.
//
// Project b's tests (b/t.sh) print pytest's summary line for
// tests/test_ids.py::test_ids and fail, unless the test file says
// `weakened` or the source b/app.py says `fixed`.
const weakenedScript = `if grep -q weakened tests/test_ids.py || grep -q fixed app.py; then echo '1 passed'; else printf 'FAILED tests/test_ids.py::test_ids - assert ids == ids2\n'; exit 1; fi
`

func weakenedWorkspace(t *testing.T) string {
	t.Helper()
	root := committed(t, "a", "true", "b", "sh t.sh", "c", "true")
	support.Write(t, root, map[string]string{
		"b/t.sh":                weakenedScript,
		"b/tests/test_ids.py":   "def test_ids():\n    assert ids == ids2\n",
		"b/tests/test_other.py": "def test_other():\n    pass\n",
	})
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "tests")
	return root
}

func weakenedRun(t *testing.T, root string, want int) map[string]any {
	t.Helper()
	r := jhonGit(t, root, "executions", "run", "c1", "--json")
	equal(t, r.Code, want, r.Stderr)
	return object(t, r.Stdout)
}

func TestExecutionsMarkATestTurnedGreenByEditingOnlyItsTest(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		edits map[string]string
		mark  bool
	}{
		{"only its test file", map[string]string{"b/tests/test_ids.py": "def test_ids():\n    assert True  # weakened\n"}, true},
		{"its test file and another project's file", map[string]string{"b/tests/test_ids.py": "# weakened\n", "a/x.go": "package a\n"}, true},
		{"its test file and another test file of its project", map[string]string{"b/tests/test_ids.py": "# weakened\n", "b/tests/test_other.py": "# more\n"}, true},
		{"its test file and a source file of its project", map[string]string{"b/tests/test_ids.py": "# weakened\n", "b/app.py": "print('b2')\n"}, false},
		{"only a source file of its project", map[string]string{"b/app.py": "print('fixed')\n"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := weakenedWorkspace(t)
			weakenedRun(t, root, 1)
			redStamp := readRecord(t, root)["timestamp"]
			support.Write(t, root, c.edits)
			out := weakenedRun(t, root, 0)
			rec := readRecord(t, root)
			marks, _ := rec["turned_green"].([]any)
			judge := object(t, jhonGit(t, root, "context", "c1", "--phase", "none", "--judge").Stdout)
			if !c.mark {
				equal(t, marks, []any(nil), "no mark")
				equal(t, judge["tests_to_judge"], nil, "nothing to judge")
				for _, n := range out["notes"].([]any) {
					if strings.Contains(n.(string), "turned green") {
						t.Errorf("a note of a mark: %s", n)
					}
				}
				return
			}
			want := []any{map[string]any{
				"project": "b", "test": "tests/test_ids.py::test_ids", "file": "b/tests/test_ids.py",
				"red_run": redStamp, "green_run": rec["timestamp"],
			}}
			got := []any{}
			for _, m := range marks {
				m := m.(map[string]any)
				delete(m, "red_tree")
				delete(m, "green_tree")
				got = append(got, m)
			}
			equal(t, got, want, "the record's mark")
			equal(t, judge["tests_to_judge"], readRecord(t, root)["turned_green"], "jhon context --judge")
			found := false
			for _, n := range out["notes"].([]any) {
				found = found || strings.Contains(n.(string), "b: tests/test_ids.py::test_ids turned green by editing only its test")
			}
			equal(t, found, true, "the run's notes name the mark", out["notes"])
			// A later run keeps the mark: the validator still has it to judge.
			weakenedRun(t, root, 0)
			equal(t, len(readRecord(t, root)["turned_green"].([]any)), 1, "the mark is carried, once")
		})
	}
}

// A test that is still failing, and a project whose tests were never red,
// mark nothing; a run with no earlier one marks nothing.
func TestExecutionsMarkNothingWithoutARedThenGreen(t *testing.T) {
	t.Parallel()
	root := weakenedWorkspace(t)
	weakenedRun(t, root, 1)
	support.Write(t, root, map[string]string{"b/tests/test_ids.py": "# still red\n"})
	weakenedRun(t, root, 1)
	equal(t, readRecord(t, root)["turned_green"], nil, "still red: no mark")
	judge := object(t, jhonGit(t, root, "context", "c1", "--phase", "none", "--judge").Stdout)
	equal(t, judge["tests_to_judge"], nil)
}
