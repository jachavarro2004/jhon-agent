package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// lastEnv is the value env gives k last (exec keeps the last of two), and
// whether it gives one.
func lastEnv(env []string, k string) (string, bool) {
	v, ok := "", false
	for _, kv := range env {
		if rest, found := strings.CutPrefix(kv, k+"="); found {
			v, ok = rest, true
		}
	}
	return v, ok
}

// T250 A8 (WU5): sync and pack build with Go caches no sandboxed command
// can write, under Jhon's state folder, not the caller's (GOCACHE in the
// cache, GOMODCACHE in ~/go/pkg, both writable inside the sandbox, where a
// command could plant a compiled package or a module's source the next
// sync links into the jhon binary): its own module cache, build cache and
// work folder, the local toolchain only, go.mod read-only, and the modules
// taken from the caller's download cache (checked against go.sum) before
// the network.
func TestGoBuildsWithCachesUnderTheStateFolder(t *testing.T) {
	// A suite that syncs names its shared cache, and the release's own tests
	// run under it (jhon sync, pack).
	t.Setenv("JHON_GO_CACHE", "")
	os.Unsetenv("JHON_GO_CACHE")
	state := t.TempDir()
	user := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("XDG_CONFIG_HOME", user)
	t.Setenv("GOCACHE", "/home/u/.cache/go-build")
	t.Setenv("GOMODCACHE", "/home/u/go/pkg/mod")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOTOOLCHAIN", "auto")
	t.Setenv("GOPROXY", "https://proxy.example,direct")
	os.Unsetenv("GOENV")
	old := MayChange
	MayChange = func(string) bool { return false }
	t.Cleanup(func() { MayChange = old })
	env, done, err := goEnv()
	if err != nil {
		t.Fatal(err)
	}
	goFolder := filepath.Join(state, "jhon", "go")
	for k, want := range map[string]string{
		"GOMODCACHE": filepath.Join(goFolder, "mod"), "GOCACHE": filepath.Join(goFolder, "build"),
		"GOTOOLCHAIN": "local", "GOFLAGS": "-mod=readonly -modcacherw",
		"GOPROXY": "file:///home/u/go/pkg/mod/cache/download,https://proxy.example,direct",
		"GOENV":   filepath.Join(user, "go", "env"),
	} {
		if got, _ := lastEnv(env, k); got != want {
			t.Errorf("%s=%s, want %s", k, got, want)
		}
	}
	// go's work folder is one of its own outside the home, under /var/tmp
	// (no sandbox binds it unless named), mode 0700, kept across runs so go
	// test's cache holds (round 3, A2), and the same on the next run.
	work, _ := lastEnv(env, "GOTMPDIR")
	t.Cleanup(func() { os.RemoveAll(work) })
	if work != goWorkPath(goFolder) || !strings.HasPrefix(work, "/var/tmp/jhon-go-") || !IsDir(work) {
		t.Errorf("GOTMPDIR=%s, want %s", work, goWorkPath(goFolder))
	}
	if fi, err := os.Lstat(work); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("%s: %v %v, want mode 0700", work, fi, err)
	}
	if tmp, _ := lastEnv(env, "TMPDIR"); tmp != work {
		t.Errorf("TMPDIR=%s, want go's work folder %s, where the tests' t.TempDir is", tmp, work)
	}
	if v, _ := lastEnv(env, "GOWORK"); v != "off" {
		t.Errorf("GOWORK=%s, want off: no go.work a command planted joins the build", v)
	}
	done()
	if again, done2, err := goEnv(); err != nil {
		t.Fatal(err)
	} else if w, _ := lastEnv(again, "GOTMPDIR"); w != work {
		t.Errorf("the next run's GOTMPDIR=%s, want the same %s", w, work)
	} else {
		done2()
	}
	// One open to others, or a link, is refused.
	if err := os.Chmod(work, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, _, err := goEnv(); err == nil || !strings.Contains(err.Error(), "go's work folder, which is not a folder of this user's open to no one else") {
		t.Errorf("a work folder open to others: %v", err)
	}
	if err := os.Chmod(work, 0o700); err != nil {
		t.Fatal(err)
	}
	// JHON_GO_CACHE names the caches' folder instead, checked the same way.
	shared := t.TempDir()
	t.Setenv("JHON_GO_CACHE", shared)
	var asked []string
	MayChange = func(p string) bool { asked = append(asked, p); return false }
	env, doneShared, err := goEnv()
	if err != nil {
		t.Fatal(err)
	}
	doneShared()
	if w, _ := lastEnv(env, "GOTMPDIR"); w != filepath.Join(shared, "work") {
		t.Errorf("with JHON_GO_CACHE, GOTMPDIR=%s, want %s", w, filepath.Join(shared, "work"))
	}
	if got, _ := lastEnv(env, "GOCACHE"); got != filepath.Join(shared, "build") || !slices.Contains(asked, filepath.Join(shared, "mod")) {
		t.Errorf("JHON_GO_CACHE: GOCACHE=%s, checked %v", got, asked)
	}
	MayChange = func(p string) bool { return strings.HasPrefix(p, shared) }
	if _, done, err := goEnv(); err == nil || !strings.HasPrefix(err.Error(), "jhon builds with the Go caches in "+shared+",") {
		t.Errorf("a JHON_GO_CACHE a sandboxed command may change: %v", err)
		if err == nil {
			done()
		}
	}
	MayChange = func(string) bool { return false }
	os.Unsetenv("JHON_GO_CACHE")
	// The caller's download cache is found where go finds it with no
	// GOMODCACHE: GOPATH's first folder, else ~/go; the proxy go would use
	// with none set is its default.
	os.Unsetenv("GOMODCACHE")
	os.Unsetenv("GOPROXY")
	t.Setenv("GOPATH", "/data/gopath:/data/other")
	env, done2, _ := goEnv()
	defer done2()
	if got, _ := lastEnv(env, "GOPROXY"); got != "file:///data/gopath/pkg/mod/cache/download,https://proxy.golang.org,direct" {
		t.Errorf("GOPROXY=%s", got)
	}
	// The owner's go env file names the proxy when the environment does not.
	if err := os.MkdirAll(filepath.Join(user, "go"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(user, "go", "env"), []byte("GOPRIVATE=x\nGOPROXY=https://corp.example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Unsetenv("GOPATH")
	env, done3, _ := goEnv()
	defer done3()
	if got, _ := lastEnv(env, "GOPROXY"); got != "file://"+filepath.Join(os.Getenv("HOME"), "go", "pkg", "mod", "cache", "download")+",https://corp.example" {
		t.Errorf("GOPROXY from the go env file = %s", got)
	}
}

// T250 A8 (WU5): a build cache or a go env file a sandboxed command could
// change refuses the build, before go runs, with what it is and the fix.
func TestGoRefusesCachesOrAGoEnvFileASandboxedCommandMayChange(t *testing.T) {
	// A suite that syncs names its shared cache, and the release's own tests
	// run under it (jhon sync, pack).
	t.Setenv("JHON_GO_CACHE", "")
	os.Unsetenv("JHON_GO_CACHE")
	state := t.TempDir()
	user := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("XDG_CONFIG_HOME", user)
	os.Unsetenv("GOENV")
	old := MayChange
	t.Cleanup(func() { MayChange = old })
	file := filepath.Join(user, "go", "env")
	for _, c := range []struct {
		changeable, want string
	}{
		{filepath.Join(state, "jhon", "go", "build"), "jhon builds with the Go caches in " + filepath.Join(state, "jhon", "go") + ", and a sandboxed command may change " + filepath.Join(state, "jhon", "go", "build") + ", where it could plant code the build would take into jhon; fix: keep Jhon's state folder outside the project, /tmp, $TMPDIR, the caches and sandbox.allow_write"},
		{"/var/tmp/jhon-go-", "jhon builds in a work folder of go's under /var/tmp, which a sandboxed command may write, where it could change a package go compiled before the build cache takes it; fix: run jhon sync with $TMPDIR outside /var/tmp, or take /var/tmp out of sandbox.allow_write"},
		{file, "go reads its settings from " + file + ", which a sandboxed command may change, where it could set what the build of jhon takes (GOFLAGS, GOPROXY, GOWORK); fix: keep that file outside the project, /tmp, $TMPDIR, the caches and sandbox.allow_write, or point GOENV at one that is"},
	} {
		var asked []string
		MayChange = func(p string) bool {
			asked = append(asked, p)
			return p == c.changeable || c.changeable == "/var/tmp/jhon-go-" && filepath.Dir(p) == "/var/tmp" && strings.HasPrefix(filepath.Base(p), "jhon-go-")
		}
		t.Cleanup(func() { os.RemoveAll(goWorkPath(filepath.Join(state, "jhon", "go"))) })
		_, done, err := goEnv()
		if err == nil || err.Error() != c.want {
			t.Errorf("%s changeable: %v, want %q", c.changeable, err, c.want)
		}
		if err == nil {
			done()
		}
		if !slices.ContainsFunc(asked, func(p string) bool { return strings.HasPrefix(p, c.changeable) }) {
			t.Errorf("%s was never checked: %v", c.changeable, asked)
		}
		out, ok := goCmd(t.TempDir(), "version")
		if ok || out != c.want {
			t.Errorf("goCmd with %s changeable: ok %v, %q; want refused with %q", c.changeable, ok, out, c.want)
		}
	}
	// GOENV=off reads no file: nothing to check there.
	t.Setenv("GOENV", "off")
	MayChange = func(p string) bool { return p == "off" || strings.HasSuffix(p, "/env") }
	if _, done, err := goEnv(); err != nil {
		t.Errorf("GOENV=off: %v", err)
	} else {
		done()
	}
}

// T250 round 3 (the verify's A1): sync tests the commit's own tree, which
// git checks out into go's work folder, never the live checkout, where a
// sandboxed command can put a go.work, a _test.go .git/info/exclude hides,
// or a change to a tracked file git is told to skip; and only the files
// that changed since the last sync are written again, so go test's cache
// holds for the rest.
func TestSyncTestsTheCommitsOwnTree(t *testing.T) {
	t.Setenv("JHON_GO_CACHE", t.TempDir())
	t.Cleanup(func() { os.RemoveAll(goWorkPath(goState())) })
	src := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", src, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(rel, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(src, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write("go.mod", "module x\n\ngo 1.21\n")
	write("engine/a/a.go", "package a\n")
	write("engine/b/b.go", "package b\n")
	git("add", "-A")
	git("commit", "-q", "-m", "one")
	commit := git("rev-parse", "HEAD")
	// The plants.
	write("go.work", "go 1.21\nuse ./elsewhere\n")
	write("engine/a/evil_test.go", "package a\nfunc init() { panic(1) }\n")
	write(".git/info/exclude", "go.work\nevil_test.go\n")
	write("engine/b/b.go", "package b\nfunc init() { panic(2) }\n")
	git("update-index", "--skip-worktree", "engine/b/b.go")
	if st := git("status", "--porcelain"); st != "" {
		t.Fatalf("the plants are not hidden from git status: %q", st)
	}
	tree, out, ok := goTestTree(src, commit)
	if !ok {
		t.Fatal(out)
	}
	for _, rel := range []string{"go.work", "engine/a/evil_test.go"} {
		if _, err := os.Lstat(filepath.Join(tree, rel)); err == nil {
			t.Errorf("%s reached the tree the tests run in", rel)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(tree, "engine/b/b.go")); string(b) != "package b\n" {
		t.Errorf("engine/b/b.go in the tree holds %q, want the commit's", b)
	}
	// A second sync of a new commit rewrites only what changed.
	a := filepath.Join(tree, "engine/a/a.go")
	before, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	git("update-index", "--no-skip-worktree", "engine/b/b.go")
	write("engine/b/b.go", "package b\n\nconst B = 1\n")
	git("commit", "-q", "-am", "two")
	if _, out, ok := goTestTree(src, git("rev-parse", "HEAD")); !ok {
		t.Fatal(out)
	}
	if after, err := os.Stat(a); err != nil || !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("an unchanged file was written again: %v %v", before.ModTime(), after)
	}
	if b, _ := os.ReadFile(filepath.Join(tree, "engine/b/b.go")); string(b) != "package b\n\nconst B = 1\n" {
		t.Errorf("engine/b/b.go in the tree holds %q, want the new commit's", b)
	}
}
