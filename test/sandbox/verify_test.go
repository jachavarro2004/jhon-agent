// The corrections of the first independent verify (T128a, L888,
// odd/proposals/t128a-verify-1.md, F1 to F7): what a sandboxed command wrote
// decides nothing about the next start, the delivery variables stay out of a
// work command, and the snapshots and the session's sockets are not there.
package sandbox

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// mkdirs makes each folder.
func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// script writes an executable shell script.
func script(t *testing.T, path, text string) {
	t.Helper()
	support.Write(t, filepath.Dir(path), map[string]string{filepath.Base(path): text})
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// onlyBwrap is the box's environment with a PATH of one folder, outside
// every writable area, that holds bubblewrap and nothing else: no git, no
// other program Jhon could start.
func onlyBwrap(t *testing.T, b box) []string {
	t.Helper()
	real, err := exec.LookPath("bwrap")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(b.base, "bwrap-bin")
	mkdirs(t, bin)
	if err := os.Symlink(real, filepath.Join(bin, "bwrap")); err != nil {
		t.Fatal(err)
	}
	return append(slices.Clone(b.env), "PATH="+bin)
}

// readOnly is whether r is a write the sandbox refused.
func readOnly(r support.Result) bool {
	return r.Code != 0 && strings.Contains(r.Stderr, "Read-only file system")
}

// bubblewrap is the one Jhon trusts, never one a sandboxed command planted on
// PATH: a writable tool folder sits before /usr/bin on a normal PATH, so a
// PATH lookup would run the plant outside the sandbox and the wrapped program
// never at all (T128a, B1). The owner opened mise's folder here: by default
// it is no longer writable (F2).
func TestBubblewrapIsNotTakenFromAWritablePATHFolder(t *testing.T) {
	b := newBox(t)
	b.settings(t, "schema = 1\n\n[sandbox]\nallow_write = [\"~/.local/share/mise\"]\n")
	shims := filepath.Join(b.home, ".local", "share", "mise", "shims")
	escaped := filepath.Join(b.base, "escaped")
	script(t, filepath.Join(shims, "bwrap"), "#!/bin/sh\necho I-AM-OUTSIDE > "+escaped+"\n")
	env := append(slices.Clone(b.env), "PATH="+shims+":"+os.Getenv("PATH"))
	r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", "echo ran")
	if exists(escaped) {
		t.Fatalf("the planted bwrap ran outside the sandbox (%s exists); exit %d, stdout %q", escaped, r.Code, r.Stdout)
	}
	if r.Code != 0 || r.Stdout != "ran\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q; want 0 and the program's output", r.Code, r.Stdout, r.Stderr)
	}
}

// Where bubblewrap is only in a folder a sandboxed command may write (the
// scratch root, the project), nothing runs: run refuses on its one line, and
// status and doctor say it cannot run and why, without starting it (F1).
func TestBubblewrapOnlyInAWritableFolderRunsNothing(t *testing.T) {
	b := newBox(t)
	// The mode off: a warning (T128f: on, by default on Linux, is a problem).
	b.settings(t, "[sandbox]\nmode = \"off\"\n")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	tools := filepath.Join(b.base, "tools")
	mkdirs(t, tools)
	if err := os.Symlink(git, filepath.Join(tools, "git")); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, bin string }{
		{"the scratch root", filepath.Join(b.tmp, "bin")},
		{"the project", filepath.Join(b.proj, "bin")},
	} {
		t.Run(c.name, func(t *testing.T) {
			escaped := filepath.Join(b.base, "escaped")
			script(t, filepath.Join(c.bin, "bwrap"), "#!/bin/sh\necho I-AM-OUTSIDE > "+escaped+"\n")
			env := append(slices.Clone(b.env), "PATH="+c.bin+":"+tools)
			reason := "bubblewrap (bwrap) is on PATH only in a folder a sandboxed command may write (" + c.bin + ")"
			fix := "install bubblewrap for the whole system (pacman -S bubblewrap, apt install bubblewrap or dnf install bubblewrap), or put it in a PATH folder outside the project and the folders the sandbox opens for writing"
			r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "/bin/sh", "-c", "echo ran")
			if want := "jhon sandbox: " + reason + ", so nothing was run; fix: " + fix + "\n"; r.Code != 126 || r.Stdout != "" || r.Stderr != want {
				t.Errorf("run: exit %d, stdout %q, stderr %q; want 126 and %q", r.Code, r.Stdout, r.Stderr, want)
			}
			if got := statusOf(t, b, env); got != (status{Mode: "off", Reason: reason, Fix: fix, Project: b.proj}) {
				t.Errorf("status --json: %+v; want unable, %q and the fix", got, reason)
			}
			if r := support.Jhon(t, b.proj, env, "sandbox", "status"); r.Code != 0 || r.Stdout != "sandbox off; it cannot run here: "+reason+"; fix: "+fix+"; project "+b.proj+"\n" {
				t.Errorf("status: exit %d, %q", r.Code, r.Stdout)
			}
			if r := support.Jhon(t, b.proj, env, "doctor"); !strings.Contains(r.Stdout, "\nsandbox\n  warn  sandbox off; it cannot run here: "+reason+"; fix: "+fix+"; the model's commands run unsandboxed\n") {
				t.Errorf("doctor:\n%s", r.Out())
			}
			if exists(escaped) {
				t.Errorf("the bwrap in %s ran", c.bin)
			}
		})
	}
}

