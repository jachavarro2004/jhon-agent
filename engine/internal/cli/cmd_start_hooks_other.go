//go:build !linux

package cli

import (
	"os"
	"path/filepath"
	"strconv"
)

// fileIdentity is what a file too big to read is compared by; off Linux,
// its size and modification time.
func fileIdentity(fi os.FileInfo) string {
	return "size " + strconv.FormatInt(fi.Size(), 10) + ", mtime " + strconv.FormatInt(fi.ModTime().UnixNano(), 10)
}

// devIno is unknown off Linux: a folder link is followed by its real path.
func devIno(os.FileInfo) (uint64, uint64, bool) { return 0, 0, false }

// writable fails closed off Linux: every file a setting names counts.
func writable(string) bool { return true }

// children are the entries of the folder at path, each stat'ed, links
// followed.
func children(path string) ([]os.FileInfo, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]os.FileInfo, 0, len(entries))
	for _, e := range entries {
		if fi, err := os.Stat(filepath.Join(path, e.Name())); err == nil {
			out = append(out, fi)
		}
	}
	return out, nil
}
