// T255: the pytest-testmon record shared by every worktree of a repository,
// and a record that cannot converge (odd/tasks/t255.md S1, L1724, L1725).
//
// Each worktree (one per change, removed after) narrows from one record per
// repository and project; a run writes a private copy and replaces the
// record only when pytest ran to its end (1, 2); a worktree's own record
// from before is adopted (3); a project with no record is named, with its
// tests, before it runs whole (4); a run whose failures are the
// environment's keeps no record (5); a record that marks most tests failed
// is named with the command that drops it (6); and a run that has to end
// once for the record to narrow gets a longer deadline, while a cut run
// says how far it got (7).
//
// No pytest runs here but in the last test, which needs uv and
// pytest-testmon offline: the fake uv of executions_unit_test.go.
package ldd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// worktree is a second worktree of the project's repository, on a branch of
// its own off feat, with its uv faked as the project's.
func (p *pytestProject) worktree(t *testing.T) *pytestProject {
	t.Helper()
	wt := filepath.Join(temp(t), "wt")
	support.Git(t, p.root, "worktree", "add", "-q", "-b", "feat2", wt, "feat")
	q := *p
	q.root = wt
	return &q
}

// otherRecord is a record that differs from what the fake uv writes by
// default (p.made): a run that replaces the shared record with it shows.
func (p *pytestProject) otherRecord(t *testing.T, failed int) string {
	t.Helper()
	db := filepath.Join(filepath.Dir(p.log), "other-"+strconv.Itoa(failed)+".db")
	writeRecord(t, db, failed)
	return db
}

// writeRecord writes at db a record of lib's 10 tests, failed of them marked
// failed, all of which ran lib/other.py only: a change to lib/util.py
// reaches none of them.
func writeRecord(t *testing.T, db string, failed int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(db), 0o755); err != nil {
		t.Fatal(err)
	}
	os.Remove(db)
	script := `import sqlite3, sys
c = sqlite3.connect(sys.argv[1])
c.executescript("create table test_execution (id integer primary key, environment_id integer, test_name text, duration float, failed bit, forced bit); create table file_fp (id integer primary key, filename text, method_checksums blob, mtime float, fsha text); create table test_execution_file_fp (test_execution_id integer, fingerprint_id integer)")
c.execute("insert into file_fp values (1, 'lib/other.py', x'00', 0, 'b')")
for i in range(10):
    c.execute("insert into test_execution values (?, 1, ?, 6, ?, 0)", (i, "lib/tests/test_other.py::t%d" % i, 1 if i < int(sys.argv[2]) else 0))
    c.execute("insert into test_execution_file_fp values (?, 1)", (i,))
c.commit()`
	if out, err := exec.Command("python3", "-I", "-c", script, db, strconv.Itoa(failed)).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
}

// leftBeside is what a run left in the records' folder but the records.
func leftBeside(t *testing.T, p *pytestProject) []string {
	t.Helper()
	var out []string
	entries, _ := os.ReadDir(filepath.Dir(p.data))
	for _, e := range entries {
		if n := filepath.Join(filepath.Dir(p.data), e.Name()); n != p.data && n != p.legacy {
			out = append(out, e.Name())
		}
	}
	return out
}

// shared reads the shared record's bytes.
func (p *pytestProject) shared(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(p.data)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// planJSON is a dry run's JSON, as the tests here read it.
type planJSON struct {
	Plan []struct {
		Project, Kind, Command string
		TimeoutMinutes         float64 `json:"timeout_minutes"`
	}
	Projects []struct {
		Project, Tests string
		FirstRun       *struct {
			Record          string
			Tests, Modules  int
			DeadlineMinutes float64 `json:"deadline_minutes"`
		} `json:"first_run"`
		FailedMarks *struct{ Failed, Total int } `json:"failed_marks"`
	}
	Notes []string
}

func (p *pytestProject) planJSON(t *testing.T, args ...string) planJSON {
	t.Helper()
	r := p.jhon(t, nil, append([]string{"executions", "run", "c1", "--dry-run", "--json"}, args...)...)
	equal(t, r.Code, 0, r.Stderr)
	var out planJSON
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatal(err, r.Stdout)
	}
	return out
}

