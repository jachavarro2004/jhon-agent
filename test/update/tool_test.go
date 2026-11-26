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

	// T8c (L45): the studio's own workflow at another tag names the identity
	// expected and the one found, and changes nothing.
	rels.add(t, vs, srv.URL, "0.5.0", ">=0.5.0", studioAt("v0.4.0"))
	r = e.run(t, jhon, []string{"update"}, env...)
	wantCode(t, r, 1)
	equal(t, r.Stderr, "jhon: jhon-studio: jhon-studio-0.5.0.tar.gz is signed by the release workflow at another ref, not at the release's tag: expected "+
		studioAt("v0.5.0")+", found "+studioAt("v0.4.0")+"; nothing was changed\n")
	sameSnapshot(t, snapshot(t, e.home), before)
	equal(t, versions(), kept)
	linksTo("0.1.0")
}

// T8c (L45): GitHub's API refusing under its rate limit, for jhon's own
// release (JHON_RELEASES_URL) and for a tool's (JHON_TOOL_RELEASES_URL), is
// said as such, with the reset GitHub gives, and changes nothing.
func TestUpdateNamesGitHubsRateLimit(t *testing.T) {
	limited := func(w http.ResponseWriter, code int, headers ...string) {
		for i := 0; i+1 < len(headers); i += 2 {
			w.Header().Set(headers[i], headers[i+1])
		}
		w.WriteHeader(code)
		w.Write([]byte(`{"message":"API rate limit exceeded for 203.0.113.7.","documentation_url":"https://docs.github.com/rest/overview/resources-in-the-rest-api#rate-limiting"}`))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/limited/repos/jachavarro2004/jhon-agent/releases/latest":
			limited(w, http.StatusForbidden, "X-RateLimit-Remaining", "0", "X-RateLimit-Reset", "1795000000")
		case "/repos/jachavarro2004/jhon-studio/releases":
			limited(w, http.StatusTooManyRequests, "Retry-After", "60")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	e := scratch(t, []byte("{}"))
	support.Write(t, e.home, map[string]string{"settings.toml": "schema = 1\n\n[integrations]\nstudio = true\n"})
	jhon := cli(t, "0.5.0")
	settings := "JHON_SETTINGS=" + filepath.Join(e.home, "settings.toml")

	latest := srv.URL + "/limited/repos/jachavarro2004/jhon-agent/releases/latest"
	r := e.run(t, jhon, []string{"update", "--check"}, "JHON_RELEASES_URL="+latest, "JHON_TOOL_RELEASES_URL="+srv.URL, settings)
	wantCode(t, r, 1)
	equal(t, r.Stderr, "jhon: update failed: "+latest+" refused: GitHub's API rate limit for this address is used up (403 Forbidden, X-RateLimit-Remaining: 0); "+
		"it resets at 2026-11-18 11:06:40 UTC; jhon sends no GitHub token, so retry after the reset; nothing was changed\n")

	for _, d := range []string{share(e.home), filepath.Join(e.home, ".local", "bin")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	before := snapshot(t, e.home)
	r = e.run(t, jhon, []string{"update"}, "JHON_RELEASES_URL="+srv.URL+"/repos/jachavarro2004/jhon-agent/releases/latest", "JHON_TOOL_RELEASES_URL="+srv.URL, settings)
	wantCode(t, r, 1)
	equal(t, r.Stderr, "jhon: jhon-studio: "+srv.URL+"/repos/jachavarro2004/jhon-studio/releases?per_page=30 refused: GitHub's API rate limit for this address is used up (429 Too Many Requests); "+
		"it resets in 60 s; jhon sends no GitHub token, so retry after the reset; nothing was changed\n")
	sameSnapshot(t, snapshot(t, e.home), before)
}

// T247 (odd/tasks/phase1-security.md S4): `jhon update --check` gives the
// advice the update would: a changed active folder, which the update
// refuses, is said with its fix (move it away), not "run 'jhon update'";
// a missing one still says to run it.
func TestUpdateCheckSaysAChangedToolFolderIsRefused(t *testing.T) {
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
	env := []string{"JHON_RELEASES_URL=" + srv.URL + "/repos/jachavarro2004/jhon-agent/releases/latest",
		"JHON_TOOL_RELEASES_URL=" + srv.URL, "JHON_SETTINGS=" + filepath.Join(e.home, "settings.toml")}
	rels.add(t, vs, srv.URL, "0.1.0", ">=0.5.0-rc.1", "https://github.com/jachavarro2004/jhon-studio/.github/workflows/release.yml@refs/tags/v0.1.0")
	wantCode(t, e.run(t, jhon, []string{"update"}, env...), 0)
	program := filepath.Join(e.home, ".local", "share", "jhon-studio", "0.1.0", "bin", "jhon-studio")
	if err := os.WriteFile(program, []byte("#!/bin/sh\necho changed\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(share(e.home), 0o755); err != nil {
		t.Fatal(err)
	}
	refused := "jhon: jhon-studio: ~/.local/share/jhon-studio/0.1.0 is not jhon-studio's signed release 0.1.0 (its files differ); move it away for jhon to install it; nothing was changed\n"
	before := snapshot(t, e.home)
	r := e.run(t, jhon, []string{"update", "--check"}, env...)
	wantCode(t, r, 1)
	equal(t, r.Stderr, refused)
	if strings.Contains(r.Stdout, "jhon-studio") {
		t.Errorf("stdout: %q", r.Stdout)
	}
	r = e.run(t, jhon, []string{"update", "--check", "--json"}, env...)
	wantCode(t, r, 1)
	var out struct {
		Tools []map[string]string `json:"tools"`
	}
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil || len(out.Tools) != 1 || out.Tools[0]["status"] != "failed" ||
		out.Tools[0]["error"] != strings.TrimSuffix(strings.TrimPrefix(refused, "jhon: jhon-studio: "), "\n") {
		t.Errorf("--json: %s (%v)", r.Stdout, err)
	}
	r = e.run(t, jhon, []string{"update"}, env...)
	wantCode(t, r, 1)
	equal(t, r.Stderr, refused)
	sameSnapshot(t, snapshot(t, e.home), before)
}
