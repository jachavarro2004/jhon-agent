package sandbox

// T128f slice 8b (L1379, under L1366: do not limit the user): with no owner
// value for sandbox.mode, the default is on only where the probe starts a
// sandbox. A bubblewrap that passes Check and then cannot start (AppArmor
// blocking user namespaces, as on Ubuntu 24.04, or one too old) leaves the
// default off: the status gives the probe's reason and fix, the model's
// commands run unsandboxed and doctor warns. An owner's explicit on still
// refuses every command, and doctor fails. A hook call starts bubblewrap at
// most once.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// failingBody is a bwrap that fails as where user namespaces are blocked.
const failingBody = "echo 'bwrap: No permissions to create a new namespace' >&2\nexit 1\n"

// countedBwrap is env with a bwrap first on PATH, in a folder no sandboxed
// command may write, that logs each start to the returned file and then
// runs body: failingBody, the real bwrap, or one that hangs.
func countedBwrap(t *testing.T, b box, name, body string) (env []string, starts string) {
	t.Helper()
	bin := filepath.Join(b.base, name)
	starts = filepath.Join(b.base, name+".starts")
	support.Write(t, bin, map[string]string{"bwrap": "#!/bin/sh\necho start >> " + starts + "\n" + body})
	if err := os.Chmod(filepath.Join(bin, "bwrap"), 0o755); err != nil {
		t.Fatal(err)
	}
	return append(append([]string{}, b.env...), "PATH="+bin+":"+os.Getenv("PATH"), "JHON_SANDBOX_PROJECT="+b.proj), starts
}

// startsIn is how many times the counted bwrap started since the last call.
func startsIn(t *testing.T, file string) int {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "start\n")
}

func TestTheDefaultModeIsOffWhereBubblewrapCannotStart(t *testing.T) {
	b := newBox(t)
	env, starts := countedBwrap(t, b, "failing-counted", failingBody)
	const reason = "bubblewrap cannot start a sandbox (bwrap: No permissions to create a new namespace)"
	const fix = "turn unprivileged user namespaces on (sysctl kernel.unprivileged_userns_clone=1 and user.max_user_namespaces above 0; where AppArmor restricts them, a profile that lets bwrap use them)"
	last := func(s string) string { l := strings.Split(strings.TrimSpace(s), "\n"); return l[len(l)-1] }
	b.settings(t, "[sandbox]\nmode = \"off\"\n")
	off := support.Jhon(t, b.proj, env, "doctor")
	startsIn(t, starts)

	for _, text := range []string{"", "schema = 1\n"} {
		b.settings(t, text)
		r := support.Jhon(t, b.proj, env, "sandbox", "status", "--json")
		var s struct {
			Mode     string `json:"mode"`
			Source   string `json:"mode_source"`
			Why      string `json:"mode_reason"`
			CanRun   bool   `json:"can_run"`
			Reason   string `json:"reason"`
			Fix      string `json:"fix"`
			TimedOut *bool  `json:"timed_out"`
		}
		if r.Code != 0 || json.Unmarshal([]byte(r.Stdout), &s) != nil || s.Mode != "off" || s.TimedOut == nil || *s.TimedOut || s.Source != "default" || s.Why != reason+"; fix: "+fix || s.CanRun || s.Reason != reason || s.Fix != fix {
			t.Errorf("%q: status --json: exit %d, %q; want off from the default with the probe's reason and fix", text, r.Code, r.Stdout)
		}
		if n := startsIn(t, starts); n != 1 {
			t.Errorf("%q: status --json started bwrap %d times; want once", text, n)
		}
		row := "\nsandbox\n  warn  sandbox off by default; it cannot run here: " + reason + "; fix: " + fix + " to turn the sandbox on; the model's commands run unsandboxed\n"
		d := support.Jhon(t, b.proj, env, "doctor")
		if !strings.Contains(d.Stdout, row) || d.Code != off.Code || last(d.Stdout) != last(off.Stdout) {
			t.Errorf("%q: doctor: exit %d, %q; want the row %q and the verdict with the mode off (exit %d, %q)\n%s", text, d.Code, last(d.Stdout), row, off.Code, last(off.Stdout), d.Out())
		}
		startsIn(t, starts)
		h, _ := preTool(t, b, env, []string{"safety-guard"}, "Bash", `{"command":"ls"}`, true)
		// Unsandboxed, as it came but for the agent's marker (T219 S9).
		if h.Code != 0 || h.Stderr != "" || h.Stdout != `{"hookSpecificOutput":{"hookEventName":"PreToolUse","updatedInput":{"command":"export JHON_AGENT=1\nls"}}}` {
			t.Errorf("%q: pre-tool Bash: exit %d, stdout %q, stderr %q; want it run as it came, unsandboxed, with the agent's marker", text, h.Code, h.Stdout, h.Stderr)
		}
		if n := startsIn(t, starts); n != 1 {
			t.Errorf("%q: the hook call started bwrap %d times; want once", text, n)
		}
	}

	// The owner's explicit on: refused with the probe's fix; doctor fails.
	b.settings(t, "[sandbox]\nmode = \"on\"\n")
	d := support.Jhon(t, b.proj, env, "doctor")
	row := "\nsandbox\n  FAIL  sandbox on; it cannot run here: " + reason + "; fix: " + fix + "; until then every command of the model is refused\n"
	if d.Code != 1 || !strings.Contains(d.Stdout, row) {
		t.Errorf("explicit on: doctor: exit %d; want 1 and the row %q\n%s", d.Code, row, d.Out())
	}
	startsIn(t, starts)
	h, _ := preTool(t, b, env, []string{"safety-guard"}, "Bash", `{"command":"ls"}`, true)
	refusal := "sandbox: sandbox.mode is on and the sandbox cannot run here: " + reason + "; the call is refused; fix: " + fix + "\n"
	if h.Code != 2 || h.Stdout != "" || h.Stderr != refusal {
		t.Errorf("explicit on: pre-tool Bash: exit %d, stdout %q, stderr %q; want 2 and %q", h.Code, h.Stdout, h.Stderr, refusal)
	}
	if n := startsIn(t, starts); n != 1 {
		t.Errorf("explicit on: the hook call started bwrap %d times; want once", n)
	}
}

