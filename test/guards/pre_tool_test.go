package guards

// `jhon hook pre-tool` (docket AY B4) is the guards of one agent in one
// process: each guard whose matcher takes the call judges it, in render
// order, on the one payload. It must decide exactly what the separate hooks
// decide: the first refusal, with its own guard's message (jhon audit
// attributes refusals by that prefix), else the first ask, else nothing.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// preToolGuards are a bounded writer's guards as the render gives them,
// with their arguments: the reference the combined hook is held to. The
// tools each judges are the binary's (`jhon hook pre-tool --matchers`).
var preToolGuards = []struct {
	name string
	args func(fw string) []string
}{
	{"safety-guard", func(string) []string { return nil }},
	{"vcs-guard", func(string) []string { return []string{".git", ".hg", ".svn"} }},
	{"framework-guard", func(fw string) []string { return []string{fw} }},
	{"scope-guard", func(string) []string { return nil }},
}

// guardTools are the tools each guard judges, as the binary states them.
func guardTools(t *testing.T) map[string][]string {
	t.Helper()
	r := hook(t, nil, "pre-tool", []string{"--matchers"}, "")
	if r.Code != 0 {
		t.Fatalf("pre-tool --matchers: %d %s", r.Code, r.Stderr)
	}
	out := map[string][]string{}
	for _, l := range strings.Split(strings.TrimSpace(r.Stdout), "\n") {
		if name, tools, ok := strings.Cut(l, "\t"); ok {
			out[name] = strings.Split(tools, "|")
		}
	}
	return out
}

func TestPreToolDecidesWhatTheSeparateGuardsDecide(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	// The guards' decisions alone: the sandbox's wrap, on by default on
	// Linux (T128f), is sandbox_wrap_test.go's.
	s.env = sandboxEnv(t, s, "off")
	fw := mkdir(t, filepath.Join(s.scratch, "fw"))
	type call struct {
		tool  string
		input map[string]any
	}
	var calls []call
	commands := append(append([]string{}, sgDeny...), sgAllow...)
	for _, a := range sgAsk {
		commands = append(commands, a[0])
	}
	commands = append(commands, "rm -rf "+fw+"/src", "echo x > "+fw+"/a", "git -C "+fw+" commit -m x")
	for _, c := range commands {
		calls = append(calls, call{"Bash", cmd(s.expand(c))})
	}
	for _, tool := range []string{"Edit", "Write", "NotebookEdit"} {
		for _, p := range []string{filepath.Join(s.p, ".git", "config"), filepath.Join(fw, "a.go"), filepath.Join(s.p, "src", "a.go")} {
			calls = append(calls, call{tool, map[string]any{"file_path": p, "notebook_path": p, "content": "x"}})
		}
	}
	tools := guardTools(t)
	combined := []string{"--agent", "worker"}
	for _, g := range preToolGuards {
		combined = append(append(combined, g.name), g.args(fw)...)
	}
	// Each case spawns a guard per matcher and the combined hook: hundreds of
	// independent processes, so the cases run in parallel.
	for i, c := range calls {
		for _, top := range []bool{false, true} {
			t.Run(strconv.Itoa(i)+"/"+strconv.FormatBool(top), func(t *testing.T) {
				t.Parallel()
				decideAlike(t, s, fw, combined, tools, i, c.tool, c.input, top)
			})
		}
	}
}

