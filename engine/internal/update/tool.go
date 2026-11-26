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
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
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
	want := "https://github.com/" + t.Owner + "/" + t.Repo + "/.github/workflows/release.yml@refs/tags/v" + version
	return Signer{Issuer: Issuer, Identity: "^" + regexp.QuoteMeta(want) + "$", Want: want}
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

// Installed is what is on disk: a development clone; why jhon leaves the
// place alone (Foreign: the share folder is not a folder, or the link is
// not jhon's); the version the link points at, and whether its folder is
// the one jhon verified and recorded there (Verified); and the versions
// kept, oldest first.
type Installed struct {
	Dev      bool
	Foreign  string
	Active   string
	Verified bool
	Kept     []string
}

// Installed reads t's place p, never through a link at p.Dir.
func (t Tool) Installed(p Place) Installed {
	var in Installed
	if why := t.shareNotOurs(p); why != "" {
		in.Foreign = why
		return in
	}
	if _, err := os.Lstat(filepath.Join(p.Dir, ".git")); err == nil {
		in.Dev = true
		return in
	}
	if es, err := os.ReadDir(p.Dir); err == nil {
		for _, e := range es {
			if e.IsDir() && toolVersion.MatchString(e.Name()) && release.IsFile(t.program(p, e.Name())) {
				in.Kept = append(in.Kept, e.Name())
			}
		}
	}
	slices.SortFunc(in.Kept, func(a, b string) int { return semver.Compare("v"+a, "v"+b) })
	in.Active, in.Foreign = t.linkState(p)
	if in.Active != "" {
		in.Verified = t.verified(p, in.Active)
	}
	return in
}

// shareNotOurs says why p.Dir is not a folder jhon installs into: a link,
// or not a folder; "" when it is one, or is not there.
func (t Tool) shareNotOurs(p Place) string {
	fi, err := os.Lstat(p.Dir)
	switch {
	case err != nil:
		return ""
	case fi.Mode()&os.ModeSymlink != 0:
		target, _ := os.Readlink(p.Dir)
		return release.Tilde(p.Dir) + " is a link to " + target + ", not a folder"
	case !fi.IsDir():
		return release.Tilde(p.Dir) + " is a file, not a folder"
	}
	return ""
}

// linkState is the version p.Link points at when it is jhon's link to a
// version's program; else, when it is there, why it is not jhon's.
func (t Tool) linkState(p Place) (active, foreign string) {
	fi, err := os.Lstat(p.Link)
	switch {
	case err != nil:
		return "", ""
	case fi.Mode()&os.ModeSymlink == 0:
		return "", release.Tilde(p.Link) + " is a file, not jhon's link to a " + t.Binary + " release"
	}
	target, _ := os.Readlink(p.Link)
	if v, ok := t.ours(p, target); ok {
		return v, ""
	}
	return "", release.Tilde(p.Link) + " is a link to " + target + ", not jhon's link to a " + t.Binary + " release"
}

// ours is the version whose program target (a link's) names, when it names
// one under p.Dir.
func (t Tool) ours(p Place, target string) (string, bool) {
	abs := target
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(filepath.Dir(p.Link), abs)
	}
	v := filepath.Base(filepath.Dir(filepath.Dir(abs)))
	return v, toolVersion.MatchString(v) && filepath.Clean(abs) == t.program(p, v)
}

// verifiedDir, under a tool's share folder, records the digest of each
// version folder jhon placed there from a verified release
// (VerifiedRecord): a folder is linked, by an install or a roll-back, only
// while its files still have that digest (TreeDigest).
const verifiedDir = ".jhon-verified"

// VerifiedRecord is where version's digest is recorded under p.
func VerifiedRecord(p Place, version string) string {
	return filepath.Join(p.Dir, verifiedDir, version)
}

// verified is whether p.Dir/<version> is a folder whose digest is the one
// recorded for it.
func (t Tool) verified(p Place, version string) bool {
	fi, err := os.Lstat(filepath.Join(p.Dir, version))
	if err != nil || !fi.IsDir() {
		return false
	}
	rec, err := os.ReadFile(VerifiedRecord(p, version))
	if err != nil {
		return false
	}
	d, err := TreeDigest(filepath.Join(p.Dir, version))
	return err == nil && strings.TrimSpace(string(rec)) == d
}

