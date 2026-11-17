// The stale-session note on every refusing guard (docket AI2).
//
// When a guard refuses and can tell, from the hook payload's transcript and
// the install record, that the session started before the framework was
// last installed, it appends one line saying so. The decision never changes:
// the exit code and the reason are the same with or without the note, and an
// undeterminable start leaves the message as it was.
//
// Each guard is spawned as Claude Code runs it, with HOME a temporary
// directory holding the install record and a fixture transcript, and a PATH
// with no `claude`, so the start is the transcript's first timestamp.

package guards

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

const (
	installedAt = "2026-09-25T01:19:40.000Z"
	staleNote   = "This session started before the framework was last installed (3eb2e0b at 2026-09-25T01:19:40.000Z); restart it with `jhon start --resume <id>` to load the current contract and agents."
)

type staleHome struct{ home, transcript, empty string }

// newStaleHome is a HOME with the install record, and a transcript whose
// first timestamp is start (none when empty).
func newStaleHome(t *testing.T, start string) staleHome {
	t.Helper()
	h := realTemp(t)
	lines := js(t, map[string]any{"type": "agent-setting", "agentSetting": "jhon-orchestrator", "sessionId": "s1"}) + "\n"
	if start != "" {
		lines += js(t, map[string]any{"type": "user", "timestamp": start, "sessionId": "s1"}) + "\n"
	}
	rel := filepath.Join(".claude", "projects", "-w", "s1.jsonl")
	support.Write(t, h, map[string]string{
		filepath.Join(".cache", "jhon", "install.json"): js(t, map[string]any{"installed_at": installedAt, "commit": "3eb2e0b0000000000000000000000000000000aa"}),
		rel: lines,
	})
	return staleHome{h, filepath.Join(h, rel), realTemp(t)}
}

func (h staleHome) run(t *testing.T, g staleGuard, payload map[string]any) support.Result {
	t.Helper()
	return hook(t, []string{"HOME=" + h.home, "PATH=" + h.empty}, g.name, g.args, payload)
}

type staleGuard struct {
	name             string
	args             []string
	refused, allowed map[string]any
	prefix           string
}

// staleGuards is one refusal and one allowed call per guard, as its owner.
var staleGuards = []staleGuard{
	{
		name: "inspect-guard", args: []string{"--agent", "review-risk", "git diff", "git show"}, prefix: "inspect-guard: ",
		refused: map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "git fetch origin"}, "agent_type": "review-risk"},
		allowed: map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "git diff a b"}, "agent_type": "review-risk"},
	},
	{
		name: "vcs-guard", args: []string{"--agent", "general", ".git"}, prefix: "vcs-guard: ",
		refused: map[string]any{"tool_name": "Write", "cwd": "/repo", "tool_input": map[string]any{"file_path": ".git/config"}, "agent_type": "general"},
		allowed: map[string]any{"tool_name": "Write", "cwd": "/repo", "tool_input": map[string]any{"file_path": "/tmp/x.ts"}, "agent_type": "general"},
	},
	{
		name: "safety-guard", args: []string{"--agent", "general"}, prefix: "safety-guard: ",
		refused: map[string]any{"tool_name": "Bash", "cwd": "/repo", "tool_input": map[string]any{"command": "git reset --hard"}, "agent_type": "general"},
		allowed: map[string]any{"tool_name": "Bash", "cwd": "/repo", "tool_input": map[string]any{"command": "git status"}, "agent_type": "general"},
	},
}

func TestGuardStaleNote(t *testing.T) {
	t.Parallel()
	for _, g := range staleGuards {
		t.Run(g.name+"/a refusal in a session older than the install ends with the note", func(t *testing.T) {
			t.Parallel()
			h := newStaleHome(t, "2026-09-24T18:13:29.864Z")
			r := h.run(t, g, with(g.refused, "transcript_path", h.transcript, "session_id", "s1"))
			code(t, r, 2, "refused")
			if !strings.HasPrefix(r.Stderr, g.prefix) {
				t.Errorf("stderr does not start with %q: %s", g.prefix, r.Stderr)
			}
			lines := strings.Split(strings.TrimRight(r.Stderr, " \t\r\n"), "\n")
			if last := lines[len(lines)-1]; last != staleNote {
				t.Errorf("last line = %q; want the note", last)
			}
		})

		t.Run(g.name+"/the same refusal in a newer session, or with no transcript, carries no note", func(t *testing.T) {
			t.Parallel()
			for _, h := range []staleHome{newStaleHome(t, "2026-09-25T04:45:21.000Z"), newStaleHome(t, "")} {
				r := h.run(t, g, with(g.refused, "transcript_path", h.transcript, "session_id", "s1"))
				code(t, r, 2, "refused")
				if strings.Contains(r.Stderr, "started before the framework") {
					t.Errorf("a note: %s", r.Stderr)
				}
			}
			h := newStaleHome(t, "2026-09-24T18:13:29.864Z")
			r := h.run(t, g, g.refused)
			code(t, r, 2, "refused")
			if strings.Contains(r.Stderr, "started before the framework") {
				t.Errorf("no transcript_path: unchanged: %s", r.Stderr)
			}
		})

		t.Run(g.name+"/an allowed call in an older session stays allowed and silent", func(t *testing.T) {
			t.Parallel()
			h := newStaleHome(t, "2026-09-24T18:13:29.864Z")
			r := h.run(t, g, with(g.allowed, "transcript_path", h.transcript, "session_id", "s1"))
			code(t, r, 0, "allowed")
			if r.Stderr != "" {
				t.Errorf("stderr = %q", r.Stderr)
			}
		})
	}
}

func TestGuardStaleNoteKeepsAllowedForms(t *testing.T) {
	t.Parallel()
	h := newStaleHome(t, "2026-09-24T18:13:29.864Z")
	g := staleGuards[0]
	r := h.run(t, g, with(g.refused, "transcript_path", h.transcript))
	matches(t, r.Stderr, `(?m)^allowed forms: git diff, git show$`, "stderr")
}
