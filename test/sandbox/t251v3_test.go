// T251 third re-verify (odd/tasks/v0-1-release.md L1729, L1731): the
// folders a line outside the sandbox writes are judged as they are once it
// is in the registry, not as Jhon remembered them before, and a kept
// line's note names the cause it found.
package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A clone's new folder whose first missing folder a sandboxed command makes
// a link to the home after Jhon read the line (it did not exist then, so
// the deepest existing folder Jhon remembered is the repository): git
// creates the clone where the link leads.
func TestT251V3ACloneIntoAFolderMadeALinkAfterJhonReadItKeepsTheLineInside(t *testing.T) {
	for _, where := range []string{"repository", "project"} {
		t.Run(where, func(t *testing.T) {
			b := newT251Box(t, "")
			dir := b.repo
			if where == "project" {
				dir = b.proj
			}
			r := t251v2Race(t, b, "ln -s \"$HOME\" "+dir+"/nd", b.proj, "git -C "+dir+" clone -q ssh://example.invalid"+b.remote+" nd/newc")
			if exists(filepath.Join(b.home, "newc")) {
				t.Errorf("the clone wrote %s, in the home (exit %d, stderr %q)", filepath.Join(b.home, "newc"), r.Code, r.Stderr)
			}
		})
	}
}

// A -C through a link that was always there stays inside (declared in
// ecfaf7e), and its note says so, with the real path: not that a folder
// changed and the line should run again.
func TestT251V3AStaticLinkInTheChainSaysItIsALink(t *testing.T) {
	b := newT251Box(t, "")
	link := filepath.Join(b.home, "alink")
	if err := os.Symlink(b.proj, link); err != nil {
		t.Fatal(err)
	}
	r := b.outside(t, b.proj, b.env, "git -C "+link+" fetch origin")
	if !strings.Contains(r.Stderr, t251Kept) || strings.Contains(r.Stderr, "changed after Jhon read it") || strings.Contains(r.Stderr, "run the line again") || !strings.Contains(r.Stderr, b.proj) {
		t.Errorf("exit %d, stderr %q; want a note naming the link and the real path %s", r.Code, r.Stderr, b.proj)
	}
}