// Where the sandbox starts, the default is on: the hook call wraps the
// command after starting bubblewrap once, and status starts it once.
func TestTheDefaultModeProbesOncePerCall(t *testing.T) {
	b := newBox(t)
	real, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip(err)
	}
	env, starts := countedBwrap(t, b, "real-counted", "exec "+real+" \"$@\"\n")
	r := support.Jhon(t, b.proj, env, "sandbox", "status", "--json")
	var s struct {
		Mode   string `json:"mode"`
		CanRun bool   `json:"can_run"`
	}
	if r.Code != 0 || json.Unmarshal([]byte(r.Stdout), &s) != nil || s.Mode != "on" || !s.CanRun {
		t.Errorf("status --json: exit %d, %q; want on, able to run", r.Code, r.Stdout)
	}
	if n := startsIn(t, starts); n != 1 {
		t.Errorf("status --json started bwrap %d times; want once", n)
	}
	h, a := preTool(t, b, env, []string{"safety-guard"}, "Bash", `{"command":"ls"}`, true)
	if h.Code != 0 || !strings.Contains(a.command(t), " sandbox run --project ") {
		t.Errorf("pre-tool Bash: exit %d, %q; want the wrapped command", h.Code, h.Stdout)
	}
	if n := startsIn(t, starts); n != 1 {
		t.Errorf("the hook call started bwrap %d times; want once", n)
	}
}

// T128f slice 8c (L1382): a probe that times out is no verdict. With no
// owner value the default stays on: the command is wrapped as usual, and
// `jhon sandbox run` fails visibly if bubblewrap really cannot start. Only
// a real start failure turns the default off.
func TestAProbeTimeoutLeavesTheDefaultOn(t *testing.T) {
	b := newBox(t)
	env, starts := countedBwrap(t, b, "hanging-counted", "exec sleep 30\n")
	r := support.Jhon(t, b.proj, env, "sandbox", "status", "--json")
	var s struct {
		Mode     string `json:"mode"`
		Source   string `json:"mode_source"`
		Why      string `json:"mode_reason"`
		CanRun   bool   `json:"can_run"`
		TimedOut *bool  `json:"timed_out"`
	}
	// can_run stays false, truthfully; timed_out tells pi to wrap (L1383).
	if r.Code != 0 || json.Unmarshal([]byte(r.Stdout), &s) != nil || s.Mode != "on" || s.Source != "default" || s.Why != "" || s.CanRun || s.TimedOut == nil || !*s.TimedOut {
		t.Errorf("status --json: exit %d, %q; want on from the default, with no mode_reason, can_run false and timed_out true", r.Code, r.Stdout)
	}
	if n := startsIn(t, starts); n != 1 {
		t.Errorf("status --json started bwrap %d times; want once", n)
	}
	// Doctor warns that the probe timed out; it is no problem.
	d := support.Jhon(t, b.proj, env, "doctor")
	if !strings.Contains(d.Stdout, "\nsandbox\n  warn  sandbox on; it cannot run here: bubblewrap did not start a sandbox within 5s") || strings.Contains(d.Stdout, "until then every command of the model is refused") {
		t.Errorf("doctor with a probe that times out:\n%s", d.Out())
	}
	startsIn(t, starts)
	h, a := preTool(t, b, env, []string{"safety-guard"}, "Bash", `{"command":"ls"}`, true)
	if h.Code != 0 || h.Stderr != "" || a.Out.Updated == nil || !strings.Contains(a.command(t), " sandbox run --project ") {
		t.Errorf("pre-tool Bash: exit %d, stdout %q, stderr %q; want the command wrapped, never run unwrapped", h.Code, h.Stdout, h.Stderr)
	}
	if n := startsIn(t, starts); n != 1 {
		t.Errorf("the hook call started bwrap %d times; want once", n)
	}
}
