package release

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jachavarro2004/jhon-agent/engine/internal/render"
)

// The renderer's operations, one function each, over internal/render:
// its output and exit code. Each names the tree it renders from: doctor the source,
// sync the release (the binary's renderer judges the release's files).

// HasNode is whether node is on PATH. Only `jhon test`
// still needs it: the renderer is Go.
func HasNode() bool {
	_, err := exec.LookPath("node")
	return err == nil
}

// combined is a render call whose stdout and stderr are interleaved into one
// substituted output, as `$(… 2>&1)` keeps them, and whether it returned 0.
func combined(call func(stdout, stderr io.Writer) int) (string, bool) {
	var b bytes.Buffer
	code := call(&b, &b)
	return Substituted(b.Bytes()), code == 0
}

// Validate is the renderer's --validate in the tree at root.
func Validate(root string) (string, bool) {
	return combined(func(o, e io.Writer) int { return render.Validate(root, o, e) })
}

// Check is the renderer's --check in the tree at root; its verdict is in the
// output.
func Check(root string) string {
	out, _ := combined(func(o, e io.Writer) int { return render.Check(root, o, e) })
	return out
}

// Preflight is the release's own renderer's `--preflight --source <source>`:
// validate the release and the install it would make, writing nothing. The
// release's binary judges it, never this process's, so a release that adds
// a command or changes the render is checked by its own code.
func Preflight(release, source string) (string, bool) {
	out, err := releaseRenderer(release, "--preflight", "--source", sourceOf(release, source)).CombinedOutput()
	return Substituted(out), err == nil
}

// Install is the renderer's `--install --source <source>` run by the binary
// of the release at current, its output on this process's; whether it
// succeeded. The release's own code writes what it projects.
func Install(current, source string) bool {
	return InstallTo(current, source, os.Stdout, os.Stderr)
}

// InstallTo is Install with the renderer's output on stdout and stderr
// instead of this process's: a TUI keeps it in a buffer, `jhon update
// --json` sends it to stderr. Its stdin is empty.
func InstallTo(current, source string, stdout, stderr io.Writer) bool {
	c := releaseRenderer(current, "--install", "--source", sourceOf(current, source))
	c.Stdout, c.Stderr = stdout, stderr
	return statusTo(c.Run(), stderr) == 0
}

// sourceOf is the --source a render protects with framework-guard: the
// maintainer's checkout, or, on a user's install with no checkout (L51),
// the release itself, so nothing rendered names a source path that does
// not exist.
func sourceOf(release, source string) string {
	if IsFile(filepath.Join(source, SourceMarker)) {
		return source
	}
	if p, ok := ReadlinkF(release); ok {
		return p
	}
	return release
}

// releaseRenderer is `<release>/build/jhon render-engine <args>` rooted at
// the release.
func releaseRenderer(release string, args ...string) *exec.Cmd {
	c := exec.Command(filepath.Join(release, Binary), append([]string{"render-engine"}, args...)...)
	c.Env = append(os.Environ(), "JHON_ROOT="+release)
	return c
}

// RuntimeDoctor is `jhon doctor --runtime <args>` in the tree at root, its
// output on this process's; its exit code.
func RuntimeDoctor(root string, args []string) int {
	return render.RuntimeDoctor(root, args, os.Stdout, os.Stderr)
}

// Run runs c on this process's stdio and returns its code by the shell's
// convention: the exit code, or 128 + the signal that killed it; a program
// that could not start is said on stderr, 126 when it is not executable and
// 127 otherwise.
func Run(c *exec.Cmd) int {
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return Status(c.Run())
}

// Status is the code Run returns for a command's error.
func Status(err error) int { return statusTo(err, os.Stderr) }

// statusTo is Status saying a program that could not start on stderr.
func statusTo(err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		fmt.Fprintf(stderr, "jhon: %s\n", startError(err))
		if errors.Is(err, fs.ErrPermission) {
			return 126
		}
		return 127
	}
	if ws, ok := exit.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return exit.ExitCode()
}

// startError is the shell's report of a program that could not start:
// `<path>: <reason>`, the reason as strerror spells it.
func startError(err error) string {
	var path *fs.PathError
	var errno syscall.Errno
	if errors.As(err, &path) && errors.As(err, &errno) {
		reason := errno.Error()
		return path.Path + ": " + strings.ToUpper(reason[:1]) + reason[1:]
	}
	var e *exec.Error
	if errors.As(err, &e) {
		return e.Name + ": command not found"
	}
	return err.Error()
}
