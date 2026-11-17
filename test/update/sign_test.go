package update

import (
	"encoding/base64"
	"encoding/hex"
	"testing"

	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	protorekor "github.com/sigstore/protobuf-specs/gen/pb-go/rekor/v1"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/testing/ca"
	"github.com/sigstore/sigstore-go/pkg/tlog"
	"google.golang.org/protobuf/encoding/protojson"
)

// The signatures of these tests come from sigstore-go's virtual Sigstore:
// a Fulcio, a Rekor and a timestamp authority of its own, whose trusted root
// the jhon_testroot build reads from JHON_TEST_TRUSTED_ROOT.

const (
	// The public repository's release workflow, and the old public
	// repository's (bridgeWorkflow), which signs the v0.3.0 bridge (T18).
	workflow       = "https://github.com/jachavarro2004/jhon-agent/.github/workflows/release.yml@refs/tags/v0.2.0"
	bridgeWorkflow = "https://github.com/sdsoftwaresas/jhon/.github/workflows/release.yml@refs/tags/v0.2.0"
	issuer         = "https://token.actions.githubusercontent.com"
)

// trustedRoot is vs's trusted root as JSON. The virtual logs' IDs are the
// hex text of the key ID; the JSON form carries the raw bytes.
func trustedRoot(t testing.TB, vs *ca.VirtualSigstore) []byte {
	t.Helper()
	raw := func(logs map[string]*root.TransparencyLog) map[string]*root.TransparencyLog {
		for _, l := range logs {
			id, err := hex.DecodeString(string(l.ID))
			if err != nil {
				t.Fatal(err)
			}
			l.ID = id
		}
		return logs
	}
	tr, err := root.NewTrustedRoot(root.TrustedRootMediaType01, vs.FulcioCertificateAuthorities(), raw(vs.CTLogs()), vs.TimestampingAuthorities(), raw(vs.RekorLogs()))
	if err != nil {
		t.Fatal(err)
	}
	b, err := tr.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// signBundle is a cosign-style keyless bundle (v0.1: a certificate, the
// message signature, a Rekor entry with its signed promise, an RFC 3161
// timestamp) of artifact by identity, from vs.
func signBundle(t testing.TB, vs *ca.VirtualSigstore, identity string, artifact []byte) []byte {
	t.Helper()
	e, err := vs.Sign(identity, issuer, artifact)
	if err != nil {
		t.Fatal(err)
	}
	vc, _ := e.VerificationContent()
	sc, _ := e.SignatureContent()
	ms := sc.(*bundle.MessageSignature)
	entries, _ := e.TlogEntries()
	tle := entries[0].TransparencyLogEntry()
	set, err := vs.RekorSignPayload(tlog.RekorPayload{
		Body:           base64.StdEncoding.EncodeToString(tle.CanonicalizedBody),
		IntegratedTime: tle.IntegratedTime,
		LogIndex:       tle.LogIndex,
		LogID:          hex.EncodeToString(tle.LogId.KeyId),
	})
	if err != nil {
		t.Fatal(err)
	}
	tle.InclusionPromise = &protorekor.InclusionPromise{SignedEntryTimestamp: set}
	tle.KindVersion = &protorekor.KindVersion{Kind: "hashedrekord", Version: "0.0.1"}
	ts, _ := e.Timestamps()
	pb := &protobundle.Bundle{
		MediaType: "application/vnd.dev.sigstore.bundle+json;version=0.1",
		VerificationMaterial: &protobundle.VerificationMaterial{
			Content: &protobundle.VerificationMaterial_X509CertificateChain{X509CertificateChain: &protocommon.X509CertificateChain{
				Certificates: []*protocommon.X509Certificate{{RawBytes: vc.Certificate().Raw}},
			}},
			TlogEntries:               []*protorekor.TransparencyLogEntry{tle},
			TimestampVerificationData: &protobundle.TimestampVerificationData{Rfc3161Timestamps: []*protocommon.RFC3161SignedTimestamp{{SignedTimestamp: ts[0]}}},
		},
		Content: &protobundle.Bundle_MessageSignature{MessageSignature: &protocommon.MessageSignature{
			MessageDigest: &protocommon.HashOutput{Algorithm: protocommon.HashAlgorithm_SHA2_256, Digest: ms.Digest()},
			Signature:     ms.Signature(),
		}},
	}
	b, err := protojson.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
