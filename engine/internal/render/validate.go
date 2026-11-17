package render

import (
	"errors"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jachavarro2004/jhon-agent/engine/internal/auditcheck"
	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/manifest"
	"github.com/jachavarro2004/jhon-agent/engine/internal/profiles"
)

// Validation. Byte-diffing generated output cannot catch a rule that is
// internally inconsistent, so every rule below exists because a real
// defect shipped past a clean `--check`. Validation runs before any write and aborts the render.

// topLevelPrefixes are the Claude Code prefixes of the integration granting
// memory-session, the top-level tools' capability: the primary agent holds
// each top-level tool under every one of them.
func topLevelPrefixes() []string {
	m, _ := integrations.ByCapability("memory-session")
	return m.ClaudePrefixes()
}

// problem is one validation finding; line 0 names no line.
type problem struct {
	file string
	line int
	rule string
	msg  string
}

// claudeEfforts are the effort levels Claude Code documents for sub-agent frontmatter.
var claudeEfforts = []string{"low", "medium", "high", "xhigh", "max"}

// treeDirs are the top directories outside prose/ a reference may name.
var treeDirs = []string{"engine", "prose", "runtimes", "test", "tools", "docs"}

// repoDirs are the directories under prose/ whose Markdown is held to the rules.
var repoDirs = []string{"contracts", "agents", "persona"}

// enforcementProjections are the primitives through which each runtime's
// projection emits enforcement (docket AG4): an `enforced` level in a
// rendered runtime must rest on one of these.
var enforcementProjections = func() manifest.Map[[]string] {
	var m manifest.Map[[]string]
	m.Set("claude-code", []string{"blocking-hook-agent", "agent-tool-allowlist"})
	m.Set("pi", []string{"blocking-hook-agent", "agent-tool-allowlist"})
	return m
}()

// refusalPrimitives need a refusal observed: only a spike record verifies them.
var refusalPrimitives = []string{"blocking-hook-agent", "agent-tool-allowlist"}

// behaviouralAnchors are the rules whose breach `jhon audit` looks for, or
// declares it does not (audit-registry).
var behaviouralAnchors = []string{
	"runtime-boundaries/stop", "runtime-boundaries/same-effect", "runtime-boundaries/allowed-form", "runtime-boundaries/delegation",
	"runtime-boundaries/refusal-not-failure", "runtime-boundaries/no-technique", "runtime-boundaries/orchestrator-no-bypass",
	"top-level-tools/executor-never-calls",
	"isolation/no-other-tree",
	"delivery/tested-revision", "delivery/same-tree", "delivery/high-risk-verified",
	"framework-boundary/no-framework-write",
	"safety/destructive-deny", "safety/outward-ask",
	"efficiency/writer-scope",
	"ldd/slices", "ldd/red-first", "ldd/independent-verify", "ldd/explore-scope",
	"tdd/no-weaken",
}

// vocabulary is contracts/vocabulary.yaml, parsed: the values the code reads.
type vocabulary struct {
	file                                                  string
	exists                                                bool
	phases, artifactState, envelopeVerdict, nextAction    []string
	auditValidationStatus, topLevelTools, guaranteeLevels []string
	primitiveVerification, spikeRecordFields              []string
	auditRules                                            []map[string]string
	// sliceLimits is `slice_limits`, whose advisory size jhon status reads.
	sliceLimits map[string]string
}

func (e *engine) vocabulary() *vocabulary {
	if e.vocab != nil {
		return e.vocab
	}
	file := filepath.Join(e.prose, "contracts/vocabulary.yaml")
	src := readText(file)
	list := func(key string) []string { return contract.List(src, key) }
	e.vocab = &vocabulary{
		file: file, exists: src != "",
		phases: list("phases"), artifactState: list("artifact_state"), envelopeVerdict: list("envelope_verdict"),
		nextAction: list("next_action"), auditValidationStatus: list("audit_validation_status"),
		topLevelTools: list("top_level_tools"), guaranteeLevels: list("guarantee_levels"),
		primitiveVerification: list("primitive_verification"), spikeRecordFields: list("spike_record_fields"),
		auditRules: contract.Records(src, "audit_rules"),
	}
	_, e.vocab.sliceLimits = contract.Map(src, "slice_limits")
	return e.vocab
}

// generatedBodies are every templated agent's generated body, by the path
// it is rendered to.
func (e *engine) generatedBodies() *manifest.Map[string] {
	var out manifest.Map[string]
	for _, a := range e.m.Agents {
		if body, ok := e.generatedBody(a); ok {
			out.Set(e.bodyPath(a), body)
		}
	}
	return &out
}

// repoMarkdown is the validation surface, derived, never hand-picked:
// everything in the repository, plus every SKILL.md an agent body reads.
func (e *engine) repoMarkdown() []string {
	out := []string{filepath.Join(e.prose, "orchestrator.md")}
	if exists(filepath.Join(e.root, "README.md")) {
		out = append(out, filepath.Join(e.root, "README.md"))
	}
	// A generated body on disk may be stale or absent; validation reads
	// the expansion itself.
	generated := e.generatedBodies()
	for _, dir := range repoDirs {
		abs := filepath.Join(e.prose, dir)
		for _, f := range readDir(abs) {
			if p := filepath.Join(abs, f.Name()); strings.HasSuffix(f.Name(), ".md") && !generated.Has(p) {
				out = append(out, p)
			}
		}
	}
	out = append(out, generated.Keys()...)
	for _, name := range e.releaseCommands() {
		out = append(out, filepath.Join(e.pluginSkills(), name, "SKILL.md"))
	}
	for _, skill := range e.ownedSkills() {
		from := filepath.Join(e.prose, "skills", skill)
		for _, rel := range walk(from) {
			if strings.HasSuffix(rel, ".md") {
				out = append(out, filepath.Join(from, rel))
			}
		}
	}
	return out
}

// joinPath is path.join: a trailing slash on the last part is kept.
func joinPath(parts ...string) string {
	p := filepath.Join(parts...)
	if last := parts[len(parts)-1]; strings.HasSuffix(last, "/") && !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}

var ownSkillRef = lazyre.New(`^\.(?:claude|agents)/skills/([a-z0-9-]+)(/` + js.Dot + `*)?$`)

