package execution

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// T255 (L1724): every worktree of a repository shares one pytest-testmon
// record per project, keyed by the repository's git common dir and the
// project's path, so a new worktree narrows from the record the others
// built instead of running every test of each affected project again.
func TestTestmonDataIsSharedByEveryWorktree(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", tempDir(t))
	main := tempDir(t)
	gitT(t, main, "init", "-q")
	gitT(t, main, "commit", "-q", "--allow-empty", "-m", "base")
	wt := filepath.Join(tempDir(t), "wt")
	gitT(t, main, "worktree", "add", "-q", "-b", "other", wt)
	data := TestmonData(main, "backend/svc")
	if got := TestmonData(wt, "backend/svc"); got != data {
		t.Errorf("a second worktree has a record of its own: %s, the main checkout's is %s", got, data)
	}
	if !strings.HasPrefix(data, filepath.Join(os.Getenv("XDG_CACHE_HOME"), "jhon", "testmon")+string(filepath.Separator)) || filepath.Ext(data) != ".db" {
		t.Errorf("the record is not in Jhon's cache: %s", data)
	}
	if TestmonData(wt, "backend/other") == data {
		t.Error("another project shares the record")
	}
	fork := tempDir(t)
	gitT(t, fork, "init", "-q")
	if TestmonData(fork, "backend/svc") == data {
		t.Error("another repository shares the record")
	}
	if data == legacyTestmonData(main, "backend/svc") {
		t.Error("the shared record is keyed as the per-worktree one was")
	}
	// Outside git there is no repository to share: the record is the folder's.
	plain := tempDir(t)
	if got := TestmonData(plain, "p"); got != legacyTestmonData(plain, "p") {
		t.Errorf("outside git: %s", got)
	}
}

// T255 (3): a worktree's record from before the shared one (keyed by its top
// level) is adopted, copied, when the shared record is missing, and only then:
// an existing shared record is never replaced by it.
func TestTestmonDataAdoptsTheWorktreesOwnRecordOnce(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", tempDir(t))
	top := tempDir(t)
	gitT(t, top, "init", "-q")
	old := legacyTestmonData(top, "svc")
	write(t, old, "old record")
	data := TestmonData(top, "svc")
	if b, err := os.ReadFile(data); err != nil || string(b) != "old record" {
		t.Fatalf("the old record is not adopted: %q %v", b, err)
	}
	if b, _ := os.ReadFile(old); string(b) != "old record" {
		t.Error("the old record is not left where it was")
	}
	write(t, old, "newer per-worktree record")
	write(t, data, "shared record")
	TestmonData(top, "svc")
	if b, _ := os.ReadFile(data); string(b) != "shared record" {
		t.Errorf("an existing shared record was replaced: %q", b)
	}
	if left := beside(t, data, old); len(left) != 0 {
		t.Errorf("adopting left %v", left)
	}
}

// T255 (2): a run never writes the shared record in place. It runs on a
// private copy, which replaces the shared record, atomically, only when
// pytest ran to its end (exit 0 or 1), and is dropped otherwise: a run
// interrupted (exit 2), cut by its deadline or a signal (no code), or that
// failed inside pytest (3, 4, 5) leaves the shared record as it was.
func TestAPrivateCopyReplacesTheSharedRecordOnlyWhenPytestRanToItsEnd(t *testing.T) {
	dir := tempDir(t)
	data := filepath.Join(dir, "x.db")
	write(t, data, "shared")
	command := "mkdir -p " + shq(dir) + " && TESTMON_DATAFILE=" + shq(data) + " uv run --with pytest-testmon pytest --testmon --rootdir='/r'"
	for _, c := range []struct {
		name string
		code *int
		kept bool
	}{
		{"no code", nil, false},
		{"interrupted", ptr(2), false},
		{"internal error", ptr(3), false},
		{"usage error", ptr(4), false},
		{"no test collected", ptr(5), false},
		{"tests fail", ptr(1), true},
		{"tests pass", ptr(0), true},
	} {
		write(t, data, "shared")
		p, err := CheckOut(data)
		if err != nil {
			t.Fatal(err)
		}
		run := p.Command(command)
		if strings.Contains(run, shq(data)) || !strings.Contains(run, "TESTMON_DATAFILE="+shq(p.File())+" uv run") || filepath.Dir(p.File()) != dir {
			t.Fatalf("%s: the run does not write a private copy beside the record: %s", c.name, run)
		}
		if b, _ := os.ReadFile(p.File()); string(b) != "shared" {
			t.Errorf("%s: the copy starts as %q", c.name, b)
		}
		write(t, p.File(), "run "+c.name)
		published, refused := p.Finish(c.code, NewWatch())
		if refused != "" {
			t.Errorf("%s: refused: %s", c.name, refused)
		}
		want := "shared"
		if c.kept {
			want = "run " + c.name
		}
		if b, _ := os.ReadFile(data); string(b) != want || published != c.kept {
			t.Errorf("%s: the shared record is %q (published %v), want %q", c.name, b, published, want)
		}
		if left := beside(t, data); len(left) != 0 {
			t.Errorf("%s: left %v", c.name, left)
		}
	}
	// A copy the run did not change is not written back: it would replace a
	// record another run finished meanwhile with an older one.
	p, err := CheckOut(data)
	if err != nil {
		t.Fatal(err)
	}
	write(t, data, "finished meanwhile")
	if published, _ := p.Finish(ptr(0), nil); published {
		t.Error("an unchanged copy was published")
	}
	if b, _ := os.ReadFile(data); string(b) != "finished meanwhile" {
		t.Errorf("an unchanged copy replaced the record: %q", b)
	}
	// With no shared record yet, the run starts from none and its record becomes it.
	none := filepath.Join(dir, "none", "y.db")
	p, err = CheckOut(none)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.File()); err == nil {
		t.Error("a copy of no record exists before the run")
	}
	write(t, p.File(), "first")
	if published, _ := p.Finish(ptr(0), nil); !published {
		t.Error("the first record was not published")
	}
	if b, _ := os.ReadFile(none); string(b) != "first" {
		t.Errorf("the first record: %q", b)
	}
	// A command that does not name the record is left as written.
	if got := p.Command("uv run pytest 'tests/a.py'"); got != "uv run pytest 'tests/a.py'" {
		t.Errorf("a command without the record: %q", got)
	}
}