// git is not Jhon's to start either: a command may write .git/config.worktree,
// which sent the start to git, and a planted git would then run outside
// (T128a, B1).
func TestGitIsNotTakenFromAWritablePATHFolder(t *testing.T) {
	b := newBox(t)
	shims := filepath.Join(b.home, ".local", "share", "mise", "shims")
	ran := filepath.Join(b.base, "fake-git-ran")
	script(t, filepath.Join(shims, "git"), "#!/bin/sh\necho ran > "+ran+"\nexec /usr/bin/git \"$@\"\n")
	support.Write(t, b.proj, map[string]string{".git/config.worktree": "[core]\n\tworktree = " + b.home + "\n"})
	env := append(slices.Clone(b.env), "PATH="+shims+":"+os.Getenv("PATH"))
	support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", "echo ran")
	if exists(ran) {
		t.Fatalf("the planted git ran outside the sandbox (%s exists)", ran)
	}
}

// A PATH folder outside the project is read-only inside the sandbox, also
// inside a writable folder: a tool folder of the default list, and one the
// owner opened. The folders around it stay writable, and a PATH folder in
// the project is the project's (F2).
func TestAPATHFolderIsReadOnlyInsideAWritableFolder(t *testing.T) {
	b := newBox(t)
	b.settings(t, "schema = 1\n\n[sandbox]\nallow_write = [\"~/.local/share/mise\"]\n")
	shims := filepath.Join(b.home, ".local", "share", "mise", "shims")
	gradle := filepath.Join(b.home, ".gradle", "bin")
	inProject := filepath.Join(b.proj, "node_modules", ".bin")
	mkdirs(t, shims, gradle, inProject, filepath.Join(b.home, ".gradle", "caches"))
	env := append(slices.Clone(b.env), "PATH="+strings.Join([]string{shims, gradle, inProject, os.Getenv("PATH")}, ":"))
	for _, dir := range []string{shims, gradle} {
		r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `echo plant > "$1/bwrap"`, "sh", dir)
		if !readOnly(r) || exists(filepath.Join(dir, "bwrap")) {
			t.Errorf("a write in the PATH folder %s: exit %d, stderr %q; want a read-only refusal and no file", dir, r.Code, r.Stderr)
		}
	}
	r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `echo x > "$HOME/.gradle/caches/x" && echo x > "$HOME/.local/share/mise/x" && echo x > node_modules/.bin/tool && echo done`)
	if r.Code != 0 || r.Stdout != "done\n" {
		t.Errorf("a write beside the PATH folders and in the project's own: exit %d, stderr %q", r.Code, r.Stderr)
	}
}

