package hook

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

var agentID = lazyre.New(`^[\w-]+$`)

// launchPrompt is the prompt a sub-agent's scope is read from: the payload's
// launch_prompt, which a runtime without per-agent transcripts passes (pi's
// child agents, runtimes/pi; its shell resolves a continued agent's scope),
// or else read from its own transcript (Claude Code); ok is false when there
// is none to read.
//
// In the transcript it is the launch prompt, the first user entry, unless a
// resumed delegation carried a corrected `## Allowed edit surfaces` (T133):
// Claude Code appends the message of a SendMessage to the same transcript as
// a user entry whose origin is the coordinator, and the last of those that
// carries the heading is the scope from then on. Only the launching side
// sets it: the writer's own text, a tool result, and any other entry the
// runtime injects (no origin, or another one) change nothing.
func launchPrompt(p Payload) (string, bool) {
	if s := p.Str("launch_prompt"); s != "" {
		return s, true
	}
	file := subagentFile(p.Str("transcript_path"), strings.TrimSpace(p.Str("agent_id")))
	if file == "" {
		return "", false
	}
	f, err := os.Open(file)
	if err != nil {
		return "", false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<30)
	prompt, launched := "", false
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Past the launch prompt only a coordinator's entry is read.
		if launched && !strings.Contains(line, coordinatorOrigin) {
			continue
		}
		var o struct {
			Type    any `json:"type"`
			Origin  any `json:"origin"`
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &o) != nil {
			if !launched {
				return "", false // unreadable: the scope is unknown
			}
			continue // an unreadable later line sets no scope
		}
		if o.Type != "user" {
			continue
		}
		text, ok := promptText(o.Message.Content)
		if !launched {
			if !ok {
				return "", false
			}
			prompt, launched = text, true
			continue
		}
		if origin, _ := o.Origin.(map[string]any); origin["kind"] != coordinatorOrigin || !ok {
			continue
		}
		if _, listed := guard.SurfacesOf(text); listed {
			prompt = text
		}
	}
	return prompt, launched
}

// coordinatorOrigin is the origin kind of the user entry Claude Code writes
// in a sub-agent's transcript for a message its launcher sent it.
const coordinatorOrigin = "coordinator"

// promptText is a user entry's content as text: a string, or the text of
// its parts; ok is false for any other content.
func promptText(content any) (string, bool) {
	switch c := content.(type) {
	case string:
		return c, true
	case []any:
		var b strings.Builder
		for _, x := range c {
			if m, ok := x.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					b.WriteString(t)
				}
			}
		}
		return b.String(), true
	}
	return "", false
}

// ScopeGuard keeps a bounded writer to the files its handoff names
// (scope-guard, contracts/efficiency.md "Writer scope"). A Bash write whose
// file the guard cannot resolve runs with a note naming the surfaces (a
// refusal until T153, L1364 b: it is not destructive); one it resolves
// outside them is refused.
func ScopeGuard(args []string, io IO) int {
	scoped, name, _ := owner(args)
	var p Payload
	block := func(reason string) int {
		fmt.Fprintf(io.Err, "scope-guard: %s\n%s", reason, staleNote(p))
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
	prompt, ok := launchPrompt(p)
	if !ok {
		return 0
	}
	surfaces, ok := guard.SurfacesOf(prompt)
	if !ok {
		return 0
	}
	cwd := p.Cwd()
	input := p.Obj("tool_input")
	listed := strings.Join(surfaces, ", ")
	if listed == "" {
		listed = "(none)"
	}
	rule := "outside the allowed edit surfaces (" + listed + "); stop and return status: blocked naming the file and why the change needs it, so the orchestrator can widen the list (contracts/efficiency.md, \"Writer scope\")"
	switch tool := p.Str("tool_name"); tool {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		path := input["file_path"]
		if path == nil {
			path = input["notebook_path"]
		}
		s, isStr := path.(string)
		if !isStr || s == "" {
			return block("the write names no path")
		}
		abs := project.Resolve(cwd, s)
		if !guard.InSurfaces(abs, surfaces) {
			return block(fmt.Sprintf("%s %s is %s", tool, abs, rule))
		}
	case "Bash":
		if command, isStr := input["command"].(string); isStr {
			if outside := guard.WritesOutside(command, cwd, surfaces); len(outside) > 0 {
				return block("the command writes " + strings.Join(outside, ", ") + ", " + rule)
			}
			// A write whose file is unknown may be outside, unless a file its code
			// names is in scope (T94, L652): it runs, and the writer is told.
			var unknown []string
			for _, u := range guard.UnresolvedWrites(command, cwd) {
				if !slices.ContainsFunc(u.Targets, func(f string) bool { return guard.InSurfaces(f, surfaces) }) {
					unknown = append(unknown, u.Label)
				}
			}
			if len(unknown) > 0 {
				fmt.Fprint(io.Out, noteJSON(modeNotePrefix+"the command writes "+strings.Join(unknown, ", ")+", which the guard cannot hold to the allowed edit surfaces ("+listed+"): write only inside them, or stop and return status: blocked naming the file and why the change needs it (contracts/efficiency.md, \"Writer scope\")"))
			}
		}
	}
	return 0
}

// subagentFile is the transcript of the sub-agent id of the session whose
// transcript is main (Claude Code: <session>/subagents/agent-<id>.jsonl);
// "" when either is not given or the file does not exist.
func subagentFile(main, id string) string {
	if id == "" || main == "" || !agentID.MatchString(id) {
		return ""
	}
	file := filepath.Join(filepath.Dir(main), strings.TrimSuffix(filepath.Base(main), ".jsonl"), "subagents", "agent-"+id+".jsonl")
	if _, err := os.Stat(file); err != nil {
		return ""
	}
	return file
}
