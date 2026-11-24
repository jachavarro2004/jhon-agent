// Package update is `jhon update` (T6, S8, S9): it reads the latest release
// of the public repository, picks the artifact for this machine, compares it
// with the version stamped into this binary, downloads the tarball, its
// .sha256 and its .sigstore.json, verifies the sha256 and then the cosign
// keyless signature with sigstore-go against the expected signer, and hands
// the verified artifact to release.InstallArtifact, which activates it.
// Rollback re-activates an earlier release kept on disk.
//
// Any failure before the install (the network, a missing asset, the
// checksum, the signature, the signer) changes nothing on disk outside a
// temporary directory, and says what failed. The sigstore modules are
// imported by this package alone (test/architecture).
package update

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/version"
)

// LatestURL is where the latest release is read (S9): the public repository
// jachavarro2004/jhon-agent, where releases are published from v0.3.1 on
// (T18); JHON_RELEASES_URL overrides it (the tests' httptest server).
const LatestURL = "https://api.github.com/repos/jachavarro2004/jhon-agent/releases/latest"

// Source is the repository releases come from, as the update screen shows it.
const Source = "github.com/jachavarro2004/jhon-agent"

// Asset is one file of a release.
type Asset struct{ Name, URL string }

// Latest is the latest release: its version (the tag without its v), the
// date it was published, the bullet lines of its notes, and this machine's
// tarball with its checksum and its signature bundle.
type Latest struct {
	Version, Date        string
	Notes                []string
	Tarball, Sum, Bundle Asset
}

// Client finds, downloads and verifies releases.
type Client struct {
	URL     string
	HTTP    *http.Client
	Arch    string
	Current string
	// HTTPSOnly refuses a URL, an asset's or a redirect's, that is not
	// https, and a release marked pre-release: the default;
	// JHON_RELEASES_URL (the tests', an rc's) lifts both.
	HTTPSOnly bool
	// trust is the verifier's trusted material and policy: the Sigstore
	// public-good instance in production; tests inject a virtual one.
	trust func() (Trust, error)
}

// New is the client of this binary: the S9 URL (or JHON_RELEASES_URL),
// this machine's architecture, the stamped version, the public-good trust.
func New() *Client {
	url := os.Getenv("JHON_RELEASES_URL")
	c := &Client{URL: url, HTTP: &http.Client{Timeout: 2 * time.Minute}, Arch: runtime.GOARCH, Current: version.Version, trust: publicGood}
	if url == "" {
		c.URL, c.HTTPSOnly = LatestURL, true
		c.HTTP.CheckRedirect = httpsRedirects
	}
	if testTrust != nil {
		c.trust = testTrust
	}
	return c
}

// Newer is whether latest is a newer version than current. A development
// build (0.0.0-dev, 0.0.0-dev+<commit>) is older than any release.
func Newer(current, latest string) bool {
	return semver.Compare("v"+latest, "v"+current) > 0
}

// IsDev is whether v is a development build's version (version.IsDev).
func IsDev(v string) bool { return version.IsDev(v) }

// ErrNoRelease is Latest's answer when /releases/latest is 404: GitHub's
// answer while only pre-releases are published (S11). It is not a failure.
var ErrNoRelease = errors.New("no stable release is published yet")

// Latest reads the latest release and finds this machine's artifact in it.
// A release marked pre-release is refused from the default URL (S11): an rc
// is offered only through JHON_RELEASES_URL. A 404 from a URL ending in
// /releases/latest is ErrNoRelease; from any other (a JHON_RELEASES_URL
// naming a tag) it is the missing release it says.
func (c *Client) Latest(ctx context.Context) (Latest, error) {
	var buf bytes.Buffer
	if err := c.get(ctx, c.URL, c.URL, &buf, maxAPI); err != nil {
		var se *statusError
		if errors.As(err, &se) && se.code == http.StatusNotFound && strings.HasSuffix(strings.TrimRight(c.URL, "/"), "/releases/latest") {
			return Latest{}, ErrNoRelease
		}
		return Latest{}, err
	}
	b := buf.Bytes()
	var gh struct {
		Tag       string `json:"tag_name"`
		Published string `json:"published_at"`
		Body      string `json:"body"`
		Pre       bool   `json:"prerelease"`
		Assets    []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(b, &gh); err != nil {
		return Latest{}, fmt.Errorf("the latest release at %s is not a GitHub release: %v", c.URL, err)
	}
	v := strings.TrimPrefix(gh.Tag, "v")
	if !semver.IsValid("v"+v) || !release.VersionPattern.MatchString(v) {
		return Latest{}, fmt.Errorf("the latest release's tag %q is not a version", gh.Tag)
	}
	if gh.Pre && c.HTTPSOnly {
		return Latest{}, fmt.Errorf("the latest release %s is marked pre-release; only JHON_RELEASES_URL offers one", v)
	}
	l := Latest{Version: v, Date: strings.SplitN(gh.Published, "T", 2)[0]}
	for _, line := range strings.Split(gh.Body, "\n") {
		if n, ok := strings.CutPrefix(strings.TrimSpace(line), "- "); ok {
			l.Notes = append(l.Notes, n)
		}
	}
	name := release.ArtifactName(v, c.Arch)
	for _, want := range []struct {
		name string
		to   *Asset
	}{{name, &l.Tarball}, {name + ".sha256", &l.Sum}, {name + ".sigstore.json", &l.Bundle}} {
		for _, a := range gh.Assets {
			if a.Name == want.name {
				*want.to = Asset{a.Name, a.URL}
			}
		}
		if want.to.URL == "" {
			return l, fmt.Errorf("release %s has no %s", v, want.name)
		}
		if u, err := url.Parse(want.to.URL); c.HTTPSOnly && (err != nil || u.Scheme != "https") {
			return l, fmt.Errorf("release %s's %s is not at an https URL: %s", v, want.name, want.to.URL)
		}
	}
	return l, nil
}

// Fetch downloads l's tarball, checksum and signature into dir, each within
// its size limit, verifies the sha256, then the signature, then that the
// tarball's jhon is stamped l's version, and returns the tarball's path (its
// .sha256 beside it, as release.InstallArtifact reads it) and who signed it.
func (c *Client) Fetch(ctx context.Context, l Latest, dir string) (artifact, signer string, err error) {
	for _, a := range []struct {
		Asset
		limit int64
	}{{l.Tarball, maxTarball}, {l.Sum, maxSum}, {l.Bundle, maxBundle}} {
		if err := c.download(ctx, a.Asset, dir, a.limit); err != nil {
			return "", "", err
		}
	}
	artifact = filepath.Join(dir, l.Tarball.Name)
	if err := checkSum(artifact, artifact+".sha256"); err != nil {
		return "", "", err
	}
	trust, err := c.trust()
	if err != nil {
		return "", "", fmt.Errorf("cannot load Sigstore's trusted root: %v", err)
	}
	if signer, err = Verify(artifact, filepath.Join(dir, l.Bundle.Name), trust); err != nil {
		return "", "", err
	}
	v, err := stamped(artifact, dir)
	if err != nil {
		return "", "", err
	}
	if v != l.Version {
		return "", "", fmt.Errorf("%s holds jhon %s, not the release's %s", l.Tarball.Name, v, l.Version)
	}
	return artifact, signer, nil
}
