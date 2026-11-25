package integrations

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// prSetChildSubreaper is prctl's PR_SET_CHILD_SUBREAPER.
const prSetChildSubreaper = 36

// reapMu keeps one reaping run at a time: jhon is the subreaper only for it.
var reapMu sync.Mutex

// reaping runs run with this process a child subreaper, so whatever the
// program it starts leaves behind, a setsid daemon too, becomes this
// process's child when its parent ends; then it kills and reaps every child
// that was not this process's before, until none is left. Where the
// subreaper cannot be set, run runs as it is (its process group is still
// killed).
func reaping(run func() error) error {
	reapMu.Lock()
	defer reapMu.Unlock()
	before := map[int]bool{}
	for _, pid := range children() {
		before[pid] = true
	}
	if _, _, e := syscall.RawSyscall(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0); e != 0 {
		return run()
	}
	defer syscall.RawSyscall(syscall.SYS_PRCTL, prSetChildSubreaper, 0, 0)
	err := run()
	for range 100 {
		var left []int
		for _, pid := range children() {
			if !before[pid] {
				left = append(left, pid)
			}
		}
		if len(left) == 0 {
			break
		}
		for _, pid := range left {
			syscall.Kill(pid, syscall.SIGKILL)
		}
		for _, pid := range left {
			var ws syscall.WaitStatus
			syscall.Wait4(pid, &ws, 0, nil)
		}
	}
	return err
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