// T255 (1, 2; L1724): a new worktree of the repository narrows from the
// record another worktree built, instead of running every test of each
// affected project once to build its own; its run works on a private copy
// of the record, which replaces it when pytest ran to its end.
func TestExecutionsASecondWorktreeNarrowsFromTheSharedRecord(t *testing.T) {
	t.Parallel()
	p := newPytestProject(t)
	equal(t, p.jhon(t, nil, "executions", "prime").Code, 0, "the main checkout builds the record")
	built := p.shared(t)

	wt := p.worktree(t)
	command, tests, notes := wt.plan(t, "--affected", "base")
	equal(t, command, wt.narrowed(), "the second worktree narrows from the shared record")
	equal(t, tests, "testmon")
	noMatch(t, notes, `no record`)

	other := p.otherRecord(t, 0)
	r := wt.jhon(t, []string{"FAKE_UV_RECORD=" + other}, "executions", "run", "c1", "--affected", "base", "--json")
	equal(t, r.Code, 0, r.Out())
	equal(t, wt.calls(t)[1:], []string{p.data + ".run|run --with pytest-testmon --extra dev pytest --testmon --rootdir=" + wt.root},
		"pytest-testmon ran once, narrowed, on a private copy of the shared record")
	if p.shared(t) == built {
		t.Error("the run that ended did not replace the shared record")
	}
	b, _ := os.ReadFile(other)
	equal(t, p.shared(t), string(b), "the shared record is the run's")
	equal(t, leftBeside(t, p), []string(nil), "no private copy is left")
	command, _, _ = p.plan(t, "--affected", "base")
	equal(t, command, p.narrowed(), "the main checkout narrows from the same record")
}

// T255 (2): a run that did not end (pytest interrupted, exit 2, or cut by
// its deadline) leaves the shared record as it was.
func TestExecutionsARunThatDidNotEndLeavesTheSharedRecord(t *testing.T) {
	t.Parallel()
	p := newPytestProject(t)
	equal(t, p.jhon(t, nil, "executions", "prime").Code, 0)
	built := p.shared(t)
	other := p.otherRecord(t, 0)
	for _, c := range []struct {
		name string
		env  []string
		args []string
	}{
		{"interrupted", []string{"FAKE_UV_EXIT=2"}, nil},
		{"cut by its deadline", []string{"FAKE_UV_SLEEP=30"}, []string{"--timeout", "0.01"}},
	} {
		r := p.jhon(t, append([]string{"FAKE_UV_RECORD=" + other}, c.env...), append([]string{"executions", "run", "c1", "--affected", "base", "--json"}, c.args...)...)
		equal(t, r.Code, 1, c.name, r.Out())
		equal(t, p.calls(t)[len(p.calls(t))-1], p.data+".run|run --with pytest-testmon --extra dev pytest --testmon --rootdir="+p.root, c.name)
		equal(t, p.shared(t), built, c.name+": the shared record is as it was")
		equal(t, leftBeside(t, p), []string(nil), c.name+": no private copy is left")
	}
}

// T255 (3): a worktree's own record from before the shared one is adopted
// when the shared one is missing: copied, the old one left where it was.
// T256 (L1726): by a run, never a dry run, which writes nothing at all and
// reads the old record as the run will.
func TestExecutionsAdoptsTheWorktreesOwnRecord(t *testing.T) {
	t.Parallel()
	p := newPytestProject(t)
	b, err := os.ReadFile(p.made)
	if err != nil {
		t.Fatal(err)
	}
	support.Write(t, filepath.Dir(p.legacy), map[string]string{filepath.Base(p.legacy): string(b)})
	before := p.untouched(t)
	command, tests, _ := p.plan(t, "--affected", "base")
	equal(t, command, p.narrowed(), "the old record narrows")
	equal(t, tests, "testmon")
	human := p.jhon(t, nil, "executions", "run", "c1", "--affected", "base", "--dry-run")
	equal(t, human.Code, 0, human.Out())
	match(t, human.Stdout, `(?m)^would run  lib: sh -c '`+regexp.QuoteMeta(p.narrowed())+`'`)
	equal(t, p.untouched(t), before, "a dry run writes nothing")
	equal(t, exists(p.data), false, "a dry run adopts nothing")

	r := p.jhon(t, nil, "executions", "run", "c1", "--affected", "base", "--json")
	equal(t, r.Code, 0, r.Out())
	equal(t, p.calls(t), []string{p.data + ".run|run --with pytest-testmon --extra dev pytest --testmon --rootdir=" + p.root}, "the run narrows from the adopted record")
	equal(t, p.shared(t), string(b), "the shared record is a copy of it")
	equal(t, exists(p.legacy), true, "the old record is left where it was")
}

