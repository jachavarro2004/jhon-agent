package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// A running dashboard and its replacement (T15.16, L180): an upgrade left a
// dashboard from an older release on the port, without the newer routes.
// A dashboard says which build it is (/api/version, Build) and records itself
// at start (Record, in StateDir), so `jhon sync`, `jhon update` and
// `jhon dashboard` can prove a dashboard on a port is Jhon's (Inspect) before
// they stop it and start the current release's on the same port
// (RestartOlder). Whatever is not proven is left running and reported.

// BuildInfo is what /api/version answers: the version, the release the
// binary runs from ("" outside a release), the binary and its pid.
type BuildInfo struct {
	Version string `json:"version"`
	Release string `json:"release"`
	Binary  string `json:"binary"`
	Pid     int    `json:"pid"`
}

// Build is this server's BuildInfo; `jhon dashboard` sets it.
var Build BuildInfo

// Record is the file a dashboard writes at start and removes at a clean
// exit: its pid, port, release, binary and arguments (the flags a restart
// passes again).
type Record struct {
	Pid     int      `json:"pid"`
	Port    string   `json:"port"`
	Release string   `json:"release"`
	Binary  string   `json:"binary"`
	Args    []string `json:"args"`
}

// StateDir is where the records live:
// ${XDG_STATE_HOME:-~/.local/state}/jhon/dashboard.
func StateDir() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" || !filepath.IsAbs(state) {
		home, _ := os.UserHomeDir()
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "jhon", "dashboard")
}

func recordPath(dir, port string) string { return filepath.Join(dir, port+".json") }

// ReleaseOf is the release id binary runs from when it is
// <releases>/<id>/build/jhon, read through links; "" otherwise.
func ReleaseOf(binary, releases string) string {
	b, err1 := filepath.EvalSymlinks(binary)
	r, err2 := filepath.EvalSymlinks(releases)
	if err1 != nil || err2 != nil {
		return ""
	}
	rel, err := filepath.Rel(r, b)
	if err != nil {
		return ""
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 3 || parts[0] == ".." || parts[0] == "." || parts[1] != "build" || parts[2] != "jhon" {
		return ""
	}
	return parts[0]
}

// Serve records rec in dir, calls ready, serves h on ln until SIGTERM,
// SIGINT or SIGHUP, then removes the record (only its own) and returns nil.
func Serve(ln net.Listener, h http.Handler, dir string, rec Record, ready func()) error {
	// Catch the signals before the record exists and ready announces the
	// server: a SIGTERM sent right after ready must still remove the record.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(stop)
	if err := writeRecord(dir, rec); err != nil {
		ln.Close()
		return err
	}
	ready()
	srv := &http.Server{Handler: h}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	select {
	case <-stop:
		removeRecord(dir, rec)
		srv.Close()
		return nil
	case err := <-done:
		removeRecord(dir, rec)
		return err
	}
}

func writeRecord(dir string, rec Record) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(rec)
	tmp := recordPath(dir, rec.Port) + "." + strconv.Itoa(rec.Pid) + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, recordPath(dir, rec.Port))
}

func removeRecord(dir string, rec Record) {
	if r, ok := readRecord(dir, rec.Port); ok && r.Pid == rec.Pid {
		os.Remove(recordPath(dir, rec.Port))
	}
}

func readRecord(dir, port string) (Record, bool) {
	var r Record
	b, err := os.ReadFile(recordPath(dir, port))
	if err != nil || json.Unmarshal(b, &r) != nil || r.Pid <= 0 || r.Port != port {
		return Record{}, false
	}
	return r, true
}

// recordedPorts are the ports a record names in dir, sorted.
func recordedPorts(dir string) []string {
	es, _ := os.ReadDir(dir)
	var ports []string
	for _, e := range es {
		if p, ok := strings.CutSuffix(e.Name(), ".json"); ok && digitsOnly(p) {
			ports = append(ports, p)
		}
	}
	slices.SortFunc(ports, func(a, b string) int { x, _ := strconv.Atoi(a); y, _ := strconv.Atoi(b); return x - y })
	return ports
}

func digitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// What runs on a port, as Inspect finds it.
const (
	Absent   = "absent"   // nothing answers
	Current  = "current"  // Jhon's, proven, from the current release
	Older    = "older"    // Jhon's, proven, from another release
	Unproven = "unproven" // something answers that is not proven Jhon's
)

