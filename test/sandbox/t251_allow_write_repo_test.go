// T251 (odd/tasks/v0-1-release.md L1721, L1722): the owner's sessions are
// pinned to one project while a second repository sits inside a
// sandbox.allow_write folder. A delivery line (fetch, pull, push,
// ls-remote, clone) of that repository runs outside the sandbox with every
// check the pinned project's lines get: the repository's git keys, the walk
// of its git folders, the clean PATH, the registry's reverse rule and the
// path opened with no link followed. Anything that fails a check keeps the
// line inside, and its note says why instead of "run it alone".
package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// t251Kept opens the note of a delivery line kept inside for a reason other
// than its shape.
const t251Kept = "git over SSH and gh ran in the sandbox, not outside it: "

// t251Box is the owner's shape: the box's project (the session's), the
// second repository work/b inside the sandbox.allow_write folder work, one
// commit behind its origin, a bare repository reached over ssh://. The ssh
// first on PATH, in a folder no sandboxed command may write, fails inside
// the sandbox as the real one does there ("Bad owner or permissions"),
// and outside it writes "ran" to note, waits $T251_SSH_SLEEP seconds and
// serves the remote.
type t251Box struct {
	box
	work, repo, remote, note string
}

func newT251Box(t *testing.T, settings string) t251Box {
	t.Helper()
	b := t251Box{box: newBox(t)}
	b.work = filepath.Join(b.home, "work")
	b.repo = filepath.Join(b.work, "b")
	b.remote = filepath.Join(b.base, "remote-b.git")
	b.note = filepath.Join(b.base, "ssh-ran")
	mkdirs(t, b.repo)
	support.GitRepo(t, b.repo)
	support.Git(t, b.base, "init", "-q", "--bare", b.remote)
	support.Git(t, b.repo, "commit", "-q", "--allow-empty", "-m", "ahead")
	support.Git(t, b.repo, "push", "-q", b.remote, "HEAD:refs/heads/main")
	support.Git(t, b.repo, "reset", "-q", "--hard", "HEAD~1")
	support.Git(t, b.repo, "remote", "add", "origin", "ssh://example.invalid"+b.remote)
	support.Git(t, b.repo, "config", "branch.main.remote", "origin")
	support.Git(t, b.repo, "config", "branch.main.merge", "refs/heads/main")
	bin := filepath.Join(b.base, "t251-bin")
	script(t, filepath.Join(bin, "ssh"), "#!/bin/sh\n"+
		"if [ \"$JHON_IN_SANDBOX\" = 1 ]; then echo 'Bad owner or permissions on /etc/ssh/ssh_config.d/20-systemd-ssh-proxy.conf' >&2; exit 255; fi\n"+
		"echo ran >> "+b.note+"\nsleep \"${T251_SSH_SLEEP:-0}\"\nfor last; do :; done\nexec sh -c \"$last\"\n")
	for i, kv := range b.env {
		if path, ok := strings.CutPrefix(kv, "PATH="); ok {
			b.env[i] = "PATH=" + bin + ":" + path
		}
	}
	b.settings(t, "schema = 1\n\n[sandbox]\n"+settings+"allow_write = [\""+b.work+"\"]\n")
	return b
}

// outside runs line in dir as the runtimes run a line they asked to run
// outside the sandbox, in a session pinned to the box's project.
func (b t251Box) outside(t *testing.T, dir string, env []string, line string) support.Result {
	t.Helper()
	return support.Jhon(t, dir, env, "sandbox", "run", "--project", b.proj, "--profile", "unsandboxed", "--", realShell(t, "sh"), "-c", line)
}