// untouched is Jhon's cache, every file with its bytes and mode, and the
// project's git status: what a command that writes nothing leaves as it
// found them.
func (p *pytestProject) untouched(t *testing.T) []string {
	t.Helper()
	return append(snapshot(t, filepath.Dir(filepath.Dir(filepath.Dir(p.data))), true), support.Git(t, p.root, "status", "--porcelain", "--untracked-files=all"))
}

// T256 (L1726): what a killed jhon left beside the records (a first run's
// file, a private copy) is swept by the next run when its process is gone,
// and the record the run publishes is 0644, readable by every worktree.
func TestExecutionsSweepsWhatAKilledRunLeftAndPublishesTheRecord0644(t *testing.T) {
	t.Parallel()
	p := newPytestProject(t)
	equal(t, p.jhon(t, nil, "executions", "prime").Code, 0)
	c := exec.Command("true")
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	dead := strconv.Itoa(c.Process.Pid)
	left := map[string]string{}
	for _, f := range []string{".building", ".building-wal", "-3.run", "-3.run-shm"} {
		left[filepath.Base(p.data)+"."+dead+f] = "left by a killed run"
	}
	support.Write(t, filepath.Dir(p.data), left)
	r := p.jhon(t, nil, "executions", "run", "c1", "--affected", "base", "--json")
	equal(t, r.Code, 0, r.Out())
	equal(t, leftBeside(t, p), []string(nil), "the killed run's files are swept")
	st, err := os.Stat(p.data)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, st.Mode().Perm(), os.FileMode(0o644), "the published record's mode")
}

// T256 (L1726): a run that names its deadline (--timeout) gives a first run
// no deadline of its own: `deadline_minutes` is absent, not 0.
func TestExecutionsFirstRunDeadlineIsAbsentWithATimeout(t *testing.T) {
	t.Parallel()
	p := newPytestProject(t)
	r := p.jhon(t, nil, "executions", "run", "c1", "--affected", "base", "--dry-run", "--json", "--timeout", "5")
	equal(t, r.Code, 0, r.Out())
	var out struct {
		Projects []struct {
			Project  string
			FirstRun map[string]any `json:"first_run"`
		}
	}
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatal(err, r.Stdout)
	}
	var first map[string]any
	for _, pp := range out.Projects {
		if pp.Project == "lib" {
			first = pp.FirstRun
		}
	}
	equal(t, first, map[string]any{"record": "none", "tests": 1.0, "modules": 1.0}, "first_run with --timeout")
}

// T256 (L1726): the estimate of what pytest-testmon would run counts the
// tests its record marks failed, which it runs again whatever changed, so it
// agrees with the note on them.
func TestExecutionsEstimateCountsTheFailedTests(t *testing.T) {
	t.Parallel()
	p := newPytestProject(t)
	writeRecord(t, p.data, 3)
	out := p.planJSON(t, "--affected", "base")
	notes := strings.Join(out.Notes, "\n")
	match(t, notes, `(?m)^lib: every test pytest-testmon selects is one its record marks failed \(3\)`)
	match(t, notes, `(?m)^lib: pytest-testmon would run about 3 of its 10 tests, 18 s of 1 min by the last recorded durations$`)
	r := p.jhon(t, nil, "executions", "run", "c1", "--affected", "base", "--dry-run", "--json")
	var est struct {
		Projects []struct {
			Project  string
			Estimate *struct{ Tests, Total int } `json:"estimate"`
		}
	}
	if err := json.Unmarshal([]byte(r.Stdout), &est); err != nil {
		t.Fatal(err, r.Stdout)
	}
	got := ""
	for _, pp := range est.Projects {
		if pp.Project == "lib" && pp.Estimate != nil {
			got = strconv.Itoa(pp.Estimate.Tests) + "/" + strconv.Itoa(pp.Estimate.Total)
		}
	}
	equal(t, got, "3/10", "the estimate as data")
}

