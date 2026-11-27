package ldd

// T268 (L1762): the owner's member held nine untracked out-* folders git did
// not ignore, and `jhon risk` and `jhon status` read every file of them.
// Past the budget a check hashes (10000 untracked files or 128 MiB), the
// risk judges the tracked changes only and says once which folders to
// ignore; within it, untracked files are judged as before.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestRiskLeavesAnUntrackedTreeOverTheBudgetUnread(t *testing.T) {
	t.Parallel()
	repo := filepath.Join(temp(t), "proj")
	support.Write(t, repo, map[string]string{"src/a.py": "X = 1\n", "openspec/config.yaml": "schema: spec-driven\n"})
	support.GitRepo(t, repo)
	support.Git(t, repo, "add", "-A")
	support.Git(t, repo, "commit", "-q", "-m", "base")
	support.Git(t, repo, "switch", "-q", "-c", "feat/x")
	support.Write(t, repo, map[string]string{"docs/guide.md": "# guide\n", "out-2/a.log": "x\n"})
	env := support.Env("XDG_STATE_HOME=" + filepath.Join(filepath.Dir(repo), "state"))

	// Within the budget, untracked files are judged as before.
	r := support.Jhon(t, repo, env, "risk", "--base", "main")
	if r.Code != 0 || r.Stdout != "risk: medium (base main)\n  passive docs/guide.md: documentation\n  medium  out-2/a.log: other file\n" {
		t.Errorf("within the budget: exit %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}

	// Past it: the tracked change alone, and the note once.
	out := filepath.Join(repo, "out-1")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range 10000 {
		if err := os.WriteFile(filepath.Join(out, fmt.Sprintf("f%05d", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	support.Write(t, repo, map[string]string{"src/a.py": "X = 2\n"})
	support.Git(t, repo, "add", "src/a.py")
	r = support.Jhon(t, repo, env, "risk", "--base", "main")
	want := "risk: high (base main)\n" +
		"  high    src/a.py: code changed and no test touched\n" +
		"  medium  the working tree holds 10002 untracked files (10 B) git does not ignore, over the 10000 files a check hashes in its time: docs/, out-2/, out-1/; when they are build or test output, add them to .gitignore (docs/, out-2/, out-1/), else commit them; its untracked files are not judged\n"
	if r.Code != 0 || r.Stdout != want {
		t.Errorf("over the budget: exit %d, stderr %q, stdout:\n%s\nwant:\n%s", r.Code, r.Stderr, firstLines(r.Stdout, 6), want)
	}
	// Status's route reads the same risk, and counts no untracked line.
	s := support.Jhon(t, repo, env, "status", "--json")
	route, _ := object(t, s.Stdout)["route"].(map[string]any)
	if s.Code != 0 || route["tier"] != "high" || route["lines"] != float64(1) {
		t.Errorf("status over the budget: exit %d, stderr %q, route %v", s.Code, s.Stderr, route)
	}
}

// firstLines is s's first n lines.
func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	return strings.Join(lines[:min(n, len(lines))], "\n")
}

// T268: an untracked binary file (git's test: a NUL in its first 8000 bytes)
// has no content for the rules, as a tracked one's diff has no added lines;
// an untracked text file is judged by its content, as before.
func TestRiskJudgesAnUntrackedBinaryFileAsGitDoes(t *testing.T) {
	t.Parallel()
	repo := filepath.Join(temp(t), "proj")
	support.Write(t, repo, map[string]string{"openspec/config.yaml": "schema: spec-driven\n", "tracked.bin": "\x00old\n"})
	support.GitRepo(t, repo)
	support.Git(t, repo, "add", "-A")
	support.Git(t, repo, "commit", "-q", "-m", "base")
	support.Write(t, repo, map[string]string{"tracked.bin": "\x00drop table users\n", "out/blob.bin": "\x00drop table users\n", "out/run.log": "drop table users\n"})
	env := support.Env("XDG_STATE_HOME=" + filepath.Join(filepath.Dir(repo), "state"))
	r := support.Jhon(t, repo, env, "risk", "--base", "HEAD")
	want := "risk: high (base HEAD)\n" +
		"  medium  tracked.bin: other file\n" +
		"  medium  out/blob.bin: other file\n" +
		"  high    out/run.log: destructive data statement\n"
	if r.Code != 0 || r.Stdout != want {
		t.Errorf("risk: exit %d, stderr %q, stdout:\n%s\nwant:\n%s", r.Code, r.Stderr, r.Stdout, want)
	}
}
