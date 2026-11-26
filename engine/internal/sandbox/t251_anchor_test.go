//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T251 with T248's anchors: once in the registry, the second repository is
// opened with no link followed and must be, by device and inode, the folder
// Jhon judged, and each folder in it the line's git works in must be
// inside it on the disk; a repository or a folder swapped for a link is
// refused, and the project's line (no repository) checks nothing here.
func TestT251RepositoryProblemAnchorsTheRepository(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(base, "work", "b")
	elsewhere := filepath.Join(base, "elsewhere")
	for _, d := range []string{filepath.Join(b, ".git"), filepath.Join(b, "sub"), elsewhere} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	in := Input{Cwd: base, Home: base, Stat: Disk, Lstat: DiskLstat}
	if p := in.RepositoryProblem("", "git fetch"); p != nil {
		t.Errorf("the project's line: %v", p)
	}
	for _, line := range []string{"git -C " + b + " fetch", "git -C " + b + "/sub pull", "cd " + b + " && git push"} {
		if p := in.RepositoryProblem(b, line); p != nil {
			t.Errorf("%q: %v", line, p)
		}
	}
	// A folder in b the line names that is a link now.
	if err := os.Symlink(elsewhere, filepath.Join(b, "out")); err != nil {
		t.Fatal(err)
	}
	if p := in.RepositoryProblem(b, "git -C "+b+"/out fetch"); p == nil || !strings.HasPrefix(p.Reason, "the repository "+b+" changed after Jhon read it: "+filepath.Join(b, "out")+" is or passes through a link") {
		t.Errorf("a folder of b swapped for a link: %v", p)
	}
	// b itself swapped for a link to another folder.
	if err := os.Rename(b, filepath.Join(base, "work", "b-real")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, b); err != nil {
		t.Fatal(err)
	}
	if p := in.RepositoryProblem(b, "git -C "+b+" fetch"); p == nil || !strings.HasPrefix(p.Reason, "the repository "+b+" changed after Jhon read it: "+b+" cannot be opened with no link followed") {
		t.Errorf("b swapped for a link: %v", p)
	}
}
