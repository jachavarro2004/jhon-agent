//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// B2 (L1729), through LineFoldersProblem (L1731): once the line is in the
// registry, the folder this process
// works in must be the folder its path names, opened with no link
// followed, and inside the root that path lies in: a folder moved aside
// with a copy put at its path (the repository's or a project subfolder,
// which bf705e0 never anchored either) keeps the line inside.
func TestT251XTheWorkingFolderIsAnchored(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	proj, b := filepath.Join(base, "project"), filepath.Join(base, "work", "b")
	for _, d := range []string{filepath.Join(proj, ".git"), filepath.Join(proj, "sub"), filepath.Join(b, ".git"), filepath.Join(b, "sub")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct{ name, cwd, top string }{
		{"the repository's subfolder", filepath.Join(b, "sub"), b},
		{"the repository", b, b},
		{"the project's subfolder", filepath.Join(proj, "sub"), ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Chdir(c.cwd)
			in := Input{Cwd: c.cwd, Roots: []string{proj}, CommonDir: filepath.Join(proj, ".git"), Home: base, Stat: Disk, Lstat: DiskLstat}
			if p := in.LineFoldersProblem("git fetch", c.top); p != nil {
				t.Fatalf("in place: %v", p)
			}
			aside := c.cwd + "-aside"
			if err := os.Rename(c.cwd, aside); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(c.cwd); os.Rename(aside, c.cwd) })
			if err := os.MkdirAll(filepath.Join(c.cwd, ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			p := in.LineFoldersProblem("git fetch", c.top)
			if want := "the folder this line runs in, " + c.cwd + ", changed after Jhon read it: "; p == nil || !strings.HasPrefix(p.Reason, want) {
				t.Errorf("moved aside with a copy at its path: %v; want a problem opening with %q", p, want)
			}
		})
	}
}
