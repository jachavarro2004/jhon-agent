//go:build unix

package project

import "syscall"

// device is the file system p is on; ok is false when p cannot be read.
func device(p string) (uint64, bool) {
	var st syscall.Stat_t
	if err := syscall.Stat(p, &st); err != nil {
		return 0, false
	}
	return uint64(st.Dev), true
}
