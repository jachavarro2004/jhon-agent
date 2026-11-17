package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sigstore/sigstore-go/pkg/testing/ca"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// packed is the artifacts 0.1.0 and 0.2.0 of two commits of a scratch copy
// of this tree, with their .sha256, in one directory.
func packed(t *testing.T, e env) string {
	t.Helper()
	src := filepath.Join(temp(t), "jhon")
	if err := os.Rename(support.CopyRepo(t), src); err != nil {
		t.Fatal(err)
	}
	support.Git(t, src, "init", "-q")
	support.Git(t, src, "add", "-A")
	support.Git(t, src, "commit", "-q", "--no-verify", "-m", "first")
	out := temp(t)
	for i, v := range []string{"0.1.0", "0.2.0"} {
		if i > 0 {
			support.Write(t, src, map[string]string{"update-fixture.txt": v + "\n"})
			support.Git(t, src, "add", "-A")
			support.Git(t, src, "commit", "-q", "--no-verify", "-m", v)
		}
		wantCode(t, e.run(t, filepath.Join(src, "build", "jhon"), []string{"release", "pack", "--version", v, "--out", out}), 0)
	}
	return out
}

func TestUpdate(t *testing.T) {
	vs, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatal(err)
	}
	e := scratch(t, trustedRoot(t, vs))
	art := packed(t, e)
	read := func(n string) []byte { return []byte(support.Read(t, filepath.Join(art, n))) }
	assets := func(v string) map[string][]byte {
		return map[string][]byte{name(v): read(name(v)), name(v) + ".sha256": read(name(v) + ".sha256"),
			name(v) + ".sigstore.json": signBundle(t, vs, workflow, read(name(v)))}
	}
	rel := &releases{}
	var base string
	srv := httptest.NewServer(rel.handler(&base))
	defer srv.Close()
	base = srv.URL
	url := "JHON_RELEASES_URL=" + srv.URL + "/latest"

	jhon := cli(t, "0.1.0")
	wantCode(t, e.run(t, jhon, []string{"release", "install", filepath.Join(art, name("0.1.0"))}), 0)
	installed := filepath.Join(e.home, ".local", "bin", "jhon")
	version := func() string { return e.run(t, installed, []string{"version"}).Stdout }

	t.Run("up to date: one line, exit 0, nothing written", func(t *testing.T) {
		rel.set("v0.1.0", assets("0.1.0"))
		before := snapshot(t, e.home)
		r := e.run(t, jhon, []string{"update"}, url)
		wantCode(t, r, 0)
		equal(t, r.Stdout, "jhon 0.1.0 is up to date (latest release 0.1.0)\n")
		equal(t, r.Stderr, "")
		sameSnapshot(t, snapshot(t, e.home), before)
	})

	rel.set("v0.2.0", assets("0.2.0"))

	t.Run("--check reports the newer release and writes nothing", func(t *testing.T) {
		before := snapshot(t, e.home)
		r := e.run(t, jhon, []string{"update", "--check"}, url)
		wantCode(t, r, 0)
		equal(t, r.Stdout, "update available: jhon 0.1.0 -> 0.2.0 (released 2026-11-01)\nrun 'jhon update' to install it\n")
		sameSnapshot(t, snapshot(t, e.home), before)
	})

	t.Run("--check --json", func(t *testing.T) {
		r := e.run(t, jhon, []string{"update", "--check", "--json"}, url)
		wantCode(t, r, 0)
		equal(t, r.Stdout, `{"current":"0.1.0","latest":"0.2.0","status":"available"}`+"\n")
	})

	t.Run("a development build is never offered a release (T27)", func(t *testing.T) {
		r := e.run(t, cli(t, ""), []string{"update", "--check"}, url)
		wantCode(t, r, 0)
		equal(t, r.Stdout, "jhon: this is a development build (0.0.0-dev) installed by `jhon sync`; `jhon update` installs only published releases. Run `jhon sync` in the source checkout to update it.\n")
	})

	refused := func(t *testing.T, a map[string][]byte, want string, extra ...string) {
		t.Helper()
		rel.set("v0.2.0", a)
		defer rel.set("v0.2.0", assets("0.2.0"))
		before := snapshot(t, e.home)
		r := e.run(t, jhon, []string{"update"}, append([]string{url}, extra...)...)
		wantCode(t, r, 1)
		match(t, r.Stderr, "^jhon: update failed: "+want+"; nothing was changed\n$")
		sameSnapshot(t, snapshot(t, e.home), before)
		if !strings.HasPrefix(version(), "jhon 0.1.0 ") {
			t.Fatalf("jhon version = %q", version())
		}
	}

	t.Run("a checksum mismatch is refused and changes nothing", func(t *testing.T) {
		a := assets("0.2.0")
		a[name("0.2.0")+".sha256"] = []byte(strings.Repeat("0", 64) + "  " + name("0.2.0") + "\n")
		refused(t, a, regexp.QuoteMeta("checksum mismatch for "+name("0.2.0")))
	})

	t.Run("a signature from another identity is refused and changes nothing", func(t *testing.T) {
		a := assets("0.2.0")
		a[name("0.2.0")+".sigstore.json"] = signBundle(t, vs, "https://github.com/someone/fork/.github/workflows/release.yml@refs/tags/v0.2.0", a[name("0.2.0")])
		refused(t, a, regexp.QuoteMeta(name("0.2.0")+" is signed by https://github.com/someone/fork/.github/workflows/release.yml@refs/tags/v0.2.0 (issuer "+issuer+"), not by the release workflow"))
	})

	t.Run("a fork of the public repository, or its branch, is refused and changes nothing (T18)", func(t *testing.T) {
		for _, id := range []string{
			"https://github.com/someone/jhon-agent/.github/workflows/release.yml@refs/tags/v0.2.0",
			"https://github.com/jachavarro2004/jhon-agent/.github/workflows/release.yml@refs/heads/main",
		} {
			a := assets("0.2.0")
			a[name("0.2.0")+".sigstore.json"] = signBundle(t, vs, id, a[name("0.2.0")])
			refused(t, a, regexp.QuoteMeta(name("0.2.0")+" is signed by "+id+" (issuer "+issuer+"), not by the release workflow"))
		}
	})

	t.Run("a signed tarball of 0.1.0 published as release 0.2.0 is refused and changes nothing", func(t *testing.T) {
		old := read(name("0.1.0"))
		sum := sha256.Sum256(old)
		a := map[string][]byte{name("0.2.0"): old, name("0.2.0") + ".sha256": []byte(hex.EncodeToString(sum[:]) + "  " + name("0.2.0") + "\n"),
			name("0.2.0") + ".sigstore.json": signBundle(t, vs, workflow, old)}
		refused(t, a, regexp.QuoteMeta(name("0.2.0")+" holds jhon 0.1.0, not the release's 0.2.0"))
	})

	t.Run("a plain build ignores JHON_TEST_TRUSTED_ROOT: a test-signed release is refused offline", func(t *testing.T) {
		rel.set("v0.2.0", assets("0.2.0"))
		before := snapshot(t, e.home)
		offline := []string{url, "HTTPS_PROXY=http://127.0.0.1:1", "HTTP_PROXY=http://127.0.0.1:1", "https_proxy=http://127.0.0.1:1", "http_proxy=http://127.0.0.1:1", "NO_PROXY=", "no_proxy="}
		plain := filepath.Join(temp(t), "jhon")
		c := exec.Command("go", "build", "-o", plain, "-ldflags", "-X "+versionPkg+".Version=0.1.0", "./engine/cmd/jhon")
		c.Dir = support.Root()
		if b, err := c.CombinedOutput(); err != nil {
			t.Fatal(err, string(b))
		}
		r := e.run(t, plain, []string{"update"}, offline...)
		wantCode(t, r, 1)
		match(t, r.Stderr, "^jhon: update failed: cannot load Sigstore's trusted root: .*; nothing was changed\n$")
		sameSnapshot(t, snapshot(t, e.home), before)
	})

	t.Run("a release without its .sigstore.json is refused and changes nothing", func(t *testing.T) {
		a := assets("0.2.0")
		delete(a, name("0.2.0")+".sigstore.json")
		refused(t, a, regexp.QuoteMeta("release 0.2.0 has no "+name("0.2.0")+".sigstore.json"))
	})

	t.Run("a network error is refused and changes nothing", func(t *testing.T) {
		refused(t, assets("0.2.0"), `cannot reach http://127\.0\.0\.1:1/latest: .*connection refused`, "JHON_RELEASES_URL=http://127.0.0.1:1/latest")
	})

	t.Run("a newer signed release installs, and jhon version reports it", func(t *testing.T) {
		// The dashboard 0.1.0 started is restarted from 0.2.0 (T15.16): on a
		// port the test picked, its record in the scratch HOME.
		state := "XDG_STATE_HOME=" + filepath.Join(e.home, ".local", "state")
		port, from, stop := dashboardOf(t, installed, support.Env("HOME="+e.home, state, "XDG_CACHE_HOME="+filepath.Join(e.home, ".cache")))
		defer stop()
		r := e.run(t, jhon, []string{"update"}, url, state)
		wantCode(t, r, 0)
		lines := strings.Split(strings.TrimSuffix(r.Stdout, "\n"), "\n")
		equal(t, strings.Join(lines[:2], "\n"), "update available: jhon 0.1.0 -> 0.2.0 (released 2026-11-01)\n"+
			"  ok    sha256 and signature verified: signed by "+workflow)
		equal(t, lines[len(lines)-2], "updated jhon 0.1.0 -> 0.2.0")
		to := dashboardRelease(port)
		equal(t, lines[len(lines)-1], "dashboard restarted on 127.0.0.1:"+port+" ("+from[:8]+" → "+to[:min(8, len(to))]+")")
		if to == from || filepath.Base(filepath.Dir(filepath.Dir(installedBinary(t, installed)))) != to {
			t.Errorf("the dashboard runs %q, not the new current", to)
		}
		match(t, version(), `^jhon 0\.2\.0 `)
	})

	t.Run("--rollback re-activates the previous release", func(t *testing.T) {
		// A record in the test process's own XDG_STATE_HOME (the owner's,
		// outside a test) names a server that is not Jhon's: the scratch HOME's
		// update never reads it.
		foreign := httptest.NewServer(http.NotFoundHandler())
		defer foreign.Close()
		fport := strconv.Itoa(foreign.Listener.Addr().(*net.TCPAddr).Port)
		ambient := temp(t)
		support.Write(t, ambient, map[string]string{"jhon/dashboard/" + fport + ".json": `{"pid":1,"port":"` + fport + `","release":"x","binary":"/bin/true","args":[]}` + "\n"})
		t.Setenv("XDG_STATE_HOME", ambient)
		r := e.run(t, jhon, []string{"update", "--rollback"})
		wantCode(t, r, 0)
		if strings.Contains(r.Out(), "left running") {
			t.Fatalf("the update read the ambient dashboard records:\n%s", r.Out())
		}
		lines := strings.Split(strings.TrimSuffix(r.Stdout, "\n"), "\n")
		equal(t, lines[0], "rolling back jhon 0.2.0 -> 0.1.0")
		equal(t, lines[len(lines)-1], "rolled back to jhon 0.1.0")
		match(t, version(), `^jhon 0\.1\.0 `)
		// Both releases stay for the next update or roll-back.
		es, _ := os.ReadDir(filepath.Join(share(e.home), "releases"))
		equal(t, len(es), 2)
	})

	t.Run("--rollback with no older release kept says so and changes nothing", func(t *testing.T) {
		before := snapshot(t, e.home)
		r := e.run(t, jhon, []string{"update", "--rollback"})
		wantCode(t, r, 1)
		equal(t, r.Stderr, "jhon: no release older than jhon 0.1.0 is kept; nothing was changed\n")
		sameSnapshot(t, snapshot(t, e.home), before)
	})

	t.Run("--json: stdout is only the result, the install's lines go to stderr", func(t *testing.T) {
		r := e.run(t, jhon, []string{"update", "--json"}, url)
		wantCode(t, r, 0)
		match(t, r.Stdout, `^\{"current":"0\.1\.0","latest":"0\.2\.0","status":"updated"\}\n$`)
		match(t, r.Stderr, `(?m)^updated jhon 0\.1\.0 -> 0\.2\.0$`)
		match(t, version(), `^jhon 0\.2\.0 `)
	})

	t.Run("--json: a .sha256 naming another file fails as one object and changes nothing", func(t *testing.T) {
		r := e.run(t, jhon, []string{"update", "--rollback"})
		wantCode(t, r, 0)
		a := assets("0.2.0")
		sum, _, _ := strings.Cut(string(a[name("0.2.0")+".sha256"]), " ")
		a[name("0.2.0")+".sha256"] = []byte(sum + "  other.tar.gz\n")
		rel.set("v0.2.0", a)
		defer rel.set("v0.2.0", assets("0.2.0"))
		before := snapshot(t, e.home)
		r = e.run(t, jhon, []string{"update", "--json"}, url)
		wantCode(t, r, 1)
		match(t, r.Stdout, `^\{"current":"0\.1\.0","latest":"0\.2\.0","status":"failed","error":"[^"]+ is not the checksum of [^"]+"\}\n$`)
		sameSnapshot(t, snapshot(t, e.home), before)
	})

	t.Run("a usage error exits 2", func(t *testing.T) {
		r := e.run(t, jhon, []string{"update", "--rollback", "--check"})
		wantCode(t, r, 2)
		equal(t, r.Stderr, "jhon update: --rollback takes no other option\nusage: jhon update [--check] [--json] | jhon update --rollback\n")
	})
}

