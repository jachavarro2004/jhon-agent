package hook

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	goio "io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// lostAgentNotice is told to the owner when the session runs no agent:
// Claude Code's /clear relaunches a session without --agent, keeping only
// the plugin and the name.
const lostAgentNotice = "Jhon: this session was started without --agent (Claude Code's /clear relaunches without it); " +
	"the Jhon plugin loads the agent's definition and runs its guards."

// bindingLead says the agent's definition binds the session, whatever the
// runtime's own system prompt says.
const bindingLead = "Jhon runs this session as the %s agent. Its definition is binding: " +
	"it says how this session works, whatever the session's own system prompt says."

// definitionLead heads each part of the agent's definition SessionStart
// hands the session.
const definitionLead = bindingLead + " Part %d of %d:\n\n"

// SessionParts are the plugin's SessionStart entries, `--part 1` to
// `--part SessionParts`; partLimit keeps each part under the 10,000
// characters Claude Code shows of a hook's context before it saves the rest
// to a file and shows a preview.
const (
	SessionParts = 4
	partLimit    = 9000
)

// AgentLoadedEnv names the agent whose definition the launch made the
// session's system prompt: jhon start sets it on a print-mode Claude Code
// launch, and the hook process inherits it.
const AgentLoadedEnv = "JHON_AGENT_LOADED"

func refuseUsage(io IO) int {
	fmt.Fprintln(io.Err, "agent-hooks: usage: agent-hooks <default agent> [--part <1-"+strconv.Itoa(SessionParts)+">]; the plugin is broken")
	return 2
}

// sectionsOf is text cut before each line that opens a `## ` heading.
func sectionsOf(text string) []string {
	var out []string
	// No text has no section: an empty definition, or one that is all
	// frontmatter.
	if text == "" {
		return nil
	}
	for {
		i := strings.Index(text[1:], "\n## ")
		if i < 0 {
			return append(out, text)
		}
		out, text = append(out, text[:i+2]), text[i+2:]
	}
}

// Parts are text's `## ` sections packed in order into at most
// SessionParts parts of at most limit bytes; a section longer than limit is
// split at a line, and the last part takes whatever is left.
func Parts(text string, limit int) []string {
	var sections []string
	for _, s := range sectionsOf(text) {
		for len(s) > limit {
			cut := strings.LastIndex(s[:limit], "\n") + 1
			if cut <= 0 {
				cut = limit
			}
			sections, s = append(sections, s[:cut]), s[cut:]
		}
		sections = append(sections, s)
	}
	var parts []string
	var cur strings.Builder
	for _, s := range sections {
		if cur.Len() > 0 && cur.Len()+len(s) > limit && len(parts) < SessionParts-1 {
			parts, cur = append(parts, cur.String()), strings.Builder{}
		}
		cur.WriteString(s)
	}
	if strings.TrimSpace(cur.String()) != "" {
		parts = append(parts, cur.String())
	}
	return parts
}

// agent-hooks runs the other handlers, so it joins Handlers after them.
func init() {
	Handlers["agent-hooks"] = AgentHooks
	Handlers["session-brief"] = SessionBrief
}

// SessionBrief is what pi's main session receives beside its agent's
// definition, as Claude Code's does at SessionStart (`session-brief <agent>`,
// payload {"cwd"}): {"lead","brief"}, the binding lead and the project's
// rules (ldd.ProjectBrief). Where LDD is not set up yet (no LDD config) it
// is {"inactive"}, ldd.InactiveFor. A project the retired `jhon ldd off`
// left an off record for has ldd.OldSwitchNotice after either, once.
func SessionBrief(args []string, io IO) int {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		fmt.Fprintln(io.Err, "session-brief: usage: session-brief <agent>")
		return 2
	}
	p, ok := read(io.In)
	if !ok {
		return 0
	}
	brief := ldd.ProjectBrief(p.Cwd())
	notice := oldSwitchNotice(p.Cwd())
	if brief == "" {
		fmt.Fprint(io.Out, marshal(map[string]string{"inactive": ldd.InactiveFor(p.Cwd()) + notice(" "), "context": ContextLean}))
		return 0
	}
	fmt.Fprint(io.Out, marshal(map[string]string{"lead": fmt.Sprintf(bindingLead, strings.TrimSpace(args[0])), "brief": brief + notice("\n\n"), "context": startContext(p.Cwd())}))
	return 0
}

