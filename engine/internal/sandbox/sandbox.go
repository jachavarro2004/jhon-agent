// Package sandbox builds the bubblewrap arguments a model-chosen command
// runs under (T128a, odd/proposals/t128-sandbox.md section 4): the kernel,
// not the command's text, keeps its writes inside the project, the scratch
// roots and the caches, hides the credentials, and leaves Jhon's own files
// read-only.
//
// Args is pure: what it is given decides the arguments, and the disk is read
// only through Input.Stat. A run starts bubblewrap with Open's arguments,
// Args's with each bind opened with no link followed and passed as a
// descriptor (T248). Current gathers an Input for a working directory
// from the project code that knows the roots, the worktrees and the scratch
// roots, reading the repository from the disk. The start runs no program but
// bubblewrap itself, and trusts nothing a sandboxed command may have written
// (L888, odd/proposals/t128a-verify-1.md): bubblewrap comes from a PATH folder
// outside every writable area (Check), and the project is where `.git` is,
// whatever its files say (project.ReadRepo). The protected home paths are the
// guard's (guard.ProtectedHome), the one thing this package takes from it.
package sandbox

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

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
	// Main is the main worktree among Roots when it is not Roots[0] (a
	// linked worktree's), as the project's code reads it: the folder that
	// holds CommonDir, read through every link (L4). Open binds it only
	// when it is that folder on the disk.
	Main string
	// Worktrees are the repository's other listed worktrees.
	Worktrees []string
	// Tmp are the scratch roots: /tmp and $TMPDIR, as written, not where
	// their links lead (L26). Each is bound only where a scratch root may be
	// (layout).
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
	// they exist: Current gives tmux's, X11's, ICE's and the pi harness's
	// (gentle-pi) under /tmp.
	SocketFolders []string
	// AllowWrite is sandbox.allow_write, each folder absolute.
	AllowWrite []string
	// SettingsProblem is why the settings file cannot be read, so
	// AllowWrite holds nothing the owner wrote ("" when it can): a kept
	// line's note names it (T251, L1731).
	SettingsProblem string
	// DeployTools is sandbox.deploy_tools: the programs, by bare name, a
	// line runs outside the sandbox as it runs docker (ProfileAt).
	DeployTools []string
	// Protected are Jhon's own files besides the framework paths under the
	// home: the settings, and the state folder with the grants and modes.
	Protected []string
	// Kept are Jhon's own paths no writable area may let a sandboxed command
	// replace (KeptProblem's, the caller's keptOwn): a cache or store that
	// holds one where a command could change it is not bound writable
	// (holdsKept, T250 A6).
	Kept []Kept
	// Telemetry is the telemetry folder, writable inside the read-only share.
	Telemetry string
	// Registry is the folder of the registry of running runs (Registry):
	// hidden inside the sandbox, where no command has a reason to see it or
	// to hold its lock (L36 A).
	Registry string
	// Docker is sandbox.docker: the container sockets (containerSockets)
	// are shown inside the sandbox, an escape the owner accepted (L37).
	Docker bool
	// ContainerHosts are the endpoints the docker CLI's contexts name
	// (~/.docker/contexts): with Docker off, each unix one is hidden, as
	// DOCKER_HOST's and CONTAINER_HOST's are (L39).
	ContainerHosts []string
	// Stat is what path is on disk, through symlinks: its real path and
	// whether it is a directory; ok is false when it does not exist.
	Stat func(path string) (real string, dir, ok bool)
	// ProjectFrom is where the project was taken from, for Open (T248, L4):
	// "." for the folder this process works in, which Roots[0] is or holds,
	// or the --project value as written, which Roots[0] is. The project's
	// own code reads Roots[0] through every link (project.Realish), at a
	// moment a concurrent run may have swapped a folder on the way, so Open
	// binds no project folder until Roots[0] is the same folder as this. ""
	// with Roots refuses a run.
	ProjectFrom string
	// Lstat is what path itself is, a link not followed: whether it exists,
	// and for a link its text. nil takes Stat's answer: a path whose real
	// path is another is a link to that one (replaceable).
	Lstat func(path string) (link string, isLink, ok bool)
	// Empty is whether path is a folder with nothing in it; nil answers no.
	// A runtime's empty folder in another worktree is not mounted (T250 B3).
	Empty func(path string) bool
}

// toolCaches are the caches and stores the common toolchains write outside
// a project (go, cargo, npm, gradle, maven, nuget, pip, mise): the variable
// that moves each and the folder under it, and its place under the home;
// either may be "". None holds the programs the owner or a runtime runs
// later (go's and cargo's bin, rustup, mise's installs and shims, pnpm's and
// bun's own folders), nor what decides which ones run (mise's state, with
// the configs it trusts): what a sandboxed command wrote there would run
// outside the sandbox (L888, F2). Those are the owner's sandbox.allow_write.
// uv's, pnpm's and bun's stores are not among them: they are read-only
// (linkedStores, T250 A5).
var toolCaches = []struct{ env, sub, home string }{
	{"GOPATH", "pkg", "go/pkg"}, {"GOMODCACHE", "", ""}, {"GOCACHE", "", ""},
	{"CARGO_HOME", "registry", ".cargo/registry"}, {"CARGO_HOME", "git", ".cargo/git"},
	{"npm_config_cache", "", ".npm"},
	{"GRADLE_USER_HOME", "", ".gradle"},
	{"", "", ".m2/repository"},
	{"NUGET_PACKAGES", "", ".nuget/packages"},
	{"PIP_CACHE_DIR", "", ""},
	{"MISE_CACHE_DIR", "", ""},
}

