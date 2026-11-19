// Package hooks tests the repository's own git hooks (tools/git-hooks) in
// scratch repositories, with a fake `go` first on PATH that logs each call:
// no real check runs and nothing is pushed beyond a local bare remote.
package hooks

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// fakeGo logs its arguments, one call per line, to FAKE_GO_LOG and exits
// with FAKE_GO_EXIT (0 when unset). go vet, `go tool staticcheck` and
// `go test` all run through it.
const fakeGo = `#!/bin/sh
printf '%s\n' "$*" >>"$FAKE_GO_LOG"
exit "${FAKE_GO_EXIT:-0}"
`

// scratch is a jhon-shaped repository (engine/cmd/jhon/main.go) with one
// tagged commit on main, a copy of the real pre-push hook as its only hook, two
// bare remotes (origin and mirror), and a bin directory holding the fake go.
type scratch struct {
	t                *testing.T
	repo, hooks, bin string
	log, home        string
}

func newScratch(t *testing.T) *scratch {
	t.Helper()
	base := t.TempDir()
	s := &scratch{t: t, repo: filepath.Join(base, "repo"), hooks: filepath.Join(base, "hooks"), bin: filepath.Join(base, "bin"), log: filepath.Join(base, "go.log"), home: filepath.Join(base, "home")}
	for _, d := range []string{s.repo, s.hooks, s.bin, s.home} {
		if err := os.MkdirAll(d, 0o777); err != nil {
			t.Fatal(err)
		}
	}
	hook := support.Read(t, support.Path("tools", "git-hooks", "pre-push"))
	if err := os.WriteFile(filepath.Join(s.hooks, "pre-push"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.bin, "go"), []byte(fakeGo), 0o755); err != nil {
		t.Fatal(err)
	}
	support.GitInit(t, s.repo)
	support.Write(t, s.repo, map[string]string{"engine/cmd/jhon/main.go": "package main\n\nfunc main() {}\n"})
	s.commit("init")
	// A release tag: the guards' differential needs one, and runs only when a
	// push changes what judges the guards since it, which no push here does.
	support.Git(t, s.repo, "tag", "v0.0.0")
	for _, remote := range []string{"origin", "mirror"} {
		bare := filepath.Join(base, remote+".git")
		support.Git(t, base, "init", "-q", "--bare", bare)
		support.Git(t, s.repo, "remote", "add", remote, bare)
	}
	support.Git(t, s.repo, "config", "core.hooksPath", s.hooks)
	return s
}

// commit commits the work tree on main with the hooks off (the pre-commit
// hook is not under test) and returns the new sha.
func (s *scratch) commit(msg string) string {
	s.t.Helper()
	support.Git(s.t, s.repo, "add", "-A")
	support.Git(s.t, s.repo, "-c", "core.hooksPath=/dev/null", "commit", "-q", "--allow-empty", "-m", msg)
	return strings.TrimSpace(support.Git(s.t, s.repo, "rev-parse", "HEAD"))
}

// push runs `git push <remote> main` with the fake go first on PATH and env.
func (s *scratch) push(remote string, env ...string) support.Result {
	s.t.Helper()
	return support.Run(s.t, support.Cmd{
		Args: []string{"git", "-c", "user.name=jhon-test", "-c", "user.email=jhon-test@example.invalid", "push", "-q", remote, "main"},
		Dir:  s.repo,
		Env:  support.Env(append([]string{"PATH=" + s.bin + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + s.home, "FAKE_GO_LOG=" + s.log, "FAKE_GO_EXIT="}, env...)...),
	})
}

// checks is how many times the hook has run go so far.
func (s *scratch) checks() int {
	b, err := os.ReadFile(s.log)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		s.t.Fatal(err)
	}
	return strings.Count(string(b), "\n")
}

// record is the hook's record of passed shas: in the git common dir, outside the work tree.
func (s *scratch) record() string {
	b, err := os.ReadFile(filepath.Join(s.repo, ".git", "jhon", "pre-push-passed"))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		s.t.Fatal(err)
	}
	return string(b)
}

const runs = "pre-push: main moves; running go vet, staticcheck and the conformance suite"

