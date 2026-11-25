package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sigstore/sigstore-go/pkg/testing/ca"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
)

// T242 (odd/tasks/studio-integration-v1.md S5): jhon installs a cli
// integration's program (jhon-studio) from its signed GitHub releases, the
// newest whose requires_jhon the running jhon satisfies, verified as jhon's
// own releases are (sha256, then the cosign keyless bundle against that
// repository's release workflow at the release's tag), unpacked into
// ~/.local/share/<binary>/<version> and linked from ~/.local/bin/<binary>;
// the previous version stays for a roll-back. A development clone is left
// alone. Everything here is served by an httptest server and signed by a
// virtual Sigstore: no network.

// studioTool is the compiled-in studio manifest's release source.
func studioTool(t *testing.T) Tool {
	t.Helper()
	m, ok := integrations.Get("studio")
	if !ok {
		t.Fatal("no studio manifest")
	}
	tool, ok := ToolOf(m)
	if !ok {
		t.Fatal("ToolOf(studio) = false")
	}
	return tool
}

// entry is one member of a test tarball.
type entry struct {
	name, body, link string
	kind             byte
	mode             int64
}

// pack is a gzip tarball of entries, in order.
func pack(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Typeflag: e.kind, Mode: e.mode, Linkname: e.link}
		if h.Typeflag == 0 {
			h.Typeflag = tar.TypeReg
		}
		if h.Mode == 0 {
			h.Mode = 0o644
		}
		if h.Typeflag == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// studioScript is a bin/jhon-studio that answers `integration --json` as
// version v requiring requires ("" declares none).
func studioScript(v, requires string) string {
	answer := `{"schema":"jhon-studio.integration/v1","version":"` + v + `","plugin_dir":"/p","skill":"/p/SKILL.md","contract":"jhon.integration/v1"`
	if requires != "" {
		answer += `,"requires_jhon":"` + requires + `"`
	}
	answer += "}"
	return "#!/bin/sh\ncase \"$*\" in\n  'integration --json') echo '" + answer + "' ;;\n  '--version') echo " + v + " ;;\n  *) exit 2 ;;\nesac\n"
}

// studioTar is a release tarball of jhon-studio v, laid out as tools/pack.sh
// packs it: no leading folder.
func studioTar(t *testing.T, v, requires string) []byte {
	return pack(t, []entry{
		{name: "bin/", kind: tar.TypeDir, mode: 0o755},
		{name: "bin/jhon-studio", body: studioScript(v, requires), mode: 0o755},
		{name: "plugin/", kind: tar.TypeDir, mode: 0o755},
		{name: "plugin/README.md", body: "plugin " + v + "\n"},
	})
}

// toolRelease is one published release of the fake repository.
type toolRelease struct {
	tag string
	pre bool
	// assets by name; a missing one is not published.
	assets map[string][]byte
}

// repo serves the studio repository's releases API and downloads over TLS.
type repo struct {
	releases []toolRelease
	hits     atomic.Int64
}

func (r *repo) serve(t *testing.T, vs *ca.VirtualSigstore) *Client {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.hits.Add(1)
		if req.URL.Path == "/repos/jachavarro2004/jhon-studio/releases" {
			var out []map[string]any
			for _, rel := range r.releases {
				var assets []map[string]string
				for n := range rel.assets {
					assets = append(assets, map[string]string{"name": n, "browser_download_url": srv.URL + "/dl/" + rel.tag + "/" + n})
				}
				out = append(out, map[string]any{"tag_name": rel.tag, "prerelease": rel.pre, "assets": assets})
			}
			json.NewEncoder(w).Encode(out)
			return
		}
		tag, n, _ := strings.Cut(strings.TrimPrefix(req.URL.Path, "/dl/"), "/")
		for _, rel := range r.releases {
			if b, ok := rel.assets[n]; ok && rel.tag == tag {
				w.Write(b)
				return
			}
		}
		http.NotFound(w, req)
	}))
	t.Cleanup(srv.Close)
	c := &Client{URL: srv.URL, HTTP: srv.Client(), HTTPSOnly: true, trust: func() (Trust, error) { return Trust{Material: vs}, nil }}
	c.HTTP.CheckRedirect = httpsRedirects
	return c
}

// studioWorkflow is the studio repository's release workflow at tag.
func studioWorkflow(tag string) string {
	return "https://github.com/jachavarro2004/jhon-studio/.github/workflows/release.yml@refs/tags/" + tag
}