// resolveRef resolves a documented path to a real one. The source's legacy paths
// (`~/dev/jhon/...`, formerly `~/.agents/jhon/...`) and the active release (`~/.local/share/jhon/
// current/...`) both name this repository, and `~/.claude/skills/<name>` or
// `~/.agents/skills/<name>` a skill it owns, so they resolve against the
// root rather than $HOME.
func (e *engine) resolveRef(ref string) string {
	if !strings.HasPrefix(ref, "~/") {
		// The prose's references are relative to prose/; a path into the
		// rest of the tree names its top directory.
		if top, _, _ := strings.Cut(ref, "/"); slices.Contains(treeDirs, top) {
			return joinPath(e.root, ref)
		}
		return joinPath(e.prose, ref)
	}
	rest := ref[2:]
	if own := ownSkillRef.FindStringSubmatch(rest); own != nil && exists(filepath.Join(e.prose, "skills", own[1])) {
		return joinPath(e.prose, "skills", own[1], own[2])
	}
	switch {
	case strings.HasPrefix(rest, "dev/jhon/"):
		return joinPath(e.root, rest[len("dev/jhon/"):])
	case strings.HasPrefix(rest, ".agents/jhon/"): // the source's place before L207
		return joinPath(e.root, rest[len(".agents/jhon/"):])
	case rest == ".local/share/jhon/current" || rest == "dev/jhon" || rest == ".agents/jhon":
		return e.root
	case strings.HasPrefix(rest, ".local/share/jhon/current/"):
		return joinPath(e.root, rest[len(".local/share/jhon/current/"):])
	}
	return joinPath(e.home, rest)
}

var (
	changeArtifacts = []string{
		"proposal.md", "tasks.md", "design.md", "spec.md", "exploration.md",
		"research.md", "verify-report.md", "tdd-evidence.md", "SKILL.md", "config.yaml",
		"skill-registry.md", "CLAUDE.md", "AGENTS.md", "README.md",
		"execution-record.json", "delivery.json",
	}
	rootedRef    = lazyre.New("`(~/[^`" + js.SpaceChars + "]+|(?:contracts|agents|persona|skills|engine|prose|runtimes|test|tools|docs)/[^`" + js.SpaceChars + "]+)`")
	bareRef      = lazyre.New("`([\\w.-]+\\.(?:md|ts|yaml|json))`")
	malformed    = lazyre.New(`~~/`)
	slashCommand = lazyre.New(`/((?:jhon-)?ldd(?:-[a-z][a-z-]*)?)`)
	trailingDash = lazyre.New(`-+$`)
	frontmatter  = lazyre.New(`\A---\n((?s:.*?))\n---\n`)
	verdictLine  = lazyre.New(`^` + js.Space + `*verdict:` + js.Space + `*(` + js.Dot + `+)$`)
	fence        = lazyre.New("^" + js.Space + "*(```|~~~)")
	namedJhon    = lazyre.New("`jhon ([a-z][a-z-]*)")
	runJhon      = lazyre.New(`^` + js.Space + `*(?:\$` + js.Space + `+)?jhon ([a-z][a-z-]*)`)
	judgmentLine = lazyre.New(`JUDGMENT:` + js.Space + `*([A-Z_]+)`)
	whitespace   = lazyre.New(js.Space + `+`)
	fixtureName  = lazyre.New(`^[\w.-]+$`)
)

// lines is src split at "\n", as String.prototype.split("\n").
func lines(src string) []string { return strings.Split(src, "\n") }

// hasLine is a `^line$` test under the m flag: whether some line of src,
// split at every JavaScript line terminator, is exactly line.
func hasLine(src, line string) bool {
	return slices.Contains(strings.FieldsFunc(src, func(r rune) bool {
		return r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029
	}), line)
}

// slashCommands are the `/ldd…` and `/jhon-ldd…` tokens of a line that
// start a word: (?<=^|[\s`("'[]) emulated, as RE2 has no lookbehind.
func slashCommands(line string) []string {
	var out []string
	for from := 0; from < len(line); {
		m := slashCommand.FindStringSubmatchIndex(line[from:])
		if m == nil {
			break
		}
		start := from + m[0]
		prev, _ := utf8.DecodeLastRuneInString(line[:start])
		if start == 0 || js.IsSpace(prev) || strings.ContainsRune("`(\"'[", prev) {
			out = append(out, line[from+m[2]:from+m[3]])
			from += m[1]
		} else {
			from = start + 1
		}
	}
	return out
}

// sameSet is whether a and b hold the same items, as their sorted joins.
func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	sortJS(x)
	sortJS(y)
	return strings.Join(x, ",") == strings.Join(y, ",")
}

