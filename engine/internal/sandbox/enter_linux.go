package sandbox

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// stRdonly is statfs's ST_RDONLY: the mount is read-only.
const stRdonly = 0x1

// Enter is `jhon sandbox enter args`: it checks the sandbox (enterProblem)
// and replaces this process with the program (execvp), or gives the exit
// code: 2 for arguments it does not take, 126 when the sandbox is not what
// Jhon checked, and bubblewrap's 1 when the program cannot start.
func Enter(args []string) int {
	expects, program, ok := parseEnter(args)
	if !ok {
		os.Stderr.WriteString(enterUsage)
		return 2
	}
	if why := enterProblem(expects); why != "" {
		os.Stderr.WriteString(why)
		return 126
	}
	// The descriptors it checked with, and the one it runs from, end here:
	// the program holds only what bubblewrap would have left it.
	if err := CloseInherited(); err != nil {
		fmt.Fprintf(os.Stderr, "jhon sandbox: the descriptors of the sandbox's check cannot be closed for the program (%v), so nothing was run; fix: run the command again\n", err)
		return 126
	}
	return execvp(program)
}

// enterProblem is enter's note when the sandbox is not the one Jhon set up,
// "" when it is: / is read-only (the machine's root, bound read-only: not
// so outside Jhon's sandbox), and each expectation's destination, opened
// from / with no link followed, is a mount root (its mount is not its
// parent's), its folder or file is the one the expectation's descriptor
// holds (device and inode), and it is read-only where the expectation says
// so.
func enterProblem(expects []expectation) string {
	var root syscall.Statfs_t
	if err := syscall.Statfs("/", &root); err != nil || root.Flags&stRdonly == 0 {
		return "jhon sandbox: / is not read-only, so this is not Jhon's sandbox and nothing was run; fix: run the program with jhon sandbox run\n"
	}
	for _, e := range expects {
		if !mountedAsChecked(e) {
			return enterRefused(e.dest)
		}
	}
	return ""
}

// mountedAsChecked is whether e holds (enterProblem).
func mountedAsChecked(e expectation) bool {
	var want syscall.Stat_t
	if err := syscall.Fstat(e.fd, &want); err != nil {
		return false
	}
	fd, err := openat2(e.dest)
	if err != nil {
		return false
	}
	defer syscall.Close(fd)
	var got syscall.Stat_t
	if err := syscall.Fstat(fd, &got); err != nil || got.Dev != want.Dev || got.Ino != want.Ino {
		return false
	}
	if e.ro {
		var fs syscall.Statfs_t
		if err := syscall.Fstatfs(fd, &fs); err != nil || fs.Flags&stRdonly == 0 {
			return false
		}
	}
	// A mount root: its mount is not the one of the folder that holds it.
	// A folder's ".." from its descriptor; a file's folder by its path, with
	// no link followed.
	var parent int
	if got.Mode&syscall.S_IFMT == syscall.S_IFDIR {
		parent, err = syscall.Openat(fd, "..", oPath|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	} else {
		parent, err = openat2(filepath.Dir(e.dest))
	}
	if err != nil {
		return false
	}
	defer syscall.Close(parent)
	mine, ok := mountID(fd)
	theirs, ok2 := mountID(parent)
	return ok && ok2 && mine != theirs
}

// mountID is the id of the mount fd lies on (/proc/self/fdinfo/<fd>,
// mnt_id).
func mountID(fd int) (int, bool) {
	data, err := os.ReadFile("/proc/self/fdinfo/" + strconv.Itoa(fd))
	if err != nil {
		return 0, false
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		if v, ok := bytes.CutPrefix(line, []byte("mnt_id:")); ok {
			n, err := strconv.Atoi(strings.TrimSpace(string(v)))
			return n, err == nil
		}
	}
	return 0, false
}

// execvp replaces this process with argv as bubblewrap's execvp does (glibc's
// __execvpe): a name with a / is run as it is; any other is looked for in
// each PATH folder in turn (with PATH unset, /bin:/usr/bin; an empty entry
// is the working folder), past a folder where it is missing and,
// remembered, one where it may not run; a file the kernel cannot run (no #!
// line) is run by /bin/sh. When nothing runs it says so as bubblewrap does
// and gives bubblewrap's exit code, 1.
func execvp(argv []string) int {
	env := os.Environ()
	name := argv[0]
	try := func(file string) error {
		err := syscall.Exec(file, argv, env)
		if err == syscall.ENOEXEC {
			syscall.Exec("/bin/sh", append([]string{"/bin/sh", file}, argv[1:]...), env)
		}
		return err
	}
	var err error = syscall.ENOENT
	switch {
	case name == "":
	case strings.Contains(name, "/"):
		err = try(name)
	default:
		path, set := os.LookupEnv("PATH")
		if !set {
			path = "/bin:/usr/bin"
		}
		denied := false
	search:
		for _, dir := range strings.Split(path, ":") {
			file := name
			if dir != "" {
				file = dir + "/" + name
			}
			switch err = try(file); err {
			case syscall.EACCES:
				denied = true
			case syscall.ENOENT, syscall.ESTALE, syscall.ENOTDIR, syscall.ENODEV, syscall.ETIMEDOUT:
			default:
				break search
			}
		}
		if denied && (err == syscall.ENOENT || err == syscall.ESTALE || err == syscall.ENOTDIR || err == syscall.ENODEV || err == syscall.ETIMEDOUT) {
			err = syscall.EACCES
		}
	}
	// The same words bubblewrap's own failure gives (strerror's).
	said := err.Error()
	if said != "" {
		said = strings.ToUpper(said[:1]) + said[1:]
	}
	fmt.Fprintf(os.Stderr, "bwrap: execvp %s: %s\n", name, said)
	return 1
}
