// Package sandbox builds the bubblewrap arguments a model-chosen command
// runs under (T128a, odd/proposals/t128-sandbox.md section 4): the kernel,
// not the command's text, keeps its writes inside the project, the scratch
// roots and the caches, hides the credentials, and leaves Jhon's own files
// read-only.
//
// Args is pure: what it is given decides the arguments, and the disk is read
// only through Input.Stat. Current gathers an Input for a working directory
// from the project code that knows the roots, the worktrees and the scratch
// roots, reading the repository from the disk. The start runs no program but
// bubblewrap itself, and trusts nothing a sandboxed command may have written
// (L888, odd/proposals/t128a-verify-1.md): bubblewrap comes from a PATH folder
// outside every writable area (Check), and the project is where `.git` is,
// whatever its files say (project.ReadRepo). The protected home paths are the
// guard's (guard.ProtectedHome), the one thing this package takes from it.
package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/workspace"
)

// Work is bubblewrap's one profile (design section 4, D4). The delivery
// profile, which showed ~/.ssh, ~/.config/gh and the SSH agent socket, had
// no caller and is gone (T181, L1503): git over SSH and gh run with no
// sandbox, on a line of their own (ProfileFor).
const Work = "work"

// Input is everything the arguments depend on.
type Input struct {
	// Cwd is where the program starts.
	Cwd string
	// Roots are the project's roots (project.Repo's): the top folder, the
	// main worktree of a linked one, and each one's <root>-worktrees; in a
	// workspace, the primary's and the joined members' (workspace.DiskRoots).
	Roots []string
	// CommonDir is the git common dir: a linked worktree commits through
	// it, and it is outside the worktree's root.
	CommonDir string
	// Worktrees are the repository's other listed worktrees.
	Worktrees []string
	// Tmp are the scratch roots (project.TmpRoots): /tmp and $TMPDIR. Each
	// is bound only where a scratch root may be (layout).
	Tmp  []string
	Home string
	// Env is the environment the program gets: the XDG folders, the tool
	// caches a variable moves, TMPDIR, the runtime dir, the session bus and
	// the credential variables.
	Env map[string]string
	// Path are PATH's folders, in its order.
	Path []string
	// RuntimeDir is the user's runtime dir, /run/user/<uid>: private like
	// the one $XDG_RUNTIME_DIR names, whether or not the variable names it.
	RuntimeDir string
	// SocketFolders are the folders outside the runtime dir where the
	// terminal's and the desktop's programs keep their sockets, hidden when
	// they exist: Current gives tmux's, X11's and ICE's under /tmp.
	SocketFolders []string
	// AllowWrite is sandbox.allow_write, each folder absolute.
	AllowWrite []string
	// DeployTools is sandbox.deploy_tools: the programs, by bare name, a
	// line runs outside the sandbox as it runs docker (ProfileAt).
	DeployTools []string
	// Protected are Jhon's own files besides the framework paths under the
	// home: the settings, and the state folder with the grants and modes.
	Protected []string
	// Telemetry is the telemetry folder, writable inside the read-only share.
	Telemetry string
	// Stat is what path is on disk, through symlinks: its real path and
	// whether it is a directory; ok is false when it does not exist.
	Stat func(path string) (real string, dir, ok bool)
}

// toolCaches are the caches and stores the common toolchains write outside
// a project (go, cargo, npm, pnpm, bun, gradle, maven, nuget, pip, uv, mise):
// the variable that moves each and the folder under it, and its place under
// the home; either may be "". None holds the programs the owner or a runtime
// runs later (go's and cargo's bin, rustup, mise's installs and shims, pnpm's
// and bun's own folders), nor what decides which ones run (mise's state,
// with the configs it trusts): what a sandboxed command wrote there would
// run outside the sandbox (L888, F2). Those are the owner's
// sandbox.allow_write.
var toolCaches = []struct{ env, sub, home string }{
	{"GOPATH", "pkg", "go/pkg"}, {"GOMODCACHE", "", ""}, {"GOCACHE", "", ""},
	{"CARGO_HOME", "registry", ".cargo/registry"}, {"CARGO_HOME", "git", ".cargo/git"},
	{"npm_config_cache", "", ".npm"},
	{"PNPM_HOME", "store", ".local/share/pnpm/store"}, {"", "", ".pnpm-store"},
	{"BUN_INSTALL", "install/cache", ".bun/install/cache"},
	{"GRADLE_USER_HOME", "", ".gradle"},
	{"", "", ".m2/repository"},
	{"NUGET_PACKAGES", "", ".nuget/packages"},
	{"PIP_CACHE_DIR", "", ""}, {"UV_CACHE_DIR", "", ""},
	{"MISE_CACHE_DIR", "", ""},
}

