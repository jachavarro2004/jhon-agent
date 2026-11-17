package execution

import (
	"reflect"
	"testing"
)

// T137 (L1064 KB1): a test's own file is read from its id, pytest's or
// Vitest's, relative to its project or the root; a run's test commands are
// kept with the tests they named failed, except a reuse, a code check, and
// a failure that named none.
func TestTestFileOfAnId(t *testing.T) {
	root := t.TempDir()
	files := []string{"b/tests/test_ids.py", "web/src/a.test.ts", "tests/test_root.py"}
	for _, c := range []struct{ project, test, want string }{
		{"b", "tests/test_ids.py::test_ids", "b/tests/test_ids.py"},
		{"b", "b/tests/test_ids.py::TestX::test_y", "b/tests/test_ids.py"},
		{"web", "src/a.test.ts > ids > stay", "web/src/a.test.ts"},
		{".", "tests/test_root.py::test_r", "tests/test_root.py"},
		{"b", "tests/test_other.py::test_o", ""},
	} {
		if got := testFileOf(root, c.project, c.test, files); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.project, c.test, got, c.want)
		}
	}
}

func TestTestRunsOfARun(t *testing.T) {
	got := testRunsOf([]Execution{
		{Project: "a", Status: Pass},
		{Project: "a", Kind: "lint", Status: Fail, FailedTests: []string{"x::y"}},
		{Project: "b", Status: Fail, FailedTests: []string{"t.py::t"}},
		{Project: "c", Status: Fail},
		{Project: "d", Status: Pass, Reused: true},
		{Project: "e", Status: Timeout},
	})
	want := []TestRun{{"a", nil}, {"b", []string{"t.py::t"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%v, want %v", got, want)
	}
}