// The default list holds the toolchains' caches and stores, not the folders
// with the programs the owner or a runtime runs later (F2).
func TestTheDefaultToolFoldersAreCachesAndStores(t *testing.T) {
	b := newBox(t)
	closed := []string{"go/bin", ".cargo/bin", ".rustup", ".bun/bin", ".local/share/pnpm", ".local/share/mise/shims", ".local/share/mise/installs", ".local/share/uv", ".local/share/NuGet", ".local/state/mise"}
	open := []string{"go/pkg", ".cargo/registry", ".cargo/git", ".npm", ".local/share/pnpm/store", ".pnpm-store", ".bun/install/cache", ".gradle", ".m2/repository", ".nuget/packages"}
	for _, rel := range slices.Concat(closed, open) {
		mkdirs(t, filepath.Join(b.home, rel))
	}
	for _, rel := range closed {
		file := filepath.Join(b.home, rel, "written")
		if r := b.sh(t, b.proj, "", `echo x > "$HOME/`+rel+`/written"`); !readOnly(r) || exists(file) {
			t.Errorf("a write in ~/%s: exit %d, stderr %q; want a read-only refusal and no file", rel, r.Code, r.Stderr)
		}
	}
	for _, rel := range open {
		file := filepath.Join(b.home, rel, "written")
		if r := b.sh(t, b.proj, "", `echo x > "$HOME/`+rel+`/written"`); r.Code != 0 || !exists(file) {
			t.Errorf("a write in ~/%s: exit %d, stderr %q; want it written", rel, r.Code, r.Stderr)
		}
	}
}

// What one command writes in .git, the next start must not trust: no folder
// outside the project becomes writable because a gitdir file names it
// (T128a, B2).
func TestGitMetadataWrittenInsideCannotOpenAFolderOutside(t *testing.T) {
	b := newBox(t)
	support.Git(t, b.proj, "worktree", "add", filepath.Join(b.base, "wt"), "-b", "wt")
	outside := filepath.Join(b.home, ".config", "systemd", "user")
	mkdirs(t, outside)
	admin, _ := filepath.Glob(filepath.Join(b.proj, ".git", "worktrees", "*"))
	if len(admin) != 1 {
		t.Fatalf("worktree admin dirs %v, want one", admin)
	}
	r := b.sh(t, b.proj, "", "printf '%s\\n' "+filepath.Join(outside, ".git")+" > "+filepath.Join(admin[0], "gitdir"))
	if r.Code != 0 {
		t.Fatalf("writing in .git failed: exit %d, %s", r.Code, r.Stderr)
	}
	r = b.sh(t, b.proj, "", "echo unit > "+filepath.Join(outside, "pwn.service"))
	if r.Code == 0 || exists(filepath.Join(outside, "pwn.service")) {
		t.Fatalf("a folder outside the project became writable: exit %d, stdout %q", r.Code, r.Stdout)
	}
}

// The same for a `.git` file a command wrote in the project, naming another
// repository: that repository is no root (T128a, B2).
func TestAGitFileWrittenInsideCannotOpenAnotherRepository(t *testing.T) {
	b := newBox(t)
	other := filepath.Join(b.home, "other")
	mkdirs(t, other)
	support.GitRepo(t, other)
	r := b.sh(t, b.proj, "", "mkdir sub && printf 'gitdir: %s\\n' "+filepath.Join(other, ".git")+" > sub/.git")
	if r.Code != 0 {
		t.Fatalf("writing the .git file failed: exit %d, %s", r.Code, r.Stderr)
	}
	for _, file := range []string{filepath.Join(other, "written"), filepath.Join(other, ".git", "written")} {
		if r := support.Jhon(t, filepath.Join(b.proj, "sub"), b.env, "sandbox", "run", "--", "sh", "-c", `echo x > "$1"`, "sh", file); !readOnly(r) || exists(file) {
			t.Errorf("a write to %s: exit %d, stderr %q; want a read-only refusal and no file", file, r.Code, r.Stderr)
		}
	}
}

