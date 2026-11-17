// Read-only git runs take no optional lock (L320, T35): a `git status` that
// refreshes the index takes .git/index.lock, and one killed mid-way leaves
// it behind for every later git command. `jhon doctor` names such a stale
// lock and how to remove it, and never removes it itself.
package ldd

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// fakeGit is a bin directory whose `git` logs each run's GIT_OPTIONAL_LOCKS
// and arguments to the returned file, then runs the real git.
func fakeGit(t *testing.T) (bin, log string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := temp(t)
	bin, log = filepath.Join(dir, "bin"), filepath.Join(dir, "git.log")
	support.Write(t, bin, map[string]string{
		"git":    "#!/bin/sh\nprintf '%s\\t%s\\n' \"${GIT_OPTIONAL_LOCKS-unset}\" \"$*\" >> " + log + "\nexec " + real + " \"$@\"\n",
		"claude": "#!/bin/sh\nexit 1\n",
	})
	for _, f := range []string{"git", "claude"} {
		if err := os.Chmod(filepath.Join(bin, f), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return bin, log
}

// readSubcommands are the git subcommands that only read.
var readSubcommands = regexp.MustCompile(`^(status|diff|rev-parse|log|ls-files|show|cat-file|merge-base|branch|for-each-ref|config|worktree|remote|ls-remote|archive)$`)

// subcommand is the git subcommand of a logged argument line, past -C/-c.
func subcommand(args string) string {
	f := strings.Fields(args)
	for i := 0; i < len(f); i++ {
		switch f[i] {
		case "-C", "-c":
			i++
		default:
			return f[i]
		}
	}
	return ""
}

func TestReadOnlyGitTakesNoOptionalLocks(t *testing.T) {
	t.Parallel()
	root := support.GitRepo(t, tree(t, map[string]string{"package.json": "{}"}))
	bin, log := fakeGit(t)
	env := support.Env("HOME="+temp(t), "PATH="+bin+":"+os.Getenv("PATH"))
	support.Jhon(t, root, env, "status", "--json")
	support.Jhon(t, root, env, "doctor")
	seen := map[string]bool{}
	for _, l := range strings.Split(strings.TrimSpace(support.Read(t, log)), "\n") {
		locks, args, _ := strings.Cut(l, "\t")
		sub := subcommand(args)
		if !readSubcommands.MatchString(sub) {
			continue
		}
		seen[sub] = true
		equal(t, locks, "0", "git "+args)
	}
	for _, sub := range []string{"status", "rev-parse"} {
		if !seen[sub] {
			t.Errorf("no git %s run; log:\n%s", sub, support.Read(t, log))
		}
	}
}

func TestStatusLeavesTheIndexAlone(t *testing.T) {
	t.Parallel()
	root := tree(t, map[string]string{"package.json": "{}", "a.txt": "a\n"})
	support.GitInit(t, root)
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "init")
	// A file touched after the index was written makes a locking
	// `git status` refresh and rewrite the index.
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(filepath.Join(root, "a.txt"), later, later); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(root, ".git", "index")
	before, err := os.Stat(index)
	if err != nil {
		t.Fatal(err)
	}
	r := jhonGit(t, root, "status")
	equal(t, r.Code, 0, r.Stderr)
	after, err := os.Stat(index)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, after.ModTime(), before.ModTime(), "index mtime")
	if _, err := os.Stat(index + ".lock"); err == nil {
		t.Error("jhon status left .git/index.lock")
	}
}

// ── stale locks in `jhon doctor` ────────────────────────────────────────────

// lock writes a lock file of content dated age ago.
func lock(t *testing.T, path, content string, age time.Duration) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-age)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// lockWarnings are the doctor's project lines that name a lock.
func lockWarnings(t *testing.T, root string) [][2]string {
	t.Helper()
	var out [][2]string
	for _, l := range doctor(t, root) {
		if strings.Contains(l[1], ".lock") {
			out = append(out, l)
		}
	}
	return out
}

func TestDoctorStaleLock(t *testing.T) {
	t.Parallel()
	root := support.GitRepo(t, tree(t, map[string]string{"package.json": "{}"}))
	git := filepath.Join(root, ".git")
	lock(t, filepath.Join(git, "index.lock"), "", 5*time.Minute)
	lock(t, filepath.Join(git, "HEAD.lock"), "", time.Hour)
	got := lockWarnings(t, root)
	if len(got) != 2 {
		t.Fatalf("want two lock warnings, got %q", got)
	}
	for i, c := range []struct{ file, age string }{{"HEAD.lock", "1h0m"}, {"index.lock", "5m"}} {
		p := filepath.Join(git, c.file)
		equal(t, got[i][0], "warn")
		equal(t, got[i][1], "stale git lock "+p+": 0 bytes, "+c.age+" old, no git process running in "+root+
			"; close whatever runs git there, then: rm "+p)
	}
	for _, f := range []string{"index.lock", "HEAD.lock"} {
		if _, err := os.Stat(filepath.Join(git, f)); err != nil {
			t.Errorf("doctor removed %s: %v", f, err)
		}
	}
}

func TestDoctorStaleLockInAWorktree(t *testing.T) {
	t.Parallel()
	main := support.GitRepo(t, tree(t, map[string]string{"package.json": "{}"}))
	wt := filepath.Join(temp(t), "wt")
	support.Git(t, main, "worktree", "add", "-q", wt)
	own := strings.TrimSpace(support.Git(t, wt, "rev-parse", "--path-format=absolute", "--git-dir"))
	lock(t, filepath.Join(own, "index.lock"), "", 10*time.Minute)
	got := lockWarnings(t, wt)
	if len(got) != 1 {
		t.Fatalf("want one lock warning, got %q", got)
	}
	match(t, got[0][1], `^stale git lock `+regexp.QuoteMeta(filepath.Join(own, "index.lock"))+`: 0 bytes, 10m old`)
}

func TestDoctorNoWarningForALiveLock(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		content string
		age     time.Duration
	}{
		"fresh":     {"", 30 * time.Second},
		"non-empty": {"x", time.Hour},
	} {
		root := support.GitRepo(t, tree(t, map[string]string{"package.json": "{}"}))
		lock(t, filepath.Join(root, ".git", "index.lock"), c.content, c.age)
		if got := lockWarnings(t, root); len(got) != 0 {
			t.Errorf("%s: want no lock warning, got %q", name, got)
		}
	}
}

func TestDoctorNoWarningForALockHeldByGit(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat("/proc/self/cwd"); err != nil {
		t.Skip("no /proc: the process check is skipped there")
	}
	root := support.GitRepo(t, tree(t, map[string]string{"package.json": "{}"}))
	lock(t, filepath.Join(root, ".git", "index.lock"), "", time.Hour)
	// A process named git whose cwd is inside the repo holds it.
	bin := temp(t)
	support.Write(t, bin, map[string]string{"git": "#!/bin/sh\nsleep 60\n"})
	if err := os.Chmod(filepath.Join(bin, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	held := exec.Command(filepath.Join(bin, "git"))
	held.Dir = root
	if err := held.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Process.Kill(); _ = held.Wait() })
	if got := lockWarnings(t, root); len(got) != 0 {
		t.Errorf("want no lock warning, got %q", got)
	}
}
