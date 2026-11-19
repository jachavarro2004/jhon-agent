package hook

import (
	"bytes"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/grant"
	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/workspace"
)

// VCSGuard is the PreToolUse guard for the file-writing tools (Edit, Write,
// NotebookEdit):
// no one writes a VCS directory or the grants directory, and no agent writes
// Claude Code settings. A write outside the project (with a workspace's
// primary and joined members, workspace.ProjectRoots) and the scratch roots
// runs with a note, and so does an explore agent's write beyond its record
// (each a refusal or an ask until T153, L957: neither is destructive). Fail
// closed.
func VCSGuard(args []string, io IO) int {
	scoped, name, rest := owner(args)
	var names []string
	for _, n := range rest {
		if n = strings.ToLower(strings.TrimSpace(n)); n != "" {
			names = append(names, n)
		}
	}
	var p Payload
	block := func(reason string) int {
		fmt.Fprintf(io.Err, "vcs-guard: %s\n%s", reason, staleNote(p))
		return 2
	}
	p, ok := read(io.In)
	if !ok {
		return block("hook payload is not valid JSON")
	}
	if scoped && name == "" {
		return block("--agent was given without an agent name")
	}
	if othersCall(scoped, name, p) {
		return 0
	}
	if len(names) == 0 {
		return block("no VCS directory names were given; the render is broken")
	}
	tool, paths, isWrite := writes(p)
	if !isWrite {
		return 0
	}
	if len(paths) == 0 {
		return block(fmt.Sprintf("%s call names no file", tool))
	}
	var notes []string
	// The explore agent writes only its change's record (T123); another
	// write runs with a note.
	if caller(name, p) == guard.ExploreAgent {
		for _, raw := range paths {
			if abs := project.Resolve(p.Cwd(), raw); !exploreMayWrite(p.Cwd(), abs) {
				notes = append(notes, fmt.Sprintf("%s; %s of %s is outside it: return what you found to the orchestrator instead of writing it.", guard.ExploreScope, tool, abs))
			}
		}
	}
	var asks []string
	var passed []modePass
	for _, raw := range paths {
		switch verdict, why, pass := vcsJudge(p, tool, raw, names); verdict {
		case vcsRefuse:
			return block(why)
		case vcsAsk:
			asks = append(asks, why)
		case vcsMode:
			passed = append(passed, pass)
		case vcsNote:
			notes = append(notes, why)
		}
	}
	if len(asks) > 0 {
		fmt.Fprintln(io.Out, askJSON(strings.Join(asks, "; ")))
		return 0
	}
	// The mode's note and the advisories are one answer, opened once by
	// modeNotePrefix.
	var byMode bytes.Buffer
	passByMode(IO{io.In, &byMode, io.Err}, p, "vcs-guard", passed)
	if note, ok := noteOf(byMode.Bytes()); ok {
		notes = append([]string{strings.TrimPrefix(note, modeNotePrefix)}, notes...)
	}
	if len(notes) > 0 {
		fmt.Fprint(io.Out, noteJSON(modeNotePrefix+strings.Join(notes, "; ")))
	}
	return 0
}

type vcsVerdict int

const (
	vcsAllow vcsVerdict = iota
	vcsAsk
	vcsRefuse
	// vcsMode is an ask the owner's mode passes.
	vcsMode
	// vcsNote allows the write with a note for the agent.
	vcsNote
)

// vcsJudge is vcs-guard's verdict on one written path, and its reason: the
// ask's text, the refusal's or the note's; or what the owner's mode passed,
// with that mode and its class.
func vcsJudge(p Payload, tool, raw string, names []string) (vcsVerdict, string, modePass) {
	var none modePass
	cwd := p.Cwd()
	abs := project.Resolve(cwd, raw)
	real := project.Realish(abs)
	hit := func(path string) string {
		for _, seg := range strings.Split(path, string(filepath.Separator)) {
			if slices.Contains(names, strings.ToLower(seg)) {
				return seg
			}
		}
		return ""
	}
	found := hit(abs)
	if found == "" {
		found = hit(real)
	}
	if found != "" {
		return vcsRefuse, fmt.Sprintf("%s is inside a VCS directory (%s); no one writes there through %s", raw, found, tool), none
	}
	if grant.InGrants(abs) {
		return vcsRefuse, raw + " is inside the grants directory; a grant is the owner's act (`! jhon grant` in Claude Code, /jhon:grants in pi), never a write", none
	}
	if project.IsClaudeSettings(abs) {
		why := fmt.Sprintf("%s of %s changes Claude Code settings, which configure the guards", tool, raw)
		if m, ok := modeFor(project.Root(cwd), "", "settings"); ok {
			return vcsMode, "", modePass{m, "settings", why}
		}
		if canAsk(p) {
			return vcsAsk, "settings: " + why, none
		}
		return vcsRefuse, why + "; no agent writes them. Stop and report this refusal verbatim; the owner edits the settings by hand in their own terminal, or with `!` in Claude Code.", none
	}

	home := project.Realish(project.Home())
	allowed := append(append(workspace.ProjectRoots(cwd), project.TmpRoots()...), filepath.Join(home, ".claude"), filepath.Join(home, ".cache"))
	for _, r := range allowed {
		if project.Within(real, r) || project.Within(real, project.Realish(r)) {
			return vcsAllow, "", none
		}
	}
	root := project.Root(cwd)
	shown := root
	if shown == "" {
		shown = "no project root: " + project.Realish(cwd) + " is not a git repository"
	}
	// Not destructive: the write runs, and the agent is told (T153, L957).
	return vcsNote, fmt.Sprintf("%s of %s lands outside the project (%s) and the scratch roots; check that the task means this file", tool, raw, shown), none
}

// exploreMayWrite is whether explore, working in cwd, may write abs: both
// the path it gave and that path's real one (symlinks resolved, a
// not-yet-existing file through its deepest existing ancestor) are a
// change's record right under cwd's project root (T123).
func exploreMayWrite(cwd, abs string) bool {
	root := project.Root(cwd)
	if root == "" {
		return false
	}
	given := guard.ExploreMayWrite(abs, root)
	// The root as cwd spells it, when cwd reaches it through a symlink.
	if c := filepath.Clean(project.Resolve("", cwd)); !given {
		if rel, err := filepath.Rel(root, project.Realish(c)); err == nil && (rel == "." || strings.HasSuffix(c, string(filepath.Separator)+rel)) {
			lexical := c
			if rel != "." {
				lexical = filepath.Clean(strings.TrimSuffix(c, rel))
			}
			given = lexical != root && guard.ExploreMayWrite(abs, lexical)
		}
	}
	return given && guard.ExploreMayWrite(project.Realish(abs), root)
}
