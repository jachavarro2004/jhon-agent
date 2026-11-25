package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sigstore/sigstore-go/pkg/testing/ca"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T242 (odd/tasks/studio-integration-v1.md S5), end to end through the
// built jhon (jhon_testroot): with integrations.studio on, `jhon update`
// installs jhon-studio's newest signed release that supports it from the
// releases API JHON_TOOL_RELEASES_URL names, into
// ~/.local/share/jhon-studio/<version> linked from ~/.local/bin; a newer
// release updates it beside the previous one; `jhon update --rollback`
// links the previous one again; one whose signature is not the studio's
// release workflow's at its tag changes nothing.

// studioRelease is a jhon-studio tarball of v, as tools/pack.sh packs it,
// whose program answers `integration --json` as v requiring requires.
func studioRelease(t *testing.T, v, requires string) []byte {
	t.Helper()
	script := "#!/bin/sh\ncase \"$*\" in\n  'integration --json') echo '{\"version\":\"" + v + "\",\"plugin_dir\":\"/p\",\"requires_jhon\":\"" + requires + "\",\"contract\":\"jhon.integration/v1\"}' ;;\n  *) exit 2 ;;\nesac\n"
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range []*tar.Header{
		{Name: "bin/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "bin/jhon-studio", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(script))},
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

// studioReleases serves jhon-studio's releases API and downloads; jhon's
// own latest release is none yet (404).
type studioReleases struct {
	mu   sync.Mutex
	list []map[string]any
	blob map[string][]byte
}

func (s *studioReleases) add(t *testing.T, vs *ca.VirtualSigstore, base, v, requires, signer string) {
	t.Helper()
	tarball := studioRelease(t, v, requires)
	n := "jhon-studio-" + v + ".tar.gz"
	sum := sha256.Sum256(tarball)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blob == nil {
		s.blob = map[string][]byte{}
	}
	s.blob[v+"/"+n] = tarball
	s.blob[v+"/"+n+".sha256"] = []byte(hex.EncodeToString(sum[:]) + "  " + n + "\n")
	s.blob[v+"/"+n+".sigstore.json"] = signBundle(t, vs, signer, tarball)
	var assets []map[string]string
	for _, a := range []string{n, n + ".sha256", n + ".sigstore.json"} {
		assets = append(assets, map[string]string{"name": a, "browser_download_url": base + "/dl/" + v + "/" + a})
	}
	s.list = append([]map[string]any{{"tag_name": "v" + v, "prerelease": false, "assets": assets}}, s.list...)
}

func (s *studioReleases) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.URL.Path == "/repos/jachavarro2004/jhon-studio/releases" {
		json.NewEncoder(w).Encode(s.list)
		return
	}
	if b, ok := s.blob[strings.TrimPrefix(r.URL.Path, "/dl/")]; ok {
		w.Write(b)
		return
	}
	http.NotFound(w, r)
}

func TestUpdateInstallsAToolFromItsSignedRelease(t *testing.T) {
	vs, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatal(err)
	}
	e := scratch(t, trustedRoot(t, vs))
	support.Write(t, e.home, map[string]string{"settings.toml": "schema = 1\n\n[integrations]\nstudio = true\n"})
	jhon := cli(t, "0.5.0")
	rels := &studioReleases{}
	srv := httptest.NewServer(rels)
	defer srv.Close()
	studioAt := func(tag string) string {
		return "https://github.com/jachavarro2004/jhon-studio/.github/workflows/release.yml@refs/tags/" + tag
	}
	env := []string{"JHON_RELEASES_URL=" + srv.URL + "/repos/jachavarro2004/jhon-agent/releases/latest",
		"JHON_TOOL_RELEASES_URL=" + srv.URL, "JHON_SETTINGS=" + filepath.Join(e.home, "settings.toml")}
	studio := filepath.Join(e.home, ".local", "share", "jhon-studio")
	link := filepath.Join(e.home, ".local", "bin", "jhon-studio")
	linksTo := func(v string) {
		t.Helper()
		got, err := os.Readlink(link)
		if want := filepath.Join(studio, v, "bin", "jhon-studio"); err != nil || got != want {
			t.Fatalf("%s -> %q (%v), want %s", link, got, err, want)
		}
	}

	rels.add(t, vs, srv.URL, "0.1.0", ">=0.5.0-rc.1", studioAt("v0.1.0"))
	rels.add(t, vs, srv.URL, "0.2.0", ">=0.6.0", studioAt("v0.2.0"))
	r := e.run(t, jhon, []string{"update"}, env...)
	wantCode(t, r, 0)
	match(t, r.Stdout, `(?s)^no stable release is published yet; jhon 0\.5\.0 is installed\n  ok    jhon-studio 0\.1\.0: sha256 and signature verified: signed by `+
		`https://github\.com/jachavarro2004/jhon-studio/\.github/workflows/release\.yml@refs/tags/v0\.1\.0\ninstalled jhon-studio 0\.1\.0 at ~/\.local/share/jhon-studio/0\.1\.0\n$`)
	linksTo("0.1.0")

	// L18 A3: a deleted active folder is installed again, never "up to date".
	if err := os.RemoveAll(filepath.Join(studio, "0.1.0")); err != nil {
		t.Fatal(err)
	}
	r = e.run(t, jhon, []string{"update", "--check"}, env...)
	wantCode(t, r, 0)
	match(t, r.Stdout, `\njhon-studio 0\.1\.0 needs reinstalling \(its folder is missing, or is not the one jhon verified\): run 'jhon update'\n$`)
	r = e.run(t, jhon, []string{"update"}, env...)
	wantCode(t, r, 0)
	match(t, r.Stdout, `\nreinstalled jhon-studio 0\.1\.0 at ~/\.local/share/jhon-studio/0\.1\.0 \(its folder was missing, or was not the one jhon verified\)\n$`)
	linksTo("0.1.0")

	// A newer release that supports this jhon updates it beside the first.
	rels.add(t, vs, srv.URL, "0.3.0", ">=0.5.0", studioAt("v0.3.0"))
	r = e.run(t, jhon, []string{"update"}, env...)
	wantCode(t, r, 0)
	match(t, r.Stdout, `\nupdated jhon-studio 0\.1\.0 -> 0\.3\.0\n$`)
	linksTo("0.3.0")
	if _, err := os.Stat(filepath.Join(studio, "0.1.0", "bin", "jhon-studio")); err != nil {
		t.Errorf("the previous version is gone: %v", err)
	}

	r = e.run(t, jhon, []string{"update", "--rollback"}, env...)
	wantCode(t, r, 0)
	equal(t, r.Stdout, "no release older than jhon 0.5.0 is kept; jhon stays at 0.5.0\nrolled back jhon-studio 0.3.0 -> 0.1.0\n")
	linksTo("0.1.0")

	// A release signed by another workflow changes nothing.
	rels.add(t, vs, srv.URL, "0.4.0", ">=0.5.0", "https://github.com/someone/jhon-studio/.github/workflows/release.yml@refs/tags/v0.4.0")
	if err := os.MkdirAll(share(e.home), 0o755); err != nil {
		t.Fatal(err)
	}
	versions := func() string {
		names, _ := filepath.Glob(filepath.Join(studio, "*"))
		return strings.Join(names, " ")
	}
	before, kept := snapshot(t, e.home), versions()
	r = e.run(t, jhon, []string{"update"}, env...)
	wantCode(t, r, 1)
	match(t, r.Stderr, `^jhon: jhon-studio: jhon-studio-0\.4\.0\.tar\.gz is signed by https://github\.com/someone/jhon-studio/.*, not by the release workflow; nothing was changed\n$`)
	sameSnapshot(t, snapshot(t, e.home), before)
	equal(t, versions(), kept)
	linksTo("0.1.0")
}