// A fetch, pull, push, ls-remote and clone of the second repository, named
// with -C, a leading cd or run in it, run outside the sandbox from the
// project's session: the ssh wrote its note, which only an unsandboxed ssh
// can, and git did the work. `jhon sandbox profile` and Claude Code's hook
// send such a line outside.
func TestT251ASessionOfTheProjectDeliversTheRepositoryInAllowWrite(t *testing.T) {
	b := newT251Box(t, "mode = \"on\"\n")
	main := strings.TrimSpace(support.Git(t, b.remote, "rev-parse", "main"))
	if r := support.Jhon(t, b.proj, b.env, "sandbox", "profile", "--project", b.proj, "--", "git -C "+b.repo+" fetch origin"); r.Code != 0 || r.Stdout != "unsandboxed\n" {
		t.Errorf("profile: exit %d, stdout %q, stderr %q; want unsandboxed", r.Code, r.Stdout, r.Stderr)
	}
	r := b.outside(t, b.proj, b.env, "git -C "+b.repo+" fetch -q origin")
	if r.Code != 0 || r.Stderr != "" || !exists(b.note) {
		t.Fatalf("git -C <b> fetch: exit %d, stderr %q, ran outside %v; want it run outside", r.Code, r.Stderr, exists(b.note))
	}
	if got := strings.TrimSpace(support.Git(t, b.repo, "rev-parse", "refs/remotes/origin/main")); got != main {
		t.Errorf("origin/main is %q after the fetch; want %q", got, main)
	}
	os.Remove(b.note)
	if r := b.outside(t, b.proj, b.env, "cd "+b.repo+" && git pull -q --ff-only"); r.Code != 0 || r.Stderr != "" || !exists(b.note) {
		t.Errorf("cd <b> && git pull: exit %d, stderr %q, ran outside %v", r.Code, r.Stderr, exists(b.note))
	}
	if got := strings.TrimSpace(support.Git(t, b.repo, "rev-parse", "HEAD")); got != main {
		t.Errorf("b's HEAD is %q after the pull; want %q", got, main)
	}
	support.Git(t, b.repo, "commit", "-q", "--allow-empty", "-m", "feature")
	os.Remove(b.note)
	if r := b.outside(t, b.repo, b.env, "git push -q origin HEAD:refs/heads/feature"); r.Code != 0 || r.Stderr != "" || !exists(b.note) {
		t.Errorf("git push run in b: exit %d, stderr %q, ran outside %v", r.Code, r.Stderr, exists(b.note))
	}
	if got, want := strings.TrimSpace(support.Git(t, b.remote, "rev-parse", "feature")), strings.TrimSpace(support.Git(t, b.repo, "rev-parse", "HEAD")); got != want {
		t.Errorf("the remote's feature is %q; want b's HEAD %q", got, want)
	}
	if r := b.outside(t, b.proj, b.env, "git -C "+b.repo+" ls-remote origin"); r.Code != 0 || !strings.Contains(r.Stdout, "refs/heads/feature") {
		t.Errorf("git -C <b> ls-remote: exit %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}
	if r := b.outside(t, b.proj, b.env, "git -C "+b.repo+" clone -q ssh://example.invalid"+b.remote+" vendor/copy"); r.Code != 0 || !exists(filepath.Join(b.repo, "vendor", "copy", ".git")) {
		t.Errorf("git -C <b> clone into b: exit %d, stderr %q", r.Code, r.Stderr)
	}
	// Claude Code's hook hands the line back to run outside the sandbox.
	bash := realShell(t, "bash")
	env := append(append([]string{}, b.env...), "JHON_SANDBOX_PROJECT="+b.proj, "SHELL="+bash)
	line := "git -C " + b.repo + " fetch origin"
	if _, a := preTool(t, b.box, env, bashGuards, "Bash", `{"command":`+quoted(t, line)+`}`, false); a.command(t) != unwrapped(t, b.proj, bash, line) {
		t.Errorf("the hook wrapped the line as\n %s\nwant\n %s", a.command(t), unwrapped(t, b.proj, bash, line))
	}
}

// The repository reached through a link, a repository outside every
// allowed folder and a folder in the allowed one that is no repository keep
// the line inside; its failure says which.
func TestT251ALineKeptInsideSaysWhy(t *testing.T) {
	b := newT251Box(t, "")
	link := filepath.Join(b.home, "blink")
	if err := os.Symlink(b.repo, link); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(b.home, "other")
	mkdirs(t, other)
	support.GitRepo(t, other)
	support.Git(t, other, "remote", "add", "origin", "ssh://example.invalid"+b.remote)
	plain := filepath.Join(b.work, "plain")
	mkdirs(t, plain)
	neither := "; fix: run the line in a session of that repository (jhon start there), add a folder that holds the repository to sandbox.allow_write (jhon settings set sandbox.allow_write <folder>,…) if sandboxed commands may write it, or run the line yourself in a terminal\n"
	for _, c := range []struct{ name, line, note string }{
		{"through a link", "git -C " + link + " fetch origin", link + " is or passes through the link " + link + ", and a line outside the sandbox reaches a repository only by a path with no link; fix: name its real path (" + b.repo + ") instead\n"},
		{"a cd through a link", "cd " + link + " && git fetch origin", link + " is or passes through the link " + link + ", and a line outside the sandbox reaches a repository only by a path with no link; fix: name its real path (" + b.repo + ") instead\n"},
		{"outside every allowed folder", "git -C " + other + " ls-remote origin", other + " is neither in the project (" + b.proj + ") nor in a git repository inside a sandbox.allow_write folder" + neither},
		{"no repository", "git -C " + plain + " ls-remote ssh://example.invalid" + b.remote, plain + " is in the sandbox.allow_write folder " + b.work + " but in no git repository inside it; fix: only a git repository there counts as a project: run the line in one, or run it yourself in a terminal\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			os.Remove(b.note)
			if r := support.Jhon(t, b.proj, b.env, "sandbox", "profile", "--project", b.proj, "--", c.line); r.Stdout != "work\n" {
				t.Errorf("profile: %q, want work", r.Stdout)
			}
			r := b.outside(t, b.proj, b.env, c.line)
			if r.Code == 0 || exists(b.note) || !strings.HasSuffix(r.Stderr, "\n"+t251Kept+c.note) || strings.Contains(r.Stderr, "run it alone") {
				t.Errorf("exit %d, ran outside %v, stderr %q; want a failure inside ending with\n %s", r.Code, exists(b.note), r.Stderr, t251Kept+c.note)
			}
		})
	}
	// A line kept inside for its shape keeps its note.
	if r := b.outside(t, b.proj, b.env, "set -o pipefail; git -C "+b.repo+" fetch origin | cat"); !strings.HasSuffix(r.Stderr, "\n"+deliveryNote) {
		t.Errorf("a piped fetch: exit %d, stderr %q; want the run-it-alone note", r.Code, r.Stderr)
	}
}

// A sandboxed command of the project's session can write the second
// repository (it is in allow_write), so it may poison its git folder or
// configuration: the outside line then runs inside, says why at its start,
// and its failure ends with the same reason; ~/.bashrc stays as it was. A
// hard link across the sandbox's mounts cannot be made inside it, so that
// one is planted outside, as one left from before.
func TestT251APoisonedRepositoryKeepsTheLineInside(t *testing.T) {
	for _, c := range []struct {
		name, plant, why string
		outside          bool
	}{
		{"a link in the git folder", `ln -s "$HOME/.bashrc" .git/FETCH_HEAD`, "{repo}/.git/FETCH_HEAD in the repository's git folder is a link, which git would write through", false},
		{"a hard link in the git folder", `: > "$HOME/notes" && ln "$HOME/notes" .git/FETCH_HEAD`, "{repo}/.git/FETCH_HEAD in the repository's git folder shares its data with another file (a hard link), which git would write through", true},
		{"a FIFO in the git folder", `mkfifo .git/hold`, "{repo}/.git/hold in the repository's git folder is not a folder or a regular file", false},
		{"a key off the allowlist", `git config http.cookieFile "$HOME/.bashrc"`, "the repository's configuration sets http.cookiefile, which is not among the keys a git line runs outside the sandbox with", false},
		{"a git folder elsewhere", `mv .git ../b.git && printf 'gitdir: %s\n' "$PWD/../b.git" > .git`, "the repository's git folder {work}/b.git is not in the repository {repo}", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newT251Box(t, "")
			support.Write(t, b.home, map[string]string{".bashrc": "owner\n"})
			args := []string{support.Bin(t), "sandbox", "run", "--project", b.proj, "--", "sh", "-c", c.plant + " && echo set"}
			if c.outside {
				args = []string{"sh", "-c", c.plant + " && echo set"}
			}
			if r := support.Run(t, support.Cmd{Args: args, Dir: b.repo, Env: b.env}); r.Stdout != "set\n" {
				t.Fatalf("plant: exit %d, %q, %q", r.Code, r.Stdout, r.Stderr)
			}
			why := strings.NewReplacer("{repo}", b.repo, "{work}", b.work).Replace(c.why)
			r := b.outside(t, b.proj, b.env, "git -C "+b.repo+" fetch origin")
			start := "jhon sandbox: " + why + "; the line runs in the sandbox instead\n"
			end := "\n" + t251Kept + why + "; fix: remove that from the repository (a sandboxed command may have put it there), or run the line yourself in a terminal\n"
			// A failure above 128 (git's 255 on a read-only FETCH_HEAD) gets
			// no note, as any such exit.
			if r.Code == 0 || exists(b.note) || !strings.HasPrefix(r.Stderr, start) || r.Code <= 128 && !strings.HasSuffix(r.Stderr, end) || strings.Contains(r.Stderr, "run it alone") {
				t.Errorf("exit %d, ran outside %v, stderr %q; want a failure inside opening with %q and ending with %q", r.Code, exists(b.note), r.Stderr, start, end)
			}
			if got := support.Read(t, filepath.Join(b.home, ".bashrc")); got != "owner\n" {
				t.Errorf("~/.bashrc is %q", got)
			}
		})
	}
}

