//go:build jhon_testroot

package update

import (
	"errors"
	"os"

	"github.com/sigstore/sigstore-go/pkg/root"
)

// A build with the jhon_testroot tag, the conformance tests' alone, checks
// signatures against the trusted root in JHON_TEST_TRUSTED_ROOT (a virtual
// Sigstore's, whose certificates carry no CT timestamp). Releases are never
// built with this tag.
func init() {
	testTrust = func() (Trust, error) {
		p := os.Getenv("JHON_TEST_TRUSTED_ROOT")
		if p == "" {
			return Trust{}, errors.New("JHON_TEST_TRUSTED_ROOT is not set")
		}
		tr, err := root.NewTrustedRootFromPath(p)
		if err != nil {
			return Trust{}, err
		}
		return Trust{Material: tr}, nil
	}
}