// validate judges the repository alone; it passes on a machine where
// nothing has been rendered.
func (e *engine) validate() []problem {
	var problems []problem
	// A problem names a prose file as the prose names it, relative to prose/.
	rel := func(f string) string {
		if r, ok := strings.CutPrefix(f, e.prose+"/"); ok {
			return r
		}
		return strings.Replace(f, e.root+"/", "", 1)
	}
	add := func(file string, line int, rule, msg string) {
		problems = append(problems, problem{rel(file), line, rule, msg})
	}
	manifestFile := filepath.Join(e.prose, "agents.yaml")
	for _, p := range e.integrationProblems() {
		add(filepath.Join(e.root, p.File), 0, "integration", strings.TrimPrefix(p.Field+": "+p.Msg, ": "))
	}

	files := e.repoMarkdown()
	generated := e.generatedBodies()
	var text manifest.Map[string]
	for _, f := range files {
		if body, ok := generated.Get(f); ok {
			text.Set(f, body)
		} else {
			text.Set(f, mustReadText(f))
		}
	}
	textOf := func(f string) string { s, _ := text.Get(f); return s }
	refExists := func(p string) bool { return exists(p) || generated.Has(p) }

	// 1. Every file reference must resolve.
	repoNames := map[string]bool{}
	for _, f := range e.repoMarkdown() {
		repoNames[f[strings.LastIndex(f, "/")+1:]] = true
	}
	for _, dir := range []string{e.root, e.prose} {
		for _, f := range readDir(dir) {
			repoNames[f.Name()] = true
		}
	}
	skills := filepath.Join(e.prose, "skills") + "/"
	readme := filepath.Join(e.root, "README.md")
	for _, file := range text.Keys() {
		isSkill := strings.HasPrefix(file, skills)
		// The README maps the repository; a release ships without its
		// development half (T10f), so its references are the checkout's.
		repoMap := file == readme && e.inRelease()
		for i, line := range lines(textOf(file)) {
			if malformed.MatchString(line) {
				add(file, i+1, "malformed-tilde", "'~~/' is not a home reference")
			}
			for _, m := range rootedRef.FindAllStringSubmatch(line, -1) {
				ref := m[1]
				if strings.ContainsAny(ref, "<{") {
					continue // a template, not a path
				}
				if isSkill && !strings.HasPrefix(ref, "~/dev/jhon/") && !strings.HasPrefix(ref, "~/.agents/jhon/") && !strings.HasPrefix(ref, runtimeRef+"/") {
					continue
				}
				if !repoMap && !refExists(e.resolveRef(ref)) {
					add(file, i+1, "broken-ref", ref+" does not resolve")
				}
			}
			if isSkill {
				continue // bare filenames in a skill name project files
			}
			for _, m := range bareRef.FindAllStringSubmatch(line, -1) {
				if name := m[1]; !slices.Contains(changeArtifacts, name) && !repoNames[name] {
					add(file, i+1, "broken-ref", name+" names no file in this repo and is not a change artifact")
				}
			}
		}
	}

	// 2. Manifest integrity.
	for _, m := range e.m.Issues {
		add(manifestFile, m.Line, "manifest", m.Msg)
	}
	for _, runtime := range e.m.Tiers.Keys() {
		row, _ := e.m.Tiers.Get(runtime)
		for _, tier := range row.Keys() {
			value, _ := row.Get(tier)
			t, ok := profiles.TierValue(value)
			if !ok {
				add(manifestFile, 0, "manifest", fmt.Sprintf("tiers.%s.%s: write the value as model or model@effort", runtime, tier))
			} else if runtime == "claude-code" && t.Effort != "" && !slices.Contains(claudeEfforts, t.Effort) {
				add(manifestFile, 0, "manifest", fmt.Sprintf("tiers.%s.%s: effort '%s' is not one of %s", runtime, tier, t.Effort, strings.Join(claudeEfforts, ", ")))
			}
		}
	}
	for _, a := range e.m.Agents {
		e.agentProblems(a, func(rule, msg string) { add(manifestFile, 0, rule, msg) }, rel)
	}

	// 4. The vocabulary contract is what every value below is compared against.
	vocab := e.vocabulary()
	if !vocab.exists {
		add(vocab.file, 0, "vocabulary", "contracts/vocabulary.yaml is missing")
		return problems // every rule below depends on it
	}
	for _, l := range []struct {
		name   string
		values []string
	}{{"phases", vocab.phases}, {"artifact_state", vocab.artifactState}, {"envelope_verdict", vocab.envelopeVerdict}, {"next_action", vocab.nextAction}, {"top_level_tools", vocab.topLevelTools}} {
		if len(l.values) == 0 {
			add(vocab.file, 0, "vocabulary", l.name+" is empty")
		}
	}
	// An advisory size jhon status cannot read drops its note, so it is
	// caught here, before a release carries it.
	if n, err := strconv.Atoi(vocab.sliceLimits["tasks"]); err != nil || n < 1 {
		add(vocab.file, 0, "vocabulary", fmt.Sprintf("slice_limits.tasks must be a positive number, found %q", vocab.sliceLimits["tasks"]))
	}

	// 10. Slash commands: the real names are the plugin's skills.
	known := e.releaseCommands()
	for _, file := range text.Keys() {
		for i, line := range lines(textOf(file)) {
			for _, cmd := range slashCommands(line) {
				name := trailingDash.ReplaceAllString(cmd, "")
				if !strings.HasPrefix(name, "jhon-") {
					add(file, i+1, "slash-command", "/"+name+" is not a command; the command is /jhon-ldd <action>")
				} else if !slices.Contains(known, name) {
					add(file, i+1, "slash-command", fmt.Sprintf("/%s has no plugin/skills/%s/SKILL.md", name, name))
				}
			}
		}
	}

	// 11. Plugin commands: `name` equal to its directory, and
	// `disable-model-invocation: true`.
	for _, name := range e.releaseCommands() {
		file := filepath.Join(e.pluginSkills(), name, "SKILL.md")
		fm := ""
		if exists(file) {
			if m := frontmatter.FindStringSubmatch(mustReadText(file)); m != nil {
				fm = m[1]
			}
		}
		if !hasLine(fm, "name: "+name) {
			add(file, 1, "plugin-skill", "the frontmatter must say name: "+name)
		}
		if !hasLine(fm, "disable-model-invocation: true") {
			add(file, 1, "plugin-skill", "a command needs disable-model-invocation: true")
		}
	}

	// 14. A verify report's `verdict:` line stays two-valued.
	for _, file := range text.Keys() {
		if file == vocab.file {
			continue
		}
		for i, line := range lines(textOf(file)) {
			m := verdictLine.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			var declared, bad []string
			for _, s := range strings.Split(m[1], "|") {
				v := strings.ReplaceAll(js.Trim(s), "`", "")
				declared = append(declared, v)
				if v != "" && !slices.Contains(vocab.envelopeVerdict, v) {
					bad = append(bad, v)
				}
			}
			if len(bad) > 0 {
				add(file, i+1, "verdict-vocab", fmt.Sprintf("envelope verdict {%s} must be {%s}", strings.Join(declared, ", "), strings.Join(vocab.envelopeVerdict, ", ")))
			}
		}
	}

	// 21. `jhon` subcommands named in prose must be ones jhon dispatches.
	e.jhonCommands(&text, add)

	// 22. The delta contract agrees with itself and the spec template.
	{
		deltaFile := filepath.Join(e.prose, "contracts/spec-delta.md")
		src := textOf(deltaFile)
		order, metadata := contract.List(src, "delta_order"), contract.List(src, "delta_metadata")
		if len(order) == 0 {
			add(deltaFile, 0, "spec-delta", "delta_order is empty")
		}
		if len(metadata) == 0 {
			add(deltaFile, 0, "spec-delta", "delta_metadata is empty")
		}
		keys, values := contract.Map(src, "delta_required")
		for _, op := range keys {
			if !slices.Contains(order, op) {
				add(deltaFile, 0, "spec-delta", fmt.Sprintf("delta_required names '%s', which is not in delta_order", op))
			}
			for _, need := range contract.InlineList(values[op]) {
				if !slices.Contains(metadata, need) {
					add(deltaFile, 0, "spec-delta", fmt.Sprintf("delta_required %s needs '%s', which is not in delta_metadata", op, need))
				}
			}
		}
		for _, op := range contract.List(src, "delta_heading_only") {
			if !slices.Contains(order, op) {
				add(deltaFile, 0, "spec-delta", fmt.Sprintf("delta_heading_only names '%s', which is not in delta_order", op))
			}
		}
		spec := filepath.Join(e.prose, "skills/ldd/references/spec.md")
		template := textOf(spec)
		for _, op := range order {
			if !strings.Contains(template, "## "+op+" Requirements") {
				add(spec, 0, "spec-delta", fmt.Sprintf("template (SKILL.md or references/) has no '## %s Requirements' section", op))
			}
		}
	}

	// 23. The judgment contract's lists agree with one another.
	e.judgmentProblems(&text, add)

	// 26a. The always-on template places every group and names no other.
	if tpl := e.claudeMdTemplate(); !exists(tpl) {
		add(tpl, 0, "claude-md-template", "is missing")
	} else {
		var groups manifest.Map[string]
		for _, g := range alwaysOnGroups {
			groups.Set(g.name, "")
		}
		_, unknown, unused := expandTemplate(mustReadText(tpl), &groups)
		for _, u := range unknown {
			add(tpl, 0, "claude-md-template", fmt.Sprintf("placeholder {{%s}} is not an ALWAYS_ON group", u))
		}
		for _, u := range unused {
			add(tpl, 0, "claude-md-template", fmt.Sprintf("ALWAYS_ON group '%s' has no {{%s}} placeholder", u, u))
		}
	}

	// 29. Top-level tools belong to the agent that owns the session: read
	// from the projected tool sets, never the caps.
	top := unique(vocab.topLevelTools)
	for _, a := range e.m.Agents {
		tools := e.claudeTools(a)
		primary := a.Mode == "primary"
		if primary {
			for _, t := range top {
				for _, p := range topLevelPrefixes() {
					if !slices.Contains(tools, p+t) {
						add(manifestFile, 0, "top-level-tools", fmt.Sprintf("%s: lacks %s in Claude Code; grant it through memory-session", a.Name, p+t))
					}
				}
			}
			continue
		}
		for _, t := range tools {
			if strings.HasPrefix(t, "mcp__") && slices.Contains(top, t[strings.LastIndex(t, "__")+2:]) {
				add(manifestFile, 0, "top-level-tools", fmt.Sprintf("%s: holds %s in Claude Code; only the top-level agent may call it", a.Name, t))
			}
		}
	}

	// An always-on file's runtime term with no words for a runtime ships as
	// literal `{{runtime:<term>}}` text in that runtime's prompts.
	for _, f := range alwaysOn() {
		for _, m := range runtimePlaceholder.FindAllStringSubmatch(mustReadText(filepath.Join(e.prose, f)), -1) {
			for _, r := range e.renderers() {
				if _, ok := runtimeTerms[m[1]][r.Runtime()]; !ok {
					add(filepath.Join(e.prose, f), 0, "agent-template", fmt.Sprintf("runtime term {{runtime:%s}} has no words for %s", m[1], r.Runtime()))
				}
			}
		}
	}

	// 31. Runtime boundaries: the stop is a written rule every agent receives.
	flat := func(f string) string { return whitespace.ReplaceAllString(textOf(f), " ") }
	const rb = "contracts/runtime-boundaries.md"
	if !slices.Contains(alwaysOn(), rb) {
		add(filepath.Join(e.root, "engine/internal/render/render.go"), 0, "runtime-boundaries", rb+" is not always on")
	}
	anchors(add, "runtime-boundaries", flat, []anchor{
		{rb, `\*\*A safety refusal is a stop\.\*\*`, "that a safety refusal is a stop"},
		{rb, `Report it verbatim`, "that a refusal is reported verbatim"},
		{rb, `\*\*Never reach the same effect another way\.\*\* The criterion is the effect, not the command`, "that the same effect is never reached another way"},
		{rb, `a delegate asked to do what the refusal prevented`, "that delegating the refused effect is the same bypass"},
		{rb, `\*\*A refusal is not a failure\.\*\*`, "that an operational failure is not a refusal"},
		{rb, `\*\*Record the refusal, never the way around it\*\*`, "that a way around a refusal is never recorded"},
		{rb, `using the allowed form is compliance, not a bypass\.`, "that a form the same authority allows is compliance, not a bypass"},
		{rb, "\\*\\*The owner resolves it\\*\\*[^*]*`jhon grant <class>`", "that the owner resolves a refusal, with `jhon grant <class>` for an outward-facing class"},
	}, e.prose)
	if orch := filepath.Join(e.prose, "orchestrator.md"); !strings.Contains(flat(orch), "Never perform, propose, or instruct a bypass, including by re-delegating the same effect to another agent.") {
		add(orch, 0, "runtime-boundaries", "does not forbid bypassing an executor's refusal (\"Executor refusals\")")
	}

	// 31b. pi's appended text (pilean.go): every edit applies once, and every
	// reference it adds resolves.
	{
		_, missing := piLean(pi{e}.promptFull(), filepath.Join(e.current(), ProseDir))
		for _, m := range missing {
			add(filepath.Join(e.root, "engine/internal/render/pilean.go"), 0, "pi-prompt", m)
		}
		for _, ed := range piEdits(filepath.Join(e.current(), ProseDir)) {
			for _, ref := range ed.refs {
				if !exists(filepath.Join(e.prose, ref)) {
					add(filepath.Join(e.root, "engine/internal/render/pilean.go"), 0, "pi-prompt", fmt.Sprintf("edit %q names %s, which does not resolve", ed.name, ref))
				}
			}
		}
	}

	// 31d. The contracts read on demand (ondemand.go, T146 FA3): each moved
	// rule stays in its contract, out of the fixed context, and the
	// orchestrator of each runtime points to it.
	for _, m := range e.onDemandProblems() {
		add(filepath.Join(e.root, "engine/internal/render/ondemand.go"), 0, "on-demand", m)
	}

	// 31c. Writer mode (T117b): the shell splices the writer rules over the
	// marked region of pi's prompt, so the region and the file must exist.
	{
		orch := filepath.Join(e.prose, "orchestrator.md")
		for _, m := range delegationRegionProblems(pi{e}.promptText()) {
			add(orch, 0, "delegation-region", m)
		}
		writer := filepath.Join(e.prose, writerProse)
		if !exists(writer) {
			add(writer, 0, "delegation-region", "the writer-mode rules are missing")
		} else if m := runtimePlaceholder.FindString(inRuntime("pi", textOf(writer))); m != "" {
			add(writer, 0, "delegation-region", fmt.Sprintf("runtime term %s has no words for pi", m))
		}
	}

	// 32. The audit registry, and the runtime capability model (docket AG).
	e.auditRegistry(vocab, add)

	// 37. Safety (docket AH1).
	anchors(add, "safety", flat, []anchor{
		{"orchestrator.md", "`jhon hook safety-guard` refuses irreversible operations to every agent and to this thread",
			"that the safety guard refuses irreversible operations to every agent (\"The limits\")"},
		{"orchestrator.md", "runs once the owner approves it \\(class `destroy`\\); it asks for outward-facing ones",
			"that a deletion that loses work runs on the owner's approval, and outward-facing ones are asked (\"The limits\")"},
		{"orchestrator.md", `No agent reads, edits, or deletes a credentials file`,
			"that no agent touches a credentials file (\"The limits\")"},
		{"contracts/ldd.md", `Any agent that holds Bash may perform repository-state operations locally`,
			"that any agent with Bash may perform repository-state operations locally (section I)"},
		{"contracts/ldd.md", "`jhon hook safety-guard` refuses irreversible operations to every agent and the top-level session",
			"that the safety guard refuses irreversible operations to every agent and the top-level session (section I)"},
		{"contracts/ldd.md", "runs once the owner approves it: class `destroy`",
			"that a deletion that loses work runs on the owner's approval (section I)"},
		{"contracts/ldd.md", `It asks for the outward-facing ones`, "that the safety guard asks for the outward-facing operations (section I)"},
		{"contracts/ldd.md", `A runtime isolation refusal of a write is a stop as well\. Creating or selecting another worktree or root and making the refused write there is a bypass`,
			"that making the refused write in another tree after an isolation refusal is a bypass (section I)"},
		{"skills/ldd/references/apply.md", `A runtime isolation refusal of a write is a stop: report it verbatim and never create or select another worktree or root to make the write there`,
			"that an isolation refusal is a stop, never a reason to create or select another worktree (Rules)"},
	}, e.prose)

	return problems
}

