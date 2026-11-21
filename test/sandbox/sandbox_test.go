// `jhon sandbox run` (T128a, odd/proposals/t128-sandbox.md sections 3-5): a
// program runs under bubblewrap, where the kernel refuses a write outside
// the project, the scratch roots and the caches, credentials are not there,
// and Jhon's own files are read-only, whatever the command's spelling.
//
// Every test runs under a scratch HOME (/home/u under a scratch folder) with
// fake credential files: no real credential path is read, listed or named.
// The scratch folder is under the user's cache directory, never /tmp: /tmp is
// writable inside the sandbox, so a HOME there would prove nothing.
package sandbox

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// TestMain points the binary at this checkout (test/ldd/helpers_test.go).
func TestMain(m *testing.M) {
	os.Setenv("JHON_ROOT", support.Root())
	os.Exit(m.Run())
}

// secret is what every fake credential file holds.
const secret = "FAKE-SECRET-example"

// box is one scratch machine: a HOME, a project in it, and a scratch root
// under the home, where a $TMPDIR may be.
type box struct {
	base, home, proj, tmp string
	env                   []string
}

// newBox is a scratch HOME with a git project, skipping the test where
// bubblewrap cannot run.
func newBox(t *testing.T, extra ...string) box {
	t.Helper()
	if out, err := exec.Command("bwrap", "--ro-bind", "/", "/", "true").CombinedOutput(); err != nil {
		t.Skipf("skipped: bubblewrap cannot run on this machine (%v: %s), so the sandbox cannot be tested here", err, strings.TrimSpace(string(out)))
	}
	cache := filepath.Join(os.Getenv("HOME"), ".cache")
	if real, err := filepath.EvalSymlinks(cache); err != nil || real == "/tmp" || strings.HasPrefix(real, "/tmp/") {
		t.Skipf("skipped: the scratch HOME needs a folder outside /tmp, and %s is not one", cache)
	}
	base, err := os.MkdirTemp(cache, "jhon-sandbox-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	if base, err = filepath.EvalSymlinks(base); err != nil {
		t.Fatal(err)
	}
	b := box{base: base, home: filepath.Join(base, "home", "u")}
	b.proj, b.tmp = filepath.Join(b.home, "project"), filepath.Join(b.home, "tmp")
	for _, d := range []string{b.proj, b.tmp, filepath.Join(b.home, ".cache")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	support.GitRepo(t, b.proj)
	for _, kv := range support.Env() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "JHON_") || strings.HasPrefix(k, "XDG_") || k == "HOME" || k == "TMPDIR" || k == "SSH_AUTH_SOCK" || k == "DBUS_SESSION_BUS_ADDRESS" {
			continue
		}
		b.env = append(b.env, kv)
	}
	b.env = append(b.env, "HOME="+b.home, "TMPDIR="+b.tmp, "JHON_TELEMETRY=off", "JHON_ROOT="+support.Root())
	b.env = append(b.env, extra...)
	return b
}

// sh runs script under `jhon sandbox run` in dir, with the profile when given.
func (b box) sh(t *testing.T, dir, profile, script string) support.Result {
	t.Helper()
	args := []string{"sandbox", "run"}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	return support.Jhon(t, dir, b.env, append(args, "--", "sh", "-c", script)...)
}

