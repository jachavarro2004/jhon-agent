package sandbox

import (
	"os/exec"
	"syscall"
)

// DieWithParent makes cmd's process end with SIGKILL when the thread that
// starts it ends (T250, L43f): a jhon that supervises a line outside the
// sandbox and is killed (SIGKILL, which it cannot forward) leaves no
// program of the line running once its record in the registry is gone, as
// --die-with-parent does for bubblewrap. The caller starts and waits for
// cmd on one locked OS thread (runtime.LockOSThread), so no other goroutine
// ends that thread while the program runs. A program the line's shell
// starts in turn (not exec'd) is not reached: the kernel signals the child
// only.
func DieWithParent(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}