// Found is what Inspect found on a port: the state, the record and build
// that prove it, or what runs there and why it is not proven.
type Found struct {
	Port, State string
	Record      Record
	Running     BuildInfo
	What, Why   string
}

// Inspect is what runs on 127.0.0.1:port, against the records in dir, the
// releases directory and the current release id. Older and Current need all
// of: an answer carrying X-Jhon-Dashboard: 1 at /api/version, a record its
// pid matches, that pid alive and listening on the port, and its executable
// <releases>/<id>/build/jhon, the record's binary.
func Inspect(port, dir, releases, current string) Found {
	f := Found{Port: port, State: Unproven}
	running, answered, why := askVersion(port)
	if !answered {
		f.State = Absent
		return f
	}
	f.What = describe(port)
	if why != "" {
		f.Why = why
		return f
	}
	f.Running = running
	rec, ok := readRecord(dir, port)
	if !ok {
		f.Why = "no record of a Jhon dashboard on this port"
		return f
	}
	f.Record = rec
	if why := prove(rec, running, releases); why != "" {
		f.Why = why
		return f
	}
	f.State = Older
	if rec.Release == current {
		f.State = Current
	}
	return f
}

// askVersion asks the port's /api/version; answered is false when nothing
// accepts the connection, why says what is wrong with an answer.
func askVersion(port string) (b BuildInfo, answered bool, why string) {
	c := &http.Client{Timeout: 2 * time.Second}
	res, err := c.Get("http://127.0.0.1:" + port + "/api/version")
	if err != nil {
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" {
			return b, false, ""
		}
		return b, true, "it does not answer HTTP: " + err.Error()
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	if res.Header.Get("X-Jhon-Dashboard") != "1" {
		return b, true, "it does not say it is a Jhon dashboard"
	}
	if res.StatusCode != 200 || json.Unmarshal(body, &b) != nil || b.Pid <= 0 {
		return b, true, "a Jhon dashboard without /api/version, from a release before it"
	}
	return b, true, ""
}

// prove is why rec and running are not a Jhon dashboard proven by the
// system, "" when they are.
func prove(rec Record, running BuildInfo, releases string) string {
	if running.Pid != rec.Pid {
		return "the pid it answers is not the recorded one"
	}
	if !alive(rec.Pid) {
		return "the recorded pid is not running"
	}
	if !listens(rec.Pid, rec.Port) {
		return "the recorded pid does not listen on the port"
	}
	exe, err := os.Readlink("/proc/" + strconv.Itoa(rec.Pid) + "/exe")
	if err != nil {
		return "its executable cannot be read"
	}
	id := ReleaseOf(exe, releases)
	if id == "" || id != rec.Release || id != running.Release {
		return "its executable " + exe + " is not the recorded release's jhon"
	}
	if b, err := filepath.EvalSymlinks(rec.Binary); err != nil || b != exe {
		return "its executable " + exe + " is not the recorded binary"
	}
	return ""
}

// alive is whether pid runs (a zombie does not).
func alive(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	return i >= 0 && i+2 < len(s) && s[i+2] != 'Z' && s[i+2] != 'X'
}

// listenInodes are the socket inodes listening on port, from /proc/net.
func listenInodes(port string) map[string]bool {
	n, err := strconv.Atoi(port)
	out := map[string]bool{}
	if err != nil {
		return out
	}
	hexPort := fmt.Sprintf(":%04X", n)
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n")[1:] {
			v := strings.Fields(line)
			if len(v) > 9 && strings.HasSuffix(v[1], hexPort) && v[3] == "0A" {
				out[v[9]] = true
			}
		}
	}
	return out
}

// holds is whether pid has one of the socket inodes open.
func holds(pid int, inodes map[string]bool) bool {
	fd := "/proc/" + strconv.Itoa(pid) + "/fd"
	es, err := os.ReadDir(fd)
	if err != nil {
		return false
	}
	for _, e := range es {
		l, err := os.Readlink(filepath.Join(fd, e.Name()))
		if s, ok := strings.CutPrefix(l, "socket:["); err == nil && ok && inodes[strings.TrimSuffix(s, "]")] {
			return true
		}
	}
	return false
}

func listens(pid int, port string) bool {
	in := listenInodes(port)
	return len(in) > 0 && holds(pid, in)
}

