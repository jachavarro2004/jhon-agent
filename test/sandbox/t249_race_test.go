package sandbox

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
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

// L37 ("Esperar durante toda linea de afuera", superseding L35): a
// sandboxed command of the project that starts while a deploy line of it
// runs outside the sandbox waits, then is refused with the note, as for git;
// one of another project runs.
func TestASandboxedCommandWaitsForAnOutsideDeployLine(t *testing.T) {
	b := newBox(t)
	bin, began := filepath.Join(b.base, "deploy-bin"), filepath.Join(b.base, "deploy-began")
	script(t, filepath.Join(bin, "mydeploy"), "#!/bin/sh\ntouch "+began+"\nsleep 8\n")
	prependPath(&b, bin)
	b.settings(t, "schema = 1\n\n[sandbox]\ndeploy_tools = [\"mydeploy\"]\n")
	startOutside(t, b, "mydeploy up", began)
	r := b.sh(t, b.proj, "", "echo ran")
	if r.Code != 126 || r.Stdout != "" || !strings.HasPrefix(r.Stderr, insideWhileOutside) {
		t.Errorf("a sandboxed command during the deploy: exit %d, stdout %q, stderr %q; want 126 and the note", r.Code, r.Stdout, r.Stderr)
	}
	other := filepath.Join(b.home, "other")
	mkdirs(t, other)
	support.GitRepo(t, other)
	if r := b.sh(t, other, "", "echo ran"); r.Code != 0 || r.Stdout != "ran\n" {
		t.Errorf("a sandboxed command of another project during the deploy: exit %d, stdout %q, stderr %q; want it run", r.Code, r.Stdout, r.Stderr)
	}
}

// L36 B1 (the verifier's test): a line that backgrounds git outside the
// sandbox would outlive its registry record, and a filter planted after it
// started would run outside. Such a line runs in the work profile.
func TestABackgroundedOutsideGitLineRunsNoPlantedFilter(t *testing.T) {
	b := newBox(t)
	originOverSSH(t, &b, filepath.Join(b.base, "ssh-ran"))
	support.Write(t, b.proj, map[string]string{".gitattributes": "*.txt filter=evil\n", "a.txt": "content\n"})
	support.Git(t, b.proj, "add", ".")
	support.Git(t, b.proj, "commit", "-q", "-m", "a")
	support.Git(t, b.proj, "push", "-q", filepath.Join(b.base, "remote.git"), "HEAD:refs/heads/main")
	support.Git(t, b.proj, "-c", "filter.evil.smudge=cat", "reset", "-q", "--hard", "HEAD~1")
	ran, evil := filepath.Join(b.base, "filter-ran"), filepath.Join(b.proj, ".git", "evil.sh")
	script(t, evil, "#!/bin/sh\necho filter >> "+ran+"\ncat\n")
	clean := support.Read(t, filepath.Join(b.proj, ".git", "config"))
	support.Write(t, b.proj, map[string]string{".git/cfg-evil": clean + "[filter \"evil\"]\n\tsmudge = " + evil + "\n"})
	slow := filepath.Join(b.base, "slow")
	script(t, filepath.Join(slow, "ssh"), "#!/bin/sh\nsleep 2\nexec "+filepath.Join(b.base, "bin", "ssh")+" \"$@\"\n")
	prependPath(&b, slow)
	b.unsandboxed(t, b.env, "git pull -q --ff-only >/dev/null 2>&1 &")
	b.sh(t, b.proj, "", "cp .git/cfg-evil .git/c1 && mv .git/c1 .git/config")
	time.Sleep(5 * time.Second)
	if exists(ran) {
		t.Error("a filter planted after a backgrounded outside git line started ran outside the sandbox")
	}
}

