package sandbox

// T248's second verify (odd/tasks/phase1-security.md S3, after 86ee602),
// kept as regressions: races against jhon sandbox enter and the anchors (the
// project, a linked worktree's main worktree and common dir, $TMPDIR, a
// workspace member), the project the hook and the status pin, what reaches
// the program, how it starts, and the hidden paths.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// t248v2Flip is the swapping program: it exchanges <work>/sub and a link to
// ".." (the home) back and forth for <seconds>.
const t248v2Flip = `import ctypes, os, sys, time
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
`

// t248v2Seconds is how long each race runs (T248V2_SECONDS, default 25).
func t248v2Seconds() string {
	if s := os.Getenv("T248V2_SECONDS"); s != "" {
		return s
	}
	return "25"
}

var t248v2Digits = regexp.MustCompile(`[0-9]+`)

// t248v2Race runs a sandboxed run of work (a git repo) that swaps work/sub
// with a link to the home while runs started by victim (each given its run
// number) go on; it fails when any decoy (a home folder) gains an entry or
// a refusal is not exit 126 with jhon's note, and logs the counts.
func t248v2Race(t *testing.T, b box, work string, decoys []string, victim func(n int) support.Result) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("skipped: no python3 for the swapping run")
	}
	support.Write(t, work, map[string]string{"flip.py": t248v2Flip})
	done := make(chan support.Result, 1)
	go func() {
		done <- support.Jhon(t, work, b.env, "sandbox", "run", "--", "python3", filepath.Join(work, "flip.py"), work, t248v2Seconds())
	}()
	time.Sleep(time.Second)
	runs, started, refused, bwrapRefused := 0, 0, 0, 0
	other := map[string]int{}
	var attacker support.Result
loop:
	for {
		select {
		case attacker = <-done:
			break loop
		default:
		}
		runs++
		r := victim(runs)
		switch {
		case r.Code == 0:
			started++
		case r.Code == 126 && strings.HasPrefix(r.Stderr, "jhon sandbox: ") && strings.Contains(r.Stderr, ", so nothing was run; fix: "):
			refused++
		case r.Code != 0 && strings.Contains(r.Stderr, ": Read-only file system"):
			// The program ran; its own path went through the swapped link
			// to the read-only home while it ran.
			started++
		case r.Code == 1 && t248v2BwrapRaced(r.Stderr) && strings.Contains(r.Stderr, "\njhon sandbox: bubblewrap refused the sandbox because a folder changed while it mounted it, so nothing was run; fix: run the command again\n"):
			// bubblewrap's own check refused the run, in its words (exit 1),
			// with jhon's note: a folder changed, or one it binds inside the
			// project is gone (T250: the project's .claude, read-only), when
			// it opened the descriptor's path again.
			bwrapRefused++
		default:
			line, _, _ := strings.Cut(r.Stderr, "\n")
			other[fmt.Sprintf("exit %d: %s", r.Code, t248v2Digits.ReplaceAllString(strings.ReplaceAll(line, b.base, "B"), "N"))]++
		}
	}
	if !strings.Contains(attacker.Stdout, "flips") {
		t.Fatalf("the swapping run did not swap: exit %d, %q %q", attacker.Code, attacker.Stdout, attacker.Stderr)
	}
	for _, d := range decoys {
		var names []string
		_ = filepath.WalkDir(d, func(p string, e os.DirEntry, err error) error {
			if err == nil && strings.Contains(e.Name(), "planted-") {
				names = append(names, p)
			}
			return nil
		})
		if len(names) > 0 {
			t.Errorf("%d of %d runs wrote the home folder %s: %v", len(names), runs, d, names[:min(3, len(names))])
		}
	}
	for k, n := range other {
		t.Errorf("%d runs ended neither started nor refused with exit 126 and jhon's note: %s", n, k)
	}
	if started == 0 {
		t.Errorf("none of %d runs started its program", runs)
	}
	t.Logf("%d runs: %d started, %d refused by jhon (126 with the note), %d refused by bubblewrap (exit 1, \"bwrap: Race condition binding dirfd\"), %d other; %s", runs, started, refused, bwrapRefused, runs-started-refused-bwrapRefused, strings.TrimSpace(attacker.Stdout))
}

