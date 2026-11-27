// T252 (odd/tasks/v0-1-release.md L1721, L1740): two notes that keep an
// agent from a wrong conclusion. With the docker socket hidden
// (sandbox.docker off), a docker call inside the sandbox fails with the
// client's own "cannot connect" message, and a builder concluded the
// machine had no Docker: the run now ends with a note that names the
// setting, that a docker line of its own runs outside, and what the owner
// would change. A delivery line whose leading cd names its folder with a
// `..` part stays in the sandbox, and its note said "run it alone", which
// it already was: it now says to name the folder with no `..`, and gives
// the line that runs outside.
package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// t252DockerNote is the note a run ends with when its output says docker
// cannot reach its daemon and the sandbox hides the socket.
const t252DockerNote = "jhon sandbox: docker cannot reach its daemon from inside the sandbox, which hides the docker socket (sandbox.docker is off), so this says nothing of whether Docker runs on this machine; fix: run the docker command on a line of its own in the project, with no pipes or other commands (as in docker compose up -d), which runs outside the sandbox, or ask the owner to run jhon settings set sandbox.docker true, which shows the socket inside the sandbox, where a container can write anywhere the user can\n"

// The client's own messages: docker 29's, and the one older clients and
// docker-compose v1 print.
const (
	t252Docker29  = "failed to connect to the docker API at unix:///var/run/docker.sock; check if the path is correct and if the daemon is running: dial unix /var/run/docker.sock: connect: no such file or directory"
	t252DockerOld = "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?"
)

// t252Docker puts a docker first on PATH, in a folder no sandboxed command
// may write: it prints $T252_SAYS on stderr and exits 1, and outside the
// sandbox it also writes "outside" to the note it returns.
func t252Docker(t *testing.T, b *box) string {
	t.Helper()
	note := filepath.Join(b.base, "docker-ran")
	bin := filepath.Join(b.base, "t252-bin")
	script(t, filepath.Join(bin, "docker"), "#!/bin/sh\n[ \"$JHON_IN_SANDBOX\" = 1 ] || echo outside >> "+note+"\necho \"$T252_SAYS\" >&2\nexit 1\n")
	for i, kv := range b.env {
		if path, ok := strings.CutPrefix(kv, "PATH="); ok {
			b.env[i] = "PATH=" + bin + ":" + path
		}
	}
	return note
}

// A docker call that cannot reach its daemon inside the sandbox, with the
// socket hidden, ends with the note on a line of its own after the
// client's message, whatever its exit (a pipe to cat exits 0); its exit
// and output are the client's.
func TestT252ADockerCallWithTheSocketHiddenSaysWhy(t *testing.T) {
	b := newBox(t)
	t252Docker(t, &b)
	for _, c := range []struct {
		name, says, line, stdout, stderr string
		code                             int
	}{
		{"docker 29", t252Docker29, "docker ps", "", t252Docker29 + "\n" + t252DockerNote, 1},
		{"an older client", t252DockerOld, "docker compose up -d", "", t252DockerOld + "\n" + t252DockerNote, 1},
		{"piped to cat", t252Docker29, "docker ps 2>&1 | cat", t252Docker29 + "\n", t252DockerNote, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			env := append(append([]string{}, b.env...), "T252_SAYS="+c.says)
			r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", c.line)
			if r.Code != c.code || r.Stdout != c.stdout || r.Stderr != c.stderr {
				t.Errorf("exit %d, stdout %q, stderr %q; want %d, %q and\n%q", r.Code, r.Stdout, r.Stderr, c.code, c.stdout, c.stderr)
			}
		})
	}
}

