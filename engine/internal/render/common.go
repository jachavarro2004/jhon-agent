package render

import (
	"errors"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/manifest"
	"github.com/jachavarro2004/jhon-agent/engine/internal/profiles"
)

// File is one projected file: its path and its whole content.
type File struct {
	Path, Content string
}

// Renderer projects the manifest onto one runtime.
type Renderer interface {
	// Runtime is the runtime's name, as --only takes it.
	Runtime() string
	// Dir is the runtime's own directory, which a runtime turned off in
	// settings.runtimes leaves as it is.
	Dir() string
	// Files are the projection, in write order.
	Files() []File
	// Install writes the projection (or, under --check, reports its
	// drift) and prunes what an earlier one left; false when the runtime
	// is absent and nothing was projected.
	Install(s *run) bool
}

// renderers are the runtimes Jhon projects, in render order.
func (e *engine) renderers() []Renderer { return []Renderer{claude{e}, pi{e}} }

// on is whether runtime is on in settings.runtimes (S37, T56): a runtime
// that is off is neither projected nor compared.
func (e *engine) on(runtime string) bool {
	_, off := e.off[runtime]
	return !off
}

// ── files, as node's fs reads and writes them ──

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// lexists is whether p exists, a dangling link included.
func lexists(p string) bool { _, err := os.Lstat(p); return err == nil }

func isLink(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode()&fs.ModeSymlink != 0
}

// readText is the file's text, "" when it is absent.
func readText(p string) string {
	if !exists(p) {
		return ""
	}
	return mustReadText(p)
}

// mustReadText is readFileSync(p, "utf8"), or a panic with node's message.
func mustReadText(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		panic(js.FSError(err).Error())
	}
	return js.DecodeUTF8(b)
}

func writeText(p, s string) {
	if err := os.WriteFile(p, []byte(js.WellFormed(s)), 0o666); err != nil {
		panic(js.FSError(err).Error())
	}
}

func mkdirAll(p string) {
	if err := os.MkdirAll(p, 0o777); err != nil {
		panic(js.FSError(err).Error())
	}
}

// readDir is the directory's entries, sorted by name; none when it cannot be read.
func readDir(p string) []os.DirEntry {
	entries, _ := os.ReadDir(p)
	return entries
}

// sortJS sorts as Array.prototype.sort does without a comparator.
func sortJS(xs []string) { slices.SortFunc(xs, js.Compare) }

// namePattern is a plain agent or skill name: a record line that is not
// one (a path, `..`) is ignored rather than trusted to a removal.
var namePattern = lazyre.New(`^[a-z0-9][a-z0-9-]*$`)

// ── bodies ──

// bodyPath is an agent's prompt file: explicit `body:`, else
// agents/<name>.md. A templated agent's body is expanded from its template
// (generatedBody), and validation reads that expansion at this path.
func (e *engine) bodyPath(a manifest.Agent) string {
	if a.Body != "" {
		return filepath.Join(e.prose, a.Body)
	}
	return filepath.Join(e.prose, "agents", a.Name+".md")
}

func (e *engine) templatePath(a manifest.Agent) string {
	return filepath.Join(e.prose, "agents", "templates", a.Template+".md")
}

var placeholder = lazyre.New(`\{\{([\w-]+)\}\}`)

// expandTemplate substitutes `{{name}}` placeholders. A placeholder with no
// value is left in place and reported (unknown), and so is a value no
// placeholder uses (unused): either means the manifest and the template
// disagree about what the body says.
func expandTemplate(src string, vars *manifest.Map[string]) (text string, unknown, unused []string) {
	used := map[string]bool{}
	text = placeholder.ReplaceAllStringFunc(src, func(whole string) string {
		name := whole[2 : len(whole)-2]
		v, ok := vars.Get(name)
		if !ok {
			if !slices.Contains(unknown, name) {
				unknown = append(unknown, name)
			}
			return whole
		}
		used[name] = true
		return v
	})
	for _, k := range vars.Keys() {
		if !used[k] {
			unused = append(unused, k)
		}
	}
	return text, unknown, unused
}

// vars is an agent's template values, {} when it has none.
func vars(a manifest.Agent) *manifest.Map[string] {
	if a.Vars == nil {
		return &manifest.Map[string]{}
	}
	return a.Vars
}

