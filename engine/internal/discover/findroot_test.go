package discover

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitIn runs git in dir with a fixed identity and no inherited GIT_*
// variable, failing the test when it fails.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=jhon-test", "-c", "user.email=jhon-test@example.invalid",
		"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_") {
			cmd.Env = append(cmd.Env, e)
		}
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}

// repoAt makes dir (created when missing) a git repository with one commit
// of what it holds.
func repoAt(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
	return dir
}

// realTree is tree by its real path, so roots compare as FindRoot reports them.
func realTree(t *testing.T, files map[string]string) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(tree(t, files))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// Project discovery stops at the git toplevel (L786, L853): a layout above
// the repository is not the repository's; inside it, and in a folder in no
// repository, discovery is as it was.
func TestFindRootStopsAtTheGitToplevel(t *testing.T) {
	t.Run("openspec above the repository is ignored", func(t *testing.T) {
		parent := realTree(t, map[string]string{"openspec/config.yaml": "delivery: slice\n", "openspec/changes/c1/tasks.md": "x\n", "inner/a/b/x": ""})
		inner := repoAt(t, filepath.Join(parent, "inner"))
		for _, from := range []string{inner, filepath.Join(inner, "a", "b")} {
			if got := FindRoot(from); got != from {
				t.Errorf("FindRoot(%s) = %q, want the directory itself", from, got)
			}
		}
	})
	t.Run("openspec changes only above the repository is ignored", func(t *testing.T) {
		parent := realTree(t, map[string]string{"openspec/changes/c1/tasks.md": "x\n"})
		inner := repoAt(t, filepath.Join(parent, "inner"))
		if got := FindRoot(inner); got != inner {
			t.Errorf("FindRoot = %q, want %q", got, inner)
		}
	})
	t.Run("ldd above the repository is ignored", func(t *testing.T) {
		parent := realTree(t, map[string]string{"ldd/config.yaml": "delivery: slice\n"})
		inner := repoAt(t, filepath.Join(parent, "inner"))
		if got := FindRoot(inner); got != inner {
			t.Errorf("FindRoot = %q, want %q", got, inner)
		}
	})
	for _, dir := range []string{"openspec", "ldd"} {
		t.Run(dir+" at the git root is found", func(t *testing.T) {
			root := repoAt(t, realTree(t, map[string]string{dir + "/config.yaml": "delivery: slice\n", "a/b/x": ""}))
			for _, from := range []string{root, filepath.Join(root, "a", "b")} {
				if got := FindRoot(from); got != root {
					t.Errorf("FindRoot(%s) = %q, want %q", from, got, root)
				}
			}
		})
	}
	t.Run("a nested repository resolves to its own toplevel", func(t *testing.T) {
		outer := repoAt(t, realTree(t, map[string]string{"openspec/config.yaml": "delivery: slice\n"}))
		nested := repoAt(t, filepath.Join(outer, "vendor", "nested"))
		if got := FindRoot(filepath.Join(outer, "vendor")); got != outer {
			t.Errorf("FindRoot in the outer repository = %q, want %q", got, outer)
		}
		if got := FindRoot(nested); got != nested {
			t.Errorf("FindRoot in the nested repository = %q, want %q", got, nested)
		}
		withOwn := repoAt(t, filepath.Join(outer, "vendor", "own"))
		if err := os.MkdirAll(filepath.Join(withOwn, "ldd", "changes"), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := FindRoot(withOwn); got != withOwn {
			t.Errorf("FindRoot in a nested repository with its own layout = %q, want %q", got, withOwn)
		}
	})
	t.Run("a linked worktree", func(t *testing.T) {
		parent := realTree(t, map[string]string{"openspec/config.yaml": "delivery: slice\n", "main/openspec/config.yaml": "delivery: slice\n", "main/src/x": "", "plain/src/x": ""})
		main := repoAt(t, filepath.Join(parent, "main"))
		wt := filepath.Join(parent, "main-worktrees", "c1")
		gitIn(t, main, "worktree", "add", "-q", "-b", "c1", wt)
		if got := FindRoot(filepath.Join(wt, "src")); got != wt {
			t.Errorf("FindRoot in a linked worktree carrying openspec/ = %q, want %q", got, wt)
		}
		plain := repoAt(t, filepath.Join(parent, "plain"))
		pwt := filepath.Join(parent, "plain-worktrees", "c1")
		gitIn(t, plain, "worktree", "add", "-q", "-b", "c1", pwt)
		from := filepath.Join(pwt, "src")
		if got := FindRoot(from); got != from {
			t.Errorf("FindRoot in a linked worktree under a folder holding openspec/ = %q, want %q", got, from)
		}
	})
	t.Run("a folder in no repository is unchanged", func(t *testing.T) {
		parent := realTree(t, map[string]string{"openspec/config.yaml": "delivery: slice\n", "a/b/x": ""})
		if got := FindRoot(filepath.Join(parent, "a", "b")); got != parent {
			t.Errorf("openspec/ above a folder in no repository: FindRoot = %q, want %q", got, parent)
		}
		native := realTree(t, map[string]string{"ldd/config.yaml": "delivery: slice\n", "a/x": ""})
		if got := FindRoot(native); got != native {
			t.Errorf("ldd/ at a folder in no repository: FindRoot = %q, want %q", got, native)
		}
		if from := filepath.Join(native, "a"); FindRoot(from) != from {
			t.Errorf("ldd/ above a folder in no repository: FindRoot = %q, want %q", FindRoot(from), from)
		}
	})
}