// dashboardOf starts `jhon dashboard` from jhon on a free port and waits for
// its release to answer; stop ends whatever dashboard the port's record
// names then (the restarted one), and the test's own.
func dashboardOf(t *testing.T, jhon string, env []string) (port, release string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port = strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	ln.Close()
	var state string
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "XDG_STATE_HOME="); ok {
			state = v
		}
	}
	cmd := exec.Command(jhon, "dashboard", "--port", port)
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go cmd.Wait()
	stop = func() {
		var r struct{ Pid int }
		if b, err := os.ReadFile(filepath.Join(state, "jhon", "dashboard", port+".json")); err == nil && json.Unmarshal(b, &r) == nil && r.Pid > 0 {
			syscall.Kill(r.Pid, syscall.SIGTERM)
			for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
				if syscall.Kill(r.Pid, 0) != nil {
					break
				}
			}
		}
		cmd.Process.Kill()
	}
	t.Cleanup(stop)
	for end := time.Now().Add(10 * time.Second); release == ""; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("the dashboard did not answer")
		}
		release = dashboardRelease(port)
	}
	return port, release, stop
}

// dashboardRelease is the release the dashboard on port says it runs, "" when none answers.
func dashboardRelease(port string) string {
	res, err := http.Get("http://127.0.0.1:" + port + "/api/version")
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	var b struct{ Release string }
	json.NewDecoder(res.Body).Decode(&b)
	return b.Release
}

func installedBinary(t *testing.T, link string) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
