package js

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// libuv's code and text for the errors a file read or write, or a spawn,
// commonly meets.
var uvErrors = map[syscall.Errno][2]string{
	syscall.ENOENT:       {"ENOENT", "no such file or directory"},
	syscall.EACCES:       {"EACCES", "permission denied"},
	syscall.EPERM:        {"EPERM", "operation not permitted"},
	syscall.EISDIR:       {"EISDIR", "illegal operation on a directory"},
	syscall.ENOTDIR:      {"ENOTDIR", "not a directory"},
	syscall.EEXIST:       {"EEXIST", "file already exists"},
	syscall.EROFS:        {"EROFS", "read-only file system"},
	syscall.ENOSPC:       {"ENOSPC", "no space left on device"},
	syscall.ELOOP:        {"ELOOP", "too many symbolic links encountered"},
	syscall.ENAMETOOLONG: {"ENAMETOOLONG", "name too long"},
	syscall.EXDEV:        {"EXDEV", "cross-device link not permitted"},
	syscall.EBUSY:        {"EBUSY", "resource busy or locked"},
	syscall.ENOEXEC:      {"ENOEXEC", "exec format error"},
	syscall.EAGAIN:       {"EAGAIN", "resource temporarily unavailable"},
	syscall.ENOMEM:       {"ENOMEM", "not enough memory"},
	syscall.E2BIG:        {"E2BIG", "argument list too long"},
}

// FSError is err with the message node's fs gives the same failure, as
// `CODE: text, syscall 'path'` (a read names no path, a rename both); an
// error it does not recognise is returned unchanged.
func FSError(err error) error {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return err
	}
	uv, ok := uvErrors[errno]
	if !ok {
		return err
	}
	msg := uv[0] + ": " + uv[1]
	var pathErr *fs.PathError
	var linkErr *os.LinkError
	switch {
	case errors.As(err, &linkErr):
		msg += ", " + linkErr.Op + " '" + linkErr.Old + "' -> '" + linkErr.New + "'"
	case errors.As(err, &pathErr) && pathErr.Op == "read":
		msg += ", read"
	case errors.As(err, &pathErr):
		msg += ", " + pathErr.Op + " '" + pathErr.Path + "'"
	}
	return errors.New(msg)
}

// SpawnCode is node's code for a spawn that did not start (ENOENT for a
// program not on PATH), or "" for an error it does not recognise.
func SpawnCode(err error) string {
	if errors.Is(err, exec.ErrNotFound) {
		return "ENOENT"
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return uvErrors[errno][0]
	}
	return ""
}

// SpawnError is the message node gives a spawn of file that did not start,
// as `<call> <file> <code>` (call is "spawn" or "spawnSync"); an error it
// does not recognise keeps its own message.
func SpawnError(call, file string, err error) string {
	if code := SpawnCode(err); code != "" {
		return call + " " + file + " " + code
	}
	return err.Error()
}

var signalNames = map[syscall.Signal]string{
	syscall.SIGHUP: "SIGHUP", syscall.SIGINT: "SIGINT", syscall.SIGQUIT: "SIGQUIT", syscall.SIGILL: "SIGILL",
	syscall.SIGTRAP: "SIGTRAP", syscall.SIGABRT: "SIGABRT", syscall.SIGBUS: "SIGBUS", syscall.SIGFPE: "SIGFPE",
	syscall.SIGKILL: "SIGKILL", syscall.SIGUSR1: "SIGUSR1", syscall.SIGSEGV: "SIGSEGV", syscall.SIGUSR2: "SIGUSR2",
	syscall.SIGPIPE: "SIGPIPE", syscall.SIGALRM: "SIGALRM", syscall.SIGTERM: "SIGTERM", syscall.SIGXCPU: "SIGXCPU",
	syscall.SIGXFSZ: "SIGXFSZ", syscall.SIGSYS: "SIGSYS",
}

// SignalName is node's name for a signal (a child's signal, a process's
// "signal" event), or its number when it has none here.
func SignalName(s syscall.Signal) string {
	if n, ok := signalNames[s]; ok {
		return n
	}
	return strconv.Itoa(int(s))
}
