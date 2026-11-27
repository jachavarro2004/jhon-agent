package release

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// T250 (the re-verify of d6b1d19, advisory 3): go's work folders
// (/var/tmp/jhon-go-<uid>-<hash>) are kept across runs for their state
// folder, and each records it; sync and publish remove this user's whose
// recorded state folder is gone, never one in use (its lock held), one
// whose state folder is there, or one with no record of its own.
func TestPruneRemovesOnlyTheWorkFoldersOfGoneStateFolders(t *testing.T) {
	t.Setenv("JHON_GO_CACHE", "")
	os.Unsetenv("JHON_GO_CACHE")
	base := t.TempDir()
	state := func(name string) string { return filepath.Join(base, name, "jhon", "go") }
	gone, inUse, here, unrecorded := state("gone"), state("in-use"), state("here"), state("unrecorded")
	for _, s := range []string{gone, inUse, here, unrecorded} {
		if err := os.MkdirAll(s, 0o700); err != nil {
			t.Fatal(err)
		}
		w, err := goWork(s)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(w) })
		if got, err := os.ReadFile(filepath.Join(w, "state")); err != nil || string(got) != s+"\n" {
			t.Fatalf("%s records %q, %v; want %s", w, got, err, s)
		}
	}
	if err := os.Remove(filepath.Join(goWorkPath(unrecorded), "state")); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(goWorkPath(inUse), "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_SH); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{gone, inUse, unrecorded} {
		if err := os.RemoveAll(filepath.Dir(filepath.Dir(s))); err != nil {
			t.Fatal(err)
		}
	}
	pruneWork()
	for s, kept := range map[string]bool{gone: false, inUse: true, here: true, unrecorded: true} {
		if _, err := os.Stat(goWorkPath(s)); (err == nil) != kept {
			t.Errorf("the work folder of %s: kept %v, want %v", s, err == nil, kept)
		}
	}
}