// anchor is a sentence a file must state, as a pattern over its text with
// whitespace flattened.
type anchor struct{ file, pattern, what string }

// anchors reports each anchor its file does not state.
func anchors(add func(string, int, string, string), rule string, flat func(string) string, list []anchor, root string) {
	for _, a := range list {
		file := filepath.Join(root, a.file)
		if !regexp.MustCompile(a.pattern).MatchString(flat(file)) {
			add(file, 0, rule, "does not state "+a.what)
		}
	}
}

// agentProblems are one agent's manifest problems: its body or template,
// tier, effort, limits, and capabilities.
func (e *engine) agentProblems(a manifest.Agent, add func(rule, msg string), rel func(string) string) {
	switch {
	case a.Template != "":
		if a.Body != "" {
			add("manifest", a.Name+": declares both body and template")
		}
		if !exists(e.templatePath(a)) {
			add("manifest", fmt.Sprintf("%s: template %s is missing", a.Name, rel(e.templatePath(a))))
		} else {
			// A placeholder with no value ships as literal `{{name}}`
			// text; a value nothing uses is a body the author believes
			// says something.
			_, unknown, unused := expandTemplate(mustReadText(e.templatePath(a)), vars(a))
			for _, u := range unknown {
				add("agent-template", fmt.Sprintf("%s: template placeholder {{%s}} has no value", a.Name, u))
			}
			for _, u := range unused {
				add("agent-template", fmt.Sprintf("%s: var '%s' is not used by template %s", a.Name, u, a.Template))
			}
		}
	case a.Vars != nil:
		add("manifest", a.Name+": declares vars without a template")
	case !exists(e.bodyPath(a)):
		add("manifest", fmt.Sprintf("%s: body %s is missing", a.Name, rel(e.bodyPath(a))))
	}
	// A runtime term with no words for a runtime ships as literal
	// `{{runtime:<term>}}` text in that runtime's prompt.
	if a.Template != "" || exists(e.bodyPath(a)) {
		for _, m := range runtimePlaceholder.FindAllStringSubmatch(e.bodyOf(a), -1) {
			for _, r := range e.renderers() {
				if _, ok := runtimeTerms[m[1]][r.Runtime()]; !ok {
					add("agent-template", fmt.Sprintf("%s: runtime term {{runtime:%s}} has no words for %s", a.Name, m[1], r.Runtime()))
				}
			}
		}
	}
	for _, runtime := range e.m.Tiers.Keys() {
		if e.m.Tier(runtime, a.TierName()) == "" {
			add("manifest", fmt.Sprintf("%s: tier '%s' undefined for %s", a.Name, a.TierName(), runtime))
		}
	}
	if a.Effort != "" {
		t, ok := profiles.TierValue(e.m.Tier("claude-code", a.TierName()))
		if !slices.Contains(claudeEfforts, a.Effort) {
			add("manifest", fmt.Sprintf("%s: effort '%s' is not one of %s", a.Name, a.Effort, strings.Join(claudeEfforts, ", ")))
		} else if ok && t.Effort == "" {
			add("manifest", fmt.Sprintf("%s: tier %s (%s) takes no effort", a.Name, a.TierName(), t.Model))
		}
	}
	if a.MaxTurns != "" && !positiveInt.MatchString(a.MaxTurns) {
		add("manifest", fmt.Sprintf("%s: maxTurns '%s' is not a positive integer", a.Name, a.MaxTurns))
	}
	if a.OmitClaudeMd != "" && a.OmitClaudeMd != "true" {
		add("manifest", fmt.Sprintf("%s: omitClaudeMd '%s' must be true", a.Name, a.OmitClaudeMd))
	}
	if a.Bounded != "" && (a.Bounded != "true" || !e.canWrite(a)) {
		add("manifest", fmt.Sprintf("%s: bounded '%s' must be true, on an agent that writes", a.Name, a.Bounded))
	}
	for _, c := range a.Caps {
		if !e.m.Capabilities.Has(c) {
			add("manifest", fmt.Sprintf("%s: capability '%s' undefined", a.Name, c))
		}
	}
}