// linkedStores are the package stores whose files uv, pnpm and bun link
// into a project by default (a hard link: one file, two names), where they
// are, as written: with one writable, a sandboxed command would change in
// place a file the owner's own installs outside the sandbox share (T250
// A5). They are read-only inside; the sandbox installs from stores of its
// own (StoreEnv). Each is where its tool finds it: uv's under the cache or
// $UV_CACHE_DIR, pnpm's under the data folder, ~/.pnpm-store, $PNPM_HOME or
// the store_dir setting's variables, bun's under ~/.bun, $BUN_INSTALL or
// $BUN_INSTALL_CACHE_DIR.
func (in Input) linkedStores() []string {
	out := []string{
		filepath.Join(in.xdg("XDG_CACHE_HOME", ".cache"), "uv"), in.under(".cache/uv"),
		filepath.Join(in.xdg("XDG_DATA_HOME", ".local/share"), "pnpm", "store"), in.under(".local/share/pnpm/store"), in.under(".pnpm-store"),
		in.under(".bun/install/cache"),
	}
	for _, v := range [][2]string{{"UV_CACHE_DIR", ""}, {"PNPM_HOME", "store"}, {"pnpm_config_store_dir", ""}, {"npm_config_store_dir", ""}, {"BUN_INSTALL", "install/cache"}, {"BUN_INSTALL_CACHE_DIR", ""}} {
		if p := in.Env[v[0]]; filepath.IsAbs(p) {
			out = append(out, filepath.Join(p, v[1]))
		}
	}
	return out
}

// StoreEnv are the variables a work command gets for its installs (T250
// A5): uv's cache, pnpm's store and bun's cache are folders of the
// sandbox's own under the cache, $XDG_CACHE_HOME/jhon/sandbox, and uv and
// pnpm copy each file into a project instead of linking it, so no file of
// the sandbox's stores is another project's too. They replace the owner's
// own values (WithEnv), which name the stores that are read-only inside.
// pnpm reads its settings from pnpm_config_* since version 11, and from
// npm_config_* before.
func StoreEnv(in Input) []string {
	stores := in.ownStores()
	uv, pnpm, bun := stores[0], stores[1], stores[2]
	return []string{
		"UV_CACHE_DIR=" + uv, "UV_LINK_MODE=copy",
		"pnpm_config_store_dir=" + pnpm, "npm_config_store_dir=" + pnpm,
		"pnpm_config_package_import_method=copy", "npm_config_package_import_method=copy",
		"BUN_INSTALL_CACHE_DIR=" + bun,
	}
}

// ownStores are the sandbox's own stores StoreEnv names, uv's, pnpm's and
// bun's, in that order, under $XDG_CACHE_HOME/jhon/sandbox. Each is a folder
// the sandbox binds onto itself where it exists (pinWay), so no command can
// put a link to a store the sandbox keeps read-only in its place, which
// would make every later install fail (T250 A5); EnsureOwnStores makes
// them.
func (in Input) ownStores() []string {
	dir := filepath.Join(in.xdg("XDG_CACHE_HOME", ".cache"), "jhon", "sandbox")
	return []string{filepath.Join(dir, "uv"), filepath.Join(dir, "pnpm-store"), filepath.Join(dir, "bun")}
}

// EnsureOwnStores makes each of the sandbox's own stores (ownStores) that
// is missing, before a run and outside the sandbox, as a folder made with
// no link followed (mkdirUnder), so the run pins it (T250 A5). One that is
// a link, which only a command could have put there in an older jhon, is
// removed first (the link alone, never where it leads): fixed gives each
// such path with where it led, made each folder made. A store outside the
// home is left as it is.
func EnsureOwnStores(in Input) (made []string, fixed [][2]string) {
	home := filepath.Clean(in.Home)
	for _, d := range in.ownStores() {
		p := in.homed(d)
		if !project.Within(p, home) || p == home {
			continue
		}
		fi, err := os.Lstat(p)
		if err == nil && fi.Mode()&os.ModeSymlink != 0 {
			to, _ := os.Readlink(p)
			if os.Remove(p) != nil {
				continue
			}
			fixed = append(fixed, [2]string{d, to})
		} else if err == nil {
			continue
		}
		if mkdirUnder(home, p) == nil {
			made = append(made, d)
		}
	}
	return made, fixed
}

// WithEnv is env with set's KEY=VALUE pairs in place of any of env's for
// the same keys, set's last: a program reads the first of two, so none is
// left beside.
func WithEnv(env, set []string) []string {
	keys := map[string]bool{}
	for _, kv := range set {
		k, _, _ := strings.Cut(kv, "=")
		keys[k] = true
	}
	var out []string
	for _, kv := range env {
		if k, _, _ := strings.Cut(kv, "="); !keys[k] {
			out = append(out, kv)
		}
	}
	return append(out, set...)
}

// projectHooks are the paths, in each of the project's tops, of the files a
// runtime runs outside the sandbox (T250 A3): Claude Code's settings with
// their hooks and its MCP servers, pi's settings and extensions; with the
// runtime each belongs to, for the note on a link. git's hooks folder is
// not among them: git takes its hooks from core.hooksPath, which a
// sandboxed command may set in .git/config, writable inside (251 of 21,212
// bash calls of the owner's 30 days write it), so a read-only .git/hooks
// contained nothing and cost `rm -rf .git` (T250 round 3).
var projectHooks = [][2]string{{".claude", "Claude Code"}, {".mcp.json", "Claude Code"}, {".pi", "pi"}}

// hookPath is one of the project's paths a runtime runs outside the
// sandbox: where it is, as written, and whose it is; root is whether its
// top is a root of the project that git works in, not another worktree.
type hookPath struct {
	path, runtime string
	root          bool
}

