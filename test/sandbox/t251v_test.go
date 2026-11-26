// T251 adversarial verify (odd/tasks/v0-1-release.md L1721, L1722): a line
// of a git repository inside a sandbox.allow_write folder runs outside the
// sandbox only where Jhon checked it; a sandboxed command of the project's
// session, which may write anything in the allowed folder, must not steer
// it elsewhere.
package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// A folder named with `..` after a link: Jhon judged work/b, the cleaned
// path, while git's chdir follows work/x, a link a sandboxed command made to
// a folder of its own outside the allowed one, and goes up from there. The
// line must not run outside, and ~/.bashrc stays as it was.
func TestT251VADotDotAfterALinkKeepsTheLineInside(t *testing.T) {
	b := newT251Box(t, "")
	support.Write(t, b.home, map[string]string{".bashrc": "owner\n"})
	q := filepath.Join(b.home, ".cache", "q")
	plant := "set -e; mkdir -p " + q + "/y; git init -q " + q + "/b; git -C " + q + "/b remote add origin ssh://example.invalid" + b.remote +
		"; ln -s \"$HOME/.bashrc\" " + q + "/b/.git/FETCH_HEAD; ln -s " + q + "/y " + b.work + "/x; echo set"
	if r := support.Jhon(t, b.proj, b.env, "sandbox", "run", "--project", b.proj, "--", "sh", "-c", plant); r.Stdout != "set\n" {
		t.Fatalf("plant: exit %d, %q, %q", r.Code, r.Stdout, r.Stderr)
	}
	for _, line := range []string{
		"git -C " + b.work + "/x/../b fetch origin",
		"git -C " + b.work + "/x/.. -C b fetch origin",
	} {
		os.Remove(b.note)
		if r := support.Jhon(t, b.proj, b.env, "sandbox", "profile", "--project", b.proj, "--", line); r.Stdout != "work\n" {
			t.Errorf("profile of %q: %q, want work", line, r.Stdout)
		}
		r := b.outside(t, b.proj, b.env, line)
		if exists(b.note) || support.Read(t, filepath.Join(b.home, ".bashrc")) != "owner\n" {
			t.Errorf("%q ran outside the sandbox in the folder the link leads to (exit %d, stderr %q); ~/.bashrc is %q", line, r.Code, r.Stderr, support.Read(t, filepath.Join(b.home, ".bashrc")))
			support.Write(t, b.home, map[string]string{".bashrc": "owner\n"})
		}
	}
}

// The working folder of a line run in the repository is the folder the
// process is in, not its path: a sandboxed command that poisons the
// repository, moves it aside and puts a clean copy at its path while the
// line waits for it in the registry must not get the line run outside in
// the poisoned one.
func TestT251VTheWorkingFolderMovedAfterJhonReadItKeepsTheLineInside(t *testing.T) {
	b := newT251Box(t, "")
	support.Write(t, b.home, map[string]string{".bashrc": "owner\n"})
	evil := filepath.Join(b.work, "b-evil")
	swap := "sleep 1; ln -s \"$HOME/.bashrc\" " + b.repo + "/.git/FETCH_HEAD && mv " + b.repo + " " + evil + " && cp -a " + evil + " " + b.repo + " && rm " + b.repo + "/.git/FETCH_HEAD"
	racer := exec.Command(support.Bin(t), "sandbox", "run", "--project", b.proj, "--", "sh", "-c", swap)
	racer.Dir, racer.Env = b.proj, b.env
	if err := racer.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { racer.Process.Kill(); racer.Wait() })
	time.Sleep(300 * time.Millisecond)
	r := b.outside(t, b.repo, b.env, "git fetch origin")
	if err := racer.Wait(); err != nil {
		t.Fatalf("the sandboxed swap: %v", err)
	}
	if got := support.Read(t, filepath.Join(b.home, ".bashrc")); got != "owner\n" {
		t.Errorf("the line ran outside in the moved repository (exit %d, stderr %q): ~/.bashrc is %q", r.Code, r.Stderr, got)
	}
}

// gh run in the repository (its working folder), as L1722 names it ("runs
// in it"), runs outside as the same line after `cd <repo> &&` does, and
// `jhon sandbox profile` and the run agree.
func TestT251VGhRunInTheRepositoryRunsOutside(t *testing.T) {
	b := newT251Box(t, "")
	gh := filepath.Join(b.base, "t251-bin", "gh")
	script(t, gh, "#!/bin/sh\nif [ \"$JHON_IN_SANDBOX\" = 1 ]; then echo 'gh: no credentials' >&2; exit 4; fi\necho ran >> "+b.note+"\n")
	for _, c := range []struct{ dir, line string }{{b.proj, "cd " + b.repo + " && gh pr list"}, {b.repo, "gh pr list"}} {
		os.Remove(b.note)
		if r := support.Jhon(t, c.dir, b.env, "sandbox", "profile", "--project", b.proj, "--", c.line); r.Stdout != "unsandboxed\n" {
			t.Errorf("profile of %q in %s: %q, want unsandboxed", c.line, c.dir, r.Stdout)
		}
		if r := b.outside(t, c.dir, b.env, c.line); r.Code != 0 || r.Stderr != "" || !exists(b.note) {
			t.Errorf("%q in %s: exit %d, stderr %q, ran outside %v; want it run outside", c.line, c.dir, r.Code, r.Stderr, exists(b.note))
		}
	}
}

// An allowed folder that is a link: the note must not send the agent to a
// path that is kept inside too (the real path is in no allowed folder).
func TestT251VAnAllowedFolderThatIsALinkNamesARealFix(t *testing.T) {
	b := newT251Box(t, "")
	real := filepath.Join(b.home, "realwork")
	if err := os.Rename(b.work, real); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, b.work); err != nil {
		t.Fatal(err)
	}
	r := b.outside(t, b.proj, b.env, "git -C "+b.repo+" ls-remote origin")
	if !strings.Contains(r.Stderr, t251Kept) {
		t.Fatalf("exit %d, stderr %q; want a kept note", r.Code, r.Stderr)
	}
	if follow := "name its real path (" + filepath.Join(real, "b") + ") instead"; strings.Contains(r.Stderr, follow) {
		if r2 := b.outside(t, b.proj, b.env, "git -C "+filepath.Join(real, "b")+" ls-remote origin"); !strings.Contains(r2.Stdout, "refs/heads/main") {
			t.Errorf("the note says %q, but that line is kept inside too: %q", follow, r2.Stderr)
		}
	}
}