// moreCredentials are the home paths that hold credentials besides the
// guard's (guard.ProtectedHome): the design's list, cargo's token, and the
// runtimes' auth files.
var moreCredentials = []string{
	".git-credentials", ".config/git/credentials", ".docker/config.json", ".kube", ".npmrc",
	".cargo/credentials.toml", ".cargo/credentials",
	".claude/.credentials.json", ".pi/agent/auth.json", ".pi/jhon/auth.json",
}

// runtimeHomes are the runtimes' and the skills' folders under the home,
// read-only wherever a writable area holds them.
var runtimeHomes = []string{".claude", ".pi", ".agents"}

// snapshots are where the snapshot tools keep copies of the file system,
// the home and its credentials included (snapper, timeshift); with
// .snapshots under the home, hidden when they exist (F5).
var snapshots = []string{"/.snapshots", "/home/.snapshots", "/timeshift", "/run/timeshift"}

// containerSockets are the sockets under $XDG_RUNTIME_DIR shown in the
// private one: docker's and podman's (docker is a non-goal of the design,
// with a guard of its own).
var containerSockets = []string{"docker.sock", "podman/podman.sock"}

// CredentialVariables are the variables that carry a credential: gh's and
// git's tokens, the SSH agent socket, and the programs git and ssh ask for
// a secret. The sandbox unsets them (F4, D4).
var CredentialVariables = []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "SSH_AUTH_SOCK", "GIT_ASKPASS", "SSH_ASKPASS"}

// A mount's kind, the weakest first: for one path the strongest wins.
const (
	write   = iota // a writable bind
	read           // a read-only bind
	hide           // an empty read-only folder, or an empty file
	private        // an empty writable folder of the sandbox's own, mode 0700
)

type mount struct {
	path string
	kind int
	dir  bool
	// shown is a path shown inside a sealed folder: a container socket.
	shown bool
	// sealed is a credential path or a folder of sockets, hidden: nothing
	// else is mounted inside it.
	sealed bool
	// guarded is one of Jhon's own or the runtimes' paths, read-only: no
	// scratch root is bound inside it.
	guarded bool
}

// under is rel under the home.
func (in Input) under(rel string) string { return filepath.Join(in.Home, rel) }

// xdg is the XDG folder a variable names, or its default under the home.
func (in Input) xdg(name, rel string) string {
	if v := in.Env[name]; filepath.IsAbs(v) {
		return v
	}
	return in.under(rel)
}

// within is whether p is one of areas or inside one.
func within(p string, areas []string) bool {
	return slices.ContainsFunc(areas, func(a string) bool { return project.Within(p, a) })
}

