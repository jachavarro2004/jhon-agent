//go:build linux

package sandbox

import (
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

// T248, the owner's option A (odd/tasks/phase1-security.md L4): bubblewrap
// 0.13 turns a --bind-fd back into the descriptor's path and resolves it
// again, and places every mount by its destination's path, so a parent a
// concurrent run swaps in that moment mounts another folder, or the right
// one elsewhere. The program therefore starts through `jhon sandbox enter`,
// run from the descriptor of jhon's own program: inside the sandbox it
// checks that each bind's destination is a mount whose folder is the one
// Jhon checked (a duplicate of its descriptor), read-only where it is, and
// only then becomes the program.

// enterRun runs program in the sandbox of b, through enter as a run starts
// it: this test binary, whose TestMain is `jhon sandbox enter`.
func enterRun(t *testing.T, bwrap string, b *Bound, program ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(bwrap, append(slices.Clone(b.Args), b.Command(program)...)...)
	cmd.ExtraFiles = b.ExtraFiles()
	cmd.WaitDelay = 10 * time.Second
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	code = cmd.ProcessState.ExitCode()
	if err != nil && code < 0 {
		t.Fatalf("bubblewrap: %v", err)
	}
	return out.String(), errOut.String(), code
}

// enterNote is enter's refusal for dest.
func enterNote(dest string) string {
	return "jhon sandbox: " + dest + " is not the folder Jhon checked (it changed while bubblewrap mounted it), so nothing was run; fix: run the command again\n"
}

// Every mount is the folder checked: the program runs, with its own argv,
// only its standard descriptors, and its write lands in the folder.
func TestEnterRunsTheProgramWhenEveryMountIsTheFolderChecked(t *testing.T) {
	bwrap, base := realSandbox(t)
	in, shared, _ := swapBox(t, base)
	b, p := Open(in)
	if p != nil {
		t.Fatalf("Open: %s", p.Reason)
	}
	defer b.Close()
	out, errOut, code := enterRun(t, bwrap, b, "sh", "-c", `echo "$0 $1"; ls /proc/$$/fd | tr '\n' ' '; echo; echo x > "$1/w" && echo wrote`, "name", shared)
	if want := "name " + shared + "\n0 1 2 \nwrote\n"; code != 0 || out != want || errOut != "" {
		t.Errorf("exit %d, stdout %q, stderr %q; want 0 and %q", code, out, errOut, want)
	}
	if _, err := os.Stat(filepath.Join(shared, "w")); err != nil {
		t.Errorf("the write did not land in %s: %v", shared, err)
	}
}

// bubblewrap placed the checked folder elsewhere: the folder that holds the
// destination became a link to the home before it mounted. Nothing runs.
func TestEnterRefusesAMountBubblewrapPlacedElsewhere(t *testing.T) {
	bwrap, base := realSandbox(t)
	in, shared, victim := swapBox(t, base)
	b, p := Open(in)
	if p != nil {
		t.Fatalf("Open: %s", p.Reason)
	}
	defer b.Close()
	moved := swap(t, filepath.Dir(shared), in.Home)
	out, errOut, code := enterRun(t, bwrap, b, "sh", "-c", `echo ran; echo x > "$1/planted"`, "sh", shared)
	if code != 126 || out != "" || errOut != enterNote(shared) {
		t.Errorf("exit %d, stdout %q, stderr %q; want 126 and %q", code, out, errOut, enterNote(shared))
	}
	for _, d := range []string{victim, filepath.Join(moved, "shared")} {
		if _, err := os.Stat(filepath.Join(d, "planted")); err == nil {
			t.Errorf("the program ran and wrote in %s", d)
		}
	}
}

// expectFd is the descriptor enter checks dest against in b.
func expectFd(t *testing.T, b *Bound, dest string) int {
	t.Helper()
	args := b.Command([]string{"true"})
	for i, a := range args {
		if a == "--expect" && i+1 < len(args) {
			d, fd, _ := strings.Cut(strings.TrimSuffix(args[i+1], ",ro"), "=")
			if d == dest {
				n, err := strconv.Atoi(fd)
				if err != nil {
					t.Fatal(err)
				}
				return n
			}
		}
	}
	t.Fatalf("no expectation for %s in %v", dest, args)
	return -1
}

// bubblewrap mounted another folder at the destination (the source path
// resolved through a swapped link): its device and inode are not the ones
// checked. Nothing runs.
func TestEnterRefusesAMountOfAnotherFolder(t *testing.T) {
	bwrap, base := realSandbox(t)
	in, shared, victim := swapBox(t, base)
	b, p := Open(in)
	if p != nil {
		t.Fatalf("Open: %s", p.Reason)
	}
	defer b.Close()
	// The expectation now names the home's folder: the bind, shared, is
	// what it does not match, as a mount of the home's folder would not
	// match the expectation of shared.
	other, err := syscall.Open(victim, oPath|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(other)
	if err := syscall.Dup3(other, expectFd(t, b, shared), syscall.O_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := enterRun(t, bwrap, b, "sh", "-c", `echo ran`)
	if code != 126 || out != "" || errOut != enterNote(shared) {
		t.Errorf("exit %d, stdout %q, stderr %q; want 126 and %q", code, out, errOut, enterNote(shared))
	}
}

// A read-only bind whose mount is writable (a remount bubblewrap gave the
// wrong mount) is not the folder checked either.
func TestEnterRefusesAReadOnlyBindThatIsWritable(t *testing.T) {
	bwrap, base := realSandbox(t)
	in, shared, _ := swapBox(t, base)
	b, p := Open(in)
	if p != nil {
		t.Fatalf("Open: %s", p.Reason)
	}
	defer b.Close()
	fd := expectFd(t, b, shared)
	out, errOut, code := enterRun(t, bwrap, b, "true")
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("exit %d, %q %q", code, out, errOut)
	}
	// The same sandbox, the writable bind expected read-only.
	cmd := b.Command([]string{"sh", "-c", "echo ran"})
	for i, a := range cmd {
		if a == shared+"="+strconv.Itoa(fd) {
			cmd[i] += ",ro"
		}
	}
	run := exec.Command(bwrap, append(slices.Clone(b.Args), cmd...)...)
	run.ExtraFiles = b.ExtraFiles()
	got, _ := run.CombinedOutput()
	if run.ProcessState.ExitCode() != 126 || string(got) != enterNote(shared) {
		t.Errorf("exit %d, %q; want 126 and %q", run.ProcessState.ExitCode(), got, enterNote(shared))
	}
}

// The program starts as bubblewrap would start it: its argv, environment,
// signal dispositions and mask, a program PATH finds, one it does not, one
// that cannot run, a script with no #! line, an empty and a missing PATH;
// the same output and exit code with enter as without it.
func TestEnterStartsTheProgramAsBubblewrapDoes(t *testing.T) {
	bwrap, base := realSandbox(t)
	in, _, _ := swapBox(t, base)
	script, plain := filepath.Join(base, "script"), filepath.Join(base, "plain")
	if err := os.WriteFile(script, []byte(`echo script "$0" "$@"`+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plain, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	look := `printf '%s|' "$0" "$@"; echo; env | LC_ALL=C sort; grep -E '^Sig(Ign|Blk)' /proc/$$/status; tr '\0' ' ' < /proc/$$/cmdline; echo`
	for _, c := range []struct {
		name    string
		env     []string
		program []string
	}{
		{"a program PATH finds", nil, []string{"sh", "-c", look, "zero", "a b", ""}},
		{"a path", nil, []string{"/bin/sh", "-c", look, "zero"}},
		{"a program PATH does not find", nil, []string{"no-such-program-t248", "x"}},
		{"a file that cannot run", nil, []string{plain, "x"}},
		{"a script with no #! line", nil, []string{script, "a", "b"}},
		{"an empty PATH", []string{"--setenv", "PATH", ""}, []string{"true"}},
		{"no PATH", []string{"--unsetenv", "PATH"}, []string{"sh", "-c", "echo $PATH"}},
	} {
		b, p := Open(in)
		if p != nil {
			t.Fatalf("Open: %s", p.Reason)
		}
		run := func(command []string) (string, int) {
			args := slices.Concat(b.Args, c.env, command)
			cmd := exec.Command(bwrap, args...)
			cmd.ExtraFiles = b.ExtraFiles()
			cmd.WaitDelay = 10 * time.Second
			out, _ := cmd.CombinedOutput()
			return string(out), cmd.ProcessState.ExitCode()
		}
		direct, directCode := run(append([]string{"--"}, c.program...))
		entered, enteredCode := run(b.Command(c.program))
		if entered != direct || enteredCode != directCode {
			t.Errorf("%s: through enter exit %d,\n%s\nwithout it exit %d,\n%s", c.name, enteredCode, entered, directCode, direct)
		}
		b.Close()
	}
}

// Outside Jhon's sandbox, or with arguments it does not take, enter runs
// nothing: it is the sandbox's last step and nothing else.
func TestEnterRunsNothingOutsideTheSandbox(t *testing.T) {
	_, base := realSandbox(t)
	marker := filepath.Join(base, "ran")
	for _, c := range []struct {
		args       []string
		code       int
		stderrHead string
	}{
		{[]string{"--", "sh", "-c", `echo ran > "$1"`, "sh", marker}, 126, "jhon sandbox: / is not read-only, so this is not Jhon's sandbox and nothing was run"},
		{[]string{"sh", "-c", `echo ran > "$1"`, "sh", marker}, 2, "usage: jhon sandbox enter"},
		{[]string{"--expect", "relative=3", "--", "true"}, 2, "usage: jhon sandbox enter"},
		{[]string{"--expect", "/tmp=1", "--", "true"}, 2, "usage: jhon sandbox enter"},
		{[]string{"--expect", "/tmp", "--", "true"}, 2, "usage: jhon sandbox enter"},
	} {
		cmd := exec.Command(os.Args[0], append([]string{"sandbox", "enter"}, c.args...)...)
		var errOut strings.Builder
		cmd.Stderr = &errOut
		_ = cmd.Run()
		if cmd.ProcessState.ExitCode() != c.code || !strings.HasPrefix(errOut.String(), c.stderrHead) {
			t.Errorf("%v: exit %d, stderr %q; want %d and %q", c.args, cmd.ProcessState.ExitCode(), errOut.String(), c.code, c.stderrHead)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("enter ran the program outside the sandbox")
	}
}
