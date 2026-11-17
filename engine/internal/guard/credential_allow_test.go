package guard

import "testing"

func TestCredentialGlobReadsCaseAndMagic(t *testing.T) {
	for _, g := range []string{"ID_*", ":(top)*id_*", ":*.json", "@*.json", "*.pem", "config/*.key"} {
		if !CredentialGlob(g) {
			t.Errorf("CredentialGlob(%q) = false", g)
		}
	}
	for _, g := range []string{"*.pem", "config/*.key", "snapshots/*.json"} {
		if !relativeGlob(g) {
			t.Errorf("relativeGlob(%q) = false", g)
		}
	}
	if w, ok := simpleWords("cat *.pem"); !ok || len(w) != 2 {
		t.Errorf("simpleWords: %v %v", w, ok)
	}
}
