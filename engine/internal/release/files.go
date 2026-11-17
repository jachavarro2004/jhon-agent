package release

import (
	"fmt"
	"os"
	"path/filepath"
)

// installFile copies src over dst through a file beside it renamed into
// place, so a running program keeps the binary it started from.
func installFile(src, dst string, mode os.FileMode) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", dst, os.Getpid())
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// cacheDir is XDG_CACHE_HOME when absolute, ~/.cache otherwise.
func cacheDir() string {
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" || !filepath.IsAbs(cache) {
		cache = filepath.Join(os.Getenv("HOME"), ".cache")
	}
	return cache
}

// PiProfileDir is the pi profile Jhon's pi sessions run in (render's piHome).
func PiProfileDir() string { return filepath.Join(os.Getenv("HOME"), ".pi", "jhon") }