// settings writes text as the scratch HOME's settings.toml.
func (b box) settings(t *testing.T, text string) string {
	t.Helper()
	support.Write(t, b.home, map[string]string{".config/jhon/settings.toml": text})
	return filepath.Join(b.home, ".config", "jhon", "settings.toml")
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func TestAWriteInsideTheWorkAreaSucceeds(t *testing.T) {
	b := newBox(t)
	extra := filepath.Join(b.base, "extra")
	if err := os.MkdirAll(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	b.settings(t, "schema = 1\n\n[sandbox]\nallow_write = [\""+extra+"\"]\n")
	slash, err := os.MkdirTemp("/tmp", "jhon-sandbox-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(slash) })
	for _, c := range []struct{ name, file string }{
		{"the project", filepath.Join(b.proj, "written.txt")},
		{"$TMPDIR", filepath.Join(b.tmp, "written.txt")},
		{"/tmp", filepath.Join(slash, "written.txt")},
		{"the cache", filepath.Join(b.home, ".cache", "written.txt")},
		{"a sandbox.allow_write path", filepath.Join(extra, "written.txt")},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := support.Jhon(t, b.proj, b.env, "sandbox", "run", "--", "sh", "-c", `echo inside > "$1" && echo done`, "sh", c.file)
			if r.Code != 0 || r.Stdout != "done\n" || r.Stderr != "" {
				t.Fatalf("exit %d, stdout %q, stderr %q; want 0, done, none", r.Code, r.Stdout, r.Stderr)
			}
			if got := support.Read(t, c.file); got != "inside\n" {
				t.Errorf("%s holds %q, want the written line", c.file, got)
			}
		})
	}
}

// A linked worktree commits through the common dir, which is outside its root.
func TestALinkedWorktreeCommitsThroughTheCommonDir(t *testing.T) {
	b := newBox(t)
	wt := filepath.Join(b.home, "project-worktrees", "example-change")
	support.Git(t, b.proj, "worktree", "add", "-q", "-b", "example-change", wt)
	r := b.sh(t, wt, "", `echo x > file.txt && git add file.txt && git -c user.name=jhon-test -c user.email=jhon-test@example.invalid -c commit.gpgsign=false commit -q -m example-change && echo committed`)
	if r.Code != 0 || r.Stdout != "committed\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q; want 0 and committed", r.Code, r.Stdout, r.Stderr)
	}
	if got := strings.TrimSpace(support.Git(t, b.proj, "log", "-1", "--format=%s", "example-change")); got != "example-change" {
		t.Errorf("the branch's last commit is %q, want the one made in the sandbox", got)
	}
	// The main worktree is the project's too.
	r = b.sh(t, wt, "", `echo x > "$HOME/project/from-linked.txt"`)
	if r.Code != 0 || !exists(filepath.Join(b.proj, "from-linked.txt")) {
		t.Errorf("a write to the main worktree from a linked one: exit %d, stderr %q", r.Code, r.Stderr)
	}
}

func TestAWriteOutsideTheWorkAreaFailsAndLeavesNoFile(t *testing.T) {
	b := newBox(t)
	settings := b.settings(t, "schema = 1\n")
	state := filepath.Join(b.home, ".local", "state", "jhon")
	releases := filepath.Join(b.home, ".local", "share", "jhon", "releases", "abc")
	for _, d := range []string{filepath.Join(state, "grants"), filepath.Join(state, "modes"), releases} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct{ name, script, file string }{
		{"a direct path in home", `echo x > "$HOME/escape.txt"`, filepath.Join(b.home, "escape.txt")},
		{"through a symlink in the project", `ln -s "$HOME" link && echo x > link/linked.txt`, filepath.Join(b.home, "linked.txt")},
		{"a path built with IFS", `IFS=:; set -- "$HOME":ifs.txt; p=$*; IFS=/; set -- $p; IFS=' '; unset p; for s in "$@"; do p="$p/$s"; done; echo x > "$p"`, filepath.Join(b.home, "ifs.txt")},
		{"Jhon's settings", `echo 'x = 1' >> "$HOME/.config/jhon/settings.toml"`, ""},
		{"a new file beside the settings", `echo x > "$HOME/.config/jhon/new.toml"`, filepath.Join(b.home, ".config", "jhon", "new.toml")},
		{"the grants", `echo x > "$HOME/.local/state/jhon/grants/push.json"`, filepath.Join(state, "grants", "push.json")},
		{"the modes", `echo x > "$HOME/.local/state/jhon/modes/mode.json"`, filepath.Join(state, "modes", "mode.json")},
		{"the release folder", `echo x > "$HOME/.local/share/jhon/releases/abc/jhon"`, filepath.Join(releases, "jhon")},
		{"a deletion in the release folder", `rmdir "$HOME/.local/share/jhon/releases/abc"`, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := b.sh(t, b.proj, "", c.script)
			if r.Code == 0 || r.Stdout != "" || !strings.Contains(r.Stderr, "Read-only file system") {
				t.Fatalf("exit %d, stdout %q, stderr %q; want a failure on a read-only file system", r.Code, r.Stdout, r.Stderr)
			}
			if c.file != "" && exists(c.file) {
				t.Errorf("%s was written", c.file)
			}
		})
	}
	if got := support.Read(t, settings); got != "schema = 1\n" {
		t.Errorf("the settings now hold %q", got)
	}
	if !exists(releases) {
		t.Error("the release folder was removed")
	}
}

