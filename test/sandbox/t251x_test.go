// T251's adversarial verify (odd/tasks/v0-1-release.md L1729), beyond the
// verifier's own tests (t251v_test.go): the working folder of a project
// line is anchored too (B2 existed at bf705e0 for a project subfolder), and
// a fetch in a repository the sandbox does not open for writing ends with
// its reason instead of a bare exit 255.
package sandbox

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// A repository nested in the project, the line run in it from the
// project's session: a sandboxed command of the project poisons it, moves
// it aside and puts a clean copy at its path while the line waits in the
// registry. The line must not run outside in the poisoned one.
func TestT251XAProjectSubfolderMovedAfterJhonReadItKeepsTheLineInside(t *testing.T) {
	b := newT251Box(t, "")
	nested := filepath.Join(b.proj, "nested")
	support.Git(t, b.base, "clone", "-q", b.repo, nested)
	support.Git(t, nested, "remote", "set-url", "origin", "ssh://example.invalid"+b.remote)
	support.Write(t, b.home, map[string]string{".bashrc": "owner\n"})
	evil := filepath.Join(b.proj, "nested-evil")
	swap := "sleep 1; ln -s \"$HOME/.bashrc\" " + nested + "/.git/FETCH_HEAD && mv " + nested + " " + evil + " && cp -a " + evil + " " + nested + " && rm " + nested + "/.git/FETCH_HEAD"
	racer := exec.Command(support.Bin(t), "sandbox", "run", "--project", b.proj, "--", "sh", "-c", swap)
	racer.Dir, racer.Env = b.proj, b.env
	if err := racer.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { racer.Process.Kill(); racer.Wait() })
	time.Sleep(300 * time.Millisecond)
	r := b.outside(t, nested, b.env, "git fetch origin")
	if err := racer.Wait(); err != nil {
		t.Fatalf("the sandboxed swap: %v", err)
	}
	if got := support.Read(t, filepath.Join(b.home, ".bashrc")); got != "owner\n" {
		t.Errorf("the line ran outside in the moved folder (exit %d, stderr %q): ~/.bashrc is %q", r.Code, r.Stderr, got)
	}
	if want := "jhon sandbox: the folder this line runs in, " + nested + ", changed after Jhon read it: "; !strings.HasPrefix(r.Stderr, want) {
		t.Errorf("stderr %q; want it to open with %q", r.Stderr, want)
	}
}

// A repository in an allowed folder where the sandbox keeps it read-only
// (inside Jhon's own settings folder): the fetch runs inside, git exits
// 255 on the read-only FETCH_HEAD, and the failure still ends with why.
func TestT251XAFetchInARepositoryTheSandboxKeepsReadOnlyEndsWithItsReason(t *testing.T) {
	b := newT251Box(t, "")
	config := filepath.Join(b.home, ".config")
	repo := filepath.Join(config, "jhon", "b")
	support.Git(t, b.base, "clone", "-q", b.repo, repo)
	support.Git(t, repo, "remote", "set-url", "origin", "ssh://example.invalid"+b.remote)
	b.settings(t, "schema = 1\n\n[sandbox]\nallow_write = [\""+config+"\"]\n")
	r := b.outside(t, b.proj, b.env, "git -C "+repo+" fetch origin")
	want := "\n" + t251Kept + "the repository " + repo + " lies in the sandbox.allow_write folder " + config + ", which the sandbox does not open for writing there (jhon sandbox status says why), so it does not count as a project; fix: run the line yourself in a terminal\n"
	if r.Code == 0 || exists(b.note) || !strings.HasSuffix(r.Stderr, want) {
		t.Errorf("exit %d, ran outside %v, stderr %q; want a failure ending with %q", r.Code, exists(b.note), r.Stderr, want)
	}
}
