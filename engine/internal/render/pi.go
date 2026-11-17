package render

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/manifest"
	"github.com/jachavarro2004/jhon-agent/engine/internal/profiles"
)

// pi runs Jhon on its own agent directory, ~/.pi/jhon, which `jhon start
// pi` selects with PI_CODING_AGENT_DIR; the owner's ~/.pi/agent stays
// theirs. The directory holds settings.json (the pinned packages, the
// release's jhon-shell package (shell/) and skills, the orchestrator's
// model), jhon.json (what the extension reads: the guards'
// arguments), mcp.json (the MCP servers pi's built-in MCP support connects:
// Engram, codegraph, jhon and context7), jhon-orchestrator.md (the main prompt),
// agents/<name>.md for every agent but the primary ones, which
// jhon-shell's `subagent` tool runs as child pi processes,
// prompts/.jhon-owned (the record that prunes the prompt templates earlier
// releases wrote: pi's /jhon-ldd is jhon-shell's command now, L194),
// links to the owner's auth.json and keybindings.json, claude-bridge.json
// (the owner's, its Claude Code executable the provider wrapper below; both
// only when the active profile runs on claude-bridge or Claude Code is one of
// the install's runtimes, and then with AskClaude off for a profile that
// names no bridge model), and the theme `rose-ink` (render.Theme, wired by the cli), seeded under themes/, as settings.json's theme.
type pi struct{ *engine }

func (pi) Runtime() string { return "pi" }

func (p pi) Dir() string { return p.piHome() }

func (e *engine) piRoot() string          { return e.path(".pi") }
func (e *engine) piHome() string          { return filepath.Join(e.piRoot(), "jhon") }
func (e *engine) piAgents() string        { return filepath.Join(e.piHome(), "agents") }
func (e *engine) piSettings() string      { return filepath.Join(e.piHome(), "settings.json") }
func (e *engine) piConfig() string        { return filepath.Join(e.piHome(), "jhon.json") }
func (e *engine) piPromptFile() string    { return filepath.Join(e.piHome(), "jhon-orchestrator.md") }
func (e *engine) piMcpFile() string       { return filepath.Join(e.piHome(), "mcp.json") }
func (e *engine) piRecord() string        { return filepath.Join(e.piAgents(), ".jhon-owned") }
func (e *engine) piPrompts() string       { return filepath.Join(e.piHome(), "prompts") }
func (e *engine) piPromptsRecord() string { return filepath.Join(e.piPrompts(), ".jhon-owned") }

// piWriterFile is the writer-mode delegation rules (T117b, L720): the shell
// splices it over the main prompt's region between delegationBegin and
// delegationEnd when the active profile's delegation is writer, and keeps
// the organic rules when either the file or a marker is missing.
func (e *engine) piWriterFile() string {
	return filepath.Join(e.piHome(), "jhon-orchestrator-writer.md")
}

// The markers around the orchestrator's organic delegation rules, and the
// prose file writer mode puts in their place.
const (
	delegationBegin = "<!-- delegation:begin -->"
	delegationEnd   = "<!-- delegation:end -->"
	writerProse     = "orchestrator-writer.md"
)

// piOwner is the owner's own pi agent directory, whose credentials Jhon's links to.
func (e *engine) piOwner() string { return filepath.Join(e.piRoot(), "agent") }

var piLinkNames = []string{"auth.json", "keybindings.json"}

func (e *engine) piBridgeConfig() string { return filepath.Join(e.piHome(), "claude-bridge.json") }
func (e *engine) piProviderWrapper() string {
	return filepath.Join(e.piHome(), "claude-code-provider")
}

// piBridge is Jhon's claude-bridge.json: the owner's, every key kept, with
// its Claude Code executable the provider wrapper, which runs the owner's
// own executable (its pathToClaudeCodeExecutable, else claude).
func (e *engine) piBridge() (config *js.Object, real string) {
	real = "claude"
	owner := js.NewObject()
	if b, err := os.ReadFile(filepath.Join(e.piOwner(), "claude-bridge.json")); err == nil {
		if v, err := js.ParseJSON(js.DecodeUTF8(b)); err == nil {
			if o, ok := v.(*js.Object); ok {
				owner = o.Clone()
			}
		}
	}
	provider, ok := js.Prop(owner, "provider").(*js.Object)
	if ok {
		provider = provider.Clone()
		if p, ok := js.Prop(provider, "pathToClaudeCodeExecutable").(string); ok && p != "" && p != e.piProviderWrapper() {
			real = p
		}
	} else {
		provider = js.NewObject()
	}
	provider.Set("pathToClaudeCodeExecutable", e.piProviderWrapper())
	owner.Set("provider", provider)
	return owner, real
}

