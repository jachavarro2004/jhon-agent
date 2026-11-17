package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A run's repo has an identity of its own, so the agent's commits work, and
// ignores Python's bytecode, as a real Python project does (L415, L418).
func TestGitRepoCommitsAndIgnoresBytecode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.py"), []byte("x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if err := gitRepo(dir, env); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		c := exec.Command("git", args...)
		c.Dir, c.Env = dir, env
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if got := git("config", "--local", "user.email"); got != "bench@localhost" {
		t.Errorf("user.email %q", got)
	}
	if got := git("config", "--local", "user.name"); got != "bench" {
		t.Errorf("user.name %q", got)
	}
	git("check-ignore", "-q", "__pycache__/a.cpython-313.pyc")
	if got := git("ls-files", ".gitignore"); got != ".gitignore" {
		t.Errorf("the template commit does not hold .gitignore: %q", got)
	}
	if got := git("status", "--porcelain"); got != "" {
		t.Errorf("the repo is not clean: %q", got)
	}
}
