package release

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// publishFixture is a source checkout whose HEAD is the active release, with
// a local bare repository as origin and a committed, clean
// tools/git-hooks/pre-push that core.hooksPath names (as the owner's
// jhon-agent checkout has it), whose run leaves the marker "committed-hook".
// markers is the folder every planted program writes its marker in.
func publishFixture(t *testing.T) (r Roots, git func(...string) string, markers string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("skipped: no git")
	}
	base := t.TempDir()
	markers = filepath.Join(base, "markers")
	src, remote := filepath.Join(base, "src"), filepath.Join(base, "origin.git")
	for _, d := range []string{markers, filepath.Join(src, "engine", "cmd", "jhon"), filepath.Join(src, "tools", "git-hooks")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env := append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	git = func(args ...string) string {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", src, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)...)
		c.Env = env
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("git", "init", "-q", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	git("init", "-q")
	write := func(rel, body string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(src, rel), []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("engine/cmd/jhon/main.go", "package main\n\nfunc main() {}\n", 0o644)
	write("tools/git-hooks/pre-push", "#!/bin/sh\ntouch '"+filepath.Join(markers, "committed-hook")+"'\n", 0o755)
	git("add", "-A")
	git("commit", "-q", "-m", "one")
	git("config", "core.hooksPath", "tools/git-hooks")
	git("remote", "add", "origin", remote)
	commit := git("rev-parse", "HEAD")
	share := filepath.Join(base, "share")
	if err := os.MkdirAll(filepath.Join(share, "releases", commit), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(share, "releases", commit), filepath.Join(share, "current")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	return Roots{Source: src, Share: share}, git, markers
}

// planted is a program that leaves the marker name in markers.
func planted(t *testing.T, markers, name string) string {
	t.Helper()
	p := filepath.Join(markers, "bin", name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!/bin/sh\ntouch '"+filepath.Join(markers, name)+"'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func publish(r Roots) (code int, out string) {
	var b bytes.Buffer
	code = Publish(r, &Out{W: &b, E: &b})
	return code, b.String()
}

// T250 (the re-verify of 5e9b12e, blocker 2): `jhon publish` runs outside
// the sandbox, so its push runs no program the repository names, which a
// sandboxed command may have written: a hook runs only when it is committed
// code (core.hooksPath in the repository, its hook files tracked and
// unmodified), and any other hook makes publish refuse, naming the key, its
// origin and the fix; the repository's ssh command, receive-pack program,
// credential helper and configured hooks are neutralised.
func TestPublishPushRunsNoProgramTheRepositoryNames(t *testing.T) {
	t.Run("a hooks folder no commit holds", func(t *testing.T) {
		r, git, markers := publishFixture(t)
		hooks := filepath.Join(markers, "hooks")
		if err := os.MkdirAll(hooks, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(hooks, "pre-push"), []byte("#!/bin/sh\ntouch '"+filepath.Join(markers, "planted-hook")+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		git("config", "core.hooksPath", hooks)
		git("config", "core.sshCommand", planted(t, markers, "planted-ssh"))
		code, out := publish(r)
		if code == 0 || exists(filepath.Join(markers, "planted-hook")) || exists(filepath.Join(markers, "planted-ssh")) {
			t.Errorf("exit %d, %q; want refused and nothing planted run", code, out)
		}
		want := "jhon: core.hooksPath = " + hooks + " (file:.git/config) names " + hooks + ", which is not a folder of committed hooks in the repository, so its pre-push is not committed code a sandboxed command could not have written; nothing was pushed; fix: unset it (git config --unset core.hooksPath) or point it at a folder of hooks the repository tracks, unmodified\n"
		if !strings.Contains(out, want) {
			t.Errorf("output %q\nwant %q", out, want)
		}
		if got := git("ls-remote", "origin"); got != "" {
			t.Errorf("something was pushed: %q", got)
		}
	})
	t.Run("a changed committed hook", func(t *testing.T) {
		r, git, markers := publishFixture(t)
		hook := filepath.Join(r.Source, "tools", "git-hooks", "pre-push")
		if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch '"+filepath.Join(markers, "planted-hook")+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		git("update-index", "--assume-unchanged", "tools/git-hooks/pre-push")
		code, out := publish(r)
		if code == 0 || exists(filepath.Join(markers, "planted-hook")) || !strings.Contains(out, "core.hooksPath = tools/git-hooks (file:.git/config) names "+filepath.Join(r.Source, "tools", "git-hooks")+", whose pre-push differs from the one the repository tracks") {
			t.Errorf("exit %d, %q; want refused, naming the changed hook", code, out)
		}
	})
	t.Run("a hook in the git folder", func(t *testing.T) {
		r, git, markers := publishFixture(t)
		git("config", "--unset", "core.hooksPath")
		hook := filepath.Join(r.Source, ".git", "hooks", "pre-push")
		if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch '"+filepath.Join(markers, "planted-hook")+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		code, out := publish(r)
		if code == 0 || exists(filepath.Join(markers, "planted-hook")) || !strings.Contains(out, "jhon: "+hook+" is a hook in the git folder, which no commit holds") {
			t.Errorf("exit %d, %q; want refused, naming the hook", code, out)
		}
	})
	t.Run("other programs the configuration names", func(t *testing.T) {
		r, git, markers := publishFixture(t)
		git("config", "remote.origin.receivepack", planted(t, markers, "planted-receivepack"))
		git("config", "credential.helper", planted(t, markers, "planted-helper"))
		git("config", "hook.p.command", planted(t, markers, "planted-config-hook"))
		git("config", "hook.p.event", "pre-push")
		git("config", "core.fsmonitor", planted(t, markers, "planted-fsmonitor"))
		code, out := publish(r)
		for _, m := range []string{"planted-receivepack", "planted-helper", "planted-config-hook", "planted-fsmonitor"} {
			if exists(filepath.Join(markers, m)) {
				t.Errorf("%s ran (exit %d, %q)", m, code, out)
			}
		}
		if code != 0 || !exists(filepath.Join(markers, "committed-hook")) {
			t.Errorf("exit %d, %q; want pushed, with the committed hook run", code, out)
		}
	})
	// The repository's ssh command never runs; the owner's own, from his
	// global configuration, does; with none, ssh itself.
	t.Run("the ssh command", func(t *testing.T) {
		r, git, markers := publishFixture(t)
		git("remote", "set-url", "origin", "ssh://git@example.invalid/x.git")
		git("config", "core.sshCommand", planted(t, markers, "planted-ssh"))
		planted(t, markers, "ssh")
		t.Setenv("PATH", filepath.Join(markers, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
		publish(r)
		if exists(filepath.Join(markers, "planted-ssh")) || !exists(filepath.Join(markers, "ssh")) {
			t.Errorf("the repository's ssh command ran %v, ssh itself %v; want ssh itself only", exists(filepath.Join(markers, "planted-ssh")), exists(filepath.Join(markers, "ssh")))
		}
		global := filepath.Join(markers, "gitconfig")
		if err := os.WriteFile(global, []byte("[core]\n\tsshCommand = "+planted(t, markers, "owner-ssh")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GIT_CONFIG_GLOBAL", global)
		publish(r)
		if exists(filepath.Join(markers, "planted-ssh")) || !exists(filepath.Join(markers, "owner-ssh")) {
			t.Errorf("with the owner's own core.sshCommand: the repository's ran %v, the owner's %v", exists(filepath.Join(markers, "planted-ssh")), exists(filepath.Join(markers, "owner-ssh")))
		}
	})
	// Preserved: the committed, clean pre-push runs, and the push lands.
	t.Run("the committed hook", func(t *testing.T) {
		r, git, markers := publishFixture(t)
		code, out := publish(r)
		if code != 0 || !exists(filepath.Join(markers, "committed-hook")) || git("ls-remote", "origin", "refs/heads/main") == "" {
			t.Errorf("exit %d, %q; want pushed, with the committed hook run", code, out)
		}
	})
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}