// Jhon's own files stay read-only where a writable area holds them: the
// settings folder directly under a sandbox.allow_write folder. The read-only
// bind is a mount point, which the kernel refuses to rename, replace or
// remove from inside the sandbox (EBUSY), and the allowed folder around it
// lies in the read-only home: no command can move either aside and write
// new settings, so the sandbox runs (S10). A state folder under XDG_STATE_HOME in the scratch
// root has a plain parent a command can rename: that stops the run
// (xdg_path_test.go).
func TestJhonsFilesStayReadOnlyInsideAWritableArea(t *testing.T) {
	b := newBox(t)
	settings := b.settings(t, "schema = 1\n\n[sandbox]\nallow_write = [\"~/.config\"]\n")
	before := support.Read(t, settings)
	for _, c := range []struct{ name, script, file, want string }{
		{"the settings under an allowed folder", `echo x > "$HOME/.config/jhon/new.toml"`, filepath.Join(b.home, ".config", "jhon", "new.toml"), "Read-only file system"},
		{"the settings folder renamed", `mv "$HOME/.config/jhon" "$HOME/.config/jhon.old"`, filepath.Join(b.home, ".config", "jhon.old"), "Device or resource busy"},
		{"another folder renamed over the settings folder", `mkdir "$HOME/.config/new" && mv -T "$HOME/.config/new" "$HOME/.config/jhon"`, "", "Device or resource busy"},
		{"the allowed folder renamed", `mv "$HOME/.config" "$HOME/.config.old" && mkdir -p "$HOME/.config/jhon" && echo x > "$HOME/.config/jhon/settings.toml"`, filepath.Join(b.home, ".config.old"), "Read-only file system"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := b.sh(t, b.proj, "", c.script)
			if r.Code == 0 || r.Code == 126 || !strings.Contains(r.Stderr, c.want) {
				t.Fatalf("exit %d, stderr %q; want a failure: %s", r.Code, r.Stderr, c.want)
			}
			if c.file != "" && exists(c.file) {
				t.Errorf("%s was written", c.file)
			}
		})
	}
	// The area around them is writable.
	r := b.sh(t, b.proj, "", `echo x > "$HOME/.config/other.txt"`)
	if r.Code != 0 || !exists(filepath.Join(b.home, ".config", "other.txt")) {
		t.Errorf("a write beside them: exit %d, stderr %q", r.Code, r.Stderr)
	}
	if got := support.Read(t, settings); got != before {
		t.Errorf("the settings now hold %q", got)
	}
}

// credentialFiles are fake credential files, relative to the scratch HOME,
// one per path the work profile hides.
var credentialFiles = []string{
	".ssh/id_fake", ".gnupg/fake.kbx", ".aws/credentials", ".config/gh/hosts.yml", ".config/gcloud/fake.db",
	".netrc", ".git-credentials", ".config/git/credentials", ".docker/config.json", ".kube/config", ".npmrc",
	".cargo/credentials.toml", ".claude/.credentials.json", ".pi/agent/auth.json",
}