// core.worktree and a config include move the top folder for git; the
// sandbox ignores both, and asks no git: the project is the folder that holds
// `.git`, under a PATH with bubblewrap and nothing else (F3).
func TestCoreWorktreeAndAConfigIncludeAreIgnored(t *testing.T) {
	for _, c := range []struct {
		name string
		set  func(t *testing.T, b box, moved string)
	}{
		{"core.worktree", func(t *testing.T, b box, moved string) { support.Git(t, b.proj, "config", "core.worktree", moved) }},
		{"a config include", func(t *testing.T, b box, moved string) {
			support.Write(t, b.proj, map[string]string{".git/moved.cfg": "[core]\n\tworktree = " + moved + "\n"})
			support.Git(t, b.proj, "config", "include.path", "moved.cfg")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := newBox(t)
			moved := filepath.Join(b.home, "moved")
			mkdirs(t, moved)
			c.set(t, b, moved)
			env := onlyBwrap(t, b)
			r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "/bin/sh", "-c", `echo x > in-project.txt && echo done`)
			if r.Code != 0 || r.Stdout != "done\n" || !exists(filepath.Join(b.proj, "in-project.txt")) {
				t.Errorf("a write in the folder that holds .git: exit %d, stderr %q; want it written", r.Code, r.Stderr)
			}
			r = support.Jhon(t, b.proj, env, "sandbox", "run", "--", "/bin/sh", "-c", `echo x > "$HOME/moved/written"`)
			if !readOnly(r) || exists(filepath.Join(moved, "written")) {
				t.Errorf("a write in the folder the config names: exit %d, stderr %q; want a read-only refusal and no file", r.Code, r.Stderr)
			}
		})
	}
}

// A real linked worktree is still the project's from the main one: its own
// `.git` file points back at the admin folder that lists it (F3).
func TestAListedWorktreeIsWritableFromTheMainOne(t *testing.T) {
	b := newBox(t)
	wt := filepath.Join(b.base, "elsewhere", "example-change")
	support.Git(t, b.proj, "worktree", "add", "-q", "-b", "example-change", wt)
	r := support.Jhon(t, b.proj, onlyBwrap(t, b), "sandbox", "run", "--", "/bin/sh", "-c", `echo x > "$1" && echo done`, "sh", filepath.Join(wt, "from-main.txt"))
	if r.Code != 0 || r.Stdout != "done\n" || !exists(filepath.Join(wt, "from-main.txt")) {
		t.Errorf("a write to the listed worktree from the main one: exit %d, stderr %q", r.Code, r.Stderr)
	}
}

// deliveryVariables are the variables that carry what the delivery profile
// shows: the tokens, the agent socket and the programs that ask for a secret.
var deliveryVariables = []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "SSH_AUTH_SOCK", "GIT_ASKPASS", "SSH_ASKPASS"}

// withDeliveryVariables is a box with every delivery variable set, and one
// that is none of them.
func withDeliveryVariables(t *testing.T) box {
	t.Helper()
	extra := []string{"JHON_TEST_OTHER=kept"}
	for _, v := range deliveryVariables {
		extra = append(extra, v+"=ghp_FAKE-example")
	}
	return newBox(t, extra...)
}

// printVariables prints each delivery variable as name=value, or name=none.
const printVariables = `for v in GH_TOKEN GITHUB_TOKEN GH_ENTERPRISE_TOKEN GITHUB_ENTERPRISE_TOKEN SSH_AUTH_SOCK GIT_ASKPASS SSH_ASKPASS JHON_TEST_OTHER; do eval "echo $v=\${$v-none}"; done`

// Masking hides the credential files; the delivery variables that carry the
// same secrets must not be handed to a work command either (T128a, G1, F4).
func TestDeliveryVariablesDoNotReachAWorkCommand(t *testing.T) {
	b := withDeliveryVariables(t)
	r := b.sh(t, b.proj, "", printVariables)
	if strings.Contains(r.Stdout, "FAKE-example") {
		t.Errorf("a work command read a delivery variable: %q", r.Stdout)
	}
	want := ""
	for _, v := range deliveryVariables {
		want += v + "=none\n"
	}
	if want += "JHON_TEST_OTHER=kept\n"; r.Code != 0 || r.Stdout != want {
		t.Errorf("exit %d, stdout %q, stderr %q; want each delivery variable unset and the rest kept", r.Code, r.Stdout, r.Stderr)
	}
}

// The delivery profile keeps them: a push authenticates (F4, D4).
func TestDeliveryVariablesReachADeliveryCommand(t *testing.T) {
	b := withDeliveryVariables(t)
	want := ""
	for _, v := range deliveryVariables {
		want += v + "=ghp_FAKE-example\n"
	}
	if r := b.sh(t, b.proj, "delivery", printVariables); r.Code != 0 || r.Stdout != want+"JHON_TEST_OTHER=kept\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want every delivery variable kept", r.Code, r.Stdout, r.Stderr)
	}
}

