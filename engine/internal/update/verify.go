package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

// The expected signer of a release (S8, S9), in this one place: the GitHub
// Actions OIDC issuer, and a release workflow running for a tag vX.Y.Z or a
// release candidate vX.Y.Z-rc.N (S11), each identity anchored at both ends.
// Exactly two workflows are trusted (T18): the public repository's and,
// for the move to it, the old public repository's.
const (
	Issuer = "https://token.actions.githubusercontent.com"

	// IdentityPattern is the release workflow of the public repository,
	// jachavarro2004/jhon-agent, at the repository root (S10): every
	// release from v0.3.1 on is signed there.
	IdentityPattern = `^https://github\.com/jachavarro2004/jhon-agent/\.github/workflows/release\.yml@refs/tags/v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-rc\.(0|[1-9]\d*))?$`

	// BridgeIdentityPattern is the release workflow of the old public
	// repository, sdsoftwaresas/jhon (dist/public/.github/workflows/release.yml),
	// which signed every release up to v0.3.0. It stays because v0.3.0, the
	// bridge, is signed there: the copies installed before the move trust
	// only it, and v0.3.0 is the release that teaches them the new
	// repository. It may go once v0.3.1 is published from
	// jachavarro2004/jhon-agent and sdsoftwaresas/jhon is deleted.
	BridgeIdentityPattern = `^https://github\.com/sdsoftwaresas/jhon/\.github/workflows/release\.yml@refs/tags/v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-rc\.(0|[1-9]\d*))?$`
)

// identityPatterns is every signer identity a release may have.
var identityPatterns = []string{IdentityPattern, BridgeIdentityPattern}

var identities = func() []*regexp.Regexp {
	rs := make([]*regexp.Regexp, len(identityPatterns))
	for i, p := range identityPatterns {
		rs[i] = regexp.MustCompile(p)
	}
	return rs
}()

// trustedSigner is whether san is one of the trusted release workflows.
func trustedSigner(san string) bool {
	for _, r := range identities {
		if r.MatchString(san) {
			return true
		}
	}
	return false
}

// Trust is what a signature is checked against: the trusted material (the
// certificate authorities, transparency logs and timestamp authorities)
// and whether the certificate must carry a CT log's timestamp, as every
// Fulcio certificate does.
type Trust struct {
	Material   root.TrustedMaterial
	RequireSCT bool
}

// testTrust, when set, replaces the public-good trust in New: only a build
// with the jhon_testroot tag sets it (testroot.go), never a release.
var testTrust func() (Trust, error)

// publicGood is the Sigstore public-good instance's trusted root, reached
// through TUF from the root.json sigstore-go embeds, cached under the
// user's cache directory for a day: the embedded TUF root anchors every
// refresh, so a key rotation of Fulcio or Rekor follows without a new jhon,
// and no network is needed for it within the day.
func publicGood() (Trust, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return Trust{}, err
	}
	opts := tuf.DefaultOptions().WithCachePath(filepath.Join(cache, "jhon", "sigstore-tuf")).WithCacheValidity(1)
	c, err := tuf.New(opts)
	if err != nil {
		return Trust{}, err
	}
	tr, err := root.GetTrustedRoot(c)
	if err != nil {
		return Trust{}, err
	}
	return Trust{Material: tr, RequireSCT: true}, nil
}

// checkSum verifies artifact against the sha256sum line in sum, which must
// name the artifact.
func checkSum(artifact, sum string) error {
	name := filepath.Base(artifact)
	b, err := os.ReadFile(sum)
	if err != nil {
		return fmt.Errorf("cannot read %s: %v", filepath.Base(sum), err)
	}
	want, file, _ := strings.Cut(strings.TrimSpace(string(b)), " ")
	if strings.TrimPrefix(strings.TrimSpace(file), "*") != name {
		return fmt.Errorf("%s is not the checksum of %s", filepath.Base(sum), name)
	}
	f, err := os.Open(artifact)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if !strings.EqualFold(want, hex.EncodeToString(h.Sum(nil))) {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	return nil
}

// Verify checks the cosign keyless bundle at bundlePath against artifact:
// the certificate chains to the trusted Fulcio, the signature is over the
// artifact, the signing is in the transparency log, and the certificate
// names the expected signer. It returns the signer's identity.
//
// The signing time needs one observer timestamp (WithObserverTimestamps(1)):
// an RFC 3161 timestamp or the Rekor entry's integrated time, either one, so
// a bundle with no RFC 3161 timestamp verifies on its Rekor time; no RFC 3161
// timestamp is required (T6's log, L19, said one was).
func Verify(artifact, bundlePath string, t Trust) (string, error) {
	name := filepath.Base(artifact)
	b, err := bundle.LoadJSONFromPath(bundlePath)
	if err != nil {
		return "", fmt.Errorf("the signature of %s is not a Sigstore bundle: %v", name, err)
	}
	opts := []verify.VerifierOption{verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1)}
	if t.RequireSCT {
		opts = append(opts, verify.WithSignedCertificateTimestamps(1))
	}
	v, err := verify.NewVerifier(t.Material, opts...)
	if err != nil {
		return "", err
	}
	// The certificate must match one identity: sigstore-go accepts the
	// first of the policy's identities that matches.
	ids := make([]verify.PolicyOption, 0, len(identityPatterns))
	for _, p := range identityPatterns {
		id, err := verify.NewShortCertificateIdentity(Issuer, "", "", p)
		if err != nil {
			return "", err
		}
		ids = append(ids, verify.WithCertificateIdentity(id))
	}
	f, err := os.Open(artifact)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := v.Verify(b, verify.NewPolicy(verify.WithArtifact(f), ids...)); err != nil {
		if san, iss, ok := signer(b); ok && (iss != Issuer || !trustedSigner(san)) {
			return "", fmt.Errorf("%s is signed by %s (issuer %s), not by the release workflow", name, san, iss)
		}
		return "", fmt.Errorf("the signature of %s does not verify: %v", name, err)
	}
	san, _, _ := signer(b)
	return san, nil
}

// signer is the identity and issuer the bundle's certificate claims.
func signer(b *bundle.Bundle) (san, issuer string, ok bool) {
	vc, err := b.VerificationContent()
	if err != nil || vc.Certificate() == nil {
		return "", "", false
	}
	s, err := certificate.SummarizeCertificate(vc.Certificate())
	if err != nil {
		return "", "", false
	}
	return s.SubjectAlternativeName, s.Extensions.Issuer, true
}
