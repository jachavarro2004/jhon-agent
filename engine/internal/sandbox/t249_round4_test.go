package sandbox

// T249 round-4 verify (studio-integration-v1.md L40): the verifier's probes
// kept as regressions.

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// hiddenIn is whether args hide p: a --tmpfs on it or a folder holding it,
// or /dev/null bound on it.
func hiddenIn(args []string, p string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--tmpfs" && (args[i+1] == p || strings.HasPrefix(p, args[i+1]+"/")) {
			return true
		}
		if args[i] == "--dev-bind" && args[i+1] == "/dev/null" && i+2 < len(args) && (args[i+2] == p || strings.HasPrefix(p, args[i+2]+"/")) {
			return true
		}
	}
	return false
}

// Item 1 (L39): with sandbox.docker off, a container socket named by
// DOCKER_HOST or CONTAINER_HOST in any spelling the docker CLI or podman
// accepts, reached through a link or through /var/run -> /run, or kept
// under $DOCKER_CONFIG, is hidden; with it on, none is.
func TestAContainerSocketInAnySpellingIsHidden(t *testing.T) {
	d := with(machine, disk{
		"/srv": "dir", "/srv/a": "dir", "/srv/a/docker.sock": "file",
		"/srv/l": "-> /srv/a", "/srv/l/docker.sock": "-> /srv/a/docker.sock",
		"/var/run": "-> /run", "/var/run/docker.sock": "-> /run/docker.sock", "/run/docker.sock": "file",
		"/var/run/podman": "-> /run/podman", "/run/podman": "dir", "/run/podman/podman.sock": "file",
		"/srv/dc": "dir", "/srv/dc/run": "dir", "/srv/dc/run/docker.sock": "file", "/srv/dc/desktop": "dir",
	})
	for _, c := range []struct{ name, variable, value, sock string }{
		{"unix:// absolute", "DOCKER_HOST", "unix:///srv/a/docker.sock", "/srv/a/docker.sock"},
		{"unix: one slash", "DOCKER_HOST", "unix:/srv/a/docker.sock", "/srv/a/docker.sock"},
		{"unix://// extra slashes", "DOCKER_HOST", "unix:////srv/a/docker.sock", "/srv/a/docker.sock"},
		{"through a link", "DOCKER_HOST", "unix:///srv/l/docker.sock", "/srv/a/docker.sock"},
		{"CONTAINER_HOST", "CONTAINER_HOST", "unix:///srv/a/docker.sock", "/srv/a/docker.sock"},
		// podman parses CONTAINER_HOST with url.Parse: the scheme is case
		// insensitive and a query string is not part of the path.
		{"uppercase scheme (podman)", "CONTAINER_HOST", "UNIX:///srv/a/docker.sock", "/srv/a/docker.sock"},
		{"a query string (podman)", "CONTAINER_HOST", "unix:///srv/a/docker.sock?secure=true", "/srv/a/docker.sock"},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := sample(d)
			in.Env[c.variable] = c.value
			if args := Args(in); !hiddenIn(args, c.sock) {
				t.Errorf("%s=%s: %s stays reachable with sandbox.docker off:\n%s", c.variable, c.value, c.sock, strings.Join(args, " "))
			}
		})
	}
	t.Run("the system sockets through /var/run -> /run", func(t *testing.T) {
		args := Args(sample(d))
		for _, p := range []string{"/run/docker.sock", "/run/podman/podman.sock"} {
			if !hiddenIn(args, p) {
				t.Errorf("%s is reachable:\n%s", p, strings.Join(args, " "))
			}
		}
	})
	t.Run("$DOCKER_CONFIG's run and desktop folders", func(t *testing.T) {
		in := sample(d)
		in.Env["DOCKER_CONFIG"] = "/srv/dc"
		args := Args(in)
		for _, p := range []string{"/srv/dc/run/docker.sock", "/srv/dc/desktop"} {
			if !hiddenIn(args, p) {
				t.Errorf("%s is reachable:\n%s", p, strings.Join(args, " "))
			}
		}
	})
	t.Run("tcp and ssh hide nothing more", func(t *testing.T) {
		plain := strings.Join(Args(sample(d)), " ")
		for _, h := range []string{"tcp://127.0.0.1:2375", "ssh://user@host", "npipe:////./pipe/docker_engine", ""} {
			in := sample(d)
			in.Env["DOCKER_HOST"], in.Env["CONTAINER_HOST"] = h, h
			in.ContainerHosts = []string{h}
			if got := strings.Join(Args(in), " "); got != plain {
				t.Errorf("DOCKER_HOST=%q changes the arguments:\n%s\nwant\n%s", h, got, plain)
			}
		}
	})
	t.Run("sandbox.docker on shows them all", func(t *testing.T) {
		in := sample(d)
		in.Docker = true
		in.Env["DOCKER_HOST"], in.Env["CONTAINER_HOST"], in.Env["DOCKER_CONFIG"] = "unix:///srv/l/docker.sock", "unix:///srv/a/docker.sock", "/srv/dc"
		in.ContainerHosts = []string{"unix:///srv/a/docker.sock"}
		args := Args(in)
		for _, p := range []string{"/srv/a/docker.sock", "/run/docker.sock", "/run/podman/podman.sock", "/srv/dc/run/docker.sock"} {
			if hiddenIn(args, p) {
				t.Errorf("%s is hidden with sandbox.docker on:\n%s", p, strings.Join(args, " "))
			}
		}
	})
}

