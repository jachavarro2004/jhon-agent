package mutate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecordDir(t *testing.T) {
	t.Setenv("HOME", "/h")
	t.Setenv("XDG_CACHE_HOME", "relative")
	if got := RecordDir("/p"); got != "/h/.cache/jhon/mut/"+Sha([]byte("/p"))[:16] {
		t.Fatalf("RecordDir = %q", got)
	}
	t.Setenv("XDG_CACHE_HOME", "/c")
	if got := RecordDir("/p"); !strings.HasPrefix(got, "/c/jhon/mut/") {
		t.Fatalf("RecordDir = %q", got)
	}
}

// Found mutating an executable script: a file written without its mode came back 644.
func TestWriteAtomicKeepsTheMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "run")
	os.WriteFile(p, []byte("a"), 0o755)
	os.Chmod(p, 0o755)
	if err := WriteAtomic(p, []byte("b")); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)
	data, _ := os.ReadFile(p)
	if fi.Mode().Perm() != 0o755 || string(data) != "b" {
		t.Fatalf("mode %o, content %q", fi.Mode().Perm(), data)
	}
}

func TestBeginWriteRestore(t *testing.T) {
	dir, root := t.TempDir(), t.TempDir()
	file := filepath.Join(root, "add.js")
	original := []byte("a + b\x00\xff\n")
	os.WriteFile(file, original, 0o644)
	m, err := Begin(dir, "1-2.json", file, original)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := Records(dir)
	if err != nil || len(pending) != 1 || pending[0].Rec.File != file {
		t.Fatalf("Records = %+v, %v", pending, err)
	}
	if err := m.Write([]byte("a - b\n")); err != nil {
		t.Fatal(err)
	}
	if err := m.Restore(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(file); string(data) != string(original) {
		t.Fatalf("restored %q", data)
	}
	if pending, _ := Records(dir); len(pending) != 0 {
		t.Fatalf("the record outlived the restore: %+v", pending)
	}
	if err := m.Write([]byte("late")); err != nil || m.Restore() != nil {
		t.Fatal("a restored mutation took a write or a second restore")
	}
	if data, _ := os.ReadFile(file); string(data) != string(original) {
		t.Fatalf("a write after the restore changed the file: %q", data)
	}
}

func TestRestoreOneChecksTheOriginal(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	rec := filepath.Join(dir, "r.json")
	os.WriteFile(rec, nil, 0o644)
	err := RestoreOne(Pending{rec, Record{File: file, Sha256: "0", Original: "YQ=="}})
	if err == nil || !strings.Contains(err.Error(), "does not match its original after the restore") {
		t.Fatalf("RestoreOne = %v", err)
	}
	if _, err := os.Stat(rec); err != nil {
		t.Fatal("a failed restore dropped its record")
	}
}

func TestRunReportsTheLastFailureLine(t *testing.T) {
	r := Run([]string{"sh", "-c", "echo 'not ok 1 - add adds' >&2; echo '  }' >&2; exit 3"})
	if r.Code != 3 || r.Failure != "not ok 1 - add adds" || r.Tail != "not ok 1 - add adds\n  }" {
		t.Fatalf("Run = %+v", r)
	}
	r = Run([]string{"sh", "-c", "echo one; echo; echo '  last  '; exit 1"})
	if r.Failure != "last" {
		t.Fatalf("without a failure line the last line counts: %+v", r)
	}
	r = Run([]string{"sh", "-c", "for i in 1 2 3 4 5 6 7; do echo $i; done"})
	if r.Code != 0 || r.Tail != "3\n4\n5\n6\n7" {
		t.Fatalf("Tail = %q", r.Tail)
	}
}

func TestRunWithoutTheCommand(t *testing.T) {
	r := Run([]string{"jhon-no-such-command"})
	if r.Code != 1 || r.Failure != "spawnSync jhon-no-such-command ENOENT" {
		t.Fatalf("Run = %+v", r)
	}
}
