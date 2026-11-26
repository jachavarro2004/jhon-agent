package sandbox

// T250 work units 5 to 7 (odd/tasks/v0-1-release.md T250): each test is
// the attack, what a sandboxed command could change that runs outside the
// sandbox later, and the ordinary work around it that keeps working.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// WU6 (A5): a file of a package store that the owner's own projects share
// by inode (a hard link from the store into an install outside the
// sandbox) cannot be changed in place from inside: uv's cache, pnpm's
// store and bun's cache are read-only there, while the sandbox installs
// from stores of its own, copied.
func TestAStoreFileTheOwnersInstallsShareCannotBeChangedInside(t *testing.T) {
	b := newBox(t)
	installed := filepath.Join(b.home, "elsewhere", "venv")
	mkdirs(t, installed)
	for _, store := range []string{".cache/uv/archive-v0/x", ".local/share/pnpm/store/v11/files/00", ".bun/install/cache/pkg@1", ".pnpm-store/v11/files/00"} {
		dir := filepath.Join(b.home, store)
		support.Write(t, dir, map[string]string{"mod.py": "ORIGINAL\n"})
		linked := filepath.Join(installed, strings.NewReplacer("/", "-").Replace(store)+".py")
		if err := os.Link(filepath.Join(dir, "mod.py"), linked); err != nil {
			t.Fatal(err)
		}
		r := support.Jhon(t, b.proj, b.env, "sandbox", "run", "--", "sh", "-c", `echo PLANTED > "$1"; mv "$2" "$2.moved"`, "sh", filepath.Join(dir, "mod.py"), dir)
		if got := support.Read(t, linked); got != "ORIGINAL\n" || !readOnly(r) {
			t.Errorf("~/%s: exit %d, stderr %q, the owner's installed copy holds %q; want a read-only refusal and the original", store, r.Code, r.Stderr, got)
		}
		if !exists(dir) {
			t.Errorf("~/%s was moved", store)
		}
		note := "the sandbox refused a write to " + filepath.Join(dir, "mod.py") + ", which exists; it is in "
		tail := ", a package store whose files your installs outside the sandbox share, read-only by design: installs inside use the sandbox's own ($UV_CACHE_DIR, pnpm's store-dir, $BUN_INSTALL_CACHE_DIR), and no setting opens it; that is a stop: tell the owner the path and why, never write it elsewhere\n"
		if !strings.Contains(r.Stderr, note) || !strings.HasSuffix(r.Stderr, tail) {
			t.Errorf("~/%s: the note on the refused write is %q; want %q…%q", store, r.Stderr, note, tail)
		}
	}
	// The sandbox's own stores: named, copied into a project, and writable.
	r := b.sh(t, b.proj, "", `printf '%s|' "$UV_CACHE_DIR" "$UV_LINK_MODE" "$pnpm_config_store_dir" "$npm_config_store_dir" "$pnpm_config_package_import_method" "$npm_config_package_import_method" "$BUN_INSTALL_CACHE_DIR"; mkdir -p "$UV_CACHE_DIR" "$pnpm_config_store_dir" "$BUN_INSTALL_CACHE_DIR" && echo x > "$UV_CACHE_DIR/f" && echo made`)
	own := filepath.Join(b.home, ".cache", "jhon", "sandbox")
	want := own + "/uv|copy|" + own + "/pnpm-store|" + own + "/pnpm-store|copy|copy|" + own + "/bun|made\n"
	if r.Code != 0 || r.Stdout != want {
		t.Errorf("the sandbox's own stores: exit %d, stdout %q, stderr %q; want %q", r.Code, r.Stdout, r.Stderr, want)
	}
	// The owner's own variables are replaced, not kept.
	env := append(b.env, "UV_CACHE_DIR="+filepath.Join(b.home, ".cache", "uv"), "UV_LINK_MODE=hardlink")
	r = support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `echo "$UV_CACHE_DIR $UV_LINK_MODE"`)
	if r.Stdout != filepath.Join(own, "uv")+" copy\n" {
		t.Errorf("with the owner's UV_CACHE_DIR and UV_LINK_MODE set: stdout %q, stderr %q", r.Stdout, r.Stderr)
	}
	// Preserved: the rest of the cache and the other stores stay writable.
	for _, rel := range []string{".cache/pip-like/f", ".npm/f"} {
		mkdirs(t, filepath.Dir(filepath.Join(b.home, rel)))
		if r := b.sh(t, b.proj, "", `echo x > "$HOME/`+rel+`"`); r.Code != 0 {
			t.Errorf("a write to ~/%s: exit %d, stderr %q; want it written", rel, r.Code, r.Stderr)
		}
	}
}

