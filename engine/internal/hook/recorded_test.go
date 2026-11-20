package hook

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestRecordedDecisions(t *testing.T) {
	for _, c := range []struct {
		code             int
		stdout, stderr   string
		decision, reason string
	}{
		{0, "", "", "allow", ""},
		{2, "", "safety-guard: refused: git push\n", "deny", "safety-guard: refused: git push"},
		{0, askJSON("push needs the owner"), "", "ask", "push needs the owner"},
		{0, `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"no"}}`, "", "deny", "no"},
		{0, `{"decision":"block","reason":"keep going"}`, "", "deny", "keep going"},
		{0, `{"hookSpecificOutput":{"additionalContext":"note"}}`, "", "context", ""},
		{1, "", "boom", "error", "boom"},
	} {
		d, r := decided(c.code, c.stdout, c.stderr)
		if d != c.decision || r != c.reason {
			t.Errorf("%d %q %q: %s %q, want %s %q", c.code, c.stdout, c.stderr, d, r, c.decision, c.reason)
		}
	}
}

// recording is a Recorder that keeps what it is given.
type recording struct {
	name, severity string
	attrs          map[string]any
	calls          int
}

func (r *recording) Record(name, severity string, attrs map[string]any) {
	r.name, r.severity, r.attrs = name, severity, attrs
	r.calls++
}

func TestRecordedPassesTheHookThroughAndRecordsIt(t *testing.T) {
	h := func(args []string, x IO) int {
		in, _ := io.ReadAll(x.In)
		fmt.Fprint(x.Err, "safety-guard: refused: "+string(in[:5]))
		return 2
	}
	var out, errw bytes.Buffer
	payload := `{"session_id":"s1","tool_name":"Bash","agent_type":"worker","hook_event_name":"PreToolUse"}`
	rec := &recording{}
	code := Recorded("safety-guard", []string{"--agent", "*"}, h, IO{In: strings.NewReader(payload), Out: &out, Err: &errw}, rec)
	if code != 2 || errw.String() != `safety-guard: refused: {"ses` {
		t.Fatalf("the hook's own answer changed: %d %q", code, errw.String())
	}
	a := rec.attrs
	if rec.calls != 1 || rec.name != "jhon.hook" || rec.severity != "WARN" || a["jhon.hook.name"] != "safety-guard" || a["jhon.decision"] != "deny" ||
		a["jhon.hook.args"] != "--agent *" || a["session.id"] != "s1" || a["gen_ai.tool.name"] != "Bash" || a["gen_ai.agent.name"] != "jhon-builder" ||
		a["jhon.hook.event"] != "PreToolUse" || a["process.exit.code"] != 2 || a["jhon.reason"] != `safety-guard: refused: {"ses` {
		t.Errorf("%d %s %s %v", rec.calls, rec.name, rec.severity, a)
	}
}

// With no recorder (telemetry off, the default) the hook runs on the
// caller's own streams and nothing is recorded.
func TestRecordedWithNoRecorderOnlyRunsTheHook(t *testing.T) {
	in := strings.NewReader("{}")
	var out, errw bytes.Buffer
	x := IO{In: in, Out: &out, Err: &errw}
	ran := false
	h := func(args []string, got IO) int {
		ran = true
		if got != x {
			t.Error("the hook got wrapped streams")
		}
		return 0
	}
	if code := Recorded("safety-guard", nil, h, x, nil); code != 0 || !ran {
		t.Errorf("code %d ran %v", code, ran)
	}
}
