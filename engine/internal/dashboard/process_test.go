package dashboard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The processes here are copies of this test binary, started by the test in
// its own scratch release tree on a port it picked: TestMain runs a copy
// started as `dashboard --port <p>` as a dashboard (Serve, the record, the
// version route) and one started as `foreign <p>` as an impostor that says
// it is a Jhon dashboard but records nothing.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "dashboard":
			fakeDashboard(os.Args[3])
			return
		case "foreign":
			fakeForeign(os.Args[2])
			return
		}
	}
	os.Exit(m.Run())
}

func fakeDashboard(port string) {
	exe, _ := os.Executable()
	releases := os.Getenv("FAKE_RELEASES")
	Build = BuildInfo{Version: "test", Release: ReleaseOf(exe, releases), Binary: exe, Pid: os.Getpid()}
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		os.Exit(3)
	}
	rec := Record{Pid: os.Getpid(), Port: port, Release: Build.Release, Binary: exe, Args: os.Args[1:]}
	if err := Serve(ln, Server(os.TempDir(), nil, "", nil), StateDir(), rec, func() {}); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func fakeForeign(port string) {
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		os.Exit(3)
	}
	http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Jhon-Dashboard", "1")
		fmt.Fprintf(w, `{"version":"x","release":"x","binary":"x","pid":%d}`, os.Getpid())
	}))
}

