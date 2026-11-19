package guard

import "testing"

func TestCredentialGlobReadsCaseAndMagic(t *testing.T) {
	for _, g := range []string{"ID_*", ":(top)*id_*", ":*.json", "@*.json", "*.pem", "config/*.key"} {
		if !CredentialGlob(g) {
			t.Errorf("CredentialGlob(%q) = false", g)
		}
	}
}
