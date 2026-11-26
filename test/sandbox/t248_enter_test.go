package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T248, the owner's option A (odd/tasks/phase1-security.md L4): every
// sandboxed program starts through `jhon sandbox enter`, which checks inside
// the sandbox that each bind is the folder Jhon checked, mounted where it
// was meant to be, and only then becomes the program.

// The race the verify measured with bubblewrap alone: a run exchanges
// work/sub and a link to the home back and forth while bubblewrap binds
// work/sub/victim from a descriptor opened as jhon opens it, and the
// program starts through enter, as jhon starts it. bubblewrap alone mounted
// the home's victim in about 1 run of 100 (49 of 5315, 26 of them written
// through); through enter none of those runs starts, and none writes the
// home.
func TestTheStandAloneBubblewrapRaceNeverWritesTheHomeThroughEnter(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("skipped: no python3 for the swapping process")
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("skipped: no bubblewrap")
	}
	b := newBox(t)
	work := filepath.Join(b.home, "work")
	victim, home := filepath.Join(work, "sub", "victim"), filepath.Join(b.home, "victim")
	mkdirs(t, victim, home)
	support.Write(t, b.base, map[string]string{"flip.py": `import ctypes, os, sys, time
libc = ctypes.CDLL(None, use_errno=True)
w, end = sys.argv[1], time.time() + float(sys.argv[2])
a, l = (w + "/sub").encode(), (w + "/sublink").encode()
os.symlink("..", w + "/sublink")
while time.time() < end:
    libc.renameat2(-100, a, -100, l, 2)
`})
	seconds, _ := strconv.Atoi(raceSeconds())
	flip := exec.Command("python3", "-I", filepath.Join(b.base, "flip.py"), work, strconv.Itoa(seconds))
	if err := flip.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { flip.Process.Kill(); flip.Wait() })
	exe, err := os.Open(support.Bin(t))
	if err != nil {
		t.Fatal(err)
	}
	defer exe.Close()
	runs, started, refused := 0, 0, 0
	end := time.Now().Add(time.Duration(seconds)*time.Second - time.Second)
	for time.Now().Before(end) {
		fd, err := openNoLink(victim)
		if err != nil {
			continue
		}
		dup, err := syscall.Dup(fd)
		if err != nil {
			t.Fatal(err)
		}
		runs++
		bind, keep := os.NewFile(uintptr(fd), "bind"), os.NewFile(uintptr(dup), "keep")
		cmd := exec.Command(bwrap, "--ro-bind", "/", "/", "--dev-bind", "/dev", "/dev", "--proc", "/proc", "--bind-fd", "3", victim,
			"--", "/proc/self/fd/5", "sandbox", "enter", "--expect", victim+"=4", "--",
			"sh", "-c", `echo x > "$1/h-$3" 2>/dev/null; echo x > "$2/v-$3" 2>/dev/null; echo ran`, "sh", home, victim, strconv.Itoa(runs))
		cmd.ExtraFiles = []*os.File{bind, keep, exe}
		cmd.Env = b.env
		var out, errOut strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &errOut
		_ = cmd.Run()
		bind.Close()
		keep.Close()
		switch {
		case out.String() == "ran\n":
			started++
		case out.String() == "" && strings.HasPrefix(errOut.String(), "jhon sandbox: "+victim) && strings.HasSuffix(errOut.String(), enterRefused):
			refused++
		case out.String() == "" && strings.HasPrefix(errOut.String(), "bwrap: "):
			// bubblewrap itself saw the race and refused.
		default:
			t.Errorf("run %d: exit %d, stdout %q, stderr %q", runs, cmd.ProcessState.ExitCode(), out.String(), errOut.String())
		}
	}
	entries, _ := os.ReadDir(home)
	if len(entries) > 0 {
		var names []string
		for _, e := range entries[:min(5, len(entries))] {
			names = append(names, e.Name())
		}
		t.Errorf("%d of %d runs wrote the home folder %s (%v)", len(entries), runs, home, names)
	}
	if started == 0 {
		t.Errorf("none of %d runs started its program", runs)
	}
	t.Logf("%d runs: %d started, %d refused by enter, %d refused by bubblewrap", runs, started, refused, runs-started-refused)
}

