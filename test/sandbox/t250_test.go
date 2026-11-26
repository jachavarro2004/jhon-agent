package sandbox

// T250 (odd/tasks/v0-1-release.md T250; studio-integration-v1 L30-L31,
// L43): the advisories T249 left. Each test is the attack: what a sandboxed
// command could have planted, and whether it runs outside the sandbox, or
// whether the sandbox still lets a command change Jhon's own files. Every
// planted program writes a note outside every folder a sandboxed command
// may write, so the note exists only when it ran outside the sandbox.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// L43b: a folder whose `.git` git ignores (a repository format it does not
// know) lists no repository scope, and rev-parse fails there: the line
// does not run outside with no walk of its git folders, as Jhon's own
// reader finds the `.git` entry.
func TestAFailingRevParseWhereAGitEntryIsKeepsTheLineInside(t *testing.T) {
	b := newBox(t)
	note := filepath.Join(b.base, "gh-ran")
	script(t, filepath.Join(b.base, "gh-bin", "gh"), "#!/bin/sh\necho ran >> "+note+"\n")
	prependPath(&b, filepath.Join(b.base, "gh-bin"))
	odd := filepath.Join(b.base, "odd")
	mkdirs(t, filepath.Join(odd, ".git", "objects"), filepath.Join(odd, ".git", "refs"))
	support.Write(t, odd, map[string]string{".git/HEAD": "ref: refs/heads/main\n", ".git/config": "[core]\n\trepositoryformatversion = 99\n"})
	r := support.Jhon(t, odd, b.env, "sandbox", "run", "--project", b.proj, "--profile", "unsandboxed", "--", realShell(t, "sh"), "-c", "gh pr list")
	if exists(note) {
		t.Errorf("gh ran outside the sandbox in a folder whose git folders were never walked (exit %d, stderr %q)", r.Code, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "jhon sandbox: git cannot name the git folders in "+odd+" (") || !strings.Contains(r.Stderr, "; the line runs in the sandbox instead\n") {
		t.Errorf("stderr %q; want the line kept inside because git cannot name the git folders in %s", r.Stderr, odd)
	}
	// Preserve: gh in a folder in no repository still runs outside.
	plain := filepath.Join(b.base, "plain")
	mkdirs(t, plain)
	r = support.Jhon(t, plain, b.env, "sandbox", "run", "--project", b.proj, "--profile", "unsandboxed", "--", realShell(t, "sh"), "-c", "gh pr list")
	if r.Code != 0 || !exists(note) || strings.Contains(r.Stderr, "runs in the sandbox instead") {
		t.Errorf("gh in a folder in no repository: exit %d, stderr %q, ran outside %v; want it run outside", r.Code, r.Stderr, exists(note))
	}
}

// L43c: an include in the owner's global configuration that names a file
// a sandboxed command may write (here in the cache) would let that command
// set core.gitProxy, which git runs outside the sandbox: such a line runs
// in the sandbox, with the file named. The same include from a folder no
// sandboxed command may write keeps the line outside.
func TestAGlobalIncludeASandboxedCommandCanWriteKeepsTheLineInside(t *testing.T) {
	b := newBox(t)
	ran := filepath.Join(b.base, "proxy-ran")
	proxy := filepath.Join(b.base, "proxy.sh")
	script(t, proxy, "#!/bin/sh\necho proxy >> "+ran+"\n")
	inc := filepath.Join(b.home, ".cache", "git", "extra.inc")
	support.Write(t, b.home, map[string]string{
		".gitconfig":           "[include]\n\tpath = " + inc + "\n",
		".cache/git/extra.inc": "[core]\n\tgitProxy = " + proxy + "\n",
	})
	r := b.unsandboxed(t, b.env, "git ls-remote git://127.0.0.1:9/x")
	if exists(ran) {
		t.Errorf("core.gitProxy from a global include in the cache ran outside the sandbox (exit %d, stderr %q)", r.Code, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "jhon sandbox: git's global configuration reads "+inc+", a file a sandboxed command may change") {
		t.Errorf("stderr %q; want the line kept inside for %s", r.Stderr, inc)
	}
	// Preserve: an include from ~/.config, which no sandboxed command may
	// write, keeps the line outside.
	safe := filepath.Join(b.home, ".config", "git", "extra.inc")
	support.Write(t, b.home, map[string]string{".gitconfig": "[include]\n\tpath = " + safe + "\n", ".config/git/extra.inc": "[user]\n\tname = owner\n"})
	if r := b.unsandboxed(t, b.env, "git ls-remote git://127.0.0.1:9/x"); strings.Contains(r.Stderr, "runs in the sandbox instead") {
		t.Errorf("an include from ~/.config kept the line inside: stderr %q", r.Stderr)
	}
}

// L43d: docker loads its CLI plugins from the configuration folder
// --config or DOCKER_CONFIG names (and the extra plugin folders its
// config.json lists): one a sandboxed command may write keeps the line in
// the sandbox; one it may not write keeps it outside.
func TestADockerConfigFolderASandboxedCommandCanWriteKeepsTheLineInside(t *testing.T) {
	b := newBox(t)
	note := filepath.Join(b.base, "docker-ran")
	fakeDocker(t, &b, note)
	script(t, filepath.Join(b.base, "docker-bin", "docker-compose"), "#!/bin/sh\necho compose >> "+note+"\n")
	planted := filepath.Join(b.proj, "dockerconf")
	mkdirs(t, filepath.Join(planted, "cli-plugins"))
	safe := filepath.Join(b.base, "dockerconf")
	mkdirs(t, filepath.Join(safe, "cli-plugins"))
	extra := filepath.Join(b.base, "extraconf")
	support.Write(t, extra, map[string]string{"config.json": `{"cliPluginsExtraDirs": ["` + filepath.Join(b.home, ".cache", "plugins") + `"]}`})
	cached := filepath.Join(b.home, ".cache", "docker")
	mkdirs(t, cached)
	for _, c := range []struct {
		name, line, env, folder string
	}{
		{"--config in the project", "docker --config " + planted + " ps", "", planted},
		{"--config= in the project", "docker --config=" + planted + " ps", "", planted},
		{"DOCKER_CONFIG in the cache", "docker ps", "DOCKER_CONFIG=" + cached, cached},
		{"docker-compose with DOCKER_CONFIG in the cache", "docker-compose up -d", "DOCKER_CONFIG=" + cached, cached},
		{"an extra plugin folder in the cache", "docker --config " + extra + " ps", "", filepath.Join(b.home, ".cache", "plugins")},
	} {
		t.Run(c.name, func(t *testing.T) {
			os.Remove(note)
			env := slices.Clone(b.env)
			if c.env != "" {
				env = append(env, c.env)
			}
			r := b.unsandboxed(t, env, c.line)
			if exists(note) {
				t.Errorf("%s ran outside the sandbox with plugins a sandboxed command may plant (exit %d, stderr %q)", c.line, r.Code, r.Stderr)
			}
			if !strings.Contains(r.Stderr, "jhon sandbox: docker loads CLI plugins from "+c.folder+", which a sandboxed command may change") {
				t.Errorf("stderr %q; want the line kept inside for %s", r.Stderr, c.folder)
			}
		})
	}
	// Preserve: a configuration folder no sandboxed command may write, and
	// the default ~/.docker, keep the line outside.
	for _, line := range []string{"docker --config " + safe + " ps", "docker ps"} {
		os.Remove(note)
		if r := b.unsandboxed(t, b.env, line); r.Code != 0 || !exists(note) {
			t.Errorf("%s: exit %d, stderr %q, ran outside %v; want it run outside", line, r.Code, r.Stderr, exists(note))
		}
	}
}

// L43e: a PATH folder inside another git work tree is written by any
// sandboxed run in that project: a deploy tool that calls helper by name,
// outside the sandbox, finds the owner's helper, never the one planted
// there.
func TestAProgramPlantedInAnotherProjectsPATHFolderIsNotWhatADeployToolRuns(t *testing.T) {
	b := newBox(t)
	other := filepath.Join(b.home, "other")
	mkdirs(t, other)
	support.GitRepo(t, other)
	deployBin, ownerBin, otherBin := filepath.Join(b.base, "deploy-bin"), filepath.Join(b.base, "owner-bin"), filepath.Join(other, "tools", "bin")
	planted, owners := filepath.Join(b.base, "planted-ran"), filepath.Join(b.base, "owner-ran")
	script(t, filepath.Join(deployBin, "mydeploy"), "#!/bin/sh\nhelper\n")
	script(t, filepath.Join(ownerBin, "helper"), "#!/bin/sh\necho owner >> "+owners+"\n")
	script(t, filepath.Join(otherBin, "helper"), "#!/bin/sh\necho planted >> "+planted+"\n")
	prependPath(&b, deployBin, otherBin, ownerBin)
	b.settings(t, "schema = 1\n\n[sandbox]\ndeploy_tools = [\"mydeploy\"]\n")
	r := b.unsandboxed(t, b.env, "mydeploy up")
	if exists(planted) {
		t.Errorf("the deploy tool ran the helper planted in another project's PATH folder outside the sandbox (exit %d, stderr %q)", r.Code, r.Stderr)
	}
	if r.Code != 0 || !exists(owners) {
		t.Errorf("exit %d, stderr %q, the owner's helper ran %v; want 0 and the owner's helper", r.Code, r.Stderr, exists(owners))
	}
}

// unsandboxedWithin is unsandboxed with a time limit: the process group is
// killed past it, and finished says whether it ended on its own.
func unsandboxedWithin(t *testing.T, b box, limit time.Duration, line string) (r support.Result, finished bool) {
	t.Helper()
	cmd := exec.Command(support.Bin(t), "sandbox", "run", "--project", b.proj, "--profile", "unsandboxed", "--", realShell(t, "sh"), "-c", line)
	cmd.Dir, cmd.Env = b.proj, b.env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	finished = true
	select {
	case <-done:
	case <-time.After(limit):
		finished = false
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
	}
	return support.Result{Code: cmd.ProcessState.ExitCode(), Stdout: out.String(), Stderr: errb.String()}, finished
}

// L43a: a pipe where a .gitattributes would be, in any folder of the work
// tree, makes git wait forever when a pull checks out a file there: the
// pull runs in the sandbox (where ssh cannot run), and nothing waits. A
// regular .gitattributes keeps the pull outside.
func TestAPipeForAGitattributesKeepsAPullInsideAndNothingWaits(t *testing.T) {
	b := newBox(t)
	ssh := filepath.Join(b.base, "ssh-ran")
	remote := originOverSSH(t, &b, ssh)
	// ssh refuses to run inside the sandbox (its system drop-ins, L1220).
	script(t, filepath.Join(b.base, "bin", "ssh"), "#!/bin/sh\n[ \"$JHON_IN_SANDBOX\" = 1 ] && { echo 'ssh: refused inside' >&2; exit 255; }\necho ran >> "+ssh+"\nfor last; do :; done\nexec sh -c \"$last\"\n")
	support.Write(t, b.proj, map[string]string{"sub/b.txt": "content\n"})
	support.Git(t, b.proj, "add", "sub/b.txt")
	support.Git(t, b.proj, "commit", "-q", "-m", "b")
	support.Git(t, b.proj, "push", "-q", remote, "HEAD:refs/heads/main")
	support.Git(t, b.proj, "reset", "-q", "--hard", "HEAD~1")
	mkdirs(t, filepath.Join(b.proj, "sub"))
	fifo := filepath.Join(b.proj, "sub", ".gitattributes")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	r, finished := unsandboxedWithin(t, b, 60*time.Second, "git pull -q --ff-only")
	if !finished {
		t.Fatalf("the pull still waited after 60 s on the pipe %s (stderr %q)", fifo, r.Stderr)
	}
	if exists(filepath.Join(b.proj, "sub", "b.txt")) || !strings.Contains(r.Stderr, "jhon sandbox: "+fifo+" is not a regular file, and git would wait on it when it checks out a file in its folder; the line runs in the sandbox instead\n") {
		t.Errorf("exit %d, stderr %q; want the pull kept inside for the pipe", r.Code, r.Stderr)
	}
	// Preserve: a regular .gitattributes keeps the pull outside.
	if err := os.Remove(fifo); err != nil {
		t.Fatal(err)
	}
	support.Write(t, b.proj, map[string]string{"sub/.gitattributes": "*.txt text\n"})
	r, finished = unsandboxedWithin(t, b, 60*time.Second, "git pull -q --ff-only")
	if !finished || r.Code != 0 || support.Read(t, filepath.Join(b.proj, "sub", "b.txt")) != "content\n" {
		t.Errorf("the pull with a regular .gitattributes: finished %v, exit %d, stderr %q; want it done outside", finished, r.Code, r.Stderr)
	}
}

// L43f: a line that runs outside the sandbox ends with the jhon that
// supervises it: SIGKILL leaves no program of the line running after its
// record in the registry is gone.
func TestAnOutsideLineEndsWhenItsJhonIsKilled(t *testing.T) {
	b := newBox(t)
	fakeDocker(t, &b, filepath.Join(b.base, "docker-ran"))
	pidFile := filepath.Join(b.base, "docker-pid")
	script(t, filepath.Join(b.base, "docker-bin", "docker"), "#!/bin/sh\necho $$ > "+pidFile+"\nexec sleep 60\n")
	cmd := exec.Command(support.Bin(t), "sandbox", "run", "--project", b.proj, "--profile", "unsandboxed", "--", realShell(t, "bash"), "-c", "docker ps")
	cmd.Dir, cmd.Env = b.proj, b.env
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := 0
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline) && pid == 0; time.Sleep(20 * time.Millisecond) {
		if data, err := os.ReadFile(pidFile); err == nil && strings.HasSuffix(string(data), "\n") {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
	}
	if pid == 0 {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("the docker line never started: stderr %q", errb.String())
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	_ = cmd.Process.Signal(syscall.SIGKILL)
	_ = cmd.Wait()
	alive := true
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline) && alive; time.Sleep(20 * time.Millisecond) {
		alive = syscall.Kill(pid, 0) == nil && !zombie(pid)
	}
	if alive {
		t.Errorf("the line's docker (pid %d) still runs after its jhon was killed", pid)
	}
}

// zombie is whether pid has ended and waits for its parent to reap it.
func zombie(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true
	}
	s := string(data)
	if i := strings.LastIndexByte(s, ')'); i >= 0 && i+2 < len(s) {
		return s[i+2] == 'Z'
	}
	return false
}

// A6: a cache variable that names a folder holding Jhon's release, where a
// sandboxed command could rename a folder around it and put its own in its
// place, is not bound writable; status and doctor say so. A cache variable
// that holds none stays writable.
func TestACacheVariableThatHoldsJhonsReleaseIsNotBoundWritable(t *testing.T) {
	b := newBox(t)
	local := filepath.Join(b.home, ".local")
	share := filepath.Join(local, "share", "jhon")
	mkdirs(t, filepath.Join(share, "current"))
	// The state folder elsewhere: under ~/.local it would refuse every run.
	env := append(slices.Clone(b.env), "GOMODCACHE="+local, "XDG_STATE_HOME="+filepath.Join(b.base, "state"))
	r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", "mv ~/.local/share ~/.local/moved && mkdir -p ~/.local/share/jhon && echo planted > ~/.local/share/jhon/current && echo replaced")
	if r.Stdout == "replaced\n" || exists(filepath.Join(local, "moved")) {
		t.Errorf("a sandboxed command replaced Jhon's release through $GOMODCACHE (exit %d, stderr %q)", r.Code, r.Stderr)
	}
	line := "$GOMODCACHE " + local + " is not used for writing: it holds Jhon's release folder (" + share + "), which a sandboxed command could replace by renaming a folder around it: point $GOMODCACHE at a folder that holds none of Jhon's own files"
	if s := support.Jhon(t, b.proj, env, "sandbox", "status"); !strings.Contains(s.Stdout, "; "+line) {
		t.Errorf("status does not say %q:\n%s", line, s.Out())
	}
	if d := support.Jhon(t, b.proj, env, "doctor"); !strings.Contains(d.Stdout, "  warn  "+line+"\n") {
		t.Errorf("doctor does not warn %q:\n%s", line, d.Out())
	}
	// Preserve: a cache variable that holds none of Jhon's files stays
	// writable.
	mod := filepath.Join(b.home, "gomod")
	mkdirs(t, mod)
	env = append(slices.Clone(b.env), "GOMODCACHE="+mod)
	if r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", "echo x > ~/gomod/x && echo wrote"); r.Code != 0 || r.Stdout != "wrote\n" {
		t.Errorf("$GOMODCACHE %s: exit %d, stdout %q, stderr %q; want it writable", mod, r.Code, r.Stdout, r.Stderr)
	}
}
