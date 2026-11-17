package audit

// `jhon audit <session> --view` and `--json` are kept per session under the
// user cache dir (S14), keyed by the session's transcripts' sizes and mtimes
// and the binary: a hit prints what the audit printed, byte for byte, with
// its exit code; any change to a transcript audits again.

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// TestMain keeps every audit this package runs out of the owner's cache.
func TestMain(m *testing.M) {
	cache, err := os.MkdirTemp("", "jhon-audit-cache-")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CACHE_HOME", cache)
	code := m.Run()
	os.RemoveAll(cache)
	os.Exit(code)
}

// copySession copies a fixture session, its transcript and its directory, into a new directory.
func copySession(t *testing.T, dir string) string {
	t.Helper()
	to := t.TempDir()
	from := support.Fixture("audit", dir)
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	main, _ := filepath.Glob(filepath.Join(to, "*.jsonl"))
	if len(main) != 1 {
		t.Fatalf("no main transcript in %s", to)
	}
	return main[0]
}

// cacheFiles are the audits kept under cache.
func cacheFiles(t *testing.T, cache string) []string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(cache, "jhon", "audit", "*"))
	return files
}

func TestAuditViewAndJSONAreCachedPerSession(t *testing.T) {
	t.Parallel()
	cache := t.TempDir()
	main := copySession(t, "apply-worktree-bypass")
	sub, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(main, ".jsonl"), "subagents", "*.jsonl"))
	if len(sub) != 1 {
		t.Fatalf("subagents: %v", sub)
	}
	env := "XDG_CACHE_HOME=" + cache
	for _, mode := range []string{"--view", "--json"} {
		first := audit(t, []string{main, mode}, env)
		equal(t, first.Code, 1)
		again := audit(t, []string{main, mode}, env)
		equal(t, again.Stdout, first.Stdout)
		equal(t, again.Stderr, first.Stderr)
		equal(t, again.Code, first.Code)
	}
	files := cacheFiles(t, cache)
	equal(t, len(files), 2)

	// A hit is what the cache holds: marked, the next audit prints the mark.
	view := audit(t, []string{main, "--view"}, env).Stdout
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(strings.Replace(string(b), `"status":"findings"`, `"status":"cached"`, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hit := audit(t, []string{main, "--view"}, env)
	equal(t, hit.Stdout, strings.Replace(view, `"status":"findings"`, `"status":"cached"`, 1))
	equal(t, hit.Code, 1)

	// A transcript touched, the main one or a sub-agent's, is audited again.
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(main, later, later); err != nil {
		t.Fatal(err)
	}
	// The view states the transcript's new mtime; the rest is the same audit.
	fresh := audit(t, []string{main, "--view"}, env).Stdout
	equal(t, strings.Contains(fresh, `"status":"findings"`), true)
	equal(t, strings.Contains(fresh, `"modified":"`+later.UTC().Format("2006-01-02T15:04:05.000Z")+`"`), true)
	for _, f := range cacheFiles(t, cache) {
		b, _ := os.ReadFile(f)
		_ = os.WriteFile(f, []byte(strings.Replace(string(b), `"status":"findings"`, `"status":"cached"`, 1)), 0o644)
	}
	equal(t, strings.Contains(audit(t, []string{main, "--view"}, env).Stdout, `"status":"cached"`), true)
	f, err := os.OpenFile(sub[0], os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	equal(t, audit(t, []string{main, "--view"}, env).Stdout, fresh)
}

// The binary is part of the key: an audit another build kept, as one from
// before the audit stated the owner's dialogue (T66a), is not served; the
// build that runs audits again and prints its own.
func TestAuditCacheMissesForAnotherBuild(t *testing.T) {
	t.Parallel()
	cache := t.TempDir()
	main := copySession(t, "apply-worktree-bypass")
	env := "XDG_CACHE_HOME=" + cache
	first := audit(t, []string{main, "--json"}, env)
	equal(t, strings.Contains(first.Stdout, `"dialogue": {`), true)
	files := cacheFiles(t, cache)
	equal(t, len(files), 1)
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files[0], []byte(strings.Replace(string(b), `"status": "findings"`, `"status": "cached"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	equal(t, strings.Contains(audit(t, []string{main, "--json"}, env).Stdout, `"status": "cached"`), true, "the same build is served what it kept")
	// The same binary under another path is another build to the key; a
	// link, or a copy where the two directories are on two filesystems.
	other := filepath.Join(t.TempDir(), "jhon")
	if os.Link(support.Bin(t), other) != nil {
		bin, err := os.ReadFile(support.Bin(t))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(other, bin, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	r := support.Run(t, support.Cmd{Args: []string{other, "audit", main, "--json"}, Env: jhonEnv(env)})
	equal(t, r.Stdout, first.Stdout, "another build audits again")
	equal(t, r.Code, first.Code)
}

// The human output and --html are not cached.
func TestAuditHumanOutputIsNotCached(t *testing.T) {
	t.Parallel()
	cache := t.TempDir()
	main := copySession(t, "apply-worktree-bypass")
	audit(t, []string{main}, "XDG_CACHE_HOME="+cache)
	equal(t, len(cacheFiles(t, cache)), 0)
}

// gitIn runs git in dir, with no GIT_ variable of this process, and returns its trimmed output.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)...)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			c.Env = append(c.Env, kv)
		}
	}
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// sessionIn is sessionWith with the session's project at repo.
func sessionIn(t *testing.T, repo string, steps ...step) string {
	t.Helper()
	main := sessionWith(t, steps...)
	err := filepath.WalkDir(filepath.Dir(main), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(p, []byte(strings.ReplaceAll(string(b), root, repo)), 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return main
}

// sameAsUncached is whether a cached audit of main as each mode prints and
// exits as an audit with an empty cache does, and the human output exits alike.
func sameAsUncached(t *testing.T, main, cache, what string) {
	t.Helper()
	for _, mode := range []string{"--json", "--view"} {
		got := audit(t, []string{main, mode}, "XDG_CACHE_HOME="+cache)
		want := audit(t, []string{main, mode}, "XDG_CACHE_HOME="+t.TempDir())
		equal(t, got.Stdout, want.Stdout, what, mode)
		equal(t, got.Code, want.Code, what, mode)
	}
	equal(t, audit(t, []string{main}, "XDG_CACHE_HOME="+cache).Code, audit(t, []string{main, "--json"}, "XDG_CACHE_HOME="+cache).Code, what, "human")
}

// What an audit reads besides the transcripts, the change's records and its
// repository's commits, is part of the cache's key (S14).
func TestAuditCacheMissesWhenTheChangeRecordOrGitMoves(t *testing.T) {
	t.Parallel()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "init", "-q")
	support.Write(t, repo, map[string]string{"a.go": "package a\n"})
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-qm", "base")
	base, baseTree := gitIn(t, repo, "rev-parse", "HEAD"), gitIn(t, repo, "rev-parse", "HEAD^{tree}")
	dir := filepath.Join(repo, "openspec", "changes", x)
	record := func(tree string, clean bool, workingTree string) {
		t.Helper()
		support.Write(t, dir, map[string]string{"execution-record.json": `{"schema":"jhon.execution-record/v2","timestamp":"2026-09-30T09:00:00.000Z","complete":true,"candidate_commit":"` + base +
			`","candidate_tree":"` + tree + `","working_tree_clean":` + map[bool]string{true: "true", false: "false"}[clean] + `,"working_tree":"` + workingTree +
			`","dirty":false,"scope":"full","executions":[{"project":".","command":"go test ./...","cwd":".","exit_code":0,"status":"pass","duration_ms":1000}]}`})
	}
	pushOf := func(sha string) call {
		return call{name: "Bash", input: map[string]any{"command": "git push origin main"}, result: "To github.com:o/r.git\n   0000000.." + sha[:7] + "  main -> main"}
	}
	// first audits main into cache, its inputs settled an hour ago, so it is kept.
	first := func(main, cache, what string) {
		t.Helper()
		ago := time.Now().Add(-time.Hour)
		err := filepath.WalkDir(repo, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return os.Chtimes(p, ago, ago)
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range []string{"--json", "--view"} {
			equal(t, audit(t, []string{main, mode}, "XDG_CACHE_HOME="+cache).Code, 1, what, mode)
		}
		equal(t, len(cacheFiles(t, cache)), 2, what, "kept")
	}

	// The change's execution record is created after the first audit.
	cache := t.TempDir()
	main := sessionIn(t, repo, userStep("go"), delegation{id: "d01", typ: "general", change: x, calls: []call{pushOf(base)}})
	first(main, cache, "no record")
	record(baseTree, true, baseTree)
	sameAsUncached(t, main, cache, "the record created")
	equal(t, audit(t, []string{main, "--json"}, "XDG_CACHE_HOME="+cache).Code, 0, "the record created")

	// The slice is tested uncommitted on base, pushed as the commit that
	// commits it, and that commit is made after the first audit.
	support.Write(t, repo, map[string]string{"b.go": "package a\n\nvar X = 1\n"})
	gitIn(t, repo, "add", "-A")
	tested := gitIn(t, repo, "write-tree")
	next := gitIn(t, repo, "commit-tree", tested, "-p", base, "-m", "slice 1")
	record(baseTree, false, tested)
	cache = t.TempDir()
	main = sessionIn(t, repo, userStep("go"), delegation{id: "d01", typ: "general", change: x, calls: []call{pushOf(next)}})
	first(main, cache, "not committed")
	gitIn(t, repo, "update-ref", "refs/heads/main", next)
	sameAsUncached(t, main, cache, "the next commit made")
	equal(t, audit(t, []string{main, "--json"}, "XDG_CACHE_HOME="+cache).Code, 0, "the next commit made")
}
