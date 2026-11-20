package audit

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A session started in a repository whose work lands in a `git worktree
// add` tree beside it: the writes and the change directory of that tree are
// the session's repository's (worktree.go).

// gitT runs git in dir, failing the test on an error. A hook's GIT_DIR,
// GIT_INDEX_FILE, and GIT_WORK_TREE are left out, so a test a pre-commit
// hook runs never reaches the repository being committed.
func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)...)
	c.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "GIT_") })
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// repoWithWorktree is a repository with one commit, and a worktree of it
// beside it in <repo>-worktrees/lane, both resolved.
func repoWithWorktree(t *testing.T) (repo, lane string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	must(t, err)
	repo, lane = filepath.Join(dir, "repo"), filepath.Join(dir, "repo-worktrees", "lane")
	must(t, os.MkdirAll(repo, 0o755))
	gitT(t, repo, "init", "-q")
	must(t, os.WriteFile(filepath.Join(repo, "README"), []byte("x\n"), 0o644))
	gitT(t, repo, "add", "README")
	gitT(t, repo, "commit", "-qm", "init")
	gitT(t, repo, "worktree", "add", "-q", lane, "-b", "lane")
	return repo, lane
}

func TestWorktreesOf(t *testing.T) {
	repo, lane := repoWithWorktree(t)
	eq(t, "from the main tree", worktreesOf(repo), []string{repo, lane})
	eq(t, "from the worktree", worktreesOf(lane), []string{lane, repo})
	eq(t, "from below the top, the same directory in each", worktreesOf(filepath.Join(repo, "svc")), []string{filepath.Join(repo, "svc"), filepath.Join(lane, "svc")})
	eq(t, "outside any repository", worktreesOf("/nonexistent/x"), []string{"/nonexistent/x"})
}

func TestEfficiencyCountsTheRepositorysWorktrees(t *testing.T) {
	repo, lane := repoWithWorktree(t)
	other := filepath.Join(filepath.Dir(repo), "other")
	must(t, os.MkdirAll(other, 0o755))
	gitT(t, other, "init", "-q")
	s := delegate("e01", "jhon-builder", chg, writeCall(lane+"/src/a.ts"), writeCall(lane+"/src/b.ts"), writeCall(repo+"/src/c.ts"), writeCall(other+"/x.ts"))
	s.root, s.prompt, s.result = repo, "Add the timeout in `src/a.ts` and `src/c.ts`.", shaped
	w := auditOf(t, user("go"), s).Efficiency[0]
	eq(t, "files written in the worktree and the root; another repository is not the root's", w.FilesWritten, 3)
	eq(t, "relative to the worktree's top", w.OutOfScope, []string{"src/b.ts"})
	named := delegate("e01", "jhon-builder", chg, writeCall(lane+"/src/a.ts"))
	named.root, named.prompt, named.result = repo, "Add the timeout in `"+lane+"/src/a.ts`.", shaped
	eq(t, "a scope naming the worktree's absolute path", auditOf(t, user("go"), named).Efficiency[0].OutOfScope, []string{})
	listed := delegate("e01", "jhon-builder", chg, writeCall(lane+"/svc/a.py"), writeCall(lane+"/docs/n.md"))
	listed.root, listed.prompt, listed.result = repo, "Apply 1.1.\n\n## Allowed edit surfaces\n- svc/**", shaped
	eq(t, "listed surfaces read in the worktree", auditOf(t, user("go"), listed).Efficiency[0].OutOfScope, []string{"docs/n.md"})
}

func TestChangeInAWorktreeOnly(t *testing.T) {
	repo, lane := repoWithWorktree(t)
	dir := filepath.Join(lane, "openspec", "changes", chg)
	must(t, os.MkdirAll(dir, 0o755))
	tasks := "## Slice 1: a\n- [x] 1.1 a\n- [ ] 1.2 b\n\n## Slice 2: b\n- [ ] 2.1 c\n- [ ] 2.2 d\n- [ ] 2.3 e\n"
	must(t, os.WriteFile(filepath.Join(dir, "tasks.md"), []byte(tasks+"- [ ] 2.4 f\n"), 0o644))
	d := delegate("o01", "jhon-builder", chg)
	d.root = repo
	s, errs := LoadSession(sessionWith(t, user("go"), d))
	if len(errs) > 0 {
		t.Fatalf("%+v", errs)
	}
	n, most := slicesOf(s, chg)
	eq(t, "slices and the largest slice's tasks", [2]int{n, most}, [2]int{2, 4})
	eq(t, "the change's roots take in the repository's worktrees", changeRoots(s, chg), []string{repo, lane, testRoot})
}
