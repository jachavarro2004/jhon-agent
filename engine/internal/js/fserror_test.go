package js

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
)

// A spawn that did not start reads as node's error: its code when node has
// one, the error's own message otherwise.
func TestSpawnError(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{exec.ErrNotFound, "spawnSync claude ENOENT"},
		{&exec.Error{Name: "claude", Err: syscall.EACCES}, "spawnSync claude EACCES"},
		{errors.New("other"), "other"},
	} {
		if got := SpawnError("spawnSync", "claude", c.err); got != c.want {
			t.Errorf("SpawnError(%v) = %q, want %q", c.err, got, c.want)
		}
	}
	if SignalName(syscall.SIGTERM) != "SIGTERM" || SignalName(syscall.Signal(64)) != "64" {
		t.Errorf("SignalName = %q, %q", SignalName(syscall.SIGTERM), SignalName(syscall.Signal(64)))
	}
}
