package release

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// publishBox is a source checkout whose HEAD is the active release, shaped
// as the owner's: origin is git@github.com:jachavarro2004/jhon-agent.git,
// which his global configuration rewrites to git@github-personal:… (a
// url.insteadOf, here in a scratch global file), and core.hooksPath names a
// committed, clean tools/git-hooks/pre-push, which leaves the marker
// "committed-hook" holding the refs git gives it on stdin. GitHub is a
// stand-in: an `ssh` first on PATH that records the host it was asked for
// and serves a local bare repository (remote), as sshd would.
type publishBox struct {
	r                        Roots
	src, remote, markers     string
	global, commit, sshHosts string
}

func newPublishBox(t *testing.T) publishBox {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("skipped: no git")
	}
	base := t.TempDir()
	b := publishBox{src: filepath.Join(base, "src"), remote: filepath.Join(base, "github.git"), markers: filepath.Join(base, "markers"), global: filepath.Join(base, "gitconfig")}
	b.sshHosts = filepath.Join(b.markers, "ssh-hosts")
	for _, d := range []string{filepath.Join(b.markers, "bin"), filepath.Join(b.src, "engine", "cmd", "jhon"), filepath.Join(b.src, "tools", "git-hooks")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(b.global, []byte("[url \"git@github-personal:jachavarro2004/\"]\n\tinsteadOf = git@github.com:jachavarro2004/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", b.global)
	stand := "#!/bin/sh\n# GitHub's stand-in: the host, then the program git asks the far end to run.\nfor a; do last=$a; done\nfor a; do case $a in -*) ;; *) echo \"$a\" >> '" + b.sshHosts + "'; break ;; esac; done\n" +
		"case $last in git-upload-pack*) exec git-upload-pack '" + b.remote + "' ;; git-receive-pack*) exec git-receive-pack '" + b.remote + "' ;; esac\nexit 1\n"
	if err := os.WriteFile(filepath.Join(b.markers, "bin", "ssh"), []byte(stand), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Join(b.markers, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, err := exec.Command("git", "init", "-q", "--bare", b.remote).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	b.git("init", "-q")
	b.write("engine/cmd/jhon/main.go", "package main\n\nfunc main() {}\n", 0o755)
	b.write("tools/git-hooks/pre-push", "#!/bin/sh\ncat > '"+filepath.Join(b.markers, "committed-hook")+"'\n", 0o755)
	b.git("add", "-A")
	b.git("commit", "-q", "-m", "one")
	b.git("config", "core.hooksPath", "tools/git-hooks")
	b.git("remote", "add", "origin", "git@github.com:jachavarro2004/jhon-agent.git")
	b.commit = b.git("rev-parse", "HEAD")
	share := filepath.Join(base, "share")
	if err := os.MkdirAll(filepath.Join(share, "releases", b.commit), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(share, "releases", b.commit), filepath.Join(share, "current")); err != nil {
		t.Fatal(err)
	}
	b.r = Roots{Source: b.src, Share: share}
	return b
}

func (b publishBox) git(args ...string) string {
	c := exec.Command("git", append([]string{"-C", b.src, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)...)
	out, err := c.CombinedOutput()
	if err != nil {
		panic("git " + strings.Join(args, " ") + ": " + err.Error() + " " + string(out))
	}
	return strings.TrimSpace(string(out))
}

func (b publishBox) write(rel, body string, mode os.FileMode) {
	if err := os.WriteFile(filepath.Join(b.src, rel), []byte(body), mode); err != nil {
		panic(err)
	}
}

// pushed is the commit the stand-in's main holds, "" for none.
func (b publishBox) pushed() string {
	out, _ := exec.Command("git", "-C", b.remote, "rev-parse", "--verify", "--quiet", "refs/heads/main").Output()
	return strings.TrimSpace(string(out))
}

// plant is a program that leaves the marker name and fails.
func (b publishBox) plant(name string) string {
	p := filepath.Join(b.markers, "plants", name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(p, []byte("#!/bin/sh\ntouch '"+filepath.Join(b.markers, name)+"'\nexit 1\n"), 0o755); err != nil {
		panic(err)
	}
	return p
}

func (b publishBox) ran(name string) bool { return exists(filepath.Join(b.markers, name)) }

func publish(r Roots) (code int, out string) {
	var buf bytes.Buffer
	code = Publish(r, &Out{W: &buf, E: &buf})
	return code, buf.String()
}

// T250 (the re-verifies of 5e9b12e and 18deae4, blocker 2): `jhon publish`
// runs outside the sandbox, and a sandboxed command can write the source's
// .git/config, so the push never runs with the source's configuration: it
// pushes from a private repository of jhon's own (the source's objects
// reached through alternates, its config jhon's, with the owner's system
// and global configuration), to the URL remote.<name>.url names as the
// owner's own configuration rewrites it, refused where the repository's
// pushurl or insteadOf would change it, or where it is local, file://,
// git:// or a transport helper. A hook runs only when it is committed code,
// and jhon runs it itself.
func TestPublishPushRunsNoProgramTheRepositoryNames(t *testing.T) {
	t.Run("a hooks folder no commit holds", func(t *testing.T) {
		b := newPublishBox(t)
		hooks := filepath.Join(b.markers, "hooks")
		if err := os.MkdirAll(hooks, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(hooks, "pre-push"), []byte("#!/bin/sh\ntouch '"+filepath.Join(b.markers, "planted-hook")+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		b.git("config", "core.hooksPath", hooks)
		code, out := publish(b.r)
		want := "jhon: core.hooksPath = " + hooks + " (file:.git/config) names " + hooks + ", which is not a folder of committed hooks in the repository, so its pre-push is not committed code a sandboxed command could not have written; nothing was pushed; fix: unset it (git config --unset core.hooksPath) or point it at a folder of hooks the repository tracks, unmodified\n"
		if code == 0 || b.ran("planted-hook") || !strings.Contains(out, want) || b.pushed() != "" {
			t.Errorf("exit %d, %q; want refused with %q, nothing run or pushed", code, out, want)
		}
	})
	t.Run("a changed committed hook", func(t *testing.T) {
		b := newPublishBox(t)
		b.write("tools/git-hooks/pre-push", "#!/bin/sh\ntouch '"+filepath.Join(b.markers, "planted-hook")+"'\n", 0o755)
		b.git("update-index", "--assume-unchanged", "tools/git-hooks/pre-push")
		code, out := publish(b.r)
		if code == 0 || b.ran("planted-hook") || !strings.Contains(out, "core.hooksPath = tools/git-hooks (file:.git/config) names "+filepath.Join(b.src, "tools", "git-hooks")+", whose pre-push differs from the one the repository tracks") || b.pushed() != "" {
			t.Errorf("exit %d, %q; want refused, naming the changed hook", code, out)
		}
	})
	t.Run("a hook in the git folder", func(t *testing.T) {
		b := newPublishBox(t)
		b.git("config", "--unset", "core.hooksPath")
		hook := filepath.Join(b.src, ".git", "hooks", "pre-push")
		if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch '"+filepath.Join(b.markers, "planted-hook")+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		code, out := publish(b.r)
		if code == 0 || b.ran("planted-hook") || !strings.Contains(out, "jhon: "+hook+" is a hook in the git folder, which no commit holds") || b.pushed() != "" {
			t.Errorf("exit %d, %q; want refused, naming the hook", code, out)
		}
	})
	t.Run("programs the configuration names", func(t *testing.T) {
		b := newPublishBox(t)
		for _, kv := range [][2]string{
			{"remote.origin.receivepack", b.plant("receivepack")},
			{"credential.helper", b.plant("helper")},
			{"hook.p.command", b.plant("config-hook")}, {"hook.p.event", "pre-push"},
			{"core.fsmonitor", b.plant("fsmonitor")},
			{"core.sshCommand", b.plant("ssh-command")},
			{"core.gitProxy", b.plant("git-proxy")},
			{"push.gpgSign", "true"}, {"gpg.program", b.plant("gpg")},
			{"core.askPass", b.plant("askpass")},
		} {
			b.git("config", kv[0], kv[1])
		}
		// The stand-in asks for signed pushes, as a remote that offers push
		// certificates does.
		if out, err := exec.Command("git", "-C", b.remote, "config", "receive.certNonceSeed", "seed").CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		code, out := publish(b.r)
		for _, m := range []string{"receivepack", "helper", "config-hook", "fsmonitor", "ssh-command", "git-proxy", "gpg", "askpass"} {
			if b.ran(m) {
				t.Errorf("the repository's %s ran (exit %d, %q)", m, code, out)
			}
		}
		if code != 0 || b.pushed() != b.commit || !b.ran("committed-hook") {
			t.Errorf("exit %d, %q; want pushed, with the committed hook run", code, out)
		}
	})
	// The target is remote.<name>.url as the owner's configuration rewrites
	// it: the repository may not change it, nor make it local, file://,
	// git:// or a transport helper, where the far end runs the repository's
	// programs or no remote is reached at all.
	for _, c := range []struct {
		name  string
		keys  [][2]string
		taken string
	}{
		{"a pushurl to git:// through core.gitProxy", [][2]string{{"remote.origin.pushurl", "git://example.invalid/x.git"}, {"core.gitProxy", "gitproxy"}}, "remote.origin.pushurl = git://example.invalid/x.git (file:.git/config) would change where origin is pushed"},
		{"a pushInsteadOf to git://", [][2]string{{"url.git://example.invalid/.pushInsteadOf", "git@github.com:"}, {"core.gitProxy", "gitproxy"}}, "url.git://example.invalid/.pushinsteadof = git@github.com: (file:.git/config) would change where origin is pushed"},
		{"an insteadOf to a local repository", [][2]string{{"url.LOCAL.insteadOf", "git@github.com:jachavarro2004/jhon-agent.git"}}, "url.LOCAL.insteadof = git@github.com:jachavarro2004/jhon-agent.git (file:.git/config) would change where origin is pushed"},
		{"a pushurl to a local repository", [][2]string{{"remote.origin.pushurl", "LOCAL"}}, "remote.origin.pushurl = LOCAL (file:.git/config) would change where origin is pushed"},
		{"a url to a local repository", [][2]string{{"remote.origin.url", "LOCAL"}}, "origin is LOCAL, a repository on this machine, where git runs that repository's programs (core.alternateRefsCommand, its hooks) and reaches no remote"},
		{"a file:// url", [][2]string{{"remote.origin.url", "file://LOCAL"}}, "origin is file://LOCAL, a repository on this machine"},
		{"a git:// url", [][2]string{{"remote.origin.url", "git://example.invalid/x.git"}}, "origin is git://example.invalid/x.git, a git:// remote, which core.gitProxy reaches through a program"},
		{"a transport helper", [][2]string{{"remote.origin.url", "ext::sh -c touch% GITPROXY"}}, "origin is ext::sh -c touch% GITPROXY, a transport helper's (a program git runs)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newPublishBox(t)
			attacker := filepath.Join(b.markers, "attacker.git")
			if out, err := exec.Command("git", "init", "-q", "--bare", attacker).CombinedOutput(); err != nil {
				t.Fatalf("%v %s", err, out)
			}
			if out, err := exec.Command("git", "-C", attacker, "config", "core.alternateRefsCommand", b.plant("alternate-refs")).CombinedOutput(); err != nil {
				t.Fatalf("%v %s", err, out)
			}
			fill := strings.NewReplacer("LOCAL", attacker, "gitproxy", b.plant("git-proxy"), "GITPROXY", filepath.Join(b.markers, "git-proxy"))
			for _, kv := range c.keys {
				b.git("config", fill.Replace(kv[0]), fill.Replace(kv[1]))
			}
			code, out := publish(b.r)
			for _, m := range []string{"git-proxy", "alternate-refs"} {
				if b.ran(m) {
					t.Errorf("%s ran (exit %d, %q)", m, code, out)
				}
			}
			want := "jhon: " + fill.Replace(c.taken)
			if code == 0 || !strings.Contains(out, want) || !strings.Contains(out, "; nothing was pushed; fix: ") || b.pushed() != "" || b.ran("committed-hook") {
				t.Errorf("exit %d, %q; want refused before the hook with %q", code, out, want)
			}
			if refs, _ := exec.Command("git", "-C", attacker, "for-each-ref").Output(); len(refs) > 0 {
				t.Errorf("pushed to the attacker's repository: %s", refs)
			}
		})
	}
	// The owner's own ssh command, from his global configuration, is used.
	t.Run("the owner's ssh command", func(t *testing.T) {
		b := newPublishBox(t)
		b.git("config", "core.sshCommand", b.plant("ssh-command"))
		owner := filepath.Join(b.markers, "bin", "owner-ssh")
		stand, _ := os.ReadFile(filepath.Join(b.markers, "bin", "ssh"))
		if err := os.WriteFile(owner, []byte(strings.Replace(string(stand), "#!/bin/sh\n", "#!/bin/sh\ntouch '"+filepath.Join(b.markers, "owner-ssh")+"'\n", 1)), 0o755); err != nil {
			t.Fatal(err)
		}
		f, _ := os.OpenFile(b.global, os.O_APPEND|os.O_WRONLY, 0)
		f.WriteString("[core]\n\tsshCommand = " + owner + "\n")
		f.Close()
		code, out := publish(b.r)
		if code != 0 || b.ran("ssh-command") || !b.ran("owner-ssh") || b.pushed() != b.commit {
			t.Errorf("exit %d, %q; the repository's ran %v, the owner's %v", code, out, b.ran("ssh-command"), b.ran("owner-ssh"))
		}
	})
	// Preserved, the owner's shape: his committed pre-push runs, in the
	// source's work tree, with git's stdin protocol, before anything is
	// pushed; the push reaches github-personal (his global insteadOf); the
	// source's origin/main follows, as after git push.
	t.Run("the committed hook and the owner's remote", func(t *testing.T) {
		b := newPublishBox(t)
		code, out := publish(b.r)
		hook, _ := os.ReadFile(filepath.Join(b.markers, "committed-hook"))
		if code != 0 || b.pushed() != b.commit {
			t.Fatalf("exit %d, %q; want pushed", code, out)
		}
		if want := "refs/heads/main " + b.commit + " refs/heads/main " + strings.Repeat("0", 40) + "\n"; string(hook) != want {
			t.Errorf("the hook read %q, want %q", hook, want)
		}
		if hosts, _ := os.ReadFile(b.sshHosts); !strings.Contains(string(hosts), "git@github-personal") {
			t.Errorf("ssh was asked for %q, want git@github-personal", hosts)
		}
		if got := b.git("rev-parse", "refs/remotes/origin/main"); got != b.commit {
			t.Errorf("origin/main is %s, want %s", got, b.commit)
		}
		// A failing committed hook pushes nothing.
		b.write("engine/cmd/jhon/main.go", "package main\n\nfunc main() { _ = 1 }\n", 0o644)
		b.write("tools/git-hooks/pre-push", "#!/bin/sh\necho no >&2\nexit 1\n", 0o755)
		b.git("commit", "-qam", "two")
		two := b.git("rev-parse", "HEAD")
		os.Remove(filepath.Join(b.r.Share, "current"))
		os.MkdirAll(filepath.Join(b.r.Share, "releases", two), 0o755)
		os.Symlink(filepath.Join(b.r.Share, "releases", two), filepath.Join(b.r.Share, "current"))
		code, out = publish(b.r)
		if code == 0 || b.pushed() != b.commit || !strings.Contains(out, "jhon: the committed pre-push hook refused the push to origin; nothing was pushed") {
			t.Errorf("a failing hook: exit %d, %q; want refused, the remote still at %s", code, out, b.commit)
		}
	})
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// The re-verify of 18deae4 (mwy7imke): a writable .git/config chose the
// push's transport and signing. Each plant: core.gitProxy with a git://
// pushurl or pushInsteadOf, a local pushurl to a bare repository a sandboxed
// command wrote (whose core.alternateRefsCommand git runs there, and which
// took the push with exit 0), and push.gpgSign with gpg.program against a
// remote that asks for push certificates. None of their programs runs, and
// the local and git:// targets refuse the publish.
func TestPushCmdRunsNoProxySignerOrLocalReceivePackTheRepositoryNames(t *testing.T) {
	for _, c := range []struct {
		name   string
		keys   func(b publishBox, attacker string) [][2]string
		refuse bool
	}{
		{"gitProxy, git:// pushurl", func(b publishBox, _ string) [][2]string {
			return [][2]string{{"core.gitProxy", b.plant("git-proxy")}, {"remote.origin.pushurl", "git://example.invalid/x.git"}}
		}, true},
		{"gitProxy, git:// pushInsteadOf", func(b publishBox, _ string) [][2]string {
			return [][2]string{{"core.gitProxy", b.plant("git-proxy")}, {"url.git://example.invalid/.pushInsteadOf", "git@github.com:"}}
		}, true},
		{"a local pushurl with alternateRefsCommand", func(b publishBox, attacker string) [][2]string {
			return [][2]string{{"remote.origin.pushurl", attacker}}
		}, true},
		{"push.gpgSign and gpg.program", func(b publishBox, _ string) [][2]string {
			return [][2]string{{"push.gpgSign", "true"}, {"gpg.program", b.plant("gpg")}}
		}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newPublishBox(t)
			attacker := filepath.Join(b.markers, "attacker.git")
			for _, args := range [][]string{{"init", "-q", "--bare", attacker}, {"-C", attacker, "config", "core.alternateRefsCommand", b.plant("alternate-refs")}, {"-C", b.remote, "config", "receive.certNonceSeed", "seed"}} {
				if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
					t.Fatalf("%v %s", err, out)
				}
			}
			for _, kv := range c.keys(b, attacker) {
				b.git("config", kv[0], kv[1])
			}
			code, out := publish(b.r)
			for _, m := range []string{"git-proxy", "alternate-refs", "gpg"} {
				if b.ran(m) {
					t.Errorf("%s ran (exit %d, %q)", m, code, out)
				}
			}
			if refs, _ := exec.Command("git", "-C", attacker, "for-each-ref").Output(); len(refs) > 0 {
				t.Errorf("pushed to the attacker's repository: %s", refs)
			}
			if c.refuse && (code == 0 || !strings.Contains(out, "; nothing was pushed; fix: ") || b.pushed() != "") {
				t.Errorf("exit %d, %q; want refused, nothing pushed", code, out)
			}
			if !c.refuse && (code != 0 || b.pushed() != b.commit) {
				t.Errorf("exit %d, %q; want pushed, unsigned", code, out)
			}
		})
	}
}
