package sandbox

// T248's adversarial verify (odd/tasks/phase1-security.md S3, L4), kept as
// regressions: a folder swapped for a link after jhon's checks never leads
// a sandboxed program to write or read outside the folder jhon checked; the
// descriptor bubblewrap binds is reached through the path the checks judged,
// with a concurrent run swapping a parent back and forth; and no descriptor
// of a host folder, jhon's or its caller's, reaches the program.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// t248StandIn is the box's environment with a bwrap first on PATH, outside
// every writable area, that runs swap (sh) and then the real bubblewrap:
// the swap lands after every check of jhon's and before any mount.
func t248StandIn(t *testing.T, b box, swap string) []string {
	t.Helper()
	real, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("skipped: no bubblewrap on this machine")
	}
	bin := filepath.Join(b.base, "t248-bin")
	script(t, filepath.Join(bin, "bwrap"), "#!/bin/sh\nprintf '%s\\n' \"$@\" > '"+filepath.Join(b.base, "t248-args.log")+"'\n"+swap+"\nexec '"+real+"' \"$@\"\n")
	return t248Env(b.env, "PATH="+bin+":"+os.Getenv("PATH"))
}

// t248Env is env with each KEY=VALUE of set replacing the key's entry.
func t248Env(env []string, set ...string) []string {
	out := slices.Clone(env)
	for _, kv := range set {
		k, _, _ := strings.Cut(kv, "=")
		out = slices.DeleteFunc(out, func(e string) bool { return strings.HasPrefix(e, k+"=") })
		out = append(out, kv)
	}
	return out
}

// Every writable area of a run (a linked worktree, its main root and
// <root>-worktrees, the git common dir, a cache a variable names, a
// sandbox.allow_write folder, $TMPDIR) lies in work/sub, a folder another
// project's sandboxed run can write. After the checks work/sub becomes a
// link to the home, where a folder of each name lies: bubblewrap then
// mounts the checked folders where the link leads, and the run is refused
// before the program starts (jhon sandbox enter), so nothing is written,
// in the home or in the folders jhon checked.
func TestT248VerifyEveryWritableBindSwappedAfterTheChecksIsNotWrittenThrough(t *testing.T) {
	for _, c := range []struct{ name, link string }{{"relative link", ".."}, {"absolute link", ""}} {
		t.Run(c.name, func(t *testing.T) {
			b := newBox(t)
			sub := filepath.Join(b.home, "work", "sub")
			main, wt := filepath.Join(sub, "main"), filepath.Join(sub, "wt")
			mkdirs(t, main, filepath.Join(sub, "main-worktrees"), filepath.Join(sub, "gocache"), filepath.Join(sub, "aw"), filepath.Join(sub, "vtmp"))
			support.GitRepo(t, main)
			support.Git(t, main, "worktree", "add", "-q", wt)
			names := []string{"wt", "main", "main/.git", "main-worktrees", "gocache", "aw", "vtmp"}
			for _, n := range names {
				mkdirs(t, filepath.Join(b.home, n))
			}
			b.settings(t, "[sandbox]\nallow_write = [\""+filepath.Join(sub, "aw")+"\"]\n")
			link := c.link
			if link == "" {
				link = b.home
			}
			env := t248Env(t248StandIn(t, b, "mv '"+sub+"' '"+sub+".moved' && ln -s '"+link+"' '"+sub+"' || exit 99"),
				"TMPDIR="+filepath.Join(sub, "vtmp"), "GOCACHE="+filepath.Join(sub, "gocache"))
			var folders []string
			for _, n := range names {
				folders = append(folders, filepath.Join(sub, n))
			}
			r := support.Jhon(t, wt, env, append([]string{"sandbox", "run", "--", "sh", "-c", `for d in "$@"; do echo x > "$d/planted" 2>/dev/null; done; :`, "sh"}, folders...)...)
			for _, n := range names {
				if exists(filepath.Join(b.home, n, "planted")) {
					t.Errorf("the write to work/sub/%s went through the link into the home folder %s", n, filepath.Join(b.home, n))
				}
			}
			if r.Code != 126 || r.Stdout != "" || !strings.HasPrefix(r.Stderr, "jhon sandbox: "+sub+"/") || !strings.HasSuffix(r.Stderr, enterRefused) {
				t.Errorf("exit %d, stdout %q, stderr %q; want 126 and the note", r.Code, r.Stdout, r.Stderr)
			}
			for _, n := range names {
				if exists(filepath.Join(sub+".moved", n, "planted")) {
					t.Errorf("the program ran: it wrote work/sub/%s", n)
				}
			}
		})
	}
}

