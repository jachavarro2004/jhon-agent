package render

import (
	"os"
	"testing"
)

// The tests never read or write the state folder of whoever runs them
// (T249): Jhon's install record lives there
// (${XDG_STATE_HOME:-~/.local/state}/jhon/install.json), so XDG_STATE_HOME
// is a temporary folder of the run's own, removed after it, unless a test
// sets its own; and they run as outside the sandbox (JHON_IN_SANDBOX unset).
func TestMain(m *testing.M) {
	state, err := os.MkdirTemp("", "jhon-render-state-")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", state)
	os.Unsetenv("JHON_IN_SANDBOX")
	code := m.Run()
	os.RemoveAll(state)
	os.Exit(code)
}
