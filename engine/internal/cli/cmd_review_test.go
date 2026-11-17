package cli

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// captureAll runs argv and returns its exit code, stdout and stderr.
func captureAll(t *testing.T, argv []string) (int, string, string) {
	t.Helper()
	read := func(f **os.File) func() string {
		r, w, _ := os.Pipe()
		old := *f
		*f = w
		done := make(chan []byte)
		go func() { b, _ := io.ReadAll(r); done <- b }()
		return func() string { *f = old; w.Close(); return string(<-done) }
	}
	out, errOut := read(&os.Stdout), read(&os.Stderr)
	code, err := Run(argv)
	o, e := out(), errOut()
	if err != nil {
		t.Fatal(err)
	}
	return code, o, e
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// objects is every object in the repository's store, so a test sees
// whether a command wrote any.
func objects(t *testing.T, dir string) string {
	return gitIn(t, dir, "cat-file", "--batch-all-objects", "--batch-check")
}

func reviewRepo(t *testing.T) string {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	root := filepath.Join(dir, "main")
	if err := os.MkdirAll(root, 0o777); err != nil {
		t.Fatal(err)
	}
	gitIn(t, root, "init", "-q")
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("a\n"), 0o666)
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-q", "-m", "base")
	return root
}

// T87a (L629, L630): a freeze of an empty diff reviews nothing; it is
// refused with its cause and its fix, and writes nothing.
func TestReviewFreezeRefusesAnEmptySubject(t *testing.T) {
	t.Setenv("JHON_TELEMETRY", "off")
	root := reviewRepo(t)
	t.Chdir(root)
	before := objects(t, root)
	code, out, errOut := captureAll(t, []string{"review", "freeze", "--base", "main"})
	if code != 2 || out != "" {
		t.Errorf("empty freeze: code %d stdout %q; want 2 and nothing", code, out)
	}
	want := "jhon review freeze: nothing to freeze in " + root + ": the diff against main is empty; run it from the change's worktree (`cd <worktree>` or `git -C <worktree>`)\n"
	if errOut != want {
		t.Errorf("stderr = %q\nwant     %q", errOut, want)
	}
	if after := objects(t, root); after != before {
		t.Errorf("an empty freeze wrote objects:\n%s\nwas\n%s", after, before)
	}
}

// T87a: the refusal names the repository's other worktrees that hold
// changes, so the agent knows where to freeze.
func TestReviewFreezeNamesTheWorktreesWithChanges(t *testing.T) {
	t.Setenv("JHON_TELEMETRY", "off")
	root := reviewRepo(t)
	dir := filepath.Dir(root)
	committed, dirty, idle := filepath.Join(dir, "wt-committed"), filepath.Join(dir, "wt-dirty"), filepath.Join(dir, "wt-idle")
	gitIn(t, root, "worktree", "add", "-q", "-b", "c1", committed)
	gitIn(t, root, "worktree", "add", "-q", "-b", "c2", dirty)
	gitIn(t, root, "worktree", "add", "-q", "-b", "c3", idle)
	os.WriteFile(filepath.Join(committed, "b.txt"), []byte("b\n"), 0o666)
	gitIn(t, committed, "add", "-A")
	gitIn(t, committed, "commit", "-q", "-m", "c1")
	os.WriteFile(filepath.Join(dirty, "a.txt"), []byte("dirty\n"), 0o666)
	t.Chdir(root)
	code, _, errOut := captureAll(t, []string{"review", "freeze", "--base", "main"})
	want := "jhon review freeze: nothing to freeze in " + root + ": the diff against main is empty; run it from the change's worktree (`cd <worktree>` or `git -C <worktree>`)\n" +
		"worktrees with changes:\n  " + committed + "\n  " + dirty + "\n"
	if code != 2 || errOut != want {
		t.Errorf("code %d stderr = %q\nwant %q", code, errOut, want)
	}
	// From the worktree the same freeze has a subject.
	t.Chdir(committed)
	code, out, errOut := captureAll(t, []string{"review", "freeze", "--base", "main"})
	if code != 0 || errOut != "" || !regexp.MustCompile(`^BASE=[0-9a-f]{40}\nCANDIDATE=[0-9a-f]{40}\nSUBJECT=[0-9a-f]{64}\n$`).MatchString(out) {
		t.Errorf("freeze in the worktree: %d %q %q", code, out, errOut)
	}
}

// A freeze with changes is unchanged: the same three lines, the subject the
// SHA-256 of the binary diff from the base to the working tree.
func TestReviewFreezeWithChangesIsUnchanged(t *testing.T) {
	t.Setenv("JHON_TELEMETRY", "off")
	root := reviewRepo(t)
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("b\n"), 0o666)
	t.Chdir(root)
	code, out, errOut := captureAll(t, []string{"review", "freeze"})
	base := gitIn(t, root, "rev-parse", "HEAD")
	m := regexp.MustCompile(`^BASE=([0-9a-f]{40})\nCANDIDATE=([0-9a-f]{40})\nSUBJECT=([0-9a-f]{64})\n$`).FindStringSubmatch(out)
	if code != 0 || errOut != "" || m == nil || m[1] != base {
		t.Fatalf("freeze: %d %q %q", code, out, errOut)
	}
	cmd := exec.Command("sh", "-c", "git diff --binary "+base+" "+m[2]+" | sha256sum")
	cmd.Dir = root
	h, err := cmd.Output()
	if err != nil || !strings.HasPrefix(string(h), m[3]) {
		t.Errorf("subject %s is not the diff's sha256 %q (%v)", m[3], h, err)
	}
	code, js, _ := captureAll(t, []string{"review", "freeze", "--json"})
	if want := "{\n  \"base\": \"" + m[1] + "\",\n  \"candidate\": \"" + m[2] + "\",\n  \"subject\": \"" + m[3] + "\"\n}\n"; code != 0 || js != want {
		t.Errorf("--json: %d %q want %q", code, js, want)
	}
}
