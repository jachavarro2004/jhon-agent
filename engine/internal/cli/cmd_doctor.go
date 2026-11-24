package cli

import (
	"context"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/discover"
	"github.com/jachavarro2004/jhon-agent/engine/internal/hook"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/render"
	"github.com/jachavarro2004/jhon-agent/engine/internal/session"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// `jhon doctor [--remote]` and `jhon doctor --runtime <name>`: every
// health check of the install in one command, in
// sections; exit 1 when a check fails. It makes no network call unless
// --remote, which asks origin and mirror through ls-remote and context7's
// endpoint whether it answers (cmd_doctor_context7.go: every optional
// integration its manifest probes remotely). Its sandbox
// section says whether the OS sandbox is on and can run here (cmd_sandbox.go),
// a warning where it cannot. --runtime
// replaces the install checks with one runtime's guarantees (docket AG3).
func init() { register("doctor", runDoctor) }

func runDoctor(argv []string) (int, error) {
	r, o := release.Locate(), out()
	// The maintainer's doctor judges the checkout; a user's, who has only
	// the release (L51), judges the installed release.
	src, installed := r.Source, false
	if !r.HasSource() {
		rel, ok := installedRelease(r)
		if !ok {
			return die(release.NoSource("doctor", r.Source))
		}
		src, installed = rel, true
	}
	if len(argv) > 0 && argv[0] == "--runtime" {
		return release.RuntimeDoctor(src, argv[1:]), nil
	}
	remote := false
	for _, a := range argv {
		if a != "--remote" {
			return usageError("unknown doctor option: " + a)
		}
		remote = true
	}
	d := doctor{r, o, src}
	o.Head("contracts")
	d.rendered()
	d.discovery()
	d.skills()
	if installed {
		o.Head("integrity")
		o.Note("skipped: an installed release; the pre-commit hook and the publication checks are the source checkout's")
		d.project()
	} else {
		d.integrity()
		d.project()
		d.publication(remote)
	}
	d.sessions()
	d.sandbox()
	d.versions()
	d.remoteProbed(remote)
	fmt.Fprint(o.W, "\n")
	if o.Failed > 0 {
		fmt.Fprintf(o.W, "%s%d problem(s)%s\n", o.R, o.Failed, o.Z)
		return 1, nil
	}
	fmt.Fprintf(o.W, "%shealthy%s\n", o.G, o.Z)
	return 0, nil
}

// versions compares each installed tool with its pin in render's versions
// table (T50g): a mismatch is a warning, never a problem; a tool that is
// not installed, or prints no version, is a note.
func (d doctor) versions() {
	o := d.o
	o.Head("versions")
	for _, t := range render.Tools {
		out, ok := probeVersion(t.Command)
		got := installedVersion(out)
		switch {
		case !ok:
			o.Note(fmt.Sprintf("%s is not installed (Jhon pins %s)", t.Command, t.Version))
		case got == "":
			o.Note(fmt.Sprintf("%s printed no version (Jhon pins %s)", t.Command, t.Version))
		case got == t.Version:
			o.OK(fmt.Sprintf("%s %s (pinned)", t.Command, got))
		default:
			o.Warn(fmt.Sprintf("%s %s is installed; Jhon pins %s", t.Command, got, t.Version))
		}
	}
}

// probeVersion is `<command> --version`'s output, bounded to 5 seconds;
// ok is false when the command is not on PATH or fails.
var probeVersion = func(command string) (string, bool) {
	path, err := exec.LookPath(command)
	if err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	// A shim whose child keeps stdout open must not hold doctor past the bound.
	cmd.WaitDelay = time.Second
	b, err := cmd.Output()
	return string(b), err == nil
}

// installedVersion is the first x.y.z in a --version output, "" when none.
func installedVersion(out string) string {
	return semver.FindString(out)
}

var semver = regexp.MustCompile(`\d+\.\d+\.\d+`)

type doctor struct {
	r release.Roots
	o *release.Out
	// src is the tree judged: the source checkout, or the installed release.
	src string
}

// installedRelease is the release the runtime runs from when it is one under
// the share directory's releases (through current), not a checkout.
func installedRelease(r release.Roots) (string, bool) {
	rel, ok := release.ReadlinkF(r.Runtime)
	if !ok || release.Exists(filepath.Join(rel, ".git")) {
		return "", false
	}
	releases, ok := release.ReadlinkF(filepath.Join(r.Share, "releases"))
	return rel, ok && filepath.Dir(rel) == releases && release.IsFile(filepath.Join(rel, release.Binary))
}

// rendered is the contracts section's verdict, then the rendered
// artifacts': the source's validation and its drift check.
func (d doctor) rendered() {
	o := d.o
	if out, ok := release.Validate(d.src); ok {
		o.OK(release.Last(out))
	} else {
		o.Bad("validation failed")
		for _, l := range release.Lines(out) {
			fmt.Fprintf(o.W, "        %s\n", l)
		}
	}
	o.Head("rendered artifacts")
	out := release.Check(d.src)
	if lineStarting(out, "up to date") {
		o.OK("up to date")
		return
	}
	o.Bad("drift — run 'jhon sync'")
	for _, l := range release.Lines(out) {
		if strings.Contains(l, "DRIFT") {
			fmt.Fprintf(o.W, "      %s\n", l)
		}
	}
}

func lineStarting(s, prefix string) bool {
	for _, l := range release.Lines(s) {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

// discovery: the rendered agents, the release's plugin, and no Jhon
// command link or MCP server registered for every session. With Claude
// Code off in settings.runtimes, ~/.claude is not checked (T56).
func (d doctor) discovery() {
	o, home := d.o, os.Getenv("HOME")
	o.Head("discovery")
	claude, key, source := settings.RuntimeSetting("claude-code")
	if !claude && source == settings.SourceDefault {
		// Off by detection (T10g), not by a setting.
		o.Note(fmt.Sprintf("claude-code was not found (no ~/.claude, no claude on PATH) and %s is not set; ~/.claude is not checked", key))
	} else if !claude {
		o.Note(fmt.Sprintf("claude-code is off (%s = false); ~/.claude is not checked", key))
	} else if n := countNamed(filepath.Join(home, ".claude", "agents"), "*.md", -1); n > 0 {
		o.OK(fmt.Sprintf("%d agents in ~/.claude/agents", n))
		// Diagnostics, they block nothing: the render's record of the agents
		// it wrote, and the main agent's definition, which agent-hooks reads
		// the session's guards from (missing, unreadable or empty, its calls
		// meet no guard).
		record, definition := hook.AgentFiles(home, session.Orchestrator)
		if !release.IsFile(record) {
			o.Bad(fmt.Sprintf("the record of the rendered agents (%s) is missing — run 'jhon sync'", release.Tilde(record)))
		}
		if what := hook.DefinitionProblem(definition); what != "" {
			// A file the owner cannot read is one `jhon sync` cannot write either.
			fix := "run 'jhon sync'"
			if what == hook.DefinitionUnreadable {
				fix = "make it a file the owner can read, or remove it, then run 'jhon sync'"
			}
			o.Bad(fmt.Sprintf("the main agent's definition (%s) %s: no guard judges the main agent's tool calls in a Claude Code session — %s", release.Tilde(definition), what, fix))
		}
	} else {
		o.Bad("no agents rendered for Claude Code")
	}
	// The /jhon-ldd command rides in the release's plugin, loaded by jhon start.
	if release.IsFile(filepath.Join(d.r.Runtime, render.PluginDir, ".claude-plugin", "plugin.json")) {
		n := countNamed(filepath.Join(d.r.Runtime, render.PluginDir, "skills"), "SKILL.md", -1)
		o.OK(fmt.Sprintf("%d commands in the release's plugin (jhon start loads it)", n))
	} else {
		o.Bad("the active release has no plugin; run 'jhon sync'")
	}
	if !claude {
		return
	}
	dir, stale := filepath.Join(home, ".claude", "commands"), ""
	for _, name := range globNames(dir) {
		target, err := os.Readlink(filepath.Join(dir, name))
		if err == nil && strings.HasPrefix(target, d.r.Share+"/") {
			stale += " " + name
		}
	}
	if stale != "" {
		o.Bad(fmt.Sprintf("Jhon command links still in %s:%s — run 'jhon sync'", release.Tilde(dir), stale))
	}
	// The jhon MCP server rides in the plugin too; one registered for every
	// session leaks into plain claude.
	if globalMCP(filepath.Join(home, ".claude.json")) {
		o.Bad("a jhon MCP server is registered for every session; the release's plugin carries it — run 'claude mcp remove jhon -s user'")
	}
}

// globalMCP is whether the file is JSON whose mcpServers.jhon is truthy.
func globalMCP(file string) bool {
	if !release.IsFile(file) {
		return false
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	v, err := js.ParseJSON(js.DecodeUTF8(b))
	return err == nil && js.Truthy(js.Prop(js.Prop(v, "mcpServers"), "jhon"))
}

// skills: every framework skill of the source resolves in the active
// release, and none is left in the global root from an earlier release.
func (d doctor) skills() {
	o, home := d.o, os.Getenv("HOME")
	o.Head("skills")
	owned, missing := 0, ""
	for _, name := range globNames(filepath.Join(d.src, render.ProseDir, "skills")) {
		if !release.IsDir(filepath.Join(d.src, render.ProseDir, "skills", name)) {
			continue
		}
		if release.IsFile(filepath.Join(d.r.Runtime, render.ProseDir, "skills", name, "SKILL.md")) {
			owned++
		} else {
			missing += " " + name
		}
	}
	if missing != "" {
		o.Bad(fmt.Sprintf("not in the active release:%s — run 'jhon sync'", missing))
	} else {
		o.OK(fmt.Sprintf("%d framework skills resolve in the active release", owned))
	}
	global, stale := filepath.Join(home, ".agents", "skills"), ""
	b, _ := os.ReadFile(filepath.Join(global, ".jhon-owned"))
	for _, name := range strings.FieldsFunc(string(b), func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' }) {
		if release.Exists(filepath.Join(global, name)) {
			stale += " " + name
		}
	}
	if stale != "" {
		o.Bad("still in ~/.agents/skills from an earlier release:" + stale + " — run 'jhon sync'")
	}
	o.Note(fmt.Sprintf("%d global skills in ~/.agents/skills (the user's)", countNamed(global, "SKILL.md", 2)))
}

// integrity: the source's pre-commit hook is active. Git resolves a
// relative core.hooksPath against the work tree, so an absolute path naming
// the same directory activates the same hook. Dangling symlinks are
// the renderer's rule (`dangling-symlink`), run in the contracts section.
func (d doctor) integrity() {
	o, src := d.o, d.r.Source
	o.Head("integrity")
	hooks := gitQuiet(src, "config", "core.hooksPath")
	if hooks != "" && !strings.HasPrefix(hooks, "/") {
		hooks = src + "/" + hooks
	}
	if hooks != "" && release.RealpathM(hooks) == release.RealpathM(src+"/tools/git-hooks") {
		o.OK("pre-commit hook active")
	} else {
		o.Bad("hook inactive — run: git -C " + release.Tilde(src) + " config core.hooksPath tools/git-hooks")
	}
}

// project (contracts/ldd.md, "Preconditions"):
// the VCS state of the project enclosing the working directory, a warning
// below ready and never a failure; outside a project, no section.
func (d doctor) project() {
	level, msg, ok := projectLine(cwd())
	if !ok {
		return
	}
	d.o.Head("project")
	if level == "ok" {
		d.o.OK(msg)
	} else {
		d.o.Warn(msg)
	}
	if top := vcs.GitTopLevel(cwd()); top != "" {
		for _, l := range staleLocks(top, time.Now()) {
			d.o.Warn(l)
		}
		// T219 S6: under LDD git refuses a move of the default branch no
		// ready merge check covers, through the merge gate.
		if project.UsesLdd(top) {
			if hook.MergeGateInstalled(top) {
				d.o.OK("merge gate: git in " + top + " refuses a move of the default branch that no ready merge check covers")
			} else {
				d.o.Warn("merge gate: git in " + top + " does not refuse a move of the default branch that no ready merge check covers — run 'jhon hook merge-gate --install' there (route-guard installs it on the next guarded command)")
			}
		}
	}
}

// projectLine is the project's one VCS line, or none when from is in no
// project or the vocabulary is broken (that failure is discarded).
func projectLine(from string) (level, msg string, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	p := enclosingProject(from)
	if p == "" {
		return "", "", false
	}
	v := vcs.State(p)
	if v.State == vcs.Ready() {
		return "ok", fmt.Sprintf("project %s: vcs %s", p, v.State), true
	}
	return "warn", fmt.Sprintf("project %s: %s", p, vcs.Note(v)), true
}

// enclosingProject is the project enclosing from: its git work
// tree, an ancestor holding openspec/, or from itself when it holds a
// project manifest; "" for none.
func enclosingProject(from string) string {
	start, _ := filepath.Abs(from)
	if top := vcs.GitTopLevel(start); top != "" {
		return top
	}
	for d := start; ; d = filepath.Dir(d) {
		if _, ok := layout.Detected(d); ok {
			return d
		}
		if d == filepath.Dir(d) {
			break
		}
	}
	for _, m := range discover.Markers {
		if release.Exists(filepath.Join(start, m.File)) {
			return start
		}
	}
	return ""
}

// publication: with --remote, each remote's branch against HEAD; and
// whether the source has uncommitted changes.
func (d doctor) publication(remote bool) {
	o := d.o
	o.Head("publication")
	if remote {
		d.remotes()
	} else {
		o.Note("remote check skipped; run 'jhon doctor --remote' to compare origin and mirror")
	}
	if gitQuiet(d.r.Source, "status", "--porcelain") != "" {
		o.Warn("working tree has uncommitted changes")
	}
}

// remotes asks each remote, not refs/remotes/<r>/<branch>, which is only as
// fresh as the last fetch or push: one ls-remote each, under 10 seconds,
// compared on the current branch, the one publish pushes. A path remote that
// cannot answer is gone, a failure; a network one may be unreachable now, a
// warning.
func (d doctor) remotes() {
	o, src := d.o, d.r.Source
	local := gitQuiet(src, "rev-parse", "HEAD")
	branch := gitQuiet(src, "branch", "--show-current")
	for _, r := range []string{"origin", "mirror"} {
		url, ok := gitQuietOK(src, "remote", "get-url", r)
		if !ok {
			o.Warn(r + " not configured, skipped")
			continue
		}
		listing, ok := lsRemote(src, r)
		if !ok {
			if strings.HasPrefix(url, "/") || strings.HasPrefix(url, "file:") {
				o.Bad(fmt.Sprintf("%s is gone: %s is not a git repository", r, release.Tilde(url)))
			} else {
				o.Warn(fmt.Sprintf("%s unreachable (%s) — offline, or the remote is down", r, url))
			}
			continue
		}
		remoteHead := refHead(listing, "refs/heads/"+branch)
		switch {
		case remoteHead == "":
			o.Bad(fmt.Sprintf("%s has no branch '%s' — run 'jhon publish'", r, branch))
		case remoteHead == local:
			o.OK(fmt.Sprintf("%s up to date (%s)", r, shortSha(local)))
		case readGit(src, "merge-base", "--is-ancestor", remoteHead, local).Run() == nil:
			o.Warn(fmt.Sprintf("%s behind local (%s vs %s) — run 'jhon publish'", r, shortSha(remoteHead), shortSha(local)))
		default:
			// Not an ancestor, or not here because never fetched: publish
			// would be rejected either way.
			o.Warn(fmt.Sprintf("%s diverged (%s vs %s) — fetch and reconcile", r, shortSha(remoteHead), shortSha(local)))
		}
	}
}

// lsRemote is `GIT_TERMINAL_PROMPT=0 timeout 10 git ls-remote <r>`: the
// listing, and whether git answered within the deadline.
func lsRemote(src, r string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "git", "-C", src, "ls-remote", r)
	c.Cancel = func() error { return c.Process.Signal(syscall.SIGTERM) }
	c.WaitDelay = time.Second
	c.Env = append(project.GitReadEnv(), "GIT_TERMINAL_PROMPT=0")
	b, err := c.Output()
	return release.Substituted(b), err == nil
}

// refHead is awk's `$2==ref {print $1}` over an ls-remote listing.
func refHead(listing, ref string) string {
	var heads []string
	for _, l := range strings.Split(listing, "\n") {
		if f := strings.Fields(l); len(f) >= 2 && f[1] == ref {
			heads = append(heads, f[0])
		}
	}
	return strings.Join(heads, "\n")
}

func shortSha(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// sessions (docket AI1): a live orchestrator session started before the
// last install runs its old prompt with the new guards and agents, a
// warning; resuming it is the user's action.
func (d doctor) sessions() {
	o := d.o
	o.Head("sessions")
	for _, l := range release.Lines(session.Human(session.Sessions(session.Sources{}))) {
		switch {
		case strings.HasPrefix(l, "  stale  "):
			o.Warn(l[2:])
		case strings.HasPrefix(l, "note:"):
			o.Note(strings.TrimPrefix(l, "note: "))
		case strings.HasPrefix(l, "no stale"):
			o.OK(l)
		case strings.Contains(l, " session(s) started before"):
			o.Warn(l)
		case strings.HasPrefix(l, "installed"), strings.HasPrefix(l, "source:"):
			o.Note(l)
		}
	}
}

// gitQuiet is `$(git -C <dir> … 2>/dev/null)`.
func gitQuiet(dir string, args ...string) string {
	s, _ := gitQuietOK(dir, args...)
	return s
}

func gitQuietOK(dir string, args ...string) (string, bool) {
	b, err := readGit(dir, args...).Output()
	return release.Substituted(b), err == nil
}

// readGit is git in dir with args, a read, with optional locks off.
func readGit(dir string, args ...string) *exec.Cmd {
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Env = project.GitReadEnv()
	return c
}

// countNamed is `find <dir> [-maxdepth <depth>] -name <pattern> | wc -l`:
// the entries under dir, links not followed, whose name matches; depth < 0
// is unbounded.
func countNamed(dir, pattern string, depth int) int {
	n := 0
	filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ok, _ := filepath.Match(pattern, e.Name()); ok {
			n++
		}
		if e.IsDir() && depth >= 0 && p != dir && strings.Count(strings.TrimPrefix(p, dir), "/") >= depth {
			return fs.SkipDir
		}
		return nil
	})
	return n
}

// globNames is the names `<dir>/*` expands to, in order: the entries not
// starting with a dot.
func globNames(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	return names
}
