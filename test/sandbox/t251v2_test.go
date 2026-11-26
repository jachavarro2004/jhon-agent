// T251 re-verify (odd/tasks/v0-1-release.md L1721, L1722, L1729): what a
// line outside the sandbox writes is judged after it enters the registry,
// and a kept line's note names a fix that works.
package sandbox

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// t251v2Race starts a sandboxed command of the box's project that runs
// swap after a second, and runs line outside the sandbox from dir while it
// waits in the registry for that command.
func t251v2Race(t *testing.T, b t251Box, swap, dir, line string) support.Result {
	t.Helper()
	racer := exec.Command(support.Bin(t), "sandbox", "run", "--project", b.proj, "--", "sh", "-c", "sleep 1; "+swap)
	racer.Dir, racer.Env = b.proj, b.env
	if err := racer.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { racer.Process.Kill(); racer.Wait() })
	time.Sleep(300 * time.Millisecond)
	r := b.outside(t, dir, b.env, line)
	if err := racer.Wait(); err != nil {
		t.Fatalf("the sandboxed swap: %v", err)
	}
	return r
}

// A clone into the repository writes its new folder where the folder that
// holds it leads when git runs: a sandboxed command that makes that folder
// a link to the home after Jhon judged the line must not get the clone
// written in the home.
func TestT251V2ACloneTargetSwappedForALinkKeepsTheLineInside(t *testing.T) {
	b := newT251Box(t, "")
	mkdirs(t, filepath.Join(b.repo, "sub"))
	r := t251v2Race(t, b, "mv "+b.repo+"/sub "+b.repo+"/sub.old && ln -s \"$HOME\" "+b.repo+"/sub", b.proj,
		"git -C "+b.repo+" clone -q ssh://example.invalid"+b.remote+" sub/newc")
	if exists(filepath.Join(b.home, "newc")) {
		t.Errorf("the clone wrote %s, in the home (exit %d, stderr %q)", filepath.Join(b.home, "newc"), r.Code, r.Stderr)
	}
}

// The same for a project's folder named with a `..` that had no link when
// Jhon read it (advisory: also at bf705e0): git goes up from where the link
// a sandboxed command made leads.
func TestT251V2AProjectDotDotSwappedForALinkKeepsTheLineInside(t *testing.T) {
	b := newT251Box(t, "")
	support.Write(t, b.home, map[string]string{".bashrc": "owner\n"})
	mkdirs(t, filepath.Join(b.proj, "sub"), filepath.Join(b.proj, "b"))
	q := filepath.Join(b.home, ".cache", "q")
	swap := "set -e; mkdir -p " + q + "/y; git init -q " + q + "/b; git -C " + q + "/b remote add origin ssh://example.invalid" + b.remote +
		"; ln -s \"$HOME/.bashrc\" " + q + "/b/.git/FETCH_HEAD; mv " + b.proj + "/sub " + b.proj + "/sub.old; ln -s " + q + "/y " + b.proj + "/sub"
	r := t251v2Race(t, b, swap, b.proj, "git -C "+b.proj+"/sub/../b fetch origin")
	if got := support.Read(t, filepath.Join(b.home, ".bashrc")); got != "owner\n" {
		t.Errorf("the line ran outside where the link leads (exit %d, stderr %q): ~/.bashrc is %q", r.Code, r.Stderr, got)
	}
}

// gh run in a repository outside every allowed folder stays inside, and its
// note says what L1721 asks: the repository is neither the project nor in
// an allowed folder, with that fix, never to remove a .git entry.
func TestT251V2GhInARepositoryOutsideEveryAllowedFolderSaysWhy(t *testing.T) {
	b := newT251Box(t, "")
	script(t, filepath.Join(b.base, "t251-bin", "gh"), "#!/bin/sh\nif [ \"$JHON_IN_SANDBOX\" = 1 ]; then echo 'gh: no credentials' >&2; exit 4; fi\necho ran >> "+b.note+"\n")
	other := filepath.Join(b.home, "other")
	mkdirs(t, other)
	support.GitRepo(t, other)
	for _, c := range []struct{ dir, line string }{{other, "gh pr list"}, {b.repo, "git -C " + b.proj + " fetch origin"}} {
		r := b.outside(t, c.dir, b.env, c.line)
		if !strings.Contains(r.Stderr, t251Kept) || strings.Contains(r.Stderr, "remove the .git") || strings.Contains(r.Stderr, "a sandboxed command may have put it there") {
			t.Errorf("%q in %s: exit %d, stderr %q; want a note naming the real reason and a fix that works", c.line, c.dir, r.Code, r.Stderr)
		}
	}
}
