// T251's second re-verify (odd/tasks/v0-1-release.md L1731): the notes of a
// line kept inside name the real reason and a fix that works.
package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// cdk run in the second repository stays inside (only git and gh count
// it), and its note says so instead of "run it alone".
func TestT251YADeployToolInTheRepositorySaysWhy(t *testing.T) {
	b := newT251Box(t, "deploy_tools = [\"cdk\"]\n")
	script(t, filepath.Join(b.base, "t251-bin", "cdk"), "#!/bin/sh\nif [ \"$JHON_IN_SANDBOX\" = 1 ]; then echo 'cdk: no credentials' >&2; exit 1; fi\necho ran >> "+b.note+"\n")
	r := b.outside(t, b.proj, b.env, "cd "+b.repo+" && cdk deploy")
	want := "\ncdk ran in the sandbox, not outside it: cdk runs outside the sandbox only in the project, and " + b.repo + " is in a git repository inside a sandbox.allow_write folder, where only git and gh do; fix: run cdk on a line of its own, from the project or a session of that repository\n"
	if exists(b.note) || !strings.HasSuffix(r.Stderr, want) || strings.Contains(r.Stderr, "run it alone") {
		t.Errorf("exit %d, ran outside %v, stderr %q; want it to end with %q", r.Code, exists(b.note), r.Stderr, want)
	}
}

// A `..` after a link that leads out of every allowed folder: the fix does
// not name a path that is kept inside too. allow_write = ["~"], which the
// settings refuse, is named as the reason allow_write counts for nothing.
func TestT251YTheFixOfAKeptLineWorks(t *testing.T) {
	b := newT251Box(t, "")
	y := filepath.Join(b.home, ".cache", "q", "y")
	mkdirs(t, y, filepath.Join(b.home, ".cache", "q", "b"))
	if err := os.Symlink(y, filepath.Join(b.work, "x")); err != nil {
		t.Fatal(err)
	}
	r := b.outside(t, b.proj, b.env, "git -C "+b.work+"/x/../b ls-remote origin")
	if !strings.Contains(r.Stderr, t251Kept) || strings.Contains(r.Stderr, "name its real path") {
		t.Errorf("a `..` leading out: exit %d, stderr %q; want a kept note whose fix names no path kept inside", r.Code, r.Stderr)
	}
	other := filepath.Join(b.home, "other")
	mkdirs(t, other)
	support.GitRepo(t, other)
	support.Git(t, other, "remote", "add", "origin", "ssh://example.invalid"+b.remote)
	b.settings(t, "schema = 1\n\n[sandbox]\nallow_write = [\"~\"]\n")
	r = b.outside(t, b.proj, b.env, "git -C "+other+" ls-remote origin")
	if !strings.Contains(r.Stderr, `; sandbox.allow_write counts for nothing now, as the settings cannot be read: `) || !strings.Contains(r.Stderr, `sandbox.allow_write: "~" is not accepted`) {
		t.Errorf("allow_write ~: exit %d, stderr %q", r.Code, r.Stderr)
	}
}
