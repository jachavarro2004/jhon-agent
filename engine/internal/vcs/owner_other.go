//go:build !unix

package vcs

import "os"

// ownedByMe is true where the temporary folder is the user's own.
func ownedByMe(os.FileInfo) bool { return true }
