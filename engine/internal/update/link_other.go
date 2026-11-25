//go:build !(linux && (amd64 || arm64))

package update

import "os"

// swapIn cannot swap two names atomically here: the link's check stands
// for the move (link.go).
func swapIn(*os.Root, string, string) error { return errNoSwap }

// placeNew renames from to to while nothing is at to.
func placeNew(r *os.Root, from, to string) error {
	if _, err := r.Lstat(to); err == nil {
		return os.ErrExist
	}
	return r.Rename(from, to)
}
