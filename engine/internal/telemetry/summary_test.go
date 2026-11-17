package telemetry

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func ev(at string, name string, attrs map[string]any) Event {
	t, _ := time.Parse(time.RFC3339, at)
	return Event{Time: t, Name: name, Attributes: attrs}
}

func TestSummarizeSumsEachKindUp(t *testing.T) {
	events := []Event{
		ev("2026-10-29T10:00:00Z", "jhon.hook", map[string]any{"jhon.hook.name": "safety-guard", "jhon.decision": "allow", "jhon.duration_ms": 10.0}),
		ev("2026-10-29T10:00:01Z", "jhon.hook", map[string]any{"jhon.hook.name": "safety-guard", "jhon.decision": "deny", "jhon.duration_ms": 30.0,
			"gen_ai.agent.name": "worker", "gen_ai.tool.name": "Bash", "jhon.reason": "refused: git push"}),
		ev("2026-10-30T10:00:00Z", "jhon.hook", map[string]any{"jhon.hook.name": "safety-guard", "jhon.decision": "ask", "jhon.duration_ms": 20.0}),
		ev("2026-10-30T10:00:01Z", "jhon.hook", map[string]any{"jhon.hook.name": "route-guard", "jhon.decision": "context"}),
		ev("2026-10-30T10:01:00Z", "jhon.command", map[string]any{"jhon.command": "executions", "jhon.command.verb": "run", "process.exit.code": 0.0,
			"jhon.duration_ms": 9000.0, "jhon.tests.executions": 3.0, "jhon.tests.passed": 3.0}),
		ev("2026-10-30T10:02:00Z", "jhon.command", map[string]any{"jhon.command": "executions", "jhon.command.verb": "run", "process.exit.code": 1.0,
			"jhon.duration_ms": 5000.0, "jhon.tests.executions": 3.0, "jhon.tests.passed": 2.0}),
		ev("2026-10-30T10:03:00Z", "jhon.command", map[string]any{"jhon.command": "deliver", "jhon.command.verb": "check", "process.exit.code": 0.0, "jhon.delivery.ready": true}),
		ev("2026-10-30T10:04:00Z", "jhon.subagent", map[string]any{"gen_ai.agent.name": "worker", "jhon.outcome": "completed", "jhon.duration_ms": 60000.0, "jhon.turns": 20.0}),
		ev("2026-10-30T10:05:00Z", "jhon.subagent", map[string]any{"gen_ai.agent.name": "worker", "jhon.outcome": "turn-cap", "jhon.duration_ms": 90000.0, "jhon.turns": 40.0}),
		ev("2026-10-30T10:06:00Z", "jhon.compaction", map[string]any{"jhon.context.percent": 61.0}),
		ev("2026-10-30T10:07:00Z", "jhon.unknown", nil),
	}
	s := Summarize(events, 7, 1)
	if s.Events != 11 || s.Skipped != 1 || len(s.Hooks) != 2 || s.Hooks[0].Name != "safety-guard" {
		t.Fatalf("%+v", s)
	}
	if h := s.Hooks[0]; h.Count != 3 || h.Allow != 1 || h.Deny != 1 || h.Ask != 1 || h.P50Ms != 20 || h.P95Ms != 30 {
		t.Errorf("safety-guard: %+v", h)
	}
	if h := s.Hooks[1]; h.Context != 1 || h.P50Ms == h.P50Ms {
		t.Errorf("route-guard, no duration recorded: %+v", h)
	}
	if len(s.Denials) != 1 || s.Denials[0].Agent != "worker" || s.Denials[0].Reason != "refused: git push" {
		t.Errorf("denials: %+v", s.Denials)
	}
	if s.TestRuns != 2 || s.TestsGreen != 1 || s.Checks != 1 || s.Ready != 1 {
		t.Errorf("tests %d/%d, checks %d/%d", s.TestsGreen, s.TestRuns, s.Ready, s.Checks)
	}
	if c := s.Commands[0]; c.Command != "executions" || c.Verb != "run" || c.Count != 2 || c.Failed != 1 || c.P50Ms != 5000 {
		t.Errorf("commands: %+v", s.Commands)
	}
	if x := s.Subagents[0]; x.Count != 2 || x.Completed != 1 || x.TurnCap != 1 || x.P50Ms != 60000 || x.P50Turns != 20 {
		t.Errorf("subagents: %+v", x)
	}
	if len(s.Compactions) != 1 || len(s.PerDay) != 2 || s.PerDay[0] != (Day{"2026-10-29", 2, 1}) || s.PerDay[1] != (Day{"2026-10-30", 9, 0}) {
		t.Errorf("compactions %d, per day %+v", len(s.Compactions), s.PerDay)
	}
	var j map[string]any
	if err := json.Unmarshal([]byte(s.JSON()), &j); err != nil {
		t.Fatal(err)
	}
	if hooks := j["hooks"].([]any); hooks[1].(map[string]any)["p50_ms"] != nil {
		t.Errorf("no duration is null: %v", hooks[1])
	}
	if h := s.Human(); !strings.Contains(h, "subagent  worker") || !strings.Contains(h, "test runs 2, all green 1") || !strings.Contains(h, "denied") {
		t.Errorf("%s", h)
	}
}

