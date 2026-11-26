package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
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
// (openNoLink), a writable one only through the path its checks judged,
// and checks it on the descriptor as layout judged its path:
// a folder is still a folder and a file still a file, and a writable folder
// is not the root, the home folder or a folder above it.
func openBind(in Input, m mount) (*os.File, *Problem) {
	// A writable area is opened only through the path its checks judged
	// (mount.written): the real path layout binds was read at another
	// moment, and a parent a concurrent run swapped for a link in between
	// would make it another folder, one with no link of its own (L4). With
	// no link on the written path, it is the real path; any other answer
	// is a change, and the run is refused. The open below follows no link,
	// so the descriptor is what the written path held then.
	if m.kind == write {
		if len(m.written) == 0 {
			return nil, changedProblem(m, m.path, "it was never checked")
		}
		for _, w := range m.written {
			if w != m.path {
				return nil, changedProblem(m, w, "it is or passes through a link now")
			}
		}
	}
	f, err := openNoLink(m.path)
	if p := openat2Problem(err); p != nil {
		return nil, p
	}
	switch {
	case errors.Is(err, syscall.ELOOP), errors.Is(err, syscall.EXDEV):
		return nil, changedProblem(m, m.path, "it is or passes through a link now")
	case errors.Is(err, syscall.ENOENT):
		return nil, changedProblem(m, m.path, "it is gone")
	case errors.Is(err, syscall.ENOTDIR):
		return nil, changedProblem(m, m.path, "a folder on its way is no longer a folder")
	case err != nil:
		return nil, changedProblem(m, m.path, "it cannot be opened ("+err.Error()+")")
	}
	var st syscall.Stat_t
	if err := syscall.Fstat(int(f.Fd()), &st); err != nil {
		f.Close()
		return nil, changedProblem(m, m.path, "it cannot be read ("+err.Error()+")")
	}
	isDir := st.Mode&syscall.S_IFMT == syscall.S_IFDIR
	switch {
	case m.dir && !isDir:
		f.Close()
		return nil, changedProblem(m, m.path, "it is no longer a folder")
	case !m.dir && isDir:
		f.Close()
		return nil, changedProblem(m, m.path, "it is a folder now")
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
				return nil, changedProblem(m, m.path, "it is the root, the home folder or a folder above it now")
			}
		}
	}
	return f, nil
}

// dupHigh is a duplicate of f at 3 or above, close-on-exec.
func dupHigh(f *os.File) (*os.File, error) {
	fd, err := fcntl(int(f.Fd()), syscall.F_DUPFD_CLOEXEC, 3)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), f.Name()), nil
}

// openSelf opens jhon's own program, the file this process runs
// (/proc/self/exe), for enter: bubblewrap runs it as /proc/self/fd/<n>, so
// what checks the sandbox is this very program, whatever a path to it
// leads to by then.
func (b *Bound) openSelf() *Problem {
	fd, err := syscall.Open("/proc/self/exe", syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
	if err == nil && fd < 3 {
		low := fd
		fd, err = fcntl(low, syscall.F_DUPFD_CLOEXEC, 3)
		syscall.Close(low)
	}
	if err != nil {
		return &Problem{Reason: "jhon cannot open its own program (/proc/self/exe: " + err.Error() + "), which checks the sandbox before the program starts", Fix: "run jhon where /proc is mounted"}
	}
	b.self = os.NewFile(uintptr(fd), "/proc/self/exe")
	b.Keep = append(b.Keep, b.self)
	return nil
}

// Inherit clears close-on-exec on b's files, for a run that replaces jhon
// with bubblewrap (syscall.Exec) and so passes them at their own numbers,
// the ones kept for enter too; every other descriptor of jhon's stays
// close-on-exec.
func (b *Bound) Inherit() error {
	for _, f := range b.all() {
		if _, err := fcntl(int(f.Fd()), syscall.F_SETFD, 0); err != nil {
			return err
		}
	}
	return nil
}

// close_range's number (the generic table) and its CLOSE_RANGE_CLOEXEC flag.
const (
	sysCloseRange     = 436
	closeRangeCloexec = 1 << 2
)

// CloseInherited marks every descriptor of this process above 2
// close-on-exec, so none a caller left open (a host folder's) reaches the
// program bubblewrap starts (T248, L4): bubblewrap keeps what it inherits.
// Call it before Open, whose descriptors Bound passes on itself. It uses
// close_range (Linux 5.11) and, where that is missing, each entry of
// /proc/self/fd; an error is a descriptor it could not mark.
func CloseInherited() error {
	if _, _, e := syscall.Syscall(sysCloseRange, 3, ^uintptr(0), closeRangeCloexec); e == 0 {
		return nil
	}
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return err
	}
	for _, e := range entries {
		n, err := strconv.Atoi(e.Name())
		if err != nil || n <= 2 {
			continue
		}
		if _, err := fcntl(n, syscall.F_SETFD, syscall.FD_CLOEXEC); err != nil && err != syscall.EBADF {
			return err
		}
	}
	return nil
}

