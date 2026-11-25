package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// mkdirUnder makes folder, which lies under home (a real path), and its
// missing parents: from / it opens each part with no link followed
// (O_NOFOLLOW), the home's parts included, and makes each missing part below
// the home in the folder it opened (mkdirat), so a link anywhere on the way,
// also one that appears after MissingAllowWrite looked, stops it with an
// error and nothing is made where the link leads.
func mkdirUnder(home, folder string) error {
	rel, err := filepath.Rel(home, folder)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || !filepath.IsAbs(home) {
		return errors.New(folder + " is not under the home folder " + home)
	}
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return &os.PathError{Op: "open", Path: "/", Err: err}
	}
	defer func() { syscall.Close(fd) }()
	cur := "/"
	step := func(part string, make bool) error {
		cur = filepath.Join(cur, part)
		if make {
			if err := syscall.Mkdirat(fd, part, 0o755); err != nil && !errors.Is(err, syscall.EEXIST) {
				return &os.PathError{Op: "mkdir", Path: cur, Err: err}
			}
		}
		next, err := syscall.Openat(fd, part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if err != nil {
			return &os.PathError{Op: "open", Path: cur, Err: err}
		}
		syscall.Close(fd)
		fd = next
		return nil
	}
	for _, part := range strings.Split(strings.TrimPrefix(home, "/"), "/") {
		if part == "" {
			continue
		}
		if err := step(part, false); err != nil {
			return err
		}
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if err := step(part, true); err != nil {
			return err
		}
	}
	return nil
}