// L36 B1: every line that backgrounds or detaches a program runs in the work
// profile, whatever the program; the same lines in the foreground keep
// running outside.
func TestALineThatBackgroundsRunsInTheSandbox(t *testing.T) {
	proj, env := deployHome(t, "mydeploy")
	profileIs(t, proj, env, map[string]string{
		"git fetch origin &":                "work",
		"(git fetch origin &)":              "work",
		"coproc git fetch origin":           "work",
		"nohup git fetch origin &":          "work",
		"nohup git fetch origin":            "work",
		"setsid git fetch origin":           "work",
		"git fetch origin & disown":         "work",
		"mydeploy up &":                     "work",
		"git fetch origin && mydeploy up &": "work",
		"git fetch origin":                  "unsandboxed",
		"(git fetch origin)":                "unsandboxed",
		"mydeploy up":                       "unsandboxed",
		"git fetch origin && mydeploy up":   "unsandboxed",
	})
}

// L36 advisory A: a sandboxed command cannot hold the registry's lock (the
// registry is hidden inside the sandbox), and a run never waits on the lock
// past the registry's wait: a run of another project starts at once, and
// none runs unchecked.
func TestASandboxedCommandCannotStallOtherRunsOnTheRegistryLock(t *testing.T) {
	b := newBox(t)
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("skipped: no flock")
	}
	lock := filepath.Join(b.home, ".local", "state", "jhon", "sandbox-runs", ".lock")
	holder := exec.Command(support.Bin(t), "sandbox", "run", "--", "sh", "-c", `flock -n "$1" -c 'echo locked; sleep 20' || echo cannot`, "sh", lock)
	holder.Dir, holder.Env = b.proj, b.env
	out, err := holder.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { holder.Process.Kill(); holder.Wait() })
	first := make([]byte, 6)
	n, _ := out.Read(first)
	if strings.HasPrefix(string(first[:n]), "locked") {
		t.Errorf("a sandboxed command took the registry's lock (%s)", lock)
	}
	other := filepath.Join(b.home, "other")
	mkdirs(t, other)
	support.GitRepo(t, other)
	start := time.Now()
	r := b.sh(t, other, "", "echo ran")
	if took := time.Since(start); took > 6*time.Second {
		t.Errorf("a run of another project waited %s on the lock", took)
	}
	if !(r.Code == 0 && r.Stdout == "ran\n") && !(r.Code == 126 && r.Stdout == "" && strings.Contains(r.Stderr, "jhon sandbox: the record of running commands")) {
		t.Errorf("a run of another project: exit %d, stdout %q, stderr %q; want it run, or refused with the note", r.Code, r.Stdout, r.Stderr)
	}
}

// When the lock is held past the wait (here by the test itself, outside
// the sandbox), the run is refused with the note within the wait, never run
// unchecked.
func TestARunRefusesWhenTheRegistryLockStaysHeld(t *testing.T) {
	b := newBox(t)
	dir := filepath.Join(b.home, ".local", "state", "jhon", "sandbox-runs")
	mkdirs(t, dir)
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	r := b.sh(t, b.proj, "", "echo ran")
	took := time.Since(start)
	if r.Code != 126 || r.Stdout != "" || !strings.HasPrefix(r.Stderr, "jhon sandbox: the record of running commands") || !strings.Contains(r.Stderr, "is locked") {
		t.Errorf("a run while the lock is held: exit %d, stdout %q, stderr %q; want 126 and the note", r.Code, r.Stdout, r.Stderr)
	}
	if took > 6*time.Second {
		t.Errorf("the run waited %s on the lock", took)
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

// serveUnix serves a unix socket at path for the test and counts the
// connections it accepts.
func serveUnix(t *testing.T, path string) *int32 {
	t.Helper()
	mkdirs(t, filepath.Dir(path))
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("skipped: cannot listen on %s: %v", path, err)
	}
	t.Cleanup(func() { l.Close() })
	var n int32
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			atomic.AddInt32(&n, 1)
			c.Close()
		}
	}()
	return &n
}

// dockerNote is what status and doctor say of the docker socket shown
// inside the sandbox (L37).
const dockerNote = "docker socket inside the sandbox: a container can write anywhere the user can"