// hookPaths are projectHooks in each top of the project: its roots that
// are no link (ownRoots: the folder the session works in, a linked
// worktree's main checkout, the workspace's members) and the repository's
// other worktrees. Where one exists in another worktree, that worktree
// cannot be removed from inside (its folder is pinned, pinWay): 3 of the
// 163 `git worktree remove` of the owner's 30 days were in a repository
// that tracks one, and the note says to remove it outside.
func (in Input) hookPaths() []hookPath {
	var roots []string
	for _, r := range in.ownRoots() {
		if _, _, ok := in.Stat(filepath.Join(r, ".git")); ok {
			roots = append(roots, filepath.Clean(r))
		}
	}
	var out []hookPath
	for _, top := range in.hookTops() {
		for _, h := range projectHooks {
			out = append(out, hookPath{filepath.Join(top, h[0]), h[1], slices.Contains(roots, top)})
		}
	}
	return out
}

// hookTops are the project's roots and listed worktrees that are no link
// and pass through none, each once.
func (in Input) hookTops() []string {
	var out []string
	for _, top := range slices.Concat(in.ownRoots(), in.Worktrees) {
		if filepath.IsAbs(top) && in.linkOn(top) == "" && !slices.Contains(out, filepath.Clean(top)) {
			out = append(out, filepath.Clean(top))
		}
	}
	return out
}

// HookProblem is why the sandbox of in runs nothing while one of the
// project's paths a runtime runs outside the sandbox is a link (T250 A3):
// a mount follows it, so the folder it leads to would be read-only and the
// link itself, in the writable project, one a command could point at files
// it wrote. nil when none is.
func HookProblem(in Input) *Problem {
	for _, h := range in.hookPaths() {
		if _, isLink, ok := in.lstat(h.path); ok && isLink {
			what := "what that folder holds"
			if strings.HasSuffix(h.path, ".json") {
				what = "what that file names"
			}
			return &Problem{
				Reason: h.path + " is a link, which a sandboxed command could point at files it wrote, and " + h.runtime + " runs " + what + " outside the sandbox",
				Fix:    "replace the link " + h.path + " with the folder or file it leads to",
			}
		}
	}
	return nil
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
// private one with sandbox.docker on (Input.Docker, L37): docker's and
// podman's. Through one a container can write anywhere the owner can, and
// it outlives the run, so by default they are hidden with the rest.
var containerSockets = []string{"docker.sock", "podman/podman.sock"}

// DockerEscape is what status and doctor say of the container sockets shown
// inside the sandbox (L37).
const DockerEscape = "docker socket inside the sandbox: a container can write anywhere the user can, and outlive the command (sandbox.docker = true; jhon settings set sandbox.docker false hides it)"

// systemContainerSockets are where a docker or podman engine run by the
// system keeps its socket (/var/run is a link to /run on most systems, kept
// for the ones where it is not), and homeContainerSockets the folders under
// the home where Docker Desktop keeps its own: hidden with sandbox.docker
// off (L39), as is any unix socket DOCKER_HOST, CONTAINER_HOST or a docker
// context names (containerHosts).
var (
	systemContainerSockets = []string{"/run/docker.sock", "/var/run/docker.sock", "/run/podman", "/var/run/podman", "/run/containerd"}
	homeContainerSockets   = []string{".docker/desktop", ".docker/run"}
)

// containerHosts are the unix socket paths of the container engines in: the
// ones DOCKER_HOST and CONTAINER_HOST name, and the docker contexts'
// (Input.ContainerHosts); tcp:// and ssh:// endpoints name no path.
func (in Input) containerHosts() []string {
	var out []string
	for _, h := range slices.Concat([]string{in.Env["DOCKER_HOST"], in.Env["CONTAINER_HOST"]}, in.ContainerHosts) {
		// The scheme in any case and the path without a query or a
		// fragment, as podman reads CONTAINER_HOST (url.Parse, L40 A1).
		if len(h) < len("unix:") || !strings.EqualFold(h[:len("unix:")], "unix:") {
			continue
		}
		p := h[len("unix:"):]
		if i := strings.IndexAny(p, "?#"); i >= 0 {
			p = p[:i]
		}
		if strings.HasPrefix(p, "//") && !strings.HasPrefix(p, "///") {
			// unix://host/path names no host for a socket; the path is
			// what follows the host.
			if i := strings.Index(p[2:], "/"); i >= 0 {
				p = p[2+i:]
			}
		}
		if filepath.IsAbs(p) {
			out = append(out, filepath.Clean(p))
		}
	}
	return out
}

// systemSockets are the machine's sockets outside the runtime dir that run
// a command or act for the owner, hidden when they exist (L36): the system
// bus (its services act for the active session) and sshd's local socket
// (a login). The rest under /run (systemd's varlink sockets, the journal,
// polkit's agent helper, root-only ones) stay: none runs a command or
// writes outside the work areas for the owner.
var systemSockets = []string{"/run/dbus", "/run/ssh-unix-local"}

// homeSockets are the folders under the home ($XDG_CONFIG_HOME's for those
// under .config) that hold the sockets of a program that runs commands for
// whoever connects, hidden when they exist (L36): herdr's, whose API runs a
// command in a pane or types into one.
var homeSockets = []string{".config/herdr"}

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
	// written are the paths, as written (the home's own way taken, homed),
	// whose checks gave a writable area this real path: no link on their
	// way (linkOn), so each is the real path itself unless a folder on its
	// way changed between the check and the read of the real path (T248,
	// L4). A run binds the area only through them (openBind).
	written []string
	// note is what a refused write there says after where it is, when the
	// mount has its own reason (storeNote, hookNote); "" for the kind's.
	note string
	// pin is a writable folder bound onto itself only so that it is a mount
	// point no command can rename (pinWay): it is mounted though it lies in
	// a writable area.
	pin bool
}

// worktreeFix is how to move or remove folder, which the sandbox keeps in
// place, outside it: for one of the repository's other worktrees,
// `git worktree remove <w>` while git still knows it (its .git file is
// there), and only `rm -r <w>` once git dropped its record (git worktree
// remove drops the record, then stops on the folder the sandbox keeps, and
// a second one says "is not a working tree").
func (in Input) worktreeFix(folder string) string {
	if !slices.Contains(in.Worktrees, folder) {
		return "move or remove it outside the sandbox, in a terminal"
	}
	if _, err := os.Lstat(filepath.Join(folder, ".git")); err == nil {
		return "remove it outside the sandbox, in a terminal: git worktree remove " + folder
	}
	return "git has already removed the rest of the worktree and its record, so git worktree remove no longer knows it: remove what is left outside the sandbox, in a terminal: rm -r " + folder
}

