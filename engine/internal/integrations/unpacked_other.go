//go:build !linux

package integrations

import (
	"context"
	"os/exec"
)

// reaped runs cmd: outside Linux there is no child subreaper, and only the
// program's process group is killed when it returns.
func reaped(_ context.Context, cmd *exec.Cmd) (bool, error) { return runGrouped(cmd) }

// mountedNoexec is false where jhon does not read the mount's flags.
var mountedNoexec = func(string) bool { return false }
