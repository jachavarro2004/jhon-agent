package integrations

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

// prSetChildSubreaper is prctl's PR_SET_CHILD_SUBREAPER.
const prSetChildSubreaper = 36

// A run is reaped by a helper of its own (T247, odd/tasks/phase1-security.md
// S4): this very program run again (/proc/self/exe, which names it even
// once an update replaced its file) with reaperArg, as a child subreaper.
// It starts the program in a process group of its own; whatever the
// program leaves behind, a setsid daemon too, becomes the helper's child
// when its parent ends; once the program returns, the helper kills its
// group and then kills and reaps every child it has, which are the run's
// descendants alone. jhon itself never becomes a subreaper, so what
// another of its children leaves behind is never adopted nor killed.
const (
	reaperArg = "jhon-integrations-reaper"
	reaperEnv = "JHON_INTEGRATIONS_REAPER=1"
)

// closeInherited marks every descriptor above 2 close-on-exec: one jhon
// was handed by whatever started it (a CI runner's pipes, a caller's open
// folder) never reaches the program it runs; the descriptors exec.Cmd
// passes on purpose (ExtraFiles) are duplicated afresh at the exec.
func closeInherited() {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return
	}
	for _, e := range entries {
		if fd, err := strconv.Atoi(e.Name()); err == nil && fd > 2 {
			syscall.CloseOnExec(fd)
		}
	}
}

// reaperExe is the helper's program: this one; a variable for the tests.
var reaperExe = "/proc/self/exe"

func init() {
	if len(os.Args) >= 3 && os.Args[1] == reaperArg && slices.Contains(os.Environ(), reaperEnv) {
		os.Exit(reaper(os.Args[2], os.Args[3:]))
	}
}

// reaped runs cmd through the helper: started is whether the program
// started, and err its Wait's (an *exec.ExitError for an exit other than
// 0) or, when it did not start, why (a *fs.PathError, as cmd.Start's).
// Where the helper cannot start, or ends without saying anything (the
// program at /proc/self/exe is not jhon: jhon run through ld.so), cmd runs
// as it is, its process group still killed when it returns; a helper the
// cut ended before it said anything (a TERM before its handler was set)
// started nothing, and the run is said as cut.
func reaped(ctx context.Context, cmd *exec.Cmd) (started bool, err error) {
	closeInherited()
	r, w, err := os.Pipe()
	if err != nil {
		return runGrouped(cmd)
	}
	defer r.Close()
	h := exec.CommandContext(ctx, reaperExe, append([]string{reaperArg, cmd.Path}, cmd.Args[1:]...)...)
	h.Env = append([]string{reaperEnv}, cmd.Env...)
	h.Dir, h.Stdin, h.Stdout, h.Stderr = cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr
	h.ExtraFiles = []*os.File{w}
	h.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// The cut asks the helper to kill the program's group and reap the
	// rest; WaitDelay later, the helper is killed.
	h.Cancel = func() error { return h.Process.Signal(syscall.SIGTERM) }
	h.WaitDelay = cmd.WaitDelay
	if err := h.Start(); err != nil {
		w.Close()
		return runGrouped(cmd)
	}
	w.Close()
	err = h.Wait()
	status, _ := io.ReadAll(io.LimitReader(r, 4096))
	first, msg, _ := strings.Cut(string(status), "\n")
	switch {
	case first == "started":
		return true, err
	case strings.HasPrefix(first, "error "):
		if n, perr := strconv.Atoi(strings.TrimPrefix(first, "error ")); perr == nil && n > 0 {
			return false, &fs.PathError{Op: "fork/exec", Path: cmd.Path, Err: syscall.Errno(n)}
		}
		return false, errors.New(msg)
	case ctx.Err() != nil:
		return true, err
	}
	return runGrouped(cmd)
}

// reaper is the helper: it runs program with args in the environment it
// was given less reaperEnv, says on fd 3 whether it started ("started", or
// "error <errno>" and the error), and once it returns kills its process
// group and every child left; it exits as the program did (one killed by a
// signal, killed too).
func reaper(program string, args []string) int {
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	closeInherited()
	status := os.NewFile(3, "status")
	_, _, e := syscall.RawSyscall(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0)
	subreaper := e == 0
	cmd := exec.Command(program, args...)
	cmd.Env = slices.DeleteFunc(os.Environ(), func(v string) bool { return v == reaperEnv })
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		var errno syscall.Errno
		errors.As(err, &errno)
		fmt.Fprintf(status, "error %d\n%v", int(errno), err)
		return 127
	}
	fmt.Fprint(status, "started\n")
	status.Close()
	pid := cmd.Process.Pid
	go func() {
		<-term
		syscall.Kill(-pid, syscall.SIGKILL)
	}()
	cmd.Wait()
	syscall.Kill(-pid, syscall.SIGKILL)
	if subreaper {
		for range 100 {
			left := children()
			if len(left) == 0 {
				break
			}
			for _, p := range left {
				syscall.Kill(p, syscall.SIGKILL)
			}
			for _, p := range left {
				var ws syscall.WaitStatus
				syscall.Wait4(p, &ws, 0, nil)
			}
		}
	}
	ws, _ := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if ws.Signaled() {
		syscall.Kill(os.Getpid(), syscall.SIGKILL)
	}
	return ws.ExitStatus()
}

// children are the processes whose parent is this one.
func children() []int {
	self := os.Getpid()
	es, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []int
	for _, e := range es {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		i := strings.LastIndexByte(string(b), ')')
		if f := strings.Fields(string(b[i+1:])); i >= 0 && len(f) > 1 && f[1] == strconv.Itoa(self) {
			out = append(out, pid)
		}
	}
	return out
}

// stNoexec is statfs's ST_NOEXEC flag.
const stNoexec = 0x8

// mountedNoexec is whether dir's file system is mounted noexec; a variable
// for the tests.
var mountedNoexec = func(dir string) bool {
	var st syscall.Statfs_t
	return syscall.Statfs(dir, &st) == nil && st.Flags&stNoexec != 0
}
