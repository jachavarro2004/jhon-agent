package telemetry

import (
	"bufio"
	"cmp"
	"encoding/json"
	"github.com/jachavarro2004/jhon-agent/engine/internal/rename"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Read is every event recorded from since on, oldest first. A line that is
// not an event (a torn last line, a hand edit) is skipped and counted.
func Read(since time.Time) (events []Event, skipped int) {
	first := since.UTC().Format("2006-01-02")
	files, _ := filepath.Glob(filepath.Join(Dir(), "????-??-??.jsonl"))
	slices.Sort(files)
	for _, f := range files {
		if strings.TrimSuffix(filepath.Base(f), ".jsonl") < first {
			continue
		}
		file, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(file)
		sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for sc.Scan() {
			var raw struct {
				Time, Name, Severity string
				Attributes           map[string]any
			}
			if json.Unmarshal(sc.Bytes(), &raw) != nil || raw.Name == "" {
				skipped++
				continue
			}
			t, err := time.Parse(time.RFC3339Nano, raw.Time)
			if err != nil {
				skipped++
				continue
			}
			if t.Before(since) {
				continue
			}
			events = append(events, Event{t, raw.Name, raw.Severity, raw.Attributes})
		}
		file.Close()
	}
	return events, skipped
}

// HookStat is one hook's decisions and their durations.
type HookStat struct {
	Name                             string
	Count                            int
	Allow, Deny, Ask, Context, Error int
	P50Ms, P95Ms                     float64
}

// Denial is one refusal, as the summary lists the latest.
type Denial struct {
	Time                      time.Time
	Hook, Agent, Tool, Reason string
}

// CommandStat is one command and verb's runs.
type CommandStat struct {
	Command, Verb string
	Count, Failed int
	P50Ms         float64
}

// SubagentStat is one agent's runs and how they ended.
type SubagentStat struct {
	Agent                                          string
	Count                                          int
	Completed, TurnCap, Timeout, Cancelled, Failed int
	P50Ms, P50Turns                                float64
}

// Day is one UTC day's events and refusals.
type Day struct {
	Day            string
	Events, Denies int
}

// Summary is what the events of a range say, as `jhon telemetry` prints it
// and the dashboard's Telemetry view shows it.
type Summary struct {
	Days, Events, Skipped int
	Hooks                 []HookStat
	Denials               []Denial
	Commands              []CommandStat
	TestRuns, TestsGreen  int
	Checks, Ready         int
	Subagents             []SubagentStat
	Compactions           []Event
	PerDay                []Day
}

// Summarize sums events up; days is the range they were read for.
func Summarize(events []Event, days, skipped int) Summary {
	s := Summary{Days: days, Events: len(events), Skipped: skipped, Hooks: []HookStat{}, Denials: []Denial{}, Commands: []CommandStat{},
		Subagents: []SubagentStat{}, Compactions: []Event{}, PerDay: []Day{}}
	hooks, hookMs := map[string]*HookStat{}, map[string][]float64{}
	commands, commandMs := map[[2]string]*CommandStat{}, map[[2]string][]float64{}
	agents, agentMs, agentTurns := map[string]*SubagentStat{}, map[string][]float64{}, map[string][]float64{}
	perDay := map[string]*Day{}
	for _, e := range events {
		a := e.Attributes
		day := e.Time.UTC().Format("2006-01-02")
		if perDay[day] == nil {
			perDay[day] = &Day{Day: day}
		}
		perDay[day].Events++
		switch e.Name {
		case "jhon.hook":
			name := str(a, "jhon.hook.name")
			h := hooks[name]
			if h == nil {
				h = &HookStat{Name: name}
				hooks[name] = h
			}
			h.Count++
			switch str(a, "jhon.decision") {
			case "allow":
				h.Allow++
			case "deny":
				h.Deny++
				perDay[day].Denies++
				s.Denials = append(s.Denials, Denial{e.Time, name, rename.AgentNow(str(a, "gen_ai.agent.name")), str(a, "gen_ai.tool.name"), str(a, "jhon.reason")})
			case "ask":
				h.Ask++
			case "context":
				h.Context++
			default:
				h.Error++
			}
			if ms, ok := a["jhon.duration_ms"].(float64); ok {
				hookMs[name] = append(hookMs[name], ms)
			}
		case "jhon.command":
			k := [2]string{str(a, "jhon.command"), str(a, "jhon.command.verb")}
			c := commands[k]
			if c == nil {
				c = &CommandStat{Command: k[0], Verb: k[1]}
				commands[k] = c
			}
			c.Count++
			if code, _ := a["process.exit.code"].(float64); code != 0 {
				c.Failed++
			}
			if ms, ok := a["jhon.duration_ms"].(float64); ok {
				commandMs[k] = append(commandMs[k], ms)
			}
			if n, ok := a["jhon.tests.executions"].(float64); ok {
				s.TestRuns++
				if passed, _ := a["jhon.tests.passed"].(float64); passed == n && n > 0 {
					s.TestsGreen++
				}
			}
			if ready, ok := a["jhon.delivery.ready"].(bool); ok {
				s.Checks++
				if ready {
					s.Ready++
				}
			}
		case "jhon.subagent":
			name := rename.AgentNow(str(a, "gen_ai.agent.name"))
			x := agents[name]
			if x == nil {
				x = &SubagentStat{Agent: name}
				agents[name] = x
			}
			x.Count++
			switch str(a, "jhon.outcome") {
			case "completed":
				x.Completed++
			case "turn-cap": // recorded by shells before T131, which had a turn cap; still counted
				x.TurnCap++
			case "timeout":
				x.Timeout++
			case "cancelled":
				x.Cancelled++
			default:
				x.Failed++
			}
			if ms, ok := a["jhon.duration_ms"].(float64); ok {
				agentMs[name] = append(agentMs[name], ms)
			}
			if n, ok := a["jhon.turns"].(float64); ok {
				agentTurns[name] = append(agentTurns[name], n)
			}
		case "jhon.compaction":
			s.Compactions = append(s.Compactions, e)
		}
	}
	for name, h := range hooks {
		h.P50Ms, h.P95Ms = quantile(hookMs[name], 0.5), quantile(hookMs[name], 0.95)
		s.Hooks = append(s.Hooks, *h)
	}
	for k, c := range commands {
		c.P50Ms = quantile(commandMs[k], 0.5)
		s.Commands = append(s.Commands, *c)
	}
	for name, x := range agents {
		x.P50Ms, x.P50Turns = quantile(agentMs[name], 0.5), quantile(agentTurns[name], 0.5)
		s.Subagents = append(s.Subagents, *x)
	}
	for _, d := range perDay {
		s.PerDay = append(s.PerDay, *d)
	}
	slices.SortFunc(s.Hooks, func(a, b HookStat) int { return cmp.Or(b.Count-a.Count, cmp.Compare(a.Name, b.Name)) })
	slices.SortFunc(s.Commands, func(a, b CommandStat) int {
		return cmp.Or(b.Count-a.Count, cmp.Compare(a.Command, b.Command), cmp.Compare(a.Verb, b.Verb))
	})
	slices.SortFunc(s.Subagents, func(a, b SubagentStat) int { return cmp.Or(b.Count-a.Count, cmp.Compare(a.Agent, b.Agent)) })
	slices.SortFunc(s.PerDay, func(a, b Day) int { return cmp.Compare(a.Day, b.Day) })
	// The latest refusals, newest first.
	slices.Reverse(s.Denials)
	s.Denials = s.Denials[:min(20, len(s.Denials))]
	return s
}

func str(a map[string]any, k string) string {
	v, _ := a[k].(string)
	return v
}

// quantile is the q-quantile of xs by nearest rank; NaN for none.
func quantile(xs []float64, q float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	return s[max(0, int(math.Ceil(q*float64(len(s))))-1)]
}