// decideAlike holds pre-tool to the separate guards on one call, by a
// subagent (top false) or the top-level session.
func decideAlike(t *testing.T, s sgEnv, fw string, combined []string, tools map[string][]string, i int, tool string, input map[string]any, top bool) {
	t.Helper()
	label := strconv.Itoa(i) + " " + tool + " " + js(t, input)
	payload := map[string]any{"tool_name": tool, "tool_input": input, "cwd": s.p, "session_id": "s", "permission_mode": "bypassPermissions"}
	if top {
		label, payload["permission_mode"] = "top "+label, "default"
	} else {
		payload["agent_id"], payload["agent_type"] = "a1", "worker"
	}
	wantCode, wantErr, wantOut := 0, "", ""
	for _, g := range preToolGuards {
		if !contains(tools[g.name], tool) {
			continue
		}
		r := hook(t, s.env, g.name, append([]string{"--agent", "worker"}, g.args(fw)...), payload)
		if r.Code != 0 {
			wantCode, wantErr, wantOut = r.Code, r.Stderr, r.Stdout
			break
		}
		if wantOut == "" {
			wantOut = r.Stdout
		}
	}
	r := hook(t, s.env, "pre-tool", combined, payload)
	got := r.Stdout
	// With the mode off, a command the guards let run carries the agent's
	// marker (T219 S9): the same decision, with updatedInput.
	if command, ok := input["command"].(string); ok && r.Code == 0 && (tool == "Bash" || tool == "Monitor") {
		got, wantOut = unmarked(t, label, got, command), decisionOf(t, wantOut)
	}
	if r.Code != wantCode || r.Stderr != wantErr || got != wantOut {
		t.Errorf("%s:\n got  %d %q %q\n want %d %q %q", label, r.Code, r.Stderr, got, wantCode, wantErr, wantOut)
	}
}

// unmarked is pre-tool's answer to an agent's command with the mode off,
// its updatedInput checked to be the command after the marker line and
// taken out, as decisionOf gives it.
func unmarked(t *testing.T, label, out, command string) string {
	t.Helper()
	var j map[string]map[string]any
	if err := json.Unmarshal([]byte(out), &j); err != nil {
		t.Errorf("%s: %q: %v", label, out, err)
		return out
	}
	in, _ := j["hookSpecificOutput"]["updatedInput"].(map[string]any)
	if c, _ := in["command"].(string); c != "export JHON_AGENT=1\n"+command {
		t.Errorf("%s: the command run is %q", label, c)
	}
	delete(j["hookSpecificOutput"], "updatedInput")
	return js(t, j)
}

// decisionOf is a guard's answer as pre-tool's marked answer carries it:
// its hookSpecificOutput's fields, or the event's name alone for none.
func decisionOf(t *testing.T, out string) string {
	t.Helper()
	j := map[string]map[string]any{"hookSpecificOutput": {"hookEventName": "PreToolUse"}}
	if strings.TrimSpace(out) != "" {
		if err := json.Unmarshal([]byte(out), &j); err != nil {
			return out
		}
	}
	return js(t, j)
}

