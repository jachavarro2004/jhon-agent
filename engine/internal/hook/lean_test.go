package hook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
)

// T146h (L1278, L1280): Claude Code's session starts lean (its agent's
// definition is the lean text) unless the active change is not small, when
// SessionStart hands the install's full context instead (T2); PostToolUse
// points to the full file once per session, on `jhon status`'s not-small
// line or a sub-agent launch (T4). pi's session-brief says which (T2).

const leanBody = "# Jhon\n\nLEAN TEXT\n"

// leanHome is a home with the lean definition and the install's full file.
func leanHome(t *testing.T) (home, full string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/bash")
	def := agentFile[:strings.Index(agentFile, "# Orchestrator")] + leanBody
	full = filepath.Join(home, ".local/share/jhon/current/prose/full-claude-code.md")
	for path, text := range map[string]string{
		filepath.Join(home, ".claude/agents/jhon-orchestrator.md"): def,
		full: "# Orchestrator\n\nFULL TEXT\n\n## The limits\n\nfull rule\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home, full
}

// changeRepo is an LDD repository on main (no change), or, with folder, on
// feat/c1 with the change c1's folder and a code edit: a change that is
// not small.
func changeRepo(t *testing.T, folder bool) string {
	t.Helper()
	repo := gitRepo(t, true)
	git := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	files := map[string]string{"a/go.mod": "module example.com/a\n", "a/a.go": "package a\n"}
	for p, text := range files {
		os.MkdirAll(filepath.Join(repo, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(repo, p), []byte(text), 0o644)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "code")
	if folder {
		git("checkout", "-q", "-b", "feat/c1")
		for p, text := range map[string]string{"openspec/changes/c1/proposal.md": "## Why\n", "a/b.go": "package a\n\nvar B = 1\n"} {
			os.MkdirAll(filepath.Join(repo, filepath.Dir(p)), 0o755)
			os.WriteFile(filepath.Join(repo, p), []byte(text), 0o644)
		}
	}
	return repo
}

// sessionStart is every part SessionStart hands for source in repo.
func sessionStart(t *testing.T, repo, source string) string {
	t.Helper()
	var all []string
	for part := 1; part <= SessionParts; part++ {
		code, out, errs := runAgentHooks(t, `{"hook_event_name":"SessionStart","source":"`+source+`","agent_type":"jhon-orchestrator","session_id":"s-start","cwd":"`+repo+`"}`, "--part", fmt.Sprint(part))
		if code != 0 || errs != "" {
			t.Fatalf("part %d: code %d, %q", part, code, errs)
		}
		all = append(all, out)
	}
	return strings.Join(all, "")
}

func TestSessionStartStartsLeanOrFullByState(t *testing.T) {
	leanHome(t)
	none, notSmall := changeRepo(t, false), changeRepo(t, true)

	t.Run("no active change: the lean text, not the full context", func(t *testing.T) {
		t.Setenv("JHON_AGENT_LOADED", "")
		got := sessionStart(t, none, "startup")
		if !strings.Contains(got, "LEAN TEXT") || strings.Contains(got, "FULL TEXT") {
			t.Errorf("want lean only: %q", got)
		}
	})
	t.Run("a change that is not small: the full context", func(t *testing.T) {
		t.Setenv("JHON_AGENT_LOADED", "")
		got := sessionStart(t, notSmall, "startup")
		if !strings.Contains(got, "FULL TEXT") || !strings.Contains(got, "full rule") || strings.Contains(got, "LEAN TEXT") {
			t.Errorf("want the full context: %q", got)
		}
	})
	t.Run("print mode: the lean system prompt gets the full context only when not small", func(t *testing.T) {
		t.Setenv("JHON_AGENT_LOADED", "jhon-orchestrator")
		if got := sessionStart(t, none, "startup"); strings.Contains(got, "FULL TEXT") || strings.Contains(got, "LEAN TEXT") {
			t.Errorf("no change: want neither body: %q", got)
		}
		if got := sessionStart(t, notSmall, "startup"); !strings.Contains(got, "FULL TEXT") {
			t.Errorf("not small: want the full context: %q", got)
		}
	})
}

// postToolUse is agent-hooks' answer to a PostToolUse payload.
func postToolUse(t *testing.T, session, tool string, input, response any) string {
	t.Helper()
	p, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "agent_type": "jhon-orchestrator", "session_id": session,
		"cwd": t.TempDir(), "tool_name": tool, "tool_input": input, "tool_response": response})
	code, out, errs := runAgentHooks(t, string(p))
	if code != 0 || errs != "" {
		t.Fatalf("code %d, %q", code, errs)
	}
	return out
}

