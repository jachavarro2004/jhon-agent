package sandbox

// T250 units 1-3, the verifier's probes (odd/tasks/studio-integration-v1.md L43).

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// L43a, same class: git opens each .gitignore on the way to an untracked file a pull would overwrite.
func TestAPipeForAGitignoreKeepsAPullInsideAndNothingWaits(t *testing.T) {
	b := newBox(t)
	ssh := filepath.Join(b.base, "ssh-ran")
	remote := originOverSSH(t, &b, ssh)
	script(t, filepath.Join(b.base, "bin", "ssh"), "#!/bin/sh\n[ \"$JHON_IN_SANDBOX\" = 1 ] && { echo 'ssh: refused inside' >&2; exit 255; }\necho ran >> "+ssh+"\nfor last; do :; done\nexec sh -c \"$last\"\n")
	support.Write(t, b.proj, map[string]string{"sub/b.txt": "content\n"})
	support.Git(t, b.proj, "add", "sub/b.txt")
	support.Git(t, b.proj, "commit", "-q", "-m", "b")
	support.Git(t, b.proj, "push", "-q", remote, "HEAD:refs/heads/main")
	support.Git(t, b.proj, "reset", "-q", "--hard", "HEAD~1")
	mkdirs(t, filepath.Join(b.proj, "sub"))
	fifo := filepath.Join(b.proj, "sub", ".gitignore")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	support.Write(t, b.proj, map[string]string{"sub/b.txt": "untracked\n"})
	r, finished := unsandboxedWithin(t, b, 30*time.Second, "git pull -q --ff-only")
	if !finished {
		t.Fatalf("the pull still waited after 30 s on the pipe %s (stderr %q)", fifo, r.Stderr)
	}
	if !strings.Contains(r.Stderr, "jhon sandbox: "+fifo+" is not a regular file") {
		t.Errorf("exit %d, stderr %q; want the pull kept inside for the pipe", r.Code, r.Stderr)
	}
}

// L43d: --config after other global options, relative, or a folder reached through a link.
func TestADockerConfigFolderHoweverItIsNamedKeepsTheLineInside(t *testing.T) {
	b := newBox(t)
	note := filepath.Join(b.base, "docker-ran")
	fakeDocker(t, &b, note)
	planted := filepath.Join(b.proj, "dockerconf")
	mkdirs(t, filepath.Join(planted, "cli-plugins"))
	link := filepath.Join(b.base, "dockerlink")
	if err := os.Symlink(planted, link); err != nil {
		t.Fatal(err)
	}
	safe := filepath.Join(b.base, "safeconf")
	mkdirs(t, safe)
	if err := os.Symlink(filepath.Join(planted, "cli-plugins"), filepath.Join(safe, "cli-plugins")); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, line, env string }{
		{"-c before --config", "docker -c default --config " + planted + " ps", ""},
		{"-H before --config", "docker -H unix:///nonexistent --config " + planted + " ps", ""},
		{"--context before --config", "docker --context default --config " + planted + " ps", ""},
		{"-l and -D before --config", "docker -D -l info --config " + planted + " ps", ""},
		{"a safe --config then the planted one", "docker --config " + safe + " --config " + planted + " ps", ""},
		{"relative --config", "docker --config dockerconf ps", ""},
		{"DOCKER_CONFIG through a link", "docker ps", "DOCKER_CONFIG=" + link},
		{"cli-plugins a link into the project", "docker ps", "DOCKER_CONFIG=" + safe},
	} {
		t.Run(c.name, func(t *testing.T) {
			os.Remove(note)
			env := slices.Clone(b.env)
			if c.env != "" {
				env = append(env, c.env)
			}
			r := b.unsandboxed(t, env, c.line)
			if exists(note) || !strings.Contains(r.Stderr, "jhon sandbox: docker loads CLI plugins from ") {
				t.Errorf("%s: ran outside %v, stderr %q; want it kept inside", c.line, exists(note), r.Stderr)
			}
		})
	}
}

// L43c: the owner's configuration from a writable file, whichever way git is told to read it.
func TestTheOwnersGitConfigurationFromAWritableFileKeepsTheLineInside(t *testing.T) {
	b := newBox(t)
	ran := filepath.Join(b.base, "proxy-ran")
	proxy := filepath.Join(b.base, "proxy.sh")
	script(t, proxy, "#!/bin/sh\necho proxy >> "+ran+"\n")
	inc := filepath.Join(b.home, ".cache", "git", "extra.inc")
	support.Write(t, b.home, map[string]string{".cache/git/extra.inc": "[core]\n\tgitProxy = " + proxy + "\n"})
	mkdirs(t, filepath.Join(b.home, ".config", "git"))
	if err := os.Symlink(inc, filepath.Join(b.home, ".config", "git", "link.inc")); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, gitconfig, env string }{
		{"an include through a link", "[include]\n\tpath = ~/.config/git/link.inc\n", ""},
		{"an include relative to ~/.gitconfig", "[include]\n\tpath = .cache/git/extra.inc\n", ""},
		{"GIT_CONFIG_GLOBAL relative to the project", "", "GIT_CONFIG_GLOBAL=../.cache/git/extra.inc"},
		{"GIT_CONFIG_SYSTEM in the cache", "", "GIT_CONFIG_SYSTEM=" + inc},
		{"include.path from GIT_CONFIG_COUNT", "", "GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=include.path GIT_CONFIG_VALUE_0=" + inc},
	} {
		t.Run(c.name, func(t *testing.T) {
			os.Remove(ran)
			support.Write(t, b.home, map[string]string{".gitconfig": c.gitconfig})
			env := slices.Clone(b.env)
			if c.env != "" {
				env = append(env, strings.Fields(c.env)...)
			}
			r := b.unsandboxed(t, env, "git ls-remote git://127.0.0.1:9/x")
			if exists(ran) || !strings.Contains(r.Stderr, " configuration reads ") {
				t.Errorf("ran outside %v, stderr %q; want the line kept inside", exists(ran), r.Stderr)
			}
		})
	}
}

// L43e: git planted in another repository's PATH folder, through a link, never runs.
func TestGitPlantedInAnotherRepositorysPATHFolderNeverRuns(t *testing.T) {
	b := newBox(t)
	other := filepath.Join(b.base, "other")
	mkdirs(t, other)
	support.GitRepo(t, other)
	planted := filepath.Join(b.base, "planted-ran")
	script(t, filepath.Join(other, "tools", "bin", "git"), "#!/bin/sh\necho \"$*\" >> "+planted+"\nexec "+realShell(t, "git")+" \"$@\"\n")
	link := filepath.Join(b.base, "otherlink")
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	prependPath(&b, filepath.Join(link, "tools", "bin"))
	b.unsandboxed(t, b.env, "git fetch -q "+b.proj)
	if exists(planted) {
		data, _ := os.ReadFile(planted)
		t.Errorf("the git planted in another repository ran outside the sandbox: %q", data)
	}
}