// T256 (L1726): a refusal a passing test logged (a client that retries) is
// not the failures' cause: with tests passing and the refusal outside the
// failures, the record is kept.
func TestExecutionsKeepsTheRecordWhenARefusalIsNotTheFailures(t *testing.T) {
	t.Parallel()
	p := newPytestProject(t)
	equal(t, p.jhon(t, nil, "executions", "prime").Code, 0)
	failed := p.otherRecord(t, 1)
	say := `WARNING  client:retry.py:12 Connection refused, retrying\n==================== FAILURES ====================\nE   assert 1 == 2\n==== 1 failed, 9 passed in 0.20s ====`
	r := p.jhon(t, []string{"FAKE_UV_RECORD=" + failed, "FAKE_UV_EXIT=1", "FAKE_UV_SAY=" + say}, "executions", "run", "c1", "--affected", "base", "--json")
	equal(t, r.Code, 1, r.Out())
	noMatch(t, r.Stderr, `not kept`)
	b, _ := os.ReadFile(failed)
	equal(t, p.shared(t), string(b), "the run's record is kept")
}

// T255 verify, T256 (L1726): `jhon executions forget` drops the shared
// record and every worktree's own from before, and only its project's: an
// odd name (a path into or out of a project, another project, a
// non-pytest one) is refused and drops nothing.
func TestExecutionsForgetDropsEveryWorktreesRecordAndRefusesOtherNames(t *testing.T) {
	t.Parallel()
	p := newPytestProject(t)
	wt := p.worktree(t)
	cache := filepath.Dir(filepath.Dir(filepath.Dir(p.data)))
	wtLegacy := testmonData(cache, wt.root, "lib")
	others := []string{testmonData(cache, filepath.Join(p.root, ".git"), "app"), testmonData(cache, filepath.Join(p.root, ".git"), "lib/tests"),
		testmonData(cache, filepath.Join(temp(t), ".git"), "lib")}
	all := append([]string{p.data, p.legacy, wtLegacy}, others...)
	for _, f := range all {
		writeRecord(t, f, 10)
	}
	for _, name := range []string{"../lib", "lib/tests", "./lib", "lib/..", ".", "app", "LIB", wt.root + "/lib", ""} {
		r := wt.jhon(t, nil, "executions", "forget", name)
		equal(t, r.Code, 2, name, r.Out())
		equal(t, r.Stderr, "jhon executions forget: "+name+" is not a uv-run pytest project here: lib\n", name)
		for _, f := range all {
			equal(t, exists(f), true, name+": "+f+" is kept")
		}
	}
	r := wt.jhon(t, nil, "executions", "forget", "lib")
	equal(t, r.Code, 0, r.Out())
	equal(t, []bool{exists(p.data), exists(p.legacy), exists(wtLegacy)}, []bool{false, false, false}, "the shared record and every worktree's own")
	for _, f := range others {
		equal(t, exists(f), true, f+": another project's or repository's record is kept")
	}
}

