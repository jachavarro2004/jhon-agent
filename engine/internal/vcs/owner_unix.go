//go:build unix

package vcs

import (
	"os"
	"syscall"
)

// ownedByMe is whether info's file belongs to this process's user.
func ownedByMe(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