// piWrapper runs Claude Code for claude-bridge. As the model provider the
// bridge runs it with no tools (--tools=) and relaunches it every turn; there
// the owner's hooks (an Engram plugin's session, protocol and prompt capture
// each turn, rtk) only cost each turn about a second and
// tokens, so they are off (disableAllHooks). The rest of the owner's settings
// stays: a Bedrock or Vertex user's env and apiKeyHelper live there. With
// tools (AskClaude) it runs as asked, every hook and Jhon's guards kept.
func piWrapper(real string) string {
	return `#!/bin/sh
# Generated by jhon sync (engine/internal/render/pi.go); edits are overwritten.
# A shell script, not a node one: pi runs on Bun, and the SDK would run a
# script with a Bun no PATH has. The SDK passes its own --settings JSON: the
# hooks' switch goes inside it, as a second --settings would stall the run.
real=` + shq(real) + `
provider=no
prev=x
for a in "$@"; do
	if [ "$a" = "--tools=" ] || { [ "$prev" = "--tools" ] && [ -z "$a" ]; }; then provider=yes; fi
	prev=$a
done
[ "$provider" = yes ] || exec "$real" "$@"
n=$#
i=0
prev=x
merged=no
while [ "$i" -lt "$n" ]; do
	a=$1
	shift
	next=$a
	if [ "$prev" = "--settings" ]; then
		case $a in
		"{}") next='{"disableAllHooks":true}'; merged=yes ;;
		"{"*) next="{\"disableAllHooks\":true,${a#\{}"; merged=yes ;;
		esac
	fi
	set -- "$@" "$next"
	prev=$a
	i=$((i + 1))
done
[ "$merged" = yes ] || set -- --settings '{"disableAllHooks":true}' "$@"
exec "$real" "$@"
`
}

// piMain is the agent whose body the main session receives.
const piMain = "jhon-orchestrator"

// piPackages are the packages: the model bridge and web access, pinned;
// pi-pretty, pinned and installed for jhon-shell to load (piPretty); then
// jhon-shell, the release's own pi package (shell/), which pi loads from its
// path. Sub-agents, asking the owner, and the shell UI are jhon-shell's; the
// MCP tools are pi's built-in MCP support's (mcp.json).
//
// pi-claude-bridge is installed when the active profile runs a pi model on
// it (piBridged) or Claude Code is one of the install's runtimes
// (piBridgeLoaded); without Claude Code, a profile on another provider (an API
// key, a local one) needs neither.
func (p pi) piPackages() []any {
	out := []any{piWebAccess, piPretty(), filepath.Join(p.current(), ShellDir)}
	if p.piBridgeLoaded() {
		out = append([]any{piClaudeBridge}, out...)
	}
	return out
}

// BridgeProvider is pi-claude-bridge's pi provider, agents.yaml's and so the
// default profile's: pi reaches Claude through Claude Code.
const BridgeProvider = "claude-bridge"

// piProvider is a pi model's provider, `provider/model` split; "" for a
// bare model.
func piProvider(model string) string {
	provider, _, ok := strings.Cut(model, "/")
	if !ok {
		return ""
	}
	return provider
}

// PiProvider is the provider pi's main session runs on under profile (the
// orchestrator's pi model's, settings.json's defaultProvider); "" when it
// names none.
func PiProvider(m manifest.Manifest, profile any) string {
	a, ok := m.Find(piMain)
	if !ok {
		return ""
	}
	v, ok := profiles.ModelFor(m.TierTable(), profile, "pi", profiles.Agent{Name: a.Name, Tier: a.Tier})
	if !ok {
		return ""
	}
	t, _ := profiles.TierValue(js.String(v.Value))
	return piProvider(t.Model)
}

// piBridged is whether a pi model of the active profile, the main session's
// or an agent's, runs on claude-bridge: then the bridge's package, its
// config, and the wrapper that runs Claude Code are installed.
func (e *engine) piBridged() bool {
	for _, a := range append([]manifest.Agent{e.mainAgent(piMain)}, e.roles()...) {
		if m, ok := e.piModel(a); ok && piProvider(m.Model) == BridgeProvider {
			return true
		}
	}
	return false
}

// piBridgeLoaded is whether pi loads pi-claude-bridge: a pi model of the
// active profile runs on it (piBridged), or Claude Code is one of this
// install's runtimes (on, settings.runtimes), so that every session's model
// registry holds the bridge's models and the profiles can choose one (T130,
// L909). A profile that names none gets the bridge as a provider only
// (piProviderOnly).
func (e *engine) piBridgeLoaded() bool {
	return e.piBridged() || e.on("claude-code")
}