// layout is the areas of in, parents first, and whether $TMPDIR is one of
// the writable ones. Every area is there only when it exists, at its real
// path. withPath is whether PATH's folders are among them: without, the
// writable areas are the ones a command may have written under any PATH.
//
// A scratch root ($TMPDIR, and what Input.Tmp names) is bound only under
// /tmp, /var/tmp, the home folder (not the home itself) or a
// sandbox.allow_write folder, and never inside a hidden folder or one of
// Jhon's own: the variable is the caller's, and any other value (/proc/sys)
// names no scratch (F7).
func layout(in Input, withPath bool) (mounts []mount, tmpdir bool) {
	real := func(p string) string {
		if r, _, ok := in.Stat(p); ok {
			return r
		}
		return p
	}
	realHome := real(in.Home)
	add := func(path string, m mount) {
		if path == "" || !filepath.IsAbs(path) {
			return
		}
		r, dir, ok := in.Stat(path)
		if !ok {
			return
		}
		// A writable area is never /, the home folder, or a folder above it.
		if m.kind == write && (r == "/" || project.Within(realHome, r)) {
			return
		}
		m.path, m.dir = r, dir
		for i, old := range mounts {
			if old.path == r {
				m.kind = max(old.kind, m.kind)
				m.shown, m.sealed, m.guarded = old.shown || m.shown, old.sealed || m.sealed, old.guarded || m.guarded
				mounts[i] = m
				return
			}
		}
		mounts = append(mounts, m)
	}
	config := in.xdg("XDG_CONFIG_HOME", ".config")

	// Writable: the project, the caches, what the owner allows.
	ours := slices.Concat(in.Roots, []string{in.CommonDir}, in.Worktrees)
	for _, p := range slices.Concat(ours, []string{in.under(".cache"), in.xdg("XDG_CACHE_HOME", ".cache")}, in.AllowWrite) {
		add(p, mount{kind: write})
	}
	for _, c := range toolCaches {
		if v := in.Env[c.env]; c.env != "" && filepath.IsAbs(v) {
			add(filepath.Join(v, c.sub), mount{kind: write})
		} else if c.home != "" {
			add(in.under(c.home), mount{kind: write})
		}
	}

	// Read-only: Jhon's own files, and the runtimes'.
	credentials, framework := guard.ProtectedHome()
	for _, p := range slices.Concat(in.Protected, []string{filepath.Join(config, "jhon"), filepath.Join(in.xdg("XDG_DATA_HOME", ".local/share"), "jhon")}) {
		add(p, mount{kind: read, guarded: true})
	}
	for _, rel := range slices.Concat(framework, runtimeHomes) {
		add(in.under(rel), mount{kind: read, guarded: true})
	}
	add(in.Telemetry, mount{kind: write})

	// Read-only: PATH's folders outside the project, also inside a writable
	// folder, where the inner mount decides: what the owner or a runtime
	// runs later is not a sandboxed command's to write (F2).
	if withPath {
		var own []string
		for _, p := range ours {
			own = append(own, real(p))
		}
		for _, dir := range in.Path {
			if !filepath.IsAbs(dir) {
				continue
			}
			if r, isDir, ok := in.Stat(dir); ok && isDir && !within(r, own) {
				add(dir, mount{kind: read})
			}
		}
	}

	// Hidden: the credentials.
	for _, rel := range slices.Concat(credentials, moreCredentials) {
		add(in.under(rel), mount{kind: hide, sealed: true})
		if moved, ok := strings.CutPrefix(rel, ".config/"); ok {
			add(filepath.Join(config, moved), mount{kind: hide, sealed: true})
		}
	}
	// Hidden: the snapshots, a second copy of all of the above (F5).
	for _, p := range slices.Concat(snapshots, []string{in.under(".snapshots")}) {
		add(p, mount{kind: hide, sealed: true})
	}
	// The session's sockets (F6): a runtime dir is a private one, with the
	// container sockets shown in it: the one $XDG_RUNTIME_DIR names and the
	// user's own, which is there whether or not the variable names it. The
	// socket folders outside them and the X authority file are hidden.
	// Abstract sockets have no path to hide: closing them takes a network
	// namespace, a non-goal.
	for _, run := range []string{in.Env["XDG_RUNTIME_DIR"], in.RuntimeDir} {
		if !filepath.IsAbs(run) {
			continue
		}
		if _, dir, ok := in.Stat(run); ok && dir {
			add(run, mount{kind: private, sealed: true})
			for _, s := range containerSockets {
				add(filepath.Join(run, s), mount{kind: read, shown: true})
			}
		}
	}
	for _, p := range slices.Concat(in.SocketFolders, []string{in.under(".Xauthority")}) {
		add(p, mount{kind: hide, sealed: true})
	}
	if path, ok := strings.CutPrefix(in.Env["DBUS_SESSION_BUS_ADDRESS"], "unix:path="); ok {
		path, _, _ = strings.Cut(path, ",")
		add(path, mount{kind: hide, sealed: true})
	}
	add(in.Env["SSH_AUTH_SOCK"], mount{kind: hide, sealed: true})

	// Writable: the scratch roots, each only where one may be.
	var allowed []string
	for _, p := range in.AllowWrite {
		if filepath.IsAbs(p) {
			allowed = append(allowed, real(p))
		}
	}
	scratch := func(p string) bool {
		if !filepath.IsAbs(p) {
			return false
		}
		r, dir, ok := in.Stat(p)
		if !ok || !dir {
			return false
		}
		if !(within(r, []string{real("/tmp"), real("/var/tmp")}) || r != realHome && project.Within(r, realHome) || within(r, allowed)) {
			return false
		}
		return !slices.ContainsFunc(mounts, func(m mount) bool {
			return m.dir && (m.sealed || m.guarded) && project.Within(r, m.path)
		})
	}
	for _, p := range in.Tmp {
		if scratch(p) {
			add(p, mount{kind: write})
		}
	}
	if tmpdir = scratch(in.Env["TMPDIR"]); tmpdir {
		add(in.Env["TMPDIR"], mount{kind: write})
	}

	// Nothing but what the sandbox shows (the container sockets) is mounted
	// inside a sealed folder: no writable area in ~/.ssh.
	sealed := func(p string) bool {
		return slices.ContainsFunc(mounts, func(h mount) bool {
			return h.sealed && h.dir && h.path != p && project.Within(p, h.path)
		})
	}
	mounts = slices.DeleteFunc(mounts, func(m mount) bool { return !m.shown && sealed(m.path) })
	// Parents first: the inner area is mounted over the outer one.
	slices.SortStableFunc(mounts, func(a, b mount) int {
		if d := strings.Count(a.path, "/") - strings.Count(b.path, "/"); d != 0 {
			return d
		}
		return strings.Compare(a.path, b.path)
	})
	return mounts, tmpdir
}

