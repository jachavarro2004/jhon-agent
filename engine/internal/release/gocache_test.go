package release

import (
	"os"
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
	// go's work folder is a fresh one outside the home, under /var/tmp
	// (no sandbox binds it unless named), and gone after the run.
	work, _ := lastEnv(env, "GOTMPDIR")
	if !strings.HasPrefix(work, "/var/tmp/jhon-go-") || !IsDir(work) {
		t.Errorf("GOTMPDIR=%s, want a fresh folder /var/tmp/jhon-go-*", work)
	}
	if tmp, _ := lastEnv(env, "TMPDIR"); tmp != work {
		t.Errorf("TMPDIR=%s, want go's work folder %s, where the tests' t.TempDir is", tmp, work)
	}
	done()
	if IsDir(work) {
		t.Errorf("%s is left behind", work)
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
		before, _ := filepath.Glob("/var/tmp/jhon-go-*")
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
		if left, _ := filepath.Glob("/var/tmp/jhon-go-*"); c.changeable == "/var/tmp/jhon-go-" && len(left) > len(before) {
			t.Errorf("a refused work folder is left behind: %v", left)
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