// t248v2BwrapRaced is whether stderr opens with bubblewrap's refusal of a
// folder that changed while it mounted it: its race check, or a bind whose
// source or destination is gone (T250: the read-only binds in the project).
func t248v2BwrapRaced(stderr string) bool {
	line, _, _ := strings.Cut(stderr, "\n")
	return strings.HasPrefix(line, "bwrap: Race condition binding dirfd") || strings.HasPrefix(line, "bwrap: Can't mount on symlink destination ") || strings.HasPrefix(line, "bwrap: Can't bind mount ") || strings.HasPrefix(line, "bwrap: Can't ") && strings.HasSuffix(line, ": No such file or directory")
}

// The project: work/sub/victim, its parent swapped for a link to the home,
// where victim lies.
func TestT248V2RaceTheProject(t *testing.T) {
	b := newBox(t)
	work := filepath.Join(b.home, "work")
	victim := filepath.Join(work, "sub", "victim")
	mkdirs(t, victim, filepath.Join(b.home, "victim"))
	support.GitRepo(t, work)
	support.GitRepo(t, victim)
	t248v2Race(t, b, work, []string{filepath.Join(b.home, "victim")}, func(n int) support.Result {
		return support.Jhon(t, victim, b.env, "sandbox", "run", "--", "sh", "-c", `echo x > "$1"`, "sh", filepath.Join(victim, fmt.Sprintf("planted-%d", n)))
	})
}

// A linked worktree, work/wt, whose main worktree work/sub/main (and its
// common dir and main-worktrees) lies in the swapped folder; the home holds
// a repository main and main-worktrees.
func TestT248V2RaceTheMainWorktreeAndCommonDir(t *testing.T) {
	b := newBox(t)
	work := filepath.Join(b.home, "work")
	main, wt := filepath.Join(work, "sub", "main"), filepath.Join(work, "wt")
	mkdirs(t, main, main+"-worktrees", filepath.Join(b.home, "main"), filepath.Join(b.home, "main-worktrees"))
	support.GitRepo(t, work)
	support.GitRepo(t, main)
	support.Git(t, main, "worktree", "add", "-q", wt)
	support.GitRepo(t, filepath.Join(b.home, "main"))
	t248v2Race(t, b, work, []string{filepath.Join(b.home, "main"), filepath.Join(b.home, "main-worktrees")}, func(n int) support.Result {
		f := fmt.Sprintf("planted-%d", n)
		return support.Jhon(t, wt, b.env, "sandbox", "run", "--", "sh", "-c", `for f in "$@"; do echo x > "$f" 2>/dev/null; done; :`, "sh",
			filepath.Join(main, f), filepath.Join(main, ".git", f), filepath.Join(main+"-worktrees", f), filepath.Join(wt, f))
	})
}

// $TMPDIR in the swapped folder: work/sub/vtmp; the home holds vtmp.
func TestT248V2RaceTMPDIR(t *testing.T) {
	b := newBox(t)
	work := filepath.Join(b.home, "work")
	vtmp := filepath.Join(work, "sub", "vtmp")
	mkdirs(t, vtmp, filepath.Join(b.home, "vtmp"))
	support.GitRepo(t, work)
	env := t248Env(b.env, "TMPDIR="+vtmp)
	t248v2Race(t, b, work, []string{filepath.Join(b.home, "vtmp")}, func(n int) support.Result {
		return support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `echo x > "$TMPDIR/planted-$1"`, "sh", strconv.Itoa(n))
	})
}