// piProviderOnly is config, a claude-bridge.json, with its AskClaude tool off
// and every other key kept: the bridge then registers its provider and no
// tool, so a session whose profile names no bridge model has the tools and
// the system prompt it had without the bridge (L708).
func piProviderOnly(config *js.Object) *js.Object {
	ask, ok := js.Prop(config, "askClaude").(*js.Object)
	if ok {
		ask = ask.Clone()
	} else {
		ask = js.NewObject()
	}
	ask.Set("enabled", false)
	config.Set("askClaude", ask)
	return config
}

// piPretty is pi-pretty (syntax-highlighted reads, diffs, file trees), pinned
// with every resource filtered out: pi installs it into the agent dir's npm
// directory, and jhon-shell's extensions/pretty.ts loads it from there under
// the shell UI, never in a sub-agent.
func piPretty() *js.Object {
	return js.Obj("source", "npm:@heyhuynhgiabuu/pi-pretty@"+PiPrettyVersion, "extensions", []any{}, "skills", []any{}, "prompts", []any{}, "themes", []any{})
}

// piMcp is mcp.json: the MCP servers by name (Engram, codegraph, Jhon's
// own read-only `jhon mcp`, its audit and status, and, unless it is off,
// context7's hosted endpoint: context7.go). pi names a server's tools
// mcp__<server>__<tool>, as Claude Code does, with every character but
// letters, digits and `_` written `_`; `direct` exposure declares
// them to the model as ordinary tools (the default reaches them only through
// codemode, which the settings turn off), so an agent's --tools can name them.
//
// The integrations' servers are their manifests' pi blocks
// (integrations/<name>/manifest.yaml): the required ones first, then
// Jhon's own, then the others, an optional one only while it is on.
func (p pi) piMcp() *js.Object {
	servers := js.Obj()
	mount := func(m integrations.Manifest) {
		if m.Pi == nil {
			return
		}
		if m.Pi.URL != "" {
			servers.Set(m.Pi.Server, js.Obj("url", m.Pi.URL, "exposure", "direct"))
			return
		}
		args := make([]any, len(m.Pi.Args))
		for i, a := range m.Pi.Args {
			args[i] = a
		}
		servers.Set(m.Pi.Server, js.Obj("command", m.Pi.Command, "args", args, "exposure", "direct"))
	}
	for _, m := range integrations.Required() {
		mount(m)
	}
	servers.Set("jhon", js.Obj("command", p.bin(), "args", []any{"mcp"}, "exposure", "direct"))
	for _, m := range integrations.All() {
		if on, optional := p.optionalOn[m.Name]; !m.Required && (!optional || on) {
			mount(m)
		}
	}
	return js.Obj("mcpServers", servers)
}

// piTools are a capability's tools in pi, Jhon's own; an integration's are
// its manifest's (integrationPiTools).
var piTools = map[string][]string{
	"read": {"read", "grep", "find", "ls"}, "write": {"edit", "write"}, "bash": {"bash"},
	"web": {"web_search", "fetch_content"}, "web-search": {"web_search"},
	"inspect": {"inspect"}, "todo": {"todo"},
}

func (e *engine) piTools(a manifest.Agent) []string {
	var tools []string
	for _, c := range a.Caps {
		if t, ok := piTools[c]; ok {
			tools = append(tools, t...)
			continue
		}
		if m, ok := integrations.ByCapability(c); ok {
			list, _ := e.m.Capabilities.Get(c)
			tools = append(tools, integrationPiTools(m, c, list)...)
		}
	}
	return unique(tools)
}

// integrationPiTools are capability c's tools in pi: the manifest's pi.tools
// for c, else each Claude Code tool of c (list) under the pi server's
// prefix, mcp__<server>__<tool>, whichever Claude Code prefix it carries.
// An integration pi mounts no server for has none.
func integrationPiTools(m integrations.Manifest, c string, list []string) []string {
	if m.Pi == nil {
		return nil
	}
	if t, ok := m.Pi.Tools[c]; ok {
		return t
	}
	var tools []string
	for _, t := range list {
		for _, p := range m.ClaudePrefixes() {
			if rest, ok := strings.CutPrefix(t, p); ok {
				tools = append(tools, m.PiPrefix()+rest)
				break
			}
		}
	}
	return tools
}

// promptText is the orchestrator's appended text for pi before its
// guarantees: its body and the always-on layer, with pi's edits (pilean.go).
func (p pi) promptText() string {
	text, _ := piLean(p.promptFull(), filepath.Join(p.current(), ProseDir))
	return text
}

