//go:build linux && (amd64 || arm64)

package update

import (
	"errors"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

// renameat2's number and flags (the syscall package lists neither on amd64).
var sysRenameat2 = map[string]uintptr{"amd64": 316, "arm64": 276}[runtime.GOARCH]

const (
	renameNoreplace = 1
	renameExchange  = 2
)

// renameat2 renames from to to, both in the folder r holds, with flags.
func renameat2(r *os.Root, from, to string, flags uintptr) error {
	d, err := r.Open(".")
	if err != nil {
		return err
	}
	defer d.Close()
	f, err := syscall.BytePtrFromString(from)
	if err != nil {
		return err
	}
	tt, err := syscall.BytePtrFromString(to)
	if err != nil {
		return err
	}
	fd := d.Fd()
	_, _, e := syscall.Syscall6(sysRenameat2, fd, uintptr(unsafe.Pointer(f)), fd, uintptr(unsafe.Pointer(tt)), flags, 0)
	runtime.KeepAlive(d)
	if e != 0 {
		return e
	}
	return nil
}

// swapIn swaps the names from and to atomically: errAbsent when nothing is
// at to, errNoSwap when the file system cannot.
func swapIn(r *os.Root, from, to string) error {
	err := renameat2(r, from, to, renameExchange)
	switch {
	case errors.Is(err, syscall.ENOENT):
		return errAbsent
	case errors.Is(err, syscall.EINVAL), errors.Is(err, syscall.ENOSYS), errors.Is(err, syscall.EOPNOTSUPP):
		return errNoSwap
	}
	return err
}

// placeNew renames from to to only while nothing is at to (os.ErrExist).
func placeNew(r *os.Root, from, to string) error {
	err := renameat2(r, from, to, renameNoreplace)
	switch {
	case errors.Is(err, syscall.EEXIST):
		return os.ErrExist
	case errors.Is(err, syscall.EINVAL), errors.Is(err, syscall.ENOSYS), errors.Is(err, syscall.EOPNOTSUPP):
		if _, err := r.Lstat(to); err == nil {
			return os.ErrExist
		}
		return r.Rename(from, to)
	}
	return err
}