func TestPostToolUsePointsToTheFullContextOnce(t *testing.T) {
	_, full := leanHome(t)
	notSmall := map[string]any{"stdout": "notes\n  - not small: read " + full + " before the next edit\n", "stderr": ""}
	pointer := func(out string) string {
		var v struct {
			Out struct {
				Event   string `json:"hookEventName"`
				Context string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		if out == "" {
			return ""
		}
		if err := json.Unmarshal([]byte(out), &v); err != nil || v.Out.Event != "PostToolUse" {
			t.Fatalf("not a PostToolUse answer: %q", out)
		}
		return v.Out.Context
	}
	got := pointer(postToolUse(t, "s1", "Bash", map[string]any{"command": "jhon status"}, notSmall))
	if !strings.Contains(got, full) || !strings.Contains(got, "before the next edit") {
		t.Errorf("status not small: want the pointer to %s, got %q", full, got)
	}
	if again := postToolUse(t, "s1", "Bash", map[string]any{"command": "jhon status"}, notSmall); again != "" {
		t.Errorf("the pointer came twice in one session: %q", again)
	}
	if got := pointer(postToolUse(t, "s2", "Agent", map[string]any{"subagent_type": "worker", "prompt": "x"}, map[string]any{})); !strings.Contains(got, full) {
		t.Errorf("a sub-agent launch: want the pointer, got %q", got)
	}
	small := map[string]any{"stdout": "notes\n  - small change (no folder, ...)\n"}
	for _, c := range []struct {
		tool  string
		input map[string]any
		resp  map[string]any
	}{
		{"Bash", map[string]any{"command": "jhon status"}, small},
		{"Bash", map[string]any{"command": "ls"}, notSmall},
		{"Read", map[string]any{"file_path": "/x"}, notSmall},
	} {
		if out := postToolUse(t, "s3", c.tool, c.input, c.resp); out != "" {
			t.Errorf("%s %v: want nothing, got %q", c.tool, c.input, out)
		}
	}
	// T192 S5: the jhon MCP server's status tool (`jhon status --json`), as
	// Claude Code names it from the plugin (and from a user-registered
	// server), is a route to the same not-small line; its response is the
	// tool's content blocks, the status JSON as text.
	statusJSON := func(note string) []any {
		text, _ := json.Marshal(map[string]any{"ok": true, "notes": []string{note}})
		return []any{map[string]any{"type": "text", "text": string(text)}}
	}
	for i, tool := range []string{"mcp__plugin_jhon_jhon__status", "mcp__jhon__status"} {
		session := fmt.Sprintf("s-mcp-%d", i)
		got := pointer(postToolUse(t, session, tool, map[string]any{}, statusJSON("not small: read "+full+" before the next edit")))
		if !strings.Contains(got, full) || !strings.Contains(got, "this change is not small") {
			t.Errorf("%s not small: want the pointer to %s, got %q", tool, full, got)
		}
		if again := postToolUse(t, session, tool, map[string]any{}, statusJSON("not small: read "+full+" before the next edit")); again != "" {
			t.Errorf("%s: the pointer came twice in one session: %q", tool, again)
		}
		if out := postToolUse(t, session+"-small", tool, map[string]any{"change": "c"}, statusJSON("small change (no folder, ...)")); out != "" {
			t.Errorf("%s small: want nothing, got %q", tool, out)
		}
	}
	if out := postToolUse(t, "s-mcp-audit", "mcp__plugin_jhon_jhon__audit", map[string]any{"session": "x"}, statusJSON("not small: read "+full+" before the next edit")); out != "" {
		t.Errorf("the audit tool is no status route: %q", out)
	}
	// A session that started full needs no pointer.
	sessionStart(t, changeRepo(t, true), "startup")
	if out := postToolUse(t, "s-start", "Agent", map[string]any{"subagent_type": "worker"}, map[string]any{}); out != "" {
		t.Errorf("a session that started full got the pointer: %q", out)
	}
}

func TestSessionBriefSaysLeanOrFull(t *testing.T) {
	leanHome(t)
	for _, c := range []struct {
		repo string
		want string
	}{{gitRepo(t, false), "lean"}, {changeRepo(t, false), "lean"}, {changeRepo(t, true), "full"}} {
		var out, errs bytes.Buffer
		if code := SessionBrief([]string{"jhon-orchestrator"}, IO{strings.NewReader(`{"cwd":"` + c.repo + `"}`), &out, &errs}); code != 0 {
			t.Fatalf("code %d: %s", code, errs.String())
		}
		var v map[string]any
		if err := json.Unmarshal(out.Bytes(), &v); err != nil {
			t.Fatal(err, out.String())
		}
		if v["context"] != c.want {
			t.Errorf("%s: context %v, want %s (%s)", c.repo, v["context"], c.want, out.String())
		}
	}
}

// postToolUseIn is postToolUse for a session working in cwd.
func postToolUseIn(t *testing.T, session, cwd, tool string, input, response any) string {
	t.Helper()
	p, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "agent_type": "jhon-orchestrator", "session_id": session,
		"cwd": cwd, "tool_name": tool, "tool_input": input, "tool_response": response})
	code, out, errs := runAgentHooks(t, string(p))
	if code != 0 || errs != "" {
		t.Fatalf("code %d, %q", code, errs)
	}
	return out
}

