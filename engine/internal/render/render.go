package render

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/manifest"
	"github.com/jachavarro2004/jhon-agent/engine/internal/profiles"
	"github.com/jachavarro2004/jhon-agent/engine/internal/session"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
)

// Usage is the renderer's usage text.
const Usage = `usage: jhon render-engine --validate | --check | --print <agent> | --install --source <dir> [--only claude-code|pi] | --preflight --source <dir>

  --install           from a release under ~/.local/share/jhon/releases/<commit>
                      only, as ` + "`jhon sync`" + ` runs it: validate, then write every
                      runtime projection into $HOME through ~/.local/share/jhon/current
                      and record the install in ~/.cache/jhon/install.json
  --preflight         from a release, as ` + "`jhon sync`" + ` runs it before switching
                      current: validate the release and the install it would
                      make (settings.json, the links it would keep); writes nothing
  --source <dir>      the framework's source root, which framework-guard protects
  --validate          validate the repository only; writes nothing
  --check             validate, then report drift; writes nothing
  --only <runtime>    restrict the render to claude-code or pi
  --print <agent>     preview one agent's rendered frontmatter; writes nothing
  -h, --help          print this usage and exit
`

// args are the renderer's flags.
type args struct {
	help, check, validate, install, preflight bool
	source, only, print                       *string
}

// parseArgs reads the closed set of arguments the renderer accepts. Anything
// else is an error: an ignored argument meant `--help` or a mistyped flag
// fell through to a full write render into $HOME.
func parseArgs(argv []string) (args, string) {
	var out args
	seen := map[string]bool{}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "-h" || a == "--help" {
			out.help = true
			continue
		}
		if seen[a] {
			return out, a + " given more than once"
		}
		seen[a] = true
		switch {
		case a == "--check":
			out.check = true
		case a == "--validate":
			out.validate = true
		case a == "--install":
			out.install = true
		case a == "--preflight":
			out.preflight = true
		case a == "--only" || a == "--print" || a == "--source":
			i++
			if i >= len(argv) || strings.HasPrefix(argv[i], "-") {
				return out, a + " requires a value"
			}
			v := argv[i]
			switch a {
			case "--only":
				if v != "claude-code" && v != "pi" {
					return out, fmt.Sprintf("--only must be claude-code or pi, not '%s'", v)
				}
				out.only = &v
			case "--source":
				out.source = &v
			default:
				out.print = &v
			}
		case strings.HasPrefix(a, "-"):
			return out, "unknown option: " + a
		default:
			return out, "unexpected argument: " + a
		}
	}
	// Docket AV1: only sync installs. A render with nothing to do but write
	// would install the checkout it runs from.
	if !out.help && !out.check && !out.validate && !out.install && !out.preflight && out.print == nil {
		return out, "jhon render-engine installs only with --install, from a release, as `jhon sync` runs it"
	}
	if out.install && (out.check || out.validate || out.print != nil || out.preflight) {
		return out, "--install takes no --check, --validate, --print, or --preflight"
	}
	if out.preflight && (out.check || out.validate || out.print != nil || out.only != nil) {
		return out, "--preflight takes no --check, --validate, --print, or --only"
	}
	if (out.install || out.preflight) != (out.source != nil) {
		return out, "--install or --preflight and --source <dir> go together"
	}
	return out, ""
}

// alwaysOnGroups is the always-on layer, in load order: each key is the
// template placeholder (templates/claude-md-block.md) under that section's
// heading, its files the group's. Claude Code receives the layer inline in
// each Jhon agent's definition (alwaysOnSection); a runtime without imports
// receives the files concatenated (alwaysOnBundle). orchestrator.md is not
// here: it binds the top-level thread only, as that agent's body.
var alwaysOnGroups = []struct {
	name  string
	files []string
}{
	{"identity", []string{"persona/jhon.md"}},
	{"contracts", []string{
		"contracts/language.md",
		"contracts/shell-tools.md",
		"contracts/remote-auth.md",
		"contracts/runtime-boundaries.md",
		"contracts/codegraph.md",
		"contracts/engram.md",
		"contracts/tdd.md",
		"contracts/archify-activation.md",
	}},
	{"routing", []string{"contracts/routing.md"}},
}

// alwaysOn is the layer's files, flattened in load order.
func alwaysOn() []string {
	var out []string
	for _, g := range alwaysOnGroups {
		out = append(out, g.files...)
	}
	return out
}

