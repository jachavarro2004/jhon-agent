package vcs

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	root, _ := filepath.Abs("../../..")
	os.Setenv("JHON_ROOT", root)
	os.Exit(m.Run())
}

// gitT runs a test git command in dir, without inherited GIT_* variables.
func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=jhon-test", "-c", "user.email=jhon-test@example.invalid",
		"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main", "-C", dir}, args...)...)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// dir is a fresh temporary directory, resolved, that no repository encloses.
func dir(t *testing.T) string {
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if GitTopLevel(d) != "" {
		t.Skip("the temporary directory is inside a git work tree")
	}
	return d
}

func repo(t *testing.T, commit bool) string {
	d := dir(t)
	gitT(t, d, "init", "-q")
	if commit {
		gitT(t, d, "commit", "-q", "--allow-empty", "-m", "init")
	}
	return d
}

func TestVocabulary(t *testing.T) {
	if got := []string{None(), NoHead(), Ready(), CandidateUnknown()}; !reflect.DeepEqual(got, []string{"none", "no_head", "ready", "unknown"}) {
		t.Errorf("vocabulary = %q", got)
	}
}

func TestState(t *testing.T) {
	plain := dir(t)
	if v := State(plain); v.State != None() || v.Reason != plain+" is not inside a git work tree" {
		t.Errorf("outside a repository: %+v", v)
	}
	if v := State(repo(t, false)); v.State != NoHead() {
		t.Errorf("git init without a commit: %+v", v)
	}
	ready := repo(t, true)
	os.Mkdir(filepath.Join(ready, "src"), 0o755)
	for _, at := range []string{ready, filepath.Join(ready, "src")} {
		if v := State(at); v.State != Ready() || !strings.HasPrefix(v.Reason, "HEAD resolves to ") || len(v.Reason) != len("HEAD resolves to ")+12 {
			t.Errorf("a repository with one commit, from %s: %+v", at, v)
		}
	}
	wt := filepath.Join(dir(t), "wt")
	gitT(t, ready, "worktree", "add", "-q", "-b", "side", wt)
	if fi, err := os.Lstat(filepath.Join(wt, ".git")); err != nil || !fi.Mode().IsRegular() {
		t.Errorf("a linked worktree's .git must be a file: %v", err)
	}
	if v := State(wt); v.State != Ready() {
		t.Errorf("a linked worktree: %+v", v)
	}
}

func TestInheritedGitDirDoesNotDecide(t *testing.T) {
	ready := repo(t, true)
	t.Setenv("GIT_DIR", filepath.Join(ready, ".git"))
	t.Setenv("GIT_WORK_TREE", ready)
	if v := State(dir(t)); v.State != None() {
		t.Errorf("GIT_DIR decided another root's state: %+v", v)
	}
}

// snapshot is every entry under d with its bytes, for before/after comparison.
func snapshot(d string) []string {
	var out []string
	filepath.WalkDir(d, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			out = append(out, p+"/")
			return nil
		}
		b, _ := os.ReadFile(p)
		out = append(out, p+"\x00"+string(b))
		return nil
	})
	return out
}

func TestStateWritesNothing(t *testing.T) {
	plain := dir(t)
	os.WriteFile(filepath.Join(plain, "a.txt"), []byte("x"), 0o644)
	for _, root := range []string{plain, repo(t, false), repo(t, true)} {
		before := snapshot(root)
		State(root)
		if after := snapshot(root); !reflect.DeepEqual(after, before) {
			t.Errorf("State wrote under %s", root)
		}
	}
}

func TestCandidate(t *testing.T) {
	u := CandidateUnknown()
	if c := CandidateOf(dir(t), nil); c != (Candidate{u, u, u}) {
		t.Errorf("outside a repository: %+v", c)
	}
	root := repo(t, true)
	c := CandidateOf(root, nil)
	if len(c.Commit) != 40 || c.Commit != HeadSha(root) || len(c.Tree) != 40 || c.Clean != true {
		t.Errorf("a clean repository: %+v", c)
	}
	os.WriteFile(filepath.Join(root, "record.json"), nil, 0o644)
	if c := CandidateOf(root, nil); c.Clean != false {
		t.Errorf("an untracked file is unclean: %+v", c)
	}
	if c := CandidateOf(root, map[string]bool{"record.json": true}); c.Clean != true {
		t.Errorf("an exempt path is clean: %+v", c)
	}
	if HeadSha(repo(t, false)) != "" {
		t.Error("HeadSha below ready")
	}
}

func TestDiffs(t *testing.T) {
	root := repo(t, true)
	empty := CandidateOf(root, nil).Tree
	os.WriteFile(filepath.Join(root, "b.txt"), []byte("b"), 0o644)
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644)
	gitT(t, root, "add", "b.txt")
	if got, ok := WorkingDiff(root, empty); !ok || !reflect.DeepEqual(got, []string{"a.txt", "b.txt"}) {
		t.Errorf("WorkingDiff = %q, %v", got, ok)
	}
	gitT(t, root, "commit", "-q", "-m", "b")
	if got, ok := TreeDiff(root, empty, "HEAD^{tree}"); !ok || !reflect.DeepEqual(got, []string{"b.txt"}) {
		t.Errorf("TreeDiff = %q, %v", got, ok)
	}
	if _, ok := TreeDiff(root, empty, "nope"); ok {
		t.Error("TreeDiff of a tree that does not resolve")
	}
	if _, ok := WorkingDiff(dir(t), empty); ok {
		t.Error("WorkingDiff outside a repository")
	}
}