// storeNote and hookNote are what a write refused in a linked store or in a
// file a runtime runs outside the sandbox says (T250 A5, A3): the store's
// with the stop, a hook file's with how to change it, as the agent's own
// file tools run outside the sandbox.
const (
	storeNote = "a package store whose files your installs outside the sandbox share, read-only by design: installs inside use the sandbox's own ($UV_CACHE_DIR, pnpm's store-dir, $BUN_INSTALL_CACHE_DIR), and no setting opens it; that is a stop: tell the owner the path and why, never write it elsewhere"
	hookNote  = "a file a runtime runs outside the sandbox (Claude Code's settings, hooks and MCP servers, pi's settings and extensions), read-only by design: change it with the agent's own file tools (write, edit), which run outside the sandbox, never with a shell command; no setting opens it"
	pinNote   = "a folder the sandbox keeps where it is, as it holds, or lies on the way to, a file a runtime runs outside the sandbox (.claude, .pi, .mcp.json), a socket the sandbox hides or the sandbox's own store, so no command moves or removes it from inside"
)

// under is rel under the home.
func (in Input) under(rel string) string { return filepath.Join(in.Home, rel) }

// xdg is the XDG folder a variable names, or its default under the home.
func (in Input) xdg(name, rel string) string {
	if v := in.Env[name]; filepath.IsAbs(v) {
		return v
	}
	return in.under(rel)
}

// socketPaths are the socket paths and folders outside the runtime dirs the
// sandbox hides, whether or not they exist (layout hides the ones that
// do): in.SocketFolders, the X authority file, systemSockets, homeSockets
// (and $XDG_CONFIG_HOME's), the session and system buses an address names,
// and the SSH agent's socket.
func (in Input) socketPaths() []string {
	out := slices.Concat(in.SocketFolders, []string{in.under(".Xauthority")}, systemSockets)
	config := in.xdg("XDG_CONFIG_HOME", ".config")
	for _, rel := range homeSockets {
		out = append(out, in.under(rel))
		if moved, ok := strings.CutPrefix(rel, ".config/"); ok {
			out = append(out, filepath.Join(config, moved))
		}
	}
	for _, bus := range []string{"DBUS_SESSION_BUS_ADDRESS", "DBUS_SYSTEM_BUS_ADDRESS"} {
		if path, ok := strings.CutPrefix(in.Env[bus], "unix:path="); ok {
			path, _, _ = strings.Cut(path, ",")
			out = append(out, path)
		}
	}
	// The container engines' sockets, unless the owner shows them (L37,
	// L39): through one a container can write anywhere the owner can.
	if !in.Docker {
		out = append(out, systemContainerSockets...)
		docker := in.under(".docker")
		if d := in.Env["DOCKER_CONFIG"]; filepath.IsAbs(d) {
			docker = d
		}
		for _, rel := range homeContainerSockets {
			out = append(out, in.under(rel), filepath.Join(docker, strings.TrimPrefix(rel, ".docker/")))
		}
		out = append(out, in.containerHosts()...)
	}
	return append(out, in.Env["SSH_AUTH_SOCK"])
}

// SocketLinkProblem is why the sandbox of in runs nothing while a variable
// names a socket the sandbox hides through a link in a folder a sandboxed
// command may write (T250, the re-verify of 5e9b12e): the sandbox hides the
// path the link leads to when a run starts, and a command could point the
// link elsewhere, so the next run would hide that and leave the socket
// shown. A bind cannot be put on a link, so the run is refused, with the
// real path to set. A link no command may change (the home's own) passes.
// nil when no variable does so.
func SocketLinkProblem(in Input) *Problem {
	named := [][2]string{{"SSH_AUTH_SOCK", in.Env["SSH_AUTH_SOCK"]}}
	for _, bus := range []string{"DBUS_SESSION_BUS_ADDRESS", "DBUS_SYSTEM_BUS_ADDRESS"} {
		if path, ok := strings.CutPrefix(in.Env[bus], "unix:path="); ok {
			path, _, _ = strings.Cut(path, ",")
			named = append(named, [2]string{bus, path})
		}
	}
	if !in.Docker {
		for _, v := range []string{"DOCKER_HOST", "CONTAINER_HOST"} {
			for _, h := range (Input{Env: map[string]string{v: in.Env[v]}}).containerHosts() {
				named = append(named, [2]string{v, h})
			}
		}
	}
	var mounts []mount
	for _, n := range named {
		if !filepath.IsAbs(n[1]) {
			continue
		}
		link := in.linkOn(n[1])
		if link == "" {
			continue
		}
		if mounts == nil {
			mounts, _ = layout(in, false)
		}
		// A command replaces the link where its folder is writable, or
		// moves that folder.
		if !writableTop(filepath.Dir(link), mounts) && !changeable(mounts, filepath.Dir(link)) {
			continue
		}
		return &Problem{
			Reason: "$" + n[0] + " names " + n[1] + ", which passes through the link " + link + " in a folder sandboxed commands may write, so a command could point it elsewhere and the sandbox would hide the wrong path",
			Fix:    "set " + n[0] + " to the socket's real path (" + realPath(n[1], in.Stat) + ")",
		}
	}
	return nil
}

