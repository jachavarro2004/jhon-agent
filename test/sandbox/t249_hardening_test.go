package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T249 (studio-integration-v1 L30-L31): what runs outside the sandbox on an
// unsandboxed line takes nothing a sandboxed command could have planted: no
// program from a PATH folder in the project (A1), no command the
// repository's git configuration names (A2); and an allow_write folder that
// holds where a PATH link leads refuses the run (A4). Each planted program
// writes a note outside every folder a sandboxed command may write, so the
// note exists only when it ran outside the sandbox.

// prependPath puts dirs first on the box's PATH.
func prependPath(b *box, dirs ...string) {
	for i, kv := range b.env {
		if path, ok := strings.CutPrefix(kv, "PATH="); ok {
			b.env[i] = "PATH=" + strings.Join(dirs, ":") + ":" + path
		}
	}
}

// A1: a deploy tool that calls helper by its name, outside the sandbox,
// finds the owner's helper, never one a sandboxed command planted in a
// PATH folder inside the project.
func TestAProgramPlantedInAProjectPATHFolderIsNotWhatADeployToolRuns(t *testing.T) {
	b := newBox(t)
	deployBin, ownerBin, projBin := filepath.Join(b.base, "deploy-bin"), filepath.Join(b.base, "owner-bin"), filepath.Join(b.proj, "bin")
	planted, owners, deployed := filepath.Join(b.base, "planted-ran"), filepath.Join(b.base, "owner-ran"), filepath.Join(b.base, "deploy-ran")
	script(t, filepath.Join(deployBin, "mydeploy"), "#!/bin/sh\necho deployed >> "+deployed+"\nhelper\n")
	script(t, filepath.Join(ownerBin, "helper"), "#!/bin/sh\necho owner >> "+owners+"\n")
	// What a sandboxed command wrote in its project.
	script(t, filepath.Join(projBin, "helper"), "#!/bin/sh\necho planted >> "+planted+"\n")
	prependPath(&b, deployBin, projBin, ownerBin)
	b.settings(t, "schema = 1\n\n[sandbox]\ndeploy_tools = [\"mydeploy\"]\n")
	r := b.unsandboxed(t, b.env, "mydeploy up")
	if !exists(deployed) {
		t.Fatalf("mydeploy did not run outside the sandbox: exit %d, stderr %q", r.Code, r.Stderr)
	}
	if exists(planted) {
		t.Errorf("the deploy tool ran the helper planted in the project's PATH folder outside the sandbox (exit %d, stderr %q)", r.Code, r.Stderr)
	}
	if r.Code != 0 || !exists(owners) {
		t.Errorf("exit %d, stderr %q, the owner's helper ran %v; want 0 and the owner's helper", r.Code, r.Stderr, exists(owners))
	}
}

// A2: core.gitProxy in the project's .git/config, which a sandboxed command
// may write, runs no program on an unsandboxed git line.
func TestTheRepositorysGitProxyDoesNotRunOutsideTheSandbox(t *testing.T) {
	b := newBox(t)
	ran := filepath.Join(b.base, "proxy-ran")
	script(t, filepath.Join(b.proj, "proxy.sh"), "#!/bin/sh\necho proxy >> "+ran+"\n")
	support.Git(t, b.proj, "config", "core.gitProxy", filepath.Join(b.proj, "proxy.sh"))
	r := b.unsandboxed(t, b.env, "git ls-remote git://127.0.0.1:9/x")
	if exists(ran) {
		t.Errorf("the repository's core.gitProxy ran outside the sandbox (exit %d, stderr %q)", r.Code, r.Stderr)
	}
}

// pullable is the project one commit behind its origin over ssh, the newer
// commit adding a.txt with a filter attribute for it.
func pullable(t *testing.T, b *box) {
	t.Helper()
	originOverSSH(t, b, filepath.Join(b.base, "ssh-ran"))
	support.Write(t, b.proj, map[string]string{".gitattributes": "*.txt filter=evil\n", "a.txt": "content\n"})
	support.Git(t, b.proj, "add", ".gitattributes", "a.txt")
	support.Git(t, b.proj, "commit", "-q", "-m", "a")
	support.Git(t, b.proj, "push", "-q", filepath.Join(b.base, "remote.git"), "HEAD:refs/heads/main")
	support.Git(t, b.proj, "reset", "-q", "--hard", "HEAD~1")
}

