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
	// The owner's state folder is never read nor written (T249): the guards'
	// stale-session note reads the install record there.
	state, err := os.MkdirTemp("", "jhon-hook-state-")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", state)
	os.Unsetenv("JHON_IN_SANDBOX")
	// Nor the owner's settings (T264): JHON_SETTINGS and XDG_CONFIG_HOME name
	// a file of the run's own that does not exist, the defaults, unless a test
	// writes its own.
	config := filepath.Join(state, "config")
	os.Setenv("XDG_CONFIG_HOME", config)
	os.Setenv("JHON_SETTINGS", filepath.Join(config, "jhon", "settings.toml"))
	code := m.Run()
	os.RemoveAll(state)
	os.Exit(code)
}