// signedTool is release v of jhon-studio holding tarball, signed by signer
// (the studio release workflow at v's tag when "").
func signedTool(t *testing.T, vs *ca.VirtualSigstore, v string, tarball []byte, signer string) toolRelease {
	t.Helper()
	if signer == "" {
		signer = studioWorkflow("v" + v)
	}
	n := "jhon-studio-" + v + ".tar.gz"
	sum := sha256.Sum256(tarball)
	return toolRelease{tag: "v" + v, pre: strings.Contains(v, "-"), assets: map[string][]byte{
		n:                    tarball,
		n + ".sha256":        []byte(hex.EncodeToString(sum[:]) + "  " + n + "\n"),
		n + ".sigstore.json": signBundle(t, vs, signer, Issuer, tarball),
	}}
}

// home is a scratch HOME and the tool's place in it.
func home(t *testing.T, tool Tool) Place {
	t.Helper()
	h, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return tool.PlaceIn(h)
}

// tree is every path under the place's folder and its link, with a file's
// content hash or a link's target: what a refusal leaves byte-identical.
func tree(t *testing.T, p Place) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, root := range []string{p.Dir, filepath.Dir(p.Link)} {
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			switch {
			case d.Type()&fs.ModeSymlink != 0:
				l, _ := os.Readlink(path)
				out[path] = "-> " + l
			case d.Type().IsRegular():
				b, _ := os.ReadFile(path)
				h := sha256.Sum256(b)
				out[path] = hex.EncodeToString(h[:])
			default:
				out[path] = "dir"
			}
			return nil
		})
	}
	return out
}

func linked(t *testing.T, p Place) string {
	t.Helper()
	l, err := os.Readlink(p.Link)
	if err != nil {
		t.Fatalf("no link at %s: %v", p.Link, err)
	}
	return l
}

