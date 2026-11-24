package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/version"
)

// A cli integration's program (jhon-studio) installs from its own signed
// GitHub releases (T242, odd/tasks/studio-integration-v1.md S5), as jhon's
// own do: `jhon setup` with the integration on, and `jhon update`, take
// the newest release whose requires_jhon the running jhon satisfies
// (integrations.Judge on the unpacked program's `integration --json`),
// verify its sha256 and its cosign keyless bundle against the repository's
// release workflow at the release's tag (VerifyAs), unpack it into
// ~/.local/share/<binary>/<version> and link ~/.local/bin/<binary> to
// <version>/bin/<binary> atomically (release.Activate). The versions
// installed before stay: RollbackTool links the previous one again.
//
// Where the releases are comes from the manifest's doctor.source,
// github.com/<owner>/<repo> (integrations.Manifest.ReleaseRepo); the asset
// names and the signer are this file's one convention. Pre-releases are
// taken only by a jhon that is itself a pre-release or a development
// build. A development clone (a git checkout at ~/.local/share/<binary>)
// is left alone, and a ~/.local/bin/<binary> jhon did not link is never
// replaced. Every refusal before the link moves leaves the disk as it was.

// ToolAPI is where a tool's releases are read: GitHub's API;
// JHON_TOOL_RELEASES_URL replaces it (the tests' server).
const ToolAPI = "https://api.github.com"

// toolVersion is a tool release's version: the tag without its v, X.Y.Z or
// X.Y.Z-rc.N with no leading zero, as the release workflow accepts.
var toolVersion = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-rc\.(0|[1-9]\d*))?$`)

// The most a tool's release may unpack to, and how many entries; the most
// releases one install downloads to find one that supports this jhon.
var (
	maxUnpacked   int64 = 1 << 30
	maxEntries          = 20000
	maxCandidates       = 10
)

// Tool is a cli integration whose program jhon installs from its releases.
type Tool struct {
	Manifest            integrations.Manifest
	Binary, Owner, Repo string
}

// ToolOf is m's release source; false when its program is not released on
// GitHub as jhon reads it (integrations.Manifest.ReleaseRepo).
func ToolOf(m integrations.Manifest) (Tool, bool) {
	owner, repo, ok := m.ReleaseRepo()
	if !ok {
		return Tool{}, false
	}
	return Tool{Manifest: m, Binary: m.Doctor.Binary, Owner: owner, Repo: repo}, true
}

// Source is the repository, github.com/<owner>/<repo>.
func (t Tool) Source() string { return "github.com/" + t.Owner + "/" + t.Repo }

// Asset is version's tarball: <binary>-<version>.tar.gz, with its .sha256
// and .sigstore.json beside it.
func (t Tool) Asset(version string) string { return t.Binary + "-" + version + ".tar.gz" }

// SignerAt is who signs version: the repository's release workflow at the
// tag v<version>, exactly, through GitHub's OIDC issuer.
func (t Tool) SignerAt(version string) Signer {
	return Signer{Issuer, `^https://github\.com/` + regexp.QuoteMeta(t.Owner) + `/` + regexp.QuoteMeta(t.Repo) +
		`/\.github/workflows/release\.yml@refs/tags/v` + regexp.QuoteMeta(version) + `$`}
}

// Place is where a tool lives: Dir holds a folder per version
// (~/.local/share/<binary>), Link is the command (~/.local/bin/<binary>).
type Place struct{ Dir, Link string }

// PlaceIn is t's place under home.
func (t Tool) PlaceIn(home string) Place {
	return Place{Dir: filepath.Join(home, ".local", "share", t.Binary), Link: filepath.Join(home, ".local", "bin", t.Binary)}
}

// program is a version's command under p.
func (t Tool) program(p Place, version string) string {
	return filepath.Join(p.Dir, version, "bin", t.Binary)
}

// Installed is what is on disk: a development clone; the version the link
// points at; what the link is when jhon did not make it; and the versions
// kept, oldest first.
type Installed struct {
	Dev     bool
	Active  string
	Foreign string
	Kept    []string
}