// A joined workspace member in the swapped folder: the project lists
// ../work/sub/mem, which joined it; the home holds a repository mem.
func TestT248V2RaceAWorkspaceMember(t *testing.T) {
	b := newBox(t)
	work := filepath.Join(b.home, "work")
	mem := filepath.Join(work, "sub", "mem")
	mkdirs(t, mem, filepath.Join(b.home, "mem"))
	support.GitRepo(t, work)
	support.GitRepo(t, mem)
	support.GitRepo(t, filepath.Join(b.home, "mem"))
	support.Write(t, b.proj, map[string]string{"ldd/config.yaml": "workspace:\n  mem: ../work/sub/mem\n"})
	if r := support.Jhon(t, mem, b.env, "workspace", "join", b.proj); r.Code != 0 {
		t.Fatalf("workspace join: %d %s", r.Code, r.Out())
	}
	if r := support.Jhon(t, b.proj, b.env, "sandbox", "run", "--", "sh", "-c", `echo x > "$1/joined"`, "sh", mem); r.Code != 0 || !exists(filepath.Join(mem, "joined")) {
		t.Fatalf("the member is not bound for the primary: exit %d %s", r.Code, r.Out())
	}
	t248v2Race(t, b, work, []string{filepath.Join(b.home, "mem")}, func(n int) support.Result {
		return support.Jhon(t, b.proj, b.env, "sandbox", "run", "--", "sh", "-c", `echo x > "$1" 2>/dev/null; :`, "sh", filepath.Join(mem, fmt.Sprintf("planted-%d", n)))
	})
}

// Claude Code's hook resolved the pinned project (JHON_SANDBOX_PROJECT)
// through every link when it wrapped a call (sandbox.ProjectFolder, also at
// 889c070), so a swap of a folder on the pinned path at that moment handed
// `jhon sandbox run --project` the home's folder, which the run bound
// writable (the anchor compared it with itself), and the session's
// ordinary call wrote it. pi's pin (status --json) is checked too.
func TestT248V2TheHookNeverPinsAHomeFolderThroughASwap(t *testing.T) {
	bash := realShell(t, "bash")
	b := newBox(t)
	work := filepath.Join(b.home, "work")
	victim := filepath.Join(work, "sub", "victim")
	mkdirs(t, victim, filepath.Join(b.home, "victim"))
	support.GitRepo(t, work)
	support.GitRepo(t, victim)
	support.Write(t, work, map[string]string{"flip.py": t248v2Flip})
	env := append(t248Env(b.env, "SHELL="+bash), "JHON_SANDBOX_PROJECT="+victim)
	done := make(chan support.Result, 1)
	go func() {
		done <- support.Jhon(t, work, b.env, "sandbox", "run", "--", "python3", filepath.Join(work, "flip.py"), work, t248v2Seconds())
	}()
	time.Sleep(time.Second)
	calls, home, pins := 0, 0, 0
	var line string
loop:
	for {
		select {
		case <-done:
			break loop
		default:
		}
		calls++
		payload := `{"hook_event_name":"PreToolUse","session_id":"s","cwd":` + quoted(t, victim) + `,"tool_name":"Bash","tool_input":{"command":"echo x > planted"},"agent_id":"a1","agent_type":"worker","permission_mode":"bypassPermissions"}`
		r := support.Run(t, support.Cmd{Args: []string{support.Bin(t), "hook", "pre-tool", "--agent", "worker", "safety-guard"}, Env: env, Stdin: payload})
		var a answer
		if json.Unmarshal([]byte(r.Stdout), &a) != nil {
			continue
		}
		var cmd string
		_ = json.Unmarshal(a.Out.Updated["command"], &cmd)
		if strings.Contains(cmd, "--project '"+filepath.Join(b.home, "victim")+"'") {
			home++
			line = cmd
			// The call as Claude Code then runs it, in the session's folder.
			support.Run(t, support.Cmd{Args: []string{bash, "-c", cmd}, Dir: victim, Env: env})
		}
		// pi pins its session's project from status --json, once.
		if r := support.Jhon(t, victim, b.env, "sandbox", "status", "--json"); strings.Contains(r.Stdout, `"project":"`+filepath.Join(b.home, "victim")+`"`) {
			pins++
		}
	}
	if home > 0 {
		t.Errorf("%d of %d calls were wrapped with the home folder as the project: %s", home, calls, line)
	}
	if pins > 0 {
		t.Errorf("%d of %d status --json answers (pi's pin) named the home folder as the project", pins, calls)
	}
	if exists(filepath.Join(b.home, "victim", "planted")) {
		t.Errorf("the wrapped call 'echo x > planted' wrote the home folder %s", filepath.Join(b.home, "victim"))
	}
	t.Logf("%d calls: %d wrapped with the home folder, %d status pins of it", calls, home, pins)
}