// T255 (4; L1723): a project with no usable record runs all its tests once:
// the dry run names it with how many tests it has, and the run says so
// before it starts.
func TestExecutionsNamesAProjectWithNoRecordAndItsTests(t *testing.T) {
	t.Parallel()
	p := newPytestProject(t)
	support.Write(t, p.root, map[string]string{"lib/tests/test_more.py": "def test_a():\n    pass\n\n\nclass TestB:\n    async def test_b(self):\n        pass\n"})
	out := p.planJSON(t, "--affected", "base")
	match(t, strings.Join(out.Notes, "\n"), `(?m)^lib: pytest-testmon has no record of it yet, so all its tests run once \(3 test functions in 2 modules\) and become its record, under the first-run deadline of 180 min; later runs are narrowed$`)
	noMatch(t, strings.Join(out.Notes, "\n"), `every test runs once`)
	var first string
	for _, pp := range out.Projects {
		if pp.Project == "lib" && pp.FirstRun != nil {
			first = pp.FirstRun.Record + " " + strconv.Itoa(pp.FirstRun.Tests) + " " + strconv.Itoa(pp.FirstRun.Modules) + " " + strconv.FormatFloat(pp.FirstRun.DeadlineMinutes, 'f', -1, 64)
		}
	}
	equal(t, first, "none 3 2 180", "the panel reads it as data")

	human := p.jhon(t, nil, "executions", "run", "c1", "--affected", "base", "--dry-run")
	equal(t, human.Code, 0, human.Out())
	match(t, human.Stdout, `(?m)^note  lib: pytest-testmon has no record of it yet, so all its tests run once \(3 test functions in 2 modules\)`)

	r := p.jhon(t, nil, "executions", "run", "c1", "--affected", "base")
	equal(t, r.Code, 0, r.Out())
	match(t, r.Stdout, `(?m)^\.\.\. lib: pytest-testmon has no record of it yet, so all its tests run once \(3 test functions in 2 modules\) and become its record, under the first-run deadline of 180 min; later runs are narrowed\n==> lib: `)
}

// envFailed is what pytest says when every test failed: the test services
// were down.
const envFailed = `==== 577 failed, 2 warnings in 5100.12s (1:25:00) ====`

// refusedNote is what a run says of a record it does not keep.
const refusedNote = `lib: the record pytest-testmon wrote in this run is not kept: every one of its 577 tests failed, which points at the test environment, not the change: are the test services (a database, a cache) up? The record stays as it was`

// T255 (5; L1725): a run whose failures are the test environment's (every
// test failing, a service refusing the connection) does not replace the
// record: a record that marks every test failed makes every later run run
// them all. A first run's builds none, and is not taken for a cut one.
func TestExecutionsKeepsNoRecordOfARunWhoseServicesWereDown(t *testing.T) {
	t.Parallel()
	p := newPytestProject(t)
	equal(t, p.jhon(t, nil, "executions", "prime").Code, 0)
	built := p.shared(t)
	failed := p.otherRecord(t, 10)
	r := p.jhon(t, []string{"FAKE_UV_RECORD=" + failed, "FAKE_UV_EXIT=1", "FAKE_UV_SAY=" + envFailed}, "executions", "run", "c1", "--affected", "base", "--json")
	equal(t, r.Code, 1, r.Out())
	match(t, r.Stderr, `(?m)^\.\.\. `+regexp.QuoteMeta(refusedNote)+`$`)
	match(t, strings.Join(notesOf(t, r.Stdout), "\n"), `(?m)^`+regexp.QuoteMeta(refusedNote)+`$`)
	equal(t, p.shared(t), built, "the shared record is as it was")
	equal(t, leftBeside(t, p), []string(nil))

	refused := "FAILED tests/test_util.py::test_x - psycopg.OperationalError: connection failed: Connection refused"
	r = p.jhon(t, []string{"FAKE_UV_RECORD=" + failed, "FAKE_UV_EXIT=1", "FAKE_UV_SAY=" + refused + "\n==== 1 failed in 0.20s ===="}, "executions", "run", "c1", "--affected", "base", "--json")
	equal(t, r.Code, 1, r.Out())
	match(t, r.Stderr, `(?m)^\.\.\. lib: the record pytest-testmon wrote in this run is not kept: its output says a test service refused the connection \(`+regexp.QuoteMeta(refused)+`\), which points at the test environment`)
	equal(t, p.shared(t), built, "the shared record is as it was")

	// A unit's RED, a few tests failing, is the change's: its record is kept.
	r = p.jhon(t, []string{"FAKE_UV_RECORD=" + failed, "FAKE_UV_EXIT=1", "FAKE_UV_SAY===== 1 failed in 0.20s ===="}, "executions", "run", "c1", "--affected", "base", "--json")
	equal(t, r.Code, 1, r.Out())
	noMatch(t, r.Stderr, `not kept`)
	b, _ := os.ReadFile(failed)
	equal(t, p.shared(t), string(b), "a RED run's record is kept")

	// A first run whose services were down builds no record, and the next
	// run tries again: it is not a first run cut by its deadline.
	q := newPytestProject(t)
	r = q.jhon(t, []string{"FAKE_UV_EXIT=1", "FAKE_UV_SAY=" + envFailed}, "executions", "run", "c1", "--affected", "base", "--json")
	equal(t, r.Code, 1, r.Out())
	match(t, r.Stderr, `(?m)^\.\.\. `+regexp.QuoteMeta(refusedNote)+`$`)
	equal(t, exists(q.data), false, "no record is built")
	command, _, _ := q.plan(t, "--affected", "base")
	equal(t, command, q.collecting(), "the next run builds it")
	equal(t, leftBeside(t, q), []string(nil))

	// prime, too.
	r = q.jhon(t, []string{"FAKE_UV_EXIT=1", "FAKE_UV_SAY=" + envFailed}, "executions", "prime")
	equal(t, r.Code, 1, r.Out())
	match(t, r.Stdout, `(?m)^<== lib: exit 1; no record was kept: every one of its 577 tests failed, which points at the test environment, not the change: are the test services \(a database, a cache\) up\?$`)
	equal(t, exists(q.data), false)
}

