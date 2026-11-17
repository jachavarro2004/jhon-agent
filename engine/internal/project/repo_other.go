//go:build !unix

package project

import "os"

// device is the file system p is on, on a system with no device number to
// read (the OS sandbox does not run there): one file system; ok is false
// when p cannot be read.
func device(p string) (uint64, bool) {
	_, err := os.Stat(p)
	return 0, err == nil
}