// Installed reads t's place p.
func (t Tool) Installed(p Place) Installed {
	var in Installed
	if _, err := os.Lstat(filepath.Join(p.Dir, ".git")); err == nil {
		in.Dev = true
		return in
	}
	if es, err := os.ReadDir(p.Dir); err == nil {
		for _, e := range es {
			if toolVersion.MatchString(e.Name()) && release.IsFile(t.program(p, e.Name())) {
				in.Kept = append(in.Kept, e.Name())
			}
		}
	}
	slices.SortFunc(in.Kept, func(a, b string) int { return semver.Compare("v"+a, "v"+b) })
	fi, err := os.Lstat(p.Link)
	switch {
	case err != nil:
	case fi.Mode()&os.ModeSymlink == 0:
		in.Foreign = "a file"
	default:
		target, _ := os.Readlink(p.Link)
		abs := target
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(filepath.Dir(p.Link), abs)
		}
		v := filepath.Base(filepath.Dir(filepath.Dir(abs)))
		if toolVersion.MatchString(v) && filepath.Clean(abs) == t.program(p, v) {
			in.Active = v
		} else {
			in.Foreign = "a link to " + target
		}
	}
	return in
}

// ToolStatus is what InstallTool did or found.
type ToolStatus string

const (
	// ToolDevelopment is a development clone, left alone.
	ToolDevelopment ToolStatus = "development"
	// ToolUpToDate is no newer release that supports this jhon.
	ToolUpToDate ToolStatus = "up-to-date"
	// ToolAvailable is a newer one found by a check, not installed.
	ToolAvailable ToolStatus = "available"
	// ToolInstalled is a first install; ToolUpdated one beside an older.
	ToolInstalled ToolStatus = "installed"
	ToolUpdated   ToolStatus = "updated"
)

// ToolResult is InstallTool's answer: From the active version before ("" for
// none), To the one active after (or, for a check, the one available),
// Signer who signed it, Skipped the notes of the newer releases that do not
// support this jhon.
type ToolResult struct {
	Status           ToolStatus
	From, To, Signer string
	Skipped          []string
}

// ToolRelease is one release of a tool: its version, whether it is a
// pre-release, and its assets (an empty one is not published).
type ToolRelease struct {
	Version              string
	Pre                  bool
	Tarball, Sum, Bundle Asset
}

// NewTools is the client of a tool's releases: GitHub's API, https only,
// the public-good trust; JHON_TOOL_RELEASES_URL (a base URL standing for
// GitHub's API) lifts https only, as JHON_RELEASES_URL does for jhon's.
func NewTools() *Client {
	base := os.Getenv("JHON_TOOL_RELEASES_URL")
	c := &Client{URL: base, HTTP: &http.Client{Timeout: 2 * time.Minute}, Current: version.Version, trust: publicGood}
	if base == "" {
		c.URL, c.HTTPSOnly = ToolAPI, true
		c.HTTP.CheckRedirect = httpsRedirects
	}
	if testTrust != nil {
		c.trust = testTrust
	}
	return c
}