// T112 (L279): a sha that already passed the hook's checks on this machine
// is not checked again; anything else runs the checks as before.
func TestPrePushSkipsAShaThatAlreadyPassed(t *testing.T) {
	t.Run("the second push of the same sha runs no check and says so in one line", func(t *testing.T) {
		s := newScratch(t)
		sha := strings.TrimSpace(support.Git(t, s.repo, "rev-parse", "HEAD"))
		first := s.push("origin")
		if first.Code != 0 || !strings.Contains(first.Stderr, runs) {
			t.Fatalf("first push: code %d, stderr %q", first.Code, first.Stderr)
		}
		if n := s.checks(); n != 3 {
			t.Fatalf("first push ran go %d times, want 3 (vet, staticcheck, the suite)", n)
		}
		second := s.push("mirror")
		if second.Code != 0 {
			t.Fatalf("second push: code %d, stderr %q", second.Code, second.Stderr)
		}
		if second.Stderr != "pre-push: "+sha+" already passed; skipping\n" {
			t.Fatalf("second push stderr %q", second.Stderr)
		}
		if n := s.checks(); n != 3 {
			t.Fatalf("the second push of %s ran go %d more times, want 0", sha, n-3)
		}
		if st := support.Git(t, s.repo, "status", "--porcelain", "--ignored"); st != "" {
			t.Fatalf("the record is in the work tree: %q", st)
		}
	})

	t.Run("a new sha runs the checks", func(t *testing.T) {
		s := newScratch(t)
		if r := s.push("origin"); r.Code != 0 {
			t.Fatalf("first push: %d %q", r.Code, r.Stderr)
		}
		s.commit("second")
		r := s.push("origin")
		if r.Code != 0 || !strings.Contains(r.Stderr, runs) {
			t.Fatalf("push of a new sha: code %d, stderr %q", r.Code, r.Stderr)
		}
		if n := s.checks(); n != 6 {
			t.Fatalf("go ran %d times over two shas, want 6", n)
		}
	})

	t.Run("a failed run records nothing: the next push runs the checks again", func(t *testing.T) {
		s := newScratch(t)
		failed := s.push("origin", "FAKE_GO_EXIT=1")
		if failed.Code == 0 || !strings.Contains(failed.Stderr, "pre-push: go vet found problems; push refused") {
			t.Fatalf("failing push: code %d, stderr %q", failed.Code, failed.Stderr)
		}
		if rec := s.record(); rec != "" {
			t.Fatalf("a failed run recorded %q", rec)
		}
		before := s.checks()
		r := s.push("origin")
		if r.Code != 0 || !strings.Contains(r.Stderr, runs) || s.checks()-before != 3 {
			t.Fatalf("push after a failure: code %d, %d checks, stderr %q", r.Code, s.checks()-before, r.Stderr)
		}
	})

	t.Run("as before: a push of another branch runs nothing and records nothing", func(t *testing.T) {
		s := newScratch(t)
		support.Git(t, s.repo, "branch", "topic")
		r := support.Run(t, support.Cmd{
			Args: []string{"git", "push", "-q", "origin", "topic"},
			Dir:  s.repo,
			Env:  support.Env("PATH="+s.bin+string(os.PathListSeparator)+os.Getenv("PATH"), "HOME="+s.home, "FAKE_GO_LOG="+s.log),
		})
		if r.Code != 0 || r.Stderr != "" || s.checks() != 0 || s.record() != "" {
			t.Fatalf("push of topic: code %d, %d checks, record %q, stderr %q", r.Code, s.checks(), s.record(), r.Stderr)
		}
	})

	t.Run("a change to the hook's checks invalidates what passed", func(t *testing.T) {
		s := newScratch(t)
		if r := s.push("origin"); r.Code != 0 {
			t.Fatalf("first push: %d %q", r.Code, r.Stderr)
		}
		hook := filepath.Join(s.hooks, "pre-push")
		changed := regexp.MustCompile(`go test -p 4 `).ReplaceAllString(support.Read(t, hook), "go test -p 2 ")
		if err := os.WriteFile(hook, []byte(changed), 0o755); err != nil {
			t.Fatal(err)
		}
		r := s.push("mirror")
		if r.Code != 0 || !strings.Contains(r.Stderr, runs) || s.checks() != 6 {
			t.Fatalf("push after the checks changed: code %d, %d checks, stderr %q", r.Code, s.checks(), r.Stderr)
		}
	})
}
