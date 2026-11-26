//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T251 with T248's anchors, through the one path every folder of a line
// takes (LineFoldersProblem, L1731): once in the registry, the second
// repository and each folder in it the line's git works in are opened with
// no link followed and walked up to the repository's top; a repository or
// a folder swapped for a link is refused.
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
	t.Chdir(base)
	in := Input{Cwd: base, Home: base, Stat: Disk, Lstat: DiskLstat}
	for _, line := range []string{"git -C " + b + " fetch", "git -C " + b + "/sub pull", "cd " + b + " && git push"} {
		if p := in.LineFoldersProblem(line, b); p != nil {
			t.Errorf("%q: %v", line, p)
		}
	}
	// A folder in b the line names that is a link now.
	if err := os.Symlink(elsewhere, filepath.Join(b, "out")); err != nil {
		t.Fatal(err)
	}
	if p := in.LineFoldersProblem("git -C "+b+"/out fetch", b); p == nil || !strings.HasPrefix(p.Reason, "the folder "+filepath.Join(b, "out")+" git -C names changed after Jhon read it: it is or passes through a link now") {
		t.Errorf("a folder of b swapped for a link: %v", p)
	}
	// b itself swapped for a link to another folder.
	if err := os.Rename(b, filepath.Join(base, "work", "b-real")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, b); err != nil {
		t.Fatal(err)
	}
	if p := in.LineFoldersProblem("git -C "+b+" fetch", b); p == nil || !strings.HasPrefix(p.Reason, "the repository "+b+" changed after Jhon read it: it is or passes through a link now") {
		t.Errorf("b swapped for a link: %v", p)
	}
}
