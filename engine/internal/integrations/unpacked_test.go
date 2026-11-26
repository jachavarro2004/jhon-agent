//go:build linux

package integrations

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
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

// Focused re-verify probes (studio-integration-v1 L21, S5/A2): the run spares a
// child jhon started before it and leaves no daemon after a cut.
func TestAskUnpackedSparesAChildStartedBefore(t *testing.T) {
	sib := exec.Command("sleep", "30")
	if err := sib.Start(); err != nil {
		t.Fatal(err)
	}
	defer sib.Process.Kill()
	program, home := unpackedAt(t, "#!/bin/sh\necho '"+answer+"'\n", 0o755)
	if _, err := AskUnpacked(cliFixture, program, home); err != nil {
		t.Fatal(err)
	}
	if !running(sib.Process.Pid) {
		t.Error("a child jhon started before the run was killed")
	}
}

func TestAskUnpackedCutLeavesNoDaemon(t *testing.T) {
	saved := askTimeout
	askTimeout = time.Second
	t.Cleanup(func() { askTimeout = saved })
	pid := filepath.Join(t.TempDir(), "pid")
	program, home := unpackedAt(t, "#!/bin/sh\n( setsid sh -c 'echo $$ > \""+pid+"\"; exec sleep 60' & )\nsleep 60\n", 0o755)
	start := time.Now()
	if _, err := AskUnpacked(cliFixture, program, home); err == nil || !strings.Contains(err.Error(), "did not answer in 1s") || time.Since(start) > 3*time.Second {
		t.Errorf("err %v after %s", err, time.Since(start))
	}
	b, _ := os.ReadFile(pid)
	p, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	deadline := time.Now().Add(2 * time.Second)
	for p > 0 && running(p) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if p == 0 || running(p) {
		t.Errorf("daemon %d outlived the cut", p)
	}
}

// T247 (odd/tasks/phase1-security.md S4): the run reaps only its own
// descendants. A grandchild of a child jhon started before the run, whose
// parent exits during the run, is not the run's and is left running.
func TestAskUnpackedSparesTheOrphanOfAChildStartedBefore(t *testing.T) {
	dir := t.TempDir()
	gpid, gate := filepath.Join(dir, "gpid"), filepath.Join(dir, "go")
	sib := exec.Command("sh", "-c", "sleep 60 </dev/null >/dev/null 2>&1 & echo $! > '"+gpid+"'; while [ ! -e '"+gate+"' ]; do sleep 0.02; done")
	if err := sib.Start(); err != nil {
		t.Fatal(err)
	}
	var g int
	for deadline := time.Now().Add(5 * time.Second); g == 0 && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		b, _ := os.ReadFile(gpid)
		g, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	if g == 0 {
		t.Fatal("no grandchild")
	}
	t.Cleanup(func() { syscall.Kill(g, syscall.SIGKILL) })
	// The program opens the gate, waits until the grandchild's parent is
	// no longer the child that started it (that child exited), then answers.
	program, home := unpackedAt(t, "#!/bin/sh\n: > '"+gate+"'\n"+
		"while read -r _ _ _ pp _ < /proc/"+strconv.Itoa(g)+"/stat && [ \"$pp\" = "+strconv.Itoa(sib.Process.Pid)+" ]; do sleep 0.02; done\n"+
		"echo '"+answer+"'\n", 0o755)
	if _, err := AskUnpacked(cliFixture, program, home); err != nil {
		t.Fatal(err)
	}
	sib.Wait()
	time.Sleep(100 * time.Millisecond)
	if !running(g) {
		t.Error("the run killed the orphan of a child jhon started before it")
	}
}

