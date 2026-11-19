package release

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A go that exits while a process it started still holds its output (the
// os/exec pipe wait) ends goCmd once go has exited and the grace has passed,
// with go's own verdict and output: a sync never waits on what go left behind.
func TestGoCmdEndsWhenGoExitsAndItsChildHoldsTheOutput(t *testing.T) {
	old := outputGrace
	outputGrace = 200 * time.Millisecond
	t.Cleanup(func() { outputGrace = old })
	bin := t.TempDir()
	pid := filepath.Join(t.TempDir(), "pid")
	script := "#!/bin/sh\necho built\nsleep 30 &\necho $! > '" + pid + "'\nexit ${CODE:-0}\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	kill := func() {
		if b, err := os.ReadFile(pid); err == nil {
			if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				syscall.Kill(n, syscall.SIGKILL)
			}
		}
	}
	for _, code := range []string{"0", "3"} {
		t.Setenv("CODE", code)
		start := time.Now()
		out, ok := goCmd(t.TempDir(), "build")
		took := time.Since(start)
		kill()
		if ok != (code == "0") || out != "built" || took > 10*time.Second {
			t.Errorf("go exiting %s with a child holding its output: ok %v, out %q, after %v", code, ok, out, took)
		}
	}
}

// TestGoCmdLeavesNoConfigDirectory: go runs with its telemetry off in the
// throwaway config directory, so it records no counters and starts no
// detached telemetry child, which would create the directory again after
// goCmd removed it (T113: hundreds of jhon-go-config-* left in /tmp).
func TestGoCmdLeavesNoConfigDirectory(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	out, ok := goCmd(t.TempDir(), "env", "GOTELEMETRY")
	if !ok || out != "off" {
		t.Errorf("go env GOTELEMETRY in goCmd's environment: ok %v, %q, want off", ok, out)
	}
	// A telemetry child starts within a moment of go; give it one.
	time.Sleep(time.Second)
	if left, _ := filepath.Glob(filepath.Join(tmp, "jhon-go-config-*")); len(left) != 0 {
		t.Errorf("left behind after goCmd: %q", left)
	}
}

// TestGoEnvKeepsTheUsersGoEnvFile: go runs with its config directory aside
// (a throwaway one, removed after), and GOENV still names the user's own
// go env file, or the one the caller set.
func TestGoEnvKeepsTheUsersGoEnvFile(t *testing.T) {
	user := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", user)
	t.Setenv("GOENV", "")
	os.Unsetenv("GOENV")
	last := func(env []string, k string) string {
		v := ""
		for _, kv := range env {
			if strings.HasPrefix(kv, k+"=") {
				v = kv[len(k)+1:]
			}
		}
		return v
	}
	env, done, err := goEnv()
	if err != nil {
		t.Fatal(err)
	}
	aside := last(env, "XDG_CONFIG_HOME")
	if aside == user || !IsDir(aside) || last(env, "GOENV") != filepath.Join(user, "go", "env") {
		t.Errorf("XDG_CONFIG_HOME=%s GOENV=%s", aside, last(env, "GOENV"))
	}
	done()
	if IsDir(aside) {
		t.Errorf("%s is left behind", aside)
	}
	t.Setenv("GOENV", "/elsewhere/env")
	env, done, _ = goEnv()
	defer done()
	if last(env, "GOENV") != "/elsewhere/env" {
		t.Errorf("GOENV=%s", last(env, "GOENV"))
	}
}
