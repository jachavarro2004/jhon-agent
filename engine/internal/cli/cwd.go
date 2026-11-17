package cli

import "syscall"

// cwd is process.cwd(): the physical working directory, not $PWD.
func cwd() string {
	wd, _ := syscall.Getwd()
	return wd
}
