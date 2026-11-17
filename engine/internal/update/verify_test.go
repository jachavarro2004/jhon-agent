package update

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// notWorkflow is the refusal of a signer other than the release workflow.
const notWorkflow = `^jhon-0\.2\.0-linux-amd64\.tar\.gz is signed by .*, not by the release workflow$`

func TestVerify(t *testing.T) {
	vs := virtual(t)
	trust := Trust{Material: vs}
	dir := t.TempDir()
	artifact := filepath.Join(dir, "jhon-0.2.0-linux-amd64.tar.gz")
	if err := os.WriteFile(artifact, []byte("the release"), 0o644); err != nil {
		t.Fatal(err)
	}
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	type signerCase struct{ name, identity, issuer, artifact, want string }
	cases := []signerCase{
		{"the release workflow's signature verifies", workflow, Issuer, "the release", ""},
		{"another identity is refused, named", "https://github.com/someone/else/.github/workflows/release.yml@refs/tags/v0.2.0", Issuer,
			"the release", `^jhon-0\.2\.0-linux-amd64\.tar\.gz is signed by https://github\.com/someone/else/\.github/workflows/release\.yml@refs/tags/v0\.2\.0 \(issuer https://token\.actions\.githubusercontent\.com\), not by the release workflow$`},
		{"a third repository's release workflow is refused", "https://github.com/someone/jhon-agent/.github/workflows/release.yml@refs/tags/v0.2.0", Issuer, "the release", notWorkflow},
		{"the new owner's old repository name is refused", "https://github.com/jachavarro2004/jhon/.github/workflows/release.yml@refs/tags/v0.2.0", Issuer, "the release", notWorkflow},
		{"a workflow under a subdirectory of the repository is refused", "https://github.com/jachavarro2004/jhon-agent/sub/.github/workflows/release.yml@refs/tags/v0.2.0", Issuer, "the release", notWorkflow},
		{"the identity repeated is refused", workflow + workflow, Issuer, "the release", notWorkflow},
		{"another issuer is refused, named", workflow, "https://accounts.example.com",
			"the release", `^jhon-0\.2\.0-linux-amd64\.tar\.gz is signed by .* \(issuer https://accounts\.example\.com\), not by the release workflow$`},
		{"the identity as a suffix is refused", "https://evil.example.com/" + workflow, Issuer, "the release", notWorkflow},
		{"a look-alike issuer is refused", workflow, Issuer + ".evil", "the release", notWorkflow},
		{"a signature over other bytes does not verify", workflow, Issuer, "another tarball", `^the signature of jhon-0\.2\.0-linux-amd64\.tar\.gz does not verify: `},
	}
	// The repository's release workflow takes these tags, and only tags (S11).
	{
		base := "https://github.com/jachavarro2004/jhon-agent"
		tag := base + "/.github/workflows/release.yml@refs/tags/"
		for _, c := range []signerCase{
			{"a release candidate's tag verifies", tag + "v0.2.0-rc.1", Issuer, "the release", ""},
			{"an rc tag without its dot is refused", tag + "v1.2.3-rc1", Issuer, "the release", notWorkflow},
			{"a beta tag is refused", tag + "v0.2.0-beta.1", Issuer, "the release", notWorkflow},
			{"a version with a trailing character is refused", tag + "v0.2.0x", Issuer, "the release", notWorkflow},
			{"a version with a path after it is refused", tag + "v0.2.0/../x", Issuer, "the release", notWorkflow},
			{"a tag that is no version is refused", tag + "vEVIL", Issuer, "the release", notWorkflow},
			{"a leading zero in the version is refused", tag + "v01.2.3", Issuer, "the release", notWorkflow},
			{"a leading zero in the rc number is refused", tag + "v0.2.0-rc.01", Issuer, "the release", notWorkflow},
			{"an rc number with a trailing character is refused", tag + "v0.2.0-rc.1x", Issuer, "the release", notWorkflow},
			{"an rc number with a path after it is refused", tag + "v0.2.0-rc.1/x", Issuer, "the release", notWorkflow},
			{"an rc without its number is refused", tag + "v0.2.0-rc.", Issuer, "the release", notWorkflow},
			{"a negative rc number is refused", tag + "v0.2.0-rc.-1", Issuer, "the release", notWorkflow},
			{"an rc with build metadata is refused", tag + "v0.2.0-rc.1+meta", Issuer, "the release", notWorkflow},
			{"an upper-case RC is refused", tag + "v0.2.0-RC.1", Issuer, "the release", notWorkflow},
			{"an rc followed by a newline is refused", tag + "v0.2.0-rc.1\n", Issuer, "the release", "(?s)" + notWorkflow},
			{"an rc branch is refused", base + "/.github/workflows/release.yml@refs/heads/v0.2.0-rc.1", Issuer, "the release", notWorkflow},
			{"a version without leading zeros verifies", tag + "v10.20.30-rc.123", Issuer, "the release", ""},
			{"v0.0.0 verifies", tag + "v0.0.0", Issuer, "the release", ""},
			{"a fork's rc is refused", base + "-evil/.github/workflows/release.yml@refs/tags/v0.2.0-rc.1", Issuer, "the release", notWorkflow},
			{"a fork is refused", base + "-evil/.github/workflows/release.yml@refs/tags/v0.2.0", Issuer, "the release", notWorkflow},
			{"another workflow is refused", base + "/.github/workflows/other.yml@refs/tags/v0.2.0", Issuer, "the release", notWorkflow},
			{"a longer workflow name is refused", base + "/.github/workflows/release.ymlX@refs/tags/v0.2.0", Issuer, "the release", notWorkflow},
			{"a branch is refused", base + "/.github/workflows/release.yml@refs/heads/main", Issuer, "the release", notWorkflow},
			{"a pull request ref is refused", base + "/.github/workflows/release.yml@refs/pull/1/merge", Issuer, "the release", notWorkflow},
		} {
			cases = append(cases, c)
		}
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := write("x.sigstore.json", signBundle(t, vs, c.identity, c.issuer, []byte(c.artifact)))
			got, err := Verify(artifact, b, trust)
			if c.want == "" {
				if err != nil || got != c.identity {
					t.Fatalf("Verify = %q, %v", got, err)
				}
				return
			}
			if err == nil || !regexp.MustCompile(c.want).MatchString(err.Error()) {
				t.Fatalf("Verify error = %v, want %s", err, c.want)
			}
		})
	}
	notBundle := `^the signature of jhon-0\.2\.0-linux-amd64\.tar\.gz is not a Sigstore bundle: `
	doesNotVerify := `^the signature of jhon-0\.2\.0-linux-amd64\.tar\.gz does not verify: `
	good := signBundle(t, vs, workflow, Issuer, []byte("the release"))
	var noTlog protobundle.Bundle
	if err := protojson.Unmarshal(good, &noTlog); err != nil {
		t.Fatal(err)
	}
	noTlog.VerificationMaterial.TlogEntries = nil
	noTlogJSON, err := protojson.Marshal(&noTlog)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name   string
		bundle []byte
		trust  Trust
		want   string
	}{
		{"a file that is no bundle is refused", []byte("{}"), trust, notBundle},
		{"a truncated bundle is refused", good[:len(good)/2], trust, notBundle},
		{"a bundle with no transparency log entry is refused", noTlogJSON, trust, "(" + notBundle + ")|(" + doesNotVerify + ")"},
		{"a certificate without a CT timestamp is refused when one is required", good, Trust{Material: vs, RequireSCT: true}, doesNotVerify},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := Verify(artifact, write("probe.sigstore.json", c.bundle), c.trust)
			if err == nil || !regexp.MustCompile(c.want).MatchString(err.Error()) {
				t.Fatalf("Verify error = %v, want %s", err, c.want)
			}
		})
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		current, latest string
		newer, dev      bool
	}{
		{"0.1.0", "0.1.0", false, false},
		{"0.1.0", "0.2.0", true, false},
		{"0.2.0", "0.1.0", false, false},
		{"0.0.0-dev", "0.1.0", true, true},
		{"0.0.0-dev+abc1234", "0.0.1", true, true},
		{"0.1.0", "0.10.0", true, false},
		{"0.1.0-rc.1", "0.1.0-rc.2", true, false},
		{"0.1.0-rc.2", "0.1.0", true, false},
		{"0.1.0-rc.1", "0.1.0", true, false},
		{"0.1.0", "0.1.0-rc.2", false, false},
		{"0.1.0", "0.1.1", true, false},
		{"0.1.0-rc.9", "0.1.0-rc.10", true, false},
	} {
		if got := Newer(c.current, c.latest); got != c.newer {
			t.Errorf("Newer(%s, %s) = %v", c.current, c.latest, got)
		}
		if got := IsDev(c.current); got != c.dev {
			t.Errorf("IsDev(%s) = %v", c.current, got)
		}
	}
}
