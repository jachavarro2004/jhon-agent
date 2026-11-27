// `jhon doctor` notes a tool's own data folder that is untracked and that
// git does not ignore (T268, L1762): CodeGraph's index made the owner's
// `jhon status` take 90 s. The row is a warning with the .gitignore line;
// once the folder is ignored, the row is gone.
package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestDoctorNamesAnUnignoredToolDataFolder(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	support.Write(t, m.proj, map[string]string{"src/app.py": "X = 1\n"})
	support.Git(t, m.proj, "add", "-A")
	support.Git(t, m.proj, "commit", "-q", "-m", "src")
	doctor := func() string {
		t.Helper()
		r := support.Jhon(t, m.proj, m.env, "doctor")
		if !strings.Contains(r.Stdout, "discovery") {
			t.Fatalf("doctor: exit %d\n%s", r.Code, r.Out())
		}
		return r.Stdout
	}
	if out := doctor(); strings.Contains(out, "untracked and git does not ignore it") {
		t.Errorf("no tool data, doctor says:\n%s", out)
	}
	support.Write(t, m.proj, map[string]string{".codegraph/codegraph.db": "index\n", "src/.codegraph-wsl/codegraph.db": "index\n", "out/build.log": "x\n"})
	gitignore := filepath.Join(m.proj, ".gitignore")
	rows := []string{
		"  warn  CodeGraph's data folder .codegraph/ in " + m.proj + " is untracked and git does not ignore it: Jhon leaves it out of every tree it reads, but git and other tools read all of it; add the line `/.codegraph/` to " + gitignore + "\n",
		"  warn  CodeGraph's data folder src/.codegraph-wsl/ in " + m.proj + " is untracked and git does not ignore it: Jhon leaves it out of every tree it reads, but git and other tools read all of it; add the line `/src/.codegraph-wsl/` to " + gitignore + "\n",
	}
	out := doctor()
	for _, row := range rows {
		if !strings.Contains(out, row) {
			t.Errorf("an unignored index, doctor says:\n%s\nwant the row %q", out, row)
		}
	}
	if strings.Contains(out, "out/") {
		t.Errorf("doctor names build output as a tool's data:\n%s", out)
	}
	support.Write(t, m.proj, map[string]string{".gitignore": "/.codegraph/\n/src/.codegraph-wsl/\n"})
	if out := doctor(); strings.Contains(out, "untracked and git does not ignore it") {
		t.Errorf("the index ignored, doctor says:\n%s", out)
	}
}
