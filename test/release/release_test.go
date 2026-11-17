// Unit and end-to-end tests for the committed release (docket AV1).
//
// `jhon sync` resolves HEAD of the source repository, archives it into
// ~/.local/share/jhon/releases/<commit>/, validates the release, switches
// ~/.local/share/jhon/current to it atomically, and installs the runtime
// projections from the release: everything the runtime executes or loads
// resolves through `current`, never through the live source. The renderer
// alone never installs, and the installed CLI never runs live source.
//
// Every case runs in a scratch HOME; the source is a scratch git repository
// holding a copy of this working tree. `claude` is shadowed by a stub that
// fails, so `jhon sync`'s session note never reaches a real runtime. Sync never
// pushes; the publish cases give the scratch repository bare remotes of its
// own, so nothing leaves the scratch directories.
package release

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// syncSlots bounds the release cases running at once: each holds a copy of
// the tree and builds releases of it.
var syncSlots = make(chan struct{}, 4)

func parallel(t *testing.T) {
	t.Parallel()
	syncSlots <- struct{}{}
	t.Cleanup(func() { <-syncSlots })
}

func temp(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// scratch is a scratch HOME and a bin directory whose `claude` always fails.
type scratch struct{ home, bin string }

func scratchHome(t *testing.T) scratch {
	t.Helper()
	home := temp(t)
	bin := filepath.Join(home, "stub-bin")
	support.Write(t, home, map[string]string{
		"stub-bin/claude":   "#!/bin/sh\nexit 1\n",
		".claude/CLAUDE.md": "@RTK.md\n\n<!-- jhon:start -->\n<!-- jhon:end -->\n",
	})
	if err := os.Chmod(filepath.Join(bin, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	return scratch{home, bin}
}

// scratchSource is a scratch source repository: this working tree, committed once.
func scratchSource(t *testing.T) string {
	t.Helper()
	src := filepath.Join(temp(t), "jhon")
	if err := os.Rename(support.CopyRepo(t), src); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(src, ".git")); err == nil {
		t.Fatal("the copy holds a .git: its git commands would reach the real repository")
	}
	support.Git(t, src, "init", "-q")
	support.Git(t, src, "add", "-A")
	support.Git(t, src, "commit", "-q", "--no-verify", "-m", "release fixture")
	return src
}

var (
	goEnvOnce sync.Once
	goEnv     []string
)

// offlineGo is the real Go caches, offline: a sync builds and tests the
// release's Go source, and under a scratch HOME the toolchain would otherwise
// download the modules into it.
func offlineGo(t *testing.T) []string {
	t.Helper()
	goEnvOnce.Do(func() {
		out, err := exec.Command("go", "env", "GOCACHE", "GOMODCACHE").Output()
		if err != nil {
			return
		}
		v := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(v) == 2 {
			goEnv = []string{"GOCACHE=" + v[0], "GOMODCACHE=" + v[1], "GOPROXY=off", "GOTOOLCHAIN=local"}
		}
	})
	if goEnv == nil {
		t.Fatal("go env GOCACHE GOMODCACHE failed")
	}
	return goEnv
}

func run(t *testing.T, env scratch, cmd string, args []string, extra ...string) support.Result {
	t.Helper()
	// The suite runs the Go tests on their own; one case below runs them through sync.
	// XDG_STATE_HOME is the scratch HOME's own, so a sync never reads the
	// dashboard records of whoever runs the suite (T15.16, L188).
	overrides := append([]string{"HOME=" + env.home, "PATH=" + env.bin + ":" + os.Getenv("PATH"), "JHON_SYNC_GO_TEST=0",
		"XDG_STATE_HOME=" + filepath.Join(env.home, ".local", "state")}, offlineGo(t)...)
	return support.Run(t, support.Cmd{Args: append([]string{cmd}, args...), Env: support.Env(merge(overrides, extra)...)})
}

// merge is base with the KEY=VALUE pairs of over replacing the same keys.
func merge(base, over []string) []string {
	keys := map[string]bool{}
	for _, o := range over {
		k, _, _ := strings.Cut(o, "=")
		keys[k] = true
	}
	var out []string
	for _, b := range base {
		if k, _, _ := strings.Cut(b, "="); !keys[k] {
			out = append(out, b)
		}
	}
	return append(out, over...)
}

// cliOf is the scratch source's own CLI: the binary copied with it.
func cliOf(src string) string { return filepath.Join(src, "build", "jhon") }

func syncOf(t *testing.T, env scratch, src string) support.Result {
	t.Helper()
	return run(t, env, cliOf(src), []string{"sync"})
}

func share(home string) string { return filepath.Join(home, ".local", "share", "jhon") }

func head(t *testing.T, src string) string {
	t.Helper()
	return strings.TrimSpace(support.Git(t, src, "rev-parse", "HEAD"))
}

func realpath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func appendTo(t *testing.T, p, text string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(support.Read(t, p)+text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

// tree is every path under dir, relative and sorted.
func tree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != dir {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

// namingSource is every file under dir whose text names the live source, src,
// or a default one, `~/dev/jhon/` or `~/.agents/jhon/`, written with ~ or
// under home.
func namingSource(t *testing.T, dir, home, src string) []string {
	t.Helper()
	out := []string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		text := support.Read(t, p)
		named := slices.ContainsFunc([]string{"~/.agents/jhon/", "~/dev/jhon/", home + "/.agents/jhon/", home + "/dev/jhon/", src + "/"}, func(s string) bool { return strings.Contains(text, s) })
		if named {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func wantCode(t *testing.T, r support.Result, code int) {
	t.Helper()
	if r.Code != code {
		t.Fatalf("exit %d, want %d\n%s", r.Code, code, r.Out())
	}
}

func wantFail(t *testing.T, r support.Result) {
	t.Helper()
	if r.Code == 0 {
		t.Fatalf("exit 0, want nonzero\n%s", r.Out())
	}
}

func match(t *testing.T, text, re string) {
	t.Helper()
	if !regexp.MustCompile(re).MatchString(text) {
		t.Fatalf("no match for %s in:\n%s", re, text)
	}
}

func noMatch(t *testing.T, text, re string) {
	t.Helper()
	if regexp.MustCompile(re).MatchString(text) {
		t.Fatalf("unexpected match for %s in:\n%s", re, text)
	}
}

func equal[T any](t *testing.T, got, want T, msg ...any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v %v", got, want, msg)
	}
}

// bareRemote is a bare repository registered as remote name of src.
func bareRemote(t *testing.T, src, name string) string {
	t.Helper()
	parent := temp(t)
	bare := filepath.Join(parent, name+".git")
	support.Git(t, parent, "init", "-q", "--bare", bare)
	support.Git(t, src, "remote", "add", name, bare)
	return bare
}

func branchOf(t *testing.T, src string) string {
	t.Helper()
	return strings.TrimSpace(support.Git(t, src, "branch", "--show-current"))
}

func remoteRef(t *testing.T, bare, branch string) string {
	t.Helper()
	r := support.Run(t, support.Cmd{Args: []string{"git", "-C", bare, "rev-parse", "--verify", "-q", "refs/heads/" + branch}, Env: support.Env()})
	return strings.TrimSpace(r.Stdout)
}

func TestRelease(t *testing.T) {
	t.Run("AV1: jhon sync archives HEAD into a release, validates it, points current at it, and installs everything through current", func(t *testing.T) {
		parallel(t)
		env := scratchHome(t)
		src := scratchSource(t)
		r := syncOf(t, env, src)
		wantCode(t, r, 0)
		commit := head(t, src)
		release := filepath.Join(share(env.home), "releases", commit)
		if !exists(filepath.Join(release, "build", "jhon")) {
			t.Fatal(r.Out())
		}
		if exists(filepath.Join(release, ".git")) {
			t.Fatal("a release is an archive, not a checkout")
		}
		// The release's binary carries its commit: 0.0.0-dev+<short commit>,
		// the commit, and the commit's date (never the build time).
		date := strings.TrimSpace(support.Git(t, src, "log", "-1", "--format=%cI", commit))
		v := run(t, env, filepath.Join(release, "build", "jhon"), []string{"version"})
		equal(t, v.Stdout, "jhon 0.0.0-dev+"+commit[:7]+" ("+commit[:12]+", "+date+")\n")
		current := filepath.Join(share(env.home), "current")
		if fi, err := os.Lstat(current); err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("current is not a link: %v", err)
		}
		equal(t, realpath(t, current), release)

		// The CLI link, the command links, the hook commands, and the imports.
		link, err := os.Readlink(filepath.Join(env.home, ".local", "bin", "jhon"))
		if err != nil {
			t.Fatal(err)
		}
		equal(t, link, filepath.Join(current, "build", "jhon"))
		cmds := filepath.Join(env.home, ".claude", "commands")
		linked := []string{}
		if exists(cmds) {
			linked = names(t, cmds)
		}
		equal(t, linked, []string{}, "a command was linked globally")
		if !exists(filepath.Join(current, "runtimes", "claude-code", "plugin", ".claude-plugin", "plugin.json")) {
			t.Fatal("the release lacks its plugin")
		}
		orch := support.Read(t, filepath.Join(env.home, ".claude", "agents", "jhon-orchestrator.md"))
		var commands []string
		for _, m := range regexp.MustCompile(`(?m)^ {10}command: (".+")$`).FindAllStringSubmatch(orch, -1) {
			var c string
			if err := json.Unmarshal([]byte(m[1]), &c); err != nil {
				t.Fatal(err)
			}
			commands = append(commands, c)
		}
		if len(commands) < 3 {
			t.Fatal(orch[:min(2000, len(orch))])
		}
		for _, c := range commands {
			if !strings.HasPrefix(c, "'"+filepath.Join(current, "build", "jhon")+"' 'hook' ") {
				t.Fatal(c)
			}
		}
		// The hooks run the release's own binary, built by the sync.
		guard := func(command string) support.Result {
			payload, _ := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": command}, "cwd": env.home})
			return support.Run(t, support.Cmd{Args: []string{filepath.Join(current, "build", "jhon"), "hook", "safety-guard"},
				Env: support.Env("HOME=" + env.home), Stdin: string(payload)})
		}
		wantCode(t, guard("ls"), 0)
		wantCode(t, guard("rm -rf /"), 2)
		equal(t, support.Read(t, filepath.Join(env.home, ".claude", "CLAUDE.md")), "@RTK.md\n", "the global CLAUDE.md kept Jhon text")
		match(t, orch, `(?m)^## Strict TDD$`)

		// Nothing installed names the live source: agents, skills, and command targets.
		equal(t, namingSource(t, filepath.Join(env.home, ".claude", "agents"), env.home, src), []string{})
		equal(t, namingSource(t, filepath.Join(release, "prose", "skills"), env.home, src), []string{})
		if exists(filepath.Join(env.home, ".agents", "skills", "ldd")) {
			t.Fatal("a skill was projected into the global root")
		}
		equal(t, namingSource(t, filepath.Join(release, "runtimes", "claude-code", "plugin"), env.home, src), []string{})

		// Provenance comes from the captured commit.
		var record struct{ Commit string }
		if err := json.Unmarshal([]byte(support.Read(t, filepath.Join(env.home, ".cache", "jhon", "install.json"))), &record); err != nil {
			t.Fatal(err)
		}
		equal(t, record.Commit, commit)
	})

	t.Run("AV1: a second sync switches current to the new commit and keeps the old release; an uncommitted edit governs nothing", func(t *testing.T) {
		parallel(t)
		env := scratchHome(t)
		src := scratchSource(t)
		wantCode(t, syncOf(t, env, src), 0)
		first := head(t, src)
		skill := filepath.Join(src, "prose", "skills", "ldd", "references", "apply.md")
		appendTo(t, skill, "\nsecond release\n")
		support.Git(t, src, "commit", "-q", "--no-verify", "-am", "second")
		second := head(t, src)
		r := syncOf(t, env, src)
		wantCode(t, r, 0)
		equal(t, realpath(t, filepath.Join(share(env.home), "current")), filepath.Join(share(env.home), "releases", second))
		if !exists(filepath.Join(share(env.home), "releases", first, "prose", "agents.yaml")) {
			t.Fatal("old releases are kept")
		}
		applied := filepath.Join(share(env.home), "current", "prose", "skills", "ldd", "references", "apply.md")
		match(t, support.Read(t, applied), `second release`)

		// A dirty tree is refused, and nothing it holds reaches the runtime.
		appendTo(t, skill, "\nuncommitted\n")
		dirty := syncOf(t, env, src)
		wantFail(t, dirty)
		match(t, dirty.Out(), `working tree is dirty`)
		equal(t, realpath(t, filepath.Join(share(env.home), "current")), filepath.Join(share(env.home), "releases", second))
		noMatch(t, support.Read(t, applied), `uncommitted`)
	})

	t.Run("AV1: a release that fails validation is never activated", func(t *testing.T) {
		parallel(t)
		env := scratchHome(t)
		src := scratchSource(t)
		wantCode(t, syncOf(t, env, src), 0)
		good := head(t, src)
		yaml := filepath.Join(src, "prose", "agents.yaml")
		support.Write(t, src, map[string]string{"prose/agents.yaml": strings.Replace(support.Read(t, yaml), "inspect_allow: [", "inspect_allowed: [", 1)})
		support.Git(t, src, "commit", "-q", "--no-verify", "-am", "broken")
		r := syncOf(t, env, src)
		wantFail(t, r)
		match(t, r.Out(), `unknown top-level key 'inspect_allowed'`)
		equal(t, realpath(t, filepath.Join(share(env.home), "current")), filepath.Join(share(env.home), "releases", good))
		if exists(filepath.Join(share(env.home), "releases", head(t, src))) {
			t.Fatal("an invalid release is not kept")
		}
	})

	t.Run("AV1: a release whose binary does not build is never activated", func(t *testing.T) {
		parallel(t)
		env := scratchHome(t)
		src := scratchSource(t)
		wantCode(t, syncOf(t, env, src), 0)
		good := head(t, src)
		support.Write(t, src, map[string]string{"engine/internal/cli/broken.go": "package cli\n\nfunc broken() { undefinedInRelease() }\n"})
		support.Git(t, src, "add", "-A")
		support.Git(t, src, "commit", "-q", "--no-verify", "-m", "broken build")
		r := syncOf(t, env, src)
		wantFail(t, r)
		match(t, r.Out(), `undefinedInRelease`)
		match(t, r.Out(), `failed to build build/jhon; nothing was activated`)
		equal(t, realpath(t, filepath.Join(share(env.home), "current")), filepath.Join(share(env.home), "releases", good))
		if exists(filepath.Join(share(env.home), "releases", head(t, src))) {
			t.Fatal("a release that does not build is not kept")
		}
	})

	t.Run("AV1: a release whose Go tests fail is never activated", func(t *testing.T) {
		parallel(t)
		env := scratchHome(t)
		src := scratchSource(t)
		support.Write(t, src, map[string]string{"engine/internal/cli/broken_test.go": "package cli\n\nimport \"testing\"\n\nfunc TestReleaseFixture(t *testing.T) { t.Fatal(\"a failing Go test\") }\n"})
		support.Git(t, src, "add", "-A")
		support.Git(t, src, "commit", "-q", "--no-verify", "-m", "failing test")
		r := run(t, env, cliOf(src), []string{"sync"}, "JHON_SYNC_GO_TEST=")
		wantFail(t, r)
		// The fixture's test is the failure, never a release test that hung
		// until go test's -timeout killed it.
		noMatch(t, r.Out(), `panic: test timed out`)
		match(t, r.Out(), `a failing Go test`)
		match(t, r.Out(), `failed the preflight; nothing was activated`)
		if exists(filepath.Join(share(env.home), "current")) {
			t.Fatal(r.Out())
		}
		if exists(filepath.Join(share(env.home), "releases", head(t, src))) {
			t.Fatal("a release whose tests fail is not kept")
		}
	})

	t.Run("upgrade: the command links an earlier release made are pruned, and current switches to the new release", func(t *testing.T) {
		parallel(t)
		env := scratchHome(t)
		src := scratchSource(t)
		// An earlier release linked its commands into ~/.claude/commands through current.
		cmds := filepath.Join(env.home, ".claude", "commands")
		support.Write(t, cmds, map[string]string{"mine.md": "mine\n"})
		if err := os.Symlink(filepath.Join(share(env.home), "current", "commands", "jhon-old-new.md"), filepath.Join(cmds, "jhon-old-new.md")); err != nil {
			t.Fatal(err)
		}
		r := syncOf(t, env, src)
		wantCode(t, r, 0)
		equal(t, realpath(t, filepath.Join(share(env.home), "current")), filepath.Join(share(env.home), "releases", head(t, src)))
		equal(t, names(t, cmds), []string{"mine.md"}, "an earlier release's command link survived")
	})

	t.Run("upgrade: a settings.json with disableAllHooks or invalid JSON stops sync before current switches", func(t *testing.T) {
		parallel(t)
		for _, bad := range []string{`{"disableAllHooks":true}`, "{ not json"} {
			env := scratchHome(t)
			src := scratchSource(t)
			wantCode(t, syncOf(t, env, src), 0)
			good := head(t, src)
			settings := filepath.Join(env.home, ".claude", "settings.json")
			if exists(settings) {
				t.Fatal("sync created settings.json")
			}
			support.Write(t, env.home, map[string]string{".claude/settings.json": bad})
			appendTo(t, filepath.Join(src, "prose", "skills", "ldd", "references", "apply.md"), "\nnext release\n")
			support.Git(t, src, "commit", "-q", "--no-verify", "-am", "next")
			r := syncOf(t, env, src)
			wantFail(t, r)
			match(t, r.Out(), `settings\.json`)
			equal(t, realpath(t, filepath.Join(share(env.home), "current")), filepath.Join(share(env.home), "releases", good), bad)
			equal(t, support.Read(t, settings), bad)
			noMatch(t, support.Read(t, filepath.Join(share(env.home), "current", "prose", "skills", "ldd", "references", "apply.md")), `next release`)
		}
	})

	t.Run("AV1: the installed CLI runs the release, and JHON_HOME cannot point it back at the source", func(t *testing.T) {
		parallel(t)
		env := scratchHome(t)
		src := scratchSource(t)
		wantCode(t, syncOf(t, env, src), 0)
		r := run(t, env, filepath.Join(env.home, ".local", "bin", "jhon"), []string{"help"}, "JHON_HOME="+src)
		wantCode(t, r, 0)
		match(t, r.Out(), `runtime: ~/\.local/share/jhon/current`)
		noMatch(t, r.Out(), `runtime: .*`+regexp.QuoteMeta(src))
	})

	t.Run("AV1: jhon render and a bare renderer install nothing", func(t *testing.T) {
		parallel(t)
		env := scratchHome(t)
		src := scratchSource(t)
		before := tree(t, env.home)
		viaCli := run(t, env, cliOf(src), []string{"render"})
		wantFail(t, viaCli)
		match(t, viaCli.Out(), `jhon sync`)
		wantCode(t, run(t, env, cliOf(src), []string{"render-engine"}, "JHON_ROOT="+src), 2)
		install := run(t, env, cliOf(src), []string{"render-engine", "--install", "--source", src}, "JHON_ROOT="+src)
		wantCode(t, install, 2)
		match(t, install.Out(), `not a release`)
		equal(t, tree(t, env.home), before)
	})

	// Docket BB1: `jhon sync --help` once published a release, because sync read
	// no argument. A help flag on any command prints the usage and runs nothing;
	// sync takes no argument, so any other one is a usage error.
	t.Run("BB1: jhon sync --help or -h prints the usage and builds, activates, and installs nothing", func(t *testing.T) {
		parallel(t)
		env := scratchHome(t)
		src := scratchSource(t)
		before := tree(t, env.home)
		for _, flag := range []string{"--help", "-h"} {
			r := run(t, env, cliOf(src), []string{"sync", flag})
			wantCode(t, r, 0)
			match(t, r.Out(), `jhon sync\s+build a release`)
		}
		equal(t, tree(t, env.home), before)
	})

	t.Run("BB1: jhon sync with any other argument is a usage error and changes nothing", func(t *testing.T) {
		parallel(t)
		env := scratchHome(t)
		src := scratchSource(t)
		before := tree(t, env.home)
		r := run(t, env, cliOf(src), []string{"sync", "--dry-run"})
		wantCode(t, r, 2)
		match(t, r.Out(), `sync takes no argument`)
		equal(t, tree(t, env.home), before)
	})

	t.Run("BB1: a help flag after any command prints the usage instead of running it", func(t *testing.T) {
		parallel(t)
		env := scratchHome(t)
		src := scratchSource(t)
		r := run(t, env, cliOf(src), []string{"executions", "run", "--help"})
		wantCode(t, r, 0)
		match(t, r.Out(), `jhon sync\s+build a release`)
	})

	// Sync activates locally; publishing is explicit. `jhon sync` builds,
	// activates, and installs, and never pushes; `jhon publish` pushes the current
	// branch to origin and mirror, and only what is installed: a clean tree whose
	// HEAD is the active release. `jhon doctor` asks the remotes only with
	// --remote.
	t.Run("publish: jhon sync activates locally and leaves every remote ref unchanged", func(t *testing.T) {
		parallel(t)
		env := scratchHome(t)
		src := scratchSource(t)
		origin := bareRemote(t, src, "origin")
		mirror := bareRemote(t, src, "mirror")
		r := syncOf(t, env, src)
		wantCode(t, r, 0)
		equal(t, realpath(t, filepath.Join(share(env.home), "current")), filepath.Join(share(env.home), "releases", head(t, src)))
		equal(t, remoteRef(t, origin, branchOf(t, src)), "", r.Out())
		equal(t, remoteRef(t, mirror, branchOf(t, src)), "", r.Out())
	})

	t.Run("publish: jhon publish pushes the active release's branch to origin and mirror", func(t *testing.T) {
		parallel(t)
		env := scratchHome(t)
		src := scratchSource(t)
		origin := bareRemote(t, src, "origin")
		mirror := bareRemote(t, src, "mirror")
		wantCode(t, syncOf(t, env, src), 0)
		r := run(t, env, cliOf(src), []string{"publish"})
		wantCode(t, r, 0)
		equal(t, remoteRef(t, origin, branchOf(t, src)), head(t, src), r.Out())
		equal(t, remoteRef(t, mirror, branchOf(t, src)), head(t, src), r.Out())
	})

	t.Run("publish: jhon publish refuses a dirty tree or a HEAD that is not the active release, and pushes nothing", func(t *testing.T) {
		parallel(t)
		env := scratchHome(t)
		src := scratchSource(t)
		origin := bareRemote(t, src, "origin")
		wantCode(t, syncOf(t, env, src), 0)
		appendTo(t, filepath.Join(src, "prose", "skills", "ldd", "references", "apply.md"), "\nuncommitted\n")
		dirty := run(t, env, cliOf(src), []string{"publish"})
		wantFail(t, dirty)
		match(t, dirty.Out(), `working tree is dirty`)
		equal(t, remoteRef(t, origin, branchOf(t, src)), "", dirty.Out())

		support.Git(t, src, "commit", "-q", "--no-verify", "-am", "not synced")
		unsynced := run(t, env, cliOf(src), []string{"publish"})
		wantFail(t, unsynced)
		match(t, unsynced.Out(), `not the active release`)
		equal(t, remoteRef(t, origin, branchOf(t, src)), "", unsynced.Out())
	})

	t.Run("publish: jhon doctor asks no remote without --remote, and says the check was skipped", func(t *testing.T) {
		parallel(t)
		env := scratchHome(t)
		src := scratchSource(t)
		support.Git(t, src, "remote", "add", "origin", filepath.Join(temp(t), "gone.git"))
		plain := run(t, env, cliOf(src), []string{"doctor"})
		match(t, plain.Out(), `remote check skipped`)
		noMatch(t, plain.Out(), `origin is gone`)
		remote := run(t, env, cliOf(src), []string{"doctor", "--remote"})
		match(t, remote.Out(), `origin is gone`)
	})

	// Git resolves a relative core.hooksPath against the work tree, so the
	// absolute path to the same `tools/git-hooks` directory activates the same pre-commit.
	t.Run("doctor: an absolute core.hooksPath naming the source's git hooks directory is active", func(t *testing.T) {
		parallel(t)
		env := scratchHome(t)
		src := scratchSource(t)
		support.Git(t, src, "config", "core.hooksPath", filepath.Join(src, "tools", "git-hooks"))
		match(t, run(t, env, cliOf(src), []string{"doctor"}).Out(), `pre-commit hook active`)
		support.Git(t, src, "config", "core.hooksPath", filepath.Join(temp(t), "hooks"))
		match(t, run(t, env, cliOf(src), []string{"doctor"}).Out(), `hook inactive`)
	})

	t.Run("doctor: a jhon MCP server registered for every session is flagged; the release's plugin carries it", func(t *testing.T) {
		parallel(t)
		env := scratchHome(t)
		src := scratchSource(t)
		noMatch(t, run(t, env, cliOf(src), []string{"doctor"}).Out(), `jhon MCP server is registered`)
		support.Write(t, env.home, map[string]string{".claude.json": `{"mcpServers":{"jhon":{"type":"stdio","command":"jhon","args":["mcp"]}}}`})
		match(t, run(t, env, cliOf(src), []string{"doctor"}).Out(), `FAIL.*a jhon MCP server is registered for every session.*claude mcp remove jhon -s user`)
	})
}