// A snapshot folder is a second copy of the home, credentials included: it
// is there, empty and read-only, in both profiles (F5).
func TestASnapshotFolderIsHidden(t *testing.T) {
	b := newBox(t)
	support.Write(t, b.home, map[string]string{".snapshots/1/snapshot/home/u/.netrc": secret + "\n"})
	for _, profile := range []string{"work", "delivery"} {
		r := b.sh(t, b.proj, profile, `ls -A "$HOME/.snapshots" | wc -l; grep -rs FAKE-SECRET "$HOME/.snapshots"; echo x > "$HOME/.snapshots/new"`)
		if strings.Contains(r.Out(), secret) || strings.TrimSpace(r.Stdout) != "0" || !readOnly(r) || exists(filepath.Join(b.home, ".snapshots", "new")) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q; want the folder empty and read-only", profile, r.Code, r.Stdout, r.Stderr)
		}
	}
}

// listen starts this test's own unix socket at path.
func listen(t *testing.T, path string) {
	t.Helper()
	mkdirs(t, filepath.Dir(path))
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
}

// The runtime dir is a private one: empty but for the container sockets,
// writable, mode 0700, and nothing written in it reaches the real one. The
// delivery profile shows the agent socket in it too (F6).
func TestTheRuntimeDirIsPrivateAndShowsOnlyTheContainerSockets(t *testing.T) {
	b := newBox(t)
	run := filepath.Join(b.base, "run")
	for _, s := range []string{"wayland-1", "pipewire-0", "docker.sock", "podman/podman.sock", "agent.sock", "app/session.sock"} {
		listen(t, filepath.Join(run, s))
	}
	support.Write(t, run, map[string]string{"dconf/user": secret + "\n"})
	b.env = append(b.env, "XDG_RUNTIME_DIR="+run, "SSH_AUTH_SOCK="+filepath.Join(run, "agent.sock"))
	look := `cd "$XDG_RUNTIME_DIR" && stat -c %a . && find . -mindepth 1 | sort && echo x > new && cat new && test -S docker.sock && test -S podman/podman.sock && echo sockets`
	r := b.sh(t, b.proj, "", look)
	if want := "700\n./docker.sock\n./podman\n./podman/podman.sock\nx\nsockets\n"; r.Code != 0 || r.Stdout != want {
		t.Errorf("work: exit %d, stdout %q, stderr %q; want %q", r.Code, r.Stdout, r.Stderr, want)
	}
	r = b.sh(t, b.proj, "delivery", look+` && test -S "$SSH_AUTH_SOCK" && echo agent`)
	if want := "700\n./agent.sock\n./docker.sock\n./podman\n./podman/podman.sock\nx\nsockets\nagent\n"; r.Code != 0 || r.Stdout != want {
		t.Errorf("delivery: exit %d, stdout %q, stderr %q; want %q", r.Code, r.Stdout, r.Stderr, want)
	}
	if exists(filepath.Join(run, "new")) {
		t.Error("a file written in the private runtime dir reached the real one")
	}
}

// The X authority file is hidden when it exists (F6). The socket folders
// under /tmp are the machine's own: they are tested against fake folders,
// through the builder (engine/internal/sandbox), never here.
func TestTheXAuthorityFileIsHidden(t *testing.T) {
	b := newBox(t)
	support.Write(t, b.home, map[string]string{".Xauthority": secret + "\n"})
	for _, profile := range []string{"work", "delivery"} {
		r := b.sh(t, b.proj, profile, `cat "$HOME/.Xauthority"; echo x >> "$HOME/.Xauthority"`)
		if strings.Contains(r.Out(), secret) || support.Read(t, filepath.Join(b.home, ".Xauthority")) != secret+"\n" {
			t.Errorf("%s: exit %d, %q; want the file empty inside and as it was on disk", profile, r.Code, r.Out())
		}
	}
}