func TestAToolsReleasesAreItsSourcesSignedByItsReleaseWorkflowAtTheTag(t *testing.T) {
	tool := studioTool(t)
	if tool.Binary != "jhon-studio" || tool.Owner != "jachavarro2004" || tool.Repo != "jhon-studio" || tool.Source() != "github.com/jachavarro2004/jhon-studio" {
		t.Errorf("ToolOf(studio) = %+v", tool)
	}
	if got := tool.Asset("0.1.0"); got != "jhon-studio-0.1.0.tar.gz" {
		t.Errorf("Asset = %q", got)
	}
	s := tool.SignerAt("0.1.0")
	if s.Issuer != Issuer || s.Identity != `^https://github\.com/jachavarro2004/jhon-studio/\.github/workflows/release\.yml@refs/tags/v0\.1\.0$` {
		t.Errorf("SignerAt = %+v", s)
	}
	for _, m := range []integrations.Manifest{
		{Kind: "mcp-stdio", Doctor: integrations.Doctor{Binary: "x", Source: "github.com/a/b"}},
		{Kind: "cli", Doctor: integrations.Doctor{Binary: "x", Source: "github.com/a/b/v3"}},
		{Kind: "cli", Doctor: integrations.Doctor{Binary: "x", Source: "npm @a/b"}},
		{Kind: "cli", Doctor: integrations.Doctor{Binary: "x", Source: "github.com/../b"}},
		{Kind: "cli", Doctor: integrations.Doctor{Binary: "../x", Source: "github.com/a/b"}},
		{Kind: "cli", Doctor: integrations.Doctor{Source: "github.com/a/b"}},
	} {
		if _, ok := ToolOf(m); ok {
			t.Errorf("ToolOf(%+v) = true", m)
		}
	}

	vs := virtual(t)
	dir := t.TempDir()
	artifact := filepath.Join(dir, "jhon-studio-0.1.0.tar.gz")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(artifact, []byte("the release"), 0o644))
	for _, c := range []struct{ name, signer, want string }{
		{"the studio's release workflow at v0.1.0 verifies", studioWorkflow("v0.1.0"), ""},
		{"its workflow at another tag is refused", studioWorkflow("v0.2.0"), "not by the release workflow"},
		{"jhon-agent's release workflow is refused", "https://github.com/jachavarro2004/jhon-agent/.github/workflows/release.yml@refs/tags/v0.1.0", "not by the release workflow"},
		{"a fork is refused", "https://github.com/someone/jhon-studio/.github/workflows/release.yml@refs/tags/v0.1.0", "not by the release workflow"},
		{"a branch is refused", "https://github.com/jachavarro2004/jhon-studio/.github/workflows/release.yml@refs/heads/v0.1.0", "not by the release workflow"},
	} {
		b := filepath.Join(dir, "b.sigstore.json")
		must(os.WriteFile(b, signBundle(t, vs, c.signer, Issuer, []byte("the release")), 0o644))
		got, err := VerifyAs(artifact, b, Trust{Material: vs}, s)
		switch {
		case c.want == "" && (err != nil || got != c.signer):
			t.Errorf("%s: %q, %v", c.name, got, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

// The newest release whose requires_jhon this jhon satisfies is installed:
// a newer one that needs a newer jhon is passed over with its note, a
// pre-release is taken only by a pre-release or development jhon.
func TestInstallToolPicksTheNewestCompatibleRelease(t *testing.T) {
	tool := studioTool(t)
	vs := virtual(t)
	r := &repo{}
	c := r.serve(t, vs)
	r.releases = []toolRelease{
		signedTool(t, vs, "0.3.0", studioTar(t, "0.3.0", ">=9.0.0"), ""),
		signedTool(t, vs, "0.2.1-rc.1", studioTar(t, "0.2.1-rc.1", ">=0.5.0-rc.1"), ""),
		signedTool(t, vs, "0.2.0", studioTar(t, "0.2.0", ">=0.5.0-rc.1"), ""),
		signedTool(t, vs, "0.1.0", studioTar(t, "0.1.0", ""), ""),
		{tag: "nightly", assets: map[string][]byte{}},
	}
	for _, x := range []struct{ jhon, want, rule string }{
		{"0.5.0", "0.2.0", "a stable jhon takes stable releases only"},
		{"0.5.0-rc.1", "0.2.1-rc.1", "a pre-release jhon takes pre-releases too"},
		{"0.0.0-dev+abc1234", "0.3.0", "a development jhon takes pre-releases, and passes any minimum"},
	} {
		t.Run(x.rule, func(t *testing.T) {
			p := home(t, tool)
			res, err := c.InstallTool(context.Background(), tool, p, x.jhon, false)
			if err != nil {
				t.Fatal(err)
			}
			if res.Status != ToolInstalled || res.From != "" || res.To != x.want || res.Signer != studioWorkflow("v"+x.want) {
				t.Errorf("result = %+v", res)
			}
			if want := filepath.Join(p.Dir, x.want, "bin", "jhon-studio"); linked(t, p) != want {
				t.Errorf("link -> %s, want %s", linked(t, p), want)
			}
			if b, err := os.ReadFile(filepath.Join(p.Dir, x.want, "plugin", "README.md")); err != nil || string(b) != "plugin "+x.want+"\n" {
				t.Errorf("unpacked plugin: %q, %v", b, err)
			}
			if inst := tool.Installed(p); inst.Active != x.want || inst.Dev || inst.Foreign != "" || !slices.Equal(inst.Kept, []string{x.want}) {
				t.Errorf("Installed = %+v", inst)
			}
		})
	}
	t.Run("a newer release that needs a newer jhon is passed over with its note", func(t *testing.T) {
		p := home(t, tool)
		res, err := c.InstallTool(context.Background(), tool, p, "0.5.0", false)
		want := "jhon-studio 0.3.0 requires jhon >=9.0.0, and this is jhon 0.5.0: jhon-studio is off for this run; update jhon (jhon update), or install a jhon-studio version that supports jhon 0.5.0"
		if err != nil || len(res.Skipped) != 1 || res.Skipped[0] != want {
			t.Errorf("Skipped = %q, %v", res.Skipped, err)
		}
	})
	t.Run("no release that supports this jhon installs nothing", func(t *testing.T) {
		r := &repo{}
		c := r.serve(t, vs)
		r.releases = []toolRelease{
			signedTool(t, vs, "0.3.0", studioTar(t, "0.3.0", ">=9.0.0"), ""),
			signedTool(t, vs, "0.2.0", studioTar(t, "0.2.0", ">=0.5.0-rc.1"), ""),
		}
		p := home(t, tool)
		before := tree(t, p)
		_, err := c.InstallTool(context.Background(), tool, p, "0.4.0", false)
		if err == nil || err.Error() != "no jhon-studio release supports jhon 0.4.0 (the newest, 0.3.0, requires jhon >=9.0.0); nothing was changed" {
			t.Errorf("error = %v", err)
		}
		if after := tree(t, p); !mapsEqual(before, after) {
			t.Errorf("changed:\n%v\n%v", before, after)
		}
	})
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// An update unpacks beside the previous version, which stays; a check
// reports without changing anything; up to date is said; a roll-back links
// the previous version again.
func TestInstallToolUpdatesKeepsThePreviousAndRollsBack(t *testing.T) {
	tool := studioTool(t)
	vs := virtual(t)
	r := &repo{}
	c := r.serve(t, vs)
	p := home(t, tool)
	ctx := context.Background()
	r.releases = []toolRelease{signedTool(t, vs, "0.1.0", studioTar(t, "0.1.0", ">=0.5.0"), "")}
	if res, err := c.InstallTool(ctx, tool, p, "0.5.0", false); err != nil || res.Status != ToolInstalled || res.To != "0.1.0" {
		t.Fatalf("first install: %+v, %v", res, err)
	}
	if res, err := c.InstallTool(ctx, tool, p, "0.5.0", false); err != nil || res.Status != ToolUpToDate || res.From != "0.1.0" || res.To != "0.1.0" {
		t.Fatalf("again: %+v, %v", res, err)
	}
	r.releases = append([]toolRelease{signedTool(t, vs, "0.2.0", studioTar(t, "0.2.0", ">=0.5.0"), "")}, r.releases...)
	before := tree(t, p)
	if res, err := c.InstallTool(ctx, tool, p, "0.5.0", true); err != nil || res.Status != ToolAvailable || res.From != "0.1.0" || res.To != "0.2.0" {
		t.Fatalf("check: %+v, %v", res, err)
	}
	if after := tree(t, p); !mapsEqual(before, after) {
		t.Errorf("a check changed the install:\n%v\n%v", before, after)
	}
	if res, err := c.InstallTool(ctx, tool, p, "0.5.0", false); err != nil || res.Status != ToolUpdated || res.From != "0.1.0" || res.To != "0.2.0" {
		t.Fatalf("update: %+v, %v", res, err)
	}
	if inst := tool.Installed(p); inst.Active != "0.2.0" || !slices.Equal(inst.Kept, []string{"0.1.0", "0.2.0"}) {
		t.Errorf("after the update: %+v", inst)
	}
	from, to, err := RollbackTool(tool, p)
	if err != nil || from != "0.2.0" || to != "0.1.0" {
		t.Fatalf("RollbackTool = %s, %s, %v", from, to, err)
	}
	if want := filepath.Join(p.Dir, "0.1.0", "bin", "jhon-studio"); linked(t, p) != want {
		t.Errorf("link -> %s, want %s", linked(t, p), want)
	}
	if _, _, err := RollbackTool(tool, p); !errors.Is(err, ErrNoOlderTool) {
		t.Errorf("a second roll-back: %v", err)
	}
	if inst := tool.Installed(p); !slices.Equal(inst.Kept, []string{"0.1.0", "0.2.0"}) {
		t.Errorf("a roll-back removed a version: %+v", inst)
	}
}

// A development clone (a git checkout at ~/.local/share/<binary>) is left
// alone: nothing is asked of the network, nothing written; a link jhon did
// not make is never replaced.
func TestInstallToolLeavesADevelopmentCloneAndAForeignLinkAlone(t *testing.T) {
	tool := studioTool(t)
	vs := virtual(t)
	r := &repo{}
	c := r.serve(t, vs)
	r.releases = []toolRelease{signedTool(t, vs, "0.1.0", studioTar(t, "0.1.0", ""), "")}
	ctx := context.Background()

	p := home(t, tool)
	for _, d := range []string{filepath.Join(p.Dir, ".git"), filepath.Join(p.Dir, "bin"), filepath.Dir(p.Link)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(p.Dir, "bin", "jhon-studio"), p.Link); err != nil {
		t.Fatal(err)
	}
	before := tree(t, p)
	for _, check := range []bool{false, true} {
		res, err := c.InstallTool(ctx, tool, p, "0.5.0", check)
		if err != nil || res.Status != ToolDevelopment {
			t.Errorf("check %v: %+v, %v", check, res, err)
		}
	}
	if from, to, err := RollbackTool(tool, p); !errors.Is(err, ErrDevelopmentTool) || from != "" || to != "" {
		t.Errorf("RollbackTool of a clone = %s, %s, %v", from, to, err)
	}
	if after := tree(t, p); !mapsEqual(before, after) || r.hits.Load() != 0 {
		t.Errorf("a development clone was touched (%d requests):\n%v\n%v", r.hits.Load(), before, after)
	}
	if inst := tool.Installed(p); !inst.Dev {
		t.Errorf("Installed = %+v", inst)
	}

	for name, make := range map[string]func(Place) error{
		"a file":                   func(p Place) error { return os.WriteFile(p.Link, []byte("#!/bin/sh\n"), 0o755) },
		"a link to somewhere else": func(p Place) error { return os.Symlink("/opt/jhon-studio/bin/jhon-studio", p.Link) },
	} {
		p := home(t, tool)
		if err := os.MkdirAll(filepath.Dir(p.Link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := make(p); err != nil {
			t.Fatal(err)
		}
		before := tree(t, p)
		// L18 A3: a link jhon did not make is skipped with one note, not a
		// failure, as a development clone is.
		res, err := c.InstallTool(ctx, tool, p, "0.5.0", false)
		if err != nil || res.Status != ToolForeign || !regexp.MustCompile(`^.*/\.local/bin/jhon-studio is (a file|a link to /opt/jhon-studio/bin/jhon-studio), not jhon's link to a jhon-studio release$`).MatchString(res.Note) {
			t.Errorf("%s: %+v, %v", name, res, err)
		}
		if _, _, err := RollbackTool(tool, p); !errors.As(err, new(*ForeignError)) {
			t.Errorf("%s: RollbackTool = %v", name, err)
		}
		if after := tree(t, p); !mapsEqual(before, after) {
			t.Errorf("%s: changed", name)
		}
	}

	// L18 A1: a share folder that is a link is never written through.
	p = home(t, tool)
	elsewhere := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(p.Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, p.Dir); err != nil {
		t.Fatal(err)
	}
	res, err := c.InstallTool(ctx, tool, p, "0.5.0", false)
	if err != nil || res.Status != ToolForeign || res.Note != p.Dir+" is a link to "+elsewhere+", not a folder" {
		t.Errorf("a share folder that is a link: %+v, %v", res, err)
	}
	if es, _ := os.ReadDir(elsewhere); len(es) != 0 {
		t.Errorf("written through the link: %v", es)
	}
}

// hostile is a tarball of jhon-studio v whose program, asked what jhon it
// needs, first runs sh (with the minimal environment it is given).
func hostile(t *testing.T, v, sh string) []byte {
	return pack(t, []entry{{name: "bin/jhon-studio", mode: 0o755, body: "#!/bin/sh\ncase \"$*\" in\n  'integration --json') " + sh +
		"\n    echo '{\"version\":\"" + v + "\",\"plugin_dir\":\"/p\"}' ;;\nesac\n"}})
}

// installed01 is a place where 0.1.0 is installed from its release, and a
// client whose repository then publishes rel.
func installed01(t *testing.T, vs *ca.VirtualSigstore) (*repo, *Client, Place) {
	t.Helper()
	tool := studioTool(t)
	r := &repo{}
	c := r.serve(t, vs)
	p := home(t, tool)
	r.releases = []toolRelease{signedTool(t, vs, "0.1.0", studioTar(t, "0.1.0", ""), "")}
	if _, err := c.InstallTool(context.Background(), tool, p, "0.5.0", false); err != nil {
		t.Fatal(err)
	}
	return r, c, p
}

// names are the entries of dir, sorted.
func names(t *testing.T, dir string) string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return strings.Join(out, " ")
}

// L18 A1: what the install's own program, or anything else, swaps in while
// it runs is left as it is: a file at the link, a link at the share folder.
// The install is refused before anything is written, through the link or
// elsewhere.
func TestInstallToolLeavesWhatAHostileProgramSwaps(t *testing.T) {
	tool := studioTool(t)
	vs := virtual(t)
	ctx := context.Background()
	t.Run("the link for a file and the share folder for a link", func(t *testing.T) {
		r, c, p := installed01(t, vs)
		elsewhere, moved := t.TempDir(), p.Dir+".moved"
		r.releases = append([]toolRelease{signedTool(t, vs, "0.2.0", hostile(t, "0.2.0",
			"rm -f '"+p.Link+"' && printf mine > '"+p.Link+"' && mv '"+p.Dir+"' '"+moved+"' && ln -s '"+elsewhere+"' '"+p.Dir+"'"), "")}, r.releases...)
		_, err := c.InstallTool(ctx, tool, p, "0.5.0", false)
		if err == nil || err.Error() != p.Dir+" became a link to "+elsewhere+" during the install; it was left as it is; nothing was changed" {
			t.Errorf("error = %v", err)
		}
		if b, err := os.ReadFile(p.Link); err != nil || string(b) != "mine" {
			t.Errorf("the link's file: %q, %v", b, err)
		}
		if l, err := os.Readlink(p.Dir); err != nil || l != elsewhere {
			t.Errorf("the share link: %q, %v", l, err)
		}
		if got := names(t, elsewhere); got != "" {
			t.Errorf("written through the link: %s", got)
		}
		if got := names(t, moved); got != ".jhon-verified 0.1.0" {
			t.Errorf("written into the moved folder: %s", got)
		}
	})
	t.Run("the link for a file", func(t *testing.T) {
		r, c, p := installed01(t, vs)
		r.releases = append([]toolRelease{signedTool(t, vs, "0.2.0", hostile(t, "0.2.0", "rm -f '"+p.Link+"' && printf mine > '"+p.Link+"'"), "")}, r.releases...)
		_, err := c.InstallTool(ctx, tool, p, "0.5.0", false)
		if err == nil || err.Error() != p.Link+" became a file, not jhon's link to a jhon-studio release, during the install; it was left as it is; nothing was changed" {
			t.Errorf("error = %v", err)
		}
		if b, _ := os.ReadFile(p.Link); string(b) != "mine" {
			t.Errorf("the link's file: %q", b)
		}
		if got := names(t, p.Dir); got != ".jhon-verified 0.1.0" {
			t.Errorf("share folder: %s", got)
		}
	})
	t.Run("the link swapped for a file right before it moves", func(t *testing.T) {
		r, c, p := installed01(t, vs)
		r.releases = append([]toolRelease{signedTool(t, vs, "0.2.0", studioTar(t, "0.2.0", ""), "")}, r.releases...)
		hookBeforeLink = func() {
			os.Remove(p.Link)
			os.WriteFile(p.Link, []byte("mine"), 0o644)
		}
		t.Cleanup(func() { hookBeforeLink = nil })
		_, err := c.InstallTool(ctx, tool, p, "0.5.0", false)
		if err == nil || err.Error() != "jhon-studio 0.2.0 is at "+filepath.Join(p.Dir, "0.2.0")+" but not linked: "+p.Link+" became a file, not jhon's link to a jhon-studio release, during the install; it was left as it is" {
			t.Errorf("error = %v", err)
		}
		if b, _ := os.ReadFile(p.Link); string(b) != "mine" {
			t.Errorf("the link's file: %q", b)
		}
		if es, _ := os.ReadDir(filepath.Dir(p.Link)); len(es) != 1 {
			t.Errorf("left beside the link: %v", es)
		}
	})
	t.Run("the share folder swapped for a link once it is open", func(t *testing.T) {
		r, c, p := installed01(t, vs)
		elsewhere, moved := t.TempDir(), p.Dir+".moved"
		r.releases = append([]toolRelease{signedTool(t, vs, "0.2.0", studioTar(t, "0.2.0", ""), "")}, r.releases...)
		hookShareOpen = func() {
			os.Rename(p.Dir, moved)
			os.Symlink(elsewhere, p.Dir)
		}
		t.Cleanup(func() { hookShareOpen = nil })
		link := linked(t, p)
		_, err := c.InstallTool(ctx, tool, p, "0.5.0", false)
		if err == nil || !strings.HasPrefix(err.Error(), "jhon-studio 0.2.0 is at ") || !strings.HasSuffix(err.Error(), " but not linked: "+p.Dir+" became a link to "+elsewhere+" during the install; it was left as it is") {
			t.Errorf("error = %v", err)
		}
		if got := names(t, elsewhere); got != "" {
			t.Errorf("written through the link: %s", got)
		}
		if linked(t, p) != link {
			t.Errorf("link moved to %s", linked(t, p))
		}
	})
}

// L18 A3: a version folder already there is linked only when it is the
// verified release, a missing active folder is installed again, and a
// changed one is reported, never called up to date; a roll-back links only
// a folder that matches what jhon installed.
func TestInstallToolLinksOnlyVerifiedFolders(t *testing.T) {
	tool := studioTool(t)
	vs := virtual(t)
	ctx := context.Background()
	write := func(t *testing.T, dir string, files map[string]string, mode map[string]os.FileMode) {
		t.Helper()
		for n, body := range files {
			if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(n)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, n), []byte(body), mode[n]|0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	release02 := map[string]string{"bin/jhon-studio": studioScript("0.2.0", ""), "plugin/README.md": "plugin 0.2.0\n"}
	exec := map[string]os.FileMode{"bin/jhon-studio": 0o755}
	t.Run("a folder that differs from the release is refused", func(t *testing.T) {
		r, c, p := installed01(t, vs)
		r.releases = append([]toolRelease{signedTool(t, vs, "0.2.0", studioTar(t, "0.2.0", ""), "")}, r.releases...)
		write(t, filepath.Join(p.Dir, "0.2.0"), map[string]string{"bin/jhon-studio": "#!/bin/sh\necho evil\n", "plugin/README.md": "plugin 0.2.0\n"}, exec)
		link := linked(t, p)
		_, err := c.InstallTool(ctx, tool, p, "0.5.0", false)
		if err == nil || err.Error() != filepath.Join(p.Dir, "0.2.0")+" is not jhon-studio's signed release 0.2.0 (its files differ); move it away for jhon to install it; nothing was changed" {
			t.Errorf("error = %v", err)
		}
		if linked(t, p) != link {
			t.Errorf("linked %s", linked(t, p))
		}
	})
	t.Run("a folder that is the release is linked and recorded", func(t *testing.T) {
		r, c, p := installed01(t, vs)
		r.releases = append([]toolRelease{signedTool(t, vs, "0.2.0", studioTar(t, "0.2.0", ""), "")}, r.releases...)
		write(t, filepath.Join(p.Dir, "0.2.0"), release02, exec)
		res, err := c.InstallTool(ctx, tool, p, "0.5.0", false)
		if err != nil || res.Status != ToolUpdated || res.To != "0.2.0" {
			t.Errorf("result = %+v, %v", res, err)
		}
		if in := tool.Installed(p); in.Active != "0.2.0" || !in.Verified {
			t.Errorf("Installed = %+v", in)
		}
	})
	t.Run("a missing active folder is installed again", func(t *testing.T) {
		_, c, p := installed01(t, vs)
		if err := os.RemoveAll(filepath.Join(p.Dir, "0.1.0")); err != nil {
			t.Fatal(err)
		}
		if in := tool.Installed(p); in.Active != "0.1.0" || in.Verified {
			t.Errorf("Installed = %+v", in)
		}
		if res, err := c.InstallTool(ctx, tool, p, "0.5.0", true); err != nil || res.Status != ToolAvailable || res.From != "0.1.0" || res.To != "0.1.0" {
			t.Errorf("check = %+v, %v", res, err)
		}
		res, err := c.InstallTool(ctx, tool, p, "0.5.0", false)
		if err != nil || res.Status != ToolRepaired || res.From != "0.1.0" || res.To != "0.1.0" {
			t.Errorf("result = %+v, %v", res, err)
		}
		if in := tool.Installed(p); in.Active != "0.1.0" || !in.Verified {
			t.Errorf("Installed = %+v", in)
		}
	})
	t.Run("a changed active folder is reported, not up to date", func(t *testing.T) {
		_, c, p := installed01(t, vs)
		write(t, filepath.Join(p.Dir, "0.1.0"), map[string]string{"plugin/README.md": "changed\n"}, nil)
		_, err := c.InstallTool(ctx, tool, p, "0.5.0", false)
		if err == nil || err.Error() != filepath.Join(p.Dir, "0.1.0")+" is not jhon-studio's signed release 0.1.0 (its files differ); move it away for jhon to install it; nothing was changed" {
			t.Errorf("error = %v", err)
		}
	})
	t.Run("a roll-back to a changed folder is refused", func(t *testing.T) {
		r, c, p := installed01(t, vs)
		r.releases = append([]toolRelease{signedTool(t, vs, "0.2.0", studioTar(t, "0.2.0", ""), "")}, r.releases...)
		if _, err := c.InstallTool(ctx, tool, p, "0.5.0", false); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(p.Dir, "0.1.0"), map[string]string{"plugin/new": "x"}, nil)
		link := linked(t, p)
		_, _, err := RollbackTool(tool, p)
		if err == nil || err.Error() != filepath.Join(p.Dir, "0.1.0")+" is not the jhon-studio 0.1.0 jhon installed (its files differ, or jhon did not install it); nothing was changed" {
			t.Errorf("error = %v", err)
		}
		if linked(t, p) != link {
			t.Errorf("linked %s", linked(t, p))
		}
	})
}

// Nothing unverified is installed: a missing or failing checksum or
// signature, a tarball holding a path out of its folder or a link, or one
// whose program answers another version, refuses with one message and
// changes nothing.
func TestInstallToolRefusesWhatDoesNotVerify(t *testing.T) {
	tool := studioTool(t)
	vs := virtual(t)
	const n = "jhon-studio-0.2.0.tar.gz"
	good := func() toolRelease { return signedTool(t, vs, "0.2.0", studioTar(t, "0.2.0", ""), "") }
	withTar := func(entries []entry) toolRelease {
		return signedTool(t, vs, "0.2.0", pack(t, entries), "")
	}
	script := entry{name: "bin/jhon-studio", body: studioScript("0.2.0", ""), mode: 0o755}
	for _, c := range []struct {
		name string
		rel  func() toolRelease
		want string
	}{
		{"a checksum that does not match", func() toolRelease {
			r := good()
			r.assets[n+".sha256"] = []byte(strings.Repeat("0", 64) + "  " + n + "\n")
			return r
		}, "^checksum mismatch for " + regexp.QuoteMeta(n) + "; nothing was changed$"},
		{"no checksum", func() toolRelease {
			r := good()
			delete(r.assets, n+".sha256")
			return r
		}, "^jhon-studio 0\\.2\\.0's release has no " + regexp.QuoteMeta(n) + "\\.sha256; nothing was changed$"},
		{"no signature", func() toolRelease {
			r := good()
			delete(r.assets, n+".sigstore.json")
			return r
		}, "^jhon-studio 0\\.2\\.0's release has no " + regexp.QuoteMeta(n) + "\\.sigstore\\.json; nothing was changed$"},
		{"a signature over other bytes", func() toolRelease {
			r := good()
			r.assets[n+".sigstore.json"] = signBundle(t, vs, studioWorkflow("v0.2.0"), Issuer, []byte("another tarball"))
			return r
		}, "^the signature of " + regexp.QuoteMeta(n) + " does not verify: .*; nothing was changed$"},
		{"jhon-agent's signer", func() toolRelease {
			return signedTool(t, vs, "0.2.0", studioTar(t, "0.2.0", ""), "https://github.com/jachavarro2004/jhon-agent/.github/workflows/release.yml@refs/tags/v0.2.0")
		}, "^" + regexp.QuoteMeta(n) + " is signed by .*, not by the release workflow; nothing was changed$"},
		{"the signature of an older tag", func() toolRelease {
			return signedTool(t, vs, "0.2.0", studioTar(t, "0.2.0", ""), studioWorkflow("v0.1.0"))
		}, "^" + regexp.QuoteMeta(n) + " is signed by .*v0\\.1\\.0 .*, not by the release workflow; nothing was changed$"},
		{"an absolute path", func() toolRelease {
			return withTar([]entry{script, {name: "/tmp/evil", body: "x"}})
		}, "^" + regexp.QuoteMeta(n) + " holds /tmp/evil, a path out of its folder; nothing was changed$"},
		{"a .. path", func() toolRelease {
			return withTar([]entry{script, {name: "bin/../../evil", body: "x"}})
		}, "^" + regexp.QuoteMeta(n) + " holds bin/\\.\\./\\.\\./evil, a path out of its folder; nothing was changed$"},
		{"a symlink out of its folder", func() toolRelease {
			return withTar([]entry{script, {name: "plugin", kind: tar.TypeSymlink, link: "/etc"}})
		}, "^" + regexp.QuoteMeta(n) + " holds plugin, a link; nothing was changed$"},
		{"a symlink within its folder", func() toolRelease {
			return withTar([]entry{script, {name: "jhon-studio", kind: tar.TypeSymlink, link: "bin/jhon-studio"}})
		}, "^" + regexp.QuoteMeta(n) + " holds jhon-studio, a link; nothing was changed$"},
		{"a hard link", func() toolRelease {
			return withTar([]entry{script, {name: "x", kind: tar.TypeLink, link: "bin/jhon-studio"}})
		}, "^" + regexp.QuoteMeta(n) + " holds x, a link; nothing was changed$"},
		{"a device", func() toolRelease {
			return withTar([]entry{script, {name: "dev", kind: tar.TypeChar}})
		}, "^" + regexp.QuoteMeta(n) + " holds dev, neither a file nor a folder; nothing was changed$"},
		{"a name given twice", func() toolRelease {
			return withTar([]entry{script, script})
		}, "^" + regexp.QuoteMeta(n) + " holds bin/jhon-studio twice; nothing was changed$"},
		{"no program", func() toolRelease {
			return withTar([]entry{{name: "README.md", body: "x"}})
		}, "^" + regexp.QuoteMeta(n) + " holds no bin/jhon-studio; nothing was changed$"},
		{"a program that answers another version", func() toolRelease {
			return withTar([]entry{{name: "bin/jhon-studio", body: studioScript("0.1.0", ""), mode: 0o755}})
		}, "^" + regexp.QuoteMeta(n) + " holds jhon-studio 0\\.1\\.0, not the release's 0\\.2\\.0; nothing was changed$"},
		{"a program that does not answer", func() toolRelease {
			return withTar([]entry{{name: "bin/jhon-studio", body: "#!/bin/sh\nexit 3\n", mode: 0o755}})
		}, "^cannot read what jhon jhon-studio 0\\.2\\.0 requires: jhon-studio exited 3; nothing was changed$"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := &repo{}
			cl := r.serve(t, vs)
			r.releases = []toolRelease{c.rel(), signedTool(t, vs, "0.1.0", studioTar(t, "0.1.0", ""), "")}
			p := home(t, tool)
			before := tree(t, p)
			_, err := cl.InstallTool(context.Background(), tool, p, "0.5.0", false)
			if err == nil || !regexp.MustCompile(c.want).MatchString(err.Error()) {
				t.Fatalf("error = %v, want %s", err, c.want)
			}
			if after := tree(t, p); !mapsEqual(before, after) {
				t.Errorf("changed:\n%v\n%v", before, after)
			}
		})
	}
}

func TestNewToolsIsHTTPSOnlyUnlessOverridden(t *testing.T) {
	t.Setenv("JHON_TOOL_RELEASES_URL", "")
	os.Unsetenv("JHON_TOOL_RELEASES_URL")
	if c := NewTools(); c.URL != ToolAPI || !c.HTTPSOnly || ToolAPI != "https://api.github.com" {
		t.Fatalf("NewTools = %q, https only %v", c.URL, c.HTTPSOnly)
	}
	t.Setenv("JHON_TOOL_RELEASES_URL", "http://127.0.0.1:1")
	if c := NewTools(); c.HTTPSOnly || c.URL != "http://127.0.0.1:1" {
		t.Fatalf("overridden: %q, https only %v", c.URL, c.HTTPSOnly)
	}
}
