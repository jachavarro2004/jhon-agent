package ldd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// countGit puts a git on PATH that logs each invocation's arguments, one
// line each, before it runs the real one; the log's path is returned.
func countGit(t *testing.T) string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not on PATH")
	}
	bin := t.TempDir()
	log := filepath.Join(bin, "git.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + log + "'\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// gitCalls are the invocations the log holds since it was last cleared.
func gitCalls(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return nonEmpty(string(b))
}

// One status reads the change's diff and its risk once (S16): the route,
// its risk, the slice in progress, and the reviews share one reading, so no
// diff, untracked listing, or base lookup runs twice with the same arguments.
// HEAD and the top level are left out: vcs reads them too, on its own.
func TestStatusReadsTheDiffOnce(t *testing.T) {
	jhonRoot(t)
	root := routeRepo(t, 3, "src/app.py", "tests/test_app.py")
	write(t, filepath.Join(root, "openspec/changes/c1/tasks.md"), "## Intent\n\nwhy\n\n## Slice 1: one\n\n- [x] a\n\n## Slice 2: two\n\n- [ ] b\n")
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "slice 1")
	write(t, filepath.Join(root, "src/more.py"), "y = 2\n")
	log := countGit(t)

	s, err := StatusOf(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Route == nil || s.Route.Slice == nil {
		t.Fatalf("status has no route or slice: %+v", s.Route)
	}
	seen := map[string]int{}
	for _, c := range gitCalls(t, log) {
		args := strings.SplitN(c, " ", 3)
		if len(args) < 3 || args[0] != "-C" {
			continue
		}
		switch rest := args[2]; {
		case strings.HasPrefix(rest, "diff "), strings.HasPrefix(rest, "ls-files --others"), strings.HasPrefix(rest, "merge-base "),
			strings.HasPrefix(rest, "rev-parse --verify") && !strings.Contains(rest, " HEAD^"):
			seen[rest]++
		}
	}
	if len(seen) == 0 {
		t.Fatalf("no git invocation logged: %v", gitCalls(t, log))
	}
	for c, n := range seen {
		if n > 1 {
			t.Errorf("git %s ran %d times in one status", c, n)
		}
	}
}
