package support

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The process bound is JHON_TEST_PROCS when a positive number, else a
// quarter of the CPUs, at least 2 (L677).
func TestProcLimit(t *testing.T) {
	for _, c := range []struct {
		env  string
		cpus int
		want int
	}{{"", 16, 4}, {"", 4, 2}, {"", 1, 2}, {"6", 16, 6}, {" 1 ", 16, 1}, {"0", 16, 4}, {"-3", 16, 4}, {"many", 32, 8}} {
		if got := procLimit(c.env, c.cpus); got != c.want {
			t.Errorf("procLimit(%q, %d) = %d, want %d", c.env, c.cpus, got, c.want)
		}
	}
}

// Run starts no more than cap(procs) processes at once: twice as many
// sleeps as slots take at least two rounds.
func TestRunBoundsConcurrentProcesses(t *testing.T) {
	const nap = 300 * time.Millisecond
	n := 2 * cap(procs)
	start := time.Now()
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r := Run(t, Cmd{Args: []string{"sleep", "0.3"}}); r.Code != 0 {
				t.Errorf("sleep: %+v", r)
			}
		}()
	}
	wg.Wait()
	if d := time.Since(start); d < 2*nap {
		t.Errorf("%d sleeps of %v with %d slots took %v, want at least %v", n, nap, cap(procs), d, 2*nap)
	}
}

// A change to a vendored module under third_party/ is a change of the
// binary's source: the build key changes with it.
func TestTheBinaryKeyCoversThirdParty(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("engine/main.go", "package main\n")
	write("go.mod", "module x\n")
	write("third_party/bubbletea/tea.go", "package tea\n")
	before := inputsHash(root)
	write("third_party/bubbletea/tea.go", "package tea // changed\n")
	if after := inputsHash(root); after == before {
		t.Fatalf("the key %s did not change with third_party/", after)
	}
}

// lookupEnv is the value of key in env, and whether it is there.
func lookupEnv(env []string, key string) (string, bool) {
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v, true
		}
	}
	return "", false
}

// T249: Jhon's install record lives in the state folder
// (${XDG_STATE_HOME:-~/.local/state}/jhon/install.json), so a test env must
// never hand the caller's XDG_STATE_HOME to a jhon it runs: with HOME
// overridden it is unset, so the state folder is under that HOME, with one of
// the test's own that one is kept, and otherwise it is a folder of the test
// binary's own, never the caller's.
func TestEnvNeverPassesTheCallersStateFolder(t *testing.T) {
	callerHome, _ := os.UserHomeDir()
	if got, set := lookupEnv(Env("HOME=/h"), "XDG_STATE_HOME"); set {
		t.Errorf("Env(HOME=/h): XDG_STATE_HOME %q; want it unset, so the state folder is under /h", got)
	}
	if got, set := lookupEnv(isolateState([]string{"HOME=/h", "XDG_STATE_HOME=" + callerState}), "XDG_STATE_HOME"); set && callerState != "" {
		t.Errorf("the caller's XDG_STATE_HOME with HOME=/h: %q; want it dropped", got)
	}
	if got, _ := lookupEnv(Env("HOME=/h", "XDG_STATE_HOME=/s"), "XDG_STATE_HOME"); got != "/s" {
		t.Errorf("Env with the test's own XDG_STATE_HOME: %q, want /s", got)
	}
	own, ok := lookupEnv(Env(), "XDG_STATE_HOME")
	if !ok || !filepath.IsAbs(own) || own == callerState || callerHome != "" && strings.HasPrefix(own, filepath.Join(callerHome, ".local")) {
		t.Fatalf("Env(): XDG_STATE_HOME %q (set %v), want a folder of the test binary's own, not the caller's %q", own, ok, callerState)
	}
	if fi, err := os.Stat(own); err != nil || !fi.IsDir() {
		t.Errorf("the test binary's state folder %s is no folder: %v", own, err)
	}
	// Run gives a nil environment, and one that carries the caller's
	// value, the same folder.
	show := []string{"sh", "-c", `printf %s "$XDG_STATE_HOME"`}
	if r := Run(t, Cmd{Args: show}); r.Stdout != own {
		t.Errorf("Run with a nil env: XDG_STATE_HOME %q, want %q", r.Stdout, own)
	}
	if callerState != "" {
		if r := Run(t, Cmd{Args: show, Env: append(os.Environ(), "XDG_STATE_HOME="+callerState)}); r.Stdout != own {
			t.Errorf("Run with the caller's XDG_STATE_HOME: %q, want %q", r.Stdout, own)
		}
	}
	if r := Run(t, Cmd{Args: show, Env: []string{"HOME=/h", "PATH=" + os.Getenv("PATH")}}); r.Stdout != "" {
		t.Errorf("Run with HOME=/h: XDG_STATE_HOME %q; want it unset", r.Stdout)
	}
}