// A $TMPDIR where a scratch root may be is bound and kept: under the cache
// (F7).
func TestATMPDIRUnderTheCacheIsBoundAndKept(t *testing.T) {
	b := newBox(t)
	tmp := filepath.Join(b.home, ".cache", "session-tmp")
	mkdirs(t, tmp)
	r := support.Jhon(t, b.proj, append(slices.Clone(b.env), "TMPDIR="+tmp), "sandbox", "run", "--", "sh", "-c", `echo x > "$TMPDIR/written" && echo "$TMPDIR"`)
	if r.Code != 0 || r.Stdout != tmp+"\n" || !exists(filepath.Join(tmp, "written")) {
		t.Errorf("exit %d, stdout %q, stderr %q; want a write in $TMPDIR and the variable kept", r.Code, r.Stdout, r.Stderr)
	}
}

// Any other $TMPDIR is not bound: the program runs, sees TMPDIR=/tmp, and
// the folder the variable named stays unwritable. /proc/sys is the verify's
// own (N1); the others are a folder outside every allowed place, /, the
// home, a folder above it, one of Jhon's own, a hidden one, and a path that
// is not absolute (F7).
func TestATMPDIRElsewhereIsNotBoundAndBecomesTmp(t *testing.T) {
	b := newBox(t)
	b.settings(t, "schema = 1\n")
	elsewhere := filepath.Join(b.base, "elsewhere")
	own, hidden := filepath.Join(b.home, ".config", "jhon", "tmp"), filepath.Join(b.home, ".ssh", "tmp")
	mkdirs(t, elsewhere, own, hidden)
	for _, c := range []struct{ tmpdir, probe string }{
		{"/proc/sys", "/proc/sys/jhon-test-new"},
		{elsewhere, filepath.Join(elsewhere, "new")},
		{"/", filepath.Join(b.home, "new")},
		{b.home, filepath.Join(b.home, "new")},
		{filepath.Dir(b.home), filepath.Join(filepath.Dir(b.home), "new")},
		{own, filepath.Join(own, "new")},
		{hidden, filepath.Join(hidden, "new")},
		{"relative", filepath.Join(b.home, "new")},
	} {
		env := append(slices.Clone(b.env), "TMPDIR="+c.tmpdir)
		r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `echo "$TMPDIR"; f=$(mktemp) && rm "$f" && echo scratch; echo x > "$1"`, "sh", c.probe)
		if r.Code == 0 || r.Stdout != "/tmp\nscratch\n" || exists(c.probe) {
			t.Errorf("TMPDIR=%s: exit %d, stdout %q, stderr %q; want TMPDIR=/tmp, a working scratch, and no write to %s", c.tmpdir, r.Code, r.Stdout, r.Stderr, c.probe)
		}
	}
}

// withOldBwrap is the box's environment with a bwrap first on PATH, in a
// folder no sandboxed command may write, that answers as one before 0.5.0
// does: it does not know --perms.
func withOldBwrap(t *testing.T, b box) []string {
	t.Helper()
	bin := filepath.Join(b.base, "old-bin")
	script(t, filepath.Join(bin, "bwrap"), "#!/bin/sh\nfor a in \"$@\"; do\n  [ \"$a\" = -- ] && break\n  if [ \"$a\" = --perms ]; then echo 'bwrap: Unknown option --perms' >&2; exit 1; fi\ndone\nexit 0\n")
	return append(slices.Clone(b.env), "PATH="+bin+":"+os.Getenv("PATH"))
}

