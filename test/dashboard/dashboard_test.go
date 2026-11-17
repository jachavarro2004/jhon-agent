// Package dashboard runs `jhon dashboard --port <p>` on a port already
// held (T15.16, L180): by a dashboard of an older release, of this release,
// or by something that is not proven to be Jhon's. Every process runs in a
// scratch HOME and state directory, from a scratch release tree holding the
// test binary as two releases, on a port the test picked; nothing reaches
// the owner's dashboard or releases.
package dashboard

import (
	"bufio"
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

const (
	oldID = "09124008aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	newID = "fe03226dbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type scratch struct {
	home string
	env  []string
}

func (s scratch) bin(id string) string {
	return filepath.Join(s.home, ".local", "share", "jhon", "releases", id, "build", "jhon")
}

func newScratch(t *testing.T) scratch {
	t.Helper()
	home, _ := filepath.EvalSymlinks(t.TempDir())
	b, err := os.ReadFile(support.Bin(t))
	if err != nil {
		t.Fatal(err)
	}
	s := scratch{home: home}
	for _, id := range []string{oldID, newID} {
		os.MkdirAll(filepath.Dir(s.bin(id)), 0o755)
		if err := os.WriteFile(s.bin(id), b, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	s.env = support.Env("HOME="+home, "XDG_STATE_HOME="+filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"), "XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"), "JHON_ROOT=", "JHON_SOURCE=")
	return s
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

// serve starts `jhon dashboard --port <port>` from binary and returns its
// stdout lines as they come; the test kills it.
func serve(t *testing.T, s scratch, binary, port string) (*exec.Cmd, <-chan string) {
	t.Helper()
	cmd := exec.Command(binary, "dashboard", "--port", port)
	cmd.Env = s.env
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
		cmd.Wait()
	}()
	t.Cleanup(func() { cmd.Process.Kill() })
	return cmd, lines
}

func line(t *testing.T, lines <-chan string) string {
	t.Helper()
	select {
	case l := <-lines:
		return l
	case <-time.After(10 * time.Second):
		t.Fatal("no line within 10 s")
		return ""
	}
}

type build struct {
	Release, Binary string
	Pid             int
}

func version(t *testing.T, port string) build {
	t.Helper()
	res, err := http.Get("http://127.0.0.1:" + port + "/api/version")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var b build
	if err := json.NewDecoder(res.Body).Decode(&b); err != nil || res.Header.Get("X-Jhon-Dashboard") != "1" {
		t.Fatalf("/api/version: %v %v", err, res.Header)
	}
	return b
}

func alive(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	return err == nil && !strings.Contains(string(b), ") Z ")
}

func TestDashboardOnAHeldPort(t *testing.T) {
	t.Run("an older Jhon dashboard is replaced by this release's on the same port", func(t *testing.T) {
		s := newScratch(t)
		port := freePort(t)
		old, lines := serve(t, s, s.bin(oldID), port)
		if l := line(t, lines); l != "jhon dashboard: http://127.0.0.1:"+port+"/  (Ctrl-C stops it)" {
			t.Fatalf("old: %q", l)
		}
		cur, lines := serve(t, s, s.bin(newID), port)
		for _, want := range []string{"dashboard restarted on 127.0.0.1:" + port + " (09124008 → fe03226d)", "jhon dashboard: http://127.0.0.1:" + port + "/  (Ctrl-C stops it)"} {
			if l := line(t, lines); l != want {
				t.Fatalf("got %q, want %q", l, want)
			}
		}
		time.Sleep(100 * time.Millisecond)
		if alive(old.Process.Pid) {
			t.Error("the old dashboard still runs")
		}
		if v := version(t, port); v.Release != newID || v.Pid != cur.Process.Pid || v.Binary != s.bin(newID) {
			t.Errorf("version %+v", v)
		}
	})
	t.Run("a dashboard of this release is left to answer", func(t *testing.T) {
		s := newScratch(t)
		port := freePort(t)
		cur, lines := serve(t, s, s.bin(newID), port)
		line(t, lines)
		r := support.Run(t, support.Cmd{Args: []string{s.bin(newID), "dashboard", "--port", port}, Env: s.env})
		if r.Code != 0 || r.Stdout != "jhon dashboard: http://127.0.0.1:"+port+"/  (already running from this release)\n" || r.Stderr != "" {
			t.Errorf("%d %q %q", r.Code, r.Stdout, r.Stderr)
		}
		if version(t, port).Pid != cur.Process.Pid {
			t.Error("it was replaced")
		}
	})
	for _, c := range []struct{ name, header, why string }{
		{"a server that is not Jhon's", "", "it does not say it is a Jhon dashboard"},
		{"a Jhon dashboard from before /api/version", "1", "a Jhon dashboard without /api/version, from a release before it"},
	} {
		t.Run(c.name+" is left running and said, exit 1", func(t *testing.T) {
			s := newScratch(t)
			port := freePort(t)
			ln, err := net.Listen("tcp", "127.0.0.1:"+port)
			if err != nil {
				t.Fatal(err)
			}
			srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if c.header != "" {
					w.Header().Set("X-Jhon-Dashboard", c.header)
				}
				http.NotFound(w, r)
			})}
			go srv.Serve(ln)
			t.Cleanup(func() { srv.Close() })
			r := support.Run(t, support.Cmd{Args: []string{s.bin(newID), "dashboard", "--port", port}, Env: s.env})
			exe, _ := os.Executable()
			want := "jhon: dashboard on 127.0.0.1:" + port + " left running: pid " + strconv.Itoa(os.Getpid()) + " (" + exe + "), not proven to be Jhon's dashboard (" + c.why + "); to restart it by hand, stop it and run 'jhon dashboard --port " + port + "'\n"
			if r.Code != 1 || r.Stdout != "" || r.Stderr != want {
				t.Errorf("%d %q\n%q\nwant\n%q", r.Code, r.Stdout, r.Stderr, want)
			}
			if c, err := net.Dial("tcp", "127.0.0.1:"+port); err != nil {
				t.Error("the server on the port is gone")
			} else {
				c.Close()
			}
		})
	}
	t.Run("a dashboard stopped with SIGTERM removes its record", func(t *testing.T) {
		s := newScratch(t)
		port := freePort(t)
		cmd, lines := serve(t, s, s.bin(newID), port)
		line(t, lines)
		rec := filepath.Join(s.home, ".local", "state", "jhon", "dashboard", port+".json")
		if _, err := os.Stat(rec); err != nil {
			t.Fatal(err)
		}
		cmd.Process.Signal(syscall.SIGTERM)
		for end := time.Now().Add(5 * time.Second); alive(cmd.Process.Pid) && time.Now().Before(end); {
			time.Sleep(50 * time.Millisecond)
		}
		if _, err := os.Stat(rec); !os.IsNotExist(err) {
			t.Errorf("the record stays: %v", err)
		}
	})
}
