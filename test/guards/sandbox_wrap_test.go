package guards

// With sandbox.mode on, `jhon hook pre-tool` hands Claude Code the wrapped
// command of a Bash or Monitor call (T128c, L894; the wrap itself is proven
// under a real bubblewrap in test/sandbox). These are the cases that need no
// sandbox: with the mode off the output is what it was, pi's calls (`--agent
// *`) get nothing new whatever the mode, and with the mode on a call that
// cannot be pinned or sandboxed is refused, never run as it came.

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// wrapGuards are the guards of the calls below: Claude Code's as the render
// names an agent's, pi's as shell/lib/guards.ts names a session's.
var (
	claudeGuards = []string{"--agent", "worker", "safety-guard", "vcs-guard", ".git", "secrets-guard"}
	piGuards     = []string{"--agent", "*", "safety-guard", "vcs-guard", ".git", "secrets-guard"}
)

// sandboxEnv is s's environment with sandbox.mode set in a settings file of
// its own, and extra variables.
func sandboxEnv(t *testing.T, s sgEnv, mode string, extra ...string) []string {
	t.Helper()
	support.Write(t, s.scratch, map[string]string{"settings-" + mode + ".toml": "schema = 1\n\n[sandbox]\nmode = \"" + mode + "\"\n"})
	return append(append(append([]string{}, s.env...), "JHON_SETTINGS="+filepath.Join(s.scratch, "settings-"+mode+".toml")), extra...)
}

// wrapCalls are a refusal, an ask and an allow of the top-level session, and
// a file read: each with what it prints today.
var wrapCalls = []struct {
	name, tool string
	input      map[string]any
	code       int
	decision   string
}{
	{"a refusal", "Bash", cmd("rm .env"), 2, ""},
	{"an ask", "Bash", cmd("git push origin main"), 0, "ask"},
	{"an allow", "Bash", cmd("ls src"), 0, ""},
	{"a monitor", "Monitor", map[string]any{"command": "tail -f build.log", "description": "the build", "timeout_ms": 300000}, 0, ""},
	{"a read", "Read", map[string]any{"file_path": "src/a.go"}, 0, ""},
}

func claudePayload(s sgEnv, tool string, input map[string]any) map[string]any {
	return map[string]any{"hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": input, "cwd": s.p, "session_id": "s", "permission_mode": "default"}
}

func TestSandboxModeOffLeavesThePreToolOutputAsItWas(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	off := sandboxEnv(t, s, "off", "JHON_SANDBOX_PROJECT="+s.p)
	for _, c := range wrapCalls {
		r := hook(t, off, "pre-tool", claudeGuards, claudePayload(s, c.tool, c.input))
		code(t, r, c.code, c.name)
		if strings.Contains(r.Stdout, "updatedInput") || strings.Contains(r.Stdout, `"allow"`) {
			t.Errorf("%s: with the mode off the output carries the sandbox's: %q", c.name, r.Stdout)
		}
		switch {
		case c.code == 2:
			if r.Stdout != "" || !strings.HasPrefix(r.Stderr, "safety-guard: ") {
				t.Errorf("%s: stdout %q, stderr %q; want the guard's own refusal and no stdout", c.name, r.Stdout, r.Stderr)
			}
		case c.decision == "ask":
			d, why, _ := decision(t, r.Stdout)
			if d != "ask" || why == "" || r.Stdout != `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask","permissionDecisionReason":`+js(t, []string{why})[1:len(js(t, []string{why}))-1]+"}}\n" || r.Stderr != "" {
				t.Errorf("%s: stdout %q, stderr %q; want the ask as the guard prints it", c.name, r.Stdout, r.Stderr)
			}
		default:
			if r.Stdout != "" || r.Stderr != "" {
				t.Errorf("%s: stdout %q, stderr %q; want nothing printed", c.name, r.Stdout, r.Stderr)
			}
		}
	}
}

// T128f (L1352 F1): the setting absent is the setting on where the sandbox
// runs, Linux; elsewhere it is off (D3).
func TestSandboxModeAbsentIsOnOnLinux(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	support.Write(t, s.scratch, map[string]string{"settings-empty.toml": ""})
	empty := append(append([]string{}, s.env...), "JHON_SETTINGS="+filepath.Join(s.scratch, "settings-empty.toml"))
	same := "off"
	if runtime.GOOS == "linux" {
		same = "on"
	}
	for _, c := range wrapCalls {
		for _, extra := range [][]string{nil, {"JHON_SANDBOX_PROJECT=" + s.p, "PATH=" + mkdir(t, filepath.Join(s.scratch, "no-bwrap"))}} {
			got := hook(t, append(append([]string{}, empty...), extra...), "pre-tool", claudeGuards, claudePayload(s, c.tool, c.input))
			if want := hook(t, sandboxEnv(t, s, same, extra...), "pre-tool", claudeGuards, claudePayload(s, c.tool, c.input)); got != want {
				t.Errorf("%s %v: with no settings %+v, with the mode %s %+v", c.name, extra, got, same, want)
			}
		}
	}
}

