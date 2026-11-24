//go:build unix

package integrations

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// odd/tasks/jhon-studio.md L11-L12: a program that hangs is cut at the
// bound with every process it started: a child it left running (here a
// sleep that keeps stdout open) is gone after the cut, not orphaned.
func TestAHungProgramIsCutWithItsChildren(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child")
	script := "#!/bin/sh\nsleep 300 &\necho $! > '" + pidFile + "'\nwait\n"
	if err := os.WriteFile(filepath.Join(dir, "hangfixture-bin"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	saved := askTimeout
	askTimeout = 300 * time.Millisecond
	t.Cleanup(func() { askTimeout = saved })
	start := time.Now()
	_, err := AskIntegration(Manifest{Name: "hangfixture", Kind: "cli", Doctor: Doctor{Binary: "hangfixture-bin"}})
	if err == nil || err.Error() != "hangfixture-bin integration --json: did not answer in 300ms" {
		t.Errorf("AskIntegration = %v", err)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("the cut took %s", took)
	}
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	for i := 0; i < 100 && alive(pid); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if alive(pid) {
		t.Errorf("the hung program's child %d outlived the cut", pid)
	}
}

// alive is whether pid runs (a zombie, killed and not yet reaped, is not).
func alive(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return syscall.Kill(pid, 0) == nil
	}
	f := strings.Fields(string(b)[strings.LastIndex(string(b), ")")+1:])
	return len(f) > 0 && f[0] != "Z"
}
