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

// WU7 (A3), round 3: the files Claude Code and pi run outside the sandbox
// (.claude, .mcp.json, .pi), in each root of the project and in the
// repository's other worktrees, cannot be written, created, moved or
// removed from inside, nor the worktree that holds one (pinned): the
// verifier planted a SessionStart hook in a sibling worktree on 1459cc1.
// The harness's own tools, which run outside, still edit them. A worktree
// that holds one is removed outside the sandbox (3 of the 163 removals in
// the owner's 30 days), and the note says so; one that holds none is
// removed from inside as before. Only the paths that are there are mounted:
// a project with no .claude gets one from a command (stays open, below).
func TestTheProjectsHookFilesCannotBePlantedInside(t *testing.T) {
	b := newBox(t)
	support.Write(t, b.proj, map[string]string{".mcp.json": "{}\n", ".pi/settings.json": "{}\n", ".claude/settings.json": "{}\n"})
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
		{"a worktree's .claude", wt, `printf '%s' '` + hooksJSON + `' > .claude/settings.local.json`},
		{"a worktree's MCP servers", wt, `echo '{"mcpServers":{}}' > .mcp.json`},
		{"another worktree's .claude, from the main checkout", b.proj, `printf '%s' '` + hooksJSON + `' > ` + wt + `/.claude/settings.local.json`},
		{"moving another worktree aside, from the main checkout", b.proj, `mv ` + wt + ` ` + wt + `-old && mkdir -p ` + wt + `/.claude && printf '%s' '` + hooksJSON + `' > ` + wt + `/.claude/settings.local.json`},
		{"moving .claude aside", b.proj, `mv .claude .claude-old && mkdir .claude && printf '%s' '` + hooksJSON + `' > .claude/settings.local.json`},
		{"moving .pi aside", b.proj, `mv .pi .pi-old`},
		{"removing .mcp.json", b.proj, `rm .mcp.json && echo '{"mcpServers":{}}' > .mcp.json`},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := b.sh(t, c.dir, "", c.script)
			if r.Code == 0 {
				t.Errorf("exit 0, stderr %q; want the write refused", r.Stderr)
			}
		})
	}
	for _, p := range []string{filepath.Join(b.proj, ".claude", "settings.local.json"), filepath.Join(wt, ".claude", "settings.local.json"), filepath.Join(b.proj, ".pi", "extensions"), filepath.Join(b.proj, ".claude-old"), filepath.Join(b.proj, ".pi-old"), wt + "-old"} {
		if exists(p) {
			t.Errorf("%s was planted", p)
		}
	}
	if got := support.Read(t, filepath.Join(b.proj, ".mcp.json")); got != "{}\n" {
		t.Errorf(".mcp.json now holds %q", got)
	}
	// The note says how to change one: with the agent's own file tools.
	r := b.sh(t, b.proj, "", `echo '{}' > "$PWD/.mcp.json"`)
	note := "the sandbox refused a write to " + filepath.Join(b.proj, ".mcp.json") + ", which exists; it is " + filepath.Join(b.proj, ".mcp.json") + ", a file a runtime runs outside the sandbox (Claude Code's settings, hooks and MCP servers, pi's settings and extensions), read-only by design: change it with the agent's own file tools (write, edit), which run outside the sandbox, never with a shell command; no setting opens it\n"
	if !readOnly(r) || !strings.HasSuffix(r.Stderr, note) {
		t.Errorf("a write to .mcp.json: exit %d, stderr %q; want it refused, ending with %q", r.Code, r.Stderr, note)
	}
	// B3 (owner's decision, option a): in a project with no .claude, the
	// run gets an empty read-only one, which bubblewrap leaves on the disk,
	// empty; no command can put settings or hooks there. Likewise .pi. No
	// .mcp.json is made: an empty file would show in git status.
	bare := filepath.Join(b.base, "bare-project")
	mkdirs(t, bare)
	support.GitRepo(t, bare)
	r = support.Jhon(t, bare, b.env, "sandbox", "run", "--", "sh", "-c", `mkdir -p .claude; echo '{}' > .claude/settings.json && echo planted; mkdir -p .pi/extensions && echo x > .pi/extensions/x.ts && echo planted-pi; ls -A .claude .pi; echo ok`)
	if r.Code != 0 || strings.Contains(r.Stdout, "planted") || !strings.HasSuffix(r.Stdout, "ok\n") {
		t.Errorf("a project with no .claude or .pi: exit %d, stdout %q, stderr %q; want nothing planted", r.Code, r.Stdout, r.Stderr)
	}
	for _, d := range []string{".claude", ".pi"} {
		if entries, err := os.ReadDir(filepath.Join(bare, d)); err != nil || len(entries) != 0 {
			t.Errorf("%s on the disk after the run: %v, %d entries; want an empty folder", d, err, len(entries))
		}
	}
	if exists(filepath.Join(bare, ".mcp.json")) {
		t.Error("an empty .mcp.json was left on the disk")
	}
	if st := support.Git(t, bare, "status", "--porcelain"); st != "" {
		t.Errorf("git status after the run: %q, want clean (an empty folder is no change)", st)
	}
	// An ordinary run there is unaffected.
	if r := support.Jhon(t, bare, b.env, "sandbox", "run", "--", "sh", "-c", `echo x > f && cat f`); r.Code != 0 || r.Stdout != "x\n" {
		t.Errorf("an ordinary run in a project with no .claude: exit %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}
	// Preserved: the rest of the project and .git (a commit, git config, a
	// hook in .git/hooks: not mounted, see TestGitHooksAreWritableInside),
	// and the harness's tools edit the hook files outside.
	r = b.sh(t, b.proj, "", `echo x > src.txt && git add src.txt && git -c user.name=t -c user.email=t@example.invalid -c commit.gpgsign=false commit -q -m x && git config branch.main.description x && mkdir -p sub/.claude && echo ok > sub/.claude/f && echo done`)
	if r.Code != 0 || r.Stdout != "done\n" {
		t.Errorf("ordinary work in the project: exit %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}
	support.Write(t, b.proj, map[string]string{".claude/settings.local.json": "{}\n"})
	// Another worktree with no hook file is removed from inside, also one a
	// session ran in as its own project, which left an empty .claude and
	// .pi there; one with a hook file is not, and the note says to remove
	// it outside.
	plain := filepath.Join(b.home, "project-worktrees", "plain")
	support.Git(t, b.proj, "worktree", "add", "-q", "-b", "plain", plain)
	if r := b.sh(t, plain, "", `echo ran`); r.Code != 0 || !exists(filepath.Join(plain, ".claude")) {
		t.Fatalf("a run in the worktree plain: exit %d, stderr %q; want it run, and an empty .claude left", r.Code, r.Stderr)
	}
	r = b.sh(t, b.proj, "", `git worktree remove `+plain+` && echo removed`)
	if r.Code != 0 || r.Stdout != "removed\n" || exists(plain) {
		t.Errorf("git worktree remove of a worktree with no hook file: exit %d, stdout %q, stderr %q; want it removed", r.Code, r.Stdout, r.Stderr)
	}
	// git removes what it can and its record, and fails on the pinned
	// folder: the note names the folder and, as git no longer knows the
	// worktree, only the fix that still works (git worktree remove would
	// say "is not a working tree").
	r = b.sh(t, b.proj, "", `git worktree remove --force `+wt+` && echo removed`)
	if r.Code == 0 || !exists(filepath.Join(wt, ".claude", "settings.json")) || !strings.Contains(r.Stderr, "it is "+wt+", a folder the sandbox keeps where it is") ||
		!strings.Contains(r.Stderr, "git has already removed the rest of the worktree and its record, so git worktree remove no longer knows it: remove what is left outside the sandbox, in a terminal: rm -r "+wt+"; no setting opens it") || strings.Contains(r.Stderr, "git worktree remove "+wt) {
		t.Errorf("git worktree remove of a worktree that holds .claude: exit %d, stdout %q, stderr %q; want it refused with the rm -r note only", r.Code, r.Stdout, r.Stderr)
	}
	if r := support.Run(t, support.Cmd{Args: []string{"git", "-C", b.proj, "worktree", "list"}, Env: support.Env()}); strings.Contains(r.Stdout, wt) {
		t.Errorf("git still lists the worktree: %q", r.Stdout)
	}
	if err := os.RemoveAll(wt); err != nil || exists(wt) {
		t.Errorf("outside the sandbox what is left is removed: %v", err)
	}
}

// T250 round 3 (B2): git's hooks folder is writable inside the sandbox, on
// purpose. git takes its hooks from core.hooksPath, which a sandboxed
// command may set in .git/config, writable inside (251 of 21,212 bash calls
// of the owner's 30 days write it), so a read-only .git/hooks contained
// nothing (the verify moved .git aside and planted one) and cost `rm -rf
// .git`. The CHANGELOG records why; re-adding the mount needs new evidence.
func TestGitHooksAreWritableInside(t *testing.T) {
	b := newBox(t)
	mkdirs(t, filepath.Join(b.proj, ".git", "hooks"))
	r := b.sh(t, b.proj, "", `printf '#!/bin/sh\n' > .git/hooks/pre-commit && rm -rf "$PWD/.git" && git init -q . && echo done`)
	if r.Code != 0 || r.Stdout != "done\n" {
		t.Errorf("a hook written and .git removed inside: exit %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}
	changelog := support.Read(t, support.Path("CHANGELOG.md"))
	if !strings.Contains(changelog, "git's hooks folder is not mounted read-only") {
		t.Error("the CHANGELOG no longer says why git's hooks folder is not mounted read-only")
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

// WU4 (b): a socket in a writable area (here $TMPDIR) is hidden, and every
// folder on the way to it there is pinned (round 3), so no run can rename
// one and read the socket under the new name in the next one: $SSH_AUTH_SOCK's,
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

// WU4 (b), round 3: the folder that holds a hidden socket is the folder the
// command runs in: it is pinned, not hidden, so the run goes, the socket
// stays hidden, and the folder cannot be moved to reach it.
func TestASocketInTheWorkingFolderStaysHiddenAndTheRunGoes(t *testing.T) {
	b := newBox(t)
	run := filepath.Join(b.proj, "run")
	support.Write(t, run, map[string]string{"agent.sock": "SSH-SECRET"})
	sock := filepath.Join(run, "agent.sock")
	env := append(slices.Clone(b.env), "SSH_AUTH_SOCK="+sock)
	r := support.Jhon(t, run, env, "sandbox", "run", "--", "sh", "-c", `cat "$1" 2>/dev/null; mv "$2" "$2.x" 2>/dev/null; rm -f "$1" 2>/dev/null; echo ran`, "sh", sock, run)
	if r.Code != 0 || r.Stdout != "ran\n" || !exists(sock) || exists(run+".x") {
		t.Errorf("exit %d, stdout %q, stderr %q; want only ran, the socket and its folder in place", r.Code, r.Stdout, r.Stderr)
	}
}

// T250 A5, round 3: no command can put a link to a store the sandbox keeps
// read-only in place of the sandbox's own pnpm store (it is pinned), and a
// link left there by an older jhon is replaced before the run, with a note,
// so installs inside do not fail on a read-only store nobody connects to it.
func TestTheSandboxsOwnStoreCannotBeSwappedForTheRealOne(t *testing.T) {
	b := newBox(t)
	real := filepath.Join(b.home, ".local", "share", "pnpm", "store")
	mkdirs(t, real)
	own := filepath.Join(b.home, ".cache", "jhon", "sandbox", "pnpm-store")
	r := b.sh(t, b.proj, "", `rm -rf "$pnpm_config_store_dir" 2>/dev/null; mv "$pnpm_config_store_dir" "$pnpm_config_store_dir.x" 2>/dev/null; mv "$(dirname "$pnpm_config_store_dir")" "$(dirname "$pnpm_config_store_dir").x" 2>/dev/null; ln -s '`+real+`' "$pnpm_config_store_dir" 2>/dev/null; echo x > "$pnpm_config_store_dir/f" && echo wrote`)
	if r.Stdout != "wrote\n" || !exists(filepath.Join(own, "f")) {
		t.Errorf("the own store after an attempt to swap it: exit %d, stdout %q, stderr %q; want it in place and writable", r.Code, r.Stdout, r.Stderr)
	}
	if fi, err := os.Lstat(own); err != nil || !fi.IsDir() {
		t.Fatalf("the own store is not a folder: %v %v", fi, err)
	}
	// A link left by an older jhon: replaced, with a note.
	if err := os.RemoveAll(own); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, own); err != nil {
		t.Fatal(err)
	}
	r = b.sh(t, b.proj, "", `echo x > "$pnpm_config_store_dir/f" && echo wrote`)
	note := "jhon sandbox: the sandbox's own store " + own + " was a link to " + real + ", which a sandboxed command may have put there, so installs inside would have failed on a read-only store; it is a folder of its own again\n"
	if r.Stdout != "wrote\n" || !strings.HasPrefix(r.Stderr, note) {
		t.Errorf("a linked own store: exit %d, stdout %q, stderr %q; want it replaced with the note %q", r.Code, r.Stdout, r.Stderr, note)
	}
	if entries, _ := os.ReadDir(real); len(entries) != 0 {
		t.Errorf("the real store was written: %v", entries)
	}
}

// T250 (the re-verify of 5e9b12e, advisory): $SSH_AUTH_SOCK through a link
// in $TMPDIR, which a run could point at an empty folder (`ln -sfn empty
// $TMPDIR/link`) so the next run hides that and shows the socket: nothing
// runs, and the note gives the real path to set.
func TestASocketThroughALinkInAWritableAreaRefusesTheRun(t *testing.T) {
	b := newBox(t)
	support.Write(t, filepath.Join(b.tmp, "real"), map[string]string{"agent.sock": "AGENT-SECRET"})
	link := filepath.Join(b.tmp, "link")
	if err := os.Symlink(filepath.Join(b.tmp, "real"), link); err != nil {
		t.Fatal(err)
	}
	env := append(slices.Clone(b.env), "SSH_AUTH_SOCK="+filepath.Join(link, "agent.sock"))
	r := support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `ln -sfn "$1" "$2"; echo ran`, "sh", filepath.Join(b.tmp, "empty"), link)
	want := "jhon sandbox: $SSH_AUTH_SOCK names " + filepath.Join(link, "agent.sock") + ", which passes through the link " + link + " in a folder sandboxed commands may write, so a command could point it elsewhere and the sandbox would hide the wrong path, so nothing was run; fix: set SSH_AUTH_SOCK to the socket's real path (" + filepath.Join(b.tmp, "real", "agent.sock") + ")\n"
	if r.Code != 126 || r.Stdout != "" || r.Stderr != want {
		t.Errorf("exit %d, stdout %q, stderr %q; want 126 and %q", r.Code, r.Stdout, r.Stderr, want)
	}
	// With the real path, the run goes and the socket stays hidden.
	env = append(slices.Clone(b.env), "SSH_AUTH_SOCK="+filepath.Join(b.tmp, "real", "agent.sock"))
	r = support.Jhon(t, b.proj, env, "sandbox", "run", "--", "sh", "-c", `cat "$1"; echo ran`, "sh", filepath.Join(b.tmp, "real", "agent.sock"))
	if r.Code != 0 || r.Stdout != "ran\n" {
		t.Errorf("with the real path: exit %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}
}