var positiveInt = lazyre.New(`^[1-9]\d*$`)

// Commands is the subcommands the jhon binary dispatches; internal/cli sets
// it, since it registers them.
var Commands func() []string

// jhonCommands reports a `jhon <subcommand>` named in prose (backticked, or
// at the start of a line in a fenced block) that jhon does not dispatch.
func (e *engine) jhonCommands(text *manifest.Map[string], add func(string, int, string, string)) {
	known := map[string]bool{}
	if Commands != nil {
		for _, name := range Commands() {
			known[name] = true
		}
	}
	if len(known) == 0 {
		add(filepath.Join(e.root, "engine/internal/cli"), 0, "jhon-command", "no command table found")
	}
	for _, file := range text.Keys() {
		src, _ := text.Get(file)
		fenced := false
		for i, line := range lines(src) {
			if fence.MatchString(line) {
				fenced = !fenced
				continue
			}
			var subs []string
			for _, m := range namedJhon.FindAllStringSubmatch(line, -1) {
				subs = append(subs, m[1])
			}
			if fenced {
				if m := runJhon.FindStringSubmatch(line); m != nil {
					subs = append(subs, m[1])
				}
			}
			for _, sub := range subs {
				if len(known) > 0 && !known[sub] {
					add(file, i+1, "jhon-command", fmt.Sprintf("'jhon %s' is not a subcommand jhon dispatches", sub))
				}
			}
		}
	}
}