// T255 (2): two runs at once each work on their own copy and never corrupt
// the shared record: the last to finish wins, whole.
func TestConcurrentRunsNeverCorruptTheSharedRecord(t *testing.T) {
	dir := tempDir(t)
	data := filepath.Join(dir, "x.db")
	write(t, data, "shared")
	const n = 8
	copies := make([]*Private, n)
	for i := range copies {
		p, err := CheckOut(data)
		if err != nil {
			t.Fatal(err)
		}
		copies[i] = p
	}
	seen := map[string]bool{}
	for _, p := range copies {
		if seen[p.File()] {
			t.Fatalf("two runs share the copy %s", p.File())
		}
		seen[p.File()] = true
	}
	var wg sync.WaitGroup
	for i, p := range copies {
		wg.Go(func() {
			if err := os.WriteFile(p.File(), []byte(strings.Repeat(strconv.Itoa(i), 1<<16)), 0o644); err != nil {
				t.Error(err)
			}
			p.Finish(ptr(0), nil)
		})
	}
	wg.Wait()
	b, _ := os.ReadFile(data)
	if len(b) != 1<<16 || strings.Trim(string(b), string(b[:1])) != "" {
		t.Errorf("the shared record is not one run's whole record: %d bytes", len(b))
	}
	if left := beside(t, data); len(left) != 0 {
		t.Errorf("left %v", left)
	}
}

// T255 (2): a first run's file is private to its process, so two first runs
// of one record at once (two worktrees) never write or remove each other's.
func TestAFirstRunsFileIsPrivateToItsProcess(t *testing.T) {
	data := filepath.Join(tempDir(t), "x.db")
	b := BuildFile(data)
	if !strings.HasPrefix(b, data+".") || !strings.Contains(b, "."+strconv.Itoa(os.Getpid())+".") || !strings.HasSuffix(b, ".building") {
		t.Errorf("a first run's file is not its own: %s", b)
	}
}

// T255 (4): a project with no usable record runs all its tests once; the dry
// run says how many, counted from the test modules' `def test` lines.
func TestCountTests(t *testing.T) {
	root := tempDir(t)
	write(t, filepath.Join(root, "svc", "tests", "test_a.py"), "def test_one():\n    pass\n\nasync def test_two():\n    pass\n\nclass TestThing:\n    def test_three(self):\n        pass\n    def helper(self):\n        pass\n")
	write(t, filepath.Join(root, "svc", "tests", "b_test.py"), "def test_four(): pass\n")
	write(t, filepath.Join(root, "svc", "tests", "conftest.py"), "def test_not_a_test(): pass\n")
	write(t, filepath.Join(root, "svc", ".venv", "lib", "test_vendored.py"), "def test_x(): pass\n")
	write(t, filepath.Join(root, "other", "test_other.py"), "def test_x(): pass\n")
	if tests, modules := CountTests(root, "svc"); tests != 4 || modules != 2 {
		t.Errorf("CountTests = %d tests in %d modules, want 4 in 2", tests, modules)
	}
	if tests, modules := CountTests(root, "none"); tests != 0 || modules != 0 {
		t.Errorf("no project: %d, %d", tests, modules)
	}
}

