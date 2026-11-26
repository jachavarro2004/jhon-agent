// T251's third re-verify (odd/tasks/v0-1-release.md L1732): `jhon sandbox
// profile` and the run agree on a -C through a link, and a missing -C
// folder's note says it does not exist.
package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// A -C through a link that was always there, last or earlier in the chain:
// profile answers work, as the run keeps it inside, and the run's note
// names the link and the real path (N2).
func TestT251ZProfileAndRunAgreeOnALinkInTheChain(t *testing.T) {
	b := newT251Box(t, "")
	link := filepath.Join(b.home, "alink")
	if err := os.Symlink(b.proj, link); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"git -C " + link + " fetch origin", "git -C " + link + " -C " + b.proj + " fetch origin", "cd " + link + " && git fetch origin"} {
		if r := support.Jhon(t, b.proj, b.env, "sandbox", "profile", "--project", b.proj, "--", line); r.Stdout != "work\n" {
			t.Errorf("profile of %q: %q, want work", line, r.Stdout)
		}
		r := b.outside(t, b.proj, b.env, line)
		want := t251Kept + link + " is or passes through the link " + link + ", and a line outside the sandbox works in a folder only by a path with no link; fix: name its real path (" + b.proj + ") instead\n"
		if !strings.HasSuffix(r.Stderr, want) || strings.Contains(r.Stderr, "changed after Jhon read it") {
			t.Errorf("%q: exit %d, stderr %q; want it to end with %q", line, r.Code, r.Stderr, want)
		}
	}
}

// A -C that names no folder: the note says it does not exist, not that git
// cannot list its configuration.
func TestT251ZAMissingFolderSaysItDoesNotExist(t *testing.T) {
	b := newT251Box(t, "")
	nope := filepath.Join(b.proj, "nope")
	r := b.outside(t, b.proj, b.env, "git -C "+nope+" fetch origin")
	want := t251Kept + nope + " does not exist, so git cannot change into it; fix: name a folder that exists\n"
	if !strings.HasSuffix(r.Stderr, want) || strings.Contains(r.Stderr, "cannot list the configuration") {
		t.Errorf("exit %d, stderr %q; want it to end with %q", r.Code, r.Stderr, want)
	}
}
