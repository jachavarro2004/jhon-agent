// The Claude Code wiring (T128c, L894): with sandbox.mode on, `jhon hook
// pre-tool` answers a Bash or Monitor call with updatedInput, the same input
// with its command run through `jhon sandbox run` in the project `jhon
// start` pinned. The guards judged the command as the model wrote it; the
// runtime gets their decision with the changed call.
package sandbox

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// wired is a box with sandbox.mode on, the project pinned as `jhon start`
// pins it, and shell as the session's $SHELL.
func wired(t *testing.T, shell string) (box, []string) {
	t.Helper()
	b := newBox(t)
	b.settings(t, "schema = 1\n\n[sandbox]\nmode = \"on\"\n")
	return b, append(append([]string{}, b.env...), "JHON_SANDBOX_PROJECT="+b.proj, "SHELL="+shell)
}

func realShell(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("skipped: no %s on PATH", name)
	}
	return p
}

// answer is what the hook printed for one call.
type answer struct {
	Out struct {
		Event    string                     `json:"hookEventName"`
		Decision string                     `json:"permissionDecision"`
		Reason   string                     `json:"permissionDecisionReason"`
		Updated  map[string]json.RawMessage `json:"updatedInput"`
		Context  string                     `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

// command is the answer's wrapped command; it fails the test when there is none.
func (a answer) command(t *testing.T) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(a.Out.Updated["command"], &s); err != nil {
		t.Fatalf("no updatedInput command: %v", err)
	}
	return s
}

// preTool runs `jhon hook pre-tool` for an agent's call with guards; the
// payload is Claude Code's, with input as its tool_input JSON.
func preTool(t *testing.T, b box, env []string, guards []string, tool, input string, top bool) (support.Result, answer) {
	t.Helper()
	who := `"agent_id":"a1","agent_type":"worker","permission_mode":"bypassPermissions"`
	if top {
		who = `"permission_mode":"default"`
	}
	payload := `{"hook_event_name":"PreToolUse","session_id":"s","cwd":` + quoted(t, b.proj) + `,"tool_name":` + quoted(t, tool) + `,"tool_input":` + input + `,` + who + `}`
	r := support.Run(t, support.Cmd{Args: append([]string{support.Bin(t), "hook", "pre-tool", "--agent", "worker"}, guards...), Env: env, Stdin: payload})
	var a answer
	if r.Stdout != "" {
		if err := json.Unmarshal([]byte(r.Stdout), &a); err != nil {
			t.Fatalf("the hook printed %q: %v", r.Stdout, err)
		}
	}
	return r, a
}

func quoted(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var bashGuards = []string{"safety-guard", "vcs-guard", ".git", "secrets-guard"}

func TestAnAllowedCommandIsHandedBackWrapped(t *testing.T) {
	bash := realShell(t, "bash")
	b, env := wired(t, bash)
	for _, c := range []struct{ tool, input, others string }{
		{"Bash", `{"command":"echo \"it's\" > made.txt && cat made.txt","description":"say it","timeout":120000,"run_in_background":false,"big":9007199254740993,"nested":{"a":[1,2.50,"x"]}}`,
			`{"big":9007199254740993,"description":"say it","nested":{"a":[1,2.50,"x"]},"run_in_background":false,"timeout":120000}`},
		{"Monitor", `{"command":"echo \"it's\" > made.txt && cat made.txt","description":"watch it","timeout_ms":300000,"persistent":false}`,
			`{"description":"watch it","persistent":false,"timeout_ms":300000}`},
	} {
		t.Run(c.tool, func(t *testing.T) {
			r, a := preTool(t, b, env, bashGuards, c.tool, c.input, false)
			if r.Code != 0 || r.Stderr != "" {
				t.Fatalf("exit %d, stderr %q, stdout %q; want 0", r.Code, r.Stderr, r.Stdout)
			}
			if a.Out.Event != "PreToolUse" || a.Out.Decision != "allow" || a.Out.Reason == "" {
				t.Errorf("the decision is %q (%q) for %q; want the guards' allow, with a reason", a.Out.Decision, a.Out.Reason, a.Out.Event)
			}
			if want := expected(t, c.tool, a.command(t), b.proj, bash, `echo "it's" > made.txt && cat made.txt`); a.command(t) != want {
				t.Errorf("the command is\n %s\nwant\n %s", a.command(t), want)
			}
			// Every other field is kept as it came.
			delete(a.Out.Updated, "command")
			others, err := json.Marshal(a.Out.Updated)
			if err != nil {
				t.Fatal(err)
			}
			if string(others) != c.others {
				t.Errorf("the other fields are %s, want %s", others, c.others)
			}
			// What the runtime then runs is the command, inside the sandbox.
			_, a = preTool(t, b, env, bashGuards, c.tool, c.input, false)
			outside := filepath.Join(b.home, "outside.txt")
			ran := support.Run(t, support.Cmd{Args: []string{bash, "-c", a.command(t)}, Dir: b.proj, Env: env})
			if ran.Code != 0 || ran.Stdout != "it's\n" || support.Read(t, filepath.Join(b.proj, "made.txt")) != "it's\n" {
				t.Errorf("the wrapped command: exit %d, stdout %q, stderr %q", ran.Code, ran.Stdout, ran.Stderr)
			}
			// No guard here judges the write: the sandbox alone refuses it.
			_, a = preTool(t, b, env, []string{"vcs-guard", ".git"}, c.tool, `{"command":"echo x > '`+outside+`'"}`, false)
			if ran := support.Run(t, support.Cmd{Args: []string{bash, "-c", a.command(t)}, Dir: b.proj, Env: env}); ran.Code == 0 || exists(outside) {
				t.Errorf("a write outside the project: exit %d, written %v; want it refused by the sandbox", ran.Code, exists(outside))
			}
		})
	}
}

// The guards judge the command the model wrote, and their decision goes with
// the changed call: an ask still asks, a refusal changes nothing.
func TestTheGuardsDecisionGoesWithTheWrappedCommand(t *testing.T) {
	bash := realShell(t, "bash")
	b, env := wired(t, bash)
	off := append(append([]string{}, env...), "JHON_SETTINGS="+filepath.Join(b.base, "no-settings.toml"))

	was, asked := preTool(t, b, off, bashGuards, "Bash", `{"command":"git push origin main","timeout":5}`, true)
	if was.Code != 0 || asked.Out.Decision != "ask" || asked.Out.Updated != nil {
		t.Fatalf("with the mode off: exit %d, %q; want the guard's ask alone", was.Code, was.Stdout)
	}
	r, a := preTool(t, b, env, bashGuards, "Bash", `{"command":"git push origin main","timeout":5}`, true)
	if r.Code != 0 || r.Stderr != "" || a.Out.Decision != "ask" || a.Out.Reason != asked.Out.Reason {
		t.Errorf("an ask: exit %d, stderr %q, decision %q (%q); want the ask, with the reason %q", r.Code, r.Stderr, a.Out.Decision, a.Out.Reason, asked.Out.Reason)
	}
	if want := expected(t, "Bash", a.command(t), b.proj, bash, "git push origin main"); a.command(t) != want || string(a.Out.Updated["timeout"]) != "5" {
		t.Errorf("an ask's input is %v, want the command %s and the timeout kept", a.Out.Updated, want)
	}

	refusedOff, _ := preTool(t, b, off, bashGuards, "Bash", `{"command":"git reset --hard"}`, false)
	refused, _ := preTool(t, b, env, bashGuards, "Bash", `{"command":"git reset --hard"}`, false)
	if refused != refusedOff || refused.Code != 2 || refused.Stdout != "" {
		t.Errorf("a refusal: %+v; with the mode off %+v; want the same, exit 2 and no stdout", refused, refusedOff)
	}
}

// recorder is a fake `bash` that writes the command the inner shell would
// run, byte for byte, to into: the one word after -c, or, for a call whose
// directory is followed (-c <script> <name> <command>), what the script
// hands eval after one blank, which the real bash that runs the script
// records in eval's place.
func recorder(t *testing.T, b box, into string) string {
	t.Helper()
	dir := filepath.Join(b.base, "recorder")
	support.Write(t, dir, map[string]string{"bash": "#!" + realShell(t, "bash") + "\n[ \"$1\" = -c ] || exit 97\nshift\n[ \"$1\" = -- ] && shift\n" +
		"if [ $# -eq 1 ]; then printf '%s' \"$1\" > '" + into + "'; exit; fi\n[ $# -eq 3 ] || exit 98\n" +
		"script=$1; shift 2\neval() { [ $# -eq 1 ] && [ \"${1%\"${1# }\"}\" = ' ' ] || exit 99; printf '%s' \"${1# }\" > '" + into + "'; }\nbuiltin eval \"$script\"\n"})
	if err := os.Chmod(filepath.Join(dir, "bash"), 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "bash")
}

// hostile are commands whose every byte must reach the inner shell.
var hostile = map[string]string{
	"single quotes":      `echo 'it'"'"'s' ''' '\''`,
	"only a quote":       `'`,
	"double quotes":      `echo "a \"b\" $HOME"`,
	"backslashes":        `echo \\ \n \' \" \` + "\\\n" + `x\`,
	"newlines":           "echo a\n\necho b\n",
	"a substitution":     `echo $(printf %s "$(id -u)") ${HOME:-x} $((1+1))`,
	"backticks":          "echo `echo \\`date\\``",
	"a leading dash":     `-n echo x`,
	"a leading plus":     `+x`,
	"a lone double dash": `--`,
	"empty":              ``,
	"blanks":             "  \t ",
	"non-ASCII":          "echo 'ñandú ☃ 日本語' \u00a0 \u2028 \U0001F600 \x7f",
	"history and globs":  `echo !! !$ * ? [a-z] {a,b} ~ # ; & | < > ( )`,
	"a wrap of its own":  `jhon sandbox run --project '/' --profile work -- sh -c 'echo x'`,
	"a very long line":   "echo " + strings.Repeat("a'b\"c\\ ", 12000),
}

func TestEveryByteOfTheCommandReachesTheInnerShell(t *testing.T) {
	b := newBox(t)
	b.settings(t, "schema = 1\n\n[sandbox]\nmode = \"on\"\n")
	into := filepath.Join(b.proj, "received")
	env := append(append([]string{}, b.env...), "JHON_SANDBOX_PROJECT="+b.proj, "SHELL="+recorder(t, b, into))
	var outer []string
	for _, name := range []string{"sh", "bash", "zsh", "dash"} {
		if p, err := exec.LookPath(name); err == nil {
			outer = append(outer, p)
		}
	}
	for name, command := range hostile {
		// No guard of these judges a Bash call: the escaping is what is tested.
		r, a := preTool(t, b, env, []string{"vcs-guard", ".git"}, "Bash", `{"command":`+quoted(t, command)+`}`, false)
		if r.Code != 0 || a.Out.Decision != "allow" {
			t.Errorf("%s: exit %d, stderr %q, stdout %q; want the wrapped command", name, r.Code, r.Stderr, r.Stdout)
			continue
		}
		for _, shell := range outer {
			os.Remove(into)
			ran := support.Run(t, support.Cmd{Args: []string{shell, "-c", a.command(t)}, Dir: b.proj, Env: env})
			got, err := os.ReadFile(into)
			if ran.Code != 0 || err != nil || string(got) != command {
				t.Errorf("%s through %s: exit %d, stderr %q; the inner shell got %q, want %q", name, filepath.Base(shell), ran.Code, ran.Stderr, clip(string(got)), clip(command))
			}
		}
	}
}

func clip(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// A command that starts with a dash is the shell's command, not its option.
func TestALeadingDashIsACommandForARealShell(t *testing.T) {
	for _, name := range []string{"bash", "zsh"} {
		t.Run(name, func(t *testing.T) {
			shell := realShell(t, name)
			b, env := wired(t, shell)
			_, a := preTool(t, b, env, []string{"vcs-guard", ".git"}, "Bash", `{"command":"-v; echo after"}`, false)
			ran := support.Run(t, support.Cmd{Args: []string{shell, "-c", a.command(t)}, Dir: b.proj, Env: env})
			if ran.Code != 0 || ran.Stdout != "after\n" || !strings.Contains(ran.Stderr, "-v") {
				t.Errorf("exit %d, stdout %q, stderr %q; want -v run as a command, then after", ran.Code, ran.Stdout, ran.Stderr)
			}
		})
	}
}

// A wrap the model wrote is a command like any other: it is wrapped, and a
// sandbox cannot start inside the sandbox, so nothing of it runs.
func TestAWrapTheModelWroteIsWrappedAndCannotStart(t *testing.T) {
	bash := realShell(t, "bash")
	b, env := wired(t, bash)
	marker := filepath.Join(b.proj, "inner-ran")
	inner := " -- sh -c 'echo x > " + marker + "; echo inner ran'"
	for name, command := range map[string]string{
		"the hook's own spelling": support.Bin(t) + " sandbox run --project '" + b.proj + "' --profile work" + inner,
		"another project":         support.Bin(t) + " sandbox run --project " + b.home + " --profile delivery" + inner,
		"no options":              "'" + support.Bin(t) + "' sandbox run" + inner,
		"through env":             "env " + support.Bin(t) + "  sandbox   run" + inner,
	} {
		r, a := preTool(t, b, env, []string{"vcs-guard", ".git"}, "Bash", `{"command":`+quoted(t, command)+`}`, false)
		want := expected(t, "Bash", a.command(t), b.proj, bash, command)
		if r.Code != 0 || a.command(t) != want {
			t.Errorf("%s: exit %d, the command is\n %s\nwant\n %s", name, r.Code, a.command(t), want)
			continue
		}
		ran := support.Run(t, support.Cmd{Args: []string{bash, "-c", a.command(t)}, Dir: b.proj, Env: env})
		if ran.Code == 0 || strings.Contains(ran.Stdout, "inner ran") || exists(marker) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q, marker %v; want the inner sandbox refused and nothing run", name, ran.Code, ran.Stdout, ran.Stderr, exists(marker))
		}
	}
}

// The plugin's hook has no matcher, so it gets a Monitor call as Monitor,
// where updatedInput applies; agent-hooks hands it to the agent's hooks that
// take Bash, as Claude Code does for a Bash matcher.
func TestAMonitorCallReachesTheAgentsBashHooks(t *testing.T) {
	bash := realShell(t, "bash")
	b, env := wired(t, bash)
	command := "'" + support.Bin(t) + "' 'hook' 'pre-tool' '--agent' 'worker' 'safety-guard' 'secrets-guard'"
	support.Write(t, b.home, map[string]string{".claude/agents/worker.md": "---\nname: worker\nhooks:\n  PreToolUse:\n" +
		"    - matcher: \"Read|Grep|Glob|Edit|MultiEdit|Write|NotebookEdit|Bash|Agent\"\n      hooks:\n        - type: command\n          command: " + quoted(t, command) + "\n" +
		"  PostToolUse:\n    - matcher: \"Edit|Write|Bash\"\n      hooks:\n        - type: command\n          command: " + quoted(t, "'"+support.Bin(t)+"' 'hook' 'no-such-hook'") + "\n---\nbody\n"})
	payload := func(event, tool, input string) string {
		return `{"hook_event_name":` + quoted(t, event) + `,"session_id":"s","cwd":` + quoted(t, b.proj) + `,"tool_name":` + quoted(t, tool) + `,"tool_input":` + input + `,"agent_id":"a1","agent_type":"worker","permission_mode":"bypassPermissions"}`
	}
	run := func(env []string, stdin string) (support.Result, answer) {
		r := support.Run(t, support.Cmd{Args: []string{support.Bin(t), "hook", "agent-hooks", "jhon-orchestrator"}, Env: env, Stdin: stdin})
		var a answer
		if r.Stdout != "" {
			if err := json.Unmarshal([]byte(r.Stdout), &a); err != nil {
				t.Fatalf("agent-hooks printed %q: %v", r.Stdout, err)
			}
		}
		return r, a
	}
	for _, tool := range []string{"Monitor", "Bash"} {
		r, a := run(env, payload("PreToolUse", tool, `{"command":"tail -f build.log","timeout_ms":1000}`))
		want := expected(t, tool, a.command(t), b.proj, bash, "tail -f build.log")
		if r.Code != 0 || r.Stderr != "" || a.Out.Decision != "allow" || a.command(t) != want || string(a.Out.Updated["timeout_ms"]) != "1000" {
			t.Errorf("%s: exit %d, stderr %q, stdout %q; want the wrapped command %s", tool, r.Code, r.Stderr, r.Stdout, want)
		}
	}
	// With the mode off a Monitor call prints nothing, as before.
	off := append(append([]string{}, env...), "JHON_SETTINGS="+filepath.Join(b.base, "no-settings.toml"))
	if r, _ := run(off, payload("PreToolUse", "Monitor", `{"command":"tail -f build.log"}`)); r.Code != 0 || r.Stdout != "" || r.Stderr != "" {
		t.Errorf("Monitor with the mode off: %+v; want nothing printed", r)
	}
	// A PostToolUse hook with a Bash matcher does not run for Monitor.
	if r, _ := run(env, payload("PostToolUse", "Monitor", `{"command":"tail -f build.log"}`)); r.Code != 0 || r.Stdout != "" || r.Stderr != "" {
		t.Errorf("PostToolUse for Monitor: %+v; want no hook run", r)
	}
}

// An allow that carries a guard's note for the agent keeps the note beside
// the wrapped command.
func TestAnAllowsNoteStaysWithTheWrappedCommand(t *testing.T) {
	bash := realShell(t, "bash")
	b, env := wired(t, bash)
	if r := support.Jhon(t, b.proj, b.env, "mode", "yolo"); r.Code != 0 {
		t.Fatalf("jhon mode yolo: %d\n%s", r.Code, r.Out())
	}
	off := append(append([]string{}, env...), "JHON_SETTINGS="+filepath.Join(b.base, "no-settings.toml"))
	for _, c := range []struct{ tool, input string }{
		{"Bash", `{"command":"git push origin main","timeout":5}`},
	} {
		was, noted := preTool(t, b, off, bashGuards, c.tool, c.input, true)
		if was.Code != 0 || !strings.Contains(noted.Out.Context, "allowed by the owner's yolo mode: push: ") || noted.Out.Decision != "" {
			t.Errorf("%s with the mode off: exit %d, %q; want the mode's note and no decision", c.tool, was.Code, was.Stdout)
			continue
		}
		r, a := preTool(t, b, env, bashGuards, c.tool, c.input, true)
		if r.Code != 0 || r.Stderr != "" || a.Out.Decision != "allow" || a.Out.Context != noted.Out.Context {
			t.Errorf("%s: exit %d, stderr %q, decision %q, note %q; want allow with the note %q", c.tool, r.Code, r.Stderr, a.Out.Decision, a.Out.Context, noted.Out.Context)
		}
		if want := expected(t, c.tool, a.command(t), b.proj, bash, "git push origin main"); a.command(t) != want {
			t.Errorf("%s: the command is %s, want %s", c.tool, a.command(t), want)
		}
	}
}