// The program, supervised and on a terminal, holds no descriptor of jhon's
// program or of a host folder, and neither does bubblewrap's pid 1, the only
// other process it sees.
func TestT248V2NoDescriptorOfEnterReachesTheProgram(t *testing.T) {
	b := newBox(t)
	bin, _ := filepath.EvalSymlinks(support.Bin(t))
	prog := `ls -l /proc/[0-9]*/fd/ 2>/dev/null; ls /proc | grep -c "^[0-9]"; :`
	check := func(t *testing.T, out string) {
		t.Helper()
		for _, m := range fdLine.FindAllStringSubmatch(out, -1) {
			n, target := m[1], strings.TrimRight(m[2], "\r")
			if fi, err := os.Stat(target); err == nil && fi.IsDir() && strings.HasPrefix(target, "/") || strings.Contains(target, bin) || strings.HasSuffix(target, "/jhon") {
				t.Errorf("descriptor %s leads to %s", n, target)
			}
		}
		if !strings.Contains(out, " 2 -> ") {
			t.Errorf("no descriptor listing:\n%s", out)
		}
	}
	t.Run("supervised", func(t *testing.T) { check(t, b.sh(t, b.proj, "", prog).Stdout) })
	t.Run("terminal", func(t *testing.T) {
		if _, err := exec.LookPath("script"); err != nil {
			t.Skip("skipped: no script(1)")
		}
		r := support.Run(t, support.Cmd{Args: []string{"script", "-qec", support.Bin(t) + ` sandbox run -- sh -c '` + prog + `'`, "/dev/null"}, Dir: b.proj, Env: b.env})
		check(t, r.Stdout)
	})
}

// The program starts as bubblewrap would start it (as at 738da19, supervised:
// the caller's ignored SIGINT and SIGHUP were already reset there): its
// argv, a caller's signal mask, file limit and umask, a program
// with no #! line, one PATH does not hold, one that may not run, a
// directory, and its own exit codes.
func TestT248V2TheProgramStartsAsBubblewrapStartsIt(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("skipped: no python3 for the caller's signal state")
	}
	b := newBox(t)
	support.Write(t, b.proj, map[string]string{"noshebang": "echo noshebang-ran \"$0\" \"$@\"\n", "notexec": "x", "adir/x": ""})
	if err := os.Chmod(filepath.Join(b.proj, "noshebang"), 0o755); err != nil {
		t.Fatal(err)
	}
	wrap := `import os, resource, signal, sys
for s in (signal.SIGINT, signal.SIGHUP): signal.signal(s, signal.SIG_IGN)
signal.pthread_sigmask(signal.SIG_BLOCK, {signal.SIGUSR1})
resource.setrlimit(resource.RLIMIT_NOFILE, (1000, resource.getrlimit(resource.RLIMIT_NOFILE)[1]))
os.umask(0o027)
os.execv(sys.argv[1], sys.argv[1:])
`
	support.Write(t, b.base, map[string]string{"wrap.py": wrap})
	run := func(args ...string) support.Result {
		return support.Run(t, support.Cmd{Args: append([]string{"python3", "-I", filepath.Join(b.base, "wrap.py"), support.Bin(t), "sandbox", "run", "--"}, args...), Dir: b.proj, Env: b.env})
	}
	r := run("sh", "-c", `echo "$0|$1|$2"; grep -E "^Sig(Blk|Ign)" /proc/self/status | tr -s "\t\n" "  "; echo; ulimit -Sn; umask`, "zero", "a b", "")
	if want := "zero|a b|\nSigBlk: 0000000000000200 SigIgn: 0000000000000000 \n1000\n0027\n"; r.Code != 0 || r.Stdout != want {
		t.Errorf("argv and signal state: exit %d, %q; want %q (stderr %q)", r.Code, r.Stdout, want, r.Stderr)
	}
	for _, c := range []struct {
		args         []string
		code         int
		stdout, note string
	}{
		{[]string{"./noshebang", "a1"}, 0, "noshebang-ran ./noshebang a1\n", ""},
		{[]string{"noshebang"}, 1, "", "bwrap: execvp noshebang: No such file or directory\n"},
		{[]string{"./notexec"}, 1, "", "bwrap: execvp ./notexec: Permission denied\n"},
		{[]string{"./adir"}, 1, "", "bwrap: execvp ./adir: Permission denied\n"},
		{[]string{"sh", "-c", "exit 126"}, 126, "", ""},
		{[]string{"sh", "-c", "exit 127"}, 127, "", ""},
		{[]string{"sh", "-c", "kill -TERM $$"}, 143, "", ""},
		{[]string{"--expect", "/x=3", "--", "true"}, 1, "", "bwrap: execvp --expect: No such file or directory\n"},
	} {
		if r := run(c.args...); r.Code != c.code || r.Stdout != c.stdout || !strings.HasSuffix(r.Stderr, c.note) {
			t.Errorf("%q: exit %d, stdout %q, stderr %q; want %d, %q, %q", c.args, r.Code, r.Stdout, r.Stderr, c.code, c.stdout, c.note)
		}
	}
}

