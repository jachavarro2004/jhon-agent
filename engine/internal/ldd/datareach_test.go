package ldd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// T138 (L1054 D1, L1064 KC1-KC4): a slice commit of a `delivery: change`
// whose slice touches a declared data-access path needs a recorded run on
// that tree in which a declared real-database test reached the changed file,
// by pytest-testmon's record, by file.

const dataReachDeclared = "delivery: change\nexecutions_data_access: [app/repo/**]\nexecutions_database_tests: [app/tests/db/*.py]\n"

// dataReachRepo is scenarioRepo with app/repo/ on the default branch, merged
// into the feature branch (so the change's base holds it), the cache in a
// temporary directory, and config as its openspec/config.yaml.
func dataReachRepo(t *testing.T, config string) string {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 reads the pytest-testmon record")
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := scenarioRepo(t)
	gitT(t, root, "checkout", "-q", "main")
	write(t, filepath.Join(root, "app/repo/query.py"), "def page(): return []\n")
	write(t, filepath.Join(root, "app/repo/other.py"), "X = 1\n")
	write(t, filepath.Join(root, "openspec/config.yaml"), config)
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "the repository layer")
	gitT(t, root, "checkout", "-q", "feature")
	gitT(t, root, "merge", "-q", "--no-edit", "main")
	return root
}

// testmonOf writes app's pytest-testmon record, in the schema precise.go
// reads: each test (its name) and the files its last execution ran.
func testmonOf(t *testing.T, root string, tests map[string][]string) {
	t.Helper()
	script := `import sqlite3, sys, json
c = sqlite3.connect(sys.argv[1])
c.executescript("create table test_execution (id integer primary key, environment_id integer, test_name text, duration float, failed bit, forced bit); create table file_fp (id integer primary key, filename text, method_checksums blob, mtime float, fsha text); create table test_execution_file_fp (test_execution_id integer, fingerprint_id integer)")
fp = {}
for name, files in json.loads(sys.argv[2]).items():
    tid = c.execute("insert into test_execution (environment_id, test_name, duration, failed, forced) values (1, ?, 1, 0, 0)", (name,)).lastrowid
    for f in files:
        if f not in fp:
            fp[f] = c.execute("insert into file_fp (filename, method_checksums, mtime, fsha) values (?, x'00', 0, 'sha')", (f,)).lastrowid
        c.execute("insert into test_execution_file_fp values (?, ?)", (tid, fp[f]))
c.commit()`
	var b strings.Builder
	b.WriteString("{")
	first := true
	for name, files := range tests {
		if !first {
			b.WriteString(",")
		}
		first = false
		b.WriteString(`"` + name + `": ["` + strings.Join(files, `", "`) + `"]`)
	}
	b.WriteString("}")
	data := execution.TestmonData(root, "app")
	os.MkdirAll(filepath.Dir(data), 0o755)
	os.Remove(data)
	if out, err := exec.Command("python3", "-I", "-c", script, data, b.String()).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
}

// ranOnTree records a passing run of the working tree as it is, whose one
// test execution is app's command.
func ranOnTree(t *testing.T, root, command string) {
	t.Helper()
	tree := vcs.WorkingTree(root, execution.ChangeRecordPaths(root, "c1"))
	if tree == "" {
		t.Fatal("the working tree cannot be read")
	}
	write(t, execution.RecordFile(root, "c1"), `{"complete": true, "scope": "affected", "working_tree": "`+tree+`", "after_working_tree": "`+tree+`", "executions": [{"project": "app", "command": "`+command+`", "status": "pass"}]}`+"\n")
}

// closeDataSlice changes app/repo/query.py, checks off slice 1, records
// command's run, and asks whether the slice commit may run.
func closeDataSlice(t *testing.T, root, command string) string {
	t.Helper()
	write(t, filepath.Join(root, "app/repo/query.py"), "def page(): return [(1, 2, 3, 4)]\n")
	write(t, filepath.Join(root, "openspec/changes/c1/tasks.md"), scenarioFirst)
	ranOnTree(t, root, command)
	return SliceCommitRefusal(root)
}

const (
	testmonRun       = "uv run --with pytest-testmon pytest --testmon"
	dataReachRefusal = "this commit closes Slice 1: a of c1, and it changes app/repo/query.py under a declared data-access path (executions_data_access), and no test declared as real-database (executions_database_tests) reached it in the recorded run on this tree, by pytest-testmon's record: a test declared as real-database must reach it in the recorded run. Write or select one that runs it against the database, run `jhon executions run c1 --affected <base>` on the slice as it will be committed, then commit."
)

