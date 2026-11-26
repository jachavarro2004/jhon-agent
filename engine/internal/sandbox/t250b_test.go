package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// T250 A5 (WU6): the stores whose files the toolchains hard-link into the
// owner's projects (uv's cache, pnpm's store, bun's cache) are read-only
// inside, wherever they are (inside the writable cache, or a store of
// their own), and the sandbox installs from stores of its own, copied.
func TestLinkedStoresAreReadOnlyAndTheSandboxInstallsFromItsOwnCopied(t *testing.T) {
	d := with(machine, disk{
		"/home/u/.cache/uv": "dir", "/home/u/.local/share/pnpm": "dir", "/home/u/.local/share/pnpm/store": "dir",
		"/home/u/.pnpm-store": "dir", "/home/u/.bun": "dir", "/home/u/.bun/install/cache": "dir",
		"/data/uv": "dir", "/data/bun": "dir", "/data/pnpm": "dir",
		// Preserved: the other caches and stores stay writable.
		"/home/u/.npm": "dir", "/home/u/go": "dir", "/home/u/go/pkg": "dir", "/home/u/.cargo": "dir", "/home/u/.cargo/registry": "dir",
	})
	in := sample(d)
	args := Args(in)
	cache := index(args, "--bind", "/home/u/.cache")
	if uv := index(args, "--ro-bind", "/home/u/.cache/uv"); uv < 0 || cache < 0 || uv < cache {
		t.Errorf("uv's cache is not read-only inside the writable cache (cache at %d, uv at %d):\n%s", cache, uv, strings.Join(args, " "))
	}
	for _, store := range []string{"/home/u/.local/share/pnpm/store", "/home/u/.pnpm-store", "/home/u/.bun/install/cache"} {
		if index(args, "--bind", store) >= 0 {
			t.Errorf("%s is writable:\n%s", store, strings.Join(args, " "))
		}
		if slices.Contains(writable(in), store) {
			t.Errorf("%s counts as writable: %v", store, writable(in))
		}
	}
	for _, open := range []string{"/home/u/.npm", "/home/u/go/pkg", "/home/u/.cargo/registry"} {
		if index(args, "--bind", open) < 0 {
			t.Errorf("%s is no longer writable:\n%s", open, strings.Join(args, " "))
		}
	}
	// A store a variable moves is read-only too, also one the owner put
	// inside a writable folder.
	in.Env["UV_CACHE_DIR"], in.Env["BUN_INSTALL_CACHE_DIR"], in.Env["pnpm_config_store_dir"] = "/data/uv", "/data/bun", "/home/u/.cache/pnpm"
	in.AllowWrite = []string{"/data"}
	in.Stat = with(d, disk{"/home/u/.cache/pnpm": "dir", "/data": "dir"}).stat
	args = Args(in)
	for _, store := range []string{"/data/uv", "/data/bun", "/home/u/.cache/pnpm"} {
		if index(args, "--ro-bind", store) < 0 || index(args, "--bind", store) >= 0 {
			t.Errorf("the store %s a variable names is not read-only:\n%s", store, strings.Join(args, " "))
		}
	}
	if index(args, "--bind", "/data") < 0 {
		t.Errorf("the allow_write folder that holds them is no longer writable:\n%s", strings.Join(args, " "))
	}
	in.Env["XDG_CACHE_HOME"] = "/data/cache"
	want := []string{
		"UV_CACHE_DIR=/data/cache/jhon/sandbox/uv", "UV_LINK_MODE=copy",
		"pnpm_config_store_dir=/data/cache/jhon/sandbox/pnpm-store", "npm_config_store_dir=/data/cache/jhon/sandbox/pnpm-store",
		"pnpm_config_package_import_method=copy", "npm_config_package_import_method=copy",
		"BUN_INSTALL_CACHE_DIR=/data/cache/jhon/sandbox/bun",
	}
	if got := StoreEnv(in); !slices.Equal(got, want) {
		t.Errorf("StoreEnv =\n%v\nwant\n%v", got, want)
	}
	// The owner's own values are replaced, not kept beside: a program reads
	// the first of two.
	env := WithEnv([]string{"PATH=/bin", "UV_CACHE_DIR=/data/uv", "UV_LINK_MODE=hardlink", "HOME=/home/u"}, StoreEnv(in))
	if got := strings.Join(env, " "); got != "PATH=/bin HOME=/home/u "+strings.Join(want, " ") {
		t.Errorf("WithEnv = %s", got)
	}
}

