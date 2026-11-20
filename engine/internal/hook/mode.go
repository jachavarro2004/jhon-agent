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
// the guard cannot tell (guard.UnknownRoot) is normal.
func modeFor(own, root, cls string) (grant.ModeRecord, bool) {
	if root == guard.UnknownRoot {
		m := grant.ModeOf("")
		return m, grant.ModeCovers(m.Mode, cls)
	}
	mine := grant.ModeOf(own)
	if root != "" && !project.Exists(root) {
		root = project.Root(existingAncestor(root))
	}
	if root == "" || !isRepo(root) || grant.Root(root) == mine.Root {
		return mine, grant.ModeCovers(mine.Mode, cls)
	}
	theirs := grant.ModeOf(root)
	return theirs, grant.ModeCovers(mine.Mode, cls) && grant.ModeCovers(theirs.Mode, cls)
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
	mode      grant.ModeRecord
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
		if slices.Contains(byMode[x.mode.Mode], part) {
			continue
		}
		if !slices.Contains(modes, x.mode.Mode) {
			modes = append(modes, x.mode.Mode)
		}
		byMode[x.mode.Mode] = append(byMode[x.mode.Mode], part)
		// The log is the audit's record; a call is not refused for it.
		_ = grant.RecordAllowed(grant.Allowed{At: now, Mode: x.mode.Mode, Root: x.mode.Root, Class: x.cls, What: x.what,
			Guard: guardName, Session: p.Str("session_id"), Agent: agent})
	}
	var notes []string
	for _, m := range modes {
		notes = append(notes, fmt.Sprintf("allowed by the owner's %s mode: %s", m, strings.Join(byMode[m], "; ")))
	}
	fmt.Fprint(io.Out, noteJSON(modeNotePrefix+strings.Join(notes, "; ")))
}

// modeNotePrefix opens a mode's note: the call ran and was not refused.
const modeNotePrefix = "note (not a refusal; the call ran): "

// noteJSON is what a PreToolUse hook prints to allow a call with a note for the agent.
func noteJSON(note string) string {
	return marshal(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PreToolUse", "additionalContext": note}})
}
