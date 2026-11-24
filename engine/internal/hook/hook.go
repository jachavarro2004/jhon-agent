// Package hook holds the hook handlers: each reads a runtime's payload on
// stdin, decides, and answers with an exit code, stderr, or JSON on stdout,
// as the runtime's hook protocol reads it.
package hook

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/rename"
	"github.com/jachavarro2004/jhon-agent/engine/internal/session"
)

// IO is what a handler reads and writes.
type IO struct {
	In       io.Reader
	Out, Err io.Writer
}

// A Handler runs one hook with its arguments and returns the exit code.
type Handler func(args []string, io IO) int

// Handlers are the hooks, by the name the runtimes' settings call them by.
var Handlers = map[string]Handler{
	"announce-stop":   AnnounceStop,
	"context-notice":  ContextNotice,
	"format-edit":     FormatEdit,
	"framework-guard": FrameworkGuard,
	"inspect-guard":   InspectGuard,
	"merge-gate":      MergeGate,
	"pre-tool":        PreTool,
	"return-shape":    ReturnShape,
	"route-guard":     RouteGuard,
	"safety-guard":    SafetyGuard,
	"scope-guard":     ScopeGuard,
	"secrets-guard":   SecretsGuard,
	"vcs-guard":       VCSGuard,
}

// Payload is a hook payload: a JSON object, or nil when stdin held none.
type Payload map[string]any

// read reads the payload; ok is false when stdin is not JSON. A JSON value
// that is not an object reads as an empty payload, as `payload?.x` does.
// PayloadHooks are the hooks that read a JSON payload with the call's cwd
// and call PayloadRead with it once they have; PayloadRead, when set, is
// what the caller does with that cwd ("" when the payload names none):
// the cli flushes that folder's Engram queue (T222 S1b), as pi runs its
// guards from anywhere.
var (
	PayloadHooks = []string{"pre-tool", "agent-hooks"}
	PayloadRead  func(cwd string)
)

// payloadRead calls PayloadRead, when set, with payload's cwd.
func payloadRead(payload []byte) {
	if PayloadRead == nil {
		return
	}
	cwd := ""
	if p, ok := read(bytes.NewReader(payload)); ok {
		cwd = p.Str("cwd")
	}
	PayloadRead(cwd)
}

func read(r io.Reader) (Payload, bool) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, false
	}
	var v any
	if json.Unmarshal(data, &v) != nil {
		return nil, false
	}
	m, _ := v.(map[string]any)
	return Payload(m), true
}

// Str is the string at key, or "" when it is absent or not a string.
func (p Payload) Str(key string) string {
	s, _ := p[key].(string)
	return s
}

// Obj is the object at key, or an empty one.
func (p Payload) Obj(key string) Payload {
	m, _ := p[key].(map[string]any)
	return Payload(m)
}

// Cwd is the payload's cwd, or this process's.
func (p Payload) Cwd() string {
	if c := p.Str("cwd"); c != "" {
		return c
	}
	wd, _ := os.Getwd()
	return wd
}

// jsString is String(v ?? ""): a value as JavaScript prints it.
func jsString(v any) string {
	if v == nil {
		return ""
	}
	return js.String(v)
}

// marshal is compact JSON: no HTML escaping, no trailing newline.
func marshal(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSuffix(b.String(), "\n")
}

// staleNote is the stale-session line for a payload, "\n"-terminated, or "".
func staleNote(p Payload) string {
	if n := session.Note(p.Str("session_id"), p.Str("transcript_path"), project.Home()); n != "" {
		return n + "\n"
	}
	return ""
}

// owner reads `--agent <name>` from a guard's arguments: whether it was
// given, the owner, and the remaining arguments.
func owner(args []string) (scoped bool, name string, rest []string) {
	if len(args) > 0 && args[0] == "--agent" {
		if len(args) > 1 {
			// A runtime file rendered before T177 names the agent by its old name.
			return true, rename.AgentNow(strings.TrimSpace(args[1])), args[2:]
		}
		return true, "", nil
	}
	return false, "", args
}

// othersCall is whether a scoped guard's call belongs to another agent, left
// to that agent's own hooks. A guard scoped to `*` is every agent's: pi,
// whose payloads name no agent, gives it every call.
func othersCall(scoped bool, name string, p Payload) bool {
	caller := rename.AgentNow(strings.TrimSpace(p.Str("agent_type")))
	return scoped && name != "*" && caller != "" && caller != name
}

// caller is the agent making the call: the owner a guard was rendered for
// (Claude Code, `--agent <name>`), else the payload's agent_type, else, in
// a pi child (`--agent *`), the agent pi's subagent tool named in
// JHON_PI_AGENT (shell/lib/agents.ts); "" when none is named.
func caller(name string, p Payload) string {
	if name != "" && name != "*" {
		return name
	}
	if t := strings.TrimSpace(p.Str("agent_type")); t != "" {
		return rename.AgentNow(t)
	}
	if os.Getenv("JHON_PI_CHILD") == "1" {
		return rename.AgentNow(strings.TrimSpace(os.Getenv("JHON_PI_AGENT")))
	}
	return ""
}

// askModes are the permission modes in which Claude Code shows the user a prompt.
var askModes = []string{"default", "acceptEdits", "plan"}

// canAsk is whether the user can be asked: a top-level call in an interactive permission mode.
func canAsk(p Payload) bool {
	sub := strings.TrimSpace(p.Str("agent_id")) != ""
	mode, ok := p["permission_mode"].(string)
	return !sub && ok && slices.Contains(askModes, mode)
}

// grantHint is the refusal text of an ASK class nobody can be asked about:
// how the owner grants it in either runtime, pi having no `!`.
func grantHint(cls string) string {
	return cls + " needs the owner's authorization. Ask the owner; they grant it by running `! jhon grant " + cls + " [--for 30m]` in this project, or in pi, which has no `!`, from /jhon:grants in the shell."
}

// askJSON is what a PreToolUse hook prints to have the user asked.
func askJSON(reason string) string {
	return marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": "ask", "permissionDecisionReason": reason,
	}})
}

// writes is what a file-writing tool call writes: the tool, and every path
// as the call names it (an Edit's or a Write's file_path, a NotebookEdit's
// notebook_path). isWrite is false
// for a tool that writes no file by name.
func writes(p Payload) (tool string, paths []string, isWrite bool) {
	tool = p.Str("tool_name")
	input := p.Obj("tool_input")
	switch tool {
	case "Edit", "Write", "MultiEdit":
		if s, ok := input["file_path"].(string); ok && strings.TrimSpace(s) != "" {
			paths = []string{s}
		}
	case "NotebookEdit":
		if s, ok := input["notebook_path"].(string); ok && strings.TrimSpace(s) != "" {
			paths = []string{s}
		}
	default:
		return tool, nil, false
	}
	return tool, paths, true
}
