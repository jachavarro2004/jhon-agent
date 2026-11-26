//go:build !linux

package sandbox

import "os/exec"

// DieWithParent does nothing off Linux, where the sandbox does not run and
// the kernel has no parent-death signal.
func DieWithParent(cmd *exec.Cmd) {}