// anchorProblem is why the project's top, in.Roots[0], is not the folder
// the project was taken from (in.ProjectFrom), nil when it is or there is
// no project. The top's path is read through every link by the project's
// code (project.Realish), so a parent a concurrent run swapped for a link
// to the home in that moment names a home folder with no link of its own,
// which every later check passes (L4). Compared by device and inode, with
// no link followed: for ".", the top must be this process's working folder
// or a folder above it, walked up through ".." from the folder itself,
// which no rename moves; for a --project value, the top must be the folder
// the value names as written (the home's own way taken, homed), with no
// link on it.
func anchorProblem(in Input) *Problem {
	if len(in.Roots) == 0 || in.Roots[0] == "" {
		return nil
	}
	top := filepath.Clean(in.Roots[0])
	from := in.ProjectFrom
	if from == "" {
		return projectProblem(top, top, "was not read from a folder Jhon can check it against")
	}
	f, err := openNoLink(in.homed(top))
	if p := openat2Problem(err); p != nil {
		return p
	}
	if err != nil {
		return projectProblem(top, top, "changed after Jhon read it: it cannot be opened with no link followed ("+err.Error()+")")
	}
	defer f.Close()
	var want syscall.Stat_t
	if err := syscall.Fstat(int(f.Fd()), &want); err != nil {
		return projectProblem(top, top, "cannot be read ("+err.Error()+")")
	}
	same := func(st *syscall.Stat_t) bool { return st.Dev == want.Dev && st.Ino == want.Ino }
	if p := mainProblem(in); p != nil {
		return p
	}
	if from != "." {
		g, err := openNoLink(in.homed(filepath.Clean(from)))
		if p := openat2Problem(err); p != nil {
			return p
		}
		if err != nil {
			return projectProblem(top, from, "is pinned as "+from+", which is or passes through a link or cannot be opened ("+err.Error()+"): pass the project's real path to --project")
		}
		defer g.Close()
		var st syscall.Stat_t
		if syscall.Fstat(int(g.Fd()), &st) != nil || !same(&st) {
			return projectProblem(top, from, "is not the pinned project "+from+": a folder on its way changed after Jhon read it")
		}
		return nil
	}
	cur, err := syscall.Open(".", oPath|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return projectProblem(top, ".", "cannot be checked against the folder this command runs in ("+err.Error()+")")
	}
	for range 4096 {
		var st syscall.Stat_t
		if err := syscall.Fstat(cur, &st); err != nil {
			break
		}
		if same(&st) {
			syscall.Close(cur)
			return nil
		}
		up, err := syscall.Openat(cur, "..", oPath|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		syscall.Close(cur)
		if err != nil {
			return projectProblem(top, ".", "cannot be checked against the folder this command runs in ("+err.Error()+")")
		}
		var at syscall.Stat_t
		if syscall.Fstat(up, &at) == nil && at.Dev == st.Dev && at.Ino == st.Ino {
			// The root: its ".." is itself.
			syscall.Close(up)
			return projectProblem(top, ".", "is neither the folder this command runs in nor one that holds it: a folder on its way changed after Jhon read it")
		}
		cur = up
	}
	syscall.Close(cur)
	return projectProblem(top, ".", "is neither the folder this command runs in nor one that holds it: a folder on its way changed after Jhon read it")
}

// mainProblem is why in.Main, the main worktree of a linked one as the
// project's code read it (through every link, at its own moment), is not the
// folder that holds the common dir, nil when it is or there is none (L4):
// compared by device and inode with the common dir's "..", both opened with
// no link followed.
func mainProblem(in Input) *Problem {
	if in.Main == "" {
		return nil
	}
	main := filepath.Clean(in.Main)
	differs := "is not the folder that holds the git common dir " + in.CommonDir + ": a folder on its way changed after Jhon read it"
	if in.CommonDir == "" {
		return projectProblem(main, main, differs)
	}
	f, err := openNoLink(in.homed(main))
	if p := openat2Problem(err); p != nil {
		return p
	}
	if err != nil {
		return projectProblem(main, main, "changed after Jhon read it: it cannot be opened with no link followed ("+err.Error()+")")
	}
	defer f.Close()
	c, err := openNoLink(in.homed(filepath.Clean(in.CommonDir)))
	if p := openat2Problem(err); p != nil {
		return p
	}
	if err != nil {
		return projectProblem(main, in.CommonDir, differs)
	}
	defer c.Close()
	up, err := syscall.Openat(int(c.Fd()), "..", oPath|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return projectProblem(main, in.CommonDir, differs)
	}
	defer syscall.Close(up)
	var a, b syscall.Stat_t
	if syscall.Fstat(int(f.Fd()), &a) != nil || syscall.Fstat(up, &b) != nil || a.Dev != b.Dev || a.Ino != b.Ino {
		return projectProblem(main, in.CommonDir, differs)
	}
	return nil
}

// pinnedAs is whether real, a pinned project's real path read through every
// link, is the folder written names opened with no link followed (in.homed:
// the home's own way taken), by device and inode (ProjectFolder). Where
// openat2 is missing or refused it says yes: the sandbox cannot run there,
// and a run fails closed on its own.
func pinnedAs(in Input, real, written string) bool {
	w, err := openNoLink(in.homed(filepath.Clean(written)))
	if openat2Problem(err) != nil {
		return true
	}
	if err != nil {
		return false
	}
	defer w.Close()
	r, err := openNoLink(real)
	if err != nil {
		return false
	}
	defer r.Close()
	var a, b syscall.Stat_t
	return syscall.Fstat(int(w.Fd()), &a) == nil && syscall.Fstat(int(r.Fd()), &b) == nil && a.Dev == b.Dev && a.Ino == b.Ino
}