// mostlyFailedNote is what a run says of a record that marks most of lib's
// tests failed.
const mostlyFailedNote = "lib: pytest-testmon's record marks 10 of its 10 tests failed, and pytest-testmon runs every failed test again whatever changed: a run while the test services were down (connection refused) leaves such a record; with them up, a run that ends repairs it, or `jhon executions forget lib` drops it and the next run builds it again"

// T255 (6; L1725): a record that marks most tests failed, so every run runs
// them all whatever changed, is named in the dry run and the run, with its
// cause and `jhon executions forget`, which drops it.
func TestExecutionsNamesARecordThatMarksMostTestsFailed(t *testing.T) {
	t.Parallel()
	p := newPytestProject(t)
	writeRecord(t, p.data, 10)
	out := p.planJSON(t, "--affected", "base")
	match(t, strings.Join(out.Notes, "\n"), `(?m)^`+regexp.QuoteMeta(mostlyFailedNote)+`$`)
	var marks string
	for _, pp := range out.Projects {
		if pp.Project == "lib" && pp.FailedMarks != nil {
			marks = strconv.Itoa(pp.FailedMarks.Failed) + "/" + strconv.Itoa(pp.FailedMarks.Total)
		}
	}
	equal(t, marks, "10/10")
	r := p.jhon(t, nil, "executions", "run", "c1", "--affected", "base")
	equal(t, r.Code, 0, r.Out())
	match(t, r.Stdout, `(?m)^\.\.\. `+regexp.QuoteMeta(mostlyFailedNote)+`\n(?:\.\.\. [^\n]*\n)*==> lib: `)

	// A record with a few failed tests is a change's RED, not named.
	writeRecord(t, p.data, 2)
	noMatch(t, strings.Join(p.planJSON(t, "--affected", "base").Notes, "\n"), `forget`)

	// forget drops the record, and the worktree's own from before, which
	// would otherwise be adopted again.
	writeRecord(t, p.data, 10)
	writeRecord(t, p.legacy, 10)
	r = p.jhon(t, nil, "executions", "forget", "lib")
	equal(t, r.Code, 0, r.Out())
	equal(t, r.Stdout, "jhon executions forget: lib's pytest-testmon record is dropped ("+p.data+"); the next affected run builds it again, every test once\n")
	equal(t, []bool{exists(p.data), exists(p.legacy)}, []bool{false, false})
	command, _, _ := p.plan(t, "--affected", "base")
	equal(t, command, p.collecting(), "the next run builds the record")

	r = p.jhon(t, nil, "executions", "forget", "lib")
	equal(t, r.Code, 0, r.Out())
	equal(t, r.Stdout, "jhon executions forget: lib has no pytest-testmon record; nothing to drop\n")
	for _, bad := range [][]string{{"forget"}, {"forget", "lib", "extra"}, {"forget", "--root"}} {
		r := p.jhon(t, nil, append([]string{"executions"}, bad...)...)
		equal(t, r.Code, 2, bad)
		match(t, r.Stderr, `usage: jhon executions forget <project> \[--root <project>\]`, bad)
	}
	r = p.jhon(t, nil, "executions", "forget", "ghost")
	equal(t, r.Code, 2, r.Out())
	equal(t, r.Stderr, "jhon executions forget: ghost is not a uv-run pytest project here: lib\n")
	match(t, jhonGit(t, p.root, "help").Stdout, `(?m)^  jhon executions forget <project> \[--root <project>\]$`)
}

