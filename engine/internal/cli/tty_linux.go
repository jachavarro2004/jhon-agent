package cli

import (
	"os"
	"syscall"
	"unsafe"
)

// isTerminal is the shell's `[ -t <fd> ]`: whether f is a terminal.
func isTerminal(f *os.File) bool {
	var t syscall.Termios
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&t)))
	return e == 0
}
