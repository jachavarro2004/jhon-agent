package guard

import (
	"os"
	"path/filepath"
	"testing"
)

// The guards read the session's working set (T281): the workspace pointers
// under Jhon's state folder and sandbox.allow_write in the settings. No test
// reads the owner's: XDG_STATE_HOME is the run's own, and JHON_SETTINGS and
// XDG_CONFIG_HOME name a file of the run's own that does not exist, the
// defaults, unless a test writes its own.
func TestMain(m *testing.M) {
	state, err := os.MkdirTemp("", "jhon-guard-state-")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", state)
	config := filepath.Join(state, "config")
	os.Setenv("XDG_CONFIG_HOME", config)
	os.Setenv("JHON_SETTINGS", filepath.Join(config, "jhon", "settings.toml"))
	code := m.Run()
	os.RemoveAll(state)
	os.Exit(code)
}