// Item 1: the docker contexts as the docker CLI writes them, and shapes it
// does not: a context's unix endpoint is found; a meta.json that is a FIFO
// (planted where a command could write the contexts) does not hang the
// read of every next sandbox.
func TestDockerContextsReadEveryShapeAndNeverBlock(t *testing.T) {
	home := t.TempDir()
	write := func(name, body string) {
		p := filepath.Join(home, ".docker", "contexts", "meta", name, "meta.json")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("aa", `{"Name":"colima","Metadata":{"Description":"x"},"Endpoints":{"docker":{"Host":"unix:///home/u/.colima/default/docker.sock","SkipTLSVerify":false}}}`)
	write("bb", `{"Name":"lower","endpoints":{"docker":{"host":"unix:///srv/lower.sock"}}}`)
	write("cc", `not json`)
	got := strings.Join(DockerContexts(home, map[string]string{}), " ")
	for _, want := range []string{"unix:///home/u/.colima/default/docker.sock", "unix:///srv/lower.sock"} {
		if !strings.Contains(got, want) {
			t.Errorf("DockerContexts = %q; want %q in it", got, want)
		}
	}
	fifo := filepath.Join(home, ".docker", "contexts", "meta", "00", "meta.json")
	if err := os.MkdirAll(filepath.Dir(fifo), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("no FIFO: %v", err)
	}
	done := make(chan []string, 1)
	go func() { done <- DockerContexts(home, map[string]string{}) }()
	select {
	case got := <-done:
		if !strings.Contains(strings.Join(got, " "), "colima") {
			t.Errorf("with a FIFO context: %q", got)
		}
	case <-time.After(3 * time.Second):
		if f, err := os.OpenFile(fifo, os.O_WRONLY, 0); err == nil {
			f.Close()
		}
		t.Errorf("DockerContexts blocks on a context's meta.json that is a FIFO")
	}
}

// Item 2 (L39): each repository config that names a helper transport, an
// option, a program or another file keeps an outside git line in the
// sandbox, also spelled with mixed case, a dotted subsection, in the
// worktree scope, or after a good value of the same key.
func TestRepositoryKeysThatKeepTheLineInside(t *testing.T) {
	gate := [][2]string{{"hook.jhon-merge-gate.event", ""}, {"hook.jhon-merge-gate.event", "reference-transaction"}, {"hook.jhon-merge-gate.command", "'/j' hook merge-gate"}, {"hook.jhon-merge-gate.enabled", "true"}}
	for name, listing := range map[string]string{
		"url ext::":                       entry("local", "remote.origin.url", "ext::sh -c x"),
		"url EXT:: upper":                 entry("local", "remote.origin.url", "EXT::sh -c x"),
		"url fd::":                        entry("local", "remote.origin.url", "fd::3"),
		"pushurl ext::":                   entry("local", "remote.origin.pushurl", "ext::sh -c x"),
		"url starting with -":             entry("local", "remote.origin.url", "-oProxyCommand=x"),
		"dotted subsection url ext::":     entry("local", "remote.Origin.x.url", "ext::sh -c x"),
		"duplicate url, second bad":       entry("local", "remote.origin.url", "https://example.invalid/r") + entry("local", "remote.origin.url", "ext::sh -c x"),
		"tagopt as an option":             entry("local", "remote.origin.tagopt", "--upload-pack=x"),
		"include.path":                    entry("local", "include.path", "../x"),
		"includeIf":                       entry("local", "includeif.gitdir:/.path", "/x"),
		"worktree cookieFile":             entry("worktree", "http.cookiefile", "/home/u/.bashrc"),
		"worktree core.worktree":          entry("worktree", "core.worktree", "/home/u"),
		"url insteadOf":                   entry("local", "url.ext::sh -c x.insteadof", "https://example.invalid/"),
		"core subsection hookspath":       entry("local", "core.x.hookspath", "x"),
		"remote without subsection url":   entry("local", "remote.url", "x"),
		"gate command near miss":          entry("local", "hook.jhon-merge-gate.command", "'/j' hook merge-gate; evil"),
		"gate event near miss":            entry("local", "hook.jhon-merge-gate.event", "pre-push"),
		"gate enabled near miss":          entry("local", "hook.jhon-merge-gate.enabled", "yes"),
		"another hook with the gate name": entry("local", "hook.jhon-merge-gate2.command", "'/j' hook merge-gate"),
		"interactive branch rebase":       entry("local", "branch.main.rebase", "interactive"),
		"gpg.format other":                entry("local", "gpg.format", "ssh;x"),
		"gpg.ssh.program":                 entry("local", "gpg.ssh.program", "evil"),
		"gpg.ssh.defaultKeyCommand":       entry("local", "gpg.ssh.defaultkeycommand", "evil"),
		"user.signingkey in a subsection": entry("local", "user.x.signingkey", "x"),
		// branch.<name>.remote names a remote, or is itself the URL when no
		// remote has that name: the allowlist checks a remote's url for a
		// helper transport, so the same value here should be judged too.
		"branch remote ext::":     entry("local", "branch.main.remote", "ext::sh -c x"),
		"branch pushremote ext::": entry("local", "branch.main.pushremote", "ext::sh -c x"),
		"branch remote option":    entry("local", "branch.main.remote", "-oProxyCommand=x:y"),
	} {
		if why := RepositoryGitConfig(listing, gate); why == "" {
			t.Errorf("%s: %q stays outside", name, listing)
		}
	}
}

// Item 2: keys a real repository sets stay outside (no false refusal).
func TestRepositoryKeysThatStayOutside(t *testing.T) {
	gate := [][2]string{{"hook.jhon-merge-gate.event", ""}, {"hook.jhon-merge-gate.event", "reference-transaction"}, {"hook.jhon-merge-gate.command", "'/j' hook merge-gate"}, {"hook.jhon-merge-gate.enabled", "true"}}
	for name, listing := range map[string]string{
		"signingkey path":       entry("local", "gpg.format", "ssh") + entry("local", "user.signingkey", "/home/u/keys/signing.pub"),
		"signingkey key::":      entry("local", "gpg.format", "ssh") + entry("local", "user.signingkey", "key::ssh-ed25519 AAAA x"),
		"allowedsignersfile":    entry("local", "gpg.ssh.allowedsignersfile", "/home/u/.config/git/allowed_signers"),
		"hookspath":             entry("local", "core.hookspath", "tools/git-hooks"),
		"gate":                  entry("local", "hook.jhon-merge-gate.event", "reference-transaction") + entry("local", "hook.jhon-merge-gate.command", "'/j' hook merge-gate") + entry("local", "hook.jhon-merge-gate.enabled", "true"),
		"file url":              entry("local", "remote.other.url", "file:///srv/other.git"),
		"path url":              entry("local", "remote.other.url", "/srv/other.git"),
		"fetch to a local head": entry("local", "remote.origin.fetch", "+refs/heads/main:refs/heads/mirror"),
		"no value":              "local\x00remote.origin.url\x00",
		"mixed case subsection": entry("local", "remote.Origin.url", "https://example.invalid/r") + entry("local", "branch.Feature/X.merge", "refs/heads/x"),
	} {
		if why := RepositoryGitConfig(listing, gate); why != "" {
			t.Errorf("%s: %s", name, why)
		}
	}
}

// L40: the git folders an outside git line uses hold no link and no
// hard-linked file git writes, and lie in the project; objects may be hard
// links (git clone --local makes them).
func TestGitFolderProblem(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	proj, other := filepath.Join(base, "p"), filepath.Join(base, "other")
	git := filepath.Join(proj, ".git")
	for _, d := range []string{filepath.Join(git, "objects", "ab"), filepath.Join(git, "refs", "heads"), filepath.Join(git, "logs"), filepath.Join(other, ".git")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"HEAD", "config", "objects/ab/cd", "refs/heads/main"} {
		if err := os.WriteFile(filepath.Join(git, f), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	in := Input{Home: filepath.Join(base, "home"), Roots: []string{proj}, CommonDir: git, Env: map[string]string{}, Stat: Disk, Lstat: DiskLstat}
	if why := in.GitFolderProblem(git, git); why != "" {
		t.Fatalf("a plain git folder: %s", why)
	}
	// An object hard-linked from another repository is fine.
	if err := os.Link(filepath.Join(git, "objects", "ab", "cd"), filepath.Join(other, "cd")); err != nil {
		t.Skipf("no hard links here: %v", err)
	}
	if why := in.GitFolderProblem(git, git); why != "" {
		t.Errorf("a hard-linked object: %s", why)
	}
	// A hard-linked FETCH_HEAD is not.
	if err := os.WriteFile(filepath.Join(other, "bashrc"), []byte("owner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(other, "bashrc"), filepath.Join(git, "FETCH_HEAD")); err != nil {
		t.Fatal(err)
	}
	if why := in.GitFolderProblem(git, git); !strings.Contains(why, "FETCH_HEAD in the repository's git folder shares its data with another file") {
		t.Errorf("a hard-linked FETCH_HEAD: %q", why)
	}
	os.Remove(filepath.Join(git, "FETCH_HEAD"))
	// A link anywhere, objects included, is not.
	for _, l := range []string{"logs/HEAD", "objects/ef"} {
		p := filepath.Join(git, l)
		if err := os.Symlink(other, p); err != nil {
			t.Fatal(err)
		}
		if why := in.GitFolderProblem(git, git); why != p+" in the repository's git folder is a link, which git would write through" {
			t.Errorf("%s a link: %q", l, why)
		}
		os.Remove(p)
	}
	// A git folder outside the project (a .git file naming another
	// repository's) is not the project's.
	if why := in.GitFolderProblem(filepath.Join(other, ".git"), filepath.Join(other, ".git")); why != "the repository's git folder "+filepath.Join(other, ".git")+" is not in the project" {
		t.Errorf("another repository's git folder: %q", why)
	}
}