// withCredentials writes the fake credential files, and the stand-ins of the
// session D-Bus socket and the SSH agent socket under a scratch runtime dir.
func withCredentials(t *testing.T, b *box) (bus, agent string) {
	t.Helper()
	files := map[string]string{}
	for _, f := range credentialFiles {
		files[f] = secret + "\n"
	}
	support.Write(t, b.home, files)
	run := filepath.Join(b.base, "run")
	support.Write(t, run, map[string]string{"bus": secret + "\n", "agent.sock": secret + "\n"})
	bus, agent = filepath.Join(run, "bus"), filepath.Join(run, "agent.sock")
	b.env = append(b.env, "XDG_RUNTIME_DIR="+run, "DBUS_SESSION_BUS_ADDRESS=unix:path="+bus, "SSH_AUTH_SOCK="+agent)
	return bus, agent
}

func TestTheWorkProfileHidesEveryCredentialPath(t *testing.T) {
	b := newBox(t)
	bus, agent := withCredentials(t, &b)
	paths := []string{bus, agent}
	for _, f := range credentialFiles {
		paths = append(paths, filepath.Join(b.home, f))
	}
	for _, p := range paths {
		t.Run(strings.TrimPrefix(p, b.base), func(t *testing.T) {
			// The file, and whatever its folder lists.
			r := support.Jhon(t, b.proj, b.env, "sandbox", "run", "--", "sh", "-c", `cat "$1"; ls -A "$(dirname "$1")"; grep -rs FAKE-SECRET "$(dirname "$1")"; true`, "sh", p)
			if r.Code != 0 || strings.Contains(r.Out(), secret) {
				t.Fatalf("exit %d; the secret must not be readable:\n%s", r.Code, r.Out())
			}
			if strings.HasPrefix(p, b.home) && strings.Contains(strings.TrimPrefix(p, b.home+"/"), "/") && !strings.HasSuffix(p, ".json") && !strings.HasSuffix(p, ".toml") && !strings.HasSuffix(p, "git/credentials") {
				// A credentials folder is there, empty and read-only.
				w := support.Jhon(t, b.proj, b.env, "sandbox", "run", "--", "sh", "-c", `ls -A "$1" | wc -l; echo x > "$1/new"`, "sh", filepath.Dir(p))
				if w.Code == 0 || strings.TrimSpace(w.Stdout) != "0" || exists(filepath.Join(filepath.Dir(p), "new")) {
					t.Errorf("the folder of %s: exit %d, %q, %q; want it empty and read-only", p, w.Code, w.Stdout, w.Stderr)
				}
			}
			if got := support.Read(t, p); got != secret+"\n" {
				t.Errorf("%s on disk now holds %q", p, got)
			}
		})
	}
}

// The delivery profile is gone (T181, L1503): asking for it is a usage
// error, and no credential is read.
func TestTheDeliveryProfileIsGone(t *testing.T) {
	b := newBox(t)
	_, agent := withCredentials(t, &b)
	for _, p := range []string{filepath.Join(b.home, ".ssh", "id_fake"), filepath.Join(b.home, ".config", "gh", "hosts.yml"), agent} {
		r := support.Jhon(t, b.proj, b.env, "sandbox", "run", "--profile", "delivery", "--", "cat", p)
		if r.Code != 2 || r.Stdout != "" || !strings.HasPrefix(r.Stderr, "jhon sandbox: unknown profile: delivery (accepted: work)\n") {
			t.Errorf("delivery reads %s: exit %d, stdout %q, stderr %q; want the usage error and nothing read", p, r.Code, r.Stdout, r.Stderr)
		}
	}
}

