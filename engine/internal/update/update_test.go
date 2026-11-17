package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func serve(t *testing.T, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	t.Cleanup(srv.Close)
	return &Client{URL: srv.URL, HTTP: srv.Client(), Arch: "arm64", Current: "0.1.0"}
}

func TestLatest(t *testing.T) {
	t.Run("this machine's tarball, its checksum and its signature; the date and the notes", func(t *testing.T) {
		c := serve(t, `{"tag_name":"v0.2.0","published_at":"2026-11-01T10:00:00Z","body":"Notes\n- faster\n- smaller\n","assets":[
			{"name":"jhon-0.2.0-linux-amd64.tar.gz","browser_download_url":"u/a"},
			{"name":"jhon-0.2.0-linux-arm64.tar.gz","browser_download_url":"u/t"},
			{"name":"jhon-0.2.0-linux-arm64.tar.gz.sha256","browser_download_url":"u/s"},
			{"name":"jhon-0.2.0-linux-arm64.tar.gz.sigstore.json","browser_download_url":"u/b"}]}`)
		l, err := c.Latest(context.Background())
		want := Latest{Version: "0.2.0", Date: "2026-11-01", Notes: []string{"faster", "smaller"},
			Tarball: Asset{"jhon-0.2.0-linux-arm64.tar.gz", "u/t"}, Sum: Asset{"jhon-0.2.0-linux-arm64.tar.gz.sha256", "u/s"},
			Bundle: Asset{"jhon-0.2.0-linux-arm64.tar.gz.sigstore.json", "u/b"}}
		if err != nil || !reflect.DeepEqual(l, want) {
			t.Fatalf("Latest = %+v, %v", l, err)
		}
	})
	for _, c := range []struct{ name, body, want string }{
		{"no asset for this machine", `{"tag_name":"v0.2.0","assets":[{"name":"jhon-0.2.0-linux-amd64.tar.gz","browser_download_url":"u"}]}`, "release 0.2.0 has no jhon-0.2.0-linux-arm64.tar.gz"},
		{"a tag that is no version", `{"tag_name":"nightly"}`, `the latest release's tag "nightly" is not a version`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := serve(t, c.body).Latest(context.Background()); err == nil || err.Error() != c.want {
				t.Fatalf("Latest error = %v, want %s", err, c.want)
			}
		})
	}
}

func TestLatestPrerelease(t *testing.T) {
	const rc = `{"tag_name":"v0.1.1-rc.1","prerelease":true,"assets":[
		{"name":"jhon-0.1.1-rc.1-linux-arm64.tar.gz","browser_download_url":"https://h/t"},
		{"name":"jhon-0.1.1-rc.1-linux-arm64.tar.gz.sha256","browser_download_url":"https://h/s"},
		{"name":"jhon-0.1.1-rc.1-linux-arm64.tar.gz.sigstore.json","browser_download_url":"https://h/b"}]}`
	t.Run("the default URL never offers a pre-release", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(rc)) }))
		defer srv.Close()
		c := &Client{URL: srv.URL, HTTP: srv.Client(), Arch: "arm64", Current: "0.1.0", HTTPSOnly: true}
		want := "the latest release 0.1.1-rc.1 is marked pre-release; only JHON_RELEASES_URL offers one"
		if _, err := c.Latest(context.Background()); err == nil || err.Error() != want {
			t.Fatalf("Latest error = %v, want %s", err, want)
		}
	})
	t.Run("JHON_RELEASES_URL offers a pre-release", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(rc)) }))
		defer srv.Close()
		t.Setenv("JHON_RELEASES_URL", srv.URL)
		c := New()
		c.Arch = "arm64"
		if l, err := c.Latest(context.Background()); err != nil || l.Version != "0.1.1-rc.1" {
			t.Fatalf("Latest = %+v, %v", l, err)
		}
	})
	t.Run("a release not marked pre-release is offered by the default URL", func(t *testing.T) {
		t.Setenv("JHON_RELEASES_URL", "")
		os.Unsetenv("JHON_RELEASES_URL")
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(strings.Replace(rc, `"prerelease":true`, `"prerelease":false`, 1)))
		}))
		defer srv.Close()
		c := &Client{URL: srv.URL, HTTP: srv.Client(), Arch: "arm64", Current: "0.1.0", HTTPSOnly: true}
		if l, err := c.Latest(context.Background()); err != nil || l.Version != "0.1.1-rc.1" {
			t.Fatalf("Latest = %+v, %v", l, err)
		}
	})
}

// TestLatestNoStableRelease (T11e, S11): GitHub's /releases/latest answers
// 404 while only pre-releases are published; that is ErrNoRelease, not a
// failure. A 404 from any other URL (a JHON_RELEASES_URL naming a missing
// tag) and any other status stay errors.
func TestLatestNoStableRelease(t *testing.T) {
	for _, c := range []struct {
		path   string
		status int
		none   bool
		want   string
	}{
		{"/repos/o/r/releases/latest", http.StatusNotFound, true, "no stable release is published yet"},
		{"/repos/o/r/releases/tags/v0.2.0", http.StatusNotFound, false, "/repos/o/r/releases/tags/v0.2.0 answered 404 Not Found"},
		{"/repos/o/r/releases/latest", http.StatusInternalServerError, false, "/repos/o/r/releases/latest answered 500 Internal Server Error"},
		{"/repos/o/r/releases/latest", http.StatusForbidden, false, "/repos/o/r/releases/latest answered 403 Forbidden"},
	} {
		t.Run(c.path+" "+http.StatusText(c.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(c.status) }))
			defer srv.Close()
			cl := &Client{URL: srv.URL + c.path, HTTP: srv.Client(), Arch: "arm64", Current: "0.1.0-rc.1"}
			_, err := cl.Latest(context.Background())
			if err == nil || errors.Is(err, ErrNoRelease) != c.none || !strings.HasSuffix(err.Error(), c.want) {
				t.Fatalf("Latest error = %v, want ErrNoRelease %v ending %q", err, c.none, c.want)
			}
		})
	}
}