// DockerContexts are the endpoints the docker CLI's contexts name: the
// docker endpoint's Host in each <config>/contexts/meta/*/meta.json, where
// <config> is $DOCKER_CONFIG or ~/.docker, at most 64 contexts and 64 KiB
// each (Input.ContainerHosts).
func DockerContexts(home string, env map[string]string) []string {
	config := filepath.Join(home, ".docker")
	if d := env["DOCKER_CONFIG"]; filepath.IsAbs(d) {
		config = d
	}
	metas, _ := filepath.Glob(filepath.Join(config, "contexts", "meta", "*", "meta.json"))
	var out []string
	for i, m := range metas {
		if i >= 64 {
			break
		}
		// Only a regular file, opened without blocking: a FIFO or a device
		// in its place would hang every next run (L40 A2).
		if fi, err := os.Lstat(m); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		f, err := os.OpenFile(m, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			continue
		}
		if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
			f.Close()
			continue
		}
		var meta struct {
			Endpoints map[string]struct{ Host string }
		}
		err = json.NewDecoder(io.LimitReader(f, 64<<10)).Decode(&meta)
		f.Close()
		if err != nil {
			continue
		}
		for _, e := range meta.Endpoints {
			if e.Host != "" {
				out = append(out, e.Host)
			}
		}
	}
	return out
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
				m.written = append(slices.Clone(old.written), m.written...)
				if m.note == "" {
					m.note = old.note
				}
				m.pin = old.pin || m.pin
				mounts[i] = m
				return
			}
		}
		mounts = append(mounts, m)
	}
	// area adds a writable area: never one that is or passes through a link
	// (L28, linkOn), which a command may have put there. The project's own
	// folders are no exception (T249): <root>-worktrees is only a name next
	// to the root, which a run in an enclosing project or in $TMPDIR may
	// have made a link.
	area := func(path string) {
		if in.linkOn(path) == "" {
			add(path, mount{kind: write, written: []string{in.homed(filepath.Clean(path))}})
		}
	}
	config := in.xdg("XDG_CONFIG_HOME", ".config")

	// Writable: the project, the caches, what the owner allows.
	ours := in.ownFolders()
	for _, p := range ours {
		area(p)
	}
	var anyPath []mount
	for _, a := range in.areas() {
		// A cache or store under the home that holds a PATH folder is not
		// bound with PATH known (L29, holdsPATH), nor one that holds one of
		// Jhon's own paths where a command could change it (T250 A6,
		// holdsKept); under any PATH (writable, mayChange) it still counts as
		// writable.
		if withPath && a.cache && (in.holdsPATH(a.path) != "" || in.holdsKept(a.path, &anyPath) != nil) {
			continue
		}
		area(a.path)
	}

	// Read-only: Jhon's own files, and the runtimes'.
	credentials, framework := guard.ProtectedHome()
	for _, p := range slices.Concat(in.Protected, []string{filepath.Join(config, "jhon"), filepath.Join(in.xdg("XDG_DATA_HOME", ".local/share"), "jhon")}) {
		add(p, mount{kind: read, guarded: true})
	}
	for _, rel := range slices.Concat(framework, runtimeHomes) {
		add(in.under(rel), mount{kind: read, guarded: true})
	}
	area(in.Telemetry)

	// Read-only: the stores the owner's installs share files with by inode
	// (T250 A5), where their links lead: what matters is their files, which
	// no link to them makes writable. And the project's files a runtime runs
	// outside the sandbox (T250 A3), each where it is as written: one that is
	// or passes through a link is mounted nowhere, as the link itself would
	// stay for a command to point elsewhere (HookProblem refuses the run).
	for _, p := range in.linkedStores() {
		add(p, mount{kind: read, guarded: true, note: storeNote})
	}
	for _, h := range in.hookPaths() {
		if in.linkOn(h.path) != "" {
			continue
		}
		_, _, there := in.Stat(h.path)
		_, isLink, _ := in.lstat(h.path)
		switch {
		case there && !h.root && in.Empty != nil && in.Empty(h.path):
			// An empty .claude or .pi in another worktree, which a
			// session there left: no mount, so `git worktree remove`
			// of it still works (T250 B3).
		case there:
			add(h.path, mount{kind: read, guarded: true, note: hookNote})
		case !isLink && h.root && !strings.HasSuffix(h.path, ".json") && writableTop(filepath.Dir(h.path), mounts):
			// A root's missing .claude or .pi (T250 B3, the owner's
			// decision): an empty read-only folder, which bubblewrap
			// makes as its mount point, so an empty folder is left on
			// the disk; no command can create one with settings, hooks
			// or extensions. Not .mcp.json, a file: an empty one would
			// show in every project's git status.
			mounts = append(mounts, mount{path: filepath.Clean(h.path), kind: hide, dir: true, guarded: true, note: hookNote})
		}
	}

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
			if in.Docker {
				for _, s := range containerSockets {
					add(filepath.Join(run, s), mount{kind: read, shown: true})
				}
			}
		}
	}
	for _, p := range in.socketPaths() {
		add(p, mount{kind: hide, sealed: true})
	}
	add(in.Registry, mount{kind: hide, sealed: true})

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
			area(p)
		}
	}
	scratchTMPDIR := scratch(in.Env["TMPDIR"]) && in.linkOn(in.Env["TMPDIR"]) == ""
	if scratchTMPDIR {
		area(in.Env["TMPDIR"])
	}

	// Pinned: every folder on the way to what the sandbox hides or keeps
	// read-only inside a writable area, and to the sandbox's own stores,
	// is a mount point no command can rename (pinWay, T250 round 3).
	var targets [][2]string
	for _, m := range mounts {
		if (m.sealed || m.note != "") && m.kind != write {
			targets = append(targets, [2]string{m.path, ""})
		}
	}
	for _, d := range in.ownStores() {
		if r, dir, ok := in.Stat(d); ok && dir && in.linkOn(d) == "" {
			targets = append(targets, [2]string{r, "self"})
		}
	}
	for _, t := range targets {
		for _, f := range pinWay(t[0], mounts, t[1] == "self") {
			add(f, mount{kind: write, pin: true, written: []string{in.homed(f)}})
		}
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
	// A writable area inside another one takes no mount of its own (L26): it
	// is writable there already, and a command there could replace a folder
	// on the way to it before the next run binds it. One inside a folder the
	// sandbox mounts read-only there (a PATH folder in a cache) keeps its
	// mount. With no link on any area's way (area), every area bound
	// writable is reached only through folders no command can change.
	all := slices.Clone(mounts)
	mounts = slices.DeleteFunc(mounts, func(m mount) bool { return m.kind == write && !m.pin && inWritable(m.path, all) })
	// $TMPDIR stays for the program while it is writable.
	if scratchTMPDIR {
		r, _, _ := in.Stat(in.Env["TMPDIR"])
		m := innermost(mounts, r)
		tmpdir = m != nil && m.kind == write
	}
	return mounts, tmpdir
}