func TestTheProgramsExitCodeAndOutputPassThrough(t *testing.T) {
	b := newBox(t)
	for _, c := range []struct {
		name, script string
		code         int
	}{
		{"exit 0", `echo out; echo err >&2`, 0},
		{"exit 3", `echo out; echo err >&2; exit 3`, 3},
		{"a signal", `echo out; echo err >&2; kill -TERM $$`, 128 + 15},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := b.sh(t, b.proj, "", c.script)
			if r.Code != c.code || r.Stdout != "out\n" || r.Stderr != "err\n" {
				t.Errorf("exit %d, stdout %q, stderr %q; want %d, out, err", r.Code, r.Stdout, r.Stderr, c.code)
			}
		})
	}
	// Its stdin, its working directory, and options of its own.
	r := support.Run(t, support.Cmd{Args: []string{support.Bin(t), "sandbox", "run", "--", "sh", "-c", `cat; pwd`}, Dir: b.proj, Env: b.env, Stdin: "from stdin\n"})
	if r.Code != 0 || r.Stdout != "from stdin\n"+b.proj+"\n" {
		t.Errorf("stdin and cwd: exit %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}
	// A program of the test's own, not a system one whose --help text varies
	// by coreutils (uutils printf took --help as its own, L1142): it echoes
	// each argument, writes to stderr and exits 4.
	args := filepath.Join(b.proj, "args")
	if err := os.WriteFile(args, []byte("#!/bin/sh\nfor a; do echo \"arg $a\"; done\necho its-own-stderr >&2\nexit 4\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r = support.Jhon(t, b.proj, b.env, "sandbox", "run", "--", args, "--help", "--profile", "delivery")
	if r.Code != 4 || r.Stdout != "arg --help\narg --profile\narg delivery\n" || r.Stderr != "its-own-stderr\n" {
		t.Errorf("the program's own --help and --profile: exit %d, stdout %q, stderr %q; want 4, each argument passed to the program, its stderr", r.Code, r.Stdout, r.Stderr)
	}
}

func TestRunRefusesWhatItCannotRead(t *testing.T) {
	b := newBox(t)
	marker := filepath.Join(b.proj, "ran.txt")
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"an unknown profile", []string{"run", "--profile", "root", "--", "touch", marker}, "jhon sandbox: unknown profile: root (accepted: work)\n"},
		{"no program", []string{"run", "--"}, "jhon sandbox: run takes a program after --\n"},
		{"no --", []string{"run", "touch", marker}, "jhon sandbox: run takes a program after --\n"},
		{"an unknown subcommand", []string{"wrap"}, "jhon sandbox: unknown subcommand: wrap\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := support.Jhon(t, b.proj, b.env, append([]string{"sandbox"}, c.args...)...)
			if r.Code != 2 || r.Stdout != "" || !strings.HasPrefix(r.Stderr, c.want) || !strings.Contains(r.Stderr, "usage: jhon sandbox run") {
				t.Errorf("exit %d, stdout %q, stderr %q; want 2 and %q with the usage", r.Code, r.Stdout, r.Stderr, c.want)
			}
			if exists(marker) {
				t.Error("the program ran")
			}
		})
	}
}

// withoutBwrap is the box's environment with a PATH that holds no bwrap.
func withoutBwrap(t *testing.T, b box) []string {
	t.Helper()
	empty := filepath.Join(b.base, "empty-bin")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	return append(append([]string{}, b.env...), "PATH="+empty)
}

// withFailingBwrap is the box's environment with a bwrap first on PATH that
// fails as one does where user namespaces are off.
func withFailingBwrap(t *testing.T, b box, path string) []string {
	t.Helper()
	bin := filepath.Join(b.base, "failing-bin")
	support.Write(t, bin, map[string]string{"bwrap": "#!/bin/sh\necho 'bwrap: No permissions to create a new namespace' >&2\nexit 1\n"})
	if err := os.Chmod(filepath.Join(bin, "bwrap"), 0o755); err != nil {
		t.Fatal(err)
	}
	return append(append([]string{}, b.env...), "PATH="+bin+":"+path)
}

