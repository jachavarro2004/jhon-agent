package audit

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	lddlayout "github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
)

// inline-not-small (delegation/not-small-inline, T220 S2), heuristic: a
// suspect, never a violation. contracts/ldd.md's apply row has the
// jhon-builder agent apply a change and keeps inline work for a small one.
// Once the main thread was told its change is not small, its own successful
// Edit, MultiEdit, or Write of a file outside every change's record folder
// (`ldd|openspec/changes/**`, archived copies included) is the finding:
// once per change, at its first such edit, naming the change and the file.
// The state belongs to that one change (T220 L6): the next `jhon status`
// result moves it (another change that is not small) or ends it (one that
// is small, or none), and a jhon-builder or jhon-checker launched for the
// change, or the owner's message naming another change the session names,
// ends it. Told is either
//
//   - a `jhon status` result, through Bash or the jhon MCP server's status
//     tool, holding status's not-small line (ldd.NotSmallNote, "not small:
//     read "); the change is the one status names (its operand, else its
//     first line or its JSON's `change`), else the one the main thread last
//     named;
//   - pi's shell loading the full context ("Jhon: this work needs the full
//     context") when no sub-agent launch came since the last owner message:
//     the shell loads it then for a change moment the engine found not
//     small; the change is the one the main thread last named.
//
// Sub-agents' writes are theirs to make, and a write through Bash or under
// the system temp directory is not read. The transcript does not record the
// delegation mode (inline or writer), nor whether a writer was blocked and
// the owner let the main thread write instead: it cannot tell them apart,
// so the finding is a suspect for a person to read.

// notSmallLine is status's line for a change that is not small (ldd.NotSmallNote).
const notSmallLine = "not small: read "

// fullContext is how pi's shell opens the full context it loads (shell/lib/lean-context.ts FULL_MESSAGE).
const fullContext = "Jhon: this work needs the full context"

var statusChange = lazyre.New(`(?m)^change\s+(\S+)`)

// changesFolder matches a path in any change's record folder, in either layout.
var changesFolder = lazyre.New(`(^|/)(` + lddlayout.DirsPattern() + `)/changes/`)

// runsStatus is whether command runs `jhon status`.
func runsStatus(command string) bool {
	for _, x := range codeless(plain(command)) {
		if x.Program == "jhon" && x.Sub == "status" {
			return true
		}
	}
	return false
}

// statusTool is whether a tool is the jhon MCP server's status tool, as Claude Code or pi names it.
func statusTool(name string) bool {
	return name == "mcp__jhon__status" || name == "mcp__plugin_jhon_jhon__status"
}

// statusNames is the change a `jhon status` result names: the
// command's operand, else its JSON's `change`, else its first line's; "".
func statusNames(command, text string) string {
	if c := statusOperand(command); c != "" {
		return c
	}
	if c, ok := strProp(jsonObject(text), "change"); ok {
		return c
	}
	if m := statusChange.FindStringSubmatch(text); m != nil && m[1] != "none" {
		return m[1]
	}
	return ""
}

// namesChange is whether an owner's message names change as a word of its own.
func namesChange(text, change string) bool {
	for _, w := range changeWord.FindAllString(text, -1) {
		if w == change {
			return true
		}
	}
	return false
}

var changeWord = lazyre.New(`[\w.-]+`)

func checkInlineNotSmall(s *Session, emit emitFunc) {
	t := s.Main
	type told struct{ change, how string }
	var (
		since    *told  // the change the main thread was last told is not small
		named    string // the change the main thread last named
		launched bool   // a sub-agent was launched since the last owner message
		uses     = map[string]ToolUse{}
		at       = map[string]int{} // the line of each call
		reported = map[string]bool{}
		changes  []string              // every change the session names
		launch   = map[string]string{} // each jhon-builder or jhon-checker launch's change
	)
	for _, x := range s.transcripts() {
		for _, e := range x.Entries {
			for _, u := range e.ToolUses {
				if u.named != "" {
					addOnce(&changes, u.named)
				}
			}
		}
	}
	for _, a := range s.Subagents {
		if a.change != "" {
			addOnce(&changes, a.change)
		}
		if a.Parent == "" && (a.AgentType == "jhon-builder" || a.AgentType == "jhon-checker") {
			launch[a.ToolUseID] = a.change
		}
	}
	for _, e := range t.Entries {
		if IsGenuineUserMessage(e) {
			launched = false
			// The owner's request that names another change ends the state.
			if since != nil && slices.ContainsFunc(changes, func(c string) bool { return c != since.change && namesChange(e.note(), c) }) {
				since = nil
			}
		}
		if e.Role == "user" && e.Origin != nil && *e.Origin == "extension" && strings.HasPrefix(e.note(), fullContext) && !launched {
			since = &told{named, "after the shell loaded the full context for it"}
		}
		for _, u := range e.ToolUses {
			uses[u.ID], at[u.ID] = u, e.Line
			if u.named != "" {
				named = u.named
			}
			if u.Name == "Agent" || u.Name == "Task" || u.Name == "SendMessage" {
				launched = true
			}
			// A jhon-builder or jhon-checker launched for the change: the work left the main thread.
			if c, ok := launch[u.ID]; ok && since != nil && (c == "" || c == since.change) {
				since = nil
			}
		}
		for _, r := range e.ToolResults {
			u, ok := uses[r.ID]
			if !ok || r.IsError {
				continue
			}
			command, _ := strProp(u.Input, "command")
			// Each status says which change is current and whether it is small: the state follows it.
			if u.Name == "Bash" && runsStatus(command) || statusTool(u.Name) {
				since = nil
				if strings.Contains(r.Text, notSmallLine) {
					since = &told{or(statusNames(command, r.Text), named), "after jhon status called it not small"}
				}
				continue
			}
			_, file, write := writeOf(u)
			// No change's record files ever count, in either layout, archived or not.
			if !write || since == nil || scratch(file) || changesFolder.MatchString(file) {
				continue
			}
			change := or(since.change, "(unnamed)")
			if reported[change] {
				continue
			}
			reported[change] = true
			emit(emitted{check: "inline-not-small", severity: "suspect", anchor: "delegation/not-small-inline", transcript: t.File, line: at[r.ID],
				agent: t.Agent, agentType: t.AgentType, evidence: file, layout: byCheck,
				message: fmt.Sprintf("%s %s edits %s of %s itself %s: the `jhon-builder` agent applies a change that is not small, inline only a small one (contracts/ldd.md, the apply row)",
					t.AgentType, t.Agent, file, change, since.how)})
		}
	}
}