// writable are the real paths of what a sandboxed command may write outside
// the sandbox's own folders, under any PATH: the writable areas of in, and
// /dev, which is the machine's (with /dev/shm).
func writable(in Input) []string {
	mounts, _ := layout(in, false)
	out := []string{"/dev"}
	for _, m := range mounts {
		if m.kind == write {
			out = append(out, m.path)
		}
	}
	return out
}

// Args is bubblewrap's arguments for in, up to and without the `--` that
// precedes the program.
//
// The whole machine is bound read-only, with /dev as it is (the devices, and
// /dev/shm writable); no PID or network namespace is made, so /proc and
// /sys are the machine's, read-only. Then every area (layout), each only
// when it exists and at its real path: bubblewrap fails on a missing source,
// and a symlinked path is bound where it leads. The areas are mounted
// parents first, so where they nest the inner one decides: the telemetry
// folder is writable inside the read-only share, Jhon's settings and a PATH
// folder stay read-only inside a writable folder, a credential path stays
// hidden inside one. Nothing is mounted inside a credential folder or the
// private runtime dir but the container sockets. For one path, hidden
// wins over read-only, and read-only over writable. An area that is what its
// surroundings already are (a read-only folder outside every writable one,
// the git folder inside the project) takes no mount.
//
// Then the environment: the credential variables that are set are unset
// (F4), a $TMPDIR that is not bound becomes /tmp (F7), and InsideVariable
// marks what runs inside (T217).
func Args(in Input) []string {
	mounts, tmpdir := layout(in, true)
	args := []string{"--ro-bind", "/", "/", "--dev-bind", "/dev", "/dev"}
	var folders []string
	var kept []mount
	for _, m := range mounts {
		// What m lies in: the machine, read-only, or the innermost area
		// mounted so far, the last one that holds it.
		outer := read
		for _, k := range kept {
			if k.dir && project.Within(m.path, k.path) {
				outer = k.kind
			}
		}
		if m.kind == outer {
			continue
		}
		kept = append(kept, m)
		switch {
		case m.kind == write:
			args = append(args, "--bind", m.path, m.path)
		case m.kind == read:
			args = append(args, "--ro-bind", m.path, m.path)
		case m.kind == private:
			args = append(args, "--perms", "0700", "--tmpfs", m.path)
		case m.dir:
			args = append(args, "--tmpfs", m.path)
			folders = append(folders, m.path)
		default:
			// An empty file: the null device, readable as one (a plain bind
			// would refuse the read).
			args = append(args, "--dev-bind", "/dev/null", m.path)
		}
	}
	// A hidden folder turns read-only once what it shows is mounted in it.
	for _, f := range folders {
		args = append(args, "--remount-ro", f)
	}
	for _, name := range CredentialVariables {
		if _, set := in.Env[name]; set {
			args = append(args, "--unsetenv", name)
		}
	}
	if in.Env["TMPDIR"] != "" && !tmpdir {
		args = append(args, "--setenv", "TMPDIR", "/tmp")
	}
	args = append(args, "--setenv", InsideVariable, "1", "--die-with-parent")
	if in.Cwd != "" {
		args = append(args, "--chdir", in.Cwd)
	}
	return args
}

