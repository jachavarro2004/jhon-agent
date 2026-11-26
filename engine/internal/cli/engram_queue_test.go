package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// queueRepo is a git repository, the working directory, with an Engram
// queue holding files (name to content), Engram the fake body and on, and
// the flush's stderr in the buffer it gives; the last flush is reset.
func queueRepo(t *testing.T, files map[string]string, body string) (repo, queue string, errw *bytes.Buffer) {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	must(t, err)
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	queue = filepath.Join(repo, ".git", "jhon", "engram-queue")
	must(t, os.MkdirAll(queue, 0o700))
	for name, content := range files {
		must(t, os.WriteFile(filepath.Join(queue, name), []byte(content), 0o600))
	}
	t.Chdir(repo)
	t.Setenv("JHON_IN_SANDBOX", "")
	t.Setenv("JHON_SETTINGS", filepath.Join(repo, "settings.toml"))
	engram := filepath.Join(t.TempDir(), "engram")
	must(t, os.WriteFile(engram, []byte("#!/bin/sh\n"+body), 0o755))
	t.Setenv("JHON_ENGRAM", engram)
	wasFlush, wasErr := lastFlush, queueErr
	t.Cleanup(func() { lastFlush, queueErr = wasFlush, wasErr })
	lastFlush, errw = ldd.FlushReport{}, &bytes.Buffer{}
	queueErr = errw
	return repo, queue, errw
}

// doctorQueue is doctor's Engram queue rows.
func doctorQueue() string {
	var out bytes.Buffer
	doctor{o: &release.Out{W: &out}}.engramQueue()
	return out.String()
}

const (
	openat2Refused = "openat2 is refused here (operation not permitted: a seccomp filter, as a container's, blocks it), which the Engram queue needs to follow no link"
	seccompFixText = "allow openat2 in the seccomp filter jhon runs under (a container's profile), or run jhon outside it"
)

// T227 (a): an openat2 a seccomp filter refuses (EPERM) is said as such,
// with its own fix: never "a link or not a folder", and never doctor's
// "remove it", which would discard the queued records.
func TestAnOpenat2RefusedBySeccompIsNoLink(t *testing.T) {
	const rec = "20261123T120000.000000000Z-c1.json"
	_, queue, errw := queueRepo(t, map[string]string{rec: "{}"}, "exit 0\n")
	was := ldd.OpenCommonDir
	t.Cleanup(func() { ldd.OpenCommonDir = was })
	ldd.OpenCommonDir = func(string) (int, error) { return -1, syscall.EPERM }
	flushEngramQueue("")
	if want := "jhon: the Engram queue was not read: " + openat2Refused + "; fix: " + seccompFixText + "\n"; errw.String() != want {
		t.Errorf("stderr %q, want %q", errw.String(), want)
	}
	if got, want := doctorQueue(), "  warn  Engram queue \""+queue+"\" was not read: "+openat2Refused+"; fix: "+seccompFixText+"\n"; got != want {
		t.Errorf("doctor %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(queue, rec)); err != nil {
		t.Errorf("the queue changed: %v", err)
	}
}

// T227 (b): the stderr line of the records a flush kept counts records
// only, not the other entries, and says Engram did not take them only when
// Engram was asked; doctor's row the same.
func TestTheKeptLineCountsRecordsAndSaysWhetherEngramWasAsked(t *testing.T) {
	repo, _, errw := queueRepo(t, map[string]string{
		"20261123T120000.000000000Z-c1.json": "{}", "20261123T120001.000000000Z-c2.json": "{}", "junk": "x",
	}, "touch \"$0.called\"\n")
	was := ldd.OpenCommonDir
	t.Cleanup(func() { ldd.OpenCommonDir = was })
	ldd.OpenCommonDir = func(p string) (int, error) {
		if p == repo {
			return -1, syscall.EACCES
		}
		return was(p)
	}
	flushEngramQueue("")
	why := fmt.Sprintf("%q", "open "+repo+": permission denied")
	if want := "jhon: the Engram queue refused \"junk\": its name is not <time>-<change>.json; it was never saved\n" +
		"jhon: the Engram queue kept 2 records for the next jhon command: Engram was not asked (" + why + ")\n"; errw.String() != want {
		t.Errorf("stderr\n got %q\nwant %q", errw.String(), want)
	}
	if _, err := os.Stat(os.Getenv("JHON_ENGRAM") + ".called"); !os.IsNotExist(err) {
		t.Errorf("engram was called: %v", err)
	}
	if got := doctorQueue(); !strings.Contains(got, "  warn  Engram queue: Engram was not asked to save the queued records ("+why+"); the next jhon command outside the sandbox tries again\n") ||
		strings.Contains(got, "did not take") {
		t.Errorf("doctor %q", got)
	}
}

// PRESERVE (T227 b): a record Engram was asked for and did not take is
// still said so, on stderr and by doctor.
func TestTheKeptLineSaysEngramDidNotTakeARecord(t *testing.T) {
	main, queue, errw := queueRepo(t, nil, "echo 'engram: database is locked' >&2; exit 1\n")
	b, err := json.Marshal(map[string]string{"title": ldd.StateTitle("c1"), "topic": ldd.StateTopic("c1"),
		"content": (ldd.ChangeState{Change: "c1", Phase: "verify"}).Content(), "project": main})
	must(t, err)
	must(t, os.WriteFile(filepath.Join(queue, "20261123T120000.000000000Z-c1.json"), b, 0o600))
	flushEngramQueue("")
	if want := "jhon: the Engram queue kept 1 record for the next jhon command: Engram did not take it (\"engram: database is locked\")\n"; errw.String() != want {
		t.Errorf("stderr %q, want %q", errw.String(), want)
	}
	if got := doctorQueue(); !strings.Contains(got, "  warn  Engram queue: Engram did not take the queued records (\"engram: database is locked\"); the next jhon command outside the sandbox tries again\n") {
		t.Errorf("doctor %q", got)
	}
}

// PRESERVE (T227 a): a queue reached through a link is still refused on
// stderr as a link, and doctor still gives its fix, remove it.
func TestAQueueThroughALinkIsStillSaidSo(t *testing.T) {
	repo, _, errw := queueRepo(t, nil, "exit 0\n")
	jhon := filepath.Join(repo, ".git", "jhon")
	must(t, os.Rename(jhon, filepath.Join(repo, "elsewhere")))
	must(t, os.Symlink(filepath.Join(repo, "elsewhere"), jhon))
	queue := filepath.Join(jhon, "engram-queue")
	flushEngramQueue("")
	if want := fmt.Sprintf("jhon: the Engram queue refused %q: the queue is a link or not a folder; nothing in it was read; it was never saved\n", queue); errw.String() != want {
		t.Errorf("stderr %q, want %q", errw.String(), want)
	}
	if got, want := doctorQueue(), fmt.Sprintf("  warn  Engram queue %q cannot be read: %q; nothing in it is saved; fix: remove it, and Jhon makes it again\n", queue, jhon+" is a link or not a folder"); got != want {
		t.Errorf("doctor %q, want %q", got, want)
	}
}