// A PATH folder bound read-only inside $TMPDIR, tmp/a/herdr: after the
// checks tmp/a becomes a link to ~/.config, where the hidden folder herdr
// lies. Its contents never show inside the sandbox.
func TestT248VerifyAReadOnlyBindSwappedAfterTheChecksShowsNoHiddenFolder(t *testing.T) {
	b := newBox(t)
	a := filepath.Join(b.tmp, "a")
	mkdirs(t, filepath.Join(a, "herdr"))
	support.Write(t, b.home, map[string]string{".config/herdr/note.txt": "HIDDEN-NOTE"})
	env := t248StandIn(t, b, "mv '"+a+"' '"+a+".moved' && ln -s ../.config '"+a+"' || exit 99")
	env = t248Env(env, "PATH="+filepath.Join(a, "herdr")+":"+strings.TrimPrefix(env[slices.IndexFunc(env, func(e string) bool { return strings.HasPrefix(e, "PATH=") })], "PATH="))
	r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `cat "$1" "$2" 2>/dev/null; :`, "sh",
		filepath.Join(a, "herdr", "note.txt"), filepath.Join(b.home, ".config", "herdr", "note.txt"))
	if strings.Contains(r.Stdout, "HIDDEN-NOTE") {
		t.Errorf("the hidden folder ~/.config/herdr showed through the swapped read-only bind: exit %d, stdout %q", r.Code, r.Stdout)
	}
}

// A concurrent sandboxed run of another project (work) swaps work/sub and a
// link to the home back and forth (renameat2 RENAME_EXCHANGE) while runs
// of the project work/sub/victim write their own folder: no write reaches
// the home folder victim.
func TestT248VerifyAConcurrentRunSwappingAFolderNeverLeadsAnotherRunToWriteTheHome(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("skipped: no python3 for the swapping run")
	}
	b := newBox(t)
	work := filepath.Join(b.home, "work")
	victim := filepath.Join(work, "sub", "victim")
	mkdirs(t, victim, filepath.Join(b.home, "victim"))
	support.GitRepo(t, work)
	support.GitRepo(t, victim)
	support.Write(t, work, map[string]string{"flip.py": `import ctypes, os, sys, time
libc = ctypes.CDLL(None, use_errno=True)
w, end, n = sys.argv[1], time.time() + float(sys.argv[2]), 0
a, l = (w + "/sub").encode(), (w + "/sublink").encode()
os.symlink("..", w + "/sublink")
while time.time() < end:
    for _ in range(2):
        if libc.renameat2(-100, a, -100, l, 2) != 0:
            sys.exit("renameat2: " + os.strerror(ctypes.get_errno()))
        time.sleep(0.001)
    n += 1
    time.sleep(0.001)
print("flips", n)
`})
	venv := t248StandIn(t, b, "")
	done := make(chan support.Result, 1)
	go func() {
		done <- support.Jhon(t, work, b.env, "sandbox", "run", "--", "python3", filepath.Join(work, "flip.py"), work, raceSeconds())
	}()
	time.Sleep(time.Second)
	runs := 0
	var attacker support.Result