// A2: a filter the repository's configuration defines runs no program when
// an unsandboxed pull checks out a file that names it; the pull itself
// still works.
func TestTheRepositorysFilterDoesNotRunOnAnUnsandboxedPull(t *testing.T) {
	b := newBox(t)
	pullable(t, &b)
	ran := filepath.Join(b.base, "filter-ran")
	script(t, filepath.Join(b.proj, ".git", "evil.sh"), "#!/bin/sh\necho filter >> "+ran+"\ncat\n")
	support.Git(t, b.proj, "config", "filter.evil.smudge", filepath.Join(b.proj, ".git", "evil.sh"))
	r := b.unsandboxed(t, b.env, "git pull -q --ff-only")
	if exists(ran) {
		t.Errorf("the repository's filter ran outside the sandbox (exit %d, stderr %q)", r.Code, r.Stderr)
	}
	if r.Code != 0 || support.Read(t, filepath.Join(b.proj, "a.txt")) != "content\n" {
		t.Errorf("the pull: exit %d, stderr %q; want it done with a.txt checked out", r.Code, r.Stderr)
	}
}

// A2: the repository's core.worktree would move what an unsandboxed pull
// checks out outside the project: such a line runs in the sandbox, where
// the checkout cannot reach the folder it names.
func TestTheRepositorysWorktreeKeepsAPullInTheSandbox(t *testing.T) {
	b := newBox(t)
	pullable(t, &b)
	victim := filepath.Join(b.base, "victim")
	mkdirs(t, victim)
	support.Git(t, b.proj, "config", "core.worktree", victim)
	r := b.unsandboxed(t, b.env, "git pull -q --ff-only")
	if exists(filepath.Join(victim, "a.txt")) {
		t.Errorf("an unsandboxed pull checked a.txt out into the folder core.worktree names (exit %d, stderr %q)", r.Code, r.Stderr)
	}
}

// A4: ~/.local/bin/claude, on PATH, leads into ~/.local/share/claude: an
// allow_write that holds that folder refuses the run as one that holds a
// PATH folder does; one beside it is accepted.
func TestAnAllowWriteHoldingWhereAPATHLinkLeadsRefusesTheRun(t *testing.T) {
	b := newBox(t)
	bin, versions := filepath.Join(b.home, ".local", "bin"), filepath.Join(b.home, ".local", "share", "claude", "versions")
	other := filepath.Join(b.home, ".local", "share", "other")
	mkdirs(t, bin, versions, other)
	script(t, filepath.Join(versions, "2.1.0"), "#!/bin/sh\n")
	if err := os.Symlink(filepath.Join(versions, "2.1.0"), filepath.Join(bin, "claude")); err != nil {
		t.Fatal(err)
	}
	prependPath(&b, bin)
	b.settings(t, "schema = 1\n\n[sandbox]\nallow_write = [\"~/.local/share/claude\"]\n")
	r := b.sh(t, b.proj, "", "echo ran")
	if r.Code != 126 || r.Stdout != "" || !strings.Contains(r.Stderr, "sandbox.allow_write folder "+filepath.Join(b.home, ".local", "share", "claude")+" holds "+versions+", where the program "+filepath.Join(bin, "claude")+" on PATH leads") {
		t.Errorf("allow_write ~/.local/share/claude: exit %d, stdout %q, stderr %q; want the run refused (126) for where claude leads", r.Code, r.Stdout, r.Stderr)
	}
	b.settings(t, "schema = 1\n\n[sandbox]\nallow_write = [\"~/.local/share/other\"]\n")
	if r := b.sh(t, b.proj, "", "echo ran > ~/.local/share/other/x && echo ok"); r.Code != 0 || r.Stdout != "ok\n" {
		t.Errorf("allow_write ~/.local/share/other: exit %d, stdout %q, stderr %q; want it accepted and writable", r.Code, r.Stdout, r.Stderr)
	}
}