// T247: interpreterDir reads env's options as env does: in a #! line the
// rest after env is one argument, split only by -S, whose options (-u
// NAME, -C DIR, --unset=NAME, NAME=VALUE...) are skipped with their
// arguments, never taken for the interpreter.
func TestInterpreterDirReadsEnvsOptions(t *testing.T) {
	tools, decoys := t.TempDir(), t.TempDir()
	for _, p := range []string{filepath.Join(tools, "fakeinterp"), filepath.Join(decoys, "decoy")} {
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", decoys+":"+tools+":/usr/bin:/bin")
	for _, c := range []struct{ line, want string }{
		{"#!/usr/bin/env fakeinterp\n", tools},
		{"#!/usr/bin/env fakeinterp", tools},
		{"#!/usr/bin/env -S fakeinterp --flag\n", tools},
		{"#!/usr/bin/env -S -u decoy fakeinterp\n", tools},
		{"#!/usr/bin/env -S -udecoy fakeinterp\n", tools},
		{"#!/usr/bin/env -S --unset=decoy fakeinterp\n", tools},
		{"#!/usr/bin/env -S --unset decoy fakeinterp\n", tools},
		{"#!/usr/bin/env -S -C decoy fakeinterp\n", tools},
		{"#!/usr/bin/env -S --chdir decoy fakeinterp\n", tools},
		{"#!/usr/bin/env -S -i -v A=decoy fakeinterp\n", tools},
		{"#!/usr/bin/env -S -0 fakeinterp\n", ""},
		{"#!/usr/bin/env -vS fakeinterp\n", tools},
		{"#!/usr/bin/env --split-string=fakeinterp --flag\n", tools},
		{"#!/usr/bin/env -S -- fakeinterp\n", tools},
		{"#!/usr/bin/env -S --ignore-signal=PIPE fakeinterp\n", tools},
		// What env would not run as written names no interpreter.
		{"#!/usr/bin/env fakeinterp --flag\n", ""},
		{"#!/usr/bin/env -S -u\n", ""},
		{"#!/usr/bin/env -S --frobnicate decoy\n", ""},
		{"#!/usr/bin/env -S '" + "fakeinterp'\n", ""},
		{"#!/usr/bin/env -S ${X}fakeinterp\n", ""},
		{"#!/usr/bin/env -S ./fakeinterp\n", ""},
		{"#!/bin/sh\n", ""},
	} {
		p, body := filepath.Join(t.TempDir(), "prog"), c.line
		if strings.HasSuffix(body, "\n") {
			body += "body\n"
		}
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := interpreterDir(p); got != c.want {
			t.Errorf("%q: %q, want %q", c.line, got, c.want)
		}
	}
}

// T247: the first line is read as the kernel reads it, up to 256 bytes:
// a first line with no end (a pipe that never closes, or a huge line) is
// not read past them.
func TestInterpreterDirReadsABoundedFirstLine(t *testing.T) {
	tools := t.TempDir()
	if err := os.WriteFile(filepath.Join(tools, "fakeinterp"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+":/usr/bin:/bin")
	fifo := filepath.Join(t.TempDir(), "prog")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() { done <- interpreterDir(fifo) }()
	w, err := os.OpenFile(fifo, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := w.Write([]byte("#!/usr/bin/env -S fakeinterp " + strings.Repeat("x", 300))); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got != tools {
			t.Errorf("interpreterDir = %q, want %q", got, tools)
		}
	case <-time.After(3 * time.Second):
		w.Close()
		<-done
		t.Fatal("interpreterDir read past the first 256 bytes of a line with no end")
	}
	huge := filepath.Join(t.TempDir(), "huge")
	if err := os.WriteFile(huge, []byte("#!/usr/bin/env fakeinterp"+strings.Repeat(" ", 1<<20)), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := interpreterDir(huge); got != tools {
		t.Errorf("a huge first line: %q, want %q", got, tools)
	}
}

// T247's verify probes (odd/tasks/phase1-security.md T4), kept as
// regressions.

// pidIn is the pid written to f, waited for up to 5 s.
func pidIn(t *testing.T, f string) int {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		b, _ := os.ReadFile(f)
		if p, _ := strconv.Atoi(strings.TrimSpace(string(b))); p > 0 {
			return p
		}
	}
	t.Fatalf("no pid in %s", f)
	return 0
}

// ends is whether pid ends within 2 s.
func ends(pid int) bool {
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if !running(pid) {
			return true
		}
	}
	return false
}

// Item 1: a setsid daemon a child jhon started before the run starts
// during the run, orphaned during it, is not the run's: it is left running.
func TestAskUnpackedSparesADaemonAnotherChildStartsDuringTheRun(t *testing.T) {
	dir := t.TempDir()
	gpid, gate := filepath.Join(dir, "gpid"), filepath.Join(dir, "gate")
	sib := exec.Command("sh", "-c", "while [ ! -e '"+gate+"' ]; do sleep 0.02; done; ( setsid sh -c 'echo $$ > \""+gpid+"\"; exec sleep 60' </dev/null >/dev/null 2>&1 & ); exit 0")
	if err := sib.Start(); err != nil {
		t.Fatal(err)
	}
	program, home := unpackedAt(t, "#!/bin/sh\n: > '"+gate+"'\nwhile [ ! -s '"+gpid+"' ]; do sleep 0.02; done\nsleep 0.3\necho '"+answer+"'\n", 0o755)
	if _, err := AskUnpacked(cliFixture, program, home); err != nil {
		t.Fatal(err)
	}
	sib.Wait()
	g := pidIn(t, gpid)
	t.Cleanup(func() { syscall.Kill(g, syscall.SIGKILL) })
	time.Sleep(200 * time.Millisecond)
	if !running(g) {
		t.Error("the run killed a daemon another child of jhon's left behind")
	}
}

// Item 1: the run's own double-forked setsid grandchild, and the child of
// a descendant that made itself a subreaper, are reaped.
func TestAskUnpackedReapsItsDeepDescendants(t *testing.T) {
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		t.Skip("no /usr/bin/python3")
	}
	dir := t.TempDir()
	a, b, c := filepath.Join(dir, "a"), filepath.Join(dir, "b"), filepath.Join(dir, "c")
	py := "import ctypes,os,time\nctypes.CDLL(None).prctl(36,1,0,0,0)\nos.setsid()\nif os.fork()==0:\n  open('" + c + "','w').write(str(os.getpid()))\n  time.sleep(60)\nopen('" + b + "','w').write(str(os.getpid()))\ntime.sleep(60)\n"
	if err := os.WriteFile(filepath.Join(dir, "sub.py"), []byte(py), 0o644); err != nil {
		t.Fatal(err)
	}
	program, home := unpackedAt(t, "#!/bin/sh\n( ( setsid sh -c 'echo $$ > \""+a+"\"; exec sleep 60' </dev/null >/dev/null 2>&1 & ) & )\n"+
		"( /usr/bin/python3 '"+filepath.Join(dir, "sub.py")+"' </dev/null >/dev/null 2>&1 & )\n"+
		"while [ ! -s '"+a+"' ] || [ ! -s '"+b+"' ] || [ ! -s '"+c+"' ]; do sleep 0.02; done\necho '"+answer+"'\n", 0o755)
	if _, err := AskUnpacked(cliFixture, program, home); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{a, b, c} {
		if p := pidIn(t, f); !ends(p) {
			syscall.Kill(p, syscall.SIGKILL)
			t.Errorf("%s: %d outlived the run", filepath.Base(f), p)
		}
	}
}

