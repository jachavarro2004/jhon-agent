// A main agent's definition that is empty does not fail SessionStart (the
// crash the third verify of T128b and T128c found, L941): with no text there
// is nothing to hand the session, and nothing is said.
package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestSessionStartWithAnEmptyMainAgentDefinitionSaysNothing(t *testing.T) {
	b, env := wired(t, realShell(t, "bash"))
	rendered(t, b)
	definition := filepath.Join(b.home, ".claude", "agents", "jhon-orchestrator.md")
	if err := os.WriteFile(definition, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	off := modeOff(t, b, env)
	for _, mode := range []struct {
		name string
		env  []string
	}{{"on", env}, {"off", off}} {
		// SessionStart, as the plugin's entries run it: with no part, and each part.
		for _, part := range [][]string{nil, {"--part", "1"}, {"--part", "2"}, {"--part", "3"}, {"--part", "4"}} {
			for _, source := range []string{"startup", "clear", "resume", "compact"} {
				payload := `{"hook_event_name":"SessionStart","source":"` + source + `","session_id":"s","cwd":` + quoted(t, b.proj) + mainAgent + `}`
				r := support.Run(t, support.Cmd{Args: append([]string{support.Bin(t), "hook", "agent-hooks", "jhon-orchestrator"}, part...), Env: mode.env, Stdin: payload})
				if r.Code != 0 || r.Stdout != "" || r.Stderr != "" {
					t.Errorf("mode %s, SessionStart %v after %s: exit %d, stdout %q, stderr %q; want exit 0 and nothing said", mode.name, part, source, r.Code, r.Stdout, clip(r.Stderr))
				}
			}
		}
	}
}

// Preserve: a main agent's definition that holds text and declares no
// hook is read as before: its calls meet no guard of Jhon's and pass, and
// SessionStart hands the session its text.
func TestAMainAgentDefinitionWithNoHooksIsReadAsBefore(t *testing.T) {
	b, env := wired(t, realShell(t, "bash"))
	rendered(t, b)
	support.Write(t, b.home, map[string]string{".claude/agents/jhon-orchestrator.md": "---\nname: jhon-orchestrator\n---\n# Orchestrator\n"})
	if r, _ := agentCall(t, b, env, "PreToolUse", "Read", `{"file_path":"src/a.txt"}`, mainAgent); r.Code != 0 || r.Stdout != "" || r.Stderr != "" {
		t.Errorf("PreToolUse: exit %d, stdout %q, stderr %q; want the call let through with nothing said", r.Code, r.Stdout, r.Stderr)
	}
	payload := `{"hook_event_name":"SessionStart","source":"clear","session_id":"s","cwd":` + quoted(t, b.proj) + mainAgent + `}`
	r := support.Run(t, support.Cmd{Args: []string{support.Bin(t), "hook", "agent-hooks", "jhon-orchestrator", "--part", "1"}, Env: env, Stdin: payload})
	if r.Code != 0 || r.Stderr != "" || !strings.Contains(r.Stdout, `"hookEventName":"SessionStart"`) || !strings.Contains(r.Stdout, "# Orchestrator") {
		t.Errorf("SessionStart: exit %d, stdout %q, stderr %q; want part 1 with the definition's text", r.Code, clip(r.Stdout), r.Stderr)
	}
}
