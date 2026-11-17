package update

import (
	"context"
	"errors"
	"os"

	"golang.org/x/mod/semver"

	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
)

// Install downloads l into a temporary directory, verifies it (Fetch), and
// installs it with release.InstallArtifact, whose output goes to o;
// verified, when given, hears who signed it before the install starts. It
// returns the signer. Nothing is changed until the verification passed;
// the temporary directory is removed either way.
func (c *Client) Install(ctx context.Context, r release.Roots, o *release.Out, l Latest, verified func(signer string)) (string, error) {
	dir, err := os.MkdirTemp("", "jhon-update-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	artifact, signer, err := c.Fetch(ctx, l, dir)
	if err != nil {
		return "", err
	}
	if verified != nil {
		verified(signer)
	}
	if release.InstallArtifact(r, o, artifact) != 0 {
		return signer, ErrInstall
	}
	return signer, nil
}

// ErrInstall is a verified release whose install refused it; the install
// said why.
var ErrInstall = errors.New("the install of the verified release failed")

// Previous is the release a roll-back returns to: of the releases kept on
// disk that current does not point at, the newest whose version is older
// than the active one's (current when no kept release is active), the
// latest placed among equals.
func Previous(r release.Roots, current string) (release.Kept, bool) {
	kept := release.KeptReleases(r)
	for _, k := range kept {
		if k.Active {
			current = k.Version
		}
	}
	var best release.Kept
	found := false
	for _, k := range kept {
		if k.Active || semver.Compare("v"+k.Version, "v"+current) >= 0 && !(IsDev(k.Version) && IsDev(current)) {
			continue
		}
		if !found || semver.Compare("v"+k.Version, "v"+best.Version) >= 0 {
			best, found = k, true
		}
	}
	return best, found
}

// Find is the kept release, not the active one, stamped version, the latest
// placed when several are.
func Find(r release.Roots, version string) (release.Kept, bool) {
	var out release.Kept
	found := false
	for _, k := range release.KeptReleases(r) {
		if !k.Active && k.Version == version {
			out, found = k, true
		}
	}
	return out, found
}
