// Tests for `jhon update` (T6, S8, S9): it reads the latest release from
// JHON_RELEASES_URL (an httptest server here, never the network), downloads
// this machine's tarball, its .sha256 and its .sigstore.json, verifies the
// checksum and the cosign keyless signature, and installs the release; any
// failure changes nothing. `--rollback` re-activates the previous release.
//
// The binary under test is built with the jhon_testroot tag, which checks
// signatures against a virtual Sigstore's trusted root; a release build has
// no such hook. The releases it installs are real artifacts, packed by
// `jhon release pack` from a scratch copy of this tree, in a scratch HOME.
package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

const versionPkg = "github.com/jachavarro2004/jhon-agent/engine/internal/version"

func name(v string) string { return "jhon-" + v + "-linux-" + runtime.GOARCH + ".tar.gz" }

func temp(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// cli builds this tree's jhon with the jhon_testroot tag, stamped ver when
// given, at <dir>/build/jhon.
func cli(t *testing.T, ver string) string {
	t.Helper()
	support.Bin(t) // records the engine's sources as the test's inputs
	out := filepath.Join(temp(t), "build", "jhon")
	args := []string{"build", "-tags", "jhon_testroot", "-o", out}
	if ver != "" {
		args = append(args, "-ldflags", "-X "+versionPkg+".Version="+ver)
	}
	c := exec.Command("go", append(args, "./engine/cmd/jhon")...)
	c.Dir = support.Root()
	if b, err := c.CombinedOutput(); err != nil {
		t.Fatal(err, string(b))
	}
	return out
}

// env is a scratch HOME whose `claude` always fails, the real Go caches
// offline (pack builds Go), and the virtual Sigstore's trusted root; its
// XDG_STATE_HOME is the HOME's own, so an update never reads the dashboard
// records of whoever runs the suite.
type env struct{ home, bin, root string }

func scratch(t *testing.T, trusted []byte) env {
	t.Helper()
	home := temp(t)
	support.Write(t, home, map[string]string{
		"stub-bin/claude":   "#!/bin/sh\nexit 1\n",
		".claude/CLAUDE.md": "@RTK.md\n\n<!-- jhon:start -->\n<!-- jhon:end -->\n",
	})
	if err := os.Chmod(filepath.Join(home, "stub-bin", "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(temp(t), "trusted_root.json")
	if err := os.WriteFile(root, trusted, 0o644); err != nil {
		t.Fatal(err)
	}
	return env{home, filepath.Join(home, "stub-bin"), root}
}

var (
	goOnce sync.Once
	goEnv  []string
)

func (e env) run(t *testing.T, cmd string, args []string, extra ...string) support.Result {
	t.Helper()
	goOnce.Do(func() {
		out, err := exec.Command("go", "env", "GOCACHE", "GOMODCACHE").Output()
		if v := strings.Split(strings.TrimSpace(string(out)), "\n"); err == nil && len(v) == 2 {
			goEnv = []string{"GOCACHE=" + v[0], "GOMODCACHE=" + v[1], "GOPROXY=off", "GOTOOLCHAIN=local"}
		}
	})
	overrides := append([]string{"HOME=" + e.home, "PATH=" + e.bin + ":" + os.Getenv("PATH"), "JHON_SYNC_GO_TEST=0",
		"JHON_TEST_TRUSTED_ROOT=" + e.root, "XDG_CACHE_HOME=" + filepath.Join(e.home, ".cache"),
		"XDG_STATE_HOME=" + filepath.Join(e.home, ".local", "state")}, goEnv...)
	return support.Run(t, support.Cmd{Args: append([]string{cmd}, args...), Env: support.Env(append(overrides, extra...)...)})
}

func share(home string) string { return filepath.Join(home, ".local", "share", "jhon") }

// snapshot is every path under the share directory and ~/.local/bin with
// its content's hash, or its link's target: what a refused update leaves
// byte-identical.
func snapshot(t *testing.T, home string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, dir := range []string{share(home), filepath.Join(home, ".local", "bin")} {
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			switch {
			case d.Type()&fs.ModeSymlink != 0:
				l, _ := os.Readlink(p)
				out[p] = "-> " + l
			case d.Type().IsRegular():
				b, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				h := sha256.Sum256(b)
				out[p] = hex.EncodeToString(h[:])
			default:
				out[p] = "dir"
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// releases is a fake GitHub: /latest is the latest release's JSON, naming
// the assets it holds, served under /dl/.
type releases struct {
	mu     sync.Mutex
	tag    string
	assets map[string][]byte
}

func (r *releases) set(tag string, assets map[string][]byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tag, r.assets = tag, assets
}

func (r *releases) handler(base *string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if req.URL.Path == "/latest" {
			var assets []map[string]string
			for n := range r.assets {
				assets = append(assets, map[string]string{"name": n, "browser_download_url": *base + "/dl/" + n})
			}
			json.NewEncoder(w).Encode(map[string]any{"tag_name": r.tag, "published_at": "2026-11-01T10:00:00Z", "body": "- faster\n", "assets": assets})
			return
		}
		b, ok := r.assets[strings.TrimPrefix(req.URL.Path, "/dl/")]
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Write(b)
	})
}

func wantCode(t *testing.T, r support.Result, code int) {
	t.Helper()
	if r.Code != code {
		t.Fatalf("exit %d, want %d\n%s", r.Code, code, r.Out())
	}
}

func equal[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got  %q\nwant %q", fmt.Sprint(got), fmt.Sprint(want))
	}
}

func match(t *testing.T, text, re string) {
	t.Helper()
	if !regexp.MustCompile(re).MatchString(text) {
		t.Fatalf("%q does not match %s", text, re)
	}
}

func sameSnapshot(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("the share directory changed: %d paths, was %d", len(got), len(want))
	}
	for p, h := range want {
		if got[p] != h {
			t.Fatalf("%s changed: %s, was %s", p, got[p], h)
		}
	}
}

// TestNoStableRelease (T11e, S11): while only pre-releases are published,
// GitHub's /releases/latest answers 404; `jhon update`, --check and --json
// say no stable release is published yet and exit 0, writing nothing. Any
// other status, and a 404 for a URL naming a tag, still fail with exit 1.
func TestNoStableRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/down/") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		http.NotFound(w, req)
	}))
	defer srv.Close()
	e := scratch(t, []byte("{}"))
	jhon := cli(t, "0.1.0-rc.1")
	latest := "JHON_RELEASES_URL=" + srv.URL + "/repos/o/r/releases/latest"
	for _, args := range [][]string{{"update"}, {"update", "--check"}} {
		r := e.run(t, jhon, args, latest)
		wantCode(t, r, 0)
		equal(t, r.Stdout, "no stable release is published yet; jhon 0.1.0-rc.1 is installed\n")
		equal(t, r.Stderr, "")
	}
	r := e.run(t, jhon, []string{"update", "--json"}, latest)
	wantCode(t, r, 0)
	equal(t, r.Stdout, `{"current":"0.1.0-rc.1","status":"no-release"}`+"\n")
	equal(t, r.Stderr, "")
	for _, c := range []struct{ url, want string }{
		{srv.URL + "/down/releases/latest", "answered 500 Internal Server Error; nothing was changed"},
		{srv.URL + "/repos/o/r/releases/tags/v0.2.0", "answered 404 Not Found; nothing was changed"},
	} {
		r := e.run(t, jhon, []string{"update", "--check"}, "JHON_RELEASES_URL="+c.url)
		wantCode(t, r, 1)
		match(t, r.Stderr, regexp.QuoteMeta(c.want))
	}
	if _, err := os.Stat(share(e.home)); !os.IsNotExist(err) {
		t.Fatalf("the share directory exists: %v", err)
	}
}