// status and doctor agree with run: the probe starts its sandbox with the
// options run uses, so a bubblewrap too old for them cannot run, and the fix
// names the version that can (L888 continuation, item 3).
func TestABubblewrapTooOldForTheSandboxCannotRun(t *testing.T) {
	b := newBox(t)
	// The mode off: a warning (T128f: on, by default on Linux, is a problem).
	b.settings(t, "[sandbox]\nmode = \"off\"\n")
	// A runtime dir of the box's own, so a run gives --perms on any machine.
	run := filepath.Join(b.base, "run")
	mkdirs(t, run)
	b.env = append(b.env, "XDG_RUNTIME_DIR="+run)
	env := withOldBwrap(t, b)
	reason := "bubblewrap is too old for the sandbox (bwrap: Unknown option --perms)"
	fix := "update bubblewrap to 0.5.0 or newer (pacman -S bubblewrap, apt install bubblewrap or dnf install bubblewrap)"
	if got := statusOf(t, b, env); got != (status{Mode: "off", Reason: reason, Fix: fix, Project: b.proj}) {
		t.Errorf("status --json: %+v; want unable, %q and %q", got, reason, fix)
	}
	if r := support.Jhon(t, b.proj, env, "sandbox", "status"); r.Code != 0 || r.Stdout != "sandbox off; it cannot run here: "+reason+"; fix: "+fix+"; project "+b.proj+"\n" {
		t.Errorf("status: exit %d, %q", r.Code, r.Stdout)
	}
	if r := support.Jhon(t, b.proj, env, "doctor"); !strings.Contains(r.Stdout, "\nsandbox\n  warn  sandbox off; it cannot run here: "+reason+"; fix: "+fix+"; the model's commands run unsandboxed\n") {
		t.Errorf("doctor:\n%s", r.Out())
	}
	// run says the same in bubblewrap's own words, and runs nothing.
	marker := filepath.Join(b.proj, "ran.txt")
	if r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `echo ran > "$1"`, "sh", marker); r.Code == 0 || !strings.Contains(r.Stderr, "Unknown option --perms") || exists(marker) {
		t.Errorf("run: exit %d, stderr %q; want bubblewrap's refusal and nothing run", r.Code, r.Stderr)
	}
}

// nestedRepo is a repository of its own inside the box's project.
func nestedRepo(t *testing.T, b box) string {
	t.Helper()
	nested := filepath.Join(b.proj, "nested")
	mkdirs(t, nested)
	return support.GitRepo(t, nested)
}

