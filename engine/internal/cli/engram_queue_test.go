package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
)

// T222 L14: on a kernel with no openat2 the Engram queue fails closed; the
// flush says so once on stderr and doctor warns, both with the fix, and
// every path they print is quoted.
func TestTheEngramQueueFailsClosedWithoutOpenat2(t *testing.T) {
	repo, err := filepath.EvalSymlinks(t.TempDir())
	must(t, err)
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	queue := filepath.Join(repo, ".git", "jhon", "engram-queue")
	must(t, os.MkdirAll(queue, 0o700))
	must(t, os.WriteFile(filepath.Join(queue, "20261123T120000.000000000Z-c1.json"), []byte("{}"), 0o600))
	t.Chdir(repo)
	t.Setenv("JHON_IN_SANDBOX", "")
	t.Setenv("JHON_SETTINGS", filepath.Join(repo, "settings.toml"))
	engram := filepath.Join(repo, "engram")
	must(t, os.WriteFile(engram, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	t.Setenv("JHON_ENGRAM", engram)
	was := ldd.OpenCommonDir
	t.Cleanup(func() { ldd.OpenCommonDir = was })
	ldd.OpenCommonDir = func(string) (int, error) { return -1, syscall.ENOSYS }

	var errw bytes.Buffer
	wasErr := queueErr
	t.Cleanup(func() { queueErr = wasErr })
	queueErr = &errw
	flushEngramQueue("")
	if want := "jhon: the Engram queue was not read: " + ldd.ErrNoOpenat2.Error() + "; fix: a Linux kernel 5.6 or newer\n"; errw.String() != want {
		t.Errorf("stderr %q, want %q", errw.String(), want)
	}
	var out bytes.Buffer
	o := &release.Out{W: &out}
	doctor{o: o}.engramQueue()
	if want := "  warn  Engram queue \"" + queue + "\" was not read: " + ldd.ErrNoOpenat2.Error() + "; fix: a Linux kernel 5.6 or newer\n"; out.String() != want || o.Failed != 0 {
		t.Errorf("doctor %q, want %q", out.String(), want)
	}
	if _, err := os.Stat(filepath.Join(queue, "20261123T120000.000000000Z-c1.json")); err != nil {
		t.Errorf("the queue changed: %v", err)
	}
}
