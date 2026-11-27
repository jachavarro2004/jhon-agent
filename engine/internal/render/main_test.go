package render

import (
	"os"
	"path/filepath"
	"testing"
)

// The tests never read or write the state folder of whoever runs them
// (T249): Jhon's install record lives there
// (${XDG_STATE_HOME:-~/.local/state}/jhon/install.json), so XDG_STATE_HOME
// is a temporary folder of the run's own, removed after it, unless a test
// sets its own; and they run as outside the sandbox (JHON_IN_SANDBOX unset).
//
// Nor the settings of whoever runs them (T264): a render reads
// settings.Path(), JHON_SETTINGS, else $XDG_CONFIG_HOME/jhon/settings.toml,
// so an owner with jhon-studio on measured another fixed context than CI
// did. Both name a file of the run's own that does not exist, the defaults,
// unless a test writes its own (t.Setenv("JHON_SETTINGS", …)).
// TestARenderTestNeverReadsTheCallersSettings runs this binary with a
// hostile settings file in both places and shows nothing changes.
func TestMain(m *testing.M) {
	state, err := os.MkdirTemp("", "jhon-render-state-")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", state)
	os.Unsetenv("JHON_IN_SANDBOX")
	config := filepath.Join(state, "config")
	os.Setenv("XDG_CONFIG_HOME", config)
	os.Setenv("JHON_SETTINGS", filepath.Join(config, "jhon", "settings.toml"))
	if out := os.Getenv(settingsProbeEnv); out != "" {
		code := settingsProbe(out)
		os.RemoveAll(state)
		os.Exit(code)
	}
	code := m.Run()
	os.RemoveAll(state)
	os.Exit(code)
}