// engine is one renderer run: the tree it runs from, $HOME, the flags,
// the manifest, the owner's active profile, and the runtimes the owner
// turned off.
type engine struct {
	// root is the release or checkout; prose is its prose/ directory, the
	// root of every file the agents read and of the prose's own references.
	root, prose, home string
	args              args
	out, errw         io.Writer
	m                 manifest.Manifest
	profiles          profiles.File
	profErrs          []string
	active            string
	profile           any
	vocab             *vocabulary
	// off are the runtimes settings.runtimes turns off, each with its key.
	off map[string]string
	// absent are the runtimes off by detection (no setting, nothing found:
	// settings.Detected), each with the directory not created.
	absent map[string]bool
	// chosen are the runtimes a setting turns on: created where absent.
	chosen map[string]bool
	// optionalOn is whether each optional integration's setting is on, by
	// name (context7.go).
	optionalOn map[string]bool
}

// ReadProfiles is the owner's profiles file with its active profile settled
// in one place (T56b): settings' pi.profile when the owner set it (in
// settings.toml, or the environment), else the file's own `active`. Unreadable
// settings leave the file's choice.
func ReadProfiles() (profiles.File, []string) {
	f, errs := profiles.Read(profiles.Path())
	if name, ok := settingsProfile(); ok {
		f.Active, f.HasActive = name, true
	}
	return f, errs
}

// settingsProfile is settings' pi.profile when the owner set it.
func settingsProfile() (string, bool) {
	vals, err := settings.Load(settings.Path(), os.LookupEnv)
	if err != nil {
		return "", false
	}
	v, ok := settings.Find(vals, "pi.profile")
	if !ok || v.Source == settings.SourceDefault {
		return "", false
	}
	name, ok := v.Value.(string)
	return name, ok
}

// runtimeHome is each runtime's directory under the home, as
// settings.Detected looks for it.
var runtimeHome = map[string]string{"claude-code": ".claude", "pi": ".pi"}

// newEngine reads what every run needs first: the manifest and the
// owner's profiles.
func newEngine(root string, a args, out, errw io.Writer) (*engine, error) {
	m, err := manifest.Load(root)
	if err != nil {
		return nil, err
	}
	e := &engine{root: root, prose: filepath.Join(root, ProseDir), home: profiles.HomeDir(), args: a, out: out, errw: errw, m: m}
	e.profiles, e.profErrs = ReadProfiles()
	e.active, e.profile = profiles.Active(e.profiles)
	e.off, e.absent, e.chosen = map[string]string{}, map[string]bool{}, map[string]bool{}
	e.m.Agents, e.optionalOn = withoutOff(e.m.Agents, settings.On)
	for _, r := range m.Matrix.Runtimes {
		on, key, source := settings.RuntimeSetting(r)
		if !on {
			e.off[r] = key
			e.absent[r] = source == settings.SourceDefault
		} else if source != settings.SourceDefault {
			e.chosen[r] = true
		}
	}
	return e, nil
}