// pi wraps on its own side (shell, T128b): its calls reach this hook as
// `--agent *`, and get what they got, with no updatedInput and no refusal
// for a variable pi's wrap does not need here.
func TestSandboxModeOnChangesNothingForPi(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range wrapCalls {
		// pi's payloads name every command call Bash (shell/lib/guards.ts).
		if c.tool == "Monitor" {
			c.tool = "Bash"
		}
		for _, child := range []bool{false, true} {
			payload := map[string]any{"hook_event_name": "PreToolUse", "tool_name": c.tool, "tool_input": c.input, "cwd": s.p, "session_id": "s"}
			if child {
				payload["agent_id"] = "pi-child"
			} else {
				payload["permission_mode"] = "default"
			}
			was := hook(t, s.env, "pre-tool", piGuards, payload)
			for name, env := range map[string][]string{
				"pinned":   sandboxEnv(t, s, "on", "JHON_SANDBOX_PROJECT="+s.p),
				"unpinned": sandboxEnv(t, s, "on"),
				"no bwrap": sandboxEnv(t, s, "on", "JHON_SANDBOX_PROJECT="+s.p, "PATH="+mkdir(t, filepath.Join(s.scratch, "no-bwrap"))),
			} {
				if name == "no bwrap" {
					// The guards run with the same PATH on both sides.
					if got, want := hook(t, env, "pre-tool", piGuards, payload), hook(t, append(append([]string{}, s.env...), "PATH="+filepath.Join(s.scratch, "no-bwrap")), "pre-tool", piGuards, payload); got != want {
						t.Errorf("%s, %s, child %v: %+v, was %+v", c.name, name, child, got, want)
					}
					continue
				}
				if got := hook(t, env, "pre-tool", piGuards, payload); got != was {
					t.Errorf("%s, %s, child %v: %+v, was %+v", c.name, name, child, got, was)
				}
			}
		}
	}
}

func TestSandboxModeOnRefusesACallItCannotPin(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	support.Write(t, s.scratch, map[string]string{"a-file": "x\n"})
	for name, extra := range map[string][]string{
		"unset":           nil,
		"empty":           {"JHON_SANDBOX_PROJECT="},
		"not absolute":    {"JHON_SANDBOX_PROJECT=proj"},
		"not there":       {"JHON_SANDBOX_PROJECT=" + filepath.Join(s.scratch, "gone")},
		"a file":          {"JHON_SANDBOX_PROJECT=" + filepath.Join(s.scratch, "a-file")},
		"the file system": {"JHON_SANDBOX_PROJECT=/"},
	} {
		for _, c := range wrapCalls[2:4] {
			r := hook(t, sandboxEnv(t, s, "on", extra...), "pre-tool", claudeGuards, claudePayload(s, c.tool, c.input))
			if r.Code != 2 || r.Stdout != "" || strings.Count(r.Stderr, "\n") != 1 ||
				!strings.HasPrefix(r.Stderr, "sandbox: ") || !strings.Contains(r.Stderr, "JHON_SANDBOX_PROJECT") || !strings.Contains(r.Stderr, "fix: start the session with `jhon start`") {
				t.Errorf("%s, %s: exit %d, stdout %q, stderr %q; want 2 and one line naming the variable and the fix", name, c.name, r.Code, r.Stdout, r.Stderr)
			}
		}
	}
	on := sandboxEnv(t, s, "on")
	// The guards judge first: their refusal is the answer, as it was.
	refusal := claudePayload(s, "Bash", cmd("cat .env"))
	if got, was := hook(t, on, "pre-tool", claudeGuards, refusal), hook(t, s.env, "pre-tool", claudeGuards, refusal); got != was || got.Code != 2 {
		t.Errorf("a guard's refusal: %+v, was %+v", got, was)
	}
	// A call that runs no command is not the sandbox's.
	for _, c := range []struct {
		tool  string
		input map[string]any
	}{{"Read", map[string]any{"file_path": "src/a.go"}}, {"Monitor", map[string]any{"ws": map[string]any{"url": "wss://example.invalid/feed"}, "description": "a feed"}}} {
		payload := claudePayload(s, c.tool, c.input)
		if got, was := hook(t, on, "pre-tool", claudeGuards, payload), hook(t, s.env, "pre-tool", claudeGuards, payload); got != was || got.Code != 0 {
			t.Errorf("%s %v: %+v, was %+v", c.tool, c.input, got, was)
		}
	}
	// A command call whose command is no text cannot be wrapped.
	r := hook(t, sandboxEnv(t, s, "on", "JHON_SANDBOX_PROJECT="+s.p), "pre-tool", []string{"--agent", "worker", "vcs-guard", ".git"}, claudePayload(s, "Monitor", map[string]any{"command": 7}))
	if r.Code != 2 || r.Stdout != "" || !strings.HasPrefix(r.Stderr, "sandbox: ") || strings.Count(r.Stderr, "\n") != 1 {
		t.Errorf("a command that is no text: exit %d, stdout %q, stderr %q; want 2 and one line", r.Code, r.Stdout, r.Stderr)
	}
}

func TestSandboxModeOnRefusesWhereTheSandboxCannotRun(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	env := sandboxEnv(t, s, "on", "JHON_SANDBOX_PROJECT="+s.p, "PATH="+mkdir(t, filepath.Join(s.scratch, "no-bwrap")))
	for _, c := range wrapCalls[2:4] {
		r := hook(t, env, "pre-tool", claudeGuards, claudePayload(s, c.tool, c.input))
		want := "sandbox: sandbox.mode is on and the sandbox cannot run here: bubblewrap (bwrap) is not on PATH; the call is refused; fix: install bubblewrap (pacman -S bubblewrap, apt install bubblewrap or dnf install bubblewrap)\n"
		if r.Code != 2 || r.Stdout != "" || r.Stderr != want {
			t.Errorf("%s: exit %d, stdout %q, stderr %q; want 2 and %q", c.name, r.Code, r.Stdout, r.Stderr, want)
		}
	}
}
