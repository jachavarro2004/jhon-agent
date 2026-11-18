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
