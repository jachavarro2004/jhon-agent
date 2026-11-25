package sandbox

// T249 round-4 verify (studio-integration-v1.md L40): the verifier's probes
// kept as regressions, real bubblewrap.

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// Item 1 (L39), real bubblewrap: with sandbox.docker off a container socket
// named through a link, or in a spelling podman accepts, is not reachable.
func TestAContainerSocketNamedThroughALinkOrSpellingIsHidden(t *testing.T) {
	for _, c := range []struct{ name, variable, spell string }{
		{"a link in the home", "DOCKER_HOST", "unix://LINK"},
		{"unix: one slash", "DOCKER_HOST", "unix:SOCK"},
		{"uppercase scheme", "CONTAINER_HOST", "UNIX://SOCK"},
		{"a query string", "CONTAINER_HOST", "unix://SOCK?secure=true"},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newBox(t)
			sock := filepath.Join(b.base, "srv", "engine.sock")
			conns := serveUnix(t, sock)
			link := filepath.Join(b.home, "engine.sock")
			if err := os.Symlink(sock, link); err != nil {
				t.Fatal(err)
			}
			value := strings.NewReplacer("LINK", link, "SOCK", sock).Replace(c.spell)
			b.env = append(b.env, c.variable+"="+value)
			probe := `test -S "` + sock + `" && echo shown || echo hidden`
			if r := b.sh(t, b.proj, "", probe); r.Code != 0 || r.Stdout != "hidden\n" {
				t.Errorf("%s=%s: exit %d, stdout %q, stderr %q; want the socket hidden", c.variable, value, r.Code, r.Stdout, r.Stderr)
			}
			if n := atomic.LoadInt32(conns); n != 0 {
				t.Errorf("a sandboxed command connected %d time(s)", n)
			}
		})
	}
}

// Item 1: a sandboxed command cannot plant a docker context (~/.docker, or
// $DOCKER_CONFIG under ~/.config) for the next run to read.
func TestNoDockerContextCanBePlantedFromInside(t *testing.T) {
	b := newBox(t)
	mkdirs(t, filepath.Join(b.home, ".docker"), filepath.Join(b.home, ".config", "docker"))
	for _, dir := range []string{filepath.Join(b.home, ".docker"), filepath.Join(b.home, ".config", "docker")} {
		r := b.sh(t, b.proj, "", `mkdir -p "`+dir+`/contexts/meta/x" 2>/dev/null && mkfifo "`+dir+`/contexts/meta/x/meta.json" 2>/dev/null && echo planted || echo refused`)
		if r.Stdout != "refused\n" {
			t.Errorf("%s: %q; want the plant refused", dir, r.Stdout)
		}
	}
}

// aheadOrigin is originOverSSH with the remote one commit ahead of the
// project's main, so a pull has something to fast-forward; the ssh note
// (written only by an ssh that runs outside the sandbox) is note.
func aheadOrigin(t *testing.T, b *box, note string) {
	t.Helper()
	remote := originOverSSH(t, b, note)
	support.Git(t, b.proj, "commit", "-q", "--allow-empty", "-m", "ahead")
	support.Git(t, b.proj, "push", "-q", remote, "HEAD:refs/heads/main")
	support.Git(t, b.proj, "reset", "-q", "--hard", "HEAD~1")
}