func TestStatusPaths(t *testing.T) {
	out := " M a.txt\x00R  new.txt\x00old.txt\x00?? dir/x\x00\x00"
	if got := StatusPaths(out); !reflect.DeepEqual(got, []string{"a.txt", "new.txt", "old.txt", "dir/x"}) {
		t.Errorf("StatusPaths = %q", got)
	}
}

func TestNote(t *testing.T) {
	n := Note(Vcs{"none", "/x is not inside a git work tree"})
	if !strings.HasPrefix(n, "vcs none (/x is not inside a git work tree); ") || !strings.Contains(n, "consent") {
		t.Errorf("Note = %q", n)
	}
}

func TestWorkingTreeNamesTheWorkingState(t *testing.T) {
	if WorkingTree(dir(t), nil) != "" {
		t.Error("outside a repository there is no working tree")
	}
	root := repo(t, false)
	write := func(name, s string) {
		if err := os.WriteFile(filepath.Join(root, name), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "a")
	write(".gitignore", "build/\n")
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "init")
	base := WorkingTree(root, nil)
	if len(base) != 40 || base != CandidateOf(root, nil).Tree {
		t.Fatalf("a clean tree is HEAD's: %q", base)
	}
	write("a.txt", "b")
	edited := WorkingTree(root, nil)
	if edited == base {
		t.Error("an uncommitted edit changes it")
	}
	write("new.txt", "n")
	if WorkingTree(root, nil) == edited {
		t.Error("an untracked file changes it")
	}
	os.Remove(filepath.Join(root, "new.txt"))
	if err := os.MkdirAll(filepath.Join(root, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	write("build/out", "x")
	write("record.json", "1")
	if got := WorkingTree(root, map[string]bool{"record.json": true}); got != edited {
		t.Error("an ignored file and an exempt one leave it")
	}
	write("a.txt", "a")
	if WorkingTree(root, map[string]bool{"record.json": true}) != base {
		t.Error("the same bytes are the same tree")
	}
	if out, _ := project.ReadGitCmd(root, "diff", "--cached", "--name-only").Output(); len(out) != 0 {
		t.Errorf("the real index is untouched: %q", out)
	}
}

// A review's frozen candidate is a function of the bytes: every lens that
// freezes the same working state gets the same candidate and subject, so a
// lens can freeze for itself and a differing subject means different bytes.
func TestFreezeIsTheWorkingStateAgainstItsBase(t *testing.T) {
	root := repo(t, false)
	write := func(name, s string) {
		if err := os.WriteFile(filepath.Join(root, name), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "a")
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "base")
	base := HeadSha(root)
	write("a.txt", "b")
	write("new.txt", "n")
	f, err := Freeze(root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := Freeze(root, "HEAD")
	if f.Base != base || len(f.Candidate) != 40 || len(f.Subject) != 64 || !reflect.DeepEqual(f, again) {
		t.Fatalf("freeze = %+v, again %+v", f, again)
	}
	if f.Candidate != WorkingTree(root, nil) {
		t.Error("the candidate is the working tree")
	}
	write("new.txt", "changed")
	if g, _ := Freeze(root, "HEAD"); g.Subject == f.Subject || g.Candidate == f.Candidate {
		t.Error("other bytes, another subject")
	}
	if _, err := Freeze(root, "no-such-ref"); err == nil {
		t.Error("an unknown base is an error")
	}
}

// A file rewritten in the second its index entry was written, at the same
// size ("racy git"), must still be read anew: the copied index keeps the
// real index's mtime, so git's racy-clean check holds as it does there.
func TestWorkingTreeReadsARacilyCleanEdit(t *testing.T) {
	for i := range 2 {
		root := repo(t, false)
		file := filepath.Join(root, "a.txt")
		if err := os.WriteFile(file, []byte("a\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitT(t, root, "add", "-A")
		gitT(t, root, "commit", "-q", "-m", "base")
		if err := os.WriteFile(file, []byte("b\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		// Later than that second, a copy of the index written now would look
		// newer than the edit and trust the stale entry.
		time.Sleep(1100 * time.Millisecond)
		want := WorkingTree(root, nil)
		for j := range 3 {
			if got := WorkingTree(root, nil); got != want {
				t.Fatalf("round %d, call %d: %s, then %s: the edit was read once and lost", i, j, want, got)
			}
		}
		out, _ := project.ReadGitCmd(root, "ls-tree", want, "a.txt").Output()
		blob := strings.Fields(string(out))[2]
		if content, _ := project.ReadGitCmd(root, "cat-file", "-p", blob).Output(); string(content) != "b\n" {
			t.Fatalf("round %d: the tree holds %q, the file %q", i, content, "b\n")
		}
	}
}
