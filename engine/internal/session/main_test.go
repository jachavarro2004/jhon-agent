package session

import (
	"os"
	"path/filepath"
	"testing"
)

// The tests never read or write the home or the state folder of whoever
// runs them (T249): Jhon's install record lives in the state folder
// (${XDG_STATE_HOME:-~/.local/state}/jhon/install.json), and a record left at
// the legacy ~/.cache/jhon/install.json is moved there. So HOME is a
// temporary folder of the run's own, removed after it, XDG_STATE_HOME is
// unset (each test's home holds its own state folder), and the tests run as
// outside the sandbox (JHON_IN_SANDBOX unset).
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "jhon-session-home-")
	if err != nil {
		panic(err)
	}
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Unsetenv("XDG_STATE_HOME")
	os.Unsetenv("JHON_IN_SANDBOX")
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}