// T250 A3 (WU7): the files the runtimes run outside the sandbox, in every
// root of the project (Claude Code's .claude and .mcp.json, pi's .pi, git's
// hooks in the git folder and the common dir), are read-only inside the
// writable project: a sandboxed command can neither change nor move them.
// The repository's other worktrees take no mount, so the session can still
// remove one (git worktree remove).
func TestTheProjectsHookFilesAreReadOnlyInsideTheProject(t *testing.T) {
	d := with(machine, disk{
		"/home/u/project/.claude": "dir", "/home/u/project/.mcp.json": "file", "/home/u/project/.pi": "dir", "/home/u/project/.git/hooks": "dir",
		"/home/u/project-worktrees": "dir", "/home/u/project-worktrees/a": "dir", "/home/u/project-worktrees/a/.claude": "dir", "/home/u/project-worktrees/a/.mcp.json": "file",
		"/home/u/member": "dir", "/home/u/member/.git": "dir", "/home/u/member/.pi": "dir", "/home/u/member/.git/hooks": "dir",
		"/home/u/project/src": "dir", "/home/u/project/src/.claude": "dir",
	})
	in := sample(d)
	in.Roots = append(in.Roots, "/home/u/member")
	in.Worktrees = []string{"/home/u/project-worktrees/a"}
	args := Args(in)
	proj := index(args, "--bind", "/home/u/project")
	for _, ro := range []string{"/home/u/project/.claude", "/home/u/project/.mcp.json", "/home/u/project/.pi", "/home/u/project/.git/hooks", "/home/u/member/.pi", "/home/u/member/.git/hooks"} {
		if at := index(args, "--ro-bind", ro); at < 0 || at < proj {
			t.Errorf("%s is not read-only inside the project (project at %d, it at %d):\n%s", ro, proj, at, strings.Join(args, " "))
		}
	}
	for _, other := range []string{"/home/u/project-worktrees/a/.claude", "/home/u/project-worktrees/a/.mcp.json"} {
		if index(args, "--ro-bind", other) >= 0 {
			t.Errorf("%s, in another worktree, is mounted, so the session could not remove that worktree:\n%s", other, strings.Join(args, " "))
		}
	}
	// A session in that worktree: it is a root, its main checkout another.
	wt := in
	wt.Cwd, wt.Roots, wt.Worktrees = "/home/u/project-worktrees/a", []string{"/home/u/project-worktrees/a", "/home/u/project", "/home/u/project-worktrees"}, nil
	args = Args(wt)
	for _, ro := range []string{"/home/u/project-worktrees/a/.claude", "/home/u/project-worktrees/a/.mcp.json", "/home/u/project/.claude", "/home/u/project/.git/hooks"} {
		if index(args, "--ro-bind", ro) < 0 {
			t.Errorf("from the worktree, %s is not read-only:\n%s", ro, strings.Join(args, " "))
		}
	}
	// Only a root's own: a folder of that name deeper in the project is the
	// project's files.
	if index(args, "--ro-bind", "/home/u/project/src/.claude") >= 0 {
		t.Errorf("a .claude below the root is mounted:\n%s", strings.Join(args, " "))
	}
	// No scratch root is bound inside one.
	in.Env["TMPDIR"] = "/home/u/project/.claude/tmp"
	in.Stat = with(d, disk{"/home/u/project/.claude/tmp": "dir"}).stat
	if args := Args(in); index(args, "--bind", "/home/u/project/.claude/tmp") >= 0 {
		t.Errorf("$TMPDIR inside .claude is bound writable:\n%s", strings.Join(args, " "))
	}
}

