package hook

import (
	"os"
	"path/filepath"
	"testing"
)

// The project brief a SessionStart hands reads the contracts of this checkout.
// No test reaches the owner's Engram or CodeGraph unless it names its own
// fake (an Agent launch asks both, launchcontext.go).
func TestMain(m *testing.M) {
	root, _ := filepath.Abs("../../..")
	os.Setenv("JHON_ROOT", root)
	os.Setenv("JHON_ENGRAM", "/dev/null/engram")
	codegraphProgram = "/dev/null/codegraph"
	os.Exit(m.Run())
}
