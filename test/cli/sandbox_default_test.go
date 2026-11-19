package cli

// T128f slice 8 (L1375, under L1366: do not limit the user): with no owner
// value for sandbox.mode, the default turns the sandbox on only where it can
// run. On a Linux PATH without bubblewrap the effective mode is off, from
// the default, with the reason; the model's commands run unsandboxed and
// `jhon doctor` warns with the fix. An owner's explicit on still refuses
// every command with the fix, and doctor fails.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestTheDefaultModeIsOffWhereTheSandboxCannotRun(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("the default reads as off off Linux whatever the host has (D3)")
	}
	m := newMachine(t)
	noBwrap := filepath.Join(m.home, "no-bwrap")
	if err := os.MkdirAll(noBwrap, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(m.home, "settings.toml")
	env := append(append([]string{}, m.env...), "JHON_SETTINGS="+file, "PATH="+noBwrap, "JHON_SANDBOX_PROJECT="+m.proj)
	const reason = "bubblewrap (bwrap) is not on PATH"
	const fix = "install bubblewrap (pacman -S bubblewrap, apt install bubblewrap or dnf install bubblewrap)"
	status := func() (s struct {
		Mode   string `json:"mode"`
		Source string `json:"mode_source"`
		Why    string `json:"mode_reason"`
		CanRun bool   `json:"can_run"`
		Reason string `json:"reason"`
		Fix    string `json:"fix"`
	}) {
		t.Helper()
		r := support.Jhon(t, m.proj, env, "sandbox", "status", "--json")
		if r.Code != 0 || r.Stderr != "" || json.Unmarshal([]byte(r.Stdout), &s) != nil {
			t.Fatalf("status --json: exit %d, %q, stderr %q", r.Code, r.Stdout, r.Stderr)
		}
		return s
	}
	// The doctor's verdict with the owner's off, where the sandbox adds no
	// problem: this scratch machine has others of its own (no sync, no
	// hooksPath), so the verdict is compared, not its exit 0 (test/release
	// TestUserReleaseWithoutCheckout pins exit 0 on a real user install).
	last := func(s string) string { l := strings.Split(strings.TrimSpace(s), "\n"); return l[len(l)-1] }
	support.Write(t, m.home, map[string]string{"settings.toml": "[sandbox]\nmode = \"off\"\n"})
	off := support.Jhon(t, m.proj, env, "doctor")
	payload := map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": map[string]any{"command": "ls"}, "cwd": m.proj, "session_id": "s", "permission_mode": "default"}
	guards := []string{"--agent", "worker", "safety-guard", "vcs-guard", ".git", "secrets-guard"}

	// No owner value: off by default, with the reason.
	for _, text := range []string{"", "schema = 1\n"} {
		support.Write(t, m.home, map[string]string{"settings.toml": text})
		if s := status(); s.Mode != "off" || s.Source != "default" || s.Why != reason+"; fix: "+fix || s.CanRun || s.Reason != reason || s.Fix != fix {
			t.Errorf("%q: status --json %+v; want off from the default, with the reason and fix", text, s)
		}
		want := "sandbox off by default; it cannot run here: " + reason + "; fix: " + fix + " to turn the sandbox on; project " + m.proj + "\n"
		if r := support.Jhon(t, m.proj, env, "sandbox", "status"); r.Code != 0 || r.Stdout != want {
			t.Errorf("%q: status: exit %d, %q; want %q", text, r.Code, r.Stdout, want)
		}
		r := support.Jhon(t, m.proj, env, "doctor")
		row := "\nsandbox\n  warn  sandbox off by default; it cannot run here: " + reason + "; fix: " + fix + " to turn the sandbox on; the model's commands run unsandboxed\n"
		if !strings.Contains(r.Stdout, row) || r.Code != off.Code || last(r.Stdout) != last(off.Stdout) {
			t.Errorf("%q: doctor: exit %d, %q; want the row %q and the verdict with the mode off: exit %d, %q\n%s", text, r.Code, last(r.Stdout), row, off.Code, last(off.Stdout), r.Out())
		}
		h := support.Hook(t, env, "pre-tool", payload, guards...)
		if h.Code != 0 || h.Stderr != "" || strings.Contains(h.Stdout, "updatedInput") || strings.Contains(h.Stdout, "sandbox") {
			t.Errorf("%q: pre-tool Bash: exit %d, stdout %q, stderr %q; want it run as it came, unsandboxed", text, h.Code, h.Stdout, h.Stderr)
		}
	}

	// The owner's explicit on: refused with the fix, and doctor fails.
	support.Write(t, m.home, map[string]string{"settings.toml": "[sandbox]\nmode = \"on\"\n"})
	if s := status(); s.Mode != "on" || s.Source != "settings" || s.Why != "" || s.CanRun || s.Reason != reason {
		t.Errorf("explicit on: status --json %+v; want on from the settings, unable to run", s)
	}
	r := support.Jhon(t, m.proj, env, "doctor")
	row := "\nsandbox\n  FAIL  sandbox on; it cannot run here: " + reason + "; fix: " + fix + "; until then every command of the model is refused\n"
	if r.Code != 1 || !strings.Contains(r.Stdout, row) || last(r.Stdout) == last(off.Stdout) {
		t.Errorf("explicit on: doctor: exit %d; want 1 and the row %q\n%s", r.Code, row, r.Out())
	}
	h := support.Hook(t, env, "pre-tool", payload, guards...)
	refusal := "sandbox: sandbox.mode is on and the sandbox cannot run here: " + reason + "; the call is refused; fix: " + fix + "\n"
	if h.Code != 2 || h.Stdout != "" || h.Stderr != refusal {
		t.Errorf("explicit on: pre-tool Bash: exit %d, stdout %q, stderr %q; want 2 and %q", h.Code, h.Stdout, h.Stderr, refusal)
	}
}