// The reverse rule: while a sandboxed command that can write the second
// repository runs (a session of another project, through the allowed
// folder), the repository's outside line waits and is refused; while that
// line runs, such a sandboxed command waits and is refused.
func TestT251SandboxedRunsAndTheRepositorysOutsideLineWaitForEachOther(t *testing.T) {
	b := newT251Box(t, "")
	other := filepath.Join(b.home, "d")
	mkdirs(t, other)
	support.GitRepo(t, other)
	racer := exec.Command(support.Bin(t), "sandbox", "run", "--project", other, "--", "sh", "-c", "sleep 30")
	racer.Dir, racer.Env = other, b.env
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
	time.Sleep(time.Second)
	if r := b.outside(t, b.proj, b.env, "git -C "+b.repo+" fetch -q origin"); r.Code != 126 || exists(b.note) || !strings.HasPrefix(r.Stderr, outsideWhileRunning) {
		t.Errorf("while a sandboxed command that can write b runs: exit %d, ran %v, stderr %q; want 126 and the note", r.Code, exists(b.note), r.Stderr)
	}
	stop()
	slow := exec.Command(support.Bin(t), "sandbox", "run", "--project", b.proj, "--profile", "unsandboxed", "--", realShell(t, "sh"), "-c", "git -C "+b.repo+" fetch -q origin")
	slow.Dir, slow.Env = b.proj, append(append([]string{}, b.env...), "T251_SSH_SLEEP=6")
	if err := slow.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { slow.Process.Kill(); slow.Wait() })
	for deadline := time.Now().Add(10 * time.Second); !exists(b.note) && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
	}
	if !exists(b.note) {
		t.Fatal("the outside fetch of b never reached its ssh")
	}
	if r := support.Jhon(t, other, b.env, "sandbox", "run", "--project", other, "--", "true"); r.Code != 126 || !strings.HasPrefix(r.Stderr, insideWhileOutside) {
		t.Errorf("a sandboxed command while b's outside line runs: exit %d, stderr %q; want 126 and the note", r.Code, r.Stderr)
	}
	if err := slow.Wait(); err != nil {
		t.Errorf("the outside fetch of b: %v", err)
	}
}