// T255 (5, L1725): a run whose failures are the test environment's (the
// services it connects to down, a module pytest cannot import, every test
// failing) does not replace the shared record: such a record marks every
// test it ran failed, and pytest-testmon runs every failed test again
// whatever changed, so every later run would run them all.
func TestARunWhoseFailuresAreTheEnvironmentsKeepsNoRecord(t *testing.T) {
	watch := func(lines ...string) *Watch {
		w := NewWatch()
		for _, l := range lines {
			if _, err := w.Write([]byte(l + "\n")); err != nil {
				t.Fatal(err)
			}
		}
		return w
	}
	refusedBy := "FAILED tests/test_q.py::test_q - sqlalchemy.exc.OperationalError: (psycopg.OperationalError) connection failed: Connection refused"
	for _, c := range []struct {
		name string
		w    *Watch
		want string
	}{
		{"every test failed", watch("collected 577 items", "==== 577 failed, 2 warnings in 5100.12s (1:25:00) ===="), "every one of its 577 tests failed"},
		{"every test failed or errored", watch("==== 3 failed, 4 errors in 9.00s ===="), "every one of its 7 tests failed"},
		{"a service refused", watch(refusedBy, "==== 1 failed, 9 passed in 3.00s ===="), "its output says a test service refused the connection (" + refusedBy + ")"},
		{"redis down", watch("E   redis.exceptions.ConnectionError: Error 111 connecting to localhost:6379. Connection refused.", "1 failed in 0.50s"), "its output says a test service refused the connection (E   redis.exceptions.ConnectionError: Error 111 connecting to localhost:6379. Connection refused.)"},
		{"a module not collected", watch("____ ERROR collecting tests/test_a.py ____", "==== 1 passed, 1 error in 0.20s ===="), "pytest could not collect every test module (____ ERROR collecting tests/test_a.py ____)"},
		// A unit's RED: a few new tests fail, and their record is kept.
		{"a few tests fail", watch("==== 4 failed in 0.10s ===="), ""},
		{"some fail, some pass", watch("==== 6 failed, 1 passed in 0.10s ===="), ""},
		{"a refusal tested and passing", watch("checking: Connection refused is retried", "==== 3 passed in 0.10s ===="), ""},
		{"no output", watch(), ""},
	} {
		if got := c.w.Refusal(); got != c.want {
			t.Errorf("%s: Refusal() = %q, want %q", c.name, got, c.want)
		}
	}
	var none *Watch
	if none.Refusal() != "" {
		t.Error("no watch, no refusal")
	}

	dir := tempDir(t)
	data := filepath.Join(dir, "x.db")
	write(t, data, "shared")
	p, err := CheckOut(data)
	if err != nil {
		t.Fatal(err)
	}
	write(t, p.File(), "every test failed")
	published, refused := p.Finish(ptr(1), watch("==== 577 failed in 5100.12s ===="))
	if published || refused != "every one of its 577 tests failed" {
		t.Errorf("published %v, refused %q", published, refused)
	}
	if b, _ := os.ReadFile(data); string(b) != "shared" {
		t.Errorf("the shared record was replaced: %q", b)
	}
	if left := beside(t, data); len(left) != 0 {
		t.Errorf("left %v", left)
	}

	// A first run's too: no record is built, and none is marked cut, so the
	// next run, with the services up, builds it.
	first := filepath.Join(dir, "first.db")
	write(t, BuildFile(first), "every test failed")
	kept, refused := FinishBuild(first, ptr(1), watch("==== 577 failed in 5100.12s ===="))
	if kept || refused != "every one of its 577 tests failed" || exists(first) || exists(BuildFile(first)) {
		t.Errorf("kept %v, refused %q, record %v, file %v", kept, refused, exists(first), exists(BuildFile(first)))
	}
	if _, cut := FirstRunCut(first); cut {
		t.Error("an environment's failure marked the first run cut")
	}
}