// Item 1: the cut holds for a program that ignores TERM and leaves a
// setsid daemon that ignores it too and keeps its stdout open.
func TestAskUnpackedCutHoldsAgainstIgnoredTerm(t *testing.T) {
	saved := askTimeout
	askTimeout = time.Second
	t.Cleanup(func() { askTimeout = saved })
	pid := filepath.Join(t.TempDir(), "pid")
	program, home := unpackedAt(t, "#!/bin/sh\ntrap '' TERM\n( setsid sh -c 'trap \"\" TERM; echo $$ > \""+pid+"\"; exec sleep 60' & )\nsleep 60\n", 0o755)
	start := time.Now()
	_, err := AskUnpacked(cliFixture, program, home)
	if el := time.Since(start); err == nil || err.Error() != "clifixture-bin integration --json: did not answer in 1s" || el > 3*time.Second {
		t.Errorf("error %v after %s", err, el)
	}
	if p := pidIn(t, pid); !ends(p) {
		syscall.Kill(p, syscall.SIGKILL)
		t.Errorf("daemon %d outlived the cut", p)
	}
}

// Item 1: through the helper, what a run says reads as before, and the
// program holds no descriptor of the helper's (only its stdout is a pipe)
// nor its marker.
func TestAskUnpackedSaysWhatItSaidBefore(t *testing.T) {
	for _, c := range []struct{ script, want string }{
		{"#!/bin/sh\nexit 3\n", "clifixture-bin exited 3"},
		{"#!/nonexistent/interp\n", "cannot run clifixture-bin at <P>: fork/exec <P>: no such file or directory"},
		{"#!/bin/sh\nkill -TERM $$\n", "clifixture-bin exited -1"},
		{"#!/bin/sh\necho notjson\n", "clifixture-bin integration --json: not the JSON {version, plugin_dir, skill}"},
		{"#!/bin/sh\nhead -c 70000 /dev/zero\n", "clifixture-bin integration --json: answered more than 65536 bytes"},
	} {
		program, home := unpackedAt(t, c.script, 0o755)
		_, err := AskUnpacked(cliFixture, program, home)
		if got := fmt.Sprint(err); strings.ReplaceAll(got, program, "<P>") != c.want {
			t.Errorf("%q: %s\nwant %s", c.script, got, c.want)
		}
	}
	out := filepath.Join(t.TempDir(), "o")
	program, home := unpackedAt(t, "#!/bin/sh\ntr '\\0' '\\n' < /proc/$$/environ > '"+out+"_vars'\n"+
		"ls -l /proc/$$/fd > '"+out+"_fds'\necho '"+answer+"'\n", 0o755)
	if _, err := AskUnpacked(cliFixture, program, home); err != nil {
		t.Fatal(err)
	}
	if e, _ := os.ReadFile(out + "_vars"); strings.Contains(string(e), "REAPER") {
		t.Errorf("the helper's marker reached the program: %q", e)
	}
	fds, _ := os.ReadFile(out + "_fds")
	// ls (a child; the shell's own descriptors are as the program got
	// them) lists "... <fd> -> <target>": a pipe only at stdout.
	for _, l := range strings.Split(strings.TrimSpace(string(fds)), "\n") {
		f := strings.Fields(l)
		if len(f) > 2 && f[len(f)-2] == "->" && strings.HasPrefix(f[len(f)-1], "pipe:") && f[len(f)-3] != "1" {
			t.Errorf("the program holds a pipe at fd %s: %q", f[len(f)-3], fds)
		}
	}
}