loop:
	for {
		select {
		case attacker = <-done:
			break loop
		default:
		}
		runs++
		vr := support.Jhon(t, victim, venv, "sandbox", "run", "--", "sh", "-c", fmt.Sprintf(`echo x > %q`, filepath.Join(victim, fmt.Sprintf("planted-%d", runs))))
		if exists(filepath.Join(b.home, "victim", fmt.Sprintf("planted-%d", runs))) {
			args, _ := os.ReadFile(filepath.Join(b.base, "t248-args.log"))
			t.Logf("run %d wrote the home: exit %d, stderr %q, bubblewrap's binds: %s", runs, vr.Code, vr.Stderr, regexp.MustCompile(`--bind(-fd \d+)? \S+`).FindAllString(strings.ReplaceAll(string(args), "\n", " "), -1))
		}
	}
	if !strings.Contains(attacker.Stdout, "flips") {
		t.Fatalf("the swapping run did not swap: exit %d, %q %q", attacker.Code, attacker.Stdout, attacker.Stderr)
	}
	entries, _ := os.ReadDir(filepath.Join(b.home, "victim"))
	if len(entries) > 0 {
		t.Errorf("%d of %d runs wrote through the swapped link into the home folder %s", len(entries), runs, filepath.Join(b.home, "victim"))
	}
	t.Logf("%d victim runs; %s", runs, strings.TrimSpace(attacker.Stdout))
}

// fdLine is one `ls -l /proc/<pid>/fd` entry: the number and its target.
var fdLine = regexp.MustCompile(`(?m) (\d+) -> (.*)$`)

// The program inside, supervised (pipes) and on a terminal (exec), and
// bubblewrap's init (pid 1) hold no descriptor of a host folder, and no
// descriptor above 2 lets the program write through it.
func TestT248VerifyTheProgramHoldsNoDescriptorOfAHostFolder(t *testing.T) {
	b := newBox(t)
	outside := filepath.Join(b.base, "outside")
	mkdirs(t, outside)
	prog := `ls -l /proc/$$/fd /proc/1/fd; for n in $(seq 3 64); do echo x > /proc/self/fd/$n/planted 2>/dev/null && echo "WROTE through $n"; done; :`
	check := func(t *testing.T, out string) {
		t.Helper()
		for _, m := range fdLine.FindAllStringSubmatch(out, -1) {
			n, target := m[1], strings.TrimRight(m[2], "\r")
			if fi, err := os.Stat(target); err == nil && fi.IsDir() && strings.HasPrefix(target, "/") {
				t.Errorf("descriptor %s leads to the host folder %s", n, target)
			}
		}
		if strings.Contains(out, "WROTE") {
			t.Errorf("the program wrote through an inherited descriptor:\n%s", out)
		}
		if !strings.Contains(out, " 2 -> ") {
			t.Errorf("no descriptor listing:\n%s", out)
		}
	}
	t.Run("supervised", func(t *testing.T) {
		r := b.sh(t, b.proj, "", prog)
		check(t, r.Stdout)
	})
	t.Run("terminal", func(t *testing.T) {
		if _, err := exec.LookPath("script"); err != nil {
			t.Skip("skipped: no script(1) for a terminal")
		}
		line := support.Bin(t) + ` sandbox run -- sh -c '` + prog + `'`
		r := support.Run(t, support.Cmd{Args: []string{"script", "-qec", line, "/dev/null"}, Dir: b.proj, Env: b.env})
		check(t, r.Stdout)
	})
}