// The caller pins the project: with --project, a command that removed the
// project's `.git` entry does not make the enclosing repository the next
// start's project (L888 continuation, item 4).
func TestAPinnedProjectStaysTheProjectWhenItsGitEntryIsRemoved(t *testing.T) {
	b := newBox(t)
	nested := nestedRepo(t, b)
	pinned := func(script string) support.Result {
		return support.Jhon(t, nested, b.env, "sandbox", "run", "--project", nested, "--", "sh", "-c", script)
	}
	if r := pinned(`rm -rf .git && echo removed`); r.Code != 0 || r.Stdout != "removed\n" || exists(filepath.Join(nested, ".git")) {
		t.Fatalf("removing the nested .git: exit %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}
	r := pinned(`echo x > inside.txt && echo x > ../enclosing.txt`)
	if !readOnly(r) || exists(filepath.Join(b.proj, "enclosing.txt")) || !exists(filepath.Join(nested, "inside.txt")) {
		t.Errorf("after the removal: exit %d, stderr %q; want the pinned folder writable and the enclosing repository read-only", r.Code, r.Stderr)
	}
}

// Without --project the walk up from the working directory decides, as
// before: once a command removed the nested `.git`, the enclosing repository
// is the next start's project, and writable. The runtimes pin the project
// for that reason (T128b, T128c).
func TestWithoutProjectARemovedGitEntryMakesTheEnclosingRepositoryTheProject(t *testing.T) {
	b := newBox(t)
	nested := nestedRepo(t, b)
	if r := b.sh(t, nested, "", `echo x > ../enclosing.txt`); !readOnly(r) || exists(filepath.Join(b.proj, "enclosing.txt")) {
		t.Fatalf("from the nested repository: exit %d, stderr %q; want the enclosing one read-only", r.Code, r.Stderr)
	}
	if r := b.sh(t, nested, "", `rm -rf .git`); r.Code != 0 {
		t.Fatalf("removing the nested .git: exit %d, stderr %q", r.Code, r.Stderr)
	}
	if r := b.sh(t, nested, "", `echo x > ../enclosing.txt`); r.Code != 0 || !exists(filepath.Join(b.proj, "enclosing.txt")) {
		t.Errorf("after the removal: exit %d, stderr %q; the walk up was expected to take the enclosing repository", r.Code, r.Stderr)
	}
}

// --project takes a folder a project may be: absolute, there, and not /, the
// home folder or one above it; anything else is a usage error and nothing
// runs.
func TestProjectTakesOnlyAFolderAProjectMayBe(t *testing.T) {
	b := newBox(t)
	support.Write(t, b.proj, map[string]string{"file.txt": "x\n"})
	marker := filepath.Join(b.proj, "ran.txt")
	usage := "usage: jhon sandbox run [--profile work|delivery] [--project <folder>] -- <program> [args...]\n       jhon sandbox status [--json] [--project <folder>]\n"
	for _, bad := range []string{"relative", filepath.Join(b.base, "missing"), "/", b.home, filepath.Dir(b.home), filepath.Join(b.proj, "file.txt"), ""} {
		want := "jhon sandbox: --project takes an absolute folder that exists and is not /, the home folder or one above it: " + bad + "\n\n" + usage
		for _, args := range [][]string{
			{"run", "--project", bad, "--", "touch", marker}, {"run", "--project=" + bad, "--", "touch", marker}, {"status", "--project", bad},
		} {
			r := support.Jhon(t, b.proj, b.env, append([]string{"sandbox"}, args...)...)
			if r.Code != 2 || r.Stdout != "" || r.Stderr != want {
				t.Errorf("%q: exit %d, stdout %q, stderr %q; want 2 and %q", args, r.Code, r.Stdout, r.Stderr, want)
			}
		}
	}
	if r := support.Jhon(t, b.proj, b.env, "sandbox", "run", "--project"); r.Code != 2 || !strings.HasPrefix(r.Stderr, "jhon sandbox: run takes a program after --\n") {
		t.Errorf("--project with no folder: exit %d, stderr %q", r.Code, r.Stderr)
	}
	if exists(marker) {
		t.Error("the program ran")
	}
}

// The working directory need not be in the pinned project, and the project's
// other folders still come from its own `.git`: pinned to a linked worktree,
// the main one and the common dir are writable, and the folder the program
// starts in is not. status takes --project too.
func TestAPinnedProjectNeedNotHoldTheWorkingDirectory(t *testing.T) {
	b := newBox(t)
	wt := filepath.Join(b.base, "elsewhere", "example-change")
	support.Git(t, b.proj, "worktree", "add", "-q", "-b", "example-change", wt)
	apart := filepath.Join(b.home, "apart")
	mkdirs(t, apart)
	r := support.Jhon(t, apart, b.env, "sandbox", "run", "--profile", "work", "--project="+wt, "--", "sh", "-c", `pwd; echo x > "$1/in-linked" && echo x > "$HOME/project/in-main" && echo x > "$HOME/project/.git/probe" && echo x > here.txt`, "sh", wt)
	if r.Stdout != apart+"\n" || !readOnly(r) || exists(filepath.Join(apart, "here.txt")) {
		t.Errorf("exit %d, stdout %q, stderr %q; want the program started in %s, read-only", r.Code, r.Stdout, r.Stderr, apart)
	}
	for _, file := range []string{filepath.Join(wt, "in-linked"), filepath.Join(b.proj, "in-main"), filepath.Join(b.proj, ".git", "probe")} {
		if !exists(file) {
			t.Errorf("%s was not written: stderr %q", file, r.Stderr)
		}
	}
	// status follows the pin: a bubblewrap inside the pinned project, or
	// inside a worktree it lists, is one a command may write; pinned to a
	// folder that is none of the repository's, it is not.
	script(t, filepath.Join(wt, "bin", "bwrap"), "#!/bin/sh\nexit 0\n")
	env := append(slices.Clone(b.env), "PATH="+filepath.Join(wt, "bin"))
	for _, c := range []struct {
		args []string
		can  bool
	}{{[]string{"--json", "--project", wt}, false}, {[]string{"--project=" + b.proj, "--json"}, false}, {[]string{"--project=" + apart, "--json"}, true}} {
		r := support.Jhon(t, apart, env, append([]string{"sandbox", "status"}, c.args...)...)
		if r.Code != 0 || strings.Contains(r.Stdout, `"can_run":true`) != c.can || strings.Contains(r.Stdout, `"can_run":false`) == c.can {
			t.Errorf("status %v: exit %d, %q; want can_run %v", c.args, r.Code, r.Stdout, c.can)
		}
	}
}
