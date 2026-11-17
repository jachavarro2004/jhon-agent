package render

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/manifest"
	"github.com/jachavarro2004/jhon-agent/engine/internal/profiles"
)

// Claude Code: each agent's frontmatter, body, and the always-on layer
// (alwaysOnSection), since the global CLAUDE.md carries no Jhon text.
type claude struct{ *engine }

func (claude) Runtime() string { return "claude-code" }

func (c claude) Dir() string { return c.path(".claude") }

// toolPrimitive are the tools that exist only where a runtime primitive
// does (docket AG4): claudeTools grants one only when the claude-code row
// of `runtime_primitives` has it, and `Agent(...)` likewise needs
// `named-subagents`.
var toolPrimitive = map[string]string{"AskUserQuestion": "ask-user", "ExitPlanMode": "plan-mode", "SendMessage": "named-subagents"}

// delegable are the agents an orchestrator may launch: every agent that is
// not primary.
func (e *engine) delegable() []string {
	var out []string
	for _, a := range e.m.Agents {
		if a.Mode != "primary" {
			out = append(out, a.Name)
		}
	}
	return out
}

// claudeTools is an agent's Claude Code tool allowlist: its capabilities'
// tools, those resting on an absent primitive dropped, and `orchestrate`'s
// `Agent(<names>)` derived from the manifest, never listed by hand.
func (e *engine) claudeTools(a manifest.Agent) []string {
	if a.Caps == nil {
		panic("TypeError: Cannot read properties of undefined (reading 'flatMap')")
	}
	var tools []string
	for _, c := range a.Caps {
		list, _ := e.m.Capabilities.Get(c)
		for _, t := range list {
			if p, gated := toolPrimitive[t]; !gated || e.m.Matrix.HasPrimitive("claude-code", p) {
				tools = append(tools, t)
			}
		}
	}
	if slices.Contains(a.Caps, "orchestrate") && e.m.Matrix.HasPrimitive("claude-code", "named-subagents") {
		tools = append(tools, "Agent("+strings.Join(e.delegable(), ", ")+")")
	}
	return unique(tools)
}