// WU7 (A3): the files Claude Code and pi run outside the sandbox, and git's
// hooks, in each root of the project (the folder the session works in, a
// worktree's main checkout), cannot be written, created, moved or removed
// from inside; a missing .claude folder or hooks folder is made, empty,
// before the run, so none can be planted where it was missing. The
// harness's own tools, which run outside, still edit them, and the session
// still removes another worktree of the repository (no mount there). Only
// the hook paths that are there are mounted: Jhon makes none (the owner's
// decision), so a project with no .claude folder gets one from a command.
func TestTheProjectsHookFilesCannotBePlantedInside(t *testing.T) {
	b := newBox(t)
	support.Write(t, b.proj, map[string]string{".mcp.json": "{}\n", ".pi/settings.json": "{}\n", ".claude/settings.json": "{}\n"})
	mkdirs(t, filepath.Join(b.proj, ".git", "hooks"))
	wt := filepath.Join(b.home, "project-worktrees", "w")
	support.Git(t, b.proj, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "base")
	support.Git(t, b.proj, "worktree", "add", "-q", "-b", "w", wt)
	support.Write(t, wt, map[string]string{".mcp.json": "{}\n", ".claude/settings.json": "{}\n"})
	hooksJSON := `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"touch /tmp/pwned"}]}]}}`
	for _, c := range []struct{ name, dir, script string }{
		{"Claude Code's local settings", b.proj, `printf '%s' '` + hooksJSON + `' > .claude/settings.local.json`},
		{"Claude Code's MCP servers", b.proj, `echo '{"mcpServers":{"x":{"command":"sh"}}}' > .mcp.json`},
		{"pi's extensions", b.proj, `mkdir -p .pi/extensions && echo 'x' > .pi/extensions/x.ts`},
		{"pi's settings", b.proj, `echo '{}' >> .pi/settings.json`},
		{"git's hooks", b.proj, `printf '#!/bin/sh\ntouch /tmp/pwned\n' > .git/hooks/pre-commit`},
		{"a worktree's .claude", wt, `printf '%s' '` + hooksJSON + `' > .claude/settings.local.json`},
		{"a worktree's MCP servers", wt, `echo '{"mcpServers":{}}' > .mcp.json`},
		{"git's hooks from a worktree", wt, `printf '#!/bin/sh\n' > "$(git rev-parse --git-common-dir)/hooks/post-checkout"`},
		{"moving .claude aside", b.proj, `mv .claude .claude-old && mkdir .claude && printf '%s' '` + hooksJSON + `' > .claude/settings.local.json`},
		{"moving .pi aside", b.proj, `mv .pi .pi-old`},
		{"removing .mcp.json", b.proj, `rm .mcp.json && echo '{"mcpServers":{}}' > .mcp.json`},
		{"moving the hooks folder aside", b.proj, `mv .git/hooks .git/hooks-old && mkdir .git/hooks && printf '#!/bin/sh\n' > .git/hooks/pre-push`},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := b.sh(t, c.dir, "", c.script)
			if r.Code == 0 {
				t.Errorf("exit 0, stderr %q; want the write refused", r.Stderr)
			}
		})
	}
	for _, p := range []string{filepath.Join(b.proj, ".claude", "settings.local.json"), filepath.Join(wt, ".claude", "settings.local.json"), filepath.Join(b.proj, ".pi", "extensions"), filepath.Join(b.proj, ".git", "hooks", "pre-commit"), filepath.Join(b.proj, ".git", "hooks", "post-checkout"), filepath.Join(b.proj, ".claude-old"), filepath.Join(b.proj, ".pi-old"), filepath.Join(b.proj, ".git", "hooks-old")} {
		if exists(p) {
			t.Errorf("%s was planted", p)
		}
	}
	if got := support.Read(t, filepath.Join(b.proj, ".mcp.json")); got != "{}\n" {
		t.Errorf(".mcp.json now holds %q", got)
	}
	// The note says how to change one: with the agent's own file tools.
	r := b.sh(t, b.proj, "", `echo '{}' > "$PWD/.mcp.json"`)
	note := "the sandbox refused a write to " + filepath.Join(b.proj, ".mcp.json") + ", which exists; it is " + filepath.Join(b.proj, ".mcp.json") + ", a file a runtime runs outside the sandbox (Claude Code's settings, hooks and MCP servers, pi's settings and extensions, git's hooks), read-only by design: change it with the agent's own file tools (write, edit), which run outside the sandbox, never with a shell command; no setting opens it\n"
	if !readOnly(r) || !strings.HasSuffix(r.Stderr, note) {
		t.Errorf("a write to .mcp.json: exit %d, stderr %q; want it refused, ending with %q", r.Code, r.Stderr, note)
	}
	// What stays open, on the record: in a project with no .claude, Jhon
	// makes none, and a command can make one with settings and hooks.
	bare := filepath.Join(b.base, "bare-project")
	mkdirs(t, bare)
	support.GitRepo(t, bare)
	r = support.Jhon(t, bare, b.env, "sandbox", "run", "--", "sh", "-c", `test -e .claude && echo there; mkdir .claude && echo '{}' > .claude/settings.json && echo planted`)
	if r.Code != 0 || r.Stdout != "planted\n" {
		t.Errorf("a project with no .claude: exit %d, stdout %q, stderr %q; want no folder made by Jhon and the command's own made", r.Code, r.Stdout, r.Stderr)
	}
	// Preserved: the rest of the project, and .git's own files, are written
	// as before (a commit, a branch, git config), and the harness's tools
	// edit the hook files outside.
	r = b.sh(t, b.proj, "", `echo x > src.txt && git add src.txt && git -c user.name=t -c user.email=t@example.invalid -c commit.gpgsign=false commit -q -m x && git config branch.main.description x && mkdir -p sub/.claude && echo ok > sub/.claude/f && echo done`)
	if r.Code != 0 || r.Stdout != "done\n" {
		t.Errorf("ordinary work in the project: exit %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}
	support.Write(t, b.proj, map[string]string{".claude/settings.local.json": "{}\n"})
	// Preserved: another worktree, whose .claude and .mcp.json are tracked,
	// is removed from the main checkout's session.
	support.Write(t, b.proj, map[string]string{"tracked/.keep": ""})
	other := filepath.Join(b.home, "project-worktrees", "other")
	support.Git(t, b.proj, "worktree", "add", "-q", "-b", "other", other)
	support.Write(t, other, map[string]string{".claude/settings.json": "{}\n", ".mcp.json": "{}\n"})
	support.Git(t, other, "add", ".claude", ".mcp.json")
	support.Git(t, other, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "hooks")
	r = b.sh(t, b.proj, "", `git worktree remove `+other+` && echo removed`)
	if r.Code != 0 || r.Stdout != "removed\n" || exists(other) {
		t.Errorf("git worktree remove of another worktree: exit %d, stdout %q, stderr %q; want it removed", r.Code, r.Stdout, r.Stderr)
	}
}

// WU7 (A3): a hook path that is a link would be mounted where it leads,
// leaving the link itself for a command to repoint: the run is refused,
// with the link named.
func TestAHookFolderThatIsALinkRefusesTheRun(t *testing.T) {
	b := newBox(t)
	real := filepath.Join(b.home, "shared-claude")
	mkdirs(t, real)
	link := filepath.Join(b.proj, ".claude")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	r := b.sh(t, b.proj, "", `echo ran`)
	want := "jhon sandbox: " + link + " is a link, which a sandboxed command could point at files it wrote, and Claude Code runs what that folder holds outside the sandbox, so nothing was run; fix: replace the link " + link + " with the folder or file it leads to\n"
	if r.Code != 126 || r.Stdout != "" || r.Stderr != want {
		t.Errorf("exit %d, stdout %q, stderr %q; want 126 and %q", r.Code, r.Stdout, r.Stderr, want)
	}
}

// WU5 (A8): `jhon sync` builds jhon with Go caches no sandboxed command can
// write: a go env file one could change refuses the build before go runs.
func TestSyncRefusesAGoEnvFileASandboxedCommandMayChange(t *testing.T) {
	b := newBox(t)
	src := filepath.Join(b.base, "src")
	mkdirs(t, src)
	support.GitRepo(t, src)
	support.Write(t, src, map[string]string{"engine/cmd/jhon/main.go": "package main\n\nfunc main() {}\n", "go.mod": "module x\n\ngo 1.21\n"})
	support.Git(t, src, "add", "-A")
	support.Git(t, src, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "src")
	goenv := filepath.Join(b.tmp, "goenv")
	support.Write(t, b.tmp, map[string]string{"goenv": "GOFLAGS=-tags=evil\n"})
	env := append(b.env, "JHON_ROOT="+src, "JHON_SOURCE="+src, "GOENV="+goenv, "JHON_SYNC_GO_TEST=0")
	r := support.Jhon(t, src, env, "sync")
	want := "go reads its settings from " + goenv + ", which a sandboxed command may change, where it could set what the build of jhon takes (GOFLAGS, GOPROXY, GOWORK); fix: keep that file outside the project, /tmp, $TMPDIR, the caches and sandbox.allow_write, or point GOENV at one that is"
	if r.Code == 0 || !strings.Contains(r.Stderr, want) {
		t.Errorf("exit %d, stderr %q; want a refusal naming %s", r.Code, r.Stderr, goenv)
	}
	if releases, _ := filepath.Glob(filepath.Join(b.home, ".local", "share", "jhon", "releases", "*", "build", "jhon")); len(releases) != 0 {
		t.Errorf("a binary was built: %v", releases)
	}
	// The state folder's caches are read-only inside every sandbox.
	r = b.sh(t, b.proj, "", `mkdir -p "$HOME/.local/state/jhon/go/build/00" && echo planted > "$HOME/.local/state/jhon/go/build/00/x-d"`)
	if !readOnly(r) {
		t.Errorf("a write to the state folder's build cache: exit %d, stderr %q; want it refused", r.Code, r.Stderr)
	}
}

// WU4 (b): a socket in a writable area (here $TMPDIR) is hidden at its
// topmost folder there, so no run can rename that folder and read the
// socket under the new name in the next one: $SSH_AUTH_SOCK's,
// $DBUS_SESSION_BUS_ADDRESS's path, $DOCKER_HOST's. One directly in the
// area is its own mount point. What stays open: a socket made while a
// sandboxed run is already going is not hidden from that run.
func TestASocketInAWritableAreaCannotBeMovedAndRead(t *testing.T) {
	b := newBox(t)
	support.Write(t, b.tmp, map[string]string{"ssh-X/agent.1": "SSH-SECRET", "dbus-Y/bus": "BUS-SECRET", "dock/docker.sock": "DOCKER-SECRET", "agent.sock": "FLAT-SECRET", "plain/notes": "kept"})
	env := append(slices.Clone(b.env),
		"SSH_AUTH_SOCK="+filepath.Join(b.tmp, "ssh-X", "agent.1"),
		"DBUS_SESSION_BUS_ADDRESS=unix:path="+filepath.Join(b.tmp, "dbus-Y", "bus")+",guid=x",
		"DOCKER_HOST=unix://"+filepath.Join(b.tmp, "dock", "docker.sock"))
	moves := []string{"ssh-X", "dbus-Y", "dock"}
	for _, f := range moves {
		r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `mv "$1" "$1.moved" && echo moved`, "sh", filepath.Join(b.tmp, f))
		if r.Stdout == "moved\n" || !exists(filepath.Join(b.tmp, f)) {
			t.Errorf("a run moved %s: exit %d, stdout %q, stderr %q", f, r.Code, r.Stdout, r.Stderr)
		}
	}
	r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `cat "$1"/*.moved/* "$1"/ssh-X/agent.1 "$1"/dbus-Y/bus "$1"/dock/docker.sock 2>/dev/null; cat "$1"/plain/notes`, "sh", b.tmp)
	env2 := append(slices.Clone(env), "SSH_AUTH_SOCK="+filepath.Join(b.tmp, "agent.sock"))
	r2 := support.Jhon(t, b.proj, env2, "sandbox", "run", "--", "sh", "-c", `cat "$1"; rm -f "$1"; mv "$1" "$1.x"; ls "$2"`, "sh", filepath.Join(b.tmp, "agent.sock"), b.tmp)
	for _, s := range []string{"SSH-SECRET", "BUS-SECRET", "DOCKER-SECRET"} {
		if strings.Contains(r.Stdout, s) {
			t.Errorf("a run read %s: stdout %q", s, r.Stdout)
		}
	}
	if !strings.HasSuffix(r.Stdout, "kept") {
		t.Errorf("the rest of $TMPDIR is no longer readable: stdout %q, stderr %q", r.Stdout, r.Stderr)
	}
	if strings.Contains(r2.Stdout, "FLAT-SECRET") || !exists(filepath.Join(b.tmp, "agent.sock")) {
		t.Errorf("a socket directly in $TMPDIR: stdout %q, stderr %q, still there %v", r2.Stdout, r2.Stderr, exists(filepath.Join(b.tmp, "agent.sock")))
	}
	// Preserved: $TMPDIR itself is written as before.
	if r := b.sh(t, b.proj, "", `echo x > "$TMPDIR/new" && mkdir "$TMPDIR/d" && mv "$TMPDIR/d" "$TMPDIR/e" && echo ok`); r.Code != 0 || r.Stdout != "ok\n" {
		t.Errorf("ordinary $TMPDIR work: exit %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}
}

// WU4 (b): a socket whose topmost folder in the writable area also holds
// what the run needs (here the folder it runs in) cannot be hidden that
// way: nothing runs, and the note names the socket and the fix.
func TestASocketThatCannotBePinnedRefusesTheRun(t *testing.T) {
	b := newBox(t)
	run := filepath.Join(b.proj, "run")
	support.Write(t, run, map[string]string{"agent.sock": "SSH-SECRET"})
	sock := filepath.Join(run, "agent.sock")
	env := append(slices.Clone(b.env), "SSH_AUTH_SOCK="+sock)
	r := support.Jhon(t, run, env, "sandbox", "run", "--", "sh", "-c", `echo ran; cat "$1"`, "sh", sock)
	want := "jhon sandbox: the socket $SSH_AUTH_SOCK names (" + sock + ") lies in " + b.proj + ", a folder sandboxed commands may write, and the folder that holds it there, " + run + ", also holds the folder the command runs in (" + run + "), which must stay usable, so the sandbox cannot hide that folder, and a command could rename it to reach the socket, so nothing was run; fix: keep the socket outside every folder sandboxed commands may write (under $XDG_RUNTIME_DIR, which the sandbox hides), or unset SSH_AUTH_SOCK before the session starts\n"
	if r.Code != 126 || r.Stdout != "" || r.Stderr != want {
		t.Errorf("exit %d, stdout %q, stderr %q; want 126 and %q", r.Code, r.Stdout, r.Stderr, want)
	}
	// From the project's top, the socket's folder is hidden and the run goes.
	r = support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `cat "$1" 2>/dev/null; mv "$2" "$2.x" 2>/dev/null; echo ran`, "sh", sock, run)
	if r.Code != 0 || r.Stdout != "ran\n" || !exists(run) {
		t.Errorf("from the top: exit %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}
}
