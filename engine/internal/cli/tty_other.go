//go:build !linux

package cli

import "os"

// isTerminal is the shell's `[ -t <fd> ]`, approximated where TCGETS is not
// the request: whether f is a character device.
func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
