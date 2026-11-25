//go:build linux

package integrations

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// T242 A2 (odd/tasks/studio-integration-v1.md L18): the unpacked release's
// program is asked what jhon it needs before it is installed, so it runs
// with as little as it needs: an environment of PATH (its own bin, its
// interpreter's folder, the system's) and HOME (a temporary folder, its
// working directory) alone; its answer read up to 64 KiB; every process it
// started killed when it returns, a setsid one too.

// unpackedAt writes script as <dir>/bin/clifixture-bin, mode mode, and
// returns its path and a home folder beside it.
func unpackedAt(t *testing.T, script string, mode os.FileMode) (program, home string) {
	t.Helper()
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(dir, "bin"), 0o755))
	must(os.MkdirAll(filepath.Join(dir, "home"), 0o700))
	program = filepath.Join(dir, "bin", "clifixture-bin")
	must(os.WriteFile(program, []byte(script), mode))
	must(os.Chmod(program, mode))
	return program, filepath.Join(dir, "home")
}

const answer = `{"version":"0.1.0","plugin_dir":"/p"}`

func TestAskUnpackedRunsWithAMinimalEnvironmentInItsHome(t *testing.T) {
	out := filepath.Join(t.TempDir(), "seen")
	t.Setenv("JHON_SECRET_PROBE", "leak")
	program, home := unpackedAt(t, "#!/bin/sh\ntr '\\0' '\\n' < /proc/$$/environ > '"+out+".env'\npwd -P > '"+out+".cwd'\necho '"+answer+"'\n", 0o755)
	a, err := AskUnpacked(cliFixture, program, home)
	if err != nil || a.Version != "0.1.0" {
		t.Fatalf("AskUnpacked = %+v, %v", a, err)
	}
	env, _ := os.ReadFile(out + ".env")
	got := strings.Fields(string(env))
	want := []string{"HOME=" + home, "PATH=" + filepath.Dir(program) + ":" + SystemPath}
	if !slices.Equal(got, want) {
		t.Errorf("environment %q, want %q", got, want)
	}
	if cwd, _ := os.ReadFile(out + ".cwd"); strings.TrimSpace(string(cwd)) != home {
		t.Errorf("cwd %q, want %s", cwd, home)
	}
}

// A program run through `#!/usr/bin/env [-S] <name>` finds <name> where the
// caller does: its folder joins PATH (Node from a version manager).
func TestAskUnpackedFindsItsInterpreter(t *testing.T) {
	tools := t.TempDir()
	if err := os.WriteFile(filepath.Join(tools, "fakeinterp"), []byte("#!/bin/sh\necho '"+answer+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+":/usr/bin:/bin")
	program, home := unpackedAt(t, "#!/usr/bin/env -S fakeinterp --flag\n", 0o755)
	if a, err := AskUnpacked(cliFixture, program, home); err != nil || a.Version != "0.1.0" {
		t.Fatalf("AskUnpacked = %+v, %v", a, err)
	}
}

func TestAskUnpackedReadsAtMost64KiB(t *testing.T) {
	program, home := unpackedAt(t, "#!/bin/sh\nhead -c 70000 /dev/zero | tr '\\0' x\n", 0o755)
	if _, err := AskUnpacked(cliFixture, program, home); err == nil || err.Error() != "clifixture-bin integration --json: answered more than 65536 bytes" {
		t.Errorf("a long answer: %v", err)
	}
	// The same bound for a program on PATH (jhon start, doctor).
	fakeCLI(t, "clifixture-bin", strings.Repeat("x", 70000), 0, "", 0)
	if _, err := AskIntegration(cliFixture); err == nil || err.Error() != "clifixture-bin integration --json: answered more than 65536 bytes" {
		t.Errorf("a long answer on PATH: %v", err)
	}
}

func running(pid int) bool { return syscall.Kill(pid, 0) == nil }

// A child that left the program's session (setsid) is killed and reaped
// when the program returns, as is one in its process group.
func TestAskUnpackedLeavesNoChildBehind(t *testing.T) {
	pids := filepath.Join(t.TempDir(), "pids")
	program, home := unpackedAt(t, "#!/bin/sh\n"+
		"setsid sh -c 'echo $$ >> \""+pids+"\"; exec sleep 60' </dev/null >/dev/null 2>&1 &\n"+
		"sh -c 'echo $$ >> \""+pids+"\"; exec sleep 60' </dev/null >/dev/null 2>&1 &\n"+
		"while [ \"$(wc -l < '"+pids+"' 2>/dev/null)\" != 2 ]; do sleep 0.05; done\n"+
		"echo '"+answer+"'\n", 0o755)
	if _, err := AskUnpacked(cliFixture, program, home); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(pids)
	got := strings.Fields(string(b))
	if len(got) != 2 {
		t.Fatalf("pids %q", got)
	}
	for _, s := range got {
		pid, _ := strconv.Atoi(s)
		deadline := time.Now().Add(2 * time.Second)
		for running(pid) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if running(pid) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Errorf("child %d outlived the program", pid)
		}
	}
}

func TestAskUnpackedSaysWhyItCannotRun(t *testing.T) {
	program, home := unpackedAt(t, "#!/bin/sh\necho '"+answer+"'\n", 0o644)
	if _, err := AskUnpacked(cliFixture, program, home); err == nil || err.Error() != "clifixture-bin at "+program+" is not executable" {
		t.Errorf("not executable: %v", err)
	}
	// A program whose interpreter cannot run: execve says EACCES, as it
	// does on a noexec mount, where the message says so.
	interp := filepath.Join(t.TempDir(), "interp")
	if err := os.WriteFile(interp, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	program, home = unpackedAt(t, "#!"+interp+"\n", 0o755)
	saved := mountedNoexec
	t.Cleanup(func() { mountedNoexec = saved })
	mountedNoexec = func(string) bool { return false }
	_, err := AskUnpacked(cliFixture, program, home)
	if err == nil || !errors.Is(err, syscall.EACCES) || !strings.HasPrefix(err.Error(), "cannot run clifixture-bin at "+program+": ") {
		t.Errorf("EACCES: %v", err)
	}
	mountedNoexec = func(dir string) bool { return dir == filepath.Dir(program) }
	_, err = AskUnpacked(cliFixture, program, home)
	if want := "cannot run clifixture-bin at " + program + ": its file system is mounted noexec; set TMPDIR to a folder on a file system where programs may run"; err == nil || err.Error() != want {
		t.Errorf("noexec: %v\nwant %s", err, want)
	}
}
