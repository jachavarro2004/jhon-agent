package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain keeps every command this package runs in process off the owner's
// Engram (T134): `jhon archive` and a light verify write the change's state
// there, so JHON_ENGRAM names a path that cannot exist unless a test names
// its own fake.
//
// The tests also never read or write the state folder of whoever runs them
// (T249): Jhon's install record lives there
// (${XDG_STATE_HOME:-~/.local/state}/jhon/install.json), so XDG_STATE_HOME
// is a temporary folder of the run's own, removed after it, unless a test
// sets its own; and they run as outside the sandbox (JHON_IN_SANDBOX unset).
//
// Nor the owner's settings (T264): JHON_SETTINGS and XDG_CONFIG_HOME name a
// file of the run's own that does not exist, the defaults, unless a test
// writes its own.
func TestMain(m *testing.M) {
	if _, set := os.LookupEnv("JHON_ENGRAM"); !set {
		os.Setenv("JHON_ENGRAM", "/dev/null/engram")
	}
	state, err := os.MkdirTemp("", "jhon-cli-state-")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", state)
	os.Unsetenv("JHON_IN_SANDBOX")
	config := filepath.Join(state, "config")
	os.Setenv("XDG_CONFIG_HOME", config)
	os.Setenv("JHON_SETTINGS", filepath.Join(config, "jhon", "settings.toml"))
	code := m.Run()
	os.RemoveAll(state)
	os.Exit(code)
}