// judgmentProblems holds contracts/judgment.md's lists to one another, and
// the judgment-day skill's closing verdicts to them.
func (e *engine) judgmentProblems(text *manifest.Map[string], add func(string, int, string, string)) {
	jFile := filepath.Join(e.prose, "contracts/judgment.md")
	src, _ := text.Get(jFile)
	bad := func(msg string) { add(jFile, 0, "judgment-vocab", msg) }
	list := func(k string) []string { return contract.List(src, k) }
	modes, events := list("judgment_modes"), list("judgment_events")
	verdictKeys, verdicts := contract.Map(src, "judgment_verdicts")
	limitKeys, limits := contract.Map(src, "judgment_round_limit")
	for _, k := range []string{"judgment_modes", "judgment_judges", "judgment_events", "judgment_positions", "judgment_point_status",
		"judgment_human_outcomes", "judgment_severities", "judgment_severe", "judgment_next_action", "judgment_escalation_reason"} {
		if len(list(k)) == 0 {
			bad(k + " is empty")
		}
	}
	if !sameSet(verdictKeys, modes) {
		bad(fmt.Sprintf("judgment_verdicts covers {%s}, not the modes {%s}", strings.Join(verdictKeys, ", "), strings.Join(modes, ", ")))
	}
	if !sameSet(limitKeys, modes) {
		bad(fmt.Sprintf("judgment_round_limit covers {%s}, not the modes {%s}", strings.Join(limitKeys, ", "), strings.Join(modes, ", ")))
	}
	for _, m := range verdictKeys {
		if len(contract.InlineList(verdicts[m])) != 2 {
			bad(fmt.Sprintf("judgment_verdicts.%s must list two verdicts, consensus then escalation", m))
		}
	}
	for _, m := range limitKeys {
		if !positiveInt.MatchString(limits[m]) {
			bad(fmt.Sprintf("judgment_round_limit.%s is '%s', not a positive integer", m, limits[m]))
		}
	}
	positions := list("judgment_positions")
	byRole := unique(append(list("judgment_owner_positions"), list("judgment_opponent_positions")...))
	if !sameSet(byRole, positions) {
		bad(fmt.Sprintf("owner and opponent positions {%s} are not judgment_positions {%s}", strings.Join(byRole, ", "), strings.Join(positions, ", ")))
	}
	for _, o := range list("judgment_human_outcomes") {
		if !slices.Contains(list("judgment_point_status"), o) {
			bad(fmt.Sprintf("judgment_human_outcomes names '%s', which is not a judgment_point_status", o))
		}
	}
	severities := list("judgment_severities")
	for _, s := range list("judgment_severe") {
		if !slices.Contains(severities, s) {
			bad(fmt.Sprintf("judgment_severe names '%s', which is not a judgment_severity", s))
		}
	}
	if !slices.ContainsFunc(events, func(e string) bool { return strings.Contains(e, "<n>") }) {
		bad("judgment_events declares no R<n> round template")
	}

	// The skill's closing line names a verdict; one the contract does not
	// declare would be refused only at CLOSE, mid-review.
	var all []string
	for _, m := range verdictKeys {
		all = append(all, contract.InlineList(verdicts[m])...)
	}
	skill := filepath.Join(e.prose, "skills/judgment-day/SKILL.md")
	skillText, _ := text.Get(skill)
	for i, line := range lines(skillText) {
		for _, m := range judgmentLine.FindAllStringSubmatch(line, -1) {
			if !slices.Contains(all, m[1]) {
				add(skill, i+1, "judgment-vocab", fmt.Sprintf("'JUDGMENT: %s' is not a verdict contracts/judgment.md declares", m[1]))
			}
		}
	}
}

