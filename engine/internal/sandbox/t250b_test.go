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

// T250 A3 (WU7, round 3): the files the runtimes run outside the sandbox
// (Claude Code's .claude and .mcp.json, pi's .pi) are read-only in every
// root of the project and in the repository's other worktrees; another
// worktree that holds one is pinned (bound onto itself), so no command can
// move it aside and put a new one in its place. git's hooks folder is not
// mounted: core.hooksPath in the writable .git/config redirects git anyway.
func TestTheProjectsHookFilesAreReadOnlyInsideTheProject(t *testing.T) {
	d := with(machine, disk{
		"/home/u/project/.claude": "dir", "/home/u/project/.mcp.json": "file", "/home/u/project/.pi": "dir", "/home/u/project/.git/hooks": "dir",
		"/home/u/project-worktrees": "dir", "/home/u/project-worktrees/a": "dir", "/home/u/project-worktrees/a/.claude": "dir", "/home/u/project-worktrees/a/.mcp.json": "file",
		"/home/u/project-worktrees/b": "dir",
		"/home/u/member":              "dir", "/home/u/member/.git": "dir", "/home/u/member/.pi": "dir", "/home/u/member/.git/hooks": "dir",
		"/home/u/project/src": "dir", "/home/u/project/src/.claude": "dir",
	})
	in := sample(d)
	in.Roots = append(in.Roots, "/home/u/member")
	in.Worktrees = []string{"/home/u/project-worktrees/a", "/home/u/project-worktrees/b"}
	args := Args(in)
	proj := index(args, "--bind", "/home/u/project")
	for _, ro := range []string{"/home/u/project/.claude", "/home/u/project/.mcp.json", "/home/u/project/.pi", "/home/u/member/.pi", "/home/u/project-worktrees/a/.claude", "/home/u/project-worktrees/a/.mcp.json"} {
		if at := index(args, "--ro-bind", ro); at < 0 || at < proj {
			t.Errorf("%s is not read-only inside the project (project at %d, it at %d):\n%s", ro, proj, at, strings.Join(args, " "))
		}
	}
	for _, none := range []string{"/home/u/project/.git/hooks", "/home/u/member/.git/hooks", "/home/u/project/src/.claude"} {
		if index(args, "--ro-bind", none) >= 0 {
			t.Errorf("%s is mounted:\n%s", none, strings.Join(args, " "))
		}
	}
	// The worktree that holds one is pinned; one that holds none is not, so
	// the session still removes it.
	if index(args, "--bind", "/home/u/project-worktrees/a") < 0 || index(args, "--bind", "/home/u/project-worktrees/b") >= 0 {
		t.Errorf("worktree a must be pinned and b not:\n%s", strings.Join(args, " "))
	}
	// A session in a worktree: it, and its main checkout, are roots.
	wt := in
	wt.Cwd, wt.Roots, wt.Worktrees = "/home/u/project-worktrees/a", []string{"/home/u/project-worktrees/a", "/home/u/project", "/home/u/project-worktrees"}, nil
	args = Args(wt)
	for _, ro := range []string{"/home/u/project-worktrees/a/.claude", "/home/u/project-worktrees/a/.mcp.json", "/home/u/project/.claude"} {
		if index(args, "--ro-bind", ro) < 0 {
			t.Errorf("from the worktree, %s is not read-only:\n%s", ro, strings.Join(args, " "))
		}
	}
	if index(args, "--bind", "/home/u/project-worktrees/a") < 0 {
		t.Errorf("the worktree the session works in, which lies in another writable area, is not pinned:\n%s", strings.Join(args, " "))
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

// T250 WU4 (b), round 3 (B1): a socket the sandbox hides that lies in a
// writable area is hidden at its path, and every folder on the way to it
// whose own folder a command may write, up to the outermost writable area,
// is pinned (bound onto itself), so none can be renamed: not $TMPDIR's
// ssh-X, not a writable area nested in another (an allow_write folder in
// $TMPDIR), not the working folder that holds the socket. A pin hides
// nothing, so no run is refused for it.
func TestASocketInAWritableAreaIsPinnedOnTheWayFromTheOutermostArea(t *testing.T) {
	d := with(machine, disk{
		"/home/u/tmp": "dir", "/home/u/tmp/ssh-X": "dir", "/home/u/tmp/ssh-X/agent.1": "file", "/home/u/tmp/agent.sock": "file",
		"/home/u/tmp/dbus-Y": "dir", "/home/u/tmp/dbus-Y/a": "dir", "/home/u/tmp/dbus-Y/a/bus": "file",
		"/home/u/tmp/dock": "dir", "/home/u/tmp/dock/docker.sock": "file",
		"/home/u/tmp/a": "dir", "/home/u/tmp/a/inner": "dir", "/home/u/tmp/a/inner/x": "dir", "/home/u/tmp/a/inner/x/agent.sock": "file",
		"/home/u/project/run": "dir", "/home/u/project/run/agent.sock": "file",
	})
	in := sample(d)
	in.Tmp = []string{"/tmp", "/home/u/tmp"}
	in.Env["TMPDIR"] = "/home/u/tmp"
	in.Env["SSH_AUTH_SOCK"] = "/home/u/tmp/ssh-X/agent.1"
	in.Env["DBUS_SESSION_BUS_ADDRESS"] = "unix:path=/home/u/tmp/dbus-Y/a/bus,guid=x"
	in.Env["DOCKER_HOST"] = "unix:///home/u/tmp/dock/docker.sock"
	args := Args(in)
	for _, hidden := range []string{"/home/u/tmp/ssh-X/agent.1", "/home/u/tmp/dbus-Y/a/bus", "/home/u/tmp/dock/docker.sock"} {
		if index(args, "--dev-bind", hidden) < 0 {
			t.Errorf("%s is not hidden:\n%s", hidden, strings.Join(args, " "))
		}
	}
	for _, pin := range []string{"/home/u/tmp/ssh-X", "/home/u/tmp/dbus-Y", "/home/u/tmp/dbus-Y/a", "/home/u/tmp/dock"} {
		if index(args, "--bind", pin) < 0 {
			t.Errorf("%s, on the way to a hidden socket, is not pinned:\n%s", pin, strings.Join(args, " "))
		}
	}
	if index(args, "--bind", "/home/u/tmp") < 0 || index(args, "--bind", "/home/u/tmp/a") >= 0 {
		t.Errorf("$TMPDIR must be bound and nothing unrelated in it pinned:\n%s", strings.Join(args, " "))
	}
	// B1: an allow_write folder inside $TMPDIR holds the socket: the way
	// from $TMPDIR, the outermost area, is pinned, the inner area included.
	in.AllowWrite = []string{"/home/u/tmp/a/inner"}
	in.Env["SSH_AUTH_SOCK"] = "/home/u/tmp/a/inner/x/agent.sock"
	args = Args(in)
	for _, pin := range []string{"/home/u/tmp/a", "/home/u/tmp/a/inner", "/home/u/tmp/a/inner/x"} {
		if index(args, "--bind", pin) < 0 {
			t.Errorf("%s is not pinned:\n%s", pin, strings.Join(args, " "))
		}
	}
	// Directly in the area: the hide is its own mount point, nothing pinned.
	in.AllowWrite = nil
	in.Env["SSH_AUTH_SOCK"] = "/home/u/tmp/agent.sock"
	if args := Args(in); index(args, "--dev-bind", "/home/u/tmp/agent.sock") < 0 || index(args, "--bind", "/home/u/tmp/ssh-X") >= 0 {
		t.Errorf("a socket directly in $TMPDIR:\n%s", strings.Join(args, " "))
	}
	// The folder that holds the socket is the working folder: pinned, not
	// hidden, and the run goes.
	in.Env["SSH_AUTH_SOCK"] = "/home/u/project/run/agent.sock"
	in.Cwd = "/home/u/project/run"
	args = Args(in)
	if index(args, "--bind", "/home/u/project/run") < 0 || index(args, "--tmpfs", "/home/u/project/run") >= 0 || index(args, "--dev-bind", "/home/u/project/run/agent.sock") < 0 {
		t.Errorf("the working folder that holds the socket:\n%s", strings.Join(args, " "))
	}
}

// T250 A5: the sandbox's own stores are pinned where they exist, so no
// command can put a link to a read-only store in their place.
func TestTheSandboxsOwnStoresArePinned(t *testing.T) {
	d := with(machine, disk{"/home/u/.cache/jhon": "dir", "/home/u/.cache/jhon/sandbox": "dir", "/home/u/.cache/jhon/sandbox/uv": "dir", "/home/u/.cache/jhon/sandbox/pnpm-store": "dir"})
	args := Args(sample(d))
	for _, pin := range []string{"/home/u/.cache/jhon", "/home/u/.cache/jhon/sandbox", "/home/u/.cache/jhon/sandbox/uv", "/home/u/.cache/jhon/sandbox/pnpm-store"} {
		if index(args, "--bind", pin) < 0 {
			t.Errorf("%s is not pinned:\n%s", pin, strings.Join(args, " "))
		}
	}
}

// T250 B3 (owner's decision, option a): a root of the project with no
// .claude or .pi gets an empty read-only folder there, which bubblewrap
// makes as its mount point (an empty folder is left on the disk, the
// accepted price); no command can then create one with settings, hooks or
// extensions. Not .mcp.json: an empty file there shows as an untracked file
// in every project's git status. Not the repository's other worktrees,
// where a mount point stops `git worktree remove`; an empty .claude or .pi
// there (left by a session run in that worktree) is not mounted either.
func TestAMissingClaudeOrPiFolderInARootIsAnEmptyReadOnlyFolder(t *testing.T) {
	d := with(machine, disk{
		"/home/u/project-worktrees": "dir", "/home/u/project-worktrees/a": "dir", "/home/u/project-worktrees/a/.claude": "dir",
		"/home/u/project-worktrees/b": "dir", "/home/u/project-worktrees/b/.git": "file", "/home/u/project-worktrees/c": "dir", "/home/u/project-worktrees/c/.claude": "dir", "/home/u/project-worktrees/c/.claude/settings.json": "file",
	})
	in := sample(d)
	in.Worktrees = []string{"/home/u/project-worktrees/a", "/home/u/project-worktrees/b", "/home/u/project-worktrees/c"}
	in.Empty = func(p string) bool { return p == "/home/u/project-worktrees/a/.claude" }
	args := Args(in)
	proj := index(args, "--bind", "/home/u/project")
	for _, empty := range []string{"/home/u/project/.claude", "/home/u/project/.pi"} {
		if at := index(args, "--tmpfs", empty); at < 0 || at < proj {
			t.Errorf("%s is not an empty read-only folder after the project's bind:\n%s", empty, strings.Join(args, " "))
		}
	}
	if !strings.Contains(strings.Join(args, " "), "--remount-ro /home/u/project/.claude") {
		t.Errorf("the empty .claude is not read-only:\n%s", strings.Join(args, " "))
	}
	// Nor in the <root>-worktrees folder, which is a root but no git top.
	for _, none := range []string{"/home/u/project/.mcp.json", "/home/u/project-worktrees/a/.claude", "/home/u/project-worktrees/b/.claude", "/home/u/project-worktrees/b/.pi", "/home/u/project-worktrees/.claude", "/home/u/project-worktrees/.pi"} {
		if strings.Contains(strings.Join(args, " ")+" ", " "+none+" ") {
			t.Errorf("%s is mounted:\n%s", none, strings.Join(args, " "))
		}
	}
	for _, pinned := range []string{"/home/u/project-worktrees/a", "/home/u/project-worktrees/b"} {
		if index(args, "--bind", pinned) >= 0 {
			t.Errorf("%s, a worktree with no hook file in it, is pinned:\n%s", pinned, strings.Join(args, " "))
		}
	}
	if index(args, "--ro-bind", "/home/u/project-worktrees/c/.claude") < 0 || index(args, "--bind", "/home/u/project-worktrees/c") < 0 {
		t.Errorf("worktree c, whose .claude holds a file, is not covered:\n%s", strings.Join(args, " "))
	}
	// A root the sandbox keeps read-only (Jhon's source checkout, from its
	// linked worktree) gets no empty folder: nothing can be made there, and
	// bubblewrap could not make the mount point.
	ro := in
	ro.Protected = append(slices.Clone(in.Protected), "/home/u/project")
	if args := Args(ro); index(args, "--tmpfs", "/home/u/project/.claude") >= 0 {
		t.Errorf("a read-only root gets an empty .claude mount:\n%s", strings.Join(args, " "))
	}
	// A session in worktree b: b is a root, so its missing .claude is an
	// empty read-only folder, and b, inside another writable area, pinned.
	wt := in
	wt.Cwd, wt.Roots, wt.Worktrees = "/home/u/project-worktrees/b", []string{"/home/u/project-worktrees/b", "/home/u/project", "/home/u/project-worktrees"}, nil
	args = Args(wt)
	if index(args, "--tmpfs", "/home/u/project-worktrees/b/.claude") < 0 || index(args, "--bind", "/home/u/project-worktrees/b") < 0 {
		t.Errorf("a session in worktree b:\n%s", strings.Join(args, " "))
	}
}