// openNoLink opens dir as jhon opens a bind (openat2 from /, no link on
// the way followed, RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS), an O_PATH
// descriptor: O_NOFOLLOW alone would follow a link in a parent, and hand
// bubblewrap the home's folder itself.
func openNoLink(dir string) (int, error) {
	p, err := syscall.BytePtrFromString(dir)
	if err != nil {
		return -1, err
	}
	how := struct{ flags, mode, resolve uint64 }{0x200000 | syscall.O_DIRECTORY | syscall.O_CLOEXEC, 0, 0x04 | 0x02}
	fd, _, e := syscall.Syscall6(437, ^uintptr(99), uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&how)), unsafe.Sizeof(how), 0, 0)
	if e != 0 {
		return -1, e
	}
	return int(fd), nil
}

// Inside the sandbox jhon sandbox enter is the same check and nothing more:
// with no expectation it only becomes the program, and with one that does
// not hold it runs nothing.
func TestJhonSandboxEnterInsideTheSandboxOnlyChecksAndExecs(t *testing.T) {
	b := newBox(t)
	r := b.sh(t, b.proj, "", fmt.Sprintf(`J=%q; "$J" sandbox enter -- sh -c 'echo inside "$0"' zero; echo code $?; exec 3</dev/null; "$J" sandbox enter --expect %q=3 -- sh -c 'echo ran'; echo code $?; "$J" sandbox enter sh -c 'echo ran'; echo code $?`, support.Bin(t), b.proj))
	want := "inside zero\ncode 0\ncode 126\ncode 2\n"
	if r.Code != 0 || r.Stdout != want || !strings.Contains(r.Stderr, "jhon sandbox: "+b.proj+enterRefused) || !strings.Contains(r.Stderr, "usage: jhon sandbox enter") {
		t.Errorf("exit %d, stdout %q, stderr %q; want %q and the notes", r.Code, r.Stdout, r.Stderr, want)
	}
}

// The program gets no trace of enter: its command line is its own, and no
// variable names enter or its descriptors.
func TestTheProgramGetsNoTraceOfEnter(t *testing.T) {
	b := newBox(t)
	r := support.Jhon(t, b.proj, b.env, "sandbox", "run", "--", "sh", "-c", `tr '\0' '|' < /proc/$$/cmdline; echo; env`, "zero", "a b")
	lines := strings.SplitN(r.Stdout, "\n", 2)
	if want := "sh|-c|tr '\\0' '|' < /proc/$$/cmdline; echo; env|zero|a b|"; r.Code != 0 || lines[0] != want {
		t.Errorf("exit %d, command line %q; want %q (stderr %q)", r.Code, lines[0], want, r.Stderr)
	}
	for _, mark := range []string{"enter", "--expect", "/proc/self/fd"} {
		if len(lines) > 1 && strings.Contains(lines[1], mark) {
			t.Errorf("the environment names %s:\n%s", mark, lines[1])
		}
	}
}

// bubblewrap's own refusal of a folder that changed while it mounted it
// (exit 1, "bwrap: Race condition binding dirfd: …", and enter never
// started the program): a supervised run keeps its line and adds what it
// means. One that exits otherwise gets no such note, nor does a program
// that printed the line itself (TestT248V3AProgramsOwnRaceLineGetsNoNothingWasRunNote).
func TestBubblewrapsOwnRaceRefusalGetsJhonsNote(t *testing.T) {
	b := newBox(t)
	const line = "bwrap: Race condition binding dirfd: File exists\n"
	const note = "jhon sandbox: bubblewrap refused the sandbox because a folder changed while it mounted it, so nothing was run; fix: run the command again\n"
	for _, c := range []struct {
		name, script, stdout, stderr string
	}{
		{"bubblewrap refused", "echo 'bwrap: Race condition binding dirfd: File exists' >&2; exit 1", "", line + note},
		{"another exit", "echo 'bwrap: Race condition binding dirfd: File exists' >&2; exit 2", "", line},
	} {
		env := t248StandIn(t, b, c.script)
		r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "true")
		if r.Stdout != c.stdout || r.Stderr != c.stderr {
			t.Errorf("%s: exit %d, stdout %q, stderr %q; want %q, %q", c.name, r.Code, r.Stdout, r.Stderr, c.stdout, c.stderr)
		}
	}
}