const (
	oldID = "09124008aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	newID = "fe03226dbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type tree struct{ releases, state, other string }

// scratchTree is a releases directory holding this test binary as two
// releases' jhon, a copy outside it, and a state directory (XDG_STATE_HOME).
func scratchTree(t *testing.T) tree {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	tr := tree{filepath.Join(root, "releases"), filepath.Join(root, "state"), filepath.Join(root, "other", "jhon")}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{bin(tr, oldID), bin(tr, newID), tr.other} {
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, b, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("XDG_STATE_HOME", tr.state)
	t.Setenv("FAKE_RELEASES", tr.releases)
	return tr
}

func bin(tr tree, id string) string { return filepath.Join(tr.releases, id, "build", "jhon") }

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

// launch starts a fake and waits for the port to answer; the test kills it.
func launch(t *testing.T, port, binary string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(binary, args...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go cmd.Wait()
	t.Cleanup(func() { cmd.Process.Kill() })
	// A dashboard listens before Serve writes its record, so under load the
	// port answers first: wait for both, as the record is what tests read.
	dashboard := len(args) > 0 && args[0] == "dashboard"
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if c, err := net.Dial("tcp", "127.0.0.1:"+port); err == nil {
			c.Close()
			if _, err := os.Stat(recordPath(StateDir(), port)); !dashboard || err == nil {
				return cmd
			}
		}
	}
	t.Fatalf("%s did not listen on %s (and record itself)", binary, port)
	return nil
}

// killRecorded kills whatever pid the record on port names at cleanup: a
// dashboard RestartOlder started.
func killRecorded(t *testing.T, tr tree, port string) {
	t.Cleanup(func() {
		if r, ok := readRecord(filepath.Join(tr.state, "jhon", "dashboard"), port); ok {
			syscall.Kill(r.Pid, syscall.SIGKILL)
		}
	})
}

func version(t *testing.T, port string) BuildInfo {
	t.Helper()
	res, err := http.Get("http://127.0.0.1:" + port + "/api/version")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var b BuildInfo
	body, _ := io.ReadAll(res.Body)
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	return b
}

func TestTheVersionRouteSaysWhichBuildAndIsBehindHostOrigin(t *testing.T) {
	Build = BuildInfo{Version: "0.2.0", Release: newID, Binary: "/r/" + newID + "/build/jhon", Pid: 42}
	t.Cleanup(func() { Build = BuildInfo{} })
	s := serve(t, projects(t))
	r := s.get(t, "/api/version", "GET")
	if r.status != 200 || r.typ != "application/json; charset=utf-8" || r.body != `{"version":"0.2.0","release":"`+newID+`","binary":"/r/`+newID+`/build/jhon","pid":42}` {
		t.Errorf("/api/version: %d %s %s", r.status, r.typ, r.body)
	}
	for _, h := range []map[string]string{{"Host": "evil.example:" + strings.Split(s.url, ":")[2]}, {"Origin": "http://evil.example"}} {
		req, _ := http.NewRequest("GET", s.url+"/api/version", nil)
		for k, v := range h {
			if k == "Host" {
				req.Host = v
			} else {
				req.Header.Set(k, v)
			}
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 403 || strings.Contains(string(body), newID) {
			t.Errorf("%v: %d %s", h, res.StatusCode, body)
		}
	}
}

func TestADashboardRecordsItselfAndRemovesTheRecordAtACleanExit(t *testing.T) {
	tr := scratchTree(t)
	port := freePort(t)
	cmd := launch(t, port, bin(tr, oldID), "dashboard", "--port", port)
	dir := filepath.Join(tr.state, "jhon", "dashboard")
	rec, ok := readRecord(dir, port)
	if !ok || rec.Pid != cmd.Process.Pid || rec.Release != oldID || rec.Binary != bin(tr, oldID) || strings.Join(rec.Args, " ") != "dashboard --port "+port {
		t.Fatalf("record %+v %v", rec, ok)
	}
	if v := version(t, port); v.Pid != cmd.Process.Pid || v.Release != oldID {
		t.Errorf("version %+v", v)
	}
	cmd.Process.Signal(syscall.SIGTERM)
	if !gone(cmd.Process.Pid, 5*time.Second) {
		t.Fatal("it did not exit on SIGTERM")
	}
	if _, err := os.Stat(recordPath(dir, port)); !os.IsNotExist(err) {
		t.Errorf("the record stays after a clean exit: %v", err)
	}
}

func TestRestartOlder(t *testing.T) {
	t.Run("a dashboard from an older release is restarted from the current one on the same port", func(t *testing.T) {
		tr := scratchTree(t)
		port := freePort(t)
		old := launch(t, port, bin(tr, oldID), "dashboard", "--port", port)
		killRecorded(t, tr, port)
		var out bytes.Buffer
		RestartOlder(&out, StateDir(), tr.releases, newID)
		if got := out.String(); got != "dashboard restarted on 127.0.0.1:"+port+" (09124008 → fe03226d)\n" {
			t.Fatalf("out %q", got)
		}
		if alive(old.Process.Pid) {
			t.Error("the old dashboard still runs")
		}
		v := version(t, port)
		if v.Release != newID || v.Binary != bin(tr, newID) {
			t.Errorf("version %+v", v)
		}
		if f := Inspect(port, StateDir(), tr.releases, newID); f.State != Current {
			t.Errorf("after: %+v", f)
		}
	})
	t.Run("a dashboard from the current release is left alone", func(t *testing.T) {
		tr := scratchTree(t)
		port := freePort(t)
		cur := launch(t, port, bin(tr, newID), "dashboard", "--port", port)
		killRecorded(t, tr, port)
		var out bytes.Buffer
		RestartOlder(&out, StateDir(), tr.releases, newID)
		if out.String() != "" || version(t, port).Pid != cur.Process.Pid {
			t.Errorf("out %q, pid %d", out.String(), version(t, port).Pid)
		}
	})
	t.Run("a foreign process on the port is untouched and reported, even under a forged record", func(t *testing.T) {
		tr := scratchTree(t)
		port := freePort(t)
		foreign := launch(t, port, tr.other, "foreign", port)
		dir := StateDir()
		var out bytes.Buffer
		if err := writeRecord(dir, Record{Pid: foreign.Process.Pid, Port: port, Release: oldID, Binary: bin(tr, oldID), Args: []string{"dashboard", "--port", port}}); err != nil {
			t.Fatal(err)
		}
		RestartOlder(&out, dir, tr.releases, newID)
		want := "dashboard on 127.0.0.1:" + port + " left running: pid " + strconv.Itoa(foreign.Process.Pid) + " (" + tr.other + "), not proven to be Jhon's dashboard (its executable " + tr.other + " is not the recorded release's jhon); to restart it by hand, stop it and run 'jhon dashboard --port " + port + "'\n"
		if out.String() != want {
			t.Errorf("out\n%q\nwant\n%q", out.String(), want)
		}
		if !alive(foreign.Process.Pid) || version(t, port).Pid != foreign.Process.Pid {
			t.Error("the foreign process was touched")
		}
		os.Remove(recordPath(dir, port))
		out.Reset()
		RestartOlder(&out, dir, tr.releases, newID)
		if out.String() != "" || !alive(foreign.Process.Pid) {
			t.Errorf("no record: %q", out.String())
		}
		if f := Inspect(port, dir, tr.releases, newID); f.State != Unproven || f.Why != "no record of a Jhon dashboard on this port" {
			t.Errorf("inspect without a record: %+v", f)
		}
	})
	t.Run("a record of a dead pid is ignored and removed; nothing is started", func(t *testing.T) {
		tr := scratchTree(t)
		port := freePort(t)
		dead := exec.Command("true")
		if err := dead.Run(); err != nil {
			t.Fatal(err)
		}
		dir := StateDir()
		writeRecord(dir, Record{Pid: dead.Process.Pid, Port: port, Release: oldID, Binary: bin(tr, oldID), Args: []string{"dashboard", "--port", port}})
		var out bytes.Buffer
		RestartOlder(&out, dir, tr.releases, newID)
		if out.String() != "" {
			t.Errorf("out %q", out.String())
		}
		if _, err := os.Stat(recordPath(dir, port)); !os.IsNotExist(err) {
			t.Error("the dead record stays")
		}
		if c, err := net.Dial("tcp", "127.0.0.1:"+port); err == nil {
			c.Close()
			t.Error("something was started")
		}
	})
	t.Run("with no dashboard nothing is started", func(t *testing.T) {
		tr := scratchTree(t)
		var out bytes.Buffer
		RestartOlder(&out, StateDir(), tr.releases, newID)
		if out.String() != "" {
			t.Errorf("out %q", out.String())
		}
		if es, _ := os.ReadDir(StateDir()); len(es) != 0 {
			t.Errorf("state %v", es)
		}
	})
}

func TestStopSendsNoSignalWhenTheProofFailsAfterInspect(t *testing.T) {
	tr := scratchTree(t)
	port := freePort(t)
	old := launch(t, port, bin(tr, oldID), "dashboard", "--port", port)
	killRecorded(t, tr, port)
	f := Inspect(port, StateDir(), tr.releases, newID)
	if f.State != Older {
		t.Fatalf("inspect: %+v", f)
	}
	// The pid is no longer proven between Inspect and Stop (it exited and the
	// pid was reused, say): Stop must not signal it.
	reprove = func(Record, BuildInfo, string) string { return "the pid was reused" }
	defer func() { reprove = prove }()
	err := Stop(f, tr.releases, time.Second)
	want := "pid " + strconv.Itoa(old.Process.Pid) + " is no longer proven Jhon's dashboard (the pid was reused); left running"
	if err == nil || err.Error() != want {
		t.Fatalf("err %v, want %q", err, want)
	}
	if !alive(old.Process.Pid) || version(t, port).Pid != old.Process.Pid {
		t.Error("the process was signalled")
	}
}