// generatedBody is a templated agent's body, as the file it replaces held
// it; false when the agent is not templated.
func (e *engine) generatedBody(a manifest.Agent) (string, bool) {
	if a.Template == "" {
		return "", false
	}
	p := e.templatePath(a)
	if !exists(p) {
		return "", true
	}
	text, _, _ := expandTemplate(mustReadText(p), vars(a))
	return text, true
}

func (e *engine) bodyOf(a manifest.Agent) string {
	body, ok := e.generatedBody(a)
	if !ok {
		body = mustReadText(e.bodyPath(a))
	}
	return js.Trim(body)
}

// runtimePlaceholder is how a body names what each runtime says its own
// way, its tools above all: {{runtime:<term>}}, which each runtime's render
// writes from runtimeTerms.
var runtimePlaceholder = lazyre.New(`\{\{runtime:([\w-]+)\}\}`)

// runtimeTerms are, per term, what each runtime's render writes for it.
// Validation refuses a body term missing here or missing a runtime.
var runtimeTerms = map[string]map[string]string{
	"continue-agent": {
		"claude-code": "continue it with `SendMessage` to its agent id",
		"pi":          "continue it with `subagent_continue` and the id `subagent` returned",
	},
	// T118d: the native 4R review is one call in each runtime. In Claude
	// Code it runs in the background (L1159, F3): its lens calls took 32-231 s
	// in a real review, past the Bash tool's 2-minute default, which would
	// kill it without its cleanup or its one line; a background call has no
	// such bound (each claude call keeps its own) and its notification wakes
	// the orchestrator.
	"native-review": {
		"claude-code": "`jhon review run` as a Bash call with `run_in_background: true`, which the Bash tool's 2-minute default never cuts short; act on its notification, reading its output",
		"pi":          "`jhon_review`",
	},
	"own-worktree": {
		"claude-code": "is launched with `isolation: \"worktree\"`, a git worktree of its own",
		"pi":          "is launched with `worktree: true`, a git worktree of its own beside the repository, on a new branch from HEAD",
	},
	// T90 (L649): every sub-agent runs in the background. Claude Code offers
	// run_in_background only where it does not already force it (headless
	// runs); an interactive session runs every Agent call in the background.
	// Headless `claude -p` keeps the process alive for background agents and
	// wakes the main agent with each completion as a new turn (probed on
	// Claude Code 2.1.323: task_notification, then a second `result`), so
	// unlike `pi -p` before f91ce66d nothing is lost and no wait is needed.
	"background-agents": {
		"claude-code": "launch every `Agent` call with `run_in_background: true` where the tool offers it (an interactive session already runs them there)",
		"pi":          "`subagent` returns a task id at once and shows the agent on the agents rail",
	},
	// T146 (L1211): the tools a background job runs with, each runtime's own.
	"background-jobs": {
		"claude-code": "Bash with `run_in_background: true` or `Monitor`",
		"pi":          "`bash_background` or `monitor`",
	},
	"owner-grant": {
		"claude-code": "by running it with `!`",
		"pi":          "from `/jhon:grants` in the shell",
	},
	"track-slice": {
		// T134 (L1019): the agent's own next steps, never a copy of the change's tasks.
		"claude-code": "keep in `TaskCreate` tasks your own next steps for this request, each moved with `TaskUpdate` (`in_progress` before you start it, `completed` as soon as it is), never a copy of the change's tasks, which stay in `jhon status` and tasks.md",
		"pi":          "keep in `todo` your own next steps for this request, each moved with `update` (`in_progress` before you start it, `done` as soon as it is), never a copy of the change's tasks, which stay in `jhon status` and tasks.md",
	},
	"grant-how": {
		"claude-code": "run by the owner with `!`",
		"pi":          "given by the owner from `/jhon:grants` in the shell",
	},
	"surfaces-refusal": {
		"claude-code": "by jhon hook route-guard",
		"pi":          "by the `subagent` tool",
	},
	"fresh-session": {
		"claude-code": "`/clear`",
		"pi":          "`/new`",
	},
	// T65 (L990): the persona names the ask tool a question goes through.
	"ask-tool": {
		"claude-code": "`AskUserQuestion`",
		"pi":          "`ask_user`",
	},
	// pi has no plan mode (T50d): the sentence is Claude Code's alone.
	"plan-mode": {
		"claude-code": "In plan mode, write the plan to the file the runtime names under\n~/.claude/plans/ and end with `ExitPlanMode`.",
		"pi":          "",
	},
}