// KC2: only a unit test with a fake session reaches the changed query: refused,
// the reason naming the file and the kind of test it needs.
func TestASliceCommitTouchingDataAccessNeedsARealDatabaseTestThatReachesIt(t *testing.T) {
	root := dataReachRepo(t, dataReachDeclared)
	testmonOf(t, root, map[string][]string{
		"app/tests/unit/test_query.py::test_compiled_sql": {"app/repo/query.py"},
		"app/tests/db/test_export.py::test_export":        {"app/repo/other.py"},
	})
	if got := closeDataSlice(t, root, testmonRun); got != dataReachRefusal {
		t.Errorf("no real-database test reaches the query:\n got %q\nwant %q", got, dataReachRefusal)
	}
	// KC1: the session's brief names the declaration beside executions_always.
	if got, want := ProjectBrief(root), "- executions_data_access: [app/repo/**]; executions_database_tests: [app/tests/db/*.py]. A slice commit that changes a file under a data-access path needs its recorded run to have executed a real-database test whose pytest-testmon record reaches that file; a test that compiles the query against a fake session does not."; !strings.Contains(got, want) {
		t.Errorf("the brief lacks %q:\n%s", want, got)
	}
	// A commit inside the slice is never refused for it.
	write(t, filepath.Join(root, "openspec/changes/c1/tasks.md"), scenarioOpen)
	if got := SliceCommitRefusal(root); got != "" {
		t.Errorf("a commit inside a slice: %q", got)
	}
}

// KC2: a declared real-database test reaches it in the run: the commit runs.
func TestASliceCommitWhoseRealDatabaseTestReachesTheChangeRuns(t *testing.T) {
	root := dataReachRepo(t, dataReachDeclared)
	testmonOf(t, root, map[string][]string{
		"app/tests/unit/test_query.py::test_compiled_sql": {"app/repo/query.py"},
		"app/tests/db/test_export.py::test_export":        {"app/repo/query.py", "app/repo/other.py"},
	})
	if got := closeDataSlice(t, root, testmonRun); got != "" {
		t.Errorf("a real-database test reaches the query: %q", got)
	}
	// A run that named only the change's own test modules did not run it.
	own := "uv run pytest 'tests/unit/test_query.py'"
	if got := closeDataSlice(t, root, own); got != dataReachRefusal {
		t.Errorf("a run that did not execute the real-database test:\n got %q\nwant %q", got, dataReachRefusal)
	}
	if got := closeDataSlice(t, root, own+" 'tests/db/test_export.py'"); got != "" {
		t.Errorf("a run that named the real-database test: %q", got)
	}
	// A change outside the data-access paths asks for nothing.
	write(t, filepath.Join(root, "app/repo/query.py"), "def page(): return []\n")
	write(t, filepath.Join(root, "app/service.py"), "Y = 2\n")
	ranOnTree(t, root, own)
	if got := SliceCommitRefusal(root); got != "" {
		t.Errorf("a change outside the data-access paths: %q", got)
	}
}

// KC3: without the declaration, the same slice commit runs as before.
func TestUndeclaredDataAccessNeverRefusesASliceCommit(t *testing.T) {
	root := dataReachRepo(t, "delivery: change\n")
	testmonOf(t, root, map[string][]string{"app/tests/unit/test_query.py::test_compiled_sql": {"app/repo/query.py"}})
	if got := closeDataSlice(t, root, testmonRun); got != "" {
		t.Errorf("no declaration: %q", got)
	}
	if got := ProjectBrief(root); strings.Contains(got, "executions_data_access") {
		t.Errorf("no declaration in the brief:\n%s", got)
	}
	change := "c1"
	if got := notes(statusOf(t, root, &change)); strings.Contains(got, "data-access") {
		t.Errorf("no declaration, no note:\n%s", got)
	}
}

const dataReachUnchecked = "app/repo/query.py is under a declared data-access path (executions_data_access), and whether a test declared as real-database (executions_database_tests) reached it in the recorded run could not be checked for lack of a pytest-testmon record of its tests; a slice commit is not refused for it"

