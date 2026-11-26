package sandbox

// T248's third verify (odd/tasks/phase1-security.md S3, L11, after
// 4e17203), kept as regressions: the note on bubblewrap's own race refusal
// is decided by whether the sandbox started, never by the program's output;
// the unsandboxed path and the hook's wrapped call hold under a swap. Two
// hidden-path cases belong to T250.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// A hidden path outside every writable area of the run, the SSH agent's
// socket at work/sub/ssh-X/agent.1, while a run of the enclosing project
// swaps work/sub with a link to the home (where ssh-X/agent.1 is an empty
// decoy): no run reads what the socket's file holds.
func TestT248V3RaceAHiddenPath(t *testing.T) {
	t.Skip("T250: a socket hidden by its path in a folder another sandboxed run can write or rename (pre-existing at 889c070, as the renamable /tmp socket folders)")
	b := newBox(t)
	work := filepath.Join(b.home, "work")
	mkdirs(t, work)
	support.GitRepo(t, work)
	support.Write(t, work, map[string]string{"sub/ssh-X/agent.1": "AGENT-SECRET", "flip.py": t248v2Flip})
	support.Write(t, b.home, map[string]string{"ssh-X/agent.1": ""})
	env := t248Env(t248StandIn(t, b, ""), "SSH_AUTH_SOCK="+filepath.Join(work, "sub", "ssh-X", "agent.1"))
	hidden := map[string]int{}
	done := make(chan support.Result, 1)
	go func() {
		done <- support.Jhon(t, work, b.env, "sandbox", "run", "--", "python3", filepath.Join(work, "flip.py"), work, t248v2Seconds())
	}()
	time.Sleep(time.Second)
	runs, read, started := 0, 0, 0
loop:
	for {
		select {
		case <-done:
			break loop
		default:
		}
		runs++
		r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `cat "$1" "$2" 2>/dev/null; echo ran`, "sh",
			filepath.Join(work, "sub", "ssh-X", "agent.1"), filepath.Join(work, "sublink", "ssh-X", "agent.1"))
		if strings.Contains(r.Stdout, "ran") {
			started++
		}
		if strings.Contains(r.Stdout, "AGENT-SECRET") {
			read++
			args, _ := os.ReadFile(filepath.Join(b.base, "t248-args.log"))
			if i := strings.Index(string(args), "/dev/null\n"); i >= 0 {
				line, _, _ := strings.Cut(string(args)[i+len("/dev/null\n"):], "\n")
				hidden[strings.ReplaceAll(line, b.base, "B")]++
			}
		}
	}
	if read > 0 {
		t.Errorf("%d of %d runs read the hidden socket's file", read, runs)
	}
	if started == 0 {
		t.Errorf("none of %d runs started", runs)
	}
	t.Logf("%d runs, %d started, %d read the secret; the null device was bound on: %v", runs, started, read, hidden)
}

// A program that ran, wrote, and printed bubblewrap's race line on its
// stderr before exiting 1 got jhon's note that nothing was run at 4e17203,
// inviting a repeat: the note is decided by whether enter started the
// program, which no program's output can fake.
func TestT248V3AProgramsOwnRaceLineGetsNoNothingWasRunNote(t *testing.T) {
	b := newBox(t)
	marker := filepath.Join(b.proj, "ran")
	r := b.sh(t, b.proj, "", fmt.Sprintf(`echo x > %q; echo 'bwrap: Race condition binding dirfd: File exists' >&2; exit 1`, marker))
	if !exists(marker) {
		t.Fatalf("the program did not run: exit %d, %q", r.Code, r.Stderr)
	}
	if strings.Contains(r.Stderr, "nothing was run") {
		t.Errorf("the program ran (it wrote %s), yet jhon says: %q", marker, r.Stderr)
	}
	if !strings.HasPrefix(r.Stderr, "bwrap: Race condition binding dirfd: File exists\n") || r.Code != 1 {
		t.Errorf("exit %d, stderr %q; want the program's own line and exit 1", r.Code, r.Stderr)
	}
}

