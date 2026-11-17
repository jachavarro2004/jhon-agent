package hook

import (
	"bytes"
	"io"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
)

// A Recorder records one event: its name, severity (INFO or WARN), and
// attributes. Telemetry is an opt-in integration the hooks never import
// (T55): the cli hands one in when integrations.telemetry is on.
type Recorder interface {
	Record(name, severity string, attrs map[string]any)
}

// Recorded runs hook name and, given a recorder, records its decision as a
// jhon.hook event: allow, deny (exit 2, or a JSON deny), ask, or context (a
// note for the agent), with its reason, its duration, and what the payload
// says of the call. Both runtimes run every guard through `jhon hook`, so
// this is every guard decision either makes. With no recorder (nil, the
// default) it only runs the hook.
func Recorded(name string, args []string, h Handler, x IO, rec Recorder) int {
	if rec == nil {
		return h(args, x)
	}
	in, _ := io.ReadAll(x.In)
	var out, errw bytes.Buffer
	start := time.Now()
	code := h(args, IO{In: bytes.NewReader(in), Out: io.MultiWriter(x.Out, &out), Err: io.MultiWriter(x.Err, &errw)})
	took := time.Since(start)
	p, _ := read(bytes.NewReader(in))
	decision, reason := decided(code, out.String(), errw.String())
	attrs := map[string]any{
		"jhon.hook.name":    name,
		"jhon.decision":     decision,
		"process.exit.code": code,
		"jhon.duration_ms":  took.Milliseconds(),
	}
	if len(args) > 0 {
		attrs["jhon.hook.args"] = strings.Join(args, " ")
	}
	if reason != "" {
		attrs["jhon.reason"] = clip(reason, 300)
	}
	for key, field := range map[string]string{"session.id": "session_id", "gen_ai.tool.name": "tool_name", "jhon.hook.event": "hook_event_name"} {
		if v := p.Str(field); v != "" {
			attrs[key] = v
		}
	}
	agent := strings.TrimSpace(p.Str("agent_type"))
	if agent == "" {
		agent = "main"
	}
	attrs["gen_ai.agent.name"] = agent
	severity := "INFO"
	if decision == "deny" {
		severity = "WARN"
	}
	rec.Record("jhon.hook", severity, attrs)
	return code
}

var (
	decisionField = lazyre.New(`"permissionDecision"\s*:\s*"(allow|deny|ask)"`)
	reasonField   = lazyre.New(`"(?:permissionDecisionReason|reason)"\s*:\s*"((?:[^"\\]|\\.)*)"`)
)

// decided is what a hook's exit code and output decided, and why.
func decided(code int, stdout, stderr string) (string, string) {
	why := ""
	if m := reasonField.FindStringSubmatch(stdout); m != nil {
		why = m[1]
	}
	switch {
	case code == 2:
		return "deny", strings.TrimSpace(stderr)
	case code != 0:
		return "error", strings.TrimSpace(stderr)
	}
	if m := decisionField.FindStringSubmatch(stdout); m != nil {
		return m[1], why
	}
	if strings.Contains(stdout, `"decision":"block"`) || strings.Contains(stdout, `"decision": "block"`) {
		return "deny", why
	}
	if strings.Contains(stdout, "additionalContext") {
		return "context", ""
	}
	return "allow", ""
}

// clip is s cut to n bytes at a rune boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }
