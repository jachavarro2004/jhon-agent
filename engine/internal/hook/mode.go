package hook

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/rename"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/grant"
	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/workspace"
)

// modeFor is the owner's mode that answers an ask of cls whose finding
// targets root, in a session whose own project is own (the root of its
// cwd), and whether it passes the ask. A finding that names no repository,
// a directory that is no repository (a worktree already removed), or the
// session's own project or one of its worktrees takes the session's mode
// (T51). A directory not there yet is judged by its nearest existing
// ancestor. A finding in another repository takes that project's mode, and
// passes only when the session's own mode passes it too: a cd into a
// super-yolo project lends a normal session nothing (L157), and a
// super-yolo session lends a normal project nothing (L183). A repository
// the guard cannot tell (guard.UnknownRoot) is normal. A member of a
// workspace that its primary lists, that joined and that has no mode of
// its own takes the primary's (workspace.ModeOf, T281: one mode for the
// workspace), as the session's project and as the finding's; its note says
// from which project.
func modeFor(own, root, cls string) (modeRec, bool) {
	if root == guard.UnknownRoot {
		m := grant.ModeOf("")
		return modeRec{ModeRecord: m}, grant.ModeCovers(m.Mode, cls)
	}
	m, from := workspace.ModeOf(own, own)
	mine := modeRec{m, from}
	if root != "" && !project.Exists(root) {
		root = project.Root(existingAncestor(root))
	}
	if root == "" || !isRepo(root) || grant.Root(root) == grant.Root(own) {
		return mine, grant.ModeCovers(mine.Mode, cls)
	}
	m, from = workspace.ModeOf(root, own)
	theirs := modeRec{m, from}
	return theirs, grant.ModeCovers(mine.Mode, cls) && grant.ModeCovers(theirs.Mode, cls)
}

// modeRec is the mode that answers an ask, and the project it is taken
// from when that is not the project's own (a workspace's primary, T281).
type modeRec struct {
	grant.ModeRecord
	from string
}

// label is how a note names the mode: "super-yolo mode", or "super-yolo
// mode, from <primary>" for one a member takes from its primary.
func (m modeRec) label() string {
	if m.from == "" {
		return m.Mode + " mode"
	}
	return m.Mode + " mode, from " + m.from
}

// existingAncestor is the nearest ancestor of dir that exists.
func existingAncestor(dir string) string {
	for !project.Exists(dir) {
		up := filepath.Dir(dir)
		if up == dir {
			break
		}
		dir = up
	}
	return dir
}

// isRepo is whether dir is inside a git repository, a bare one included.
func isRepo(dir string) bool {
	if !project.Exists(dir) {
		return false
	}
	_, ok := project.ReadGit(dir, "rev-parse", "--git-dir")
	return ok
}

// modePass is an ask the owner's mode let through: the mode, its class, and what.
type modePass struct {
	mode      modeRec
	cls, what string
}

// passByMode allows a call whose every open ask the owner's mode covers:
// it tells the agent so in one line, "note (not a refusal; the call ran):
// allowed by the owner's <mode> mode: <class>: <what>", as context beside
// the call's result, never in its output, and plainly not a refusal: an
// agent read the bare line as one and changed its commands (T100, L671).
// It logs each one for jhon audit. None passed prints
// nothing, the guard's allow as it is.
func passByMode(io IO, p Payload, guardName string, passed []modePass) {
	if len(passed) == 0 {
		return
	}
	var modes []string
	byMode := map[string][]string{}
	agent := rename.AgentNow(strings.TrimSpace(p.Str("agent_type")))
	if agent == "" {
		agent = "main"
	}
	now := isodate.Format(time.Now())
	for _, x := range passed {
		part := x.cls + ": " + x.what
		label := x.mode.label()
		if slices.Contains(byMode[label], part) {
			continue
		}
		if !slices.Contains(modes, label) {
			modes = append(modes, label)
		}
		byMode[label] = append(byMode[label], part)
		// The log is the audit's record; a call is not refused for it.
		_ = grant.RecordAllowed(grant.Allowed{At: now, Mode: x.mode.Mode, Root: x.mode.Root, Class: x.cls, What: x.what,
			Guard: guardName, Session: p.Str("session_id"), Agent: agent})
	}
	var notes []string
	for _, m := range modes {
		notes = append(notes, fmt.Sprintf("allowed by the owner's %s: %s", m, strings.Join(byMode[m], "; ")))
	}
	fmt.Fprint(io.Out, noteJSON(modeNotePrefix+strings.Join(notes, "; ")))
}

// modeNotePrefix opens a mode's note: the call ran and was not refused.
const modeNotePrefix = "note (not a refusal; the call ran): "

// noteJSON is what a PreToolUse hook prints to allow a call with a note for the agent.
func noteJSON(note string) string {
	return marshal(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PreToolUse", "additionalContext": note}})
}