func TestRunFailsClosedWithoutBubblewrap(t *testing.T) {
	b := newBox(t)
	marker := filepath.Join(b.proj, "ran.txt")
	r := support.Jhon(t, b.proj, withoutBwrap(t, b), "sandbox", "run", "--", "/bin/sh", "-c", `echo ran > "$1"`, "sh", marker)
	want := "jhon sandbox: bubblewrap (bwrap) is not on PATH, so nothing was run; fix: install bubblewrap (pacman -S bubblewrap, apt install bubblewrap or dnf install bubblewrap)\n"
	if r.Code != 126 || r.Stdout != "" || r.Stderr != want {
		t.Errorf("exit %d, stdout %q, stderr %q; want 126 and %q", r.Code, r.Stdout, r.Stderr, want)
	}
	if exists(marker) {
		t.Error("the program ran without a sandbox")
	}
}

type status struct {
	Mode    string `json:"mode"`
	CanRun  bool   `json:"can_run"`
	Reason  string `json:"reason"`
	Fix     string `json:"fix"`
	Project string `json:"project"`
}

func statusOf(t *testing.T, b box, env []string) status {
	t.Helper()
	r := support.Jhon(t, b.proj, env, "sandbox", "status", "--json")
	if r.Code != 0 || r.Stderr != "" {
		t.Fatalf("status --json: exit %d, stderr %q", r.Code, r.Stderr)
	}
	var s status
	if err := json.Unmarshal([]byte(r.Stdout), &s); err != nil {
		t.Fatalf("status --json printed %q: %v", r.Stdout, err)
	}
	return s
}

func TestStatusGivesTheSettingAndWhetherTheSandboxCanRun(t *testing.T) {
	b := newBox(t)
	// T128f (L1352 F1): on by default where the sandbox runs.
	if got := statusOf(t, b, b.env); got != (status{Mode: "on", CanRun: true, Project: b.proj}) {
		t.Errorf("with no settings: %+v; want on and able to run", got)
	}
	if r := support.Jhon(t, b.proj, b.env, "sandbox", "status"); r.Code != 0 || r.Stdout != "sandbox on; it can run here; project "+b.proj+"\n" {
		t.Errorf("status: exit %d, %q", r.Code, r.Stdout)
	}
	if r := support.Jhon(t, b.proj, b.env, "settings", "set", "sandbox.mode", "off"); r.Code != 0 {
		t.Fatalf("settings set sandbox.mode off: %d\n%s", r.Code, r.Out())
	}
	if got := statusOf(t, b, b.env); got != (status{Mode: "off", CanRun: true, Project: b.proj}) {
		t.Errorf("with sandbox.mode off: %+v; want off and able to run", got)
	}
	if r := support.Jhon(t, b.proj, b.env, "settings", "set", "sandbox.mode", "on"); r.Code != 0 {
		t.Fatalf("settings set sandbox.mode on: %d\n%s", r.Code, r.Out())
	}
	if got := statusOf(t, b, b.env); got != (status{Mode: "on", CanRun: true, Project: b.proj}) {
		t.Errorf("with sandbox.mode on: %+v; want on and able to run", got)
	}
	got := statusOf(t, b, withoutBwrap(t, b))
	if got.Mode != "on" || got.CanRun || got.Reason != "bubblewrap (bwrap) is not on PATH" || !strings.Contains(got.Fix, "install bubblewrap") {
		t.Errorf("without bwrap: %+v; want on, unable, and the fix", got)
	}
	got = statusOf(t, b, withFailingBwrap(t, b, os.Getenv("PATH")))
	if got.CanRun || !strings.Contains(got.Reason, "No permissions to create a new namespace") || !strings.Contains(got.Fix, "user namespaces") {
		t.Errorf("with a bwrap that cannot start: %+v; want unable, its message, and the fix", got)
	}
	r := support.Jhon(t, b.proj, withoutBwrap(t, b), "sandbox", "status")
	if r.Code != 0 || !strings.HasPrefix(r.Stdout, "sandbox on; it cannot run here: bubblewrap (bwrap) is not on PATH; fix: install bubblewrap") {
		t.Errorf("status without bwrap: exit %d, %q", r.Code, r.Stdout)
	}
}