func TestPreToolRefusesABrokenRender(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	payload := map[string]any{"tool_name": "Bash", "tool_input": cmd("ls"), "cwd": s.p}
	for name, args := range map[string][]string{
		"no owner":        {"safety-guard"},
		"no guard":        {"--agent", "w"},
		"an unknown word": {"--agent", "w", "safety-guard", "nonsense-guard"},
		"args before any": {"--agent", "w", ".git", "vcs-guard"},
	} {
		r := hook(t, s.env, "pre-tool", args, payload)
		code(t, r, 2, name)
		matches(t, r.Stderr, `^pre-tool: `, name)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// `--agent *` scopes a guard to every agent: pi, whose calls name no agent,
// gives one guard every agent's calls, and a
// sub-agent's payload names its agent_type. No guard leaves such a call to
// another agent's hooks, as it would for a guard scoped to one name.
func TestEveryGuardJudgesEveryAgentUnderAgentStar(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	sub := func(tool string, input map[string]any) map[string]any {
		return map[string]any{"tool_name": tool, "tool_input": input, "cwd": s.p, "agent_type": "worker", "agent_id": "a1", "permission_mode": "bypassPermissions"}
	}
	r := hook(t, s.env, "safety-guard", []string{"--agent", "*"}, sub("Bash", cmd("git reset --hard")))
	code(t, r, 2, "safety-guard on a sub-agent's destructive command")
	r = hook(t, s.env, "vcs-guard", []string{"--agent", "*", ".git", ".hg", ".svn"}, sub("Write", map[string]any{"file_path": filepath.Join(s.p, ".git", "config"), "content": "x"}))
	code(t, r, 2, "vcs-guard on a sub-agent's write into .git")
	r = hook(t, s.env, "pre-tool", []string{"--agent", "*", "safety-guard", "vcs-guard", ".git", ".hg", ".svn"}, sub("Bash", cmd("git reset --hard")))
	code(t, r, 2, "pre-tool --agent * on a sub-agent's call")
}

// T134 (L1036): the binary answers an Agent call of the main session the
// guards allow with ONE JSON object: the input as it came with its brief
// followed by the decisions on record and the code map (a fake Engram, a
// fake CodeGraph first on PATH); a sub-agent's own launch is answered as
// before, with nothing. The cases are pinned in engine/internal/hook.
func TestPreToolLaunchesTheMainSessionsAgentWithItsContext(t *testing.T) {
	t.Parallel()
	fakes := realTemp(t)
	support.Write(t, fakes, map[string]string{
		"engram": "#!/bin/sh\nprintf '[1] #41 (decision) — Launch context goes at the end of the brief\\n'\n",
		"codegraph": "#!/bin/sh\ncase \"$1\" in callers) printf '{}' ;; *) printf '%s' " +
			`'[{"node":{"kind":"function","name":"runAgent","filePath":"src/agents.ts","startLine":3,"signature":"()"}}]'` + " ;; esac\n",
	})
	for _, f := range []string{"engram", "codegraph"} {
		if err := os.Chmod(filepath.Join(fakes, f), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p := filepath.Join(realTemp(t), "proj")
	support.Write(t, p, map[string]string{"src/agents.ts": "export function runAgent() {}\n"})
	support.GitRepo(t, p)
	support.Write(t, p, map[string]string{".codegraph/codegraph.db": "", "src/agents.ts": "export function runAgent() { return 1 }\n"})
	env := support.Env("JHON_ENGRAM="+filepath.Join(fakes, "engram"), "PATH="+fakes+string(os.PathListSeparator)+os.Getenv("PATH"), "JHON_ROOT="+support.Root())
	brief := "Fix `runAgent`.\n\n## Allowed edit surfaces\n- src/agents.ts\n"
	call := func(extra map[string]any) support.Result {
		payload := map[string]any{"cwd": p, "permission_mode": "default", "tool_name": "Agent",
			"tool_input": map[string]any{"description": "Fix it", "prompt": brief, "subagent_type": "worker", "model": "sonnet"}}
		for k, v := range extra {
			payload[k] = v
		}
		return hook(t, env, "pre-tool", []string{"--agent", "jhon-orchestrator", "route-guard", "worker"}, payload)
	}
	r := call(nil)
	var answer struct {
		Out struct {
			Decision string         `json:"permissionDecision"`
			Input    map[string]any `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if r.Code != 0 || json.Unmarshal([]byte(r.Stdout), &answer) != nil || answer.Out.Decision != "allow" {
		t.Fatalf("exit %d: %q %q", r.Code, r.Stdout, r.Stderr)
	}
	want := brief + "\n\n## Decisions on record\nAdded at launch from `jhon context --handoff`; search Engram before deciding against one.\n" +
		"- #41 (decision) Launch context goes at the end of the brief\n\n## Code map\n" +
		"Added at launch from CodeGraph for what the brief names; lines are as of the launch.\n- function runAgent src/agents.ts:3 ()"
	in := answer.Out.Input
	if in["prompt"] != want || in["description"] != "Fix it" || in["subagent_type"] != "worker" || in["model"] != "sonnet" || len(in) != 4 {
		t.Errorf("updatedInput: %#v", in)
	}
	if r := call(map[string]any{"agent_id": "a1"}); r.Code != 0 || r.Stdout != "" {
		t.Errorf("a sub-agent's launch: %d %q", r.Code, r.Stdout)
	}
}