// The context a main session starts with (T146h T2): lean, the lean text
// alone, when no change is active or the active one is small; full, today's
// orchestrator and always-on layer, when the active change is not small.
const (
	ContextLean = "lean"
	ContextFull = "full"
)

// startContext is the context a session working in dir starts with.
func startContext(dir string) string {
	if ldd.NotSmall(dir) {
		return ContextFull
	}
	return ContextLean
}

// fullMark is the file that records a session was given, or pointed to,
// the full context (T146h T4): one pointer per session at most.
func fullMark(session string) string {
	sess := regexp.MustCompile(`[^\w.-]`).ReplaceAllString(session, "")
	if sess == "" {
		return ""
	}
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" || !filepath.IsAbs(cache) {
		cache = filepath.Join(project.Home(), ".cache")
	}
	return filepath.Join(cache, "jhon", "full-context", sess)
}

// hasFull is whether session was given, or pointed to, the full context.
func hasFull(session string) bool {
	mark := fullMark(session)
	if mark == "" {
		return false
	}
	_, err := os.Stat(mark)
	return err == nil
}

// markFull records that session has the full context; whether it had it
// already.
func markFull(session string) bool {
	mark := fullMark(session)
	if mark == "" {
		return false
	}
	if _, err := os.Stat(mark); err == nil {
		return true
	}
	if os.MkdirAll(filepath.Dir(mark), 0o700) == nil {
		_ = os.WriteFile(mark, nil, 0o600)
	}
	return false
}

// mcpStatusTools are the jhon MCP server's status tool (`jhon status
// --json`) as Claude Code names it: from the plugin's server, and from a
// server registered by hand (T192 S5).
var mcpStatusTools = map[string]bool{"mcp__plugin_jhon_jhon__status": true, "mcp__jhon__status": true}

// changesFolder is a layout's changes folder in a path: openspec/changes/ or
// ldd/changes/ (internal/ldd/layout).
var changesFolder = regexp.MustCompile(`(^|/)(openspec|ldd)/changes/`)

// changeCommand is a Bash command that may make or select a change: a git
// branch switch, a jhon run or risk read, or one naming the changes folder.
var changeCommand = regexp.MustCompile(`\bgit\s+(switch|checkout)\b|(^|[;&|(]\s*|\s)jhon\s+(executions|risk)\b|(^|[\s'"=/])(openspec|ldd)/changes/`)

// changeMoment is, for a call that may have made or selected a change
// (T192 S7), the directory whose repository to ask: a Write or Edit under
// the changes folder (the file's directory), or a changeCommand (the
// session's); "" for any other call.
func changeMoment(p Payload) string {
	in := p.Obj("tool_input")
	switch p.Str("tool_name") {
	case "Write", "Edit", "MultiEdit":
		file, _ := in["file_path"].(string)
		if file == "" || !changesFolder.MatchString(filepath.ToSlash(file)) {
			return ""
		}
		if !filepath.IsAbs(file) {
			file = filepath.Join(p.Cwd(), file)
		}
		return filepath.Dir(file)
	case "Bash":
		if cmd, _ := in["command"].(string); changeCommand.MatchString(cmd) {
			return p.Cwd()
		}
	}
	return ""
}

// fullPointer is PostToolUse's pointer to the full context (T146h T4), for
// the main agent's call that shows the work is not small: `jhon status`
// printing its not-small line, through Bash or the jhon MCP server's status
// tool (T192 S5), a sub-agent launch, or a call that made or selected a
// change (changeMoment) when the engine then finds it not small (T192 S7,
// ldd.NotSmall, the reading SessionStart starts full on). Once per session;
// "" for any other call, or a session that has the full context already.
// The full text exceeds the 10,000 characters a hook's context shows, so
// the pointer names the file.
func fullPointer(p Payload, home string) string {
	tool := p.Str("tool_name")
	notSmall := func() bool {
		resp, _ := json.Marshal(p["tool_response"])
		return strings.Contains(string(resp), "not small: read ")
	}
	cmd, _ := p.Obj("tool_input")["command"].(string)
	trigger := ""
	switch {
	case tool == "Agent" || tool == "Task":
		trigger = "before a sub-agent"
	case tool == "Bash" && strings.Contains(cmd, "jhon status") && notSmall():
		trigger = "this change is not small"
	case mcpStatusTools[tool]:
		if !notSmall() {
			return ""
		}
		trigger = "this change is not small"
	default:
		// Asked only at such a moment, and only while the session has no pointer yet.
		dir := changeMoment(p)
		if dir == "" || hasFull(p.Str("session_id")) || !ldd.NotSmall(dir) {
			return ""
		}
		trigger = "this change is not small"
	}
	if markFull(p.Str("session_id")) {
		return ""
	}
	return "Jhon: " + trigger + ": read the full context, " + ldd.FullContextFile(home, "claude-code") + ", before the next edit; it binds as the lean text does."
}

