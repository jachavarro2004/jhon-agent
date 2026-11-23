package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// disk is a fake disk: each path that exists, "dir" or "file", or "-> <path>"
// for a symlink.
type disk map[string]string

func (d disk) stat(path string) (string, bool, bool) {
	for range 8 {
		kind, ok := d[path]
		if !ok {
			return "", false, false
		}
		target, link := strings.CutPrefix(kind, "-> ")
		if !link {
			return path, kind == "dir", true
		}
		path = target
	}
	return "", false, false
}

// sample is a project at /home/u/project on a machine with a few of the
// paths the sandbox knows.
func sample(d disk) Input {
	return Input{
		Cwd:   "/home/u/project",
		Roots: []string{"/home/u/project", "/home/u/project-worktrees"}, CommonDir: "/home/u/project/.git",
		Tmp: []string{"/tmp"}, Home: "/home/u",
		Env:       map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000", "SSH_AUTH_SOCK": "/run/user/1000/agent.sock"},
		Protected: []string{"/home/u/.config/jhon/settings.toml", "/home/u/.local/state/jhon", "/home/u/.config/jhon"},
		Telemetry: "/home/u/.local/share/jhon/telemetry",
		Stat:      d.stat,
	}
}

var machine = disk{
	"/home/u": "dir", "/home/u/project": "dir", "/home/u/project/.git": "dir", "/tmp": "dir", "/home/u/.cache": "dir",
	"/home/u/.config/jhon": "dir", "/home/u/.config/jhon/settings.toml": "file", "/home/u/.local/state/jhon": "dir",
	"/home/u/.local/share/jhon": "dir", "/home/u/.local/share/jhon/telemetry": "dir",
	"/home/u/.ssh": "dir", "/home/u/.netrc": "file", "/home/u/.config/gh": "dir",
	"/run/user/1000": "dir", "/run/user/1000/bus": "file", "/run/user/1000/agent.sock": "file",
}

