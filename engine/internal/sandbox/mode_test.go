package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

// T128f (L1352 F1): sandbox.mode is on by default where the sandbox runs,
// Linux; elsewhere the default reads as off (D3). A value in the settings
// file is the owner's on every system.
func TestModeIsOnByDefaultOnLinuxOnly(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct{ file, os, mode, source string }{
		{"", "linux", "on", ModeDefault},
		{"", "darwin", "off", ModeDefault},
		{"", "windows", "off", ModeDefault},
		{"[sandbox]\nmode = \"on\"\n", "linux", "on", ModeSettings},
		{"[sandbox]\nmode = \"on\"\n", "darwin", "on", ModeSettings},
		{"[sandbox]\nmode = \"off\"\n", "linux", "off", ModeSettings},
		{"[sandbox]\nmode = \"off\"\n", "windows", "off", ModeSettings},
	} {
		file := filepath.Join(dir, "settings.toml")
		if err := os.WriteFile(file, []byte(c.file), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("JHON_SETTINGS", file)
		if mode, source := Mode(Host{OS: c.os}); mode != c.mode || source != c.source {
			t.Errorf("%s with %q: %s (%s), want %s (%s)", c.os, c.file, mode, source, c.mode, c.source)
		}
	}
}