// unique is xs without repeats, each at its first place.
func unique(xs []string) []string {
	out := []string{}
	for _, x := range xs {
		if !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

// canWrite is whether an agent's tools include Edit or Write, which carry
// the write guard (the VCS guard, internal/hook).
func (e *engine) canWrite(a manifest.Agent) bool {
	return slices.ContainsFunc(e.claudeTools(a), func(t string) bool { return t == "Edit" || t == "Write" })
}

// shq is POSIX single-quoting: the only form in which no character is special.
func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// command is a hook command line: each word single-quoted.
func command(words ...string) string {
	q := make([]string, len(words))
	for i, w := range words {
		q[i] = shq(w)
	}
	return strings.Join(q, " ")
}

// jsonText is v as compact JSON (js.StringifyValueCompact), or `undefined` for
// an absent value.
func jsonText(v any) string {
	if _, ok := v.(js.Undefined); ok {
		return "undefined"
	}
	return js.StringifyValueCompact(v)
}

// hookEntry is one PreToolUse entry: a matcher and the guard it runs,
// arguments quoted. The owning agent leads as `--agent <name>`, and the
// guard uses the name to leave other agents' calls alone. Claude Code
// 2.1.310 runs no frontmatter hooks: the plugin's agent-hooks reads these
// and runs the calling agent's.
func (e *engine) hookEntry(owner, matcher, name string, args []string) []string {
	// Docket AV1: the runtime runs the active release, never the source.
	cmd := e.hookCommand(name, append([]string{"--agent", owner}, args...)...)
	return []string{
		"    - matcher: " + jsonText(matcher),
		"      hooks:",
		"        - type: command",
		"          command: " + jsonText(cmd),
	}
}

// hooksBlock is the hooks an agent's tools require, under one
// `hooks:` key (two would be a YAML duplicate, and the parser keeps only
// the last). The guards are the safety layer: safety-guard on Bash,
// inspect-guard for a reviewer's Bash, vcs-guard on a writer's writes,
// scope-guard on a bounded writer, and framework-guard and route-guard on
// every agent (docket AV2), each scoped to the agent it is rendered for.
// boundedWriters are the agents scope-guard holds to their handoff's
// `## Allowed edit surfaces`, in manifest order.
func (e *engine) boundedWriters() []string {
	var out []string
	for _, a := range e.m.Agents {
		if a.Bounded == "true" {
			out = append(out, a.Name)
		}
	}
	return out
}

func (e *engine) hooksBlock(a manifest.Agent) []string {
	// Docket AG4: hooks are emitted only where the matrix says claude-code
	// has blocking hooks with agent identity.
	if !e.m.Matrix.HasPrimitive("claude-code", "blocking-hook-agent") {
		return nil
	}
	// Docket AY B4: the agent's guards run in one process, `jhon hook
	// pre-tool`, which applies each guard to the tools guard.Matchers gives
	// it; the entry's matcher is their union.
	var guards []string
	matched := map[string]bool{}
	add := func(name string, args ...string) {
		guards = append(append(guards, name), args...)
		for _, tool := range guard.ToolsOf(name) {
			matched[tool] = true
		}
	}
	if slices.Contains(a.Caps, "inspect") {
		add("inspect-guard", e.m.InspectAllow...)
	}
	if slices.Contains(e.claudeTools(a), "Bash") {
		add("safety-guard")
	}
	if e.canWrite(a) {
		add("vcs-guard", e.vcsDirs()...)
	}
	add("framework-guard", e.frameworkProtected()...)
	add("secrets-guard")
	if a.Bounded == "true" {
		add("scope-guard")
	}
	// Every agent is held to its change's route (hook/routeguard.go); the
	// bounded writers' names let it refuse launching one with no scope.
	add("route-guard", e.boundedWriters()...)
	var tools []string
	for _, tool := range guard.ToolOrder {
		if matched[tool] {
			tools = append(tools, tool)
		}
	}
	matcher := strings.Join(tools, "|")
	if matched[guard.All] {
		matcher = guard.All
	}
	entries := e.hookEntry(a.Name, matcher, "pre-tool", guards)
	// Plan item 8: a main agent's session tells the owner when its context
	// is long enough that a handoff and /clear would pay.
	var stop []string
	if a.Mode == "primary" {
		stop = []string{"  Stop:", "    - hooks:", "        - type: command", "          command: " + jsonText(e.hookCommand("context-notice"))}
	}
	// Docket AY B2: a writer's edited file is rewritten by its project's
	// own formatter, so style is never the agent's call.
	var post []string
	if e.canWrite(a) {
		post = append([]string{"  PostToolUse:"}, e.hookEntry(a.Name, "Edit|Write", "format-edit", nil)...)
	}
	if len(entries) == 0 {
		return nil
	}
	return append(append(append([]string{"hooks:", "  PreToolUse:"}, entries...), post...), stop...)
}

// claudeModel is the `model:` and, when the tier or the role sets one, the
// `effort:` frontmatter lines; a role's own effort overrides its tier's.
func claudeModel(value, roleEffort string) []string {
	t, ok := profiles.TierValue(value)
	if !ok {
		return []string{"model: " + value}
	}
	effort := roleEffort
	if effort == "" {
		effort = t.Effort
	}
	out := []string{"model: " + t.Model}
	if effort != "" {
		out = append(out, "effort: "+effort)
	}
	return out
}

// Files are each agent's definition under ~/.claude/agents. Each ends with
// the brief claude-code runtime guarantees (docket AG4), with the
// frontmatter that carries the enforcement.
func (c claude) Files() []File {
	guarantees := "\n\n## Runtime guarantees\n\n" + c.runtimeGuarantees("claude-code")
	primary, sub := "\n\n"+c.alwaysOnSection("claude-code", true), "\n\n"+c.alwaysOnSection("claude-code", false)
	var out []File
	for _, a := range c.m.Agents {
		tools := c.claudeTools(a)
		// `Agent(a, b)` contains commas, so such a list renders as a YAML
		// block list; every other agent keeps the single line.
		toolsField := []string{"tools: " + strings.Join(tools, ", ")}
		if slices.ContainsFunc(tools, func(t string) bool { return strings.Contains(t, ",") }) {
			toolsField = []string{"tools:"}
			for _, t := range tools {
				toolsField = append(toolsField, "  - "+t)
			}
		}
		fm := []string{"---", "name: " + a.Name, "description: " + jsonText(a.Description)}
		if value, own, ok := c.modelOf("claude-code", a); ok {
			effort := a.Effort
			if own {
				effort = ""
			}
			fm = append(fm, claudeModel(value, effort)...)
		}
		if a.MaxTurns != "" {
			fm = append(fm, "maxTurns: "+a.MaxTurns)
		}
		if a.OmitClaudeMd != "" {
			fm = append(fm, "omitClaudeMd: "+a.OmitClaudeMd)
		}
		fm = append(fm, toolsField...)
		fm = append(fm, c.hooksBlock(a)...)
		fm = append(fm, "---", "")
		layer := sub
		switch {
		case a.OmitClaudeMd != "":
			layer = ""
		case a.Mode == "primary":
			layer = primary
		}
		out = append(out, File{filepath.Join(c.claudeAgents(), a.Name+".md"), strings.Join(fm, "\n") + c.bodyFor("claude-code", a) + layer + guarantees + "\n"})
	}
	return out
}

// Install writes the agents, prunes the ones a previous render recorded
// that the manifest no longer has, removes an earlier release's block from
// CLAUDE.md, and the Jhon guards from settings.json.
func (c claude) Install(s *run) bool {
	s.println("claude-code:")
	// --check reports drift and must not touch the filesystem.
	if !s.args.check {
		mkdirAll(c.claudeAgents())
	}
	s.writeFiles(c.Files())
	removed, recordChanged := c.pruneAgents()
	for _, name := range removed {
		s.println(fmt.Sprintf("  %s  %s (no longer in agents.yaml)", s.verb("prune"), s.tilde(filepath.Join(c.claudeAgents(), name+".md"))))
	}
	s.drift += len(removed)
	if recordChanged {
		s.report(c.agentsRecord(), true)
	}

	if !exists(c.claudeMd()) {
		s.println(fmt.Sprintf("  skip   %s is absent; not created", s.tilde(c.claudeMd())))
	} else {
		// validateRuntime has already refused bad markers.
		prev := readText(c.claudeMd())
		next, _ := withoutClaudeMdBlock(prev)
		changed := prev != next
		if changed && !s.args.check {
			writeText(c.claudeMd(), next)
		}
		s.report(c.claudeMd(), changed)
	}

	changed, err := c.installSettings(s.args.check)
	if err != "" {
		s.eprintln(fmt.Sprintf("  error  %s %s: the top-level session has no Jhon guard until it is fixed", s.tilde(c.settings()), err))
		s.failed = true
	} else {
		s.report(c.settings(), changed)
	}
	return true
}

// agentName is a plain agent name, the only line of a record trusted to a removal.
var agentName = namePattern

// pruneAgents removes the agents a previous render recorded in
// ~/.claude/agents/.jhon-owned that the manifest no longer has. A name
// enters the record only by being rendered, so a file never recorded is
// never deleted. --check computes and touches nothing.
func (c claude) pruneAgents() (removed []string, recordChanged bool) {
	current := c.m.AgentNames()
	slices.SortFunc(current, js.Compare)
	return pruneRecorded(c.claudeAgents(), c.agentsRecord(), current, current, c.args.check)
}

// pruneRecorded removes <dir>/<name>.md for each name the record lists that
// current lacks, and rewrites the record as the sorted names; the record
// is the ownership marker, so a file it never named stays.
func pruneRecorded(dir, record string, current, sorted []string, check bool) (removed []string, recordChanged bool) {
	prev := readText(record)
	for _, l := range strings.Split(prev, "\n") {
		name := js.Trim(l)
		if agentName.MatchString(name) && !slices.Contains(current, name) && lexists(filepath.Join(dir, name+".md")) {
			removed = append(removed, name)
		}
	}
	next := strings.Join(sorted, "\n") + "\n"
	recordChanged = prev != next
	if !check {
		for _, name := range removed {
			os.Remove(filepath.Join(dir, name+".md"))
		}
		if recordChanged {
			writeText(record, next)
		}
	}
	return removed, recordChanged
}