// T131 (L905, L908): no shell records the turn-cap outcome any more; an event
// from before, with its `jhon.max_turns`, still counts in the summary, the
// JSON and the text.
func TestAnOldTurnCapEventStillCounts(t *testing.T) {
	s := Summarize([]Event{
		ev("2026-11-14T10:05:00Z", "jhon.subagent", map[string]any{"gen_ai.agent.name": "worker", "jhon.outcome": "turn-cap", "jhon.duration_ms": 90000.0, "jhon.turns": 80.0, "jhon.max_turns": 80.0}),
		ev("2026-11-15T10:05:00Z", "jhon.subagent", map[string]any{"gen_ai.agent.name": "worker", "jhon.outcome": "completed", "jhon.duration_ms": 60000.0, "jhon.turns": 120.0}),
	}, 7, 0)
	if x := s.Subagents[0]; x.Agent != "worker" || x.Count != 2 || x.TurnCap != 1 || x.Completed != 1 || x.Failed != 0 {
		t.Errorf("subagents: %+v", x)
	}
	var j struct {
		Subagents []struct {
			Agent   string `json:"agent"`
			TurnCap int    `json:"turn_cap"`
		} `json:"subagents"`
	}
	if err := json.Unmarshal([]byte(s.JSON()), &j); err != nil {
		t.Fatal(err)
	}
	if len(j.Subagents) != 1 || j.Subagents[0].Agent != "worker" || j.Subagents[0].TurnCap != 1 {
		t.Errorf("json: %+v", j)
	}
	if h := s.Human(); !strings.Contains(h, "completed 1  turn-cap 1  timeout 0") {
		t.Errorf("%s", h)
	}
}

func TestLastReadsItsDaysAndSkipsWhatIsNoEvent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("JHON_TELEMETRY", "on")
	defer func(f func() time.Time) { now = f }(now)
	now = func() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) }
	Record(Event{Time: time.Date(2026, 10, 28, 23, 0, 0, 0, time.UTC), Name: "jhon.hook"})
	Record(Event{Time: time.Date(2026, 10, 29, 1, 0, 0, 0, time.UTC), Name: "jhon.hook"})
	Record(Event{Name: "jhon.command"})
	if s := Last(2); s.Events != 2 || s.Days != 2 {
		t.Errorf("last 2 days: %d events", s.Events)
	}
	if s := Last(0); s.Days != 1 || s.Events != 1 {
		t.Errorf("clamped to today: %+v", s)
	}
	if s := Summarize(nil, 7, 0); !strings.Contains(s.Human(), "nothing recorded yet") || !strings.Contains(s.JSON(), `"hooks":[]`) {
		t.Errorf("%s\n%s", s.Human(), s.JSON())
	}
}