// Advisory: where /proc/self/exe is not jhon (jhon run through ld.so), the
// helper says nothing: the program runs as it is, its group still killed.
func TestAskUnpackedRunsDirectlyWhenTheHelperIsNotJhon(t *testing.T) {
	saved := reaperExe
	t.Cleanup(func() { reaperExe = saved })
	for _, exe := range []string{"/bin/true", "/bin/false"} {
		reaperExe = exe
		program, home := unpackedAt(t, "#!/bin/sh\necho '"+answer+"'\n", 0o755)
		if a, err := AskUnpacked(cliFixture, program, home); err != nil || a.Version != "0.1.0" {
			t.Errorf("helper %s: %+v, %v", exe, a, err)
		}
	}
}

// Advisory: a cut that ends the helper before it said anything (a TERM
// before its handler is set) is said as the cut, and nothing runs after.
func TestAskUnpackedCutBeforeTheHelperStarted(t *testing.T) {
	savedExe, savedTimeout := reaperExe, askTimeout
	t.Cleanup(func() { reaperExe, askTimeout = savedExe, savedTimeout })
	dir := t.TempDir()
	slow := filepath.Join(dir, "slow-helper")
	if err := os.WriteFile(slow, []byte("#!/bin/sh\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	reaperExe, askTimeout = slow, time.Second
	ran := filepath.Join(dir, "ran")
	program, home := unpackedAt(t, "#!/bin/sh\n: > '"+ran+"'\necho '"+answer+"'\n", 0o755)
	_, err := AskUnpacked(cliFixture, program, home)
	if err == nil || err.Error() != "clifixture-bin integration --json: did not answer in 1s" {
		t.Errorf("error = %v", err)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Error("the program ran after the cut")
	}
}

// Item 3 (the verify's cases): the first line read as the kernel and env
// read it; what jhon does not read (an abbreviated option, a carriage
// return, a name cut at byte 255) names no interpreter.
func TestInterpreterDirReadsAsTheKernelAndEnvDo(t *testing.T) {
	tools, decoys := t.TempDir(), t.TempDir()
	for _, p := range []string{filepath.Join(tools, "fakeinterp"), filepath.Join(decoys, "decoy")} {
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", decoys+":"+tools+":/usr/bin:/bin")
	long := "#!/usr/bin/env -S fakeinterp " + strings.Repeat("y", 400) + "\nbody\n"
	cut := "#!/usr/bin/env -S " + strings.Repeat("A=1 ", 59) + "fakeinterp\n" // the name crosses byte 255
	for _, c := range []struct{ line, want string }{
		{"#! /usr/bin/env fakeinterp\n", tools},
		{"#!/usr/bin/env  fakeinterp \n", tools},
		{"#!/usr/bin/env\tfakeinterp\n", tools},
		{"#!/bin/env fakeinterp\n", tools},
		{"#!/usr/bin/env\n", ""},
		{"#!/usr/bin/env -S\n", ""},
		{"#!/usr/bin/env -S -i fakeinterp\n", tools},
		{"#!/usr/bin/env -S - fakeinterp\n", tools},
		{"#!/usr/bin/env -S -x fakeinterp\n", ""},
		{"#!/usr/bin/env -S -P /x fakeinterp\n", ""},
		{"#!/usr/bin/env -S -u PATH decoy\n", decoys},
		{"#!/usr/bin/env -S -C/tmp fakeinterp\n", tools},
		{"#!/usr/bin/env -S --chdir=/tmp fakeinterp\n", tools},
		{"#!/usr/bin/env -S -vi -u decoy fakeinterp\n", tools},
		{"#!/usr/bin/env -S -iu decoy fakeinterp\n", tools},
		{"#!/usr/bin/env -S A=1 B=2 fakeinterp --x\n", tools},
		{"#!/usr/bin/env -S --block-signal fakeinterp\n", tools},
		{"#!/usr/bin/env -S --default-signal=INT fakeinterp\n", tools},
		{"#!/usr/bin/env -S --unset\n", ""},
		{"#!/usr/bin/env -S --version\n", ""},
		{"#!/usr/bin/env -S --help fakeinterp\n", ""},
		{"#!/usr/bin/env -S --debug=1 fakeinterp\n", ""},
		{"#!/usr/bin/env -S --split-string=x fakeinterp\n", ""},
		{"#!/usr/bin/env --split-string fakeinterp\n", ""},
		{"#!/usr/bin/env --split-string=-u decoy fakeinterp\n", tools},
		{"#!/usr/bin/env -S -S fakeinterp\n", ""},
		{"#!/usr/bin/env -S fakeinterp#x\n", ""},
		{"#!/usr/bin/env -S #fakeinterp\n", ""},
		{"#!/usr/bin/env A=1\n", ""},
		{"#!/usr/bin/env -fakeinterp\n", ""},
		{"#!/usr/bin/env -S fakeinterp\r\n", ""},
		{"#!/usr/bin/envx fakeinterp\n", ""},
		{"#!/usr/bin/env /usr/bin/fakeinterp\n", ""},
		{"#!/usr/bin/env -S -- -fakeinterp\n", ""},
		{"#!/usr/bin/env -S --ignore-env fakeinterp\n", ""},
		{long, tools},
		{cut, ""},
		{"#!/usr/bin/env decoy\n", decoys},
		{"\n#!/usr/bin/env fakeinterp\n", ""},
	} {
		p := filepath.Join(t.TempDir(), "prog")
		if err := os.WriteFile(p, []byte(c.line), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := interpreterDir(p); got != c.want {
			t.Errorf("%q: %q, want %q", c.line[:min(len(c.line), 70)], got, c.want)
		}
	}
}
