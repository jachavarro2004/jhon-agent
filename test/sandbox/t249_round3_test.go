package sandbox

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// L39 (round-3 verify): a docker socket the engine keeps under the home and
// names through DOCKER_HOST (Docker Desktop's) is not reachable inside with
// sandbox.docker off, and is with it on.
func TestADockerDesktopSocketIsHiddenUnlessTheOwnerAcceptsIt(t *testing.T) {
	b := newBox(t)
	sock := filepath.Join(b.home, ".docker", "desktop", "docker.sock")
	conns := serveUnix(t, sock)
	b.env = append(b.env, "DOCKER_HOST=unix://"+sock)
	probe := `test -S "${DOCKER_HOST#unix://}" && echo shown || echo hidden`
	if r := b.sh(t, b.proj, "", probe); r.Code != 0 || r.Stdout != "hidden\n" {
		t.Errorf("by default: exit %d, stdout %q, stderr %q; want the socket hidden", r.Code, r.Stdout, r.Stderr)
	}
	if n := atomic.LoadInt32(conns); n != 0 {
		t.Errorf("by default a sandboxed command connected %d time(s)", n)
	}
	b.settings(t, "schema = 1\n\n[sandbox]\ndocker = true\n")
	if r := b.sh(t, b.proj, "", probe); r.Code != 0 || r.Stdout != "shown\n" {
		t.Errorf("with sandbox.docker = true: exit %d, stdout %q, stderr %q; want the socket shown", r.Code, r.Stdout, r.Stderr)
	}
}

// L39 (the verifier's cookie probe, A2's second break): a sandboxed command
// sets http.cookieFile to a file in the home and http.saveCookies in the
// repository's configuration; the next `git fetch origin` would write that
// file outside the sandbox. A repository key that is not on the allowlist
// keeps the line in the sandbox, where the home file stays the owner's.
func TestARepositoryCookieFileKeepsTheFetchInTheSandbox(t *testing.T) {
	b := newBox(t)
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		// An empty dumb-HTTP answer: git ends normally and libcurl saves its
		// cookie jar, as it would for any real server.
		http.SetCookie(w, &http.Cookie{Name: "planted", Value: "1", Path: "/"})
		w.Header().Set("Content-Type", "text/plain")
	}))
	t.Cleanup(srv.Close)
	support.Git(t, b.proj, "remote", "add", "origin", srv.URL+"/repo.git")
	target := filepath.Join(b.home, "target")
	if err := os.WriteFile(target, []byte("owner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := b.sh(t, b.proj, "", `git config http.cookieFile "$HOME/target" && git config http.saveCookies true && echo set`); r.Code != 0 || r.Stdout != "set\n" {
		t.Fatalf("the plant: exit %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}
	r := b.unsandboxed(t, b.env, "git fetch origin")
	if got := support.Read(t, target); got != "owner\n" {
		t.Errorf("an outside fetch wrote the home file the repository named: %q (exit %d, stderr %q)", got, r.Code, r.Stderr)
	}
	if atomic.LoadInt32(&hits) == 0 {
		t.Error("the fetch never reached the server")
	}
}