// oldSwitchNotice is, for a session in dir still to be told the retired
// `jhon ldd off` no longer applies (T72, L555), a function that returns
// ldd.OldSwitchNotice after sep and records it told; elsewhere one that
// returns "". Only the call that prints the notice may call it.
func oldSwitchNotice(dir string) func(sep string) string {
	top, untold := ldd.OldSwitchUntold(dir)
	return func(sep string) string {
		if !untold {
			return ""
		}
		_ = project.TellOldSwitchOff(top)
		return sep + ldd.OldSwitchNotice
	}
}

// agentHook is one hook an agent's frontmatter renders: its event, its
// matcher ("" takes every tool), and its command.
type agentHook struct{ event, matcher, command string }

// AgentFiles are the two files of the home's Claude Code agents folder that
// `jhon doctor` looks for: the record a render keeps of the agents it wrote
// (render: agentsRecord), and the definition of main, the session's main
// agent, which agent-hooks reads its guards from.
func AgentFiles(home, main string) (record, definition string) {
	dir := filepath.Join(home, ".claude", "agents")
	return filepath.Join(dir, ".jhon-owned"), filepath.Join(dir, main+".md")
}

// What DefinitionProblem says of an agent's definition.
const (
	DefinitionMissing    = "is missing"
	DefinitionUnreadable = "cannot be read"
	DefinitionEmpty      = "is empty"
)

// DefinitionProblem is why the agent definition file gives a session no
// guard, or "": it is missing, it cannot be read, or it holds nothing.
// `jhon doctor` has a row for each; no call is refused for it.
func DefinitionProblem(file string) string {
	data, err := os.ReadFile(file)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return DefinitionMissing
	case err != nil:
		return DefinitionUnreadable
	case len(bytes.TrimSpace(data)) == 0:
		return DefinitionEmpty
	}
	return ""
}