// pinWay is the one rule for a path the sandbox must keep where it is
// (T250 round 3): every folder on the way to target, a real path, whose own
// folder is writable in mounts (the innermost mount that holds it is
// writable) and that is no mount point but a writable area's (a writable
// area inside another takes no mount of its own, layout), and with self
// target too. A mount point cannot be renamed from inside its namespace,
// but the folder that holds it can, and with it the path; so each such
// folder, up to the outermost writable area, is bound onto itself (a pin)
// and none can be moved: not the folder of a socket the sandbox hides, a
// worktree that holds a runtime's file, nor a writable area nested in
// another. A pin hides nothing, so it never stands in the way of the
// command. Under /dev nothing is pinned.
func pinWay(target string, mounts []mount, self bool) []string {
	if !filepath.IsAbs(target) || within(target, []string{"/dev"}) {
		return nil
	}
	writableAt := func(dir string) bool {
		var at *mount
		for i, m := range mounts {
			if (m.dir || m.path == dir) && project.Within(dir, m.path) && (at == nil || len(m.path) > len(at.path)) {
				at = &mounts[i]
			}
		}
		return at != nil && at.kind == write
	}
	point := func(p string) bool {
		return slices.ContainsFunc(mounts, func(m mount) bool { return m.path == p && (m.kind != write || m.pin) })
	}
	var out []string
	t := filepath.Clean(target)
	var way []string
	for e := t; filepath.Dir(e) != e; e = filepath.Dir(e) {
		if e != t || self {
			way = append(way, e)
		}
	}
	for i := len(way) - 1; i >= 0; i-- {
		f := way[i]
		if writableAt(filepath.Dir(f)) && !point(f) && !isOuterArea(f, mounts) {
			out = append(out, f)
		}
	}
	return out
}

// writableTop is whether dir, a real path, lies in a folder mounts (in any
// order) leave writable: the innermost one that is or holds it is. Where it
// does not (Jhon's source checkout, read-only from its linked worktree), no
// command can create anything there, and bubblewrap could not make a
// mount point there either (T250 B3).
func writableTop(dir string, mounts []mount) bool {
	var at *mount
	for i, m := range mounts {
		if m.dir && project.Within(dir, m.path) && (at == nil || len(m.path) > len(at.path)) {
			at = &mounts[i]
		}
	}
	return at != nil && at.kind == write
}

// isOuterArea is whether p is a writable area of mounts that no other
// writable area holds: bound on its own already, a mount point.
func isOuterArea(p string, mounts []mount) bool {
	if !slices.ContainsFunc(mounts, func(m mount) bool { return m.path == p && m.kind == write }) {
		return false
	}
	return !slices.ContainsFunc(mounts, func(m mount) bool {
		return m.kind == write && m.dir && m.path != p && project.Within(p, m.path)
	})
}

// inWritable is whether an entry on the way to path, a real path, sits in a
// folder the innermost of mounts (layout's, parents first) leaves writable,
// /dev included, unless the sandbox mounts a folder that is not writable
// there (a mount point, which the kernel keeps where it is).
func inWritable(path string, mounts []mount) bool {
	open := func(dir string) bool {
		if within(dir, []string{"/dev"}) {
			return true
		}
		m := innermost(mounts, dir)
		return m != nil && m.kind == write
	}
	pinned := func(p string) bool {
		return slices.ContainsFunc(mounts, func(m mount) bool { return m.dir && m.kind != write && m.path == p })
	}
	for e := filepath.Clean(path); filepath.Dir(e) != e; e = filepath.Dir(e) {
		if open(filepath.Dir(e)) && !pinned(e) {
			return true
		}
	}
	return false
}

// ownFolders are the project's folders a run may write, as layout binds
// them: the roots, the common dir and the listed worktrees that are no link
// and pass through none (linkOn, T249), as any writable area.
func (in Input) ownFolders() []string {
	var out []string
	for _, p := range slices.Concat(in.Roots, []string{in.CommonDir}, in.Worktrees) {
		if p != "" && in.linkOn(p) == "" {
			out = append(out, p)
		}
	}
	return out
}

// ownRoots are in.Roots that are no link and pass through none (ownFolders):
// a root a command made a link is not the project's.
func (in Input) ownRoots() []string {
	var out []string
	for _, r := range in.Roots {
		if r != "" && in.linkOn(r) == "" {
			out = append(out, r)
		}
	}
	return out
}

// linkOn is the first entry on path, from / down as written (in.lstat), that
// is a link, and "" when none is. Only the home's own way, the HOME
// variable as written (a home under /home -> /var/home), is the owner's: a
// path under it is read from the home's real path (homed). A link anywhere
// else that leads to the home is a link like any other (T249).
func (in Input) linkOn(path string) string {
	if !filepath.IsAbs(path) {
		return ""
	}
	p := in.homed(filepath.Clean(path))
	sep := string(filepath.Separator)
	cur := sep
	for _, part := range strings.Split(strings.TrimPrefix(p, sep), sep) {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		if _, isLink, ok := in.lstat(cur); !ok {
			return ""
		} else if isLink {
			return cur
		}
	}
	return ""
}