// ToolReleases are t's published releases, newest first: drafts and tags
// that are not vX.Y.Z or vX.Y.Z-rc.N are left out, and pre-releases
// unless pre.
func (c *Client) ToolReleases(ctx context.Context, t Tool, pre bool) ([]ToolRelease, error) {
	u := strings.TrimRight(c.URL, "/") + "/repos/" + t.Owner + "/" + t.Repo + "/releases?per_page=30"
	var buf bytes.Buffer
	if err := c.get(ctx, u, u, &buf, maxAPI); err != nil {
		return nil, err
	}
	var gh []struct {
		Tag    string `json:"tag_name"`
		Draft  bool   `json:"draft"`
		Pre    bool   `json:"prerelease"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(buf.Bytes(), &gh); err != nil {
		return nil, fmt.Errorf("the releases of %s are not GitHub's list of releases: %v", t.Source(), err)
	}
	var out []ToolRelease
	for _, r := range gh {
		v, ok := strings.CutPrefix(r.Tag, "v")
		if !ok || r.Draft || !toolVersion.MatchString(v) {
			continue
		}
		rel := ToolRelease{Version: v, Pre: r.Pre || semver.Prerelease("v"+v) != ""}
		if rel.Pre && !pre {
			continue
		}
		for _, a := range r.Assets {
			switch a.Name {
			case t.Asset(v):
				rel.Tarball = Asset{a.Name, a.URL}
			case t.Asset(v) + ".sha256":
				rel.Sum = Asset{a.Name, a.URL}
			case t.Asset(v) + ".sigstore.json":
				rel.Bundle = Asset{a.Name, a.URL}
			}
		}
		out = append(out, rel)
	}
	slices.SortStableFunc(out, func(a, b ToolRelease) int { return semver.Compare("v"+b.Version, "v"+a.Version) })
	return slices.CompactFunc(out, func(a, b ToolRelease) bool { return a.Version == b.Version }), nil
}

// FetchTool downloads r's tarball, checksum and bundle into dir, verifies
// the sha256, then the signature against t's release workflow at r's tag,
// and returns the tarball's path and who signed it.
func (c *Client) FetchTool(ctx context.Context, t Tool, r ToolRelease, dir string) (artifact, signer string, err error) {
	name := t.Asset(r.Version)
	for _, a := range []struct {
		Asset
		want  string
		limit int64
	}{{r.Tarball, name, maxTarball}, {r.Sum, name + ".sha256", maxSum}, {r.Bundle, name + ".sigstore.json", maxBundle}} {
		if a.URL == "" {
			return "", "", fmt.Errorf("%s %s's release has no %s", t.Binary, r.Version, a.want)
		}
		if err := c.download(ctx, Asset{a.want, a.URL}, dir, a.limit); err != nil {
			return "", "", err
		}
	}
	artifact = filepath.Join(dir, name)
	if err := checkSum(artifact, artifact+".sha256"); err != nil {
		return "", "", err
	}
	trust, err := c.trust()
	if err != nil {
		return "", "", fmt.Errorf("cannot load Sigstore's trusted root: %v", err)
	}
	if signer, err = VerifyAs(artifact, artifact+".sigstore.json", trust, t.SignerAt(r.Version)); err != nil {
		return "", "", err
	}
	return artifact, signer, nil
}

// unchanged is a refusal before anything was placed.
func unchanged(err error) error { return fmt.Errorf("%w; nothing was changed", err) }

// InstallTool installs, at p, t's newest release that supports jhon and is
// newer than the active one; with check, it only says which one that is.
// A development clone is left alone (ToolDevelopment) without asking the
// network. A link jhon did not make, a release that does not verify, a
// tarball holding a path out of its folder or a link, and a program that
// does not answer as its release, refuse it, saying so and that nothing
// was changed. The newest release that verifies but needs another jhon is
// passed over for the next older, at most maxCandidates downloads.
func (c *Client) InstallTool(ctx context.Context, t Tool, p Place, jhon string, check bool) (ToolResult, error) {
	in := t.Installed(p)
	res := ToolResult{From: in.Active}
	if in.Dev {
		res.Status = ToolDevelopment
		return res, nil
	}
	if in.Foreign != "" {
		return res, fmt.Errorf("%s is %s, not a link to a %s release jhon installed; move it away for jhon to install one; nothing was changed", release.Tilde(p.Link), in.Foreign, t.Binary)
	}
	rels, err := c.ToolReleases(ctx, t, semver.Prerelease("v"+jhon) != "")
	if err != nil {
		return res, unchanged(err)
	}
	rels = slices.DeleteFunc(rels, func(r ToolRelease) bool {
		return in.Active != "" && semver.Compare("v"+r.Version, "v"+in.Active) <= 0
	})
	if len(rels) > maxCandidates {
		rels = rels[:maxCandidates]
	}
	dir, err := os.MkdirTemp("", "jhon-tool-")
	if err != nil {
		return res, unchanged(err)
	}
	defer os.RemoveAll(dir)
	var newest integrations.Answer
	for i, r := range rels {
		cand := filepath.Join(dir, fmt.Sprintf("r%d", i))
		look := filepath.Join(cand, "unpacked")
		if err := os.MkdirAll(look, 0o700); err != nil {
			return res, unchanged(err)
		}
		artifact, signer, err := c.FetchTool(ctx, t, r, cand)
		if err != nil {
			return res, unchanged(err)
		}
		if err := t.unpack(artifact, look); err != nil {
			return res, unchanged(err)
		}
		a, err := integrations.AskIntegrationAt(t.Manifest, filepath.Join(look, "bin", t.Binary))
		if err != nil {
			return res, unchanged(fmt.Errorf("cannot read what jhon %s %s requires: %v", t.Binary, r.Version, err))
		}
		if a.Version != r.Version {
			return res, unchanged(fmt.Errorf("%s holds %s %s, not the release's %s", t.Asset(r.Version), t.Binary, a.Version, r.Version))
		}
		if fit := integrations.Judge(t.Manifest, a, jhon); fit.Off != "" {
			if len(res.Skipped) == 0 {
				newest = a
			}
			res.Skipped = append(res.Skipped, fit.Off)
			continue
		}
		res.To, res.Signer = r.Version, signer
		if check {
			res.Status = ToolAvailable
			return res, nil
		}
		if err := t.place(p, artifact, r.Version); err != nil {
			return res, err
		}
		res.Status = ToolInstalled
		if in.Active != "" {
			res.Status = ToolUpdated
		}
		return res, nil
	}
	if in.Active == "" {
		switch {
		case len(res.Skipped) > 0 && newest.Contract != "" && newest.Contract != integrations.Contract:
			return res, fmt.Errorf("no %s release supports jhon %s (the newest, %s, speaks %s); nothing was changed", t.Binary, jhon, newest.Version, newest.Contract)
		case len(res.Skipped) > 0:
			return res, fmt.Errorf("no %s release supports jhon %s (the newest, %s, requires jhon %s); nothing was changed", t.Binary, jhon, newest.Version, newest.RequiresJhon)
		}
		return res, fmt.Errorf("%s publishes no %s release jhon %s can install; nothing was changed", t.Source(), t.Binary, jhon)
	}
	res.Status, res.To = ToolUpToDate, in.Active
	return res, nil
}

// place unpacks the verified artifact into p.Dir/<version>, through a
// folder beside it renamed into place (a version already there is kept as
// it is), and links p.Link to its program atomically.
func (t Tool) place(p Place, artifact, version string) error {
	if err := os.MkdirAll(p.Dir, 0o755); err != nil {
		return unchanged(err)
	}
	target := filepath.Join(p.Dir, version)
	if !release.IsFile(t.program(p, version)) {
		stage, err := os.MkdirTemp(p.Dir, ".unpacking-")
		if err != nil {
			return unchanged(err)
		}
		if err := t.unpack(artifact, stage); err != nil {
			os.RemoveAll(stage)
			return unchanged(err)
		}
		if err := os.Chmod(stage, 0o755); err != nil {
			os.RemoveAll(stage)
			return unchanged(err)
		}
		if err := os.Rename(stage, target); err != nil {
			os.RemoveAll(stage)
			return unchanged(fmt.Errorf("cannot place %s %s at %s: %v", t.Binary, version, release.Tilde(target), err))
		}
	}
	if err := os.MkdirAll(filepath.Dir(p.Link), 0o755); err != nil {
		return fmt.Errorf("%s %s is at %s, but %v; the command still runs the version it ran", t.Binary, version, release.Tilde(target), err)
	}
	if err := release.Activate(t.program(p, version), p.Link); err != nil {
		return fmt.Errorf("%s %s is at %s, but cannot be linked from %s: %v; the command still runs the version it ran", t.Binary, version, release.Tilde(target), release.Tilde(p.Link), err)
	}
	return nil
}

// unpack writes the gzip tarball artifact into dir, an empty folder: only
// folders and regular files, each named by a relative path with no `..`,
// at most maxEntries of them and maxUnpacked bytes. A link (symbolic or
// hard), a device, a name out of the folder or given twice is refused, and
// so is a tarball with no bin/<binary>. A file keeps only its owner's
// execute bit's meaning: 0755 or 0644.
func (t Tool) unpack(artifact, dir string) error {
	name := filepath.Base(artifact)
	f, err := os.Open(artifact)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%s is not a gzip tarball: %v", name, err)
	}
	tr := tar.NewReader(gz)
	var total int64
	for n := 0; ; n++ {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("%s is not a tarball: %v", name, err)
		}
		if n >= maxEntries {
			return fmt.Errorf("%s holds more than %d entries", name, maxEntries)
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		clean := path.Clean(h.Name)
		if h.Name == "" || strings.HasPrefix(h.Name, "/") || strings.Contains(h.Name, `\`) || slices.Contains(strings.Split(h.Name, "/"), "..") || clean == ".." {
			return fmt.Errorf("%s holds %s, a path out of its folder", name, h.Name)
		}
		to := filepath.Join(dir, filepath.FromSlash(clean))
		switch h.Typeflag {
		case tar.TypeDir:
			if clean != "." {
				if err := os.MkdirAll(to, 0o755); err != nil {
					return err
				}
			}
		case tar.TypeReg:
			if clean == "." {
				return fmt.Errorf("%s holds %s, a path out of its folder", name, h.Name)
			}
			total += h.Size
			if total > maxUnpacked {
				return fmt.Errorf("%s unpacks to more than %d bytes", name, maxUnpacked)
			}
			if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if h.Mode&0o100 != 0 {
				mode = 0o755
			}
			out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			if errors.Is(err, os.ErrExist) {
				return fmt.Errorf("%s holds %s twice", name, h.Name)
			}
			if err != nil {
				return err
			}
			_, err = io.Copy(out, io.LimitReader(tr, h.Size))
			if cerr := out.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("%s holds %s, a link", name, h.Name)
		default:
			return fmt.Errorf("%s holds %s, neither a file nor a folder", name, h.Name)
		}
	}
	if !release.IsFile(filepath.Join(dir, "bin", t.Binary)) {
		return fmt.Errorf("%s holds no bin/%s", name, t.Binary)
	}
	return nil
}

// ErrNoOlderTool is RollbackTool's answer when no version older than the
// active one is kept; ErrDevelopmentTool when the tool is a development
// clone, which is left alone.
var (
	ErrNoOlderTool     = errors.New("no older version is kept")
	ErrDevelopmentTool = errors.New("a development clone, left alone")
)

// RollbackTool links t's previous version at p again: of the versions kept,
// the newest older than the active one. Nothing is removed.
func RollbackTool(t Tool, p Place) (from, to string, err error) {
	in := t.Installed(p)
	switch {
	case in.Dev:
		return "", "", ErrDevelopmentTool
	case in.Foreign != "":
		return "", "", fmt.Errorf("%s is %s, not a link to a %s release jhon installed; nothing was changed", release.Tilde(p.Link), in.Foreign, t.Binary)
	case in.Active == "":
		return "", "", ErrNoOlderTool
	}
	for _, v := range slices.Backward(in.Kept) {
		if semver.Compare("v"+v, "v"+in.Active) < 0 {
			if err := release.Activate(t.program(p, v), p.Link); err != nil {
				return in.Active, "", fmt.Errorf("cannot link %s to %s %s: %v; nothing was changed", release.Tilde(p.Link), t.Binary, v, err)
			}
			return in.Active, v, nil
		}
	}
	return in.Active, "", ErrNoOlderTool
}
