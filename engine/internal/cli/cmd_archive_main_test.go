package cli

import (
	"os"
	"testing"
)

// TestMain keeps every command this package runs in process off the owner's
// Engram (T134): `jhon archive` and a light verify write the change's state
// there, so JHON_ENGRAM names a path that cannot exist unless a test names
// its own fake.
func TestMain(m *testing.M) {
	if _, set := os.LookupEnv("JHON_ENGRAM"); !set {
		os.Setenv("JHON_ENGRAM", "/dev/null/engram")
	}
	os.Exit(m.Run())
}
