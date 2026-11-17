package execution

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// T138: the tests whose last execution in a pytest-testmon record ran each
// of the files asked about, by file, as TestmonReach counts them.
func TestTestmonReachers(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 reads the record")
	}
	db := filepath.Join(t.TempDir(), "t.db")
	script := `import sqlite3, sys
c = sqlite3.connect(sys.argv[1])
c.executescript("create table test_execution (id integer primary key, environment_id integer, test_name text, duration float, failed bit, forced bit); create table file_fp (id integer primary key, filename text, method_checksums blob, mtime float, fsha text); create table test_execution_file_fp (test_execution_id integer, fingerprint_id integer)")
c.execute("insert into file_fp values (1, 'app/repo/query.py', x'00', 0, 'a'), (2, 'app/repo/other.py', x'00', 0, 'b')")
# t_old reached query.py once, and its last execution no longer does.
c.execute("insert into test_execution values (1, 1, 't_old', 1, 0, 0), (2, 1, 't_db', 1, 0, 0), (3, 1, 't_old', 1, 0, 0), (4, 1, 't_unit', 1, 0, 0)")
c.execute("insert into test_execution_file_fp values (1, 1), (2, 1), (2, 2), (3, 2), (4, 1)")
c.commit()`
	if out, err := exec.Command("python3", "-I", "-c", script, db).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	got, ok := TestmonReachers(db, []string{"app/repo/query.py", "app/repo/none.py"})
	want := map[string][]string{"app/repo/query.py": {"t_db", "t_unit"}}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("reachers: %v %v, want %v", got, ok, want)
	}
	if _, ok := TestmonReachers(filepath.Join(t.TempDir(), "none.db"), []string{"a.py"}); ok {
		t.Error("no record reads as one")
	}
}

// T145 E1: the test modules a pytest-testmon record holds tests of, which a
// wide run narrows by it; one it holds none of (new, or unknown to it) runs
// whole.
func TestTestmonHeld(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 reads the record")
	}
	db := filepath.Join(t.TempDir(), "t.db")
	script := `import sqlite3, sys
c = sqlite3.connect(sys.argv[1])
c.executescript("create table test_execution (id integer primary key, environment_id integer, test_name text, duration float, failed bit, forced bit)")
c.execute("insert into test_execution values (1, 1, 'lib/tests/test_util.py::test_x', 1, 0, 0), (2, 1, 'lib/tests/test_util.py::K::test_y[1]', 1, 0, 0), (3, 1, 'lib/tests/test_other.py::test_z', 1, 0, 0), (4, 1, 'lib/tests/test_util_more.py', 1, 0, 0)")
c.commit()`
	if out, err := exec.Command("python3", "-I", "-c", script, db).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	got, ok := TestmonHeld(db, []string{"lib/tests/test_util.py", "lib/tests/test_new.py", "lib/tests/test_other.py", "lib/tests/test_util_more.py"})
	if want := []string{"lib/tests/test_util.py", "lib/tests/test_other.py"}; !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("held: %v %v, want %v", got, ok, want)
	}
	if _, ok := TestmonHeld(filepath.Join(t.TempDir(), "none.db"), []string{"a.py"}); ok {
		t.Error("no record reads as one")
	}
}
