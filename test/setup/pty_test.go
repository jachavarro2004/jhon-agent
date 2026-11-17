// Package setup drives jhon on a pseudo-terminal that never answers a
// query, as a terminal without OSC 11 or DSR support does (T5b, L17).
package setup

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// onPty runs jhon args under script(1)'s pseudo-terminal, whose other end
// is a pipe that answers nothing; keys are written to it at their offsets,
// and it closes after the last (script waits for its input to end).
// It returns the terminal's output and how long jhon took.
func onPty(t *testing.T, env []string, at time.Duration, keys string, args ...string) (string, time.Duration) {
	t.Helper()
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("script(1) is not installed")
	}
	cmd := "'" + support.Bin(t) + "' " + strings.Join(args, " ")
	c := exec.Command("script", "-qfec", cmd, "/dev/null")
	c.Env = env
	in, w := io.Pipe()
	var out bytes.Buffer
	c.Stdin, c.Stdout, c.Stderr = in, &out, &out
	begin := time.Now()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(at, func() { w.Write([]byte(keys)); w.Close() })
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case err := <-done:
		w.Close()
		if err != nil {
			t.Errorf("jhon %v: %v\n%s", args, err, out.String())
		}
		return out.String(), time.Since(begin)
	case <-time.After(10 * time.Second):
		c.Process.Kill()
		w.Close()
		<-done
		t.Fatalf("jhon %v still running after 10 s:\n%q", args, out.String())
	}
	return "", 0
}

func ptyEnv(t *testing.T) []string {
	home := t.TempDir()
	t.Cleanup(func() { os.Chmod(home, 0o755) })
	// A terminal termenv would query (screen, tmux and dumb it never does).
	// script(1) runs the command through $SHELL, else the login shell, whose
	// startup files may print; /bin/sh reads none.
	return support.Env("HOME="+home, "XDG_CONFIG_HOME=", "JHON_SETTINGS=", "JHON_PROFILES=",
		"TERM=xterm-256color", "COLORTERM=truecolor", "SHELL=/bin/sh")
}

// TestVersionDoesNotWaitForTheTerminal: `jhon version` on a terminal that
// never answers prints its line at once: nothing queries it at start.
func TestVersionDoesNotWaitForTheTerminal(t *testing.T) {
	out, took := onPty(t, ptyEnv(t), 0, "", "version")
	if !strings.HasPrefix(out, "jhon ") || took >= time.Second {
		t.Errorf("jhon version took %v: %q", took, out)
	}
	if strings.Contains(out, "\x1b]11;?") || strings.Contains(out, "\x1b[6n") {
		t.Errorf("jhon version queried the terminal: %q", out)
	}
}

// TestSetupQuitsOnAnEarlyKey: q sent 0.5 s after `jhon setup` starts is
// read, not swallowed by a startup query: setup exits within 2 s.
func TestSetupQuitsOnAnEarlyKey(t *testing.T) {
	out, took := onPty(t, ptyEnv(t), 500*time.Millisecond, "q", "setup")
	if took >= 2*time.Second {
		t.Errorf("jhon setup took %v to quit on q", took)
	}
	if strings.Contains(out, "\x1b]11;?") {
		t.Errorf("jhon setup queried the background color: %q", out)
	}
}
