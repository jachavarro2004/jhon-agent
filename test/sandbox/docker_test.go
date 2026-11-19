// T128h (L1331, L1332): Docker is where the owner deploys its apps, so a
// docker or docker-compose line runs outside the sandbox, with the owner's
// environment (the rootless context, DOCKER_HOST); login, logout and a
// change of the owner's contexts keep the work profile, and so does a
// docker a sandboxed command could have written. These tests never run the
// real docker: a fake one writes what it was given outside every folder a
// sandboxed command may write.
package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// fakeDocker puts a docker in a folder of its own outside the box's
// writable folders, first on PATH: it appends its words and DOCKER_HOST to
// note.
func fakeDocker(t *testing.T, b *box, note string) {
	t.Helper()
	bin := filepath.Join(b.base, "docker-bin")
	support.Write(t, bin, map[string]string{"docker": "#!/bin/sh\necho \"$* host=$DOCKER_HOST\" >> " + note + "\n"})
	if err := os.Chmod(filepath.Join(bin, "docker"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i, kv := range b.env {
		if path, ok := strings.CutPrefix(kv, "PATH="); ok {
			b.env[i] = "PATH=" + bin + ":" + path
		}
	}
	b.env = append(b.env, "DOCKER_HOST=unix:///run/user/1000/docker.sock")
}

// K1, K2: an unsandboxed docker line runs outside the sandbox with the
// owner's DOCKER_HOST; the same line in the work profile, a login, a
// context change and a mixed line are refused the write outside.
func TestADockerLineRunsOutsideTheSandbox(t *testing.T) {
	b := newBox(t)
	note := filepath.Join(b.base, "docker-ran")
	fakeDocker(t, &b, note)
	r := b.unsandboxed(t, b.env, "docker compose up -d")
	got, _ := os.ReadFile(note)
	if want := "compose up -d host=unix:///run/user/1000/docker.sock\n"; r.Code != 0 || string(got) != want {
		t.Fatalf("an unsandboxed docker line: exit %d, stderr %q, note %q; want 0 and %q", r.Code, r.Stderr, got, want)
	}
	// T128h2 (L1338): a single-quoted template reaches docker as written.
	os.Remove(note)
	r = b.unsandboxed(t, b.env, "docker ps -a --format '{{.Names}}'")
	got, _ = os.ReadFile(note)
	if want := "ps -a --format {{.Names}} host=unix:///run/user/1000/docker.sock\n"; r.Code != 0 || string(got) != want {
		t.Errorf("a quoted template: exit %d, stderr %q, note %q; want 0 and %q", r.Code, r.Stderr, got, want)
	}
	for name, run := range map[string]func() support.Result{
		"docker ps in the work profile": func() support.Result { return b.sh(t, b.proj, "work", "docker ps") },
		"docker login":                  func() support.Result { return b.unsandboxed(t, b.env, "docker login") },
		"docker context use":            func() support.Result { return b.unsandboxed(t, b.env, "docker context use rootless") },
		"a mixed line":                  func() support.Result { return b.unsandboxed(t, b.env, "docker ps && true") },
	} {
		os.Remove(note)
		if r := run(); r.Code == 0 || exists(note) {
			t.Errorf("%s: exit %d, stderr %q, wrote outside %v; want it refused in the work profile", name, r.Code, r.Stderr, exists(note))
		}
	}
}

// K1: a docker planted in the project's bin, first on PATH, runs with the
// work profile: it runs, and its write outside is refused.
func TestAPlantedDockerRunsInTheWorkProfile(t *testing.T) {
	b := newBox(t)
	note := filepath.Join(b.base, "docker-ran")
	bin := filepath.Join(b.proj, "bin")
	support.Write(t, bin, map[string]string{"docker": "#!/bin/sh\necho ran > " + filepath.Join(b.proj, "planted-ran") + "\necho x >> " + note + "\n"})
	if err := os.Chmod(filepath.Join(bin, "docker"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i, kv := range b.env {
		if path, ok := strings.CutPrefix(kv, "PATH="); ok {
			b.env[i] = "PATH=" + bin + ":" + path
		}
	}
	r := b.unsandboxed(t, b.env, "docker ps")
	if r.Code == 0 || exists(note) || !exists(filepath.Join(b.proj, "planted-ran")) {
		t.Errorf("a planted docker: exit %d, stderr %q, ran %v, wrote outside %v; want it run in the work profile, its write refused", r.Code, r.Stderr, exists(filepath.Join(b.proj, "planted-ran")), exists(note))
	}
}

// K3: the profile command and Claude Code's wrap give a docker line the
// unsandboxed run, and keep login in the work wrap.
func TestTheRuntimesRunADockerLineUnsandboxed(t *testing.T) {
	bash := realShell(t, "bash")
	b, env := wired(t, bash)
	for line, want := range map[string]string{"docker compose up -d": "unsandboxed\n", "docker-compose logs": "unsandboxed\n", "docker login": "work\n", "docker ps && npm test": "work\n",
		"docker ps -a --format '{{.Names}}'": "unsandboxed\n", "docker --version": "unsandboxed\n", "docker ps --filter name=a*": "work\n"} {
		if r := support.Jhon(t, b.proj, env, "sandbox", "profile", "--project", b.proj, "--", line); r.Code != 0 || r.Stdout != want || r.Stderr != "" {
			t.Errorf("profile %q: exit %d, stdout %q, stderr %q; want %q", line, r.Code, r.Stdout, r.Stderr, want)
		}
	}
	r, a := preTool(t, b, env, bashGuards, "Bash", `{"command":"docker compose up -d"}`, false)
	if r.Code != 0 || a.Out.Decision != "allow" || a.command(t) != unwrapped(t, b.proj, bash, "docker compose up -d") {
		t.Errorf("Claude Code: exit %d, stderr %q, stdout %q; want the unsandboxed run", r.Code, r.Stderr, r.Stdout)
	}
	_, a = preTool(t, b, env, bashGuards, "Bash", `{"command":"docker login"}`, false)
	if got := a.command(t); got != expected(t, "Bash", got, b.proj, bash, "docker login") {
		t.Errorf("docker login: the command is %s; want the work wrap", got)
	}
}
