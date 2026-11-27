package cli

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// fileIdentity is what a file too big to read is compared by: its size,
// modification and change times and inode. The kernel sets the change
// time on every write, rename or chmod and no user can set it back, so a
// same-size edit with its mtime restored still differs.
func fileIdentity(fi os.FileInfo) string {
	size, mtime, ctime, dev, ino, ok := statOf(fi)
	if !ok {
		return "size " + strconv.FormatInt(fi.Size(), 10) + ", mtime " + strconv.FormatInt(fi.ModTime().UnixNano(), 10)
	}
	b := make([]byte, 0, 96)
	b = append(b, "size "...)
	b = strconv.AppendInt(b, size, 10)
	b = append(b, ", mtime "...)
	b = strconv.AppendInt(b, mtime, 10)
	b = append(b, ", ctime "...)
	b = strconv.AppendInt(b, ctime, 10)
	b = append(b, ", inode "...)
	b = strconv.AppendUint(b, dev, 10)
	b = append(b, ':')
	b = strconv.AppendUint(b, ino, 10)
	return string(b)
}

// statOf is a stat's size, times (ns), device and inode.
func statOf(fi os.FileInfo) (size, mtime, ctime int64, dev, ino uint64, ok bool) {
	switch st := fi.Sys().(type) {
	case *syscall.Stat_t:
		return st.Size, st.Mtim.Nano(), st.Ctim.Nano(), uint64(st.Dev), uint64(st.Ino), true
	case *unix.Stat_t:
		return st.Size, st.Mtim.Nano(), st.Ctim.Nano(), uint64(st.Dev), uint64(st.Ino), true
	}
	return 0, 0, 0, 0, 0, false
}

// devIno is a folder's device and inode, to follow each link to it once.
func devIno(fi os.FileInfo) (uint64, uint64, bool) {
	_, _, _, dev, ino, ok := statOf(fi)
	return dev, ino, ok
}

// writable is whether the owner can write path or the folder that holds
// it: a file outside the root that a setting names counts only then.
func writable(path string) bool {
	return unix.Access(path, unix.W_OK) == nil || unix.Access(filepath.Dir(path), unix.W_OK) == nil
}

// children are the entries of the folder at path, each stat'ed as os.Stat
// does (links followed) but from the folder's descriptor, which spares the
// kernel the whole path per entry: a folder of 20,000 files a sandboxed
// command planted costs a start what their stats cost, no more.
func children(path string) ([]os.FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	fd := int(f.Fd())
	out := make([]os.FileInfo, 0, len(names))
	for _, name := range names {
		si := &statInfo{name: name}
		if unix.Fstatat(fd, name, &si.st, 0) != nil {
			continue
		}
		out = append(out, si)
	}
	return out, nil
}

// statInfo is the os.FileInfo of a Stat_t.
type statInfo struct {
	name string
	st   unix.Stat_t
}

func (s *statInfo) Name() string       { return s.name }
func (s *statInfo) Size() int64        { return s.st.Size }
func (s *statInfo) ModTime() time.Time { return time.Unix(s.st.Mtim.Unix()) }
func (s *statInfo) IsDir() bool        { return s.st.Mode&unix.S_IFMT == unix.S_IFDIR }
func (s *statInfo) Sys() any           { return &s.st }
func (s *statInfo) Mode() os.FileMode {
	m := os.FileMode(s.st.Mode & 0o777)
	switch s.st.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		m |= os.ModeDir
	case unix.S_IFLNK:
		m |= os.ModeSymlink
	case unix.S_IFIFO:
		m |= os.ModeNamedPipe
	case unix.S_IFSOCK:
		m |= os.ModeSocket
	case unix.S_IFCHR:
		m |= os.ModeDevice | os.ModeCharDevice
	case unix.S_IFBLK:
		m |= os.ModeDevice
	}
	return m
}
