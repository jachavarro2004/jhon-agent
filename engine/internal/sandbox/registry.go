package sandbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// The registry of running runs (T249, L34 "Sin procesos vivos del sandbox"):
// a line that runs outside the sandbox reads what a sandboxed command may
// write (the repository's git configuration, a program in the project), so
// it does not run while a sandboxed command that can write its project
// runs, and no such sandboxed command starts while it runs. Each run of
// `jhon sandbox run` records itself in a folder of Jhon's state (read-only
// inside the sandbox) before it starts and removes its record when it ends;
// a record whose process is gone is stale, ignored and removed. Checking
// and recording happen under one lock, so two runs never both pass.

// Run kinds: a sandboxed run, and a line that runs outside the sandbox.
const (
	KindSandboxed = "sandboxed"
	KindOutside   = "outside"
)

// RegistryWait is how long a run waits for a run of the other kind that
// shares its areas to end before it is refused.
const RegistryWait = 3 * time.Second

// RunRecord is one running run: its process (jhon's, which bubblewrap or
// the outside shell replaces on a terminal), the process's start time (so a
// reused pid is no match), its kind, and its areas, real paths: what a
// sandboxed run may write, or what an outside line reads (its project's
// folders, the folders its git works in, its working folder).
type RunRecord struct {
	PID   int      `json:"pid"`
	Start uint64   `json:"start"`
	Kind  string   `json:"kind"`
	Areas []string `json:"areas"`
}

// Registry is the folder the running runs are recorded in.
type Registry struct{ Dir string }

// ErrBusy is Enter's error when a run of the other kind that shares the
// areas still runs after the wait.
type ErrBusy struct{ Run RunRecord }

func (e ErrBusy) Error() string { return "run " + strconv.Itoa(e.Run.PID) + " is running" }

// Enter records this process as a run of kind with areas, once no live run
// of the other kind shares one of them (shares: one is the other or holds
// it), waiting up to wait and checking every 100 ms; leave removes the
// record. It fails closed: a folder, lock or record that cannot be read or
// written is an error, never a pass, and a record that cannot be read
// counts as a live run of either kind sharing every area while its process
// lives. err is ErrBusy for a run that still runs.
func (r Registry) Enter(kind string, areas []string, wait time.Duration) (leave func(), err error) {
	if r.Dir == "" || !filepath.IsAbs(r.Dir) {
		return nil, errors.New("no folder for the registry of running runs")
	}
	if err := os.MkdirAll(r.Dir, 0o700); err != nil {
		return nil, err
	}
	self := RunRecord{PID: os.Getpid(), Start: processStart(os.Getpid()), Kind: kind, Areas: areas}
	deadline := time.Now().Add(wait)
	for {
		busy, err := r.tryEnter(self)
		if err != nil {
			return nil, err
		}
		if busy == nil {
			file := r.file(self.PID)
			return func() { os.Remove(file) }, nil
		}
		if !time.Now().Before(deadline) {
			return nil, ErrBusy{*busy}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// file is the record of the run of pid.
func (r Registry) file(pid int) string {
	return filepath.Join(r.Dir, strconv.Itoa(pid)+".json")
}

// tryEnter records self unless a live run of the other kind shares one of
// its areas, which it gives; under the registry's lock.
func (r Registry) tryEnter(self RunRecord) (*RunRecord, error) {
	lock, err := os.OpenFile(filepath.Join(r.Dir, ".lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return nil, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	entries, err := os.ReadDir(r.Dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		pid, err := strconv.Atoi(name)
		if err != nil || pid == self.PID {
			continue
		}
		file := filepath.Join(r.Dir, e.Name())
		var rec RunRecord
		data, err := os.ReadFile(file)
		readable := err == nil && json.Unmarshal(data, &rec) == nil && rec.PID == pid
		if !readable {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if !alive(pid, 0) {
				os.Remove(file)
				continue
			}
			// Unreadable, its process alive: it may be anything.
			rec = RunRecord{PID: pid, Kind: "", Areas: []string{string(filepath.Separator)}}
		} else if !alive(rec.PID, rec.Start) {
			os.Remove(file)
			continue
		}
		if rec.Kind != self.Kind && shares(rec.Areas, self.Areas) {
			return &rec, nil
		}
	}
	data, err := json.Marshal(self)
	if err != nil {
		return nil, err
	}
	tmp := r.file(self.PID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, r.file(self.PID)); err != nil {
		os.Remove(tmp)
		return nil, err
	}
	return nil, nil
}

// shares is whether an area of a is one of b or holds it, or the other way
// round.
func shares(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if project.Within(x, y) || project.Within(y, x) {
				return true
			}
		}
	}
	return false
}

// processStart is the start time of process pid (/proc/<pid>/stat, field
// 22, in clock ticks since boot), 0 where it cannot be read.
func processStart(pid int) uint64 {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0
	}
	s := string(data)
	fields := strings.Fields(s[strings.LastIndex(s, ")")+1:])
	if len(fields) < 20 {
		return 0
	}
	start, _ := strconv.ParseUint(fields[19], 10, 64)
	return start
}

// alive is whether process pid runs and, with start not 0, started then: a
// pid the kernel gave a new process since is not the run's. Where neither
// /proc nor a signal can tell, it is taken as alive.
func alive(pid int, start uint64) bool {
	if pid <= 0 {
		return false
	}
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err == nil {
		return start == 0 || processStart(pid) == start
	} else if errors.Is(err, fs.ErrNotExist) {
		if _, err := os.Stat("/proc/self"); err == nil {
			return false
		}
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// WritableAreas are the real paths a sandboxed command of in may write, as
// a sandboxed run records them (Registry): the writable areas under any
// PATH and /dev.
func WritableAreas(in Input) []string { return writable(in) }

// OutsideAreas are the real paths a line that runs outside the sandbox in
// in's working folder reads, as it records them (Registry): the project's
// roots (also one that is a link), the common dir, the listed worktrees,
// the line's working folder (a leading cd's) and the folder each git on it
// works in (GitFolders).
func OutsideAreas(in Input, line string) []string {
	var out []string
	add := func(p string) {
		if filepath.IsAbs(p) {
			for _, q := range []string{filepath.Clean(p), realPath(p, in.Stat)} {
				if !contains(out, q) {
					out = append(out, q)
				}
			}
		}
	}
	for _, p := range in.Roots {
		add(p)
	}
	add(in.CommonDir)
	for _, p := range in.Worktrees {
		add(p)
	}
	add(in.Cwd)
	for _, p := range GitFolders(line, in.Cwd) {
		add(p)
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
