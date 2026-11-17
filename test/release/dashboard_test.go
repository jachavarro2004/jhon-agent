package release

import (
	"encoding/json"
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

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// TestSyncRestartsADashboardFromTheOlderRelease (T15.16, L180): once
// `jhon sync` switches current, a dashboard the previous release started is
// restarted from the new one on its port, with one line; with none running
// it starts nothing. The dashboard runs in the scratch HOME on a port the
// test picked.
func TestSyncRestartsADashboardFromTheOlderRelease(t *testing.T) {
	parallel(t)
	env := scratchHome(t)
	src := scratchSource(t)
	stateDir := filepath.Join(env.home, ".local", "state")
	state := "XDG_STATE_HOME=" + stateDir
	first := run(t, env, cliOf(src), []string{"sync"}, state)
	wantCode(t, first, 0)
	if strings.Contains(first.Out(), "dashboard") {
		t.Errorf("a sync with no dashboard running said:\n%s", first.Out())
	}
	if _, err := os.Stat(filepath.Join(stateDir, "jhon", "dashboard")); !os.IsNotExist(err) {
		t.Errorf("a sync with no dashboard recorded one: %v", err)
	}
	old := head(t, src)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	ln.Close()
	record := filepath.Join(stateDir, "jhon", "dashboard", port+".json")
	// Whatever dashboard the record names at the end is the test's to stop.
	t.Cleanup(func() {
		var r struct{ Pid int }
		if b, err := os.ReadFile(record); err == nil && json.Unmarshal(b, &r) == nil && r.Pid > 0 {
			syscall.Kill(r.Pid, syscall.SIGKILL)
		}
	})
	dash := exec.Command(filepath.Join(share(env.home), "current", "build", "jhon"), "dashboard", "--port", port)
	dash.Env = support.Env("HOME="+env.home, state, "XDG_CACHE_HOME="+filepath.Join(env.home, ".cache"))
	if err := dash.Start(); err != nil {
		t.Fatal(err)
	}
	go dash.Wait()
	t.Cleanup(func() { dash.Process.Kill() })
	answers := func(release string) bool {
		res, err := http.Get("http://127.0.0.1:" + port + "/api/version")
		if err != nil {
			return false
		}
		defer res.Body.Close()
		var b struct{ Release string }
		return json.NewDecoder(res.Body).Decode(&b) == nil && b.Release == release
	}
	for end := time.Now().Add(10 * time.Second); !answers(old); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("the dashboard of the first release did not answer")
		}
	}

	support.Write(t, src, map[string]string{"dashboard-fixture.txt": "next\n"})
	support.Git(t, src, "add", "-A")
	support.Git(t, src, "commit", "-q", "--no-verify", "-m", "next")
	next := head(t, src)
	r := run(t, env, cliOf(src), []string{"sync"}, state)
	wantCode(t, r, 0)
	if want := "dashboard restarted on 127.0.0.1:" + port + " (" + old[:8] + " → " + next[:8] + ")\n"; !strings.Contains(r.Stdout, want) {
		t.Errorf("sync did not say %q:\n%s", want, r.Out())
	}
	if !answers(next) {
		t.Error("the dashboard on the port is not the new release's")
	}
}