func with(d disk, more disk) disk {
	out := disk{}
	for _, m := range []disk{d, more} {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func TestArgsOfTheWorkProfile(t *testing.T) {
	got := strings.Join(Args(sample(machine)), " ")
	want := "--ro-bind / / --dev-bind /dev /dev" +
		" --bind /tmp /tmp" +
		" --bind /home/u/.cache /home/u/.cache" +
		" --dev-bind /dev/null /home/u/.netrc" +
		" --tmpfs /home/u/.ssh" +
		" --bind /home/u/project /home/u/project" +
		" --perms 0700 --tmpfs /run/user/1000" +
		" --tmpfs /home/u/.config/gh" +
		" --bind /home/u/.local/share/jhon/telemetry /home/u/.local/share/jhon/telemetry" +
		" --remount-ro /home/u/.ssh --remount-ro /home/u/.config/gh" +
		" --unsetenv SSH_AUTH_SOCK" +
		" --setenv JHON_IN_SANDBOX 1" +
		" --die-with-parent --chdir /home/u/project"
	if got != want {
		t.Errorf("Args(work) =\n%s\nwant\n%s", got, want)
	}
}

// index is where the mount of path starts in args, -1 when it has none.
func index(args []string, flag, path string) int {
	for i := range args {
		if args[i] == flag && i+1 < len(args) && args[len(args)-1] != flag && (args[i+1] == path || (flag == "--dev-bind" && i+2 < len(args) && args[i+2] == path)) {
			return i
		}
	}
	return -1
}

func TestWhereAreasNestTheInnerOneIsMountedLast(t *testing.T) {
	in := sample(with(machine, disk{"/home/u/.config": "dir", "/home/u/.config/git/credentials": "file"}))
	// The owner opens ~/.config: Jhon's folder in it stays read-only, the
	// credentials in it hidden.
	in.AllowWrite = []string{"/home/u/.config"}
	args := Args(in)
	open := index(args, "--bind", "/home/u/.config")
	for _, inner := range [][2]string{
		{"--ro-bind", "/home/u/.config/jhon"}, {"--tmpfs", "/home/u/.config/gh"}, {"--dev-bind", "/home/u/.config/git/credentials"},
	} {
		if at := index(args, inner[0], inner[1]); open < 0 || at < open {
			t.Errorf("%s %s is at %d, the writable ~/.config at %d; want it after:\n%s", inner[0], inner[1], at, open, strings.Join(args, " "))
		}
	}
	// The settings file inside the read-only folder takes no mount of its own.
	if index(args, "--ro-bind", "/home/u/.config/jhon/settings.toml") >= 0 {
		t.Errorf("a read-only file inside a read-only folder is mounted again:\n%s", strings.Join(args, " "))
	}
	// A read-only share with the writable telemetry folder in it: with the
	// share outside every writable area only the telemetry is mounted; inside
	// one (a home under the scratch root), the share first.
	if index(args, "--ro-bind", "/home/u/.local/share/jhon") >= 0 || index(args, "--bind", "/home/u/.local/share/jhon/telemetry") < 0 {
		t.Errorf("the telemetry folder is not writable inside the share:\n%s", strings.Join(args, " "))
	}
	in.AllowWrite = []string{"/home/u/.local"}
	in.Stat = with(machine, disk{"/home/u/.local": "dir"}).stat
	args = Args(in)
	local, share, telemetry := index(args, "--bind", "/home/u/.local"), index(args, "--ro-bind", "/home/u/.local/share/jhon"), index(args, "--bind", "/home/u/.local/share/jhon/telemetry")
	state := index(args, "--ro-bind", "/home/u/.local/state/jhon")
	if !(local >= 0 && local < share && share < telemetry && local < state) {
		t.Errorf("~/.local at %d, the share at %d, the telemetry at %d, the state at %d; want that order:\n%s", local, share, telemetry, state, strings.Join(args, " "))
	}
}

func TestAHiddenFolderHoldsNothing(t *testing.T) {
	d := with(machine, disk{"/home/u/.ssh/work": "dir", "/run/user/1000/keyring": "dir", "/run/user/1000/keyring/ssh": "file", "/run/user/1000/scratch": "dir"})
	in := sample(d)
	in.AllowWrite = []string{"/home/u/.ssh/work", "/run/user/1000/scratch"}
	in.Env["SSH_AUTH_SOCK"] = "/run/user/1000/keyring/ssh"
	args := Args(in)
	for _, inside := range []string{"/home/u/.ssh/work", "/run/user/1000/keyring/ssh", "/run/user/1000/keyring", "/run/user/1000/scratch"} {
		if strings.Contains(strings.Join(args, " ")+" ", " "+inside+" ") {
			t.Errorf("work mounts %s, inside a hidden folder:\n%s", inside, strings.Join(args, " "))
		}
	}
}

func TestAMissingPathTakesNoMountAndALinkIsBoundWhereItLeads(t *testing.T) {
	d := disk{
		"/home/u": "dir", "/home/u/project": "dir", "/tmp": "dir",
		// ~/.aws and the cache are links.
		"/home/u/.aws": "-> /mnt/vault/aws", "/mnt/vault/aws": "dir",
		"/home/u/.cache": "-> /mnt/big/cache", "/mnt/big/cache": "dir",
		"/home/u/.npmrc": "-> /home/u/project/dotfiles/npmrc", "/home/u/project/dotfiles/npmrc": "file",
	}
	in := sample(d)
	in.AllowWrite = []string{"/opt/missing"}
	got := strings.Join(Args(in), " ")
	want := "--ro-bind / / --dev-bind /dev /dev" +
		" --bind /tmp /tmp" +
		" --bind /home/u/project /home/u/project" +
		" --bind /mnt/big/cache /mnt/big/cache" +
		" --tmpfs /mnt/vault/aws" +
		" --dev-bind /dev/null /home/u/project/dotfiles/npmrc" +
		" --remount-ro /mnt/vault/aws" +
		" --unsetenv SSH_AUTH_SOCK" +
		" --setenv JHON_IN_SANDBOX 1" +
		" --die-with-parent --chdir /home/u/project"
	if got != want {
		t.Errorf("Args =\n%s\nwant\n%s", got, want)
	}
}

func TestAWritableAreaIsNeverTheRootOrTheHome(t *testing.T) {
	in := sample(with(machine, disk{"/": "dir", "/home": "dir"}))
	in.AllowWrite = []string{"/", "/home", "/home/u", "relative"}
	in.Tmp = []string{"/tmp", "/home/u"}
	args := Args(in)
	for _, p := range []string{"/", "/home", "/home/u"} {
		if index(args, "--bind", p) >= 0 {
			t.Errorf("%s is writable:\n%s", p, strings.Join(args, " "))
		}
	}
}

// The default list is the toolchains' caches and stores, each where its
// variable puts it; the folders with their programs take no mount (F2).
func TestToolFoldersAreCachesAndStoresAndFollowTheirVariables(t *testing.T) {
	d := with(machine, disk{
		"/home/u/go": "dir", "/home/u/go/bin": "dir", "/home/u/go/pkg": "dir",
		"/home/u/.cargo": "dir", "/home/u/.cargo/bin": "dir", "/home/u/.cargo/registry": "dir", "/home/u/.cargo/git": "dir", "/home/u/.cargo/credentials.toml": "file",
		"/home/u/.rustup": "dir", "/home/u/.bun": "dir", "/home/u/.bun/install/cache": "dir",
		"/home/u/.local/share/pnpm": "dir", "/home/u/.local/share/pnpm/store": "dir",
		"/home/u/.local/share/mise": "dir", "/home/u/.local/share/mise/shims": "dir", "/home/u/.local/state/mise": "dir",
		"/home/u/.npm": "dir", "/home/u/.gradle": "dir", "/home/u/.m2/repository": "dir", "/home/u/.nuget/packages": "dir",
		"/data/gopath": "dir", "/data/gopath/pkg": "dir", "/data/config": "dir", "/data/config/gh": "dir",
	})
	in := sample(d)
	args := Args(in)
	for _, open := range []string{"/home/u/go/pkg", "/home/u/.cargo/registry", "/home/u/.cargo/git", "/home/u/.bun/install/cache", "/home/u/.local/share/pnpm/store", "/home/u/.npm", "/home/u/.gradle", "/home/u/.m2/repository", "/home/u/.nuget/packages"} {
		if index(args, "--bind", open) < 0 {
			t.Errorf("%s is not writable:\n%s", open, strings.Join(args, " "))
		}
	}
	for _, closed := range []string{"/home/u/go", "/home/u/go/bin", "/home/u/.cargo", "/home/u/.cargo/bin", "/home/u/.rustup", "/home/u/.bun", "/home/u/.local/share/pnpm", "/home/u/.local/share/mise", "/home/u/.local/share/mise/shims", "/home/u/.local/state/mise"} {
		if index(args, "--bind", closed) >= 0 {
			t.Errorf("%s is writable:\n%s", closed, strings.Join(args, " "))
		}
	}
	if index(args, "--dev-bind", "/home/u/.cargo/credentials.toml") < 0 {
		t.Errorf("cargo's token is not hidden:\n%s", strings.Join(args, " "))
	}
	in.Env["GOPATH"], in.Env["XDG_CONFIG_HOME"] = "/data/gopath", "/data/config"
	args = Args(in)
	if index(args, "--bind", "/data/gopath/pkg") < 0 || index(args, "--bind", "/data/gopath") >= 0 || index(args, "--bind", "/home/u/go/pkg") >= 0 {
		t.Errorf("GOPATH moves go's module folder, and only it:\n%s", strings.Join(args, " "))
	}
	if index(args, "--tmpfs", "/data/config/gh") < 0 {
		t.Errorf("gh's folder under a moved XDG_CONFIG_HOME is not hidden:\n%s", strings.Join(args, " "))
	}
}

// A PATH folder outside the project is read-only: inside a writable folder
// it is mounted after it, outside every one it is the machine's already, and
// one in the project is the project's (F2).
func TestAPATHFolderOutsideTheProjectIsReadOnly(t *testing.T) {
	d := with(machine, disk{
		"/usr/bin": "dir", "/home/u/.gradle": "dir", "/home/u/.gradle/bin": "dir", "/home/u/tools": "dir", "/home/u/tools/bin": "dir",
		"/home/u/project/node_modules/.bin": "dir", "/home/u/.cache/bin": "dir",
	})
	in := sample(d)
	in.AllowWrite = []string{"/home/u/tools", "/home/u/tools/bin"}
	in.Path = []string{"/home/u/.gradle/bin", "/home/u/tools/bin", "/home/u/.cache/bin", "/home/u/project/node_modules/.bin", "/usr/bin", "/opt/missing/bin", "relative"}
	args := Args(in)
	for _, c := range [][2]string{{"/home/u/.gradle", "/home/u/.gradle/bin"}, {"/home/u/tools", "/home/u/tools/bin"}, {"/home/u/.cache", "/home/u/.cache/bin"}} {
		if outer, inner := index(args, "--bind", c[0]), index(args, "--ro-bind", c[1]); !(outer >= 0 && outer < inner) || index(args, "--bind", c[1]) >= 0 {
			t.Errorf("%s writable at %d, the PATH folder %s read-only at %d; want that order:\n%s", c[0], outer, c[1], inner, strings.Join(args, " "))
		}
	}
	for _, none := range []string{"/usr/bin", "/home/u/project/node_modules/.bin", "/opt/missing/bin", "relative"} {
		if strings.Contains(strings.Join(args, " ")+" ", " "+none+" ") {
			t.Errorf("%s takes a mount:\n%s", none, strings.Join(args, " "))
		}
	}
}

// The sandbox unsets each credential variable that is set (F4).
func TestTheSandboxUnsetsTheCredentialVariables(t *testing.T) {
	in := sample(machine)
	in.Env = map[string]string{"GH_TOKEN": "x", "GITHUB_TOKEN": "", "GIT_ASKPASS": "/usr/bin/ask", "HOME": "/home/u", "ANTHROPIC_API_KEY": "x"}
	got := strings.Join(Args(in), " ")
	if !strings.Contains(got, " --unsetenv GH_TOKEN --unsetenv GITHUB_TOKEN --unsetenv GIT_ASKPASS --setenv JHON_IN_SANDBOX 1 --die-with-parent") || strings.Count(got, "--unsetenv") != 3 {
		t.Errorf("work unsets the credential variables that are set, and only them:\n%s", got)
	}
	if want := []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "SSH_AUTH_SOCK", "GIT_ASKPASS", "SSH_ASKPASS"}; !slices.Equal(CredentialVariables, want) {
		t.Errorf("CredentialVariables = %v", CredentialVariables)
	}
}

// The snapshot folders are hidden when they exist (F5).
func TestTheSnapshotFoldersAreHidden(t *testing.T) {
	folders := []string{"/.snapshots", "/home/.snapshots", "/home/u/.snapshots", "/timeshift", "/run/timeshift"}
	d := with(machine, disk{})
	for _, f := range folders {
		d[f] = "dir"
	}
	args := Args(sample(d))
	for _, f := range folders {
		if index(args, "--tmpfs", f) < 0 || index(args, "--remount-ro", f) < 0 {
			t.Errorf("%s is not an empty read-only folder:\n%s", f, strings.Join(args, " "))
		}
	}
	if got := strings.Join(Args(sample(machine)), " "); strings.Contains(got, "snapshots") || strings.Contains(got, "timeshift") {
		t.Errorf("a snapshot folder that does not exist takes a mount:\n%s", got)
	}
}

// The runtime dir is a private folder with the container sockets shown in
// it; the socket folders outside it and the X authority file are hidden
// (F6).
func TestTheSessionSocketsAreNotThere(t *testing.T) {
	d := with(machine, disk{
		"/run/user/1000/docker.sock": "file", "/run/user/1000/podman": "dir", "/run/user/1000/podman/podman.sock": "file", "/run/user/1000/wayland-1": "file",
		"/tmp/tmux-1000": "dir", "/tmp/.X11-unix": "dir", "/tmp/.ICE-unix": "dir", "/home/u/.Xauthority": "file",
	})
	in := sample(d)
	in.SocketFolders = []string{"/tmp/tmux-1000", "/tmp/.X11-unix", "/tmp/.ICE-unix"}
	args := Args(in)
	got := strings.Join(args, " ")
	run := index(args, "--tmpfs", "/run/user/1000")
	if run < 1 || args[run-2] != "--perms" || args[run-1] != "0700" || index(args, "--remount-ro", "/run/user/1000") >= 0 {
		t.Errorf("the runtime dir is not a private writable folder of mode 0700:\n%s", got)
	}
	for _, sock := range []string{"/run/user/1000/docker.sock", "/run/user/1000/podman/podman.sock"} {
		if at := index(args, "--ro-bind", sock); at < run {
			t.Errorf("%s is at %d, the runtime dir at %d; want it shown after:\n%s", sock, at, run, got)
		}
	}
	if strings.Contains(got, "wayland-1") || strings.Contains(got, "/run/user/1000/podman ") {
		t.Errorf("something else of the runtime dir is mounted:\n%s", got)
	}
	tmp := index(args, "--bind", "/tmp")
	for _, f := range in.SocketFolders {
		if at := index(args, "--tmpfs", f); at < tmp || index(args, "--remount-ro", f) < 0 {
			t.Errorf("%s is not an empty read-only folder inside /tmp:\n%s", f, got)
		}
	}
	if index(args, "--dev-bind", "/home/u/.Xauthority") < 0 {
		t.Errorf("the X authority file is not hidden:\n%s", got)
	}
}

// $TMPDIR is bound only where a scratch root may be; any other value is not
// bound and becomes /tmp for the program (F7).
func TestTMPDIRIsBoundOnlyWhereAScratchRootMayBe(t *testing.T) {
	d := with(machine, disk{
		"/": "dir", "/home": "dir", "/tmp/session": "dir", "/var/tmp": "dir", "/var/tmp/x": "dir", "/home/u/.cache/tmp": "dir", "/home/u/tmp": "dir",
		"/proc/sys": "dir", "/opt/scratch": "dir", "/opt/scratch/tmp": "dir", "/home/u/.ssh/tmp": "dir", "/home/u/.config/jhon/tmp": "dir",
		"/home/u/.claude": "dir", "/home/u/.claude/tmp": "dir", "/run/user/1000/tmp": "dir", "/home/u/link": "-> /proc/sys",
	})
	for _, c := range []struct {
		tmpdir string
		bound  bool
	}{
		{"/tmp", true}, {"/tmp/session", true}, {"/var/tmp", true}, {"/var/tmp/x", true}, {"/home/u/tmp", true}, {"/home/u/.cache/tmp", true}, {"/opt/scratch/tmp", true},
		{"/proc/sys", false}, {"/home/u/link", false}, {"/", false}, {"/home", false}, {"/home/u", false}, {"relative", false}, {"/opt/missing", false},
		{"/home/u/.ssh/tmp", false}, {"/home/u/.config/jhon/tmp", false}, {"/home/u/.claude/tmp", false}, {"/run/user/1000/tmp", false},
	} {
		in := sample(d)
		in.AllowWrite = []string{"/opt/scratch"}
		in.Env["TMPDIR"] = c.tmpdir
		in.Tmp = []string{"/tmp", c.tmpdir}
		args := Args(in)
		got := strings.Join(args, " ")
		// Writable: bound itself, or inside a writable area with nothing
		// mounted over it.
		real, _, _ := d.stat(c.tmpdir)
		writable := false
		for i, a := range args {
			if a == "--bind" && real != "" && (real == args[i+1] || strings.HasPrefix(real, args[i+1]+"/")) {
				writable = true
			}
		}
		if writable != c.bound || strings.Contains(got, "--setenv TMPDIR /tmp") == c.bound {
			t.Errorf("TMPDIR=%s: writable %v, want %v, and TMPDIR=/tmp set only when it is not:\n%s", c.tmpdir, writable, c.bound, got)
		}
	}
	// No TMPDIR: nothing to set.
	if got := strings.Join(Args(sample(d)), " "); strings.Contains(got, "--setenv TMPDIR") {
		t.Errorf("with no TMPDIR, Args sets one:\n%s", got)
	}
}

// T217 S4: every sandbox marks what runs in it with JHON_IN_SANDBOX=1, once,
// so `jhon sandbox status` run inside knows it is inside; the marker is the
// only variable Args sets besides a TMPDIR that is not bound.
func TestArgsMarkWhatRunsInsideTheSandbox(t *testing.T) {
	for _, in := range []Input{sample(machine), func() Input { i := sample(machine); i.Env["TMPDIR"] = "/proc/sys"; return i }()} {
		got := strings.Join(Args(in), " ")
		if strings.Count(got, " --setenv JHON_IN_SANDBOX 1 ") != 1 || strings.Count(got, "--setenv")-strings.Count(got, "--setenv TMPDIR") != 1 {
			t.Errorf("Args sets JHON_IN_SANDBOX=1 once, and nothing else but TMPDIR:\n%s", got)
		}
	}
}

// realSandbox is this machine's bubblewrap and a scratch folder's real path,
// skipping the test where bubblewrap cannot run.
func realSandbox(t *testing.T) (bwrap, base string) {
	t.Helper()
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("skipped: no bubblewrap on this machine")
	}
	if out, err := exec.Command(bwrap, "--ro-bind", "/", "/", "true").CombinedOutput(); err != nil {
		t.Skipf("skipped: bubblewrap cannot run on this machine (%v: %s)", err, strings.TrimSpace(string(out)))
	}
	if base, err = filepath.EvalSymlinks(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	return bwrap, base
}

// The user's runtime dir (/run/user/<uid>) is private whether or not
// $XDG_RUNTIME_DIR names it; where the variable names another folder, both
// are, each with the container sockets and never the agent socket (L888
// continuation, item 2).
func TestTheUsersRuntimeDirIsPrivateWhateverTheVariableSays(t *testing.T) {
	d := with(machine, disk{
		"/run/user/1000/docker.sock": "file", "/run/user/1000/wayland-1": "file",
		"/data/run": "dir", "/data/run/podman": "dir", "/data/run/podman/podman.sock": "file", "/data/run/agent.sock": "file", "/data/run/bus": "file",
	})
	for _, c := range []struct {
		name, variable, agent string
		private               []string
	}{
		{"the variable unset", "", "/run/user/1000/agent.sock", []string{"/run/user/1000"}},
		{"the variable naming it", "/run/user/1000", "/run/user/1000/agent.sock", []string{"/run/user/1000"}},
		{"the variable naming another folder", "/data/run", "/data/run/agent.sock", []string{"/data/run", "/run/user/1000"}},
		{"another folder, the agent in the user's", "/data/run", "/run/user/1000/agent.sock", []string{"/data/run", "/run/user/1000"}},
		{"a variable that names no folder", "relative", "/run/user/1000/agent.sock", []string{"/run/user/1000"}},
	} {
		in := sample(d)
		in.RuntimeDir = "/run/user/1000"
		in.Env = map[string]string{"SSH_AUTH_SOCK": c.agent}
		if c.variable != "" {
			in.Env["XDG_RUNTIME_DIR"] = c.variable
		}
		args := Args(in)
		got := strings.Join(args, " ")
		for _, p := range c.private {
			if at := index(args, "--tmpfs", p); at < 2 || args[at-2] != "--perms" || args[at-1] != "0700" || index(args, "--remount-ro", p) >= 0 {
				t.Errorf("%s: %s is not a private writable folder of mode 0700:\n%s", c.name, p, got)
			}
		}
		if n := strings.Count(got, "--perms 0700 --tmpfs"); n != len(c.private) {
			t.Errorf("%s: %d private folders, want %d:\n%s", c.name, n, len(c.private), got)
		}
		if docker := index(args, "--ro-bind", "/run/user/1000/docker.sock"); docker < index(args, "--tmpfs", "/run/user/1000") {
			t.Errorf("%s: docker's socket is not shown in the user's runtime dir:\n%s", c.name, got)
		}
		if c.variable == "/data/run" && index(args, "--ro-bind", "/data/run/podman/podman.sock") < index(args, "--tmpfs", "/data/run") {
			t.Errorf("%s: podman's socket is not shown in the variable's folder:\n%s", c.name, got)
		}
		if agent := index(args, "--ro-bind", c.agent); agent >= 0 || strings.Contains(got, "wayland-1") || strings.Contains(got, "/data/run/bus") {
			t.Errorf("%s: the agent socket at %d, hidden, and nothing else of a runtime dir:\n%s", c.name, agent, got)
		}
	}
	// A machine with no such folder: nothing to hide.
	in := sample(disk{"/home/u": "dir", "/home/u/project": "dir", "/tmp": "dir"})
	in.RuntimeDir, in.Env = "/run/user/1000", map[string]string{}
	if got := strings.Join(Args(in), " "); strings.Contains(got, "/run/user") {
		t.Errorf("a runtime dir that does not exist takes a mount:\n%s", got)
	}
}

// The same inside a real sandbox, against fake folders under the test's own
// scratch: both runtime dirs are empty but for docker's socket, mode 0700 and
// writable, and nothing written in them reaches the real ones.
func TestTheUsersRuntimeDirIsPrivateInARealSandbox(t *testing.T) {
	bwrap, base := realSandbox(t)
	users, moved, home := filepath.Join(base, "run-user"), filepath.Join(base, "run-moved"), filepath.Join(base, "home", "u")
	for _, d := range []string{users, moved, home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{"wayland-1", "docker.sock"} {
			if err := os.WriteFile(filepath.Join(d, f), []byte("x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	look := `for d in "$@"; do stat -c %a "$d"; ls -A "$d"; echo x > "$d/new" || exit 1; done`
	for _, c := range []struct {
		name string
		env  map[string]string
	}{
		{"the variable unset", map[string]string{}},
		{"the variable naming another folder", map[string]string{"XDG_RUNTIME_DIR": moved}},
	} {
		in := Input{Cwd: base, Home: home, RuntimeDir: users, Env: c.env, Stat: Disk}
		folders := []string{users}
		if c.env["XDG_RUNTIME_DIR"] != "" {
			folders = append(folders, moved)
		}
		cmd := exec.Command(bwrap, append(append(Args(in), "--", "sh", "-c", look, "sh"), folders...)...)
		cmd.WaitDelay = 10 * time.Second
		out, err := cmd.CombinedOutput()
		if want := strings.Repeat("700\ndocker.sock\n", len(folders)); err != nil || string(out) != want {
			t.Errorf("%s: %v, %q; want %q", c.name, err, out, want)
		}
		for _, d := range folders {
			if _, err := os.Stat(filepath.Join(d, "new")); err == nil {
				t.Errorf("%s: a file written in the private %s reached the real one", c.name, d)
			}
		}
	}
}

// A socket folder is empty and read-only inside a real sandbox, and the
// scratch root around it stays writable (F6): fake folders under the test's
// own scratch, never the machine's.
func TestASocketFolderIsEmptyAndReadOnlyInARealSandbox(t *testing.T) {
	bwrap, base := realSandbox(t)
	tmp, home := filepath.Join(base, "tmp"), filepath.Join(base, "home", "u")
	folders := []string{filepath.Join(tmp, "tmux-1000"), filepath.Join(tmp, ".X11-unix"), filepath.Join(tmp, ".ICE-unix")}
	for _, d := range append([]string{home}, folders...) {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range folders {
		if err := os.WriteFile(filepath.Join(f, "socket"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	in := Input{Cwd: tmp, Home: home, AllowWrite: []string{tmp}, SocketFolders: folders, Env: map[string]string{}, Stat: Disk}
	for _, f := range folders {
		cmd := exec.Command(bwrap, append(Args(in), "--", "sh", "-c", `ls -A "$1" | wc -l; echo x > ../tmp/beside && echo x > "$1/new"`, "sh", f)...)
		cmd.WaitDelay = 10 * time.Second
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.HasPrefix(strings.TrimSpace(string(out)), "0\n") || !strings.Contains(string(out), "Read-only file system") {
			t.Errorf("%s inside: %v, %q; want it empty and read-only", f, err, out)
		}
		if _, err := os.Stat(filepath.Join(f, "new")); err == nil {
			t.Errorf("%s was written", f)
		}
	}
	if _, err := os.Stat(filepath.Join(tmp, "beside")); err != nil {
		t.Errorf("the scratch root around the socket folders is not writable: %v", err)
	}
}

// mode is a file's os.FileInfo with nothing but its mode.
type mode os.FileMode

func (m mode) Name() string       { return "" }
func (m mode) Size() int64        { return 0 }
func (m mode) Mode() os.FileMode  { return os.FileMode(m) }
func (m mode) ModTime() time.Time { return time.Time{} }
func (m mode) IsDir() bool        { return os.FileMode(m).IsDir() }
func (m mode) Sys() any           { return nil }

// host is a fake machine for Check: every file of d is executable, but the
// ones named in plain.
func host(goos string, d disk, files map[string]string, plain ...string) Host {
	return Host{
		OS: goos,
		ReadFile: func(p string) ([]byte, error) {
			if v, ok := files[p]; ok {
				return []byte(v), nil
			}
			return nil, os.ErrNotExist
		},
		Stat: func(p string) (os.FileInfo, error) {
			if _, _, ok := d.stat(p); !ok {
				return nil, os.ErrNotExist
			}
			if slices.Contains(plain, p) {
				return mode(0o644), nil
			}
			return mode(0o755), nil
		},
	}
}

func TestCheckNamesWhatIsMissingAndItsFix(t *testing.T) {
	installed := with(machine, disk{"/usr/bin": "dir", "/usr/bin/bwrap": "file"})
	path := []string{"/usr/local/bin", "/usr/bin"}
	for _, c := range []struct {
		name   string
		host   Host
		disk   disk
		reason string
		fix    string
	}{
		{"linux with bubblewrap", host("linux", installed, map[string]string{"/proc/sys/user/max_user_namespaces": "1000\n"}), installed, "", ""},
		{"no bubblewrap", host("linux", machine, nil), machine, "bubblewrap (bwrap) is not on PATH", "install bubblewrap"},
		{"a bwrap that is no program", host("linux", installed, nil, "/usr/bin/bwrap"), installed, "bubblewrap (bwrap) is not on PATH", "install bubblewrap"},
		{"no user namespaces", host("linux", installed, map[string]string{"/proc/sys/user/max_user_namespaces": "0\n"}), installed, "unprivileged user namespaces are off (user.max_user_namespaces = 0)", "sysctl"},
		{"user namespaces for root only", host("linux", installed, map[string]string{"/proc/sys/kernel/unprivileged_userns_clone": "0\n"}), installed, "unprivileged user namespaces are off (kernel.unprivileged_userns_clone = 0)", "sysctl"},
		{"macOS", host("darwin", installed, nil), installed, "the sandbox does not run on this system (darwin)", "it needs Linux"},
		{"Windows", host("windows", machine, nil), machine, "the sandbox does not run on this system (windows)", "it needs Linux"},
	} {
		in := sample(c.disk)
		in.Path = path
		bwrap, p := Check(c.host, in)
		if c.reason == "" {
			if p != nil || bwrap != "/usr/bin/bwrap" {
				t.Errorf("%s: %q, %+v; want /usr/bin/bwrap and no problem", c.name, bwrap, p)
			}
			continue
		}
		if p == nil || bwrap != "" || p.Reason != c.reason || !strings.Contains(p.Fix, c.fix) || strings.Contains(p.Line(), "\n") {
			t.Errorf("%s: %q, %+v; want %q and a fix naming %q, on one line", c.name, bwrap, p, c.reason, c.fix)
		}
	}
}

// bubblewrap is the first on PATH in a folder no sandboxed command may
// write, under any PATH: not one in the project, a scratch root, a cache, a
// folder the owner opened or /dev/shm, and not a link into one (F1).
func TestCheckTakesBubblewrapOnlyFromAFolderNoCommandMayWrite(t *testing.T) {
	d := with(machine, disk{
		"/usr/bin": "dir", "/usr/bin/bwrap": "file", "/bin": "-> /usr/bin", "/bin/bwrap": "-> /usr/bin/bwrap",
		"/home/u/project/bin": "dir", "/home/u/project/bin/bwrap": "file", "/tmp/bin": "dir", "/tmp/bin/bwrap": "file",
		"/home/u/.cache/bin": "dir", "/home/u/.cache/bin/bwrap": "file", "/dev/shm/bin": "dir", "/dev/shm/bin/bwrap": "file",
		"/home/u/tools": "dir", "/home/u/tools/bin": "dir", "/home/u/tools/bin/bwrap": "file",
		"/usr/local/bin": "dir", "/usr/local/bin/bwrap": "-> /home/u/project/bin/bwrap",
		"/home/u/.local/bin": "dir", "/home/u/.local/bin/bwrap": "file",
	})
	planted := []string{"/home/u/project/bin", "/tmp/bin", "/home/u/.cache/bin", "/dev/shm/bin", "/home/u/tools/bin", "/usr/local/bin"}
	for _, c := range []struct {
		name  string
		path  []string
		bwrap string
	}{
		{"after the planted ones", append(slices.Clone(planted), "/usr/bin"), "/usr/bin/bwrap"},
		{"through a linked folder", append(slices.Clone(planted), "relative", "/bin"), "/usr/bin/bwrap"},
		{"a folder of the home no area opens", append(slices.Clone(planted), "/home/u/.local/bin", "/usr/bin"), "/home/u/.local/bin/bwrap"},
		{"only planted ones", planted, ""},
	} {
		in := sample(d)
		in.AllowWrite = []string{"/home/u/tools"}
		// The folder is on PATH, so the sandbox binds it read-only: a
		// command may still have written it under another PATH.
		in.Path = c.path
		bwrap, p := Check(host("linux", d, nil), in)
		if bwrap != c.bwrap {
			t.Errorf("%s: bwrap %q, %+v; want %q", c.name, bwrap, p, c.bwrap)
		}
		if want := "bubblewrap (bwrap) is on PATH only in a folder a sandboxed command may write (/home/u/project/bin)"; c.bwrap == "" && (p == nil || p.Reason != want || !strings.Contains(p.Fix, "outside the project")) {
			t.Errorf("%s: %+v; want %q and the fix", c.name, p, want)
		}
	}
}

// The probe gives bubblewrap one of every option a run may give it, so a
// bubblewrap that fails on one fails the probe: status and doctor agree with
// run (L888 continuation, item 3).
func TestTheProbeUsesEveryOptionARunMayUse(t *testing.T) {
	d := with(machine, disk{
		"/home/u/.gradle": "dir", "/home/u/.gradle/bin": "dir", "/home/u/.snapshots": "dir", "/home/u/.Xauthority": "file",
		"/run/user/1000/docker.sock": "file", "/proc/sys": "dir",
	})
	in := sample(d)
	in.Path, in.RuntimeDir = []string{"/home/u/.gradle/bin"}, "/run/user/1000"
	in.Env["TMPDIR"], in.Env["GH_TOKEN"] = "/proc/sys", "x"
	for _, a := range Args(in) {
		if strings.HasPrefix(a, "--") && !slices.Contains(probeArgs, a) {
			t.Errorf("Args gives bubblewrap %s, which the probe does not try: %v", a, probeArgs)
		}
	}
	for _, option := range []string{"--ro-bind", "--dev-bind", "--bind", "--perms", "--tmpfs", "--remount-ro", "--unsetenv", "--setenv", "--die-with-parent", "--chdir"} {
		if !slices.Contains(probeArgs, option) {
			t.Errorf("the probe does not try %s: %v", option, probeArgs)
		}
	}
}