// T255 (7, L1725): a run cut by its deadline says how far it got, as
// pytest's output said: what it selected, and its last progress.
func TestWatchSaysHowFarARunGot(t *testing.T) {
	for _, c := range []struct {
		name, out, want string
	}{
		{"selected", "testmon: changed files: 0, unchanged files: 605\ncollected 605 items / 28 deselected / 577 selected\n\ntests/test_a.py ....F...                     [ 12%]\ntests/test_b.py ....                         [ 24%]\n", "pytest got through about 138 of its 577 selected tests (24%)"},
		{"deselected only", "collected 10 items / 6 deselected\n.... [100%]\n", "pytest got through about 4 of its 4 selected tests (100%)"},
		{"all collected", "collected 40 items\n.........", "pytest selected 40 tests and finished none"},
		{"split writes", "colle", ""},
		{"nothing said", "", ""},
	} {
		w := NewWatch()
		for _, chunk := range strings.SplitAfter(c.out, "\n") {
			w.Write([]byte(chunk))
		}
		if got := w.Progress(); got != c.want {
			t.Errorf("%s: Progress() = %q, want %q", c.name, got, c.want)
		}
	}
	w := NewWatch()
	w.Write([]byte("colle"))
	w.Write([]byte("cted 3 items\n... [ 66%]\n"))
	if got := w.Progress(); got != "pytest got through about 1 of its 3 selected tests (66%)" {
		t.Errorf("a line written in parts: %q", got)
	}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func ptr(n int) *int { return &n }

// beside is what is left in data's directory but data itself and known.
func beside(t *testing.T, data string, known ...string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(data))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.Name() != filepath.Base(data) && !slices.Contains(known, filepath.Join(filepath.Dir(data), e.Name())) {
			out = append(out, e.Name())
		}
	}
	return out
}

// T255 (6, 7): what a record says of the tests it marks failed, which
// pytest-testmon runs again whatever changed: most of them failed is a
// record a run while the services were down left; and a change that reaches
// none of its passing tests selects only failed ones.
func TestReachCountsTheTestsTheRecordMarksFailed(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 reads the record")
	}
	db := filepath.Join(tempDir(t), "t.db")
	// failed is how many of 10 tests failed; a.py is run by tests 0 and 9.
	record := func(failed int) {
		os.Remove(db)
		script := `import sqlite3, sys
c = sqlite3.connect(sys.argv[1])
c.executescript("create table test_execution (id integer primary key, environment_id integer, test_name text, duration float, failed bit, forced bit); create table file_fp (id integer primary key, filename text, method_checksums blob, mtime float, fsha text); create table test_execution_file_fp (test_execution_id integer, fingerprint_id integer)")
c.execute("insert into file_fp values (1, 'a.py', x'00', 0, 'a'), (2, 'b.py', x'00', 0, 'b')")
for i in range(10):
    c.execute("insert into test_execution values (?, 1, ?, 1, ?, 0)", (i, "t%d" % i, 1 if i >= 10 - int(sys.argv[2]) else 0))
    c.execute("insert into test_execution_file_fp values (?, ?)", (i, 1 if i in (0, 9) else 2))
c.commit()`
		if out, err := exec.Command("python3", "-I", "-c", script, db, strconv.Itoa(failed)).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	for _, c := range []struct {
		failed                   int
		files                    []string
		want                     Reach
		mostlyFailed, onlyFailed bool
	}{
		{0, []string{"a.py"}, Reach{Tests: 2, Total: 10, Seconds: 2, AllTime: 10, Failed: 0, Passing: 2}, false, false},
		{1, []string{"a.py"}, Reach{Tests: 2, Total: 10, Seconds: 2, AllTime: 10, Failed: 1, Passing: 1}, false, false},
		{1, nil, Reach{Tests: 0, Total: 10, Seconds: 0, AllTime: 10, Failed: 1}, false, true},
		{10, []string{"a.py"}, Reach{Tests: 2, Total: 10, Seconds: 2, AllTime: 10, Failed: 10}, true, true},
		{6, nil, Reach{Total: 10, AllTime: 10, Failed: 6}, true, true},
		{5, nil, Reach{Total: 10, AllTime: 10, Failed: 5}, false, true},
	} {
		record(c.failed)
		got, ok := TestmonReach(db, c.files)
		if !ok || got != c.want || got.MostlyFailed() != c.mostlyFailed || got.OnlyFailed() != c.onlyFailed {
			t.Errorf("%d failed, %v changed: %+v %v (mostly %v, only %v), want %+v (mostly %v, only %v)", c.failed, c.files, got, ok,
				got.MostlyFailed(), got.OnlyFailed(), c.want, c.mostlyFailed, c.onlyFailed)
		}
	}
}

// T255 (6): `jhon executions forget` drops a record, what sqlite keeps
// beside it and the marker of its cut first run.
func TestForgetDropsTheRecord(t *testing.T) {
	data := filepath.Join(tempDir(t), "x.db")
	for _, f := range []string{data, data + "-wal", data + "-shm", cutFile(data)} {
		write(t, f, "x")
	}
	other := filepath.Join(filepath.Dir(data), "y.db")
	write(t, other, "another record")
	if !Forget(data) {
		t.Error("a record was there")
	}
	if left := beside(t, data, other); len(left) != 0 || exists(data) {
		t.Errorf("left %v, record %v", left, exists(data))
	}
	if Forget(data) {
		t.Error("no record was there")
	}
}
