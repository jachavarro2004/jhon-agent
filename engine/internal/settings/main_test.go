package settings

import (
	"os"
	"path/filepath"
	"testing"
)

// The tests never read the settings of whoever runs them (T264): Path() is
// JHON_SETTINGS, else $XDG_CONFIG_HOME/jhon/settings.toml, so both name a
// file of the run's own that does not exist, the defaults, unless a test
// writes its own; the folder is removed after the run.
func TestMain(m *testing.M) {
	config, err := os.MkdirTemp("", "jhon-settings-config-")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", config)
	os.Setenv("JHON_SETTINGS", filepath.Join(config, "jhon", "settings.toml"))
	code := m.Run()
	os.RemoveAll(config)
	os.Exit(code)
}