// Item 2 (L39): a repository config off the allowlist sends the outside
// fetch to the work profile (its ssh cannot write the note outside).
func TestARepositoryConfigOffTheAllowlistRunsTheFetchInside(t *testing.T) {
	for name, plant := range map[string]string{
		"include.path":         `printf '[http]\n\tcookieFile = %s/target\n' "$HOME" > .git/extra && git config include.path extra`,
		"includeIf":            `printf '[user]\n\tname = x\n' > .git/extra && git config includeIf.onbranch:main.path extra`,
		"worktree scope":       `git config extensions.worktreeConfig true && git config --worktree http.cookieFile "$HOME/target"`,
		"duplicate url":        `git config --add remote.origin.url 'ext::sh -c true'`,
		"cannot be listed":     `printf '[remote "origin"\n' >> .git/config`,
		"mixed case insteadOf": `git config 'url.ssh://example.invalid/.insteadOf' 'HTTPS://x/'`,
	} {
		t.Run(name, func(t *testing.T) {
			b := newBox(t)
			note := filepath.Join(b.base, "ssh-ran")
			aheadOrigin(t, &b, note)
			if r := b.sh(t, b.proj, "", plant+` && echo set`); r.Stdout != "set\n" && name != "cannot be listed" {
				t.Fatalf("plant: exit %d, %q, %q", r.Code, r.Stdout, r.Stderr)
			}
			r := b.unsandboxed(t, b.env, "git fetch origin")
			if exists(note) {
				t.Errorf("%s: the fetch ran outside the sandbox (exit %d, stderr %q)", name, r.Code, r.Stderr)
			}
		})
	}
}

// Item 2: allowlisted keys with hostile values: the outside line runs
// outside, and runs no program the repository chose.
func TestAllowlistedKeysRunNoProgramOutside(t *testing.T) {
	for name, plant := range map[string]string{
		"hooksPath":           `mkdir -p hk && for h in reference-transaction post-merge post-checkout pre-auto-gc; do printf '#!/bin/sh\necho %s >> MARKER\n' $h > hk/$h; chmod +x hk/$h; done && git config core.hooksPath "$PWD/hk"`,
		"branch remote ext::": `git config branch.main.remote 'ext::sh -c "echo ext >> MARKER"'`,
		"url ssh host -o":     `git config remote.origin.url 'ssh://-oProxyCommand=echo%20px%20>>MARKER/x'`,
		"url file://":         `git config remote.origin.url "file://OTHER"`,
		"url local path":      `git config remote.origin.url "OTHER"`,
		"url newline":         `git config remote.origin.url "$(printf 'ssh://example.invalid/x\nsh -c y')"`,
	} {
		t.Run(name, func(t *testing.T) {
			b := newBox(t)
			note := filepath.Join(b.base, "ssh-ran")
			aheadOrigin(t, &b, note)
			marker := filepath.Join(b.base, "MARKER")
			other := filepath.Join(b.base, "other.git")
			support.Git(t, b.base, "init", "-q", "--bare", other)
			script(t, filepath.Join(other, "hooks", "post-update"), "#!/bin/sh\necho other >> "+marker+"\n")
			plant = strings.NewReplacer("MARKER", marker, "OTHER", other).Replace(plant)
			if r := b.sh(t, b.proj, "", plant+` && echo set`); r.Stdout != "set\n" {
				t.Fatalf("plant: exit %d, %q, %q", r.Code, r.Stdout, r.Stderr)
			}
			for _, line := range []string{"git fetch origin", "git pull --ff-only"} {
				r := b.unsandboxed(t, b.env, line)
				if exists(marker) {
					t.Errorf("%s: %q ran the repository's program outside: %q (exit %d, stderr %q)", name, line, support.Read(t, marker), r.Code, r.Stderr)
				}
			}
		})
	}
}