// t248NoOpenat2 is a program that runs its arguments with openat2 refused
// by a seccomp filter: ENOSYS (a kernel before 5.6) or EPERM (a container's
// profile), as its first argument says.
const t248NoOpenat2 = `#include <stddef.h>
#include <stdio.h>
#include <unistd.h>
#include <errno.h>
#include <sys/prctl.h>
#include <linux/seccomp.h>
#include <linux/filter.h>
#include <linux/audit.h>
int main(int argc, char **argv) {
  int e = argv[1][1] == 'N' ? ENOSYS : EPERM;
  struct sock_filter f[] = {
    BPF_STMT(BPF_LD|BPF_W|BPF_ABS, offsetof(struct seccomp_data, arch)),
    BPF_JUMP(BPF_JMP|BPF_JEQ|BPF_K, AUDIT_ARCH_X86_64, 1, 0),
    BPF_STMT(BPF_RET|BPF_K, SECCOMP_RET_ALLOW),
    BPF_STMT(BPF_LD|BPF_W|BPF_ABS, offsetof(struct seccomp_data, nr)),
    BPF_JUMP(BPF_JMP|BPF_JEQ|BPF_K, 437, 0, 1),
    BPF_STMT(BPF_RET|BPF_K, SECCOMP_RET_ERRNO | (e & SECCOMP_RET_DATA)),
    BPF_STMT(BPF_RET|BPF_K, SECCOMP_RET_ALLOW),
  };
  struct sock_fprog p = { .len = sizeof(f)/sizeof(f[0]), .filter = f };
  if (prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) || prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, &p)) { perror("seccomp"); return 3; }
  execvp(argv[2], argv + 2); perror("exec"); return 4;
}
`

// Where openat2 is missing or refused, status and doctor say the sandbox
// cannot run, and run refuses with exit 126 before bubblewrap starts: no
// fallback to a bind by path.
func TestT248VerifyNoOpenat2FailsClosed(t *testing.T) {
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("skipped: no C compiler for the seccomp filter")
	}
	b := newBox(t)
	helper := filepath.Join(b.base, "noopenat2")
	support.Write(t, b.base, map[string]string{"noopenat2.c": t248NoOpenat2})
	if out, err := exec.Command(cc, "-o", helper, helper+".c").CombinedOutput(); err != nil {
		t.Skipf("skipped: cc: %v %s", err, out)
	}
	b.settings(t, "[sandbox]\nmode = \"on\"\n")
	env := t248Env(t248StandIn(t, b, ""), "JHON_SOURCE="+support.Root())
	called := filepath.Join(b.base, "t248-args.log")
	for _, c := range []struct{ errno, reason, fix string }{
		{"ENOSYS", "this kernel has no openat2 (Linux 5.6 or newer), which the sandbox needs to bind its folders with no link followed", "a Linux kernel 5.6 or newer"},
		{"EPERM", "openat2 is refused here (operation not permitted: a seccomp filter, as a container's, blocks it), which the sandbox needs to bind its folders with no link followed", "allow openat2 in the seccomp filter jhon runs under (a container's profile), or run jhon outside it"},
	} {
		t.Run(c.errno, func(t *testing.T) {
			jhon := func(args ...string) support.Result {
				return support.Run(t, support.Cmd{Args: append([]string{helper, c.errno, support.Bin(t)}, args...), Dir: b.proj, Env: env})
			}
			if r := jhon("sandbox", "status", "--json"); !strings.Contains(r.Stdout, `"can_run":false`) || !strings.Contains(r.Stdout, `"reason":"`+c.reason+`"`) || !strings.Contains(r.Stdout, `"fix":"`+c.fix+`"`) {
				t.Errorf("status --json: exit %d, %q", r.Code, r.Stdout)
			}
			if r := jhon("doctor"); !strings.Contains(r.Stdout, "\nsandbox\n  FAIL  sandbox on; it cannot run here: "+c.reason+"; fix: "+c.fix+";") {
				t.Errorf("doctor:\n%s", r.Out())
			}
			os.Remove(called)
			marker := filepath.Join(b.proj, "ran")
			r := jhon("sandbox", "run", "--", "sh", "-c", `echo ran > "$1"`, "sh", marker)
			if want := "jhon sandbox: " + c.reason + ", so nothing was run; fix: " + c.fix + "\n"; r.Code != 126 || r.Stderr != want {
				t.Errorf("run: exit %d, stderr %q; want 126, %q", r.Code, r.Stderr, want)
			}
			if exists(marker) || exists(called) {
				t.Errorf("run started bubblewrap (%v) or the program (%v)", exists(called), exists(marker))
			}
		})
	}
}

