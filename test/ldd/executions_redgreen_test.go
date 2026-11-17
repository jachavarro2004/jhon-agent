package ldd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// A change's RED before GREEN (T31): `jhon executions` keeps each earlier
// complete run's outcome in the record's `history`, and says whether a
// failing run preceded the last one, which passed (`red_before_green`).
// `red` in the root makes project a's tests fail.
func TestExecutionsRedBeforeGreen(t *testing.T) {
	t.Parallel()
	run := func(t *testing.T, root string, red bool) map[string]any {
		t.Helper()
		flag := filepath.Join(root, "red")
		if red {
			support.Write(t, root, map[string]string{"red": ""})
		} else {
			os.Remove(flag)
		}
		r := jhonGit(t, root, "executions", "run", "c1", "--json", "--force")
		want := 0
		if red {
			want = 1
		}
		equal(t, r.Code, want, r.Stderr)
		return object(t, r.Stdout)
	}
	for _, c := range []struct {
		name string
		runs []bool
		want bool
	}{
		{"fail then pass", []bool{true, false}, true},
		{"pass only", []bool{false, false}, false},
		{"pass then fail", []bool{false, true}, false},
		{"fail, pass, then fail", []bool{true, false, true}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := workspace(t, "a", "test ! -f ../red", "b", "true", "c", "true")
			var out map[string]any
			for _, red := range c.runs {
				out = run(t, root, red)
			}
			equal(t, out["red_before_green"], c.want, "the run's summary")
			equal(t, len(readRecord(t, root)["history"].([]any)), len(c.runs)-1, "each earlier complete run is kept")
			tests := field(t, []byte(jhonGit(t, root, "status", "c1", "--json").Stdout), "tests")
			equal(t, object(t, string(tests))["red_before_green"], c.want, "status's tests")
		})
	}
}

// A record written before the history existed reads as no RED known, and
// is still read: its last run, and a new run that keeps its outcome.
func TestExecutionsOldRecordHasNoRed(t *testing.T) {
	t.Parallel()
	root := workspace(t, "a", "true", "b", "true", "c", "true")
	support.Write(t, root, map[string]string{record: `{"schema":"jhon.execution-record/v2","timestamp":"2026-01-01T00:00:00.000Z","complete":true,"scope":"full","base":null,"scope_reasons":[],"selection":[],"timeout_minutes":20,"candidate_commit":"","candidate_tree":"","working_tree_clean":false,"after":null,"dirty":false,"executions":[{"project":"a","command":"false","cwd":"a","exit_code":1,"status":"fail","duration_ms":1}]}` + "\n"})
	tests := object(t, string(field(t, []byte(jhonGit(t, root, "status", "c1", "--json").Stdout), "tests")))
	equal(t, tests["passed"], false, "the old record's last run is read")
	equal(t, tests["red_before_green"], false)
	r := jhonGit(t, root, "executions", "run", "c1", "--json")
	equal(t, r.Code, 0, r.Stderr)
	equal(t, object(t, r.Stdout)["red_before_green"], true, "the old record's failing run is kept as history")
}