// InsideVariable is the variable every sandbox sets to 1 for what runs in
// it (T217 S4): `jhon sandbox status` run inside, where no other sandbox can
// start, reads it to say so instead of probing. It changes only what status
// reports: `jhon sandbox run` still checks and starts bubblewrap, and fails
// closed where it cannot.
const InsideVariable = "JHON_IN_SANDBOX"

// Inside is whether this process runs inside Jhon's sandbox, as
// InsideVariable says.
func Inside() bool { return os.Getenv(InsideVariable) == "1" }

// probeArgs are the arguments of the empty sandbox Probe starts: one of
// every option Args may give bubblewrap, on paths every Linux has, so a
// bubblewrap that cannot take one of them fails the probe as it would fail
// a run.
var probeArgs = []string{
	"--ro-bind", "/", "/", "--dev-bind", "/dev", "/dev",
	"--bind", "/tmp", "/tmp", "--perms", "0700", "--tmpfs", "/tmp", "--remount-ro", "/tmp",
	"--unsetenv", "SSH_AUTH_SOCK", "--setenv", "TMPDIR", "/tmp",
	"--die-with-parent", "--chdir", "/",
}

// ProjectFolder is the real path of a folder the caller pins as the project
// (`jhon sandbox run --project`): absolute, a folder that exists, and not /,
// the home folder or one above it, as for any writable area.
func ProjectFolder(folder string) (real string, ok bool) {
	if !filepath.IsAbs(folder) {
		return "", false
	}
	real, dir, ok := Disk(folder)
	if !ok || !dir || real == "/" || project.Within(project.Realish(project.Home()), real) {
		return "", false
	}
	return real, true
}

// Disk is Input.Stat on this machine's disk.
func Disk(path string) (real string, dir, ok bool) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false, false
	}
	fi, err := os.Stat(real)
	if err != nil {
		return "", false, false
	}
	return real, fi.IsDir(), true
}

// remembered is stat with each path's answer kept: one start asks for the
// same paths more than once (Check, then Args).
func remembered(stat func(string) (string, bool, bool)) func(string) (string, bool, bool) {
	type answer struct {
		real    string
		dir, ok bool
	}
	seen := map[string]answer{}
	return func(path string) (string, bool, bool) {
		a, ok := seen[path]
		if !ok {
			a.real, a.dir, a.ok = stat(path)
			seen[path] = a
		}
		return a.real, a.dir, a.ok
	}
}

// Current is the Input of a program started in cwd: the
// project's roots, its common dir and its listed worktrees, all read from
// the disk with no git process and no trust in what the project's own files
// name; the scratch roots; and this process's environment, PATH, runtime dir
// and socket folders. The project is the folder the caller pinned
// (project.RepoAt), or with pinned "" the one found by walking up from cwd
// (project.ReadRepo): a command may remove its project's `.git` entry, and
// the walk would then take an enclosing repository. In a workspace the
// primary and the joined members are roots too (workspace.DiskRoots). The caller adds what the
// settings and the integrations know: AllowWrite, DeployTools, Protected,
// Telemetry.
func Current(cwd, pinned string) Input {
	repo := project.ReadRepo(cwd)
	if pinned != "" {
		repo = project.RepoAt(pinned)
	}
	uid := strconv.Itoa(os.Getuid())
	in := Input{
		Cwd: cwd, Roots: append(repo.Roots(), workspace.DiskRoots(repo)...), CommonDir: repo.Common, Worktrees: repo.Worktrees(),
		Tmp: project.TmpRoots(), Home: project.Realish(project.Home()), Env: map[string]string{},
		Path:          filepath.SplitList(os.Getenv("PATH")),
		RuntimeDir:    "/run/user/" + uid,
		SocketFolders: []string{"/tmp/tmux-" + uid, "/tmp/.X11-unix", "/tmp/.ICE-unix"},
		Stat:          remembered(Disk),
	}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		in.Env[k] = v
	}
	return in
}