// Item 2 (the request: the outside git neither runs a program nor writes
// outside the project): a sandboxed command makes an entry git writes on a
// fetch or pull (FETCH_HEAD, a reflog, a refs folder) a link to a file or
// folder of the home; the next outside line must not write there. The
// reflog line carries user.name, an allowlisted key.
func TestAnOutsideFetchWritesNothingThroughALinkInGit(t *testing.T) {
	for name, c := range map[string]struct{ plant, line, target string }{
		"FETCH_HEAD":          {`ln -s "$HOME/.bashrc" .git/FETCH_HEAD`, "git fetch origin", ".bashrc"},
		"remote reflog":       {`git config user.name 'x; echo injected; #' && mkdir -p .git/logs/refs/remotes/origin && ln -sfn "$HOME/.bashrc" .git/logs/refs/remotes/origin/main`, "git fetch origin", ".bashrc"},
		"HEAD reflog on pull": {`git config user.name 'x; echo injected; #' && ln -sfn "$HOME/.bashrc" .git/logs/HEAD`, "git pull --ff-only", ".bashrc"},
		"refs folder":         {`ln -s "$HOME/.config/systemd/user" .git/refs/remotes`, "git fetch origin", ".config/systemd/user/origin"},
	} {
		t.Run(name, func(t *testing.T) {
			b := newBox(t)
			note := filepath.Join(b.base, "ssh-ran")
			aheadOrigin(t, &b, note)
			support.Write(t, b.home, map[string]string{".bashrc": "owner\n", ".config/systemd/user/keep": "x"})
			if r := b.sh(t, b.proj, "", c.plant+` && echo set`); r.Stdout != "set\n" {
				t.Fatalf("plant: exit %d, %q, %q", r.Code, r.Stdout, r.Stderr)
			}
			r := b.unsandboxed(t, b.env, c.line)
			target := filepath.Join(b.home, c.target)
			if c.target == ".bashrc" {
				if got := support.Read(t, target); got != "owner\n" {
					t.Errorf("%q (ran outside: %v, exit %d) wrote ~/.bashrc through the link: %q", c.line, exists(note), r.Code, got)
				}
			} else if exists(target) {
				t.Errorf("%q (ran outside: %v, exit %d) made %s through the link", c.line, exists(note), r.Code, target)
			}
		})
	}
}

// L40: the owner's flow keeps running outside: a repository with the keys
// the owner's own sets (jhon-agent's .git/config) fetches outside the
// sandbox, from its main checkout and from a linked worktree of it, with no
// note.
func TestTheOwnersRepositoryFetchesOutsideFromItsWorktreesToo(t *testing.T) {
	b := newBox(t)
	note := filepath.Join(b.base, "ssh-ran")
	originOverSSH(t, &b, note)
	for _, kv := range [][2]string{
		{"core.hooksPath", "tools/git-hooks"}, {"gpg.format", "ssh"}, {"user.signingkey", "key::ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA owner"},
		{"commit.gpgsign", "true"}, {"tag.gpgsign", "true"}, {"gpg.ssh.allowedSignersFile", filepath.Join(b.home, ".config", "git", "allowed_signers")},
	} {
		support.Git(t, b.proj, "config", kv[0], kv[1])
	}
	wt := filepath.Join(b.home, "project-wt")
	support.Git(t, b.proj, "worktree", "add", "-q", "-b", "wt", wt)
	for _, dir := range []string{b.proj, wt} {
		os.Remove(note)
		r := support.Jhon(t, dir, b.env, "sandbox", "run", "--project", dir, "--profile", "unsandboxed", "--", realShell(t, "sh"), "-c", "git fetch -q origin")
		if r.Code != 0 || r.Stderr != "" || !exists(note) {
			t.Errorf("%s: exit %d, stderr %q, ran outside %v; want the fetch outside with no note", dir, r.Code, r.Stderr, exists(note))
		}
	}
}

// L40: the line that goes to the sandbox says why.
func TestALinkInTheGitFolderSaysWhyTheLineRunsInside(t *testing.T) {
	b := newBox(t)
	originOverSSH(t, &b, filepath.Join(b.base, "ssh-ran"))
	support.Write(t, b.home, map[string]string{".bashrc": "owner\n"})
	if r := b.sh(t, b.proj, "", `ln -s "$HOME/.bashrc" .git/FETCH_HEAD && echo set`); r.Stdout != "set\n" {
		t.Fatalf("plant: exit %d, %q, %q", r.Code, r.Stdout, r.Stderr)
	}
	r := b.unsandboxed(t, b.env, "git fetch -q origin")
	want := "jhon sandbox: " + filepath.Join(b.proj, ".git", "FETCH_HEAD") + " in the repository's git folder is a link, which git would write through; the line runs in the sandbox instead\n"
	if !strings.HasPrefix(r.Stderr, want) {
		t.Errorf("stderr %q; want it to open with %q", r.Stderr, want)
	}
	if got := support.Read(t, filepath.Join(b.home, ".bashrc")); got != "owner\n" {
		t.Errorf("~/.bashrc is %q", got)
	}
}
