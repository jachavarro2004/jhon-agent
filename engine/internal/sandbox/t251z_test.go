//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// L1732: nothing that runs after the registry reads a remembered fact.
// LineFoldersProblem takes its Input through Fresh, so the Stat and Lstat a
// caller built before the registry (sandbox.Current's remembered Disk) are
// never asked; a clone's folder that did not exist when Jhon read the line
// and is a link now keeps the line inside (C2), and one still missing is
// git's to create under the folder that exists.
func TestT251ZThePostRegistryCheckReadsTheDiskAnew(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	proj, b := filepath.Join(base, "project"), filepath.Join(base, "work", "b")
	for _, d := range []string{filepath.Join(proj, ".git"), filepath.Join(b, ".git")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(proj)
	asked := 0
	in := Input{Cwd: proj, Roots: []string{proj}, CommonDir: filepath.Join(proj, ".git"), Home: base, AllowWrite: []string{filepath.Join(base, "work")},
		Stat:  func(string) (string, bool, bool) { asked++; return "", false, false },
		Lstat: func(string) (string, bool, bool) { asked++; return "", false, false },
	}
	for _, c := range []struct{ line, top string }{
		{"git -C " + b + " clone https://h/o/r nd/newc", b},
		{"git clone https://h/o/r nd/newc", ""},
		{"git -C " + proj + "/nd/.. fetch", ""},
	} {
		if p := in.LineFoldersProblem(c.line, c.top); p != nil {
			t.Errorf("%q before any link: %v", c.line, p)
		}
	}
	if asked != 0 {
		t.Errorf("the post-registry check asked the Input's own Stat or Lstat %d times; want none (Fresh)", asked)
	}
	for _, c := range []struct{ dir, top string }{{b, b}, {proj, ""}} {
		if err := os.Symlink(base, filepath.Join(c.dir, "nd")); err != nil {
			t.Fatal(err)
		}
		line := "git -C " + c.dir + " clone https://h/o/r nd/newc"
		if p := in.LineFoldersProblem(line, c.top); p == nil || !strings.HasPrefix(p.Reason, "the folder git clone writes "+c.dir+"/nd/newc in changed after Jhon read it: it is or passes through a link now") {
			t.Errorf("%q with nd made a link: %v", line, p)
		}
	}
}