// promptFull is that text before pi's edits, as Claude Code's carries it.
func (p pi) promptFull() string {
	return p.bodyFor("pi", p.mainAgent(piMain)) + "\n\n" + p.alwaysOnSection("pi", true)
}

// piModel is an agent's pi value, `provider/model@thinking`, split.
func (e *engine) piModel(a manifest.Agent) (profiles.Tier, bool) {
	value, _, _ := e.modelOf("pi", a)
	return profiles.TierValue(value)
}

// Files are the settings, jhon.json, the main prompt, the prompt
// templates, and one definition per delegable agent.
func (p pi) Files() []File {
	guarantees := "\n\n## Runtime guarantees\n\n" + p.runtimeGuarantees("pi")
	main := p.mainAgent(piMain)
	mainModel, _ := p.piModel(main)
	parts := strings.Split(mainModel.Model, "/")
	settings := js.Obj(
		"packages", p.piPackages(),
		"extensions", []any{"-builtin:codemode"},
		"skills", []any{filepath.Join(p.current(), ProseDir, "skills")},
	)
	if len(parts) > 1 {
		settings.Set("defaultProvider", parts[0])
		settings.Set("defaultModel", strings.Join(parts[1:], "/"))
	}
	if mainModel.Effort != "" {
		settings.Set("defaultThinkingLevel", mainModel.Effort)
	}
	settings.Set("theme", themeName)
	settings.Set("quietStartup", true)
	if prev, ok := parsedPiSettings(readText(p.piSettings())); ok {
		for _, k := range append(piOwnedSettings, ownerPiSettings...) {
			if prev.Has(k) {
				settings.Set(k, js.Prop(prev, k))
			}
		}
	}
	config := js.Obj(
		"jhon", p.bin(),
		"orchestrator", p.piPromptFile(),
		"vcsDirs", strs(p.vcsDirs()),
		"protected", strs(p.frameworkProtected()),
		"inspectAllow", strs(p.m.InspectAllow),
	)
	out := []File{
		{p.piSettings(), js.StringifyValue(settings) + "\n"},
		{p.piConfig(), js.StringifyValue(config) + "\n"},
		{p.piMcpFile(), js.StringifyValue(p.piMcp()) + "\n"},
		{p.piPromptFile(), p.promptText() + guarantees + "\n"},
		{p.piWriterFile(), js.Trim(inRuntime("pi", mustReadText(filepath.Join(p.prose, writerProse)))) + "\n"},
	}
	if Theme != nil {
		out = append(out, File{filepath.Join(p.piHome(), "themes", themeName+".json"), Theme()})
	}
	// Another provider without Claude Code leaves the bridge's files as they
	// are (T56); with it, the bridge is a provider only (T130).
	if p.piBridgeLoaded() {
		bridge, real := p.piBridge()
		if !p.piBridged() {
			bridge = piProviderOnly(bridge)
		}
		out = append(out, File{p.piBridgeConfig(), js.StringifyValue(bridge) + "\n"}, File{p.piProviderWrapper(), piWrapper(real)})
	}
	for _, a := range p.roles() {
		fm := []string{"---", "name: " + a.Name, "description: " + jsonText(a.Description)}
		if m, ok := p.piModel(a); ok {
			fm = append(fm, "model: "+m.Model)
			if m.Effort != "" {
				fm = append(fm, "thinking: "+m.Effort)
			}
		}
		if a.Bounded == "true" {
			fm = append(fm, "bounded: true")
		}
		// No `maxTurns`: jhon-shell reads no turn cap (T131); a manifest's is Claude Code's own.
		fm = append(fm, "tools:")
		for _, t := range p.piTools(a) {
			fm = append(fm, "  - "+t)
		}
		fm = append(fm, "---", "")
		layer := ""
		if a.OmitClaudeMd == "" {
			layer = "\n\n" + p.alwaysOnSection("pi", false)
		}
		out = append(out, File{filepath.Join(p.piAgents(), a.Name+".md"), strings.Join(fm, "\n") + p.bodyFor("pi", a) + layer + guarantees + "\n"})
	}
	return out
}

// piOwnedSettings are the settings.json keys pi writes itself at startup
// (pi 1.5.0: the changelog it last showed, per extension too). They are
// pi's: never drift, and an install keeps them.
var piOwnedSettings = []string{"lastChangelogVersion", "extensionChangelogVersions"}

// ownerPiSettings are the keys the first install seeds and the owner may
// change afterwards (the theme, from Customize): an install keeps theirs.
var ownerPiSettings = []string{"theme"}