// T192 S7: in a fresh repository the session starts lean; once the agent
// makes or selects a change that is not small (a write or edit under the
// changes folder, a branch switch, a jhon run), the pointer comes then,
// before the next edit, without waiting for `jhon status`; never for a
// small change, and once per session.
func TestPostToolUsePointsToTheFullContextWhenANotSmallChangeAppears(t *testing.T) {
	_, full := leanHome(t)
	notSmall := changeRepo(t, true)
	proposal := filepath.Join(notSmall, "openspec/changes/c1/proposal.md")
	for i, c := range []struct {
		tool  string
		input map[string]any
	}{
		{"Write", map[string]any{"file_path": proposal, "content": "## Why\n"}},
		{"Edit", map[string]any{"file_path": proposal, "old_string": "a", "new_string": "b"}},
		{"MultiEdit", map[string]any{"file_path": proposal, "edits": []any{}}},
		{"Bash", map[string]any{"command": "git switch feat/c1"}},
		{"Bash", map[string]any{"command": "git checkout -b feat/c1"}},
		{"Bash", map[string]any{"command": "jhon executions run"}},
		{"Bash", map[string]any{"command": "mkdir -p openspec/changes/c1"}},
	} {
		session := fmt.Sprintf("s-appear-%d", i)
		got := postToolUseIn(t, session, notSmall, c.tool, c.input, map[string]any{})
		if !strings.Contains(got, full) || !strings.Contains(got, "this change is not small") {
			t.Errorf("%s %v: want the pointer to %s, got %q", c.tool, c.input, full, got)
		}
		if again := postToolUseIn(t, session, notSmall, c.tool, c.input, map[string]any{}); again != "" {
			t.Errorf("%s %v: the pointer came twice in one session: %q", c.tool, c.input, again)
		}
	}
	// Other calls in that repository are no moment: nothing is asked or said.
	for _, c := range []struct {
		tool  string
		input map[string]any
	}{
		{"Write", map[string]any{"file_path": filepath.Join(notSmall, "a/b.go"), "content": "package a\n"}},
		{"Read", map[string]any{"file_path": proposal}},
		{"Bash", map[string]any{"command": "ls"}},
		{"Bash", map[string]any{"command": "git status"}},
	} {
		if out := postToolUseIn(t, "s-other", notSmall, c.tool, c.input, map[string]any{}); out != "" {
			t.Errorf("%s %v: want nothing, got %q", c.tool, c.input, out)
		}
	}
	// No change, or a small one: no pointer.
	none := changeRepo(t, false)
	small := changeRepo(t, false)
	if out, err := exec.Command("git", "-C", small, "checkout", "-q", "-b", "fix-typo").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	os.WriteFile(filepath.Join(small, "README.md"), []byte("# A\n\nA typo fixed.\n"), 0o644)
	if ldd.NotSmall(small) || ldd.NotSmall(none) {
		t.Fatalf("fixture: want no change (%v) and a small one (%v)", ldd.NotSmall(none), ldd.NotSmall(small))
	}
	for _, repo := range []string{none, small} {
		for _, c := range []struct {
			tool  string
			input map[string]any
		}{
			{"Write", map[string]any{"file_path": filepath.Join(repo, "openspec/changes/notes.md"), "content": "x"}},
			{"Bash", map[string]any{"command": "git switch -c fix-typo"}},
			{"Bash", map[string]any{"command": "jhon executions run"}},
		} {
			if out := postToolUseIn(t, "s-small", repo, c.tool, c.input, map[string]any{}); out != "" {
				t.Errorf("%s %s %v: want nothing, got %q", repo, c.tool, c.input, out)
			}
		}
	}
}

// T7 (L5): the verifier's probes, kept: another server's status tool and
// Engram never point, and the jhon status tool's response read as a plain
// string still does.
func TestPostToolUsePointerRoutes(t *testing.T) {
	_, full := leanHome(t)
	line := "not small: read " + full + " before the next edit"
	blocks := []any{map[string]any{"type": "text", "text": `{"ok":true,"notes":["` + line + `"]}`}}
	for i, tool := range []string{"mcp__other__status", "mcp__plugin_other_other__status", "mcp__engram__mem_search", "mcp__plugin_engram_engram__mem_search"} {
		if out := postToolUse(t, fmt.Sprintf("s-route-%d", i), tool, map[string]any{}, blocks); out != "" {
			t.Errorf("%s: want nothing, got %q", tool, out)
		}
	}
	for i, tool := range []string{"mcp__plugin_jhon_jhon__status", "mcp__jhon__status"} {
		if got := postToolUse(t, fmt.Sprintf("s-string-%d", i), tool, map[string]any{}, `{"ok":true,"notes":["`+line+`"]}`); !strings.Contains(got, full) {
			t.Errorf("%s with a string response: want the pointer, got %q", tool, got)
		}
	}
}