func TestTheAllowWriteSettingTakesOnlyFoldersItMayOpen(t *testing.T) {
	b := newBox(t)
	for _, bad := range []string{"relative/path", "", "/", "~", "~/", b.home, "/tmp,,/var"} {
		r := support.Jhon(t, b.proj, b.env, "settings", "set", "sandbox.allow_write", bad)
		if r.Code != 2 || !strings.Contains(r.Stderr, "sandbox.allow_write") || !strings.Contains(r.Stderr, "is not accepted") {
			t.Errorf("set sandbox.allow_write %q: exit %d, stderr %q; want 2 and the refusal", bad, r.Code, r.Stderr)
		}
	}
	if exists(filepath.Join(b.home, ".config", "jhon", "settings.toml")) {
		t.Error("a refused value wrote the settings")
	}
	r := support.Jhon(t, b.proj, b.env, "settings", "set", "sandbox.allow_write", "~/.android, /opt/example")
	if r.Code != 0 {
		t.Fatalf("set sandbox.allow_write: %d\n%s", r.Code, r.Out())
	}
	if r := support.Jhon(t, b.proj, b.env, "settings", "get", "sandbox.allow_write"); r.Code != 0 || r.Stdout != "~/.android; /opt/example\n" {
		t.Errorf("get sandbox.allow_write: exit %d, %q", r.Code, r.Stdout)
	}
	// A second set replaces the list on its line.
	if r := support.Jhon(t, b.proj, b.env, "settings", "set", "sandbox.allow_write", "/opt/example"); r.Code != 0 {
		t.Fatalf("a second set: %d\n%s", r.Code, r.Out())
	}
	if got := support.Read(t, filepath.Join(b.home, ".config", "jhon", "settings.toml")); !strings.HasSuffix(got, "[sandbox]\nallow_write = [\"/opt/example\"]\n") {
		t.Errorf("the settings hold %q", got)
	}
}