// A PATH folder bound read-only inside $TMPDIR, tmp/a/bin. After the
// checks tmp/a becomes a link to tmp/decoy: bubblewrap resolves a mount's
// destination by path, so the read-only mount lands on tmp/decoy/bin, and
// the PATH folder itself, now tmp/a.moved/bin, would stay writable inside
// the sandbox (pre-existing at 889c070). jhon sandbox enter finds tmp/a/bin
// is not that mount and runs nothing.
func TestT248VerifyAReadOnlyBindsDestinationSwappedLeavesThePATHFolderReadOnly(t *testing.T) {
	b := newBox(t)
	a := filepath.Join(b.tmp, "a")
	mkdirs(t, filepath.Join(a, "bin"), filepath.Join(b.tmp, "decoy", "bin"))
	env := t248StandIn(t, b, "mv '"+a+"' '"+a+".moved' && ln -s decoy '"+a+"' || exit 99")
	env = t248Env(env, "PATH="+filepath.Join(a, "bin")+":"+strings.TrimPrefix(env[slices.IndexFunc(env, func(e string) bool { return strings.HasPrefix(e, "PATH=") })], "PATH="))
	support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `echo x > "$1/planted" 2>/dev/null; :`, "sh", filepath.Join(a+".moved", "bin"))
	if exists(filepath.Join(a+".moved", "bin", "planted")) {
		t.Errorf("the sandboxed program wrote into the PATH folder %s, which the sandbox binds read-only", filepath.Join(a, "bin"))
	}
}

// A descriptor of a host folder the caller leaves open (not close-on-exec)
// does not reach the program, supervised or on a terminal (exec): jhon
// marks every inherited descriptor above 2 close-on-exec before it starts
// bubblewrap (pre-existing at 889c070; at 738da19 still above the highest
// bind descriptor, here 61).
func TestT248VerifyACallersDescriptorOfAHostFolderDoesNotReachTheProgram(t *testing.T) {
	b := newBox(t)
	outside := filepath.Join(b.base, "outside")
	mkdirs(t, outside)
	dir, err := os.Open(outside)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	extra := make([]*os.File, 58)
	extra = append(extra, dir) // descriptor 61 in jhon
	prog := `for n in $(seq 3 64); do echo x > /proc/self/fd/$n/planted-$n 2>/dev/null; done; :`
	for _, c := range []struct {
		name string
		args []string
	}{
		{"supervised", []string{support.Bin(t), "sandbox", "run", "--", "sh", "-c", prog}},
		{"terminal", []string{"script", "-qec", support.Bin(t) + ` sandbox run -- sh -c '` + prog + `'`, "/dev/null"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			cmd := exec.Command(c.args[0], c.args[1:]...)
			cmd.Dir, cmd.Env, cmd.ExtraFiles = b.proj, b.env, extra
			out, _ := cmd.CombinedOutput()
			if planted, _ := filepath.Glob(filepath.Join(outside, "planted-*")); len(planted) > 0 {
				t.Errorf("the program wrote %v through the caller's descriptor of %s (%q)", planted, outside, out)
				for _, p := range planted {
					os.Remove(p)
				}
			}
		})
	}
}

// enterRefused ends jhon sandbox enter's refusal of a mount that is not the
// folder Jhon checked.
const enterRefused = " is not the folder Jhon checked (it changed while bubblewrap mounted it), so nothing was run; fix: run the command again\n"

// raceSeconds is how long a race test's swapping runs: T248_RACE_SECONDS,
// else 25 (about 1000 victim runs on a quiet machine).
func raceSeconds() string {
	if s := os.Getenv("T248_RACE_SECONDS"); s != "" {
		return s
	}
	return "25"
}