// The unsandboxed path: a git line whose repository, work/sub/victim, sets
// a key that runs a command (core.gitProxy), while runs of the enclosing
// project swap work/sub with a link to work/clean, where the same repository
// has a clean configuration: the command never runs outside the sandbox.
func TestT248V3RaceTheUnsandboxedPath(t *testing.T) {
	sh := realShell(t, "sh")
	b := newBox(t)
	work := filepath.Join(b.home, "work")
	mkdirs(t, work)
	support.GitRepo(t, work)
	victim, clean := filepath.Join(work, "sub", "victim"), filepath.Join(work, "clean", "victim")
	mark := filepath.Join(b.base, "proxy-ran")
	script(t, filepath.Join(b.base, "proxy.sh"), "#!/bin/sh\necho hit >> '"+mark+"'\n")
	for _, d := range []string{victim, clean} {
		mkdirs(t, d)
		support.GitRepo(t, d)
		support.Git(t, d, "remote", "add", "origin", "git://example.invalid/x")
	}
	support.Git(t, victim, "config", "core.gitProxy", filepath.Join(b.base, "proxy.sh"))
	support.Write(t, work, map[string]string{"flip.py": strings.Replace(t248v2Flip, `os.symlink("..", w + "/sublink")`, `os.symlink("clean", w + "/sublink")`, 1)})
	stop := make(chan struct{})
	attacks := make(chan int, 1)
	go func() {
		n := 0
		for {
			select {
			case <-stop:
				attacks <- n
				return
			default:
			}
			// Short runs, so the registry lets unsandboxed lines in between.
			support.Jhon(t, work, b.env, "sandbox", "run", "--", "python3", filepath.Join(work, "flip.py"), work, "0.15")
			n++
		}
	}()
	end := time.Now().Add(parseSeconds(t248v2Seconds()))
	runs := 0
	for time.Now().Before(end) {
		runs++
		support.Jhon(t, victim, b.env, "sandbox", "run", "--project", victim, "--profile", "unsandboxed", "--", sh, "-c", "git fetch")
	}
	close(stop)
	n := <-attacks
	if exists(mark) {
		hits, _ := os.ReadFile(mark)
		t.Errorf("core.gitProxy ran outside the sandbox %d times in %d lines", strings.Count(string(hits), "hit"), runs)
	}
	t.Logf("%d unsandboxed lines, %d swapping runs", runs, n)
}

func parseSeconds(s string) time.Duration {
	d, err := time.ParseDuration(s + "s")
	if err != nil {
		return 25 * time.Second
	}
	return d
}

// The hook's wrapped call itself, run as Claude Code runs it while a run of
// the enclosing project swaps work/sub with a link to the home: the call
// (pinned as JHON_SANDBOX_PROJECT is written) never writes the home folder.
func TestT248V3RaceTheHooksWrappedCall(t *testing.T) {
	bash := realShell(t, "bash")
	b := newBox(t)
	work := filepath.Join(b.home, "work")
	victim := filepath.Join(work, "sub", "victim")
	mkdirs(t, victim, filepath.Join(b.home, "victim"))
	support.GitRepo(t, work)
	support.GitRepo(t, victim)
	support.Write(t, work, map[string]string{"flip.py": t248v2Flip})
	env := append(t248Env(b.env, "SHELL="+bash), "JHON_SANDBOX_PROJECT="+victim)
	payload := `{"hook_event_name":"PreToolUse","session_id":"s","cwd":` + quoted(t, victim) + `,"tool_name":"Bash","tool_input":{"command":"echo x > planted-$$; echo x > ` + victim + `/abs-$$"},"agent_id":"a1","agent_type":"worker","permission_mode":"bypassPermissions"}`
	r := support.Run(t, support.Cmd{Args: []string{support.Bin(t), "hook", "pre-tool", "--agent", "worker", "safety-guard"}, Env: env, Stdin: payload})
	var a answer
	if err := json.Unmarshal([]byte(r.Stdout), &a); err != nil {
		t.Fatalf("hook: %q %v", r.Stdout, err)
	}
	cmd := a.command(t)
	done := make(chan support.Result, 1)
	go func() {
		done <- support.Jhon(t, work, b.env, "sandbox", "run", "--", "python3", filepath.Join(work, "flip.py"), work, t248v2Seconds())
	}()
	time.Sleep(time.Second)
	calls, ok := 0, 0
loop:
	for {
		select {
		case <-done:
			break loop
		default:
		}
		calls++
		if support.Run(t, support.Cmd{Args: []string{bash, "-c", cmd}, Dir: victim, Env: env}).Code == 0 {
			ok++
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(b.home, "victim")); len(entries) > 0 {
		t.Errorf("%d of %d wrapped calls wrote the home folder %s", len(entries), calls, filepath.Join(b.home, "victim"))
	}
	if ok == 0 {
		t.Errorf("none of %d wrapped calls succeeded", calls)
	}
	t.Logf("%d wrapped calls, %d exit 0", calls, ok)
}

// A hidden path in a writable area, the SSH agent's socket in $TMPDIR (on a
// real machine /tmp/ssh-*/agent.*, and /tmp is writable in every sandbox),
// is hidden by its path: one run renames its folder, and the next, which
// finds nothing at the path to hide, reads it under the new name. Since
// T250 (b) the folder is the hide's mount point, which no run renames.
func TestT248V3AHiddenPathMovedByAnEarlierRunStaysHidden(t *testing.T) {
	b := newBox(t)
	support.Write(t, b.tmp, map[string]string{"ssh-X/agent.1": "AGENT-SECRET"})
	env := t248Env(b.env, "SSH_AUTH_SOCK="+filepath.Join(b.tmp, "ssh-X", "agent.1"))
	support.Jhon(t, b.proj, env, "sandbox", "run", "--", "mv", filepath.Join(b.tmp, "ssh-X"), filepath.Join(b.tmp, "ssh-Y"))
	r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "cat", filepath.Join(b.tmp, "ssh-Y", "agent.1"))
	if strings.Contains(r.Stdout, "AGENT-SECRET") {
		t.Errorf("the hidden socket's file was read after an earlier run moved its folder: %q", r.Stdout)
	}
}