// KC3: with no testmon record, the commit is not refused, and `jhon status`
// (and the run, DataReachNote) names the reach it could not check.
func TestNoTestmonRecordLeavesTheReachUncheckedAndSaysSo(t *testing.T) {
	root := dataReachRepo(t, dataReachDeclared)
	if got := closeDataSlice(t, root, testmonRun); got != "" {
		t.Errorf("no record: %q", got)
	}
	if got := DataReachNote(root, "c1"); got != dataReachUnchecked {
		t.Errorf("the run's note:\n got %q\nwant %q", got, dataReachUnchecked)
	}
	change := "c1"
	if got := notes(statusOf(t, root, &change)); !strings.Contains(got, dataReachUnchecked) {
		t.Errorf("status notes lack %q:\n%s", dataReachUnchecked, got)
	}
	// With a record, the note is gone.
	testmonOf(t, root, map[string][]string{"app/tests/db/test_export.py::test_export": {"app/repo/query.py"}})
	if got := DataReachNote(root, "c1"); got != "" {
		t.Errorf("a usable record: %q", got)
	}
}

// T145 E1: a wide run's pytest-testmon command that names the touched
// modules ran only those; one that names none ran whatever its record
// selected; a whole or own run as before.
func TestRanModuleReadsATestmonCommandThatNamesModules(t *testing.T) {
	testmon := "mkdir -p '/c' && TESTMON_DATAFILE='/c/x.db' uv run --with pytest-testmon pytest --testmon --rootdir='/r'"
	for _, c := range []struct {
		command string
		want    bool
	}{
		{testmon, true},
		{"uv run pytest", true},
		{"uv run pytest 'tests/db/test_q.py'", true},
		{"uv run pytest 'tests/test_u.py'", false},
		{testmon + " 'tests/db/test_q.py'", true},
		{testmon + " 'tests/test_u.py'", false},
		{"uv run pytest 'tests/db/test_q.py' && " + testmon + " 'tests/test_u.py'", true},
		{"uv run pytest 'tests/test_doc.py' && " + testmon, true},
	} {
		if got := ranModule(c.command, true, "svc", "svc/tests/db/test_q.py"); got != c.want {
			t.Errorf("ranModule(%q) = %v, want %v", c.command, got, c.want)
		}
	}
}

// T145c H1 (L1191): a module counts as run only when its part of the
// command really ran: a wide step's parts (execution.BothCommand) both run
// whatever either's status, in either order; after a failed `&&` part the
// rest may not have run, so only the first counts; a pytest-testmon part
// that names no module ran whatever its record selected, wherever it is.
func TestRanModuleCountsAPartOnlyWhenItRan(t *testing.T) {
	testmon := "mkdir -p '/c' && TESTMON_DATAFILE='/c/x.db' uv run --with pytest-testmon pytest --testmon --rootdir='/r'"
	q, u := "'tests/db/test_q.py'", "'tests/test_u.py'"
	for _, c := range []struct {
		command string
		passed  bool
		want    bool
	}{
		{execution.BothCommand("uv run pytest "+u, testmon+" "+q), false, true},
		{execution.BothCommand("uv run pytest "+q, testmon+" "+u), false, true},
		{execution.BothCommand(testmon+" "+q, "uv run pytest "+u), false, true},
		{execution.BothCommand(testmon+" "+u, "uv run pytest "+q), false, true},
		{execution.BothCommand("uv run pytest "+u, testmon+" 'tests/test_v.py'"), true, false},
		{"uv run pytest 'tests/test_doc.py' && " + testmon, true, true},
		{"uv run pytest 'tests/test_doc.py' && " + testmon, false, false},
		{"uv run pytest " + q + " && " + testmon, false, true},
		{"uv run pytest " + u + " && " + testmon + " " + q, false, false},
		{testmon + " && uv run pytest " + u, true, true},
		{testmon + " && uv run pytest " + u, false, true},
		{testmon + " " + u + " && uv run pytest " + q, false, false},
		{testmon + " " + u + " && uv run pytest " + q, true, true},
		{"uv run pytest " + u, false, false},
		{"uv run pytest", false, true},
	} {
		if got := ranModule(c.command, c.passed, "svc", "svc/tests/db/test_q.py"); got != c.want {
			t.Errorf("ranModule(%q, passed %v) = %v, want %v", c.command, c.passed, got, c.want)
		}
	}
}