// TreeDigest is the sha256 of a folder's tree: each entry's path in order,
// its kind, a file's owner execute bit and its content's sha256, so a file
// changed, added, removed, made executable or swapped for a link changes it;
// and the permission bits that give access beyond what jhon's unpack gives
// (T247, odd/tasks/phase1-security.md S4): of a folder (the top one too), a
// group's or others' write bit; of a file, a bit beyond rwxr-xr-x for a
// program or rw-r--r-- for another, setuid and setgid included. A folder's
// setgid is left out: Linux passes it from a setgid parent (a chmod g+s
// ~/.local/share, Debian's SETGID_HOME, a setgid TMPDIR) to every folder
// made inside, and it gives no access, only a group to what is made there;
// so are a folder's setuid (Linux ignores it) and sticky bit (it only takes
// away others' right to remove what is not theirs), and a file's sticky bit
// (ignored). An ordinary tree, whatever the umask took away or a parent
// passed down, has the digest T242 gave it.
func TreeDigest(dir string) (string, error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer r.Close()
	return treeDigest(r.FS(), ".")
}

func treeDigest(fsys fs.FS, dir string) (string, error) {
	h := sha256.New()
	err := fs.WalkDir(fsys, dir, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(name, dir), "/")
		if name == dir {
			fi, err := d.Info()
			if err != nil {
				return err
			}
			beyond(h, ".", fi.Mode()&folderBits)
			return nil
		}
		switch {
		case d.IsDir():
			fmt.Fprintf(h, "d %s\n", rel)
			fi, err := d.Info()
			if err != nil {
				return err
			}
			beyond(h, rel, fi.Mode()&folderBits)
		case d.Type().IsRegular():
			fi, err := d.Info()
			if err != nil {
				return err
			}
			f, err := fsys.Open(name)
			if err != nil {
				return err
			}
			fh := sha256.New()
			_, err = io.Copy(fh, f)
			f.Close()
			if err != nil {
				return err
			}
			exec := fi.Mode().Perm()&0o100 != 0
			fmt.Fprintf(h, "f %s %t %x\n", rel, exec, fh.Sum(nil))
			ordinary := fs.FileMode(0o644)
			if exec {
				ordinary = 0o755
			}
			beyond(h, rel, fi.Mode()&^(ordinary|fs.ModeSticky))
		default:
			fmt.Fprintf(h, "o %s %v\n", rel, d.Type())
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// folderBits are the bits of a folder's mode that count in its digest:
// its group's and others' write (TreeDigest).
const folderBits fs.FileMode = 0o022

// beyond adds to the digest h the bits of rel's mode that count (extra,
// TreeDigest), when there are any, as octal with setuid 4000 and setgid
// 2000.
func beyond(h io.Writer, rel string, extra fs.FileMode) {
	mode := extra.Perm()
	if extra&fs.ModeSetuid != 0 {
		mode |= 0o4000
	}
	if extra&fs.ModeSetgid != 0 {
		mode |= 0o2000
	}
	if mode != 0 {
		fmt.Fprintf(h, "m %s %o\n", rel, uint32(mode))
	}
}

// ToolStatus is what InstallTool did or found.
type ToolStatus string

const (
	// ToolDevelopment is a development clone, left alone.
	ToolDevelopment ToolStatus = "development"
	// ToolForeign is a place jhon did not make (ToolResult.Note says why),
	// left alone.
	ToolForeign ToolStatus = "foreign"
	// ToolRepaired is the active version installed again: its folder was
	// missing, or had no record of jhon's verification.
	ToolRepaired ToolStatus = "repaired"
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
// support this jhon, Note why a ToolForeign place is left alone.
type ToolResult struct {
	Status           ToolStatus
	From, To, Signer string
	Skipped          []string
	Note             string
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
	parent := lookDir(filepath.Dir(p.Dir))
	in := t.Installed(p)
	res := ToolResult{From: in.Active}
	if in.Dev {
		res.Status = ToolDevelopment
		return res, nil
	}
	if in.Foreign != "" {
		res.Status, res.Note = ToolForeign, in.Foreign
		return res, nil
	}
	rels, err := c.ToolReleases(ctx, t, semver.Prerelease("v"+jhon) != "")
	if err != nil {
		return res, unchanged(err)
	}
	// An active folder that is missing, or is not the one jhon verified,
	// is installed again from its release: its version is a candidate too.
	repair := in.Active != "" && !in.Verified
	rels = slices.DeleteFunc(rels, func(r ToolRelease) bool {
		n := semver.Compare("v"+r.Version, "v"+in.Active)
		return in.Active != "" && (n < 0 || n == 0 && !repair)
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
		want, err := t.unpackTo(artifact, look)
		if err != nil {
			return res, unchanged(err)
		}
		home := filepath.Join(cand, "home")
		if err := os.Mkdir(home, 0o700); err != nil {
			return res, unchanged(err)
		}
		a, err := integrations.AskUnpacked(t.Manifest, filepath.Join(look, "bin", t.Binary), home)
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
			// What the install would refuse, the check says (T247).
			if err := t.unlike(p, r.Version, want); err != nil {
				return res, err
			}
			res.Status = ToolAvailable
			return res, nil
		}
		if err := t.place(p, parent, artifact, r.Version, want); err != nil {
			return res, err
		}
		switch {
		case r.Version == in.Active:
			res.Status = ToolRepaired
		case in.Active != "":
			res.Status = ToolUpdated
		default:
			res.Status = ToolInstalled
		}
		return res, nil
	}
	if repair {
		return res, fmt.Errorf("%s %s at %s is missing or is not the release jhon installed, and no release of %s that supports jhon %s can take its place; nothing was changed",
			t.Binary, in.Active, release.Tilde(filepath.Join(p.Dir, in.Active)), t.Source(), jhon)
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

// Test hooks: hookShareOpen runs once the share folder is open,
// hookBeforeLink right before the link moves and hookBeforeSwapBack right
// before what was swapped out of the link's name is put back (link.go);
// nil outside tests.
var hookShareOpen, hookBeforeLink, hookBeforeSwapBack func()

// recordTemp is the i-th name version's record is written to before it is
// renamed over its own (writeRecord).
func recordTemp(version string, i int) string {
	return fmt.Sprintf(".%s.%d-%d.tmp", version, os.Getpid(), i)
}

// became is a place that changed during the install: left as it is.
func became(why string) string {
	w := strings.TrimSuffix(strings.Replace(why, " is ", " became ", 1), ", not a folder")
	if strings.Contains(w, ", not ") {
		w += ","
	}
	return w + " during the install; it was left as it is"
}

// dirAt is what a folder's path was when an install began (lookDir): the
// folder it named, nil when nothing was there, and the link's target when
// it was a link.
type dirAt struct {
	fi   os.FileInfo
	link string
}

func lookDir(path string) dirAt {
	var d dirAt
	if li, err := os.Lstat(path); err == nil && li.Mode()&os.ModeSymlink != 0 {
		d.link, _ = os.Readlink(path)
	}
	d.fi, _ = os.Stat(path)
	return d
}

// swapped says why path is no longer what d saw (T247): a link it was not,
// or not the same folder; "" while it is, or while it is still absent or
// became a real folder where nothing was.
func (d dirAt) swapped(path string) string {
	li, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist) && d.fi == nil:
		return ""
	case err != nil:
		return release.Tilde(path) + " is gone"
	case li.Mode()&os.ModeSymlink != 0:
		if target, _ := os.Readlink(path); d.fi == nil || d.link != target {
			return release.Tilde(path) + " is a link to " + target + ", not a folder"
		}
	case !li.IsDir():
		return release.Tilde(path) + " is a file, not a folder"
	case d.link != "":
		return release.Tilde(path) + " is another folder"
	}
	if d.fi != nil {
		if fi, err := os.Stat(path); err != nil || !os.SameFile(fi, d.fi) {
			return release.Tilde(path) + " is another folder"
		}
	}
	return ""
}

// differs is the refusal of a folder at p.Dir/<version> that is not the
// signed release.
func (t Tool) differs(p Place, version string) error {
	return fmt.Errorf("%s is not %s's signed release %s (its files differ); move it away for jhon to install it; nothing was changed", release.Tilde(filepath.Join(p.Dir, version)), t.Binary, version)
}

// unlike is the refusal place would give a version folder already at
// p.Dir/<version> that is not the release whose digest is want; nil when
// there is none, or it is that release.
func (t Tool) unlike(p Place, version, want string) error {
	r, err := os.OpenRoot(p.Dir)
	if err != nil {
		return nil
	}
	defer r.Close()
	fi, err := r.Lstat(version)
	if err != nil {
		return nil
	}
	if d, err := treeDigest(r.FS(), version); !fi.IsDir() || err != nil || d != want {
		return t.differs(p, version)
	}
	return nil
}

// place puts the verified artifact, whose tree's digest is want, at
// p.Dir/<version> and links p.Link to its program. Right before it writes,
// p.Dir's parent (~/.local/share) must be what it was when the install
// began (parent): never a link swapped in since, nor another folder; p.Dir
// must be a folder (made when missing, never through a link) that is not a
// clone, and p.Link absent or jhon's link: anything else, even one swapped
// in during the install, is left as it is and refuses it. It writes
// through the folders it opened (os.Root), never through a link: a new
// version is unpacked beside, renamed into place and recorded
// (VerifiedRecord, writeRecord); one already there is linked only when its
// digest is want. The link moves atomically, and never over what is not
// jhon's (link.go). A refusal leaves no p.Dir it made (T247).
func (t Tool) place(p Place, parent dirAt, artifact, version, want string) (err error) {
	up, name := filepath.Dir(p.Dir), filepath.Base(p.Dir)
	if why := parent.swapped(up); why != "" {
		return errors.New(became(why) + "; nothing was changed")
	}
	if why := t.shareNotOurs(p); why != "" {
		return errors.New(became(why) + "; nothing was changed")
	}
	if _, foreign := t.linkState(p); foreign != "" {
		return errors.New(became(foreign) + "; nothing was changed")
	}
	if err := os.MkdirAll(up, 0o755); err != nil {
		return unchanged(err)
	}
	pr, err := os.OpenRoot(up)
	if err != nil {
		return unchanged(err)
	}
	defer pr.Close()
	if why := parent.notOpened(up, pr); why != "" {
		return errors.New(became(why) + "; nothing was changed")
	}
	made := false
	if err := pr.Mkdir(name, 0o755); err == nil {
		made = true
	} else if !errors.Is(err, os.ErrExist) {
		return unchanged(err)
	}
	r, err := pr.OpenRoot(name)
	if err != nil {
		return unchanged(err)
	}
	defer r.Close()
	if why := t.notOpened(p, parent, r); why != "" {
		return errors.New(became(why) + "; nothing was changed")
	}
	if made {
		// A refusal from here leaves no empty folder it made: rmdir
		// removes neither a link nor a folder that holds anything.
		defer func() {
			if err != nil && t.notOpened(p, parent, r) == "" {
				syscall.Rmdir(p.Dir)
			}
		}()
	}
	if _, err := r.Lstat(".git"); err == nil {
		return fmt.Errorf("%s became a development clone during the install; it was left as it is; nothing was changed", release.Tilde(p.Dir))
	}
	if hookShareOpen != nil {
		hookShareOpen()
	}
	target := filepath.Join(p.Dir, version)
	if fi, err := r.Lstat(version); err == nil {
		if !fi.IsDir() {
			return t.differs(p, version)
		}
		if d, err := treeDigest(r.FS(), version); err != nil || d != want {
			return t.differs(p, version)
		}
	} else {
		stage := fmt.Sprintf(".unpacking-%d-%d", os.Getpid(), time.Now().UnixNano())
		if err := r.Mkdir(stage, 0o700); err != nil {
			return unchanged(err)
		}
		if err := t.unpack(artifact, r, stage); err != nil {
			r.RemoveAll(stage)
			return unchanged(err)
		}
		if d, err := treeDigest(r.FS(), stage); err != nil || d != want {
			r.RemoveAll(stage)
			return unchanged(fmt.Errorf("%s unpacked twice to different files", t.Asset(version)))
		}
		if err := r.Chmod(stage, 0o755); err != nil {
			r.RemoveAll(stage)
			return unchanged(err)
		}
		if err := r.Rename(stage, version); err != nil {
			r.RemoveAll(stage)
			return unchanged(fmt.Errorf("cannot place %s %s at %s: %v", t.Binary, version, release.Tilde(target), err))
		}
	}
	placed := fmt.Sprintf("%s %s is at %s but not linked: ", t.Binary, version, release.Tilde(target))
	if err := writeRecord(r, p, version, want); err != nil {
		return errors.New(placed + err.Error())
	}
	if why := t.notOpened(p, parent, r); why != "" {
		return errors.New(placed + became(why))
	}
	if err := t.link(p, version); err != nil {
		return errors.New(placed + err.Error())
	}
	return nil
}

// writeRecord records want as version's digest under r, p.Dir's root
// (VerifiedRecord), never through a link (T247): the record's folder must
// be a folder, and the digest is written to a new file (O_EXCL, which a
// link at its name refuses: the next name is tried) renamed over the
// record's name, which replaces whatever is there, a link too, and
// follows none.
func writeRecord(r *os.Root, p Place, version, want string) error {
	if err := r.Mkdir(verifiedDir, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	notFolder := fmt.Errorf("%s is not a folder; it was left as it is", release.Tilde(filepath.Join(p.Dir, verifiedDir)))
	fi, err := r.Lstat(verifiedDir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return notFolder
	}
	vr, err := r.OpenRoot(verifiedDir)
	if err != nil {
		return err
	}
	defer vr.Close()
	if open, err := vr.Stat("."); err != nil || !os.SameFile(fi, open) {
		return notFolder
	}
	for i := 0; ; i++ {
		tmp := recordTemp(version, i)
		f, err := vr.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) && i < 100 {
			continue
		}
		if err != nil {
			return err
		}
		_, err = f.WriteString(want + "\n")
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = vr.Rename(tmp, version)
		}
		if err != nil {
			vr.Remove(tmp)
		}
		return err
	}
}

// notOpened says why p.Dir is no longer the folder r opened: its parent
// swapped since the install began (parent), p.Dir a link, not a folder, or
// another folder; "" while it is.
func (t Tool) notOpened(p Place, parent dirAt, r *os.Root) string {
	if why := parent.swapped(filepath.Dir(p.Dir)); why != "" {
		return why
	}
	if why := t.shareNotOurs(p); why != "" {
		return why
	}
	fi, err := os.Lstat(p.Dir)
	if err != nil {
		return release.Tilde(p.Dir) + " is gone"
	}
	open, err := r.Stat(".")
	if err != nil || !os.SameFile(fi, open) {
		return release.Tilde(p.Dir) + " is another folder"
	}
	return ""
}

// notOpened says why path, a parent folder, is no longer what d saw or
// the folder r opened; "" while it is both.
func (d dirAt) notOpened(path string, r *os.Root) string {
	if why := d.swapped(path); why != "" {
		return why
	}
	fi, err := os.Stat(path)
	if err != nil {
		return release.Tilde(path) + " is gone"
	}
	open, err := r.Stat(".")
	if err != nil || !os.SameFile(fi, open) {
		return release.Tilde(path) + " is another folder"
	}
	return ""
}

// unpackTo unpacks artifact into dir, an empty folder, and returns its
// tree's digest.
func (t Tool) unpackTo(artifact, dir string) (string, error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer r.Close()
	if err := t.unpack(artifact, r, "."); err != nil {
		return "", err
	}
	return treeDigest(r.FS(), ".")
}

// unpack writes the gzip tarball artifact into dir, an empty folder under
// r, through r: only folders and regular files, each named by a relative
// path with no `..`, at most maxEntries of them and maxUnpacked bytes. A
// link (symbolic or hard), a device, a name out of the folder or given
// twice is refused, and so is a tarball with no bin/<binary>. A file keeps
// only its owner's execute bit's meaning: 0755 or 0644.
func (t Tool) unpack(artifact string, r *os.Root, dir string) error {
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
				if err := r.MkdirAll(to, 0o755); err != nil {
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
			if err := r.MkdirAll(filepath.Dir(to), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if h.Mode&0o100 != 0 {
				mode = 0o755
			}
			out, err := r.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
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
	if fi, err := r.Lstat(filepath.Join(dir, "bin", t.Binary)); err != nil || !fi.Mode().IsRegular() {
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

// ForeignError is a place jhon did not make, left alone: Note says why.
type ForeignError struct{ Note string }

func (e *ForeignError) Error() string { return e.Note }

// RollbackTool links t's previous version at p again: of the versions kept,
// the newest older than the active one, when its folder is still the one
// jhon verified (VerifiedRecord). Nothing is removed.
func RollbackTool(t Tool, p Place) (from, to string, err error) {
	in := t.Installed(p)
	switch {
	case in.Dev:
		return "", "", ErrDevelopmentTool
	case in.Foreign != "":
		return "", "", &ForeignError{in.Foreign}
	case in.Active == "":
		return "", "", ErrNoOlderTool
	}
	for _, v := range slices.Backward(in.Kept) {
		if semver.Compare("v"+v, "v"+in.Active) >= 0 {
			continue
		}
		if !t.verified(p, v) {
			return in.Active, "", fmt.Errorf("%s is not the %s %s jhon installed (its files differ, or jhon did not install it); nothing was changed", release.Tilde(filepath.Join(p.Dir, v)), t.Binary, v)
		}
		if err := t.link(p, v); err != nil {
			return in.Active, "", fmt.Errorf("cannot link %s to %s %s: %v; nothing was changed", release.Tilde(p.Link), t.Binary, v, err)
		}
		return in.Active, v, nil
	}
	return in.Active, "", ErrNoOlderTool
}