// Run is `jhon render-engine <argv…>`: its output on stdout and stderr,
// and its exit code. Where the render panics (an unreadable body, a manifest
// without the main agent), Run prints the message and exits 1.
func Run(root string, argv []string, stdout, stderr io.Writer) (code int) {
	defer recoverExit(stderr, &code)
	a, perr := parseArgs(argv)
	e, err := newEngine(resolveRoot(root), a, stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if perr != "" {
		fmt.Fprintf(stderr, "jhon render-engine: %s\n\n%s\n", perr, Usage)
		return 2
	}
	return e.main()
}

// recoverExit turns a panic, a render that cannot go on, into its message
// and exit 1.
func recoverExit(stderr io.Writer, code *int) {
	if r := recover(); r != nil {
		fmt.Fprintln(stderr, r)
		*code = 1
	}
}

// ── paths ──

func (e *engine) path(rel ...string) string {
	return filepath.Join(append([]string{e.home}, rel...)...)
}

func (e *engine) share() string    { return e.path(".local/share/jhon") }
func (e *engine) releases() string { return filepath.Join(e.share(), "releases") }
func (e *engine) current() string  { return filepath.Join(e.share(), "current") }

// bin is the active release's binary: every rendered hook runs it as
// `jhon hook <name>` (docket AV1: the runtime runs the active release).
func (e *engine) bin() string { return filepath.Join(e.current(), "build", "jhon") }

// hookCommand is the command line that runs hook name with args.
func (e *engine) hookCommand(name string, args ...string) string {
	return command(append([]string{e.bin(), "hook", name}, args...)...)
}

func (e *engine) cliLink() string      { return e.path(".local/bin/jhon") }
func (e *engine) claudeAgents() string { return e.path(".claude/agents") }
func (e *engine) agentsRecord() string { return filepath.Join(e.claudeAgents(), ".jhon-owned") }
func (e *engine) claudeMd() string     { return e.path(".claude/CLAUDE.md") }
func (e *engine) settings() string     { return e.path(".claude/settings.json") }
func (e *engine) skillsRoot() string   { return e.path(".agents/skills") }
func (e *engine) ownedRecord() string  { return filepath.Join(e.skillsRoot(), ".jhon-owned") }
func (e *engine) pluginSkills() string { return filepath.Join(e.root, PluginDir, "skills") }

// The tree's layout, relative to its root.
const (
	ProseDir  = "prose"
	PluginDir = "runtimes/claude-code/plugin"
	ShellDir  = "shell"
)

// runtimeRef is `current` as the runtime prose names it.
const runtimeRef = "~/.local/share/jhon/current"

// sourceRoot is the framework's maintenance source root, which
// framework-guard protects (docket AV2): --source when sync installs from
// a release, else this tree.
func (e *engine) sourceRoot() string {
	if e.args.source == nil {
		return e.root
	}
	abs, _ := filepath.Abs(*e.args.source)
	return abs
}

// tilde is p with its first occurrence of $HOME written `~`.
func (e *engine) tilde(p string) string { return strings.Replace(p, e.home, "~", 1) }

func (e *engine) println(s string)  { fmt.Fprintln(e.out, js.WellFormed(s)) }
func (e *engine) eprintln(s string) { fmt.Fprintln(e.errw, js.WellFormed(s)) }

var commitName = lazyre.New(`^[0-9a-f]{40}$`)

// releaseCommit is the commit of the release this render runs from, or ""
// when it does not run from one (docket AV1): a directory named by a full
// commit id directly under ~/.local/share/jhon/releases.
func (e *engine) releaseCommit() string {
	name := filepath.Base(e.root)
	if !commitName.MatchString(name) {
		return ""
	}
	parent, err1 := filepath.EvalSymlinks(filepath.Dir(e.root))
	releases, err2 := filepath.EvalSymlinks(e.releases())
	if err1 != nil || err2 != nil || parent != releases {
		return ""
	}
	return name
}

// inRelease is whether this render judges a release (releaseCommit) rather
// than the source checkout: a release ships without what only development
// reads (T10f), so the repository's own evidence (the README's map, the
// audit fixtures, the matrix's evidence) is the checkout's to check, in
// `jhon validate`, `jhon test`, the pre-commit hook, sync and pack.
func (e *engine) inRelease() bool { return e.releaseCommit() != "" }

// profileIssues are the profile's problems against this manifest; a
// projection refuses to run on them.
func (e *engine) profileIssues() []string {
	ctx := profiles.Context{Runtimes: e.m.Matrix.Runtimes, Tiers: e.m.TierNames(), Agents: e.m.AgentNames()}
	return append(slices.Clone(e.profErrs), profiles.Problems(e.profiles, ctx)...)
}

// modelOf is an agent's model value in runtime under the active profile,
// and whether the profile names the agent itself.
func (e *engine) modelOf(runtime string, a manifest.Agent) (value string, own, ok bool) {
	m, ok := profiles.ModelFor(e.m.TierTable(), e.profile, runtime, profiles.Agent{Name: a.Name, Tier: a.Tier})
	if !ok {
		return "", false, false
	}
	return js.String(m.Value), strings.HasPrefix(m.Source, "profile agent"), true
}

// ── main ──

func (e *engine) main() int {
	a := e.args
	if a.help {
		fmt.Fprint(e.out, Usage)
		return 0
	}
	if (a.install || a.preflight) && e.releaseCommit() == "" {
		e.eprintln(fmt.Sprintf("jhon render-engine: %s is not a release under %s/<commit>; `jhon sync` builds one from HEAD and installs from it", e.root, e.tilde(e.releases())))
		return 2
	}

	problems := append(e.validate(), e.validateRuntime()...)
	if len(problems) > 0 {
		e.eprintln(fmt.Sprintf("validation: %d problem(s)\n", len(problems)))
		for _, p := range problems {
			line := ""
			if p.line != 0 {
				line = fmt.Sprintf(":%d", p.line)
			}
			e.eprintln(fmt.Sprintf("  %s %s%s  %s", js.PadEnd(p.rule, 16), p.file, line, p.msg))
		}
		if a.validate {
			e.eprintln("\nvalidation failed")
		} else {
			e.eprintln("\nrender aborted; nothing written")
		}
		return 1
	}
	if a.validate {
		e.println(fmt.Sprintf("validation: ok — %d agents, %d always-on files", len(e.m.Agents), len(alwaysOn())))
		return 0
	}

	// The owner's profile is machine state, not the repository: every
	// projection reads it, so a broken one stops them all before anything
	// is written, naming the file to fix.
	ctx := profiles.Context{Runtimes: e.m.Matrix.Runtimes, Tiers: e.m.TierNames(), Agents: e.m.AgentNames()}
	for _, w := range profiles.Warnings(e.profiles, ctx) {
		e.eprintln("profile: warning: " + w + " (" + e.tilde(profiles.Path()) + ")")
	}
	if errs := e.profileIssues(); len(errs) > 0 {
		// A name settings chose is blamed there, not on profiles.json (T56c).
		from := ""
		if name, ok := settingsProfile(); ok && name == e.active {
			from = ", from pi.profile in " + e.tilde(settings.Path())
		}
		e.eprintln(fmt.Sprintf("profile: %d problem(s) in %s (active: %s%s)\n", len(errs), e.tilde(profiles.Path()), e.active, from))
		for _, p := range errs {
			e.eprintln("  " + p)
		}
		e.eprintln("\nfix the file or run `jhon profile use default`; nothing written")
		return 1
	}

	// The settings preflight runs before any projection is written, and
	// sync runs it (--preflight) before it switches `current`.
	if (a.install || a.preflight) && e.on("claude-code") {
		if problem := e.settingsProblem(readText(e.settings())); problem != "" {
			e.eprintln(e.tilde(e.settings()) + " " + problem)
			if a.preflight {
				e.eprintln("preflight failed; current was not switched")
			} else {
				e.eprintln("install aborted; nothing written")
			}
			return 1
		}
	}
	if a.preflight {
		stale := ""
		if n := len(e.obsoleteCommands()); n > 0 && e.on("claude-code") {
			stale = fmt.Sprintf(", %d obsolete command link(s) to prune", n)
		}
		e.println(fmt.Sprintf("preflight: ok — %d agents, %d always-on files%s", len(e.m.Agents), len(alwaysOn()), stale))
		return 0
	}

	if a.print != nil {
		return e.preview(*a.print)
	}
	return e.write()
}

// preview prints one agent's whole rendered Claude Code frontmatter.
func (e *engine) preview(want string) int {
	if _, ok := e.m.Find(want); !ok {
		e.eprintln("unknown agent: " + want)
		return 1
	}
	e.println("── claude-code ──")
	for _, f := range (claude{e}).Files() {
		if strings.HasSuffix(f.Path, "/"+want+".md") {
			// md.slice(0, md.indexOf("\n---\n") + 4): the frontmatter is
			// ASCII up to there, so bytes count as UTF-16 units.
			md := f.Content
			e.println(md[:min(strings.Index(md, "\n---\n")+4, len(md))])
			break
		}
	}
	return 0
}

// run is the state of one write or check run across the runtimes.
type run struct {
	*engine
	drift     int
	installed bool
	failed    bool
}

// report prints one target's state and counts it as drift when changed.
func (s *run) report(path string, changed bool) {
	state := "ok   "
	if changed {
		s.drift++
		state = "write"
		if s.args.check {
			state = "DRIFT"
		}
	}
	s.println(fmt.Sprintf("  %s  %s", state, s.tilde(path)))
}

// verb is the word a change is printed with: DRIFT under --check, else w.
func (s *run) verb(w string) string {
	if s.args.check {
		return "DRIFT"
	}
	return w
}

// writeFiles writes (or, under --check, compares) each file.
func (s *run) writeFiles(files []File) {
	for _, f := range files {
		prev := readText(f.Path)
		changed := prev != f.Content
		if changed && f.Path == s.piSettings() && samePiSettings(prev, f.Content) {
			changed = false
		}
		if changed && !s.args.check {
			writeText(f.Path, f.Content)
		}
		s.report(f.Path, changed)
	}
}

// write is the render proper: each runtime, the obsolete commands and
// skills, the CLI link, and the install record.
func (e *engine) write() int {
	s := &run{engine: e}
	only := ""
	if e.args.only != nil {
		only = *e.args.only
	}
	for _, r := range e.renderers() {
		if only != "" && only != r.Runtime() {
			continue
		}
		if key, off := e.off[r.Runtime()]; off {
			if e.absent[r.Runtime()] {
				// Nothing of the runtime was found, and no setting chose it.
				e.println(r.Runtime() + ":")
				e.println(fmt.Sprintf("  skip   %s is absent; not created", e.tilde(e.path(runtimeHome[r.Runtime()]))))
			} else {
				e.println(fmt.Sprintf("skip: %s (%s = false); %s is left as is", r.Runtime(), key, e.tilde(r.Dir())))
			}
			continue
		}
		if r.Install(s) {
			s.installed = true
		}
	}
	if only == "" {
		s.commandsAndSkills()
	}

	// The install record (docket AI1): when a write render installed
	// anything, when and from which commit, for `jhon sessions` and the
	// guards' stale-session note. The time moves only when something new
	// was installed. The source checkout `jhon sync` recorded is sync's to
	// write; a render keeps it.
	if !e.args.check && s.installed {
		record := filepath.Join(e.home, session.InstallRecord)
		commit := e.releaseCommit()
		prev, ok := session.ReadInstall(e.home)
		moved := s.drift > 0 || !ok || prev.Commit != commit
		if moved {
			rec := session.Install{InstalledAt: isodate.Format(time.Now()), Commit: commit, Source: prev.Source}
			if err := session.WriteInstall(e.home, rec); err != nil {
				panic(js.FSError(err).Error())
			}
		}
		e.println("install:")
		state := "ok   "
		if moved {
			state = "write"
		}
		e.println(fmt.Sprintf("  %s  %s", state, e.tilde(record)))
	}

	if e.args.check {
		if s.drift > 0 {
			e.println(fmt.Sprintf("\n%d target(s) out of date", s.drift))
		} else {
			e.println("\nup to date")
		}
		if s.drift > 0 || s.failed {
			return 1
		}
		return 0
	}
	if s.failed {
		return 1
	}
	return 0
}

// commandsAndSkills prunes what an earlier release projected outside the
// runtimes' own directories, and links the CLI.
func (s *run) commandsAndSkills() {
	s.println("commands:")
	// ~/.claude/commands is Claude Code's: with it off, it is not read.
	var pruned []string
	if s.on("claude-code") {
		pruned = s.obsoleteCommands()
	}
	if !s.args.check {
		for _, link := range pruned {
			os.Remove(link)
		}
	}
	switch {
	case !s.on("claude-code"):
		s.println(fmt.Sprintf("  ok     %d command(s) in the release's plugin", len(s.releaseCommands())))
	case len(pruned) == 0:
		s.println(fmt.Sprintf("  ok     %d command(s) in the release's plugin; none in ~/.claude/commands", len(s.releaseCommands())))
	}
	for _, link := range pruned {
		s.println(fmt.Sprintf("  %s  %s (the release's plugin carries it now)", s.verb("prune"), s.tilde(link)))
	}
	s.drift += len(pruned)

	s.println("skills:")
	removed, recordChanged := s.pruneSkills()
	if len(removed) == 0 && !recordChanged {
		s.println(fmt.Sprintf("  ok     %d skill(s) read from the release; none in %s", len(s.ownedSkills()), s.tilde(s.skillsRoot())))
	}
	for _, name := range removed {
		s.println(fmt.Sprintf("  %s  %s (read from the release now)", s.verb("prune"), s.tilde(filepath.Join(s.skillsRoot(), name))))
	}
	s.drift += len(removed)
	if recordChanged {
		s.report(s.ownedRecord(), true)
	}

	cli := s.linkCli()
	s.println("cli:")
	state := "ok   "
	if cli {
		state = s.verb("link ")
		s.drift++
	}
	s.println(fmt.Sprintf("  %s  ~/.local/bin/jhon", state))
}
