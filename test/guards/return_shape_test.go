// return-shape: a bounded writer that stops without contracts/efficiency.md's
// return shape is asked for it once, through the release plugin's
// SubagentStop hook (spike test/testdata/spikes/claude-code-subagent-stop).

package guards

import (
	"encoding/json"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

const shaped = "status: success\nsummary: added the timeout\nfiles_changed:\n  - src/a.ts: timeout\nvalidation:\n  - npm test: 3 passed\nrisks: none"

func returnShapeEvent(kv ...any) map[string]any {
	return with(map[string]any{"hook_event_name": "SubagentStop", "agent_type": "worker", "stop_hook_active": false,
		"last_assistant_message": "Done: added the timeout."}, kv...)
}

func returnShape(t *testing.T, payload any) support.Result {
	t.Helper()
	return hook(t, nil, "return-shape", []string{"worker", "jd-fix-agent"}, payload)
}

func TestReturnShapeBlocksProseOnce(t *testing.T) {
	t.Parallel()
	r := returnShape(t, returnShapeEvent())
	if r.Code != 0 {
		t.Fatalf("code = %d: %s", r.Code, r.Stderr)
	}
	var j struct{ Decision, Reason string }
	if err := json.Unmarshal([]byte(r.Stdout), &j); err != nil {
		t.Fatalf("stdout %q: %v", r.Stdout, err)
	}
	if j.Decision != "block" {
		t.Errorf("decision = %q; want block", j.Decision)
	}
	matches(t, j.Reason, `^End with the return shape of contracts/efficiency\.md`, "reason")
	matches(t, j.Reason, `(?m)^status: success \| partial \| blocked$`, "reason")
}

func TestReturnShapeEndsTheWriter(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		why     string
		payload any
	}{
		{"shaped", returnShapeEvent("last_assistant_message", shaped)},
		{"already reminded", returnShapeEvent("stop_hook_active", true)},
		{"another agent", returnShapeEvent("agent_type", "explore")},
		{"another event", returnShapeEvent("hook_event_name", "Stop")},
		{"not JSON", "{ nope"},
	} {
		r := returnShape(t, c.payload)
		if r.Code != 0 || r.Stdout != "" {
			t.Errorf("%s: %d, %q; want 0 and nothing", c.why, r.Code, r.Stdout)
		}
	}
}

func TestReturnShapePluginDeclaresBoundedWriters(t *testing.T) {
	t.Parallel()
	var plugin struct{ Hooks string }
	if err := json.Unmarshal([]byte(support.Read(t, support.Path("runtimes", "claude-code", "plugin", ".claude-plugin", "plugin.json"))), &plugin); err != nil {
		t.Fatal(err)
	}
	if plugin.Hooks != "./hooks/hooks.json" {
		t.Errorf("plugin.hooks = %q", plugin.Hooks)
	}
	var hooks struct {
		Hooks map[string][]struct {
			Hooks []struct{ Command string }
		}
	}
	if err := json.Unmarshal([]byte(support.Read(t, support.Path("runtimes", "claude-code", "plugin", "hooks", "hooks.json"))), &hooks); err != nil {
		t.Fatal(err)
	}
	var events []string
	for k := range hooks.Hooks {
		events = append(events, k)
	}
	slices.Sort(events)
	if want := []string{"PostToolUse", "PreToolUse", "SessionStart", "Stop", "SubagentStop"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v; want %v", events, want)
	}
	// Claude Code 2.1.310 runs no agent's frontmatter hooks: the plugin runs
	// the calling agent's, and SessionStart hands the session its definition
	// in parts that each fit a hook's context.
	agentHooks := `^"\$\{CLAUDE_PLUGIN_ROOT\}/\.\./\.\./\.\./build/jhon" hook agent-hooks jhon-orchestrator`
	for _, ev := range []string{"PreToolUse", "PostToolUse", "Stop"} {
		want := 1
		if ev == "Stop" {
			want = 2
		}
		if len(hooks.Hooks[ev]) != want {
			t.Fatalf("%s has %d entries; want %d", ev, len(hooks.Hooks[ev]), want)
		}
		matches(t, hooks.Hooks[ev][0].Hooks[0].Command, agentHooks+`$`, ev)
	}
	// T135: the main session's Stop also runs announce-stop, its own entry,
	// so its decision is never joined to another hook's output.
	matches(t, hooks.Hooks["Stop"][1].Hooks[0].Command, `^"\$\{CLAUDE_PLUGIN_ROOT\}/\.\./\.\./\.\./build/jhon" hook announce-stop$`, "Stop")
	if len(hooks.Hooks["SessionStart"]) != 4 {
		t.Fatalf("SessionStart has %d entries; want 4", len(hooks.Hooks["SessionStart"]))
	}
	for i, e := range hooks.Hooks["SessionStart"] {
		matches(t, e.Hooks[0].Command, agentHooks+` --part `+strconv.Itoa(i+1)+`$`, "SessionStart")
	}
	command := hooks.Hooks["SubagentStop"][0].Hooks[0].Command
	matches(t, command, `^"\$\{CLAUDE_PLUGIN_ROOT\}/\.\./\.\./\.\./build/jhon" hook return-shape `, "command")

	// An agent of agents.yaml is bounded when `    bounded: true` is among the
	// four-space lines that follow its `  - name:` line.
	nameRe := regexp.MustCompile(`^  - name: ([\w-]+)$`)
	var bounded []string
	name := ""
	for _, l := range strings.Split(support.Read(t, support.Path("prose", "agents.yaml")), "\n") {
		if m := nameRe.FindStringSubmatch(l); m != nil {
			name = m[1]
			continue
		}
		switch {
		case name == "":
		case l == "    bounded: true":
			bounded, name = append(bounded, name), ""
		case !strings.HasPrefix(l, "    ") || strings.HasPrefix(l, "    - "):
			name = ""
		}
	}
	args := strings.Split(command, " ")[3:]
	slices.Sort(args)
	slices.Sort(bounded)
	if !reflect.DeepEqual(args, bounded) {
		t.Errorf("hook args %v; bounded agents %v", args, bounded)
	}
	if !reflect.DeepEqual(bounded, []string{"jd-fix-agent", "worker"}) {
		t.Errorf("bounded = %v; want [jd-fix-agent worker]", bounded)
	}
}