// L37 ("Ocultarlo, opt-in por setting"): the docker socket is hidden inside
// the sandbox by default; sandbox.docker = true shows it again, and status
// (text and --json) and doctor name it as a known escape the owner
// accepted.
func TestTheDockerSocketIsHiddenUnlessTheOwnerAcceptsIt(t *testing.T) {
	b := newBox(t)
	run := filepath.Join(b.base, "run")
	mkdirs(t, run)
	if err := os.Chmod(run, 0o700); err != nil {
		t.Fatal(err)
	}
	conns := serveUnix(t, filepath.Join(run, "docker.sock"))
	b.env = append(b.env, "XDG_RUNTIME_DIR="+run)
	probe := `test -S "$XDG_RUNTIME_DIR/docker.sock" && echo shown || echo hidden`
	connect := ""
	if _, err := exec.LookPath("curl"); err == nil {
		connect = `; curl -s --max-time 2 --unix-socket "$XDG_RUNTIME_DIR/docker.sock" http://d/version >/dev/null 2>&1; true`
	}
	if r := b.sh(t, b.proj, "", probe+connect); r.Code != 0 || r.Stdout != "hidden\n" {
		t.Errorf("by default: exit %d, stdout %q, stderr %q; want the socket hidden", r.Code, r.Stdout, r.Stderr)
	}
	if connect != "" && atomic.LoadInt32(conns) != 0 {
		t.Errorf("by default a sandboxed command connected to the docker socket %d time(s)", atomic.LoadInt32(conns))
	}
	if r := support.Jhon(t, b.proj, b.env, "sandbox", "status", "--json"); strings.Contains(r.Stdout, dockerNote) {
		t.Errorf("by default status names the docker socket: %s", r.Stdout)
	}
	b.settings(t, "schema = 1\n\n[sandbox]\ndocker = true\n")
	if r := b.sh(t, b.proj, "", probe+connect); r.Code != 0 || r.Stdout != "shown\n" {
		t.Errorf("with sandbox.docker = true: exit %d, stdout %q, stderr %q; want the socket shown", r.Code, r.Stdout, r.Stderr)
	}
	if connect != "" && atomic.LoadInt32(conns) == 0 {
		t.Error("with sandbox.docker = true a sandboxed command could not connect to the docker socket")
	}
	var state struct {
		Escapes []string `json:"accepted_escapes"`
	}
	r := support.Jhon(t, b.proj, b.env, "sandbox", "status", "--json")
	if err := json.Unmarshal([]byte(r.Stdout), &state); err != nil || len(state.Escapes) != 1 || !strings.HasPrefix(state.Escapes[0], dockerNote) {
		t.Errorf("status --json with sandbox.docker = true: %s (%v); want accepted_escapes with the note", r.Stdout, err)
	}
	if r := support.Jhon(t, b.proj, b.env, "sandbox", "status"); !strings.Contains(r.Stdout, dockerNote) {
		t.Errorf("status with sandbox.docker = true: %q; want the note", r.Stdout)
	}
	if r := support.Jhon(t, b.proj, b.env, "doctor"); !strings.Contains(r.Out(), dockerNote) {
		t.Errorf("doctor with sandbox.docker = true does not name the docker socket:\n%s", r.Out())
	}
}

// L36: herdr's sockets (its API runs commands in its panes) are hidden
// inside the sandbox, as tmux's are; so are the system bus and sshd's local
// socket, and the pi harness's socket folder under /tmp, where they exist.
func TestSocketsThatRunCommandsAreHidden(t *testing.T) {
	b := newBox(t)
	herdr := filepath.Join(b.home, ".config", "herdr")
	conns := serveUnix(t, filepath.Join(herdr, "herdr.sock"))
	serveUnix(t, filepath.Join(herdr, "sessions", "Work", "herdr.sock"))
	probe := `for s in "$HOME/.config/herdr/herdr.sock" "$HOME/.config/herdr/sessions/Work/herdr.sock" /run/dbus/system_bus_socket /run/ssh-unix-local/socket; do test -S "$s" && echo "shown $s"; done; ls -A /tmp/gentle-pi-$(id -u) 2>/dev/null; echo end`
	r := b.sh(t, b.proj, "", probe)
	if r.Code != 0 || r.Stdout != "end\n" {
		t.Errorf("inside the sandbox: exit %d, stdout %q, stderr %q; want no socket shown", r.Code, r.Stdout, r.Stderr)
	}
	if atomic.LoadInt32(conns) != 0 {
		t.Error("a sandboxed command connected to herdr's socket")
	}
}
