package sandbox

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Program is bubblewrap's command, as PATH's folders name it.
const Program = "bwrap"

// A Problem is why the sandbox cannot run here, and what fixes it. TimedOut
// is a probe whose bubblewrap did not end within its bound: no verdict on
// whether the sandbox runs (T128f slice 8c, L1382), so the default mode
// stays on and a command is wrapped as usual, and `jhon sandbox run` fails
// visibly if bubblewrap really cannot start.
type Problem struct {
	Reason, Fix string
	TimedOut    bool
}

// Line is the problem on one line: the reason, then the fix.
func (p *Problem) Line() string { return p.Reason + "; fix: " + p.Fix }

const (
	installFix = "install bubblewrap (pacman -S bubblewrap, apt install bubblewrap or dnf install bubblewrap)"
	trustedFix = "install bubblewrap for the whole system (pacman -S bubblewrap, apt install bubblewrap or dnf install bubblewrap), or put it in a PATH folder outside the project and the folders the sandbox opens for writing"
	// oldFix names the first bubblewrap with every option Args gives it:
	// --perms came with 0.5.0. bindFdFix is for one that does not know
	// --bind-fd or --ro-bind-fd, which a run binds every area with (T248).
	oldFix    = "update bubblewrap to 0.5.0 or newer (pacman -S bubblewrap, apt install bubblewrap or dnf install bubblewrap)"
	bindFdFix = "update bubblewrap to a release that has --bind-fd and --ro-bind-fd (pacman -S bubblewrap, apt install bubblewrap or dnf install bubblewrap)"
	usernsFix = "turn unprivileged user namespaces on (sysctl kernel.unprivileged_userns_clone=1 and user.max_user_namespaces above 0; where AppArmor restricts them, a profile that lets bwrap use them)"
)

// Host is what Check reads of the machine; a test gives its own.
type Host struct {
	OS       string
	ReadFile func(string) ([]byte, error)
	Stat     func(string) (os.FileInfo, error)
}

// ThisHost is this machine.
func ThisHost() Host {
	return Host{OS: runtime.GOOS, ReadFile: os.ReadFile, Stat: os.Stat}
}

// userNamespaceSwitches are the kernel's switches for unprivileged user
// namespaces, off at 0; a kernel without one has no such switch.
var userNamespaceSwitches = []string{"/proc/sys/user/max_user_namespaces", "/proc/sys/kernel/unprivileged_userns_clone"}

// find is the bubblewrap the sandbox of in starts with: the first executable
// bwrap in a PATH folder that lies outside every area a sandboxed command
// may write (writable), the project included, and that is not a link into
// one. A PATH lookup would take the first on PATH, and a folder a command
// may write can sit before /usr/bin there: a bwrap planted in it would run
// outside the sandbox, and the program never (L888, F1). refused is the
// first PATH folder whose bwrap was passed over for that.
func find(h Host, in Input) (path, refused string) {
	open := writable(in)
	for _, dir := range in.Path {
		if !filepath.IsAbs(dir) {
			continue
		}
		file, isDir, ok := in.Stat(filepath.Join(dir, Program))
		if !ok || isDir {
			continue
		}
		if fi, err := h.Stat(file); err != nil || fi.Mode()&0o111 == 0 {
			continue
		}
		if folder, _, _ := in.Stat(dir); !within(folder, open) && !within(file, open) {
			return file, ""
		}
		if refused == "" {
			refused = dir
		}
	}
	return "", refused
}

// Check is what `jhon sandbox run` checks before it runs anything, without
// starting a process (D3, design section 5): the system is Linux, a
// bubblewrap Jhon may trust is on PATH (find), and no kernel switch turns
// unprivileged user namespaces off (a setuid bubblewrap does not need them).
// It gives that bubblewrap's real path, and no problem when nothing stands in
// the way; where bubblewrap then cannot start, it says so itself and runs
// nothing.
func Check(h Host, in Input) (bwrap string, problem *Problem) {
	if h.OS != "linux" {
		return "", &Problem{"the sandbox does not run on this system (" + h.OS + ")", "none on this system: it needs Linux and bubblewrap", false}
	}
	bwrap, refused := find(h, in)
	if bwrap == "" && refused != "" {
		return "", &Problem{"bubblewrap (" + Program + ") is on PATH only in a folder a sandboxed command may write (" + refused + ")", trustedFix, false}
	}
	if bwrap == "" {
		return "", &Problem{"bubblewrap (" + Program + ") is not on PATH", installFix, false}
	}
	if fi, err := h.Stat(bwrap); err == nil && fi.Mode()&os.ModeSetuid != 0 {
		return bwrap, nil
	}
	for _, file := range userNamespaceSwitches {
		if b, err := h.ReadFile(file); err == nil && strings.TrimSpace(string(b)) == "0" {
			name := strings.ReplaceAll(strings.TrimPrefix(file, "/proc/sys/"), "/", ".")
			return "", &Problem{"unprivileged user namespaces are off (" + name + " = 0)", usernsFix, false}
		}
	}
	return bwrap, nil
}

// Probe is Check, then one empty sandbox started for real with the
// bubblewrap Check gave and one of every option a run gives it (probeArgs),
// bounded to 5 seconds: what `jhon sandbox status` and `jhon doctor` report,
// and what turns the default mode on on Linux (Mode).
// A bubblewrap that does not know one of the options is too old, and would
// fail every run. Its binds are opened as a run opens them (Open), so where
// openat2 is missing or refused the probe fails as a run would (T248).
func Probe(h Host, in Input) *Problem {
	bwrap, p := Check(h, in)
	if p != nil {
		return p
	}
	return started(bwrap)
}

// probeBound is how long Probe waits for its empty sandbox.
const probeBound = 5 * time.Second

// started is Probe's start of one empty sandbox with bwrap (probe). A test
// gives its own.
var started = probe

// probe starts one empty sandbox with bwrap: nil when it ran, else why it
// did not and the fix.
func probe(bwrap string) *Problem {
	tmp := "/tmp"
	if real, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = real
	}
	b := &Bound{}
	defer b.Close()
	for range 2 {
		f, err := openNoLink(tmp)
		if p := openat2Problem(err); p != nil {
			return p
		}
		if err != nil {
			return &Problem{"the sandbox cannot open " + tmp + " with no link followed (" + err.Error() + ")", "run jhon sandbox status again; a sandboxed command fails the same way until " + tmp + " opens", false}
		}
		b.Files = append(b.Files, f)
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeBound)
	defer cancel()
	args := probeArgs(int(b.Files[0].Fd()), int(b.Files[1].Fd()), tmp)
	cmd := exec.CommandContext(ctx, bwrap, append(args, "true")...)
	cmd.ExtraFiles = b.ExtraFiles()
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &Problem{"bubblewrap did not start a sandbox within " + probeBound.String() + ", which says nothing on whether it can", "run jhon sandbox status again; a sandboxed command fails with bubblewrap's own message if it cannot start", true}
	}
	said, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if said == "" {
		said = err.Error()
	}
	if strings.Contains(said, "Unknown option") {
		fix := oldFix
		if strings.Contains(said, "-bind-fd") {
			fix = bindFdFix
		}
		return &Problem{"bubblewrap is too old for the sandbox (" + said + ")", fix, false}
	}
	return &Problem{"bubblewrap cannot start a sandbox (" + said + ")", usernsFix, false}
}