// T250 A3 (WU7): a hook path that is a link is kept read-only nowhere (a
// mount follows it): the run is refused, with the link named, and a hook
// path that is none passes.
func TestAHookPathThatIsALinkRefusesTheRun(t *testing.T) {
	_, home, proj, in := t250Disk(t)
	if p := HookProblem(in); p != nil {
		t.Fatalf("a project with no hook path: %+v", p)
	}
	for _, d := range []string{filepath.Join(proj, ".claude"), filepath.Join(home, "elsewhere")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if p := HookProblem(in); p != nil {
		t.Fatalf("a real .claude folder: %+v", p)
	}
	link := filepath.Join(proj, ".pi")
	if err := os.Symlink(filepath.Join(home, "elsewhere"), link); err != nil {
		t.Fatal(err)
	}
	p := HookProblem(in)
	want := link + " is a link, which a sandboxed command could point at files it wrote, and pi runs what that folder holds outside the sandbox"
	if p == nil || p.Reason != want || p.Fix != "replace the link "+link+" with the folder or file it leads to" {
		t.Errorf("HookProblem = %+v, want %q", p, want)
	}
}

// T250 WU4 (b): a socket the sandbox hides that lies in a writable area is
// hidden at its topmost folder inside that area, a mount point no command
// can rename (the socket folders already behave so): $SSH_AUTH_SOCK in
// $TMPDIR, the session bus's path, $DOCKER_HOST's. One directly in the area
// is its own mount point. One whose topmost folder holds what the sandbox
// keeps usable (the working folder, an area) cannot be hidden so: the run
// is refused, naming the socket.
func TestASocketInAWritableAreaIsHiddenAtItsTopmostFolder(t *testing.T) {
	d := with(machine, disk{
		"/home/u/tmp": "dir", "/home/u/tmp/ssh-X": "dir", "/home/u/tmp/ssh-X/agent.1": "file", "/home/u/tmp/agent.sock": "file",
		"/home/u/tmp/dbus-Y": "dir", "/home/u/tmp/dbus-Y/a": "dir", "/home/u/tmp/dbus-Y/a/bus": "file",
		"/home/u/tmp/dock": "dir", "/home/u/tmp/dock/docker.sock": "file",
		"/home/u/project/run": "dir", "/home/u/project/run/agent.sock": "file",
	})
	in := sample(d)
	in.Tmp = []string{"/tmp", "/home/u/tmp"}
	in.Env["TMPDIR"] = "/home/u/tmp"
	in.Env["SSH_AUTH_SOCK"] = "/home/u/tmp/ssh-X/agent.1"
	in.Env["DBUS_SESSION_BUS_ADDRESS"] = "unix:path=/home/u/tmp/dbus-Y/a/bus,guid=x"
	in.Env["DOCKER_HOST"] = "unix:///home/u/tmp/dock/docker.sock"
	args := Args(in)
	for _, top := range []string{"/home/u/tmp/ssh-X", "/home/u/tmp/dbus-Y", "/home/u/tmp/dock"} {
		if index(args, "--tmpfs", top) < 0 {
			t.Errorf("%s, the topmost folder of a socket in $TMPDIR, is not hidden:\n%s", top, strings.Join(args, " "))
		}
	}
	if index(args, "--bind", "/home/u/tmp") < 0 {
		t.Errorf("$TMPDIR is no longer writable:\n%s", strings.Join(args, " "))
	}
	if p := in.SocketProblem(); p != nil {
		t.Errorf("SocketProblem = %+v, want none", p)
	}
	// Directly in the area: the socket is its own mount point.
	in.Env["SSH_AUTH_SOCK"] = "/home/u/tmp/agent.sock"
	if args := Args(in); index(args, "--dev-bind", "/home/u/tmp/agent.sock") < 0 {
		t.Errorf("a socket directly in $TMPDIR is not hidden:\n%s", strings.Join(args, " "))
	}
	// Its topmost folder holds the folder the command runs in.
	in.Env["SSH_AUTH_SOCK"] = "/home/u/project/run/agent.sock"
	in.Cwd = "/home/u/project/run"
	p := in.SocketProblem()
	want := "the socket $SSH_AUTH_SOCK names (/home/u/project/run/agent.sock) lies in /home/u/project, a folder sandboxed commands may write, and the folder that holds it there, /home/u/project/run, also holds the folder the command runs in (/home/u/project/run), which must stay usable, so the sandbox cannot hide that folder, and a command could rename it to reach the socket"
	if p == nil || p.Reason != want || p.Fix != "keep the socket outside every folder sandboxed commands may write (under $XDG_RUNTIME_DIR, which the sandbox hides), or unset SSH_AUTH_SOCK before the session starts" {
		t.Errorf("SocketProblem = %+v, want %q", p, want)
	}
	if args := Args(in); index(args, "--tmpfs", "/home/u/project/run") >= 0 {
		t.Errorf("the working folder is hidden:\n%s", strings.Join(args, " "))
	}
}