// AgentHooks is the Claude Code plugin's hook for every event (`agent-hooks
// <default agent>`): it runs the hooks the calling agent's definition under
// ~/.claude/agents renders, for the payload's event and tool. Claude Code
// 2.1.310 no longer runs an agent's frontmatter hooks, main or sub-agent,
// while a plugin's hooks run for both with the caller's agent_type. A
// payload with no agent_type is a session that lost its agent, run as the
// default agent. At SessionStart the session is handed its agent's
// definition, so the framework, not the runtime, sets how it works.
func AgentHooks(args []string, io IO) int {
	part := 0
	if len(args) == 3 && args[1] == "--part" {
		n, err := strconv.Atoi(args[2])
		if err != nil || n < 1 || n > SessionParts {
			return refuseUsage(io)
		}
		part, args = n, args[:1]
	}
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return refuseUsage(io)
	}
	payload, err := goio.ReadAll(io.In)
	if err != nil {
		return 0
	}
	var p Payload
	if json.Unmarshal(payload, &p) != nil {
		return 0
	}
	event, agent := p.Str("hook_event_name"), strings.TrimSpace(p.Str("agent_type"))
	lost := agent == ""
	if lost {
		agent = strings.TrimSpace(args[0])
	}
	if strings.ContainsAny(agent, "/\\") {
		return 0
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return 0
	}
	hooks, err := readAgentHooks(filepath.Join(home, ".claude", "agents", agent+".md"))
	if err != nil {
		// An agent Jhon does not render (Explore, general-purpose) has no hooks.
		return 0
	}
	// SessionStart, at startup, /clear, resume, and compaction alike, hands
	// the session its agent's definition: Claude Code 2.1.310 does not load
	// it in an interactive session even with --agent. A fresh start whose
	// launch already made it the system prompt (AgentLoadedEnv naming the
	// agent) takes only what the prompt lacks: the shell note and the
	// project's rules, in part 1.
	if event == "SessionStart" {
		body, err := agentBody(filepath.Join(home, ".claude", "agents", agent+".md"))
		// T146h T2: the definition is the lean text; a change that is not
		// small starts with the install's full context instead.
		full := false
		if err == nil && agent == strings.TrimSpace(args[0]) && ldd.NotSmall(p.Cwd()) {
			if b, ferr := os.ReadFile(ldd.FullContextFile(home, "claude-code")); ferr == nil && len(bytes.TrimSpace(b)) > 0 {
				body, full = string(b), true
				markFull(p.Str("session_id"))
			}
		}
		if !lost && !full && p.Str("source") == "startup" && os.Getenv(AgentLoadedEnv) == agent {
			var parts []string
			if note := OwnerShellNote(os.Getenv("SHELL")); note != "" {
				parts = append(parts, note)
			}
			if brief := ldd.ProjectBrief(p.Cwd()); brief != "" && err == nil {
				parts = append(parts, brief)
			} else if err == nil {
				parts = append(parts, ldd.InactiveFor(p.Cwd()))
			}
			if err != nil || part != 1 || len(parts) == 0 {
				return 0
			}
			parts[len(parts)-1] += oldSwitchNotice(p.Cwd())("\n\n")
			fmt.Fprint(io.Out, marshal(map[string]any{"hookSpecificOutput": map[string]string{"hookEventName": "SessionStart",
				"additionalContext": fmt.Sprintf(bindingLead, agent) + "\n\n" + strings.Join(parts, "\n\n")}}))
			return 0
		}
		parts := Parts(body, partLimit)
		// The owner's shell, which a command handed to the owner is written
		// for, rides in part 1: it adds no part of its own.
		if note := OwnerShellNote(os.Getenv("SHELL")); note != "" && len(parts) > 0 {
			parts[0] = strings.TrimRight(parts[0], "\n") + "\n\n" + note
		}
		// The project's own rules, read now, are the last part; where LDD is
		// inactive, the last part ends with the line that says so.
		brief := ldd.ProjectBrief(p.Cwd())
		if brief != "" && err == nil {
			if len(parts) < SessionParts {
				parts = append(parts, brief)
			} else {
				parts[len(parts)-1] += "\n\n" + brief
			}
		} else if err == nil && len(parts) > 0 {
			parts[len(parts)-1] = strings.TrimRight(parts[len(parts)-1], "\n") + "\n\n" + ldd.InactiveFor(p.Cwd())
		}
		if err != nil || part == 0 || part > len(parts) {
			return 0
		}
		// The retired switch's notice ends the last part, told by its call only.
		if part == len(parts) {
			if n := oldSwitchNotice(p.Cwd())("\n\n"); n != "" {
				parts[part-1] = strings.TrimRight(parts[part-1], "\n") + n
			}
		}
		out := map[string]any{"hookSpecificOutput": map[string]string{"hookEventName": "SessionStart",
			"additionalContext": fmt.Sprintf(definitionLead, agent, part, len(parts)) + parts[part-1]}}
		if lost && part == 1 {
			out["systemMessage"] = lostAgentNotice
		}
		fmt.Fprint(io.Out, marshal(out))
		return 0
	}
	// Each SessionStart part is its own entry; the other events take none.
	if part != 0 {
		return 0
	}
	if event == "PostToolUse" && agent == strings.TrimSpace(args[0]) {
		if pointer := fullPointer(p, home); pointer != "" {
			fmt.Fprint(io.Out, marshal(map[string]any{"hookSpecificOutput": map[string]string{"hookEventName": "PostToolUse", "additionalContext": pointer}}))
		}
	}
	tool := p.Str("tool_name")
	for _, h := range hooks {
		if h.event != event || !takes(event, h.matcher, tool) {
			continue
		}
		words, ok := shellWords(h.command)
		if !ok || len(words) < 3 || words[1] != "hook" || Handlers[words[2]] == nil {
			fmt.Fprintf(io.Err, "agent-hooks: %s's %s hook is not a jhon hook: %s; the render is broken\n", agent, event, h.command)
			return 2
		}
		if code := Handlers[words[2]](words[3:], IO{bytes.NewReader(payload), io.Out, io.Err}); code != 0 {
			return code
		}
	}
	return 0
}

