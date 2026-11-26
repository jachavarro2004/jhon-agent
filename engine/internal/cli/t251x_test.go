package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/sandbox"
)

// L1731 N1: a line kept inside because the folder it runs in is a
// repository outside every allowed folder gets the reason repositoryAt
// gives, with its fix, never the fix for a .git entry a sandboxed command
// planted; one run from the second repository while its git works in the
// project says so. A planted link keeps the planted fix.
func TestT251YRepositoryGitProblemGivesTheFixOfItsReason(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("skipped: no git")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	proj, other, work := filepath.Join(base, "project"), filepath.Join(base, "other"), filepath.Join(base, "work")
	b := filepath.Join(work, "b")
	for _, d := range []string{proj, other, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("git", "init", "-q", d).CombinedOutput(); err != nil {
			t.Fatalf("git init %s: %v %s", d, err, out)
		}
	}
	in := func(cwd string) sandbox.Input {
		i := sandbox.Current(cwd, proj)
		i.AllowWrite = []string{work}
		return i
	}
	p := repositoryGitProblem(in(other), "gh pr list", nil, "")
	if p == nil || p.Reason != other+" is neither in the project ("+proj+") nor in a git repository inside a sandbox.allow_write folder" || strings.Contains(p.Fix, ".git") {
		t.Errorf("gh pr list in a repository outside every allowed folder: %+v", p)
	}
	p = repositoryGitProblem(in(b), "git -C "+proj+" fetch origin", nil, "")
	if want := "the line runs in " + b + ", in the repository " + b + " of sandbox.allow_write, while its git works in the project (" + proj + "), and a line runs outside the sandbox in one repository only"; p == nil || p.Reason != want || strings.Contains(p.Fix, ".git") {
		t.Errorf("git -C <project> fetch run in b: %+v; want %q", p, want)
	}
	if err := os.Symlink(filepath.Join(base, "victim"), filepath.Join(proj, ".git", "FETCH_HEAD")); err != nil {
		t.Fatal(err)
	}
	p = repositoryGitProblem(in(proj), "git fetch origin", nil, "")
	if p == nil || !strings.HasSuffix(p.Reason, "FETCH_HEAD in the repository's git folder is a link, which git would write through") || p.Fix != "remove that from the repository (a sandboxed command may have put it there), or run the line yourself in a terminal" {
		t.Errorf("a planted link: %+v", p)
	}
}
