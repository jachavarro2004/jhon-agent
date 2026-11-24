//go:build unix

package integrations

import (
	"os/exec"
	"syscall"
)

// ownGroup starts cmd in a process group of its own and makes the cut kill
// the whole group, so a hung program's children go with it rather than
// outliving the bound (odd/tasks/jhon-studio.md L11).
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