// agentBody is an agent definition's text after its frontmatter.
func agentBody(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := string(data)
	if !strings.HasPrefix(text, "---\n") {
		return text, nil
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return "", nil
	}
	return text[4+end+5:], nil
}

// takes is whether an agent's hook for event runs for tool: its matcher
// takes the tool, or, as Claude Code runs a PreToolUse hook that takes Bash
// for the command a Monitor call starts, it takes Bash and the tool is
// Monitor. The plugin's own entry has no matcher, so the payload is the
// Monitor call's, where updatedInput applies.
func takes(event, matcher, tool string) bool {
	return matches(matcher, tool) || event == "PreToolUse" && tool == "Monitor" && matches(matcher, "Bash")
}

// matches is whether a Claude Code matcher takes tool: "" or "*" takes
// every tool, otherwise the matcher is a whole-name regular expression.
func matches(matcher, tool string) bool {
	if matcher == "" || matcher == "*" {
		return true
	}
	re, err := regexp.Compile("^(?:" + matcher + ")$")
	return err == nil && re.MatchString(tool)
}

// readAgentHooks reads the hooks of an agent definition's frontmatter, in
// the shape render/claude.go writes: `hooks:`, an event per two-space key,
// a `- matcher:` or a bare `- hooks:` per entry, and a JSON-quoted
// `command:` per hook.
func readAgentHooks(path string) ([]agentHook, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []agentHook
	in, fences := false, 0
	event, matcher := "", ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "---" {
			if fences++; fences == 2 {
				break
			}
			continue
		}
		if fences != 1 {
			continue
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case line == "hooks:":
			in = true
		case !strings.HasPrefix(line, " "):
			in = false
		case !in:
		case strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") && strings.HasSuffix(trimmed, ":"):
			event, matcher = strings.TrimSuffix(trimmed, ":"), ""
		case strings.HasPrefix(trimmed, "- matcher:"):
			matcher = unquote(strings.TrimSpace(strings.TrimPrefix(trimmed, "- matcher:")))
		case trimmed == "- hooks:":
			matcher = ""
		case strings.HasPrefix(trimmed, "command:"):
			out = append(out, agentHook{event, matcher, unquote(strings.TrimSpace(strings.TrimPrefix(trimmed, "command:")))})
		}
	}
	return out, sc.Err()
}

// unquote is a JSON-quoted YAML scalar's text, or the scalar itself.
func unquote(s string) string {
	var v string
	if json.Unmarshal([]byte(s), &v) == nil {
		return v
	}
	return s
}

// shellWords splits a command render.command quoted: words separated by
// spaces, each single-quoted with `'\”` for a quote, or bare.
func shellWords(s string) ([]string, bool) {
	var words []string
	var w strings.Builder
	inWord, quoted := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quoted && c == '\'':
			quoted = false
		case quoted:
			w.WriteByte(c)
		case c == '\'':
			quoted, inWord = true, true
		case c == '\\' && i+1 < len(s):
			i++
			w.WriteByte(s[i])
			inWord = true
		case c == ' ':
			if inWord {
				words = append(words, w.String())
				w.Reset()
				inWord = false
			}
		default:
			w.WriteByte(c)
			inWord = true
		}
	}
	if quoted {
		return nil, false
	}
	if inWord {
		words = append(words, w.String())
	}
	return words, true
}

// OwnerShellNote is the line that tells an orchestrator the owner's shell,
// read from SHELL, when it is not a POSIX one: a command it hands the owner
// to run by hand or with `!` is written for it. "" for bash, zsh, sh, or none.
func OwnerShellNote(shell string) string {
	switch filepath.Base(shell) {
	case "fish":
		return "- The owner's shell is fish. A command you hand the owner to run (by hand or with `!`) is fish: `set -x NAME value` or `env NAME=value cmd`, never `export`; `(cmd)` or `$(cmd)`; no heredocs, `[[ ]]`, or `for x in ...; do ...; done` (fish: `for x in ...; ...; end`). Your own Bash tool stays POSIX."
	case "nu":
		return "- The owner's shell is nushell. A command you hand the owner to run (by hand or with `!`) is written for nu, or wrapped in `bash -c '...'`. Your own Bash tool stays POSIX."
	}
	return ""
}
