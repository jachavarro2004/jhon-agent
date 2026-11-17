package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

// tarball is a release tarball whose build/jhon reports ver.
func tarball(t *testing.T, ver string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	script := "#!/bin/sh\necho '{\"version\":\"" + ver + "\",\"commit\":\"0123456789abcdef0123456789abcdef01234567\"}'\n"
	for _, h := range []*tar.Header{
		{Name: "build/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "build/jhon", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(script))},
	} {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
	}
	tw.Write([]byte(script))
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// published is a release vtag, its assets served by a TLS httptest server
// whose client the returned Client uses, https only, as in production.
type published struct {
	tag    string
	assets map[string][]byte
	// url, when set, replaces an asset's URL in the API's answer.
	url map[string]string
}

func (p *published) serve(t *testing.T) (*Client, *httptest.Server) {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest" {
			var assets []map[string]string
			for n := range p.assets {
				u := srv.URL + "/dl/" + n
				if p.url[n] != "" {
					u = p.url[n]
				}
				assets = append(assets, map[string]string{"name": n, "browser_download_url": u})
			}
			json.NewEncoder(w).Encode(map[string]any{"tag_name": p.tag, "assets": assets})
			return
		}
		b, ok := p.assets[strings.TrimPrefix(r.URL.Path, "/dl/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	c := &Client{URL: srv.URL + "/latest", HTTP: srv.Client(), Arch: "amd64", Current: "0.1.0", HTTPSOnly: true}
	c.HTTP.CheckRedirect = httpsRedirects
	return c, srv
}

// signed is release v's assets: tar as its tarball, its .sha256, and the
// release workflow's signature, from the virtual Sigstore the client trusts.
func signed(t *testing.T, c *Client, v string, tar []byte) map[string][]byte {
	t.Helper()
	vs := virtual(t)
	c.trust = func() (Trust, error) { return Trust{Material: vs}, nil }
	n := "jhon-" + v + "-linux-amd64.tar.gz"
	sum := sha256.Sum256(tar)
	return map[string][]byte{n: tar, n + ".sha256": []byte(hex.EncodeToString(sum[:]) + "  " + n + "\n"),
		n + ".sigstore.json": signBundle(t, vs, "https://github.com/jachavarro2004/jhon-agent/.github/workflows/release.yml@refs/tags/v"+v, Issuer, tar)}
}

// fetch reads the latest release and fetches it into a fresh directory.
func fetch(t *testing.T, c *Client) (string, error) {
	t.Helper()
	l, err := c.Latest(context.Background())
	if err != nil {
		return "", err
	}
	a, _, err := c.Fetch(context.Background(), l, t.TempDir())
	return a, err
}

func TestFetch(t *testing.T) {
	const n = "jhon-0.2.0-linux-amd64.tar.gz"
	t.Run("a signed release whose binary is stamped its tag is fetched", func(t *testing.T) {
		p := &published{tag: "v0.2.0"}
		c, _ := p.serve(t)
		p.assets = signed(t, c, "0.2.0", tarball(t, "0.2.0"))
		if a, err := fetch(t, c); err != nil || !strings.HasSuffix(a, "/"+n) {
			t.Fatalf("Fetch = %q, %v", a, err)
		}
	})
	t.Run("a release candidate stamped its rc tag is fetched", func(t *testing.T) {
		p := &published{tag: "v0.1.0-rc.1"}
		c, _ := p.serve(t)
		p.assets = signed(t, c, "0.1.0-rc.1", tarball(t, "0.1.0-rc.1"))
		if a, err := fetch(t, c); err != nil || !strings.HasSuffix(a, "/jhon-0.1.0-rc.1-linux-amd64.tar.gz") {
			t.Fatalf("Fetch = %q, %v", a, err)
		}
	})
	refused := func(t *testing.T, c *Client, want string) {
		t.Helper()
		_, err := fetch(t, c)
		if err == nil || !regexp.MustCompile(want).MatchString(err.Error()) {
			t.Fatalf("error = %v, want %s", err, want)
		}
	}
	t.Run("a signed tarball of 0.1.0 published as 0.2.0 is refused", func(t *testing.T) {
		p := &published{tag: "v0.2.0"}
		c, _ := p.serve(t)
		p.assets = signed(t, c, "0.2.0", tarball(t, "0.1.0"))
		refused(t, c, `^`+regexp.QuoteMeta(n+" holds jhon 0.1.0, not the release's 0.2.0")+`$`)
	})
	t.Run("a .sha256 naming another file is refused", func(t *testing.T) {
		p := &published{tag: "v0.2.0"}
		c, _ := p.serve(t)
		p.assets = signed(t, c, "0.2.0", tarball(t, "0.2.0"))
		sum, _, _ := strings.Cut(string(p.assets[n+".sha256"]), " ")
		p.assets[n+".sha256"] = []byte(sum + "  jhon-0.1.0-linux-amd64.tar.gz\n")
		refused(t, c, `^`+regexp.QuoteMeta(n+".sha256 is not the checksum of "+n)+`$`)
	})
	for _, c := range []struct{ name, asset, want string }{
		{"an oversized .sha256 is refused", n + ".sha256", `\.sha256 is larger than 4096 bytes$`},
		{"an oversized bundle is refused", n + ".sigstore.json", `\.sigstore\.json is larger than 1048576 bytes$`},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := &published{tag: "v0.2.0"}
			cl, _ := p.serve(t)
			p.assets = signed(t, cl, "0.2.0", tarball(t, "0.2.0"))
			p.assets[c.asset] = bytes.Repeat([]byte(" "), 1<<20+1)
			refused(t, cl, c.want)
		})
	}
	t.Run("an oversized tarball is refused", func(t *testing.T) {
		defer func(m int64) { maxTarball = m }(maxTarball)
		maxTarball = 64
		p := &published{tag: "v0.2.0"}
		c, _ := p.serve(t)
		p.assets = signed(t, c, "0.2.0", tarball(t, "0.2.0"))
		refused(t, c, regexp.QuoteMeta(n)+` is larger than 64 bytes$`)
	})
	t.Run("an oversized answer from the API is refused", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"tag_name":"v0.2.0","body":"` + strings.Repeat("x", 1<<20) + `"}`))
		}))
		defer srv.Close()
		c := &Client{URL: srv.URL, HTTP: srv.Client(), Arch: "amd64", HTTPSOnly: true}
		refused(t, c, `^`+regexp.QuoteMeta(srv.URL)+` is larger than 1048576 bytes$`)
	})
	t.Run("an asset URL that is not https is refused", func(t *testing.T) {
		p := &published{tag: "v0.2.0", url: map[string]string{n: "http://127.0.0.1:1/" + n}}
		c, _ := p.serve(t)
		p.assets = signed(t, c, "0.2.0", tarball(t, "0.2.0"))
		refused(t, c, `^`+regexp.QuoteMeta("release 0.2.0's "+n+" is not at an https URL: http://127.0.0.1:1/"+n)+`$`)
	})
	t.Run("a redirect away from https is refused", func(t *testing.T) {
		plain := httptest.NewServer(http.NotFoundHandler())
		defer plain.Close()
		p := &published{tag: "v0.2.0"}
		c, _ := p.serve(t)
		p.assets = signed(t, c, "0.2.0", tarball(t, "0.2.0"))
		redirect := httptest.NewTLSServer(http.RedirectHandler(plain.URL+"/x", http.StatusFound))
		defer redirect.Close()
		c.URL = redirect.URL
		c.HTTP.Transport = redirect.Client().Transport
		refused(t, c, `redirect to `+regexp.QuoteMeta(plain.URL+"/x")+` is not https`)
	})
	t.Run("releases are read from the public repository jachavarro2004/jhon-agent (T18)", func(t *testing.T) {
		if LatestURL != "https://api.github.com/repos/jachavarro2004/jhon-agent/releases/latest" {
			t.Errorf("LatestURL = %q", LatestURL)
		}
		if Source != "github.com/jachavarro2004/jhon-agent" {
			t.Errorf("Source = %q", Source)
		}
	})
	t.Run("the default URL is https only; JHON_RELEASES_URL may be http", func(t *testing.T) {
		t.Setenv("JHON_RELEASES_URL", "")
		os.Unsetenv("JHON_RELEASES_URL")
		if c := New(); c.URL != LatestURL || !c.HTTPSOnly {
			t.Fatalf("New = %q, https only %v", c.URL, c.HTTPSOnly)
		}
		t.Setenv("JHON_RELEASES_URL", "http://127.0.0.1:1/latest")
		if c := New(); c.HTTPSOnly {
			t.Fatal("JHON_RELEASES_URL set: https only")
		}
	})
	t.Run("an http URL is refused when https only", func(t *testing.T) {
		c := &Client{URL: "http://127.0.0.1:1/latest", HTTP: http.DefaultClient, HTTPSOnly: true}
		refused(t, c, `^`+regexp.QuoteMeta("http://127.0.0.1:1/latest is not an https URL")+`$`)
	})
}