// T146 (L1211, FA1): an MCP tool the prose names is named as the runtime
// calls it. pi registers each server tool as mcp__<server>__<tool>; Claude
// Code mounts Engram under two server names and resolves the bare name.
func init() {
	for server, tools := range map[string][]string{
		integrations.Engram:    {"mem_search", "mem_get_observation", "mem_context", "mem_current_project", "mem_review", "mem_merge_projects", "mem_session_summary"},
		integrations.CodeGraph: {"codegraph_explore"},
	} {
		for _, t := range tools {
			runtimeTerms[strings.ReplaceAll(t, "_", "-")] = map[string]string{
				"claude-code": "`" + t + "`",
				"pi":          "`mcp__" + server + "__" + t + "`",
			}
		}
	}
}

// bodyFor is an agent's body as runtime renders it: each runtime term in
// its words. A term with no words for it stays as written, which
// validation reports. An agent that does not hold an optional integration's
// capability (context7, or one that is off: context7.go) reads no sentence
// about it.
func (e *engine) bodyFor(runtime string, a manifest.Agent) string {
	return inRuntime(runtime, withoutUnheld(e.bodyOf(a), a.Caps))
}

// inRuntime is text with each runtime term in runtime's words, so a runtime
// reads only its own rules (L264).
func inRuntime(runtime, text string) string {
	return runtimePlaceholder.ReplaceAllStringFunc(text, func(whole string) string {
		if words, ok := runtimeTerms[runtimePlaceholder.FindStringSubmatch(whole)[1]][runtime]; ok {
			return words
		}
		return whole
	})
}

// ── the always-on layer ──

// subagentIdentity is what a sub-agent receives in place of the persona:
// persona/jhon.md governs reply text to the owner, and a sub-agent answers
// the agent that launched it.
const subagentIdentity = "You report to the agent that launched you, not to the owner: state findings and results plainly, in the language of the task, with evidence. The persona binds only the main agent's replies."

func (e *engine) claudeMdTemplate() string {
	return filepath.Join(e.prose, "templates/claude-md-block.md")
}

// alwaysOnSection is the always-on layer as a Jhon agent of runtime
// receives it, after its own body: the template's fixed prose with each
// group's placeholder replaced by that group's files, whole and in load
// order, each runtime term in runtime's words.
func (e *engine) alwaysOnSection(runtime string, primary bool) string {
	var v manifest.Map[string]
	for _, g := range alwaysOnGroups {
		if g.name == "identity" && !primary {
			v.Set(g.name, subagentIdentity)
			continue
		}
		texts := make([]string, len(g.files))
		for i, f := range g.files {
			texts[i] = js.Trim(mustReadText(filepath.Join(e.prose, f)))
		}
		v.Set(g.name, strings.Join(texts, "\n\n"))
	}
	text, _, _ := expandTemplate(readText(e.claudeMdTemplate()), &v)
	return inRuntime(runtime, js.Trim(text))
}

// runtimeGuarantees is the runtime guarantees of runtime (docket AG4,
// amended by docket AR), derived from agents.yaml `guarantees`.
func (e *engine) runtimeGuarantees(runtime string) string {
	return manifest.Declaration(e.m.Matrix, runtime, e.vocabulary().guaranteeLevels)
}

// alwaysOnBundle is the layer as a runtime without imports receives it:
// every file concatenated, then that runtime's guarantees.
func (e *engine) alwaysOnBundle(runtime string) string {
	var parts []string
	for _, rel := range alwaysOn() {
		parts = append(parts, inRuntime(runtime, js.Trim(mustReadText(filepath.Join(e.prose, rel)))))
	}
	return strings.Join(append(parts, e.runtimeGuarantees(runtime)), "\n\n---\n\n")
}

// ── what the guards are given ──

// vcsDirs are the VCS directories no agent may write (contracts/
// vocabulary.yaml `vcs_dirs`). An empty list renders a guard that refuses
// every write, never one that allows them all.
func (e *engine) vcsDirs() []string {
	v := filepath.Join(e.prose, "contracts", "vocabulary.yaml")
	if !exists(v) {
		return []string{}
	}
	if l := contract.List(mustReadText(v), "vcs_dirs"); l != nil {
		return l
	}
	return []string{}
}

// frameworkProtected are the paths the framework guard protects, in a
// fixed order: the source root, the releases, `current`, the CLI link,
// CLAUDE.md, and every projection this render owns.
func (e *engine) frameworkProtected() []string {
	names := e.m.AgentNames()
	sortJS(names)
	out := []string{e.sourceRoot(), e.releases(), e.current(), e.cliLink(), e.claudeMd(), profiles.Path(), e.agentsRecord()}
	for _, n := range names {
		out = append(out, filepath.Join(e.claudeAgents(), n+".md"))
	}
	return append(out, e.piSettings(), e.piConfig(), e.piMcpFile(), e.piPromptFile(), e.piWriterFile(), e.piAgents(), e.piPrompts())
}