// T255 (7; L1725): a run that has to end once for the record to narrow (a
// first run, or one whose every selected test is one the record marks
// failed) gets the first-run deadline when the run names none; an explicit
// --timeout wins. A run cut by its deadline says how far it got.
func TestExecutionsARunThatHasToEndOnceGetsTheFirstRunDeadline(t *testing.T) {
	t.Parallel()
	p := newPytestProject(t)
	deadline := func(args ...string) float64 {
		for _, s := range p.planJSON(t, append([]string{"--affected", "base"}, args...)...).Plan {
			if s.Project == "lib" && s.Kind == "" {
				return s.TimeoutMinutes
			}
		}
		return -1
	}
	equal(t, deadline(), 180.0, "a first run")
	equal(t, deadline("--timeout", "5"), 0.0, "an explicit --timeout wins: the run's own")
	human := p.jhon(t, nil, "executions", "run", "c1", "--affected", "base", "--dry-run")
	match(t, human.Stdout, `(?m)^would run  lib: sh -c '[^\n]*--testmon-noselect[^\n]*'  \(cwd [^;]+; [^;]+; deadline 180 min\)$`)

	writeRecord(t, p.data, 3)
	out := p.planJSON(t, "--affected", "base")
	equal(t, deadline(), 180.0, "every selected test is one the record marks failed")
	match(t, strings.Join(out.Notes, "\n"), `(?m)^lib: every test pytest-testmon selects is one its record marks failed \(3\), so it runs under the first-run deadline of 180 min: once it ends, the record narrows again$`)
	equal(t, deadline("--timeout", "5"), 0.0)
	writeRecord(t, p.data, 0)
	equal(t, deadline(), 0.0, "a record with no failed test: the run's deadline")

	// A cut run says how far pytest got, and that the next run selects the
	// same tests again.
	writeRecord(t, p.data, 3)
	say := `collected 605 items / 28 deselected / 577 selected\ntests/test_a.py ....F...    [ 12%]\ntests/test_b.py ....        [ 24%]`
	r := p.jhon(t, []string{"FAKE_UV_SAY=" + say, "FAKE_UV_SLEEP=30"}, "executions", "run", "c1", "--affected", "base", "--json", "--timeout", "0.01")
	equal(t, r.Code, 1, r.Out())
	match(t, r.Stderr, `(?m)^<== lib: timeout after 0\.01 min; its process group was killed; pytest got through about 138 of its 577 selected tests \(24%\)$`)
	match(t, strings.Join(notesOf(t, r.Stdout), "\n"), `(?m)^lib: cut at its 0\.01 min deadline after pytest got through about 138 of its 577 selected tests \(24%\): pytest-testmon writes its record only at a run's end, so the next run selects the same tests again; give it a longer --timeout <minutes>$`)

	// prime builds first records under the first-run deadline too, and an
	// explicit --timeout wins there as well.
	q := newPytestProject(t)
	r = q.jhon(t, nil, "executions", "prime")
	equal(t, r.Code, 0, r.Out())
	match(t, r.Stdout, `^jhon executions prime: each first run's deadline is 180 min \(--timeout sets another\)\n==> lib: `)
	os.Remove(q.data)
	r = q.jhon(t, nil, "executions", "prime", "--timeout", "5")
	match(t, r.Stdout, `^jhon executions prime: each first run's deadline is 5 min \(--timeout sets another\)\n==> lib: `)
}