// auditRegistry holds `audit_rules` to the behavioural anchors and the
// audit's checks, and the runtime capability model to its rules.
func (e *engine) auditRegistry(vocab *vocabulary, add func(string, int, string, string)) {
	classes := []string{"deterministic", "heuristic"}
	rules := vocab.auditRules
	reg := func(msg string) { add(vocab.file, 0, "audit-registry", msg) }
	if len(rules) == 0 {
		reg("declares no audit_rules")
	}
	var named, seen []string
	for _, r := range rules {
		anchor, hasAnchor := r["anchor"]
		a := anchor
		if !hasAnchor {
			a = "(no anchor)"
		}
		if anchor == "" {
			reg("an audit_rules entry has no anchor")
		} else if !slices.Contains(behaviouralAnchors, anchor) {
			reg(a + " is not a behavioural anchor the renderer enumerates (audit-registry)")
		}
		if anchor != "" && slices.Contains(seen, anchor) {
			reg(a + " has more than one audit_rules entry")
		}
		if anchor != "" {
			seen = append(seen, anchor)
		}
		check := r["check"]
		if check == "" {
			reg(a + ": entry has no check")
			continue
		}
		if check == "none" {
			if r["reason"] == "" {
				reg(a + ": check none needs a reason")
			}
			continue
		}
		// One check, or an inline list of the forms that read one rule.
		checks := contract.InlineList(check)
		if checks == nil {
			checks = []string{check}
		}
		for _, c := range checks {
			named = append(named, c)
			if !slices.Contains(auditcheck.Names, c) {
				reg(fmt.Sprintf("%s: %s is not a check internal/audit exports", a, c))
			}
		}
		// pending_on names the runtimes where a verified rule's check has no
		// real fixtures yet: there it reports a would-be violation.
		if p, ok := r["pending_on"]; ok {
			runtimes := contract.InlineList(p)
			if len(runtimes) == 0 || slices.ContainsFunc(runtimes, func(rt string) bool { return !slices.Contains(e.m.Matrix.Runtimes, rt) }) {
				reg(fmt.Sprintf("%s: pending_on must list runtimes agents.yaml declares, found '%s'", a, p))
			}
		}
		if !slices.Contains(classes, r["evidence_class"]) {
			reg(fmt.Sprintf("%s: evidence_class must be deterministic or heuristic, found '%s'", a, r["evidence_class"]))
		}
		statuses := strings.Join(vocab.auditValidationStatus, "|")
		if r["evidence_class"] == "deterministic" {
			if r["validation_status"] == "" {
				reg(fmt.Sprintf("%s: a deterministic check declares validation_status (%s)", a, statuses))
			} else if !slices.Contains(vocab.auditValidationStatus, r["validation_status"]) {
				reg(fmt.Sprintf("%s: validation_status must be one of %s, found '%s'", a, statuses, r["validation_status"]))
			}
		}
		_, positive := r["real_positive"]
		_, negative := r["real_negative"]
		if positive || negative {
			reg(a + ": real_positive and real_negative are renamed real_clean and real_violation")
		}
		clean, violation := contract.InlineList(r["real_clean"]), contract.InlineList(r["real_violation"])
		if r["validation_status"] == "verified" && (len(clean) == 0 || len(violation) == 0) {
			reg(a + ": verified needs real_clean and real_violation fixtures")
		}
		for _, dir := range append(clean, violation...) {
			if e.inRelease() {
				break // the fixtures are the checkout's evidence (T10f)
			}
			at := filepath.Join(e.root, "test/testdata/audit", dir)
			if fi, err := os.Stat(at); !fixtureName.MatchString(dir) || err != nil || !fi.IsDir() {
				reg(fmt.Sprintf("%s: %s is not a fixture directory under test/testdata/audit", a, dir))
				continue
			}
			if !realFixture(at) {
				reg(fmt.Sprintf(`%s: %s is not marked real (source.json): it needs "real": true and the session of its main transcript`, a, dir))
			}
		}
	}
	for _, anchor := range behaviouralAnchors {
		if !slices.Contains(seen, anchor) {
			reg(anchor + " has no audit_rules entry")
		}
	}
	for _, c := range auditcheck.Names {
		if !slices.Contains(named, c) {
			reg(c + " is exported by internal/audit and named by no entry")
		}
	}

	// Docket AG: every anchor declares a guarantee level for every runtime,
	// and the primitive matrix backs each one. The level names are the
	// vocabulary's, in this order, because the rules read them by position.
	levels := []string{"enforced", "behavioural", "unsupported"}
	verification := []string{"verified", "documented", "unknown"}
	if j := strings.Join(vocab.guaranteeLevels, ", "); j != strings.Join(levels, ", ") {
		add(vocab.file, 0, "runtime-guarantee", fmt.Sprintf("guarantee_levels must be [%s], found [%s]", strings.Join(levels, ", "), j))
	}
	if j := strings.Join(vocab.primitiveVerification, ", "); j != strings.Join(verification, ", ") {
		add(vocab.file, 0, "runtime-matrix", fmt.Sprintf("primitive_verification must be [%s], found [%s]", strings.Join(verification, ", "), j))
	}
	if len(vocab.spikeRecordFields) == 0 {
		add(vocab.file, 0, "runtime-matrix", "spike_record_fields is not declared")
	}
	manifestFile := filepath.Join(e.prose, "agents.yaml")
	for _, p := range e.matrixProblems() {
		add(manifestFile, 0, p.Rule, p.Msg)
	}
	// A runtime this render projects never receives a configuration that
	// does not fit: its inline bundle is held to its budget here.
	for _, rt := range enforcementProjections.Keys() {
		if why := manifest.BudgetProblem(e.m.Matrix, rt, len(e.alwaysOnBundle(rt))); why != "" {
			add(manifestFile, 0, "runtime-matrix", rt+": "+why)
		}
	}
}

// matrixProblems are the capability model's problems against the
// vocabulary, the audit rules, and the projections.
func (e *engine) matrixProblems() []manifest.Finding {
	vocab := e.vocabulary()
	return manifest.MatrixProblems(e.m.Matrix, manifest.MatrixContext{
		Levels: []string{"enforced", "behavioural", "unsupported"}, Verifications: []string{"verified", "documented", "unknown"},
		Anchors: behaviouralAnchors, AuditRules: vocab.auditRules, Projections: enforcementProjections,
		// A release ships without the evidence under test/ (T10f): the
		// checkout judged it; the release takes a cited path for the kind
		// its shape names.
		Exists: func(p string) bool { return e.inRelease() || exists(joinPath(e.root, p)) },
		Evidence: func(p string, c manifest.Claim) manifest.Evidence {
			if e.inRelease() && !exists(joinPath(e.root, p)) {
				switch {
				case spikeRecord.MatchString(p):
					return manifest.Evidence{Kind: "spike"}
				case auditFixture.MatchString(p):
					return manifest.Evidence{Kind: "real-session"}
				}
				return manifest.Evidence{}
			}
			return e.evidenceKind(p, vocab.spikeRecordFields, c)
		},
		RefusalPrimitives: refusalPrimitives,
	})
}