// ── commands, skills, and the CLI ──

// dirNames are the names of dir's subdirectories, sorted.
func dirNames(dir string) []string {
	out := []string{}
	for _, d := range readDir(dir) {
		if d.IsDir() {
			out = append(out, d.Name())
		}
	}
	sortJS(out)
	return out
}

// releaseCommands are the command names this release's plugin ships, as
// `/<name>` (plugin/skills/<name>/SKILL.md).
func (e *engine) releaseCommands() []string { return dirNames(e.pluginSkills()) }

// ownedSkills are the skills this framework owns: every directory under skills/.
func (e *engine) ownedSkills() []string { return dirNames(filepath.Join(e.prose, "skills")) }

// walk is every file under dir, as paths relative to it, sorted.
func walk(dir string) []string {
	var out []string
	var visit func(d string)
	visit = func(d string) {
		for _, entry := range readDir(d) {
			p := filepath.Join(d, entry.Name())
			if entry.IsDir() {
				visit(p)
			} else {
				out = append(out, p[len(dir)+1:])
			}
		}
	}
	if exists(dir) {
		visit(dir)
	}
	sortJS(out)
	return out
}

// resolveLink is where link points, as path.resolve(dirname(link), target).
func resolveLink(link string) string {
	t, _ := os.Readlink(link)
	if filepath.IsAbs(t) {
		return filepath.Clean(t)
	}
	return filepath.Join(filepath.Dir(link), t)
}

// ownedCommandLink is whether link in ~/.claude/commands is a Jhon command
// link: a symlink into the `commands/` of `current`, of a release, or of
// the source. A regular file or a link elsewhere is the user's.
func (e *engine) ownedCommandLink(link string) bool {
	if !isLink(link) {
		return false
	}
	dir := filepath.Dir(resolveLink(link))
	if dir == filepath.Join(e.current(), "commands") || dir == filepath.Join(e.sourceRoot(), "commands") {
		return true
	}
	return filepath.Dir(filepath.Dir(dir)) == e.releases() && filepath.Base(dir) == "commands"
}

// obsoleteCommands are the Jhon command links an earlier release left in
// ~/.claude/commands, which the install removes.
func (e *engine) obsoleteCommands() []string {
	dir := e.path(".claude/commands")
	var out []string
	for _, f := range readDir(dir) {
		if p := filepath.Join(dir, f.Name()); e.ownedCommandLink(p) {
			out = append(out, p)
		}
	}
	return out
}

// pruneSkills removes the skills a previous render projected into the
// global skills root, as its record names them, then the record; the
// stale names and whether the record changed. --check touches nothing.
func (e *engine) pruneSkills() (removed []string, recordChanged bool) {
	prev := readText(e.ownedRecord())
	for _, l := range strings.Split(prev, "\n") {
		if name := js.Trim(l); namePattern.MatchString(name) && lexists(filepath.Join(e.skillsRoot(), name)) {
			removed = append(removed, name)
		}
	}
	recordChanged = prev != ""
	if !e.args.check {
		for _, name := range removed {
			os.RemoveAll(filepath.Join(e.skillsRoot(), name))
		}
		if recordChanged {
			os.Remove(e.ownedRecord())
		}
	}
	return removed, recordChanged
}

// linkCli links ~/.local/bin/jhon to the release's binary through
// `current`, so the installed CLI runs the active release (docket AV1);
// whether the link was (or, under --check, would be) made.
func (e *engine) linkCli() bool {
	if !exists(filepath.Join(e.root, "build", "jhon")) {
		return false
	}
	src, link := e.bin(), e.cliLink()
	cur, isCurLink := "", isLink(link)
	if isCurLink {
		cur, _ = os.Readlink(link)
	}
	if isCurLink && cur == src {
		return false
	}
	if e.args.check {
		return true
	}
	mkdirAll(filepath.Dir(link))
	if exists(link) || isCurLink {
		if err := os.Remove(link); err != nil && !errors.Is(err, fs.ErrNotExist) {
			panic(js.FSError(err).Error())
		}
	}
	if err := os.Symlink(src, link); err != nil {
		panic(js.FSError(err).Error())
	}
	return true
}