// parsedPiSettings is settings.json's text as an object; false when it is
// not one.
func parsedPiSettings(text string) (*js.Object, bool) {
	v, err := js.ParseJSON(text)
	if err != nil {
		return nil, false
	}
	o, ok := v.(*js.Object)
	return o, ok
}

// samePiSettings is whether settings.json as it stands holds what the
// render wants, as JSON: pi rewrites the file in its own layout (no
// trailing newline), which is not drift.
func samePiSettings(prev, want string) bool {
	var a, b any
	if json.Unmarshal([]byte(prev), &a) != nil || json.Unmarshal([]byte(want), &b) != nil {
		return false
	}
	return reflect.DeepEqual(a, b)
}

// strs is a string list as JSON values.
func strs(xs []string) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

// Install writes the projection where ~/.pi exists or a setting chooses pi
// (T10h, as Claude Code), prunes what an earlier
// render recorded and this one does not write, seeds the theme where none
// stands, and links the owner's credentials where nothing else stands.
func (p pi) Install(s *run) bool {
	s.println("pi:")
	if !exists(p.piRoot()) && !p.chosen["pi"] {
		s.println(fmt.Sprintf("  skip   %s is absent; not created", s.tilde(p.piRoot())))
		return false
	}
	if !s.args.check {
		mkdirAll(p.piAgents())
		mkdirAll(p.piPrompts())
		mkdirAll(filepath.Join(p.piHome(), "themes"))
		// An earlier release linked claude-bridge.json to the owner's: the
		// link goes, or writing Jhon's would write through it.
		if fi, err := os.Lstat(p.piBridgeConfig()); err == nil && fi.Mode()&os.ModeSymlink != 0 && p.piBridgeLoaded() {
			if err := os.Remove(p.piBridgeConfig()); err != nil {
				panic(err.Error())
			}
		}
	}
	s.writeFiles(p.Files())
	// The provider wrapper is run as a program; the node one a release
	// before wrote goes (pi runs on Bun and could not launch it).
	if !s.args.check {
		_ = os.Remove(filepath.Join(p.piHome(), "claude-code-provider.mjs"))
		if p.piBridgeLoaded() {
			if err := os.Chmod(p.piProviderWrapper(), 0o755); err != nil && !os.IsNotExist(err) {
				panic(err.Error())
			}
		}
	}
	var agents []string
	for _, a := range p.roles() {
		agents = append(agents, a.Name)
	}
	for _, d := range []struct {
		dir, record string
		current     []string
		why         string
	}{
		{p.piAgents(), p.piRecord(), agents, "no longer in agents.yaml"},
		{p.piPrompts(), p.piPromptsRecord(), nil, "pi's /jhon-ldd is jhon-shell's command now"},
	} {
		sorted := append([]string(nil), d.current...)
		sortJS(sorted)
		removed, recordChanged := pruneRecorded(d.dir, d.record, d.current, sorted, s.args.check)
		for _, name := range removed {
			s.println(fmt.Sprintf("  %s  %s (%s)", s.verb("prune"), s.tilde(filepath.Join(d.dir, name+".md")), d.why))
		}
		s.drift += len(removed)
		if recordChanged {
			s.report(d.record, true)
		}
	}
	for _, f := range p.piLinks() {
		target, link := filepath.Join(p.piOwner(), f), filepath.Join(p.piHome(), f)
		if !s.args.check {
			if err := os.Symlink(target, link); err != nil {
				panic(err.Error())
			}
		}
		s.println(fmt.Sprintf("  %s  %s -> %s", s.verb("link "), s.tilde(link), s.tilde(target)))
		s.drift++
	}
	return true
}

// piLinks are the credential links to make: each that the owner's
// directory has and Jhon's lacks. A file or another link already there is
// left alone.
func (p pi) piLinks() []string {
	var out []string
	for _, f := range piLinkNames {
		if exists(filepath.Join(p.piOwner(), f)) && !lexists(filepath.Join(p.piHome(), f)) {
			out = append(out, f)
		}
	}
	return out
}

// roles are the agents a main session may delegate to: every one that is
// not primary.
func (e *engine) roles() []manifest.Agent {
	var out []manifest.Agent
	for _, a := range e.m.Agents {
		if a.Mode != "primary" {
			out = append(out, a)
		}
	}
	return out
}

// mainAgent is the agent named name, which a runtime's main session runs.
func (e *engine) mainAgent(name string) manifest.Agent {
	a, ok := e.m.Find(name)
	if !ok {
		panic("TypeError: Cannot read properties of undefined (reading 'name')")
	}
	return a
}