// areaSource is a writable area other than the project's, scratch and
// telemetry: what names it, its path as written, and whether it is a cache
// or store (not the owner's sandbox.allow_write).
type areaSource struct {
	name, path string
	cache      bool
}

// areas are the caches, the owner's sandbox.allow_write folders and the
// toolchains' caches and stores, as written.
func (in Input) areas() []areaSource {
	out := []areaSource{{"the cache", in.under(".cache"), true}, {"$XDG_CACHE_HOME", in.xdg("XDG_CACHE_HOME", ".cache"), true}}
	for _, p := range in.AllowWrite {
		out = append(out, areaSource{"sandbox.allow_write folder", p, false})
	}
	for _, c := range toolCaches {
		if v := in.Env[c.env]; c.env != "" && filepath.IsAbs(v) {
			out = append(out, areaSource{"$" + c.env, filepath.Join(v, c.sub), true})
		} else if c.home != "" {
			out = append(out, areaSource{"the tool folder", in.under(c.home), true})
		}
	}
	return out
}

// holdsPATH is the first PATH folder outside the project that cache, a
// cache or store whose real path lies under the home (not the home itself),
// holds at any depth, as written or where its existing part leads; "" when
// it holds none, or lies outside the home (/tmp, $TMPDIR and a cache a
// variable moves there are not judged, L29). A command could rename a
// folder around such a PATH folder and put its own program there.
func (in Input) holdsPATH(cache string) string {
	if !filepath.IsAbs(cache) {
		return ""
	}
	home := filepath.Clean(in.Home)
	real := realPath(cache, in.Stat)
	if real == home || !project.Within(real, home) {
		return ""
	}
	var roots []string
	for _, r := range in.ownRoots() {
		if filepath.IsAbs(r) {
			roots = append(roots, filepath.Clean(r), realPath(r, in.Stat))
		}
	}
	for _, dir := range in.Path {
		if !filepath.IsAbs(dir) {
			continue
		}
		d, r := filepath.Clean(dir), realPath(dir, in.Stat)
		if within(d, roots) || within(r, roots) {
			continue
		}
		if project.Within(d, filepath.Clean(cache)) || project.Within(r, real) {
			return dir
		}
	}
	return ""
}

// holdsKept is the first of in.Kept that cache, a cache or store, holds as
// written or where its existing part leads, where a sandboxed command may
// change it with the cache writable (changeable under any PATH: a folder
// around it renamed; one bound read-only directly inside the cache is a
// mount point no command moves, S10), so the cache is not bound writable
// (T250 A6): as L29 does for a cache that holds a PATH folder, but for any
// cache, in the home or not. nil when it holds none, or is /, the home or a
// folder above it, which is never bound writable. anyPath keeps the mounts
// under any PATH (layout without PATH's folders) once read.
func (in Input) holdsKept(cache string, anyPath *[]mount) *Kept {
	if !filepath.IsAbs(cache) {
		return nil
	}
	real := realPath(cache, in.Stat)
	if home := realPath(in.Home, in.Stat); project.Within(home, real) {
		return nil
	}
	for i, k := range in.Kept {
		if !filepath.IsAbs(k.Path) || !project.Within(filepath.Clean(k.Path), filepath.Clean(cache)) && !project.Within(realPath(k.Path, in.Stat), real) {
			continue
		}
		if *anyPath == nil {
			*anyPath, _ = layout(in, false)
		}
		if changeable(*anyPath, k.Path) {
			return &in.Kept[i]
		}
	}
	return nil
}

