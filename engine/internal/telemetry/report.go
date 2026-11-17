package telemetry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// ms is a duration in milliseconds as JSON holds it: null for none.
func ms(x float64) *float64 {
	if math.IsNaN(x) {
		return nil
	}
	return &x
}

// JSON is the summary as `jhon telemetry --json` prints it and
// /api/telemetry serves it (dashboard/CONTRACT.md).
func (s Summary) JSON() string {
	type hook struct {
		Name    string   `json:"name"`
		Count   int      `json:"count"`
		Allow   int      `json:"allow"`
		Deny    int      `json:"deny"`
		Ask     int      `json:"ask"`
		Context int      `json:"context"`
		Error   int      `json:"error"`
		P50Ms   *float64 `json:"p50_ms"`
		P95Ms   *float64 `json:"p95_ms"`
	}
	type denial struct {
		Time   string `json:"time"`
		Hook   string `json:"hook"`
		Agent  string `json:"agent"`
		Tool   string `json:"tool"`
		Reason string `json:"reason"`
	}
	type command struct {
		Command string   `json:"command"`
		Verb    string   `json:"verb"`
		Count   int      `json:"count"`
		Failed  int      `json:"failed"`
		P50Ms   *float64 `json:"p50_ms"`
	}
	type subagent struct {
		Agent     string   `json:"agent"`
		Count     int      `json:"count"`
		Completed int      `json:"completed"`
		TurnCap   int      `json:"turn_cap"`
		Timeout   int      `json:"timeout"`
		Cancelled int      `json:"cancelled"`
		Failed    int      `json:"failed"`
		P50Ms     *float64 `json:"p50_ms"`
		P50Turns  *float64 `json:"p50_turns"`
	}
	type compaction struct {
		Time    string   `json:"time"`
		Percent *float64 `json:"percent"`
	}
	type day struct {
		Day    string `json:"day"`
		Events int    `json:"events"`
		Denies int    `json:"denies"`
	}
	out := struct {
		Days        int          `json:"days"`
		Events      int          `json:"events"`
		Skipped     int          `json:"skipped"`
		Hooks       []hook       `json:"hooks"`
		Denials     []denial     `json:"denials"`
		Commands    []command    `json:"commands"`
		Tests       any          `json:"tests"`
		Deliveries  any          `json:"deliveries"`
		Subagents   []subagent   `json:"subagents"`
		Compactions []compaction `json:"compactions"`
		PerDay      []day        `json:"per_day"`
	}{Days: s.Days, Events: s.Events, Skipped: s.Skipped, Hooks: []hook{}, Denials: []denial{}, Commands: []command{},
		Tests: map[string]int{"runs": s.TestRuns, "green": s.TestsGreen}, Deliveries: map[string]int{"checks": s.Checks, "ready": s.Ready},
		Subagents: []subagent{}, Compactions: []compaction{}, PerDay: []day{}}
	for _, h := range s.Hooks {
		out.Hooks = append(out.Hooks, hook{h.Name, h.Count, h.Allow, h.Deny, h.Ask, h.Context, h.Error, ms(h.P50Ms), ms(h.P95Ms)})
	}
	for _, d := range s.Denials {
		out.Denials = append(out.Denials, denial{stamp(d.Time), d.Hook, d.Agent, d.Tool, d.Reason})
	}
	for _, c := range s.Commands {
		out.Commands = append(out.Commands, command{c.Command, c.Verb, c.Count, c.Failed, ms(c.P50Ms)})
	}
	for _, x := range s.Subagents {
		out.Subagents = append(out.Subagents, subagent{x.Agent, x.Count, x.Completed, x.TurnCap, x.Timeout, x.Cancelled, x.Failed, ms(x.P50Ms), ms(x.P50Turns)})
	}
	for _, c := range s.Compactions {
		p := math.NaN()
		if v, ok := c.Attributes["jhon.context.percent"].(float64); ok {
			p = v
		}
		out.Compactions = append(out.Compactions, compaction{stamp(c.Time), ms(p)})
	}
	for _, d := range s.PerDay {
		out.PerDay = append(out.PerDay, day(d))
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(out)
	return strings.TrimSuffix(b.String(), "\n")
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// human is a duration in milliseconds as a person reads it, "-" for none.
func human(x float64) string {
	switch {
	case math.IsNaN(x):
		return "-"
	case x < 1000:
		return fmt.Sprintf("%.0fms", x)
	case x < 60000:
		return fmt.Sprintf("%.1fs", x/1000)
	}
	return fmt.Sprintf("%.1fm", x/60000)
}

// Human is the summary as `jhon telemetry` prints it.
func (s Summary) Human() string {
	out := []string{fmt.Sprintf("telemetry  last %d day(s)  %d event(s)  %s", s.Days, s.Events, Dir())}
	if s.Skipped > 0 {
		out = append(out, fmt.Sprintf("  note  %d line(s) were not events and were skipped", s.Skipped))
	}
	if s.Events == 0 {
		return strings.Join(append(out, "  nothing recorded yet"), "\n")
	}
	for _, h := range s.Hooks {
		out = append(out, fmt.Sprintf("  hook      %-16s %5d  allow %d  deny %d  ask %d  context %d  error %d  p50 %s  p95 %s",
			h.Name, h.Count, h.Allow, h.Deny, h.Ask, h.Context, h.Error, human(h.P50Ms), human(h.P95Ms)))
	}
	for _, c := range s.Commands {
		name := strings.TrimSpace(c.Command + " " + c.Verb)
		out = append(out, fmt.Sprintf("  command   %-24s %4d  failed %d  p50 %s", name, c.Count, c.Failed, human(c.P50Ms)))
	}
	if s.TestRuns > 0 || s.Checks > 0 {
		out = append(out, fmt.Sprintf("  delivery  test runs %d, all green %d  ·  delivery checks %d, ready %d", s.TestRuns, s.TestsGreen, s.Checks, s.Ready))
	}
	for _, x := range s.Subagents {
		out = append(out, fmt.Sprintf("  subagent  %-16s %4d  completed %d  turn-cap %d  timeout %d  cancelled %d  failed %d  p50 %s, %s turn(s)",
			x.Agent, x.Count, x.Completed, x.TurnCap, x.Timeout, x.Cancelled, x.Failed, human(x.P50Ms), turns(x.P50Turns)))
	}
	if n := len(s.Compactions); n > 0 {
		out = append(out, fmt.Sprintf("  compactions  %d", n))
	}
	for _, d := range s.Denials[:min(5, len(s.Denials))] {
		out = append(out, fmt.Sprintf("  denied    %s  %s  %s %s  %s", stamp(d.Time), d.Hook, d.Agent, d.Tool, clipText(d.Reason, 120)))
	}
	return strings.Join(out, "\n")
}

func turns(x float64) string {
	if math.IsNaN(x) {
		return "-"
	}
	return fmt.Sprintf("%.0f", x)
}

func clipText(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
