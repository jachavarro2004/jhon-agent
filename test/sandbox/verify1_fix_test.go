// What stays of the corrections of the first independent verify of T128b
// and T128c (L917): a payload reads as one tool to the guards and to the
// wrap (C6), and no command inside the sandbox writes Jhon's settings.
package sandbox

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// rendered writes the scratch home's agents as a render leaves them: the
// main agent, with the guards of a writer with Bash and a hook for every
// other event, and a sub-agent of Jhon's with guards of its own.
func rendered(t *testing.T, b box) {
	t.Helper()
	hook := func(words ...string) string {
		return quoted(t, "'"+support.Bin(t)+"' 'hook' '"+strings.Join(words, "' '")+"'")
	}
	entry := func(matcher, command string) string {
		return "    - matcher: " + quoted(t, matcher) + "\n      hooks:\n        - type: command\n          command: " + command + "\n"
	}
	tools := "Read|Grep|Glob|Edit|MultiEdit|Write|NotebookEdit|Bash|Agent"
	support.Write(t, b.home, map[string]string{
		".claude/agents/jhon-orchestrator.md": "---\nname: jhon-orchestrator\nhooks:\n  PreToolUse:\n" +
			entry(tools, hook("pre-tool", "--agent", "jhon-orchestrator", "safety-guard", "vcs-guard", ".git", "secrets-guard")) +
			"  PostToolUse:\n" + entry("Edit|Write|Bash", hook("no-such-hook")) +
			"  Stop:\n    - hooks:\n        - type: command\n          command: " + hook("no-such-hook") + "\n---\n# Orchestrator\n",
		".claude/agents/worker.md": "---\nname: worker\nhooks:\n  PreToolUse:\n" +
			entry(tools, hook("pre-tool", "--agent", "worker", "vcs-guard", ".git", "secrets-guard")) + "---\nbody\n",
	})
}

// agentCall is `jhon hook agent-hooks jhon-orchestrator` for one event of
// who, as the plugin's entry runs it: who is the payload's caller fields.
func agentCall(t *testing.T, b box, env []string, event, tool, input, who string) (support.Result, answer) {
	t.Helper()
	payload := `{"hook_event_name":` + quoted(t, event) + `,"session_id":"s","cwd":` + quoted(t, b.proj) + `,"tool_name":` + quoted(t, tool) + `,"tool_input":` + input + who + `}`
	r := support.Run(t, support.Cmd{Args: []string{support.Bin(t), "hook", "agent-hooks", "jhon-orchestrator"}, Env: env, Stdin: payload})
	var a answer
	if r.Stdout != "" {
		if err := json.Unmarshal([]byte(r.Stdout), &a); err != nil {
			t.Fatalf("agent-hooks printed %q: %v", r.Stdout, err)
		}
	}
	return r, a
}

const (
	mainAgent = `,"agent_type":"jhon-orchestrator","permission_mode":"bypassPermissions"`
	subOf     = `,"agent_id":"a1","permission_mode":"bypassPermissions","agent_type":`
)

// A key in another case is another key (N3): the wrap reads the payload as
// the guards do, so a call they judged as Bash is wrapped as Bash.
func TestAKeyInAnotherCaseDoesNotChangeTheToolTheWrapReads(t *testing.T) {
	bash := realShell(t, "bash")
	b, env := wired(t, bash)
	for name, payload := range map[string]string{
		"TOOL_NAME after":  `"tool_name":"Bash","TOOL_NAME":"Read","tool_input":{"command":"ls src"}`,
		"TOOL_NAME before": `"TOOL_NAME":"Read","tool_name":"Bash","tool_input":{"command":"ls src"}`,
		"Tool_Input after": `"tool_name":"Bash","tool_input":{"command":"ls src"},"Tool_Input":{"file_path":"x"}`,
		"CWD after":        `"tool_name":"Bash","tool_input":{"command":"ls src"},"CWD":"/"`,
	} {
		stdin := `{"hook_event_name":"PreToolUse","session_id":"s","cwd":` + quoted(t, b.proj) + `,` + payload + `,"agent_id":"a1","agent_type":"worker","permission_mode":"bypassPermissions"}`
		r := support.Run(t, support.Cmd{Args: append([]string{support.Bin(t), "hook", "pre-tool", "--agent", "worker"}, bashGuards...), Env: env, Stdin: stdin})
		var a answer
		if r.Code != 0 || r.Stderr != "" || json.Unmarshal([]byte(r.Stdout), &a) != nil || a.Out.Decision != "allow" || a.Out.Updated == nil {
			t.Errorf("%s: exit %d, stderr %q, stdout %q; want the Bash call wrapped", name, r.Code, r.Stderr, r.Stdout)
			continue
		}
		wantBash(t, a.command(t), b.proj, bash, "ls src")
		if len(a.Out.Updated) != 1 {
			t.Errorf("%s: the input is %v; want the command alone", name, a.Out.Updated)
		}
	}
	// The guards read the same keys: a refused command is refused whatever the other key says.
	stdin := `{"hook_event_name":"PreToolUse","cwd":` + quoted(t, b.proj) + `,"tool_name":"Bash","TOOL_NAME":"Read","tool_input":{"command":"git reset --hard"},"agent_id":"a1","agent_type":"worker"}`
	if r := support.Run(t, support.Cmd{Args: append([]string{support.Bin(t), "hook", "pre-tool", "--agent", "worker"}, bashGuards...), Env: env, Stdin: stdin}); r.Code != 2 || r.Stdout != "" {
		t.Errorf("a refused command with TOOL_NAME: %+v; want the refusal", r)
	}
}

// Inside the sandbox no command writes Jhon's settings, by any of the ways
// a command changes a file: the work profile shows the file and its folder
// read-only.
func TestNoCommandInTheWorkProfileWritesTheSettings(t *testing.T) {
	b := newBox(t)
	const on = "schema = 1\n\n[sandbox]\nmode = \"on\"\n"
	settings := b.settings(t, on)
	for name, script := range map[string]string{
		"overwrite":      `printf 'schema = 1\n\n[sandbox]\nmode = "off"\n' > "$HOME/.config/jhon/settings.toml"`,
		"append":         `echo 'x = 1' >> "$HOME/.config/jhon/settings.toml"`,
		"edit in place":  `sed -i 's/on/off/' "$HOME/.config/jhon/settings.toml"`,
		"remove":         `rm -f "$HOME/.config/jhon/settings.toml" && test ! -e "$HOME/.config/jhon/settings.toml"`,
		"rename over it": `echo 'schema = 1' > "$TMPDIR/new.toml" && mv "$TMPDIR/new.toml" "$HOME/.config/jhon/settings.toml"`,
		"its own CLI":    support.Bin(t) + ` settings set sandbox.mode off`,
	} {
		r := b.sh(t, b.proj, "work", script)
		if r.Code == 0 || r.Stdout != "" {
			t.Errorf("%s: exit %d, stdout %q, stderr %q; want a failure", name, r.Code, r.Stdout, r.Stderr)
		}
		if got := support.Read(t, settings); got != on {
			t.Fatalf("%s: the settings now hold %q", name, got)
		}
	}
}