func TestDoctorSaysWhetherTheSandboxCanRun(t *testing.T) {
	b := newBox(t)
	support.Write(t, b.home, map[string]string{"bin/claude": "#!/bin/sh\nexit 1\n"})
	if err := os.Chmod(filepath.Join(b.home, "bin", "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(b.home, "bin") + ":" + os.Getenv("PATH")
	last := func(s string) string { l := strings.Split(strings.TrimSpace(s), "\n"); return l[len(l)-1] }
	b.settings(t, "[sandbox]\nmode = \"off\"\n")
	r := support.Jhon(t, b.proj, append(append([]string{}, b.env...), "PATH="+path), "doctor")
	// The owner's off on Linux warns that the shell's writes outside the
	// project run unguarded (T128f slice 7, default_outside_test.go).
	if !strings.Contains(r.Stdout, "\nsandbox\n  warn  sandbox off; it can run here; with it off the model's shell commands run unguarded outside the project") {
		t.Errorf("doctor where the sandbox can run:\n%s", r.Out())
	}
	env := withFailingBwrap(t, b, path)
	failed := support.Jhon(t, b.proj, env, "doctor")
	if !strings.Contains(failed.Stdout, "\nsandbox\n  warn  sandbox off; it cannot run here: bubblewrap cannot start a sandbox (bwrap: No permissions to create a new namespace); fix: ") ||
		!strings.Contains(failed.Stdout, "; the model's commands run unsandboxed\n") {
		t.Errorf("doctor where the sandbox cannot run:\n%s", failed.Out())
	}
	// With the mode off, a warning, never a problem: the verdict is the one without it.
	if last(failed.Stdout) != last(r.Stdout) || failed.Code != r.Code {
		t.Errorf("the sandbox line changed doctor's verdict: %q exit %d, against %q exit %d", last(failed.Stdout), failed.Code, last(r.Stdout), r.Code)
	}
	// T128f (L1352 F2): with the mode on, by default or set, where the
	// sandbox runs, doctor is ok. Set on where it cannot run refuses every
	// command: a problem, with the fix. With no owner value the default is
	// then off (slice 8b, L1379; default_probe_test.go).
	for _, text := range []string{"", "[sandbox]\nmode = \"on\"\n"} {
		b.settings(t, text)
		if ok := support.Jhon(t, b.proj, append(append([]string{}, b.env...), "PATH="+path), "doctor"); !strings.Contains(ok.Stdout, "\nsandbox\n  ok    sandbox on; it can run here\n") {
			t.Errorf("%q: doctor where the sandbox can run:\n%s", text, ok.Out())
		}
		on := support.Jhon(t, b.proj, env, "doctor")
		if text == "" {
			if !strings.Contains(on.Stdout, "\nsandbox\n  warn  sandbox off by default; it cannot run here: bubblewrap cannot start a sandbox") || on.Code != failed.Code {
				t.Errorf("no settings: doctor where bubblewrap cannot start: exit %d\n%s", on.Code, on.Out())
			}
			continue
		}
		row := "\nsandbox\n  FAIL  sandbox on; it cannot run here: bubblewrap cannot start a sandbox (bwrap: No permissions to create a new namespace); fix: turn unprivileged user namespaces on"
		if !strings.Contains(on.Stdout, row) || !strings.Contains(on.Stdout, "; until then every command of the model is refused\n") || on.Code != 1 || !strings.HasSuffix(last(on.Stdout), "problem(s)") {
			t.Errorf("%q: doctor with the mode on where the sandbox cannot run: exit %d\n%s", text, on.Code, on.Out())
		}
	}
}

// The start of every command runs no git process (T128a start cost, and
// L888: a program found on PATH may be a plant): the repository is read from
// the disk, also where its config moves the top folder for git. A `git`
// first on PATH logs its calls.
func TestRunStartsNoGitProcess(t *testing.T) {
	b := newBox(t)
	wt := filepath.Join(b.home, "project-worktrees", "example-change")
	support.Git(t, b.proj, "worktree", "add", "-q", "-b", "example-change", wt)
	deep := filepath.Join(b.proj, "a", "b")
	moved := filepath.Join(b.home, "moved")
	for _, d := range []string{deep, moved} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	support.GitRepo(t, moved)
	support.Git(t, moved, "config", "core.worktree", b.proj)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(b.tmp, "git.log")
	support.Write(t, b.base, map[string]string{"git-bin/git": "#!/bin/sh\necho \"$*\" >> '" + log + "'\nexec '" + real + "' \"$@\"\n"})
	if err := os.Chmod(filepath.Join(b.base, "git-bin", "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := append(append([]string{}, b.env...), "PATH="+filepath.Join(b.base, "git-bin")+":"+os.Getenv("PATH"))
	for _, dir := range []string{b.proj, deep, wt, moved, b.home} {
		if r := support.Jhon(t, dir, env, "sandbox", "run", "--", "true"); r.Code != 0 || r.Out() != "" {
			t.Fatalf("in %s: exit %d, %q", dir, r.Code, r.Out())
		}
	}
	if exists(log) {
		t.Errorf("jhon sandbox run started git:\n%s", support.Read(t, log))
	}
	// The log sees a git process: the program's own.
	if r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "git", "--version"); r.Code != 0 || !exists(log) || support.Read(t, log) != "--version\n" {
		t.Errorf("the program's own git: exit %d, the log exists: %v", r.Code, exists(log))
	}
	// The areas are the ones git names, under a PATH that holds bubblewrap
	// and no other program: a write from the linked worktree to the main one,
	// and to the common dir.
	r := support.Jhon(t, wt, onlyBwrap(t, b), "sandbox", "run", "--", "/bin/sh", "-c", `echo x > "$HOME/project/main.txt" && echo x > "$HOME/project/.git/probe"`)
	if r.Code != 0 || !exists(filepath.Join(b.proj, "main.txt")) || !exists(filepath.Join(b.proj, ".git", "probe")) {
		t.Errorf("a linked worktree's writes to the main worktree and the common dir: exit %d, %q", r.Code, r.Stderr)
	}
}
