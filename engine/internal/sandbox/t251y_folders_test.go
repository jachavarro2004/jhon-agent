//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// L1731: every folder a line writes or works in is judged again on the disk
// once the line is in the registry: a project folder made a link after the
// line was read (`sub/../b` then goes where the link leads) and a clone's
// folder swapped for a link keep the line inside; the lines the verifier
// found still running (`-C ..` from a subfolder, `sub/..`, a clone into
// `sub/../newc`, a -C leaving the working folder, a bind mount's folder)
// still pass.
func TestT251YEveryFolderOfTheLineIsAnchored(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	proj, b, q := filepath.Join(base, "project"), filepath.Join(base, "work", "b"), filepath.Join(base, "q")
	for _, d := range []string{filepath.Join(proj, ".git"), filepath.Join(proj, "sub"), filepath.Join(proj, "b"), filepath.Join(b, ".git"), filepath.Join(b, "sub"), filepath.Join(q, "y"), filepath.Join(q, "b")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	in := Input{Roots: []string{proj}, CommonDir: filepath.Join(proj, ".git"), Home: base, Stat: Disk, Lstat: DiskLstat, AllowWrite: []string{filepath.Join(base, "work")}}
	run := func(cwd, line, top string) *Problem {
		t.Helper()
		t.Chdir(cwd)
		in.Cwd = cwd
		return in.LineFoldersProblem(line, top)
	}
	for _, c := range []struct{ cwd, line, top string }{
		{filepath.Join(proj, "sub"), "git -C .. fetch", ""},
		{proj, "git -C " + proj + "/sub/.. fetch", ""},
		{proj, "git clone https://h/o/r sub/../newc", ""},
		{filepath.Join(proj, "sub"), "git -C " + proj + "/b fetch", ""},
		{proj, "git -C " + proj + "/sub/../b fetch", ""},
		{proj, "git -C " + b + " clone https://h/o/r sub/newc", b},
	} {
		if p := run(c.cwd, c.line, c.top); p != nil {
			t.Errorf("%q in %s before any swap: %v", c.line, c.cwd, p)
		}
	}
	// The project's sub becomes a link after Jhon read the line.
	if err := os.Rename(filepath.Join(proj, "sub"), filepath.Join(proj, "sub.old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(q, "y"), filepath.Join(proj, "sub")); err != nil {
		t.Fatal(err)
	}
	if p := run(proj, "git -C "+proj+"/sub/../b fetch", ""); p == nil || !strings.HasPrefix(p.Reason, "the folder "+proj+"/sub/../b git -C names changed after Jhon read it: ") {
		t.Errorf("a project folder's `..` after a new link: %v", p)
	}
	// b's sub, where the clone writes, becomes a link.
	if err := os.Rename(filepath.Join(b, "sub"), filepath.Join(b, "sub.old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(base, filepath.Join(b, "sub")); err != nil {
		t.Fatal(err)
	}
	if p := run(proj, "git -C "+b+" clone https://h/o/r sub/newc", b); p == nil || !strings.HasPrefix(p.Reason, "the folder git clone writes "+b+"/sub/newc in changed after Jhon read it: it is or passes through a link now") {
		t.Errorf("a clone's folder swapped for a link: %v", p)
	}
	// A -C that does not exist is git's to refuse.
	if p := run(proj, "git -C "+proj+"/gone fetch", ""); p != nil {
		t.Errorf("a missing -C folder: %v", p)
	}
}