// A PATH folder bound read-only inside $TMPDIR whose parent is swapped for
// a link while bubblewrap mounts it (the first verify's advisory b): the
// run is refused before the program, and the PATH folder is not written.
func TestT248V2AReadOnlyBindsSwappedDestinationRefusesTheRun(t *testing.T) {
	b := newBox(t)
	a := filepath.Join(b.tmp, "a")
	mkdirs(t, filepath.Join(a, "bin"), filepath.Join(b.tmp, "decoy", "bin"))
	env := t248StandIn(t, b, "mv '"+a+"' '"+a+".moved' && ln -s decoy '"+a+"' || exit 99")
	env = t248Env(env, "PATH="+filepath.Join(b.base, "t248-bin")+":"+filepath.Join(a, "bin")+":"+os.Getenv("PATH"))
	r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `echo x > "$1/planted" 2>/dev/null; echo ran`, "sh", filepath.Join(a+".moved", "bin"))
	if exists(filepath.Join(a+".moved", "bin", "planted")) {
		t.Errorf("the program wrote the PATH folder %s", filepath.Join(a, "bin"))
	}
	if want := "jhon sandbox: " + filepath.Join(a, "bin") + " is not the folder Jhon checked (it changed while bubblewrap mounted it), so nothing was run; fix: run the command again\n"; r.Code != 126 || r.Stdout != "" || r.Stderr != want {
		t.Errorf("exit %d, stdout %q, stderr %q; want 126 and %q", r.Code, r.Stdout, r.Stderr, want)
	}
}

// A hidden path in a writable area (the SSH agent's socket in $TMPDIR) whose
// parent is swapped for a link while bubblewrap mounts the null device on
// it: the hiding mount lands on the link's target (pre-existing at 889c070),
// and jhon sandbox enter, which checks the hidden paths too, runs nothing.
func TestT248V2AHiddenPathsSwappedParentKeepsItHidden(t *testing.T) {
	b := newBox(t)
	ssh := filepath.Join(b.tmp, "ssh-X")
	support.Write(t, b.tmp, map[string]string{"ssh-X/agent.1": "AGENT-SECRET", "decoy/agent.1": ""})
	env := t248Env(t248StandIn(t, b, "mv '"+ssh+"' '"+ssh+".moved' && ln -s decoy '"+ssh+"' || exit 99"), "SSH_AUTH_SOCK="+filepath.Join(ssh, "agent.1"))
	r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `cat "$1" "$2" 2>/dev/null; :`, "sh", filepath.Join(ssh, "agent.1"), filepath.Join(ssh+".moved", "agent.1"))
	if strings.Contains(r.Stdout, "AGENT-SECRET") {
		t.Errorf("the hidden SSH agent socket showed after its folder was swapped during the mounts: exit %d, stdout %q", r.Code, r.Stdout)
	}
}
