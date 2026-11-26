package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// openHow is openat2's struct open_how.
type openHow struct{ Flags, Mode, Resolve uint64 }

// openat2's number (the generic table: amd64, arm64 and the others share
// it), its resolve flags, and open's O_PATH, which syscall does not name.
const (
	sysOpenat2          = 437
	resolveNoMagiclinks = 0x02
	resolveNoSymlinks   = 0x04
	resolveBeneath      = 0x08
	oPath               = 0x200000
)

// openat2 opens path, absolute and clean, as an O_PATH descriptor,
// close-on-exec, from the root with no link on the way followed, the last
// part included (RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS).
// A rename racing the walk (EAGAIN) is tried again a few times. A test
// replaces it.
var openat2 = func(path string) (int, error) {
	root, err := syscall.Open("/", oPath|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	defer syscall.Close(root)
	rel := strings.TrimPrefix(filepath.Clean(path), "/")
	if rel == "" {
		rel = "."
	}
	p, err := syscall.BytePtrFromString(rel)
	if err != nil {
		return -1, err
	}
	how := openHow{Flags: oPath | syscall.O_CLOEXEC, Resolve: resolveBeneath | resolveNoSymlinks | resolveNoMagiclinks}
	for range 8 {
		fd, _, e := syscall.Syscall6(sysOpenat2, uintptr(root), uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&how)), unsafe.Sizeof(how), 0, 0)
		if e == syscall.EAGAIN {
			continue
		}
		if e != 0 {
			return -1, e
		}
		return int(fd), nil
	}
	return -1, syscall.EAGAIN
}

// openNoLink is openat2's descriptor of path as a file, at 3 or above so
// the standard three of the program bubblewrap starts are never one.
func openNoLink(path string) (*os.File, error) {
	fd, err := openat2(path)
	if err != nil {
		return nil, err
	}
	if fd < 3 {
		low := fd
		fd, err = fcntl(low, syscall.F_DUPFD_CLOEXEC, 3)
		syscall.Close(low)
		if err != nil {
			return nil, err
		}
	}
	return os.NewFile(uintptr(fd), path), nil
}

// fcntl is fcntl(2) on fd.
func fcntl(fd, cmd, arg int) (int, error) {
	v, _, e := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(cmd), uintptr(arg))
	if e != 0 {
		return -1, e
	}
	return int(v), nil
}

// openBind opens m, a bind of in's layout, with no link followed
// (openNoLink), and checks it on the descriptor as layout judged its path:
// a folder is still a folder and a file still a file, and a writable folder
// is not the root, the home folder or a folder above it.
func openBind(in Input, m mount) (*os.File, *Problem) {
	f, err := openNoLink(m.path)
	if p := openat2Problem(err); p != nil {
		return nil, p
	}
	switch {
	case errors.Is(err, syscall.ELOOP), errors.Is(err, syscall.EXDEV):
		return nil, changedProblem(m, "it is or passes through a link now")
	case errors.Is(err, syscall.ENOENT):
		return nil, changedProblem(m, "it is gone")
	case errors.Is(err, syscall.ENOTDIR):
		return nil, changedProblem(m, "a folder on its way is no longer a folder")
	case err != nil:
		return nil, changedProblem(m, "it cannot be opened ("+err.Error()+")")
	}
	var st syscall.Stat_t
	if err := syscall.Fstat(int(f.Fd()), &st); err != nil {
		f.Close()
		return nil, changedProblem(m, "it cannot be read ("+err.Error()+")")
	}
	isDir := st.Mode&syscall.S_IFMT == syscall.S_IFDIR
	switch {
	case m.dir && !isDir:
		f.Close()
		return nil, changedProblem(m, "it is no longer a folder")
	case !m.dir && isDir:
		f.Close()
		return nil, changedProblem(m, "it is a folder now")
	}
	if m.kind == write && isDir {
		above := []string{"/"}
		home := in.Home
		if r, _, ok := in.Stat(home); ok {
			home = r
		}
		for p := filepath.Clean(home); filepath.IsAbs(p) && p != "/"; p = filepath.Dir(p) {
			above = append(above, p)
		}
		for _, p := range above {
			var at syscall.Stat_t
			if syscall.Stat(p, &at) == nil && at.Dev == st.Dev && at.Ino == st.Ino {
				f.Close()
				return nil, changedProblem(m, "it is the root, the home folder or a folder above it now")
			}
		}
	}
	return f, nil
}

// Inherit clears close-on-exec on b's files, for a run that replaces jhon
// with bubblewrap (syscall.Exec) and so passes them at their own numbers;
// every other descriptor of jhon's stays close-on-exec.
func (b *Bound) Inherit() error {
	for _, f := range b.Files {
		if _, err := fcntl(int(f.Fd()), syscall.F_SETFD, 0); err != nil {
			return err
		}
	}
	return nil
}
