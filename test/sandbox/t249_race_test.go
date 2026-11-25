package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T249 L33-L34 ("Sin procesos vivos del sandbox"): no sandboxed command
// leaves a live process, and a line that runs outside the sandbox does not
// run while a sandboxed command that can write its project runs, nor the
// other way round, so nothing a sandboxed command does can change what an
// outside line reads while it reads it.

// outsideWhileRunning opens the note of an outside line refused while a
// sandboxed command of its project runs; insideWhileOutside the note of a
// sandboxed command refused while an outside line of its project runs.
const (
	outsideWhileRunning = "jhon sandbox: a sandboxed command that can write what this line reads is still running (pid "
	insideWhileOutside  = "jhon sandbox: a line that runs outside the sandbox and reads what this command can write is still running (pid "
)

// leftovers are the processes whose command line holds marker, other than
// this test's.
func leftovers(t *testing.T, marker string) []int {
	t.Helper()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Skipf("skipped: no /proc (%v)", err)
	}
	var out []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err == nil && strings.Contains(string(cmdline), marker) {
			out = append(out, pid)
		}
	}
	return out
}

// L34: a run that starts a setsid child and a double fork in the background
// and exits leaves no process behind.
func TestASandboxedRunLeavesNoLiveProcess(t *testing.T) {
	b := newBox(t)
	marker := "41.7" + strconv.Itoa(os.Getpid()%1000)
	t.Cleanup(func() {
		for _, pid := range leftovers(t, marker) {
			syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	r := b.sh(t, b.proj, "", `setsid sh -c 'sleep `+marker+`' </dev/null >/dev/null 2>&1 & (sh -c 'sleep `+marker+`' </dev/null >/dev/null 2>&1 &) ; echo started`)
	if r.Code != 0 || r.Stdout != "started\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(leftovers(t, marker)) > 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if left := leftovers(t, marker); len(left) > 0 {
		t.Errorf("the run ended and left %d process(es) running: %v", len(left), left)
	}
}

// The verifier's race (L33): a sandboxed command left running swaps
// .git/config between Jhon's listing of the repository's git keys and git's
// own read. With L34 the unsandboxed pulls are refused while it runs, the
// filter never runs outside, and once it has ended a plain pull works.
func TestAConfigSwappedDuringAnUnsandboxedPullRunsNoFilterOutside(t *testing.T) {
	b := newBox(t)
	originOverSSH(t, &b, filepath.Join(b.base, "ssh-ran"))
	support.Write(t, b.proj, map[string]string{".gitattributes": "*.txt filter=evil\n", "a.txt": "content\n"})
	support.Git(t, b.proj, "add", ".")
	support.Git(t, b.proj, "commit", "-q", "-m", "a")
	support.Git(t, b.proj, "push", "-q", filepath.Join(b.base, "remote.git"), "HEAD:refs/heads/main")
	back := func() { support.Git(t, b.proj, "-c", "filter.evil.smudge=cat", "reset", "-q", "--hard", "HEAD~1") }
	back()
	ran, evil := filepath.Join(b.base, "filter-ran"), filepath.Join(b.proj, ".git", "evil.sh")
	script(t, evil, "#!/bin/sh\necho filter >> "+ran+"\ncat\n")
	clean := support.Read(t, filepath.Join(b.proj, ".git", "config"))
	support.Write(t, b.proj, map[string]string{".git/cfg-clean": clean, ".git/cfg-evil": clean + "[filter \"evil\"]\n\tsmudge = " + evil + "\n"})
	racer := exec.Command(support.Bin(t), "sandbox", "run", "--", "sh", "-c",
		`end=$(( $(date +%s) + 60 )); while [ $(date +%s) -lt $end ]; do cp .git/cfg-evil .git/c1 && mv .git/c1 .git/config; cp .git/cfg-clean .git/c2 && mv .git/c2 .git/config; done`)
	racer.Dir, racer.Env = b.proj, b.env
	if err := racer.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	stop := func() {
		if !stopped {
			stopped = true
			racer.Process.Kill()
			racer.Wait()
		}
	}
	t.Cleanup(stop)
	time.Sleep(500 * time.Millisecond)
	for i := 1; i <= 6; i++ {
		r := b.unsandboxed(t, b.env, "git pull -q --ff-only")
		if exists(ran) {
			t.Fatalf("pull %d: the repository's filter ran outside the sandbox (exit %d, stderr %q)", i, r.Code, r.Stderr)
		}
		if r.Code != 126 || !strings.HasPrefix(r.Stderr, outsideWhileRunning) {
			t.Errorf("pull %d while the racer runs: exit %d, stderr %q; want 126 and the note", i, r.Code, r.Stderr)
		}
		if r.Code == 0 {
			back()
		}
	}
	// The racer is killed with its jhon: its registry entry is stale, its
	// processes gone with its PID namespace; the config it left is restored.
	stop()
	support.Write(t, b.proj, map[string]string{".git/config": clean})
	r := b.unsandboxed(t, b.env, "git pull -q --ff-only")
	if r.Code != 0 || r.Stderr != "" || exists(ran) || support.Read(t, filepath.Join(b.proj, "a.txt")) != "content\n" {
		t.Errorf("a pull after the racer ended: exit %d, stderr %q, filter ran %v; want it done", r.Code, r.Stderr, exists(ran))
	}
}

// startOutside starts line in the project with the unsandboxed profile, as
// the runtimes do, and waits for began, which the line's program touches
// once it runs outside the sandbox.
func startOutside(t *testing.T, b box, line, began string) {
	t.Helper()
	cmd := exec.Command(support.Bin(t), "sandbox", "run", "--project", b.proj, "--profile", "unsandboxed", "--", realShell(t, "sh"), "-c", line)
	cmd.Dir, cmd.Env = b.proj, b.env
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	for i := 0; i < 100 && !exists(began); i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if !exists(began) {
		t.Fatalf("%q did not start outside the sandbox", line)
	}
}

// The other way round, for git only (L35, "Solo durante git de afuera"): a
// sandboxed command of the project that starts while a git line of it runs
// outside the sandbox (here a fetch whose ssh is slow) waits, then is
// refused with the note; one of another project runs.
func TestASandboxedCommandWaitsForAnOutsideGitLineOfItsProject(t *testing.T) {
	b := newBox(t)
	bin, began := filepath.Join(b.base, "slow-bin"), filepath.Join(b.base, "ssh-began")
	script(t, filepath.Join(bin, "ssh"), "#!/bin/sh\ntouch "+began+"\nsleep 8\nexit 1\n")
	prependPath(&b, bin)
	support.Git(t, b.proj, "remote", "add", "origin", "ssh://example.invalid/remote.git")
	startOutside(t, b, "git fetch origin", began)
	r := b.sh(t, b.proj, "", "echo ran")
	if r.Code != 126 || r.Stdout != "" || !strings.HasPrefix(r.Stderr, insideWhileOutside) {
		t.Errorf("a sandboxed command during the outside fetch: exit %d, stdout %q, stderr %q; want 126 and the note", r.Code, r.Stdout, r.Stderr)
	}
	other := filepath.Join(b.home, "other")
	mkdirs(t, other)
	support.GitRepo(t, other)
	if r := b.sh(t, other, "", "echo ran"); r.Code != 0 || r.Stdout != "ran\n" {
		t.Errorf("a sandboxed command of another project during the fetch: exit %d, stdout %q, stderr %q; want it run", r.Code, r.Stdout, r.Stderr)
	}
}

// L35: while an outside line that runs no git runs (a deploy tool, which
// runs the project's code by design), sandboxed commands of the project are
// not held.
func TestASandboxedCommandRunsDuringAnOutsideDeployLine(t *testing.T) {
	b := newBox(t)
	bin, began, done := filepath.Join(b.base, "deploy-bin"), filepath.Join(b.base, "deploy-began"), filepath.Join(b.base, "deploy-done")
	script(t, filepath.Join(bin, "mydeploy"), "#!/bin/sh\ntouch "+began+"\nsleep 8\ntouch "+done+"\n")
	prependPath(&b, bin)
	b.settings(t, "schema = 1\n\n[sandbox]\ndeploy_tools = [\"mydeploy\"]\n")
	startOutside(t, b, "mydeploy up", began)
	r := b.sh(t, b.proj, "", "echo ran")
	if r.Code != 0 || r.Stdout != "ran\n" || r.Stderr != "" {
		t.Errorf("a sandboxed command during the deploy: exit %d, stdout %q, stderr %q; want it run", r.Code, r.Stdout, r.Stderr)
	}
	if exists(done) {
		t.Error("the deploy had ended before the sandboxed command ran")
	}
}

// L34: a line given the unsandboxed profile that ends up in the work profile
// (here a git -C folder whose configuration cannot be listed) runs with the
// PATH it had, project folders included, as before T249: only a line that
// runs outside gets the trimmed PATH.
func TestTheWorkFallbackKeepsItsPATH(t *testing.T) {
	b := newBox(t)
	fake, projBin := filepath.Join(b.base, "fake-git"), filepath.Join(b.proj, "bin")
	mkdirs(t, projBin)
	script(t, filepath.Join(fake, "git"), "#!/bin/sh\nfor a; do [ \"$a\" = config ] && exit 1; done\necho \"PATH=$PATH\"\n")
	prependPath(&b, fake, projBin)
	r := b.unsandboxed(t, b.env, "git -C missing fetch origin")
	if r.Code != 0 || !strings.Contains(r.Stdout, ":"+projBin+":") {
		t.Errorf("the work fallback: exit %d, stdout %q, stderr %q; want its PATH with %s", r.Code, r.Stdout, r.Stderr, projBin)
	}
}
