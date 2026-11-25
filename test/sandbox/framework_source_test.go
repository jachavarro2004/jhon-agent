// Jhon's source checkout, the one `jhon sync` records in the install record,
// stays read-only to a command whose project is another one, a linked
// worktree of that checkout included: a worker in a worktree writing the
// main checkout by its absolute path (T128f slice 7, B1 of L1367). With the
// checkout itself as the project nothing changes.
package sandbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// installedFrom is a box whose CLI runs as the installed one (its runtime is
// ~/.local/share/jhon/current, no checkout) and whose install record names
// source as the checkout `jhon sync` last installed from.
func installedFrom(t *testing.T, source string) box {
	t.Helper()
	b := newBox(t)
	current := filepath.Join(b.home, ".local", "share", "jhon", "current")
	if err := os.MkdirAll(current, 0o755); err != nil {
		t.Fatal(err)
	}
	record, err := json.Marshal(map[string]string{"installed_at": "2026-11-19T00:00:00.000Z", "commit": strings.Repeat("a", 40), "source": filepath.Join(b.home, source)})
	if err != nil {
		t.Fatal(err)
	}
	support.Write(t, b.home, map[string]string{".cache/jhon/install.json": string(record) + "\n"})
	b.env = append(b.env, "JHON_ROOT="+current)
	return b
}

func TestJhonsSourceCheckoutIsReadOnlyFromALinkedWorktree(t *testing.T) {
	b := installedFrom(t, "src")
	src, wt := filepath.Join(b.home, "src"), filepath.Join(b.home, "src-wt")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	support.GitRepo(t, src)
	support.Git(t, src, "worktree", "add", "-q", "-b", "change", wt)
	run := func(project, file string) support.Result {
		return support.Jhon(t, project, b.env, "sandbox", "run", "--project", project, "--", "sh", "-c", `echo x > "$1"`, "sh", file)
	}

	// B1: from the linked worktree, the source checkout is read-only.
	y := filepath.Join(src, "y")
	if r := run(wt, y); r.Code == 0 || r.Stdout != "" || !strings.HasSuffix(r.Stderr, y+": Read-only file system\n"+ownNote(y, src)) {
		t.Errorf("a write to the source checkout from a linked worktree: exit %d, stdout %q, stderr %q; want a read-only failure with the note", r.Code, r.Stdout, r.Stderr)
	}
	if exists(y) {
		t.Fatalf("%s was written from the linked worktree", y)
	}

	// The worktree's own files stay writable.
	if r := run(wt, filepath.Join(wt, "y")); r.Code != 0 || r.Stderr != "" || support.Read(t, filepath.Join(wt, "y")) != "x\n" {
		t.Errorf("a write in the linked worktree: exit %d, stderr %q; want it written", r.Code, r.Stderr)
	}
	// It still commits through the common dir, inside the source checkout.
	commit := support.Jhon(t, wt, b.env, "sandbox", "run", "--project", wt, "--", "sh", "-c", `git add y && git -c user.name=jhon-test -c user.email=jhon-test@example.invalid -c commit.gpgsign=false commit -q -m change && echo committed`)
	if commit.Code != 0 || commit.Stdout != "committed\n" {
		t.Errorf("a commit in the linked worktree: exit %d, stdout %q, stderr %q; want it committed", commit.Code, commit.Stdout, commit.Stderr)
	}

	// With the source checkout as the project nothing changes.
	if r := run(src, y); r.Code != 0 || r.Stderr != "" || support.Read(t, y) != "x\n" {
		t.Errorf("a write in the source checkout as the project: exit %d, stderr %q; want it written", r.Code, r.Stderr)
	}
}