// lstat is in.Lstat, or with none what in.Stat says: a path whose real path
// is another is a link to it, and one Stat does not find a plain entry (a
// fake disk may name a folder without its parents; the walk ends on a
// missing area all the same, as layout binds none).
func (in Input) lstat(p string) (link string, isLink, ok bool) {
	if in.Lstat != nil {
		return in.Lstat(p)
	}
	real, _, ok := in.Stat(p)
	if !ok {
		return "", false, true
	}
	if real != p {
		return real, true, true
	}
	return "", false, true
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
// /dev/shm writable), /sys the machine's, read-only. The program runs in a
// PID namespace of its own, with its /proc (T249, L34 "Sin procesos vivos
// del sandbox"): when it ends, bubblewrap's init ends and the kernel ends
// every process left in the namespace, a setsid or double-forked one
// included, and --die-with-parent ends it all when jhon ends. No network
// namespace is made. Then every area (layout), each only
// when it exists and at its real path: bubblewrap fails on a missing source,
// and a symlinked path is bound where it leads, but a writable area reached
// through a folder a sandboxed command may write takes no mount of its own
// (layout, L26). The areas are mounted
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
//
// Args is the readable form, each bind by its path (--bind, --ro-bind), as
// status, the tests and a reader take it. A run starts bubblewrap with Open's
// form instead, each bind through a descriptor opened with no link followed
// (T248).
func Args(in Input) []string {
	args, _ := build(in, func(m mount) ([]string, *Problem) {
		if m.kind == write {
			return []string{"--bind", m.path, m.path}, nil
		}
		return []string{"--ro-bind", m.path, m.path}, nil
	}, nil)
	return args
}

// build is Args with each bind, writable or read-only, given by bind: the
// path itself (Args) or a descriptor (Open). It stops at the first problem
// bind gives. hide, when not nil, is told of each hidden path and private
// folder it mounts (Open's expectations for enter).
func build(in Input, bind func(m mount) ([]string, *Problem), hide func(m mount)) ([]string, *Problem) {
	mounts, tmpdir := layout(in, true)
	args := []string{"--ro-bind", "/", "/", "--dev-bind", "/dev", "/dev", "--proc", "/proc"}
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
		if m.kind == outer && !m.pin {
			continue
		}
		kept = append(kept, m)
		switch {
		case m.kind == write || m.kind == read:
			b, p := bind(m)
			if p != nil {
				return nil, p
			}
			args = append(args, b...)
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
		if hide != nil && m.kind != write && m.kind != read {
			hide(m)
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
	args = append(args, "--setenv", InsideVariable, "1", "--unshare-pid", "--die-with-parent")
	if in.Cwd != "" {
		args = append(args, "--chdir", in.Cwd)
	}
	return args, nil
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
// every option a run (Open) may give bubblewrap, on paths every Linux has,
// so a bubblewrap that cannot take one of them fails the probe as it would
// fail a run. bind and ro are descriptors of /tmp's real path, opened as a
// run opens its binds (T248).
func probeArgs(bind, ro int, tmp string) []string {
	return []string{
		"--ro-bind", "/", "/", "--dev-bind", "/dev", "/dev", "--proc", "/proc",
		"--bind-fd", strconv.Itoa(bind), tmp, "--ro-bind-fd", strconv.Itoa(ro), tmp,
		"--perms", "0700", "--tmpfs", tmp, "--remount-ro", tmp,
		"--unsetenv", "SSH_AUTH_SOCK", "--setenv", "TMPDIR", "/tmp",
		"--unshare-pid", "--die-with-parent", "--chdir", "/",
	}
}

// ProjectFolder is the real path of a folder the caller pins as the project
// (`jhon sandbox run --project`): absolute, a folder that exists, and not /,
// the home folder or one above it, as for any writable area.
//
// The real path is read through every link, at a moment a concurrent run
// may have swapped a folder on the way for a link to the home (T248 L4's
// second verify): it is taken only when it is, by device and inode, the
// folder the value names as written, opened with no link followed (the
// home's own way taken, as for any area): a value through a link, or one
// read during a swap, is refused (pinnedAs). Every caller (run, status,
// profile, Claude Code's hook) gets the same answer.
func ProjectFolder(folder string) (real string, ok bool) {
	if !filepath.IsAbs(folder) {
		return "", false
	}
	real, dir, ok := Disk(folder)
	home := project.Realish(project.Home())
	if !ok || !dir || real == "/" || project.Within(home, real) {
		return "", false
	}
	if !pinnedAs(Input{Home: home, Env: map[string]string{"HOME": os.Getenv("HOME")}, Stat: Disk}, real, folder) {
		return "", false
	}
	return real, true
}

// HereProject is whether top, the project the folder this process works in
// is in as the project's code read it (project.ReadRepo, through every
// link), is that folder or one above it on the disk, by device and inode
// (anchorProblem): a swap while it was read names another folder (T248 L4).
// Where openat2 is missing or refused it says yes: the sandbox cannot run
// there, and a run fails closed on its own.
func HereProject(top string) bool {
	if top == "" {
		return true
	}
	in := Input{Roots: []string{top}, Home: project.Realish(project.Home()), Env: map[string]string{"HOME": os.Getenv("HOME")}, Stat: Disk, ProjectFrom: "."}
	p := anchorProblem(in)
	return p == nil || p.Reason == errNoOpenat2.Error() || p.Reason == errOpenat2Refused.Error()
}

// ProjectProblem is why the project of in is not the one it was taken from
// (Input.ProjectFrom: the working folder or the --project value as
// written), nil when it is: what Open checks first, for a line that would
// run outside the sandbox (T248 L4).
func (in Input) ProjectProblem() *Problem { return anchorProblem(in) }

// DiskLstat is Input.Lstat on this machine's disk.
func DiskLstat(path string) (link string, isLink, ok bool) {
	fi, err := os.Lstat(path)
	if err != nil {
		return "", false, false
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		return "", false, true
	}
	link, err = os.Readlink(path)
	if err != nil {
		// A link that cannot be read is taken as one that leads anywhere.
		return "/", true, true
	}
	return link, true, true
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

// diskEmpty is Input.Empty on this machine's disk: a folder, not a link,
// with no entry.
func diskEmpty(path string) bool {
	if fi, err := os.Lstat(path); err != nil || !fi.IsDir() {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	names, err := f.Readdirnames(1)
	return len(names) == 0 && err != nil
}

// Fresh is in with every filesystem question read from the disk anew
// (Disk, DiskLstat), nothing remembered (T251, L1732): what runs after a
// line enters the registry runs on Fresh, so no fact read before it (when
// a sandboxed command could still change the disk) can stand in for the
// disk then. Every post-registry check takes its Input through Fresh at its
// own entry.
func (in Input) Fresh() Input {
	in.Stat, in.Lstat = Disk, DiskLstat
	return in
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
	roots := repo.Roots()
	main := ""
	if len(roots) == 4 {
		// The top, the main worktree, and each one's <root>-worktrees.
		main = roots[1]
	}
	in := Input{
		Cwd: cwd, Roots: append(roots, workspace.DiskRoots(repo)...), CommonDir: repo.Common, Main: main, Worktrees: repo.Worktrees(),
		Tmp: scratchRoots(), Home: project.Realish(project.Home()), Env: map[string]string{},
		Path:          filepath.SplitList(os.Getenv("PATH")),
		RuntimeDir:    "/run/user/" + uid,
		SocketFolders: []string{"/tmp/tmux-" + uid, "/tmp/.X11-unix", "/tmp/.ICE-unix", "/tmp/gentle-pi-" + uid},
		Stat:          remembered(Disk),
		Lstat:         DiskLstat,
		Empty:         diskEmpty,
	}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		in.Env[k] = v
	}
	in.ContainerHosts = DockerContexts(in.Home, in.Env)
	return in
}

// scratchRoots are /tmp and $TMPDIR as written (absolute), not where their
// links lead (L26): project.TmpRoots resolves them, and a $TMPDIR a command
// replaced with a link would be bound where the link leads.
func scratchRoots() []string {
	out := []string{"/tmp"}
	if t := os.Getenv("TMPDIR"); filepath.IsAbs(t) && filepath.Clean(t) != "/tmp" {
		out = append(out, t)
	}
	return out
}