// describe is what listens on port: "pid <n> (<executable>)" when this user
// can see it, else "a process this user cannot see".
func describe(port string) string {
	in := listenInodes(port)
	es, _ := os.ReadDir("/proc")
	for _, e := range es {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || len(in) == 0 || !holds(pid, in) {
			continue
		}
		exe, err := os.Readlink("/proc/" + e.Name() + "/exe")
		if err != nil {
			exe = "executable unknown"
		}
		return "pid " + e.Name() + " (" + exe + ")"
	}
	return "a process this user cannot see"
}

// reprove is prove as Stop calls it; a test replaces it.
var reprove = prove

// Stop ends a proven dashboard: SIGTERM, then up to wait for it to exit;
// SIGKILL only when it is still the same proven process after that. The pid
// is proven again right before each signal, so a dashboard that exited
// after Inspect and left its pid to another process is not signalled. A
// pidfd would close the window left between that proof and the signal
// (well under a millisecond: a few /proc reads), but this Go's syscall
// package has no pidfd_open on linux/amd64 or arm64 and golang.org/x/sys is
// not a dependency; a pid is reused only after the kernel cycles pid_max.
func Stop(f Found, releases string, wait time.Duration) error {
	pid := f.Record.Pid
	if why := reprove(f.Record, f.Running, releases); why != "" {
		return fmt.Errorf("pid %d is no longer proven Jhon's dashboard (%s); left running", pid, why)
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return err
	}
	if gone(pid, wait) {
		return nil
	}
	if !alive(pid) || reprove(f.Record, f.Running, releases) != "" {
		return fmt.Errorf("pid %d did not exit and is no longer proven Jhon's dashboard; left running", pid)
	}
	syscall.Kill(pid, syscall.SIGKILL)
	if !gone(pid, 2*time.Second) {
		return fmt.Errorf("pid %d did not exit", pid)
	}
	return nil
}

func gone(pid int, wait time.Duration) bool {
	for end := time.Now().Add(wait); ; {
		if !alive(pid) {
			return true
		}
		if time.Now().After(end) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Start runs binary with args detached (its own session, no terminal), and
// waits up to wait for port to answer as release.
func Start(binary string, args []string, port, release string, wait time.Duration) error {
	cmd := exec.Command(binary, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	for end := time.Now().Add(wait); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		select {
		case err := <-exited:
			return fmt.Errorf("it exited: %v", err)
		default:
		}
		if b, ok, why := askVersion(port); ok && why == "" && b.Release == release {
			return nil
		}
	}
	return fmt.Errorf("it did not answer on 127.0.0.1:%s within %s", port, wait)
}

// Short is a release id as the reports show it: its first 8 characters.
func Short(id string) string {
	if id == "" {
		return "no release"
	}
	return id[:min(8, len(id))]
}

// Unproven's report: what runs there, why it is left, how to restart it.
func (f Found) Report() string {
	return fmt.Sprintf("dashboard on 127.0.0.1:%s left running: %s, not proven to be Jhon's dashboard (%s); to restart it by hand, stop it and run 'jhon dashboard --port %s'", f.Port, f.What, f.Why, f.Port)
}

// RestartOlder is the restart `jhon sync` and `jhon update` run once current
// is switched: on every port a record in dir names, a proven dashboard from
// a release other than current is stopped and current's binary started
// there with the same arguments, one line each; an unproven one is
// reported and left running; a record of a pid that no longer runs is
// removed; with no dashboard nothing is started.
func RestartOlder(w io.Writer, dir, releases, current string) {
	if current == "" {
		return
	}
	for _, port := range recordedPorts(dir) {
		f := Inspect(port, dir, releases, current)
		switch f.State {
		case Absent:
			if rec, ok := readRecord(dir, port); ok && !alive(rec.Pid) {
				os.Remove(recordPath(dir, port))
			}
		case Unproven:
			fmt.Fprintln(w, f.Report())
		case Older:
			binary := filepath.Join(releases, current, "build", "jhon")
			if err := Stop(f, releases, 5*time.Second); err != nil {
				fmt.Fprintf(w, "dashboard on 127.0.0.1:%s (%s) was not restarted: %v\n", port, Short(f.Record.Release), err)
				continue
			}
			if err := Start(binary, f.Record.Args, port, current, 5*time.Second); err != nil {
				fmt.Fprintf(w, "dashboard on 127.0.0.1:%s (%s) was stopped but %s did not start: %v; run 'jhon dashboard --port %s'\n", port, Short(f.Record.Release), Short(current), err, port)
				continue
			}
			fmt.Fprintf(w, "dashboard restarted on 127.0.0.1:%s (%s → %s)\n", port, Short(f.Record.Release), Short(current))
		}
	}
}