// TestDevBuild (T27, L243): a development build, installed by `jhon sync`
// from a source checkout, is never replaced by a published release (that
// would be a downgrade). `jhon update`, --check and --json install nothing,
// read no release and say so in one line; the install attempt exits 2,
// --check and --json exit 0.
func TestDevBuild(t *testing.T) {
	var asked sync.Mutex
	reads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		asked.Lock()
		reads++
		asked.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.1.0", "assets": []any{}})
	}))
	defer srv.Close()
	e := scratch(t, []byte("{}"))
	support.Write(t, share(e.home), map[string]string{"releases/0.0.0-dev+c16e873/bin/jhon": "dev\n"})
	if err := os.Symlink("releases/0.0.0-dev+c16e873", filepath.Join(share(e.home), "current")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(e.home, ".local", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(share(e.home), "current", "bin", "jhon"), filepath.Join(e.home, ".local", "bin", "jhon")); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, e.home)
	jhon := cli(t, "0.0.0-dev+c16e873")
	url := "JHON_RELEASES_URL=" + srv.URL + "/latest"
	const line = "jhon: this is a development build (0.0.0-dev+c16e873) installed by `jhon sync`; `jhon update` installs only published releases. Run `jhon sync` in the source checkout to update it.\n"

	r := e.run(t, jhon, []string{"update"}, url)
	wantCode(t, r, 2)
	equal(t, r.Stdout, "")
	equal(t, r.Stderr, line)

	r = e.run(t, jhon, []string{"update", "--check"}, url)
	wantCode(t, r, 0)
	equal(t, r.Stdout, line)
	equal(t, r.Stderr, "")

	r = e.run(t, jhon, []string{"update", "--json"}, url)
	wantCode(t, r, 0)
	equal(t, r.Stdout, `{"current":"0.0.0-dev+c16e873","status":"dev","dev":true,"update":false}`+"\n")
	equal(t, r.Stderr, "")

	sameSnapshot(t, snapshot(t, e.home), before)
	equal(t, reads, 0)
}