// The note costs ordinary work nothing: a failure that is no docker
// daemon's, docker's own other failures, a docker line run outside the
// sandbox, and a run with sandbox.docker on (the socket is there, so the
// message means what it says) end with no note.
func TestT252NoDockerNoteWhereTheSocketIsNotWhy(t *testing.T) {
	b := newBox(t)
	note := t252Docker(t, &b)
	with := func(says string) []string { return append(append([]string{}, b.env...), "T252_SAYS="+says) }
	for _, c := range []struct {
		name, says, line string
		code             int
	}{
		{"another failure", "", "echo nope >&2; exit 3", 3},
		{"docker's own other failure", "no configuration file provided: not found", "docker compose ps", 1},
	} {
		r := support.Jhon(t, b.proj, with(c.says), "sandbox", "run", "--", "sh", "-c", c.line)
		want := strings.TrimPrefix(c.says+"\n", "\n")
		if c.name == "another failure" {
			want = "nope\n"
		}
		if r.Code != c.code || r.Stderr != want {
			t.Errorf("%s: exit %d, stderr %q; want %d and %q", c.name, r.Code, r.Stderr, c.code, want)
		}
	}
	// Outside the sandbox the socket is not hidden: the message is the
	// daemon's own news.
	if r := b.unsandboxed(t, with(t252Docker29), "docker ps"); r.Code != 1 || r.Stderr != t252Docker29+"\n" || support.Read(t, note) != "outside\n" {
		t.Errorf("a docker line run outside: exit %d, stderr %q, ran outside %v; want 1 and the client's message alone", r.Code, r.Stderr, exists(note))
	}
	b.settings(t, "schema = 1\n\n[sandbox]\ndocker = true\n")
	if r := support.Jhon(t, b.proj, with(t252Docker29), "sandbox", "run", "--", "sh", "-c", "docker ps"); r.Code != 1 || r.Stderr != t252Docker29+"\n" {
		t.Errorf("sandbox.docker on: exit %d, stderr %q; want 1 and the client's message alone", r.Code, r.Stderr)
	}
}

// A delivery line whose leading cd names its folder with `..` stays in the
// sandbox (profile work), and its failure says to name the folder with no
// `..`, giving the line that would run outside; that line does run
// outside. With the `..` gone, a line still kept inside for where its git
// works ends with that reason, and one kept inside for its shape keeps the
// run-it-alone note.
func TestT252ACdWithDotDotSaysHowToRunOutside(t *testing.T) {
	b := newT251Box(t, "")
	other := filepath.Join(b.home, "other")
	mkdirs(t, other)
	support.GitRepo(t, other)
	support.Git(t, other, "remote", "add", "origin", "ssh://example.invalid"+b.remote)
	fixed := "cd " + b.repo + " && git fetch origin"
	neither := "; fix: run the line in a session of that repository (jhon start there), add a folder that holds the repository to sandbox.allow_write (jhon settings set sandbox.allow_write <folder>,…) if sandboxed commands may write it, or run the line yourself in a terminal\n"
	for _, c := range []struct{ name, line, note string }{
		{"a relative cd with ..", "cd ../work/b && git fetch origin", t251Kept + "the line's cd names its folder with a .. part, which a line outside the sandbox never takes (a cd through a link and back up lands elsewhere); fix: name the folder by its absolute path with no .. part, as in " + fixed + "\n"},
		{"an absolute cd with ..", "cd " + b.proj + "/../work/b && git fetch origin", t251Kept + "the line's cd names its folder with a .. part, which a line outside the sandbox never takes (a cd through a link and back up lands elsewhere); fix: name the folder by its absolute path with no .. part, as in " + fixed + "\n"},
		{"a folder outside every allowed one", "cd ../other && git fetch origin", t251Kept + other + " is neither in the project (" + b.proj + ") nor in a git repository inside a sandbox.allow_write folder" + neither},
		{"a piped fetch", "set -o pipefail; cd ../work/b && git fetch origin | cat", deliveryNote},
	} {
		t.Run(c.name, func(t *testing.T) {
			os.Remove(b.note)
			if r := support.Jhon(t, b.proj, b.env, "sandbox", "profile", "--project", b.proj, "--", c.line); r.Stdout != "work\n" {
				t.Errorf("profile: %q, want work", r.Stdout)
			}
			r := b.outside(t, b.proj, b.env, c.line)
			if r.Code == 0 || exists(b.note) || !strings.HasSuffix(r.Stderr, "\n"+c.note) {
				t.Errorf("exit %d, ran outside %v, stderr %q; want a failure inside ending with\n %s", r.Code, exists(b.note), r.Stderr, c.note)
			}
		})
	}
	// The line the note gives runs outside.
	os.Remove(b.note)
	if r := b.outside(t, b.proj, b.env, fixed); r.Code != 0 || !exists(b.note) {
		t.Errorf("the note's line: exit %d, ran outside %v, stderr %q; want it run outside", r.Code, exists(b.note), r.Stderr)
	}
}