// realFixture is whether a fixture directory's source.json says real and
// names the session of its main transcript.
func realFixture(dir string) bool {
	var source any = js.Undefined{}
	if b, err := os.ReadFile(filepath.Join(dir, "source.json")); err == nil {
		if v, err := js.ParseJSON(js.DecodeUTF8(b)); err == nil {
			source = v
		}
	}
	main := ""
	for _, f := range readDir(dir) {
		if strings.HasSuffix(f.Name(), ".jsonl") {
			main = f.Name()
			break
		}
	}
	session, ok := js.Prop(source, "session").(string)
	return js.Prop(source, "real") == true && main != "" && ok && session == strings.TrimSuffix(main, ".jsonl")
}

var (
	spikeRecord    = lazyre.New(`^test/testdata/spikes/[\w.-]+/record\.json$`)
	auditFixture   = lazyre.New(`^test/testdata/audit/([\w.-]+)/?$`)
	guardMechanism = lazyre.New(`^hooks/[\w-]+-guard\.mjs$`)
)

// evidenceKind is what a path cited by a `verified` primitive proves: a
// spike record holding every `spike_record_fields` field and corresponding
// to the claim that cites it (docket AR: a filled form is not a proof), or
// a real-session fixture. Anything else, a unit test included, proves
// nothing about the runtime.
func (e *engine) evidenceKind(rel string, fields []string, claim manifest.Claim) manifest.Evidence {
	at := joinPath(e.root, rel)
	if spikeRecord.MatchString(rel) {
		b, err := os.ReadFile(at)
		var record any
		if err == nil {
			record, err = js.ParseJSON(js.DecodeUTF8(b))
		}
		o, ok := record.(*js.Object)
		if err != nil || !ok {
			return manifest.Evidence{Invalid: "is not a JSON object"}
		}
		var missing []string
		for _, f := range fields {
			if emptyValue(js.Prop(o, f)) {
				missing = append(missing, f)
			}
		}
		if len(missing) > 0 {
			return manifest.Evidence{Invalid: "lacks " + strings.Join(missing, ", ")}
		}
		guard := js.String(js.Prop(o, "guard"))
		// A record pins the guard's version, so a guard since removed still
		// evidences the runtime primitive it exercised.
		mechanism := guard == claim.Primitive || (claim.Primitive == "blocking-hook-agent" && guardMechanism.MatchString(guard))
		var wrong []string
		if rt := js.Prop(o, "runtime"); rt != any(claim.Runtime) {
			wrong = append(wrong, fmt.Sprintf("runtime %s is not %s", js.String(rt), claim.Runtime))
		}
		if v := js.Prop(o, "runtime_version"); v != any(claim.Version) {
			wrong = append(wrong, fmt.Sprintf("runtime_version %s is not %s", js.String(v), claim.Version))
		}
		if !mechanism {
			wrong = append(wrong, fmt.Sprintf("guard %s is not a mechanism of %s", guard, claim.Primitive))
		}
		if len(wrong) > 0 {
			return manifest.Evidence{Invalid: "does not correspond to the claim: " + strings.Join(wrong, "; ")}
		}
		return manifest.Evidence{Kind: "spike"}
	}
	if auditFixture.MatchString(rel) {
		if fi, err := os.Stat(at); err == nil && fi.IsDir() && realFixture(at) {
			return manifest.Evidence{Kind: "real-session"}
		}
	}
	return manifest.Evidence{}
}

// emptyValue is a record field a spike leaves unfilled: absent, null, "",
// or an object or list with nothing in it.
func emptyValue(v any) bool {
	switch x := v.(type) {
	case nil, js.Undefined:
		return true
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	case *js.Object:
		return len(x.Keys()) == 0
	}
	return false
}

// delegationRegionProblems is why text has no single delegation region: each
// marker exactly once, the begin before the end.
func delegationRegionProblems(text string) []string {
	var out []string
	for _, m := range []string{delegationBegin, delegationEnd} {
		if n := strings.Count(text, m); n != 1 {
			out = append(out, fmt.Sprintf("%s appears %d times, want 1", m, n))
		}
	}
	if out == nil && strings.Index(text, delegationEnd) < strings.Index(text, delegationBegin) {
		out = append(out, delegationEnd+" comes before "+delegationBegin)
	}
	return out
}

// integrationProblems are the tree's integration manifests' problems
// (integrations/<name>/manifest.yaml, T58a): each malformed one, a set the
// engine cannot use (integrations.Assumed), and a capability its manifest
// grants that agents.yaml does not declare, or whose Claude Code tools are
// not its servers'. A tree without integrations/ (a root older than v0.3.3,
// or one that does not ship it) has the compiled-in manifests judged
// instead (embeddedIntegrations).
func (e *engine) integrationProblems() []integrations.Problem {
	var ms []integrations.Manifest
	var probs []integrations.Problem
	if e.embeddedIntegrations() {
		ms, probs = integrations.Embedded()
	} else {
		ms, probs = integrations.Load(os.DirFS(filepath.Join(e.root, "integrations")))
		probs = append(probs, integrations.Assumed(ms)...)
	}
	for _, m := range ms {
		for _, c := range m.Capabilities {
			list, ok := e.m.Capabilities.Get(c)
			if !ok {
				probs = append(probs, integrations.Problem{File: m.File, Field: "capabilities", Msg: c + " is not a capability of agents.yaml"})
				continue
			}
			for _, t := range list {
				if !m.ClaudeTool(t) {
					probs = append(probs, integrations.Problem{File: m.File, Field: "capabilities", Msg: c + " holds " + t + ", which is not a tool of claude-code.servers"})
				}
			}
		}
	}
	return probs
}

// embeddedIntegrations is whether the tree has no integrations/ folder, so
// validation judges the manifests compiled into jhon (L1159, F5).
func (e *engine) embeddedIntegrations() bool {
	_, err := os.Stat(filepath.Join(e.root, "integrations"))
	return errors.Is(err, fs.ErrNotExist)
}
