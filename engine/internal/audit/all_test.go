package audit

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
)

// jhon audit --all: every session's audit, aggregated into what decides the
// next change: findings by check and severity, Jhon's sessions apart, and
// each agent type's turns against its own turn cap with its share of tokens.
func TestAggregateSumsFindingsAndAgentTurns(t *testing.T) {
	results := []Audited{
		{Path: "a.jsonl", Jhon: true, Result: Audit(fixture(t, "background-run-failed-delivered"))},
		{Path: "b.jsonl", Result: Audit(fixture(t, "background-run-delivered"))},
		{Path: "c.jsonl", Result: &Result{Status: "audit-error", Errors: []AuditError{{Message: "unreadable"}}}},
		{Path: "d.jsonl", Jhon: true, Result: &Result{Status: "clean", ContextUse: &ContextUse{Agents: []*AgentContextUse{
			{AgentType: "worker", Turns: 30, Total: 1000}, {AgentType: "worker", Turns: 50, Total: 3000, TurnContext: slices.Repeat([]float64{60}, 50)}, {AgentType: "verify", Turns: 10, Total: 500},
			{AgentType: "explore", Turns: 64, Total: 9000},
		}}}},
	}
	f := Aggregate(results, map[string]int{"worker": 40, "verify": 40}, time.Time{})
	eq(t, "sessions", [3]int{f.Sessions, f.JhonSessions, f.Errors}, [3]int{4, 2, 1})
	eq(t, "delivered-untested violations", f.Findings["delivered-untested"]["violation"], 1)
	var worker *AgentTypeStats
	for i := range f.AgentTypes {
		if f.AgentTypes[i].AgentType == "worker" {
			worker = &f.AgentTypes[i]
		}
	}
	if worker == nil {
		t.Fatalf("no worker row: %+v", f.AgentTypes)
	}
	eq(t, "worker turns", [4]int{worker.Agents, worker.MedianTurns, worker.MaxTurns, worker.OverCap}, [4]int{2, 30, 50, 1})
	eq(t, "the share of the worker's tokens over the cap", worker.OverCapTokenShare, 600.0/4000)
	eq(t, "agent types, most tokens first", f.AgentTypes[1].AgentType, "worker")
	eq(t, "an agent with no cap is over none", [2]int{f.AgentTypes[0].Cap, f.AgentTypes[0].OverCap}, [2]int{0, 0})
}

// The past share counts only the tokens of the turns past the mark: a run
// stopped at 82 turns by a cap of 80 brings its turns 81-82, a run at or
// under its mark brings none, and the review mark of a type with no cap
// counts the same way.
func TestThePastShareCountsOnlyTheTurnsPastTheMark(t *testing.T) {
	run := func(typ string, turns int) *AgentContextUse {
		a := &AgentContextUse{AgentType: typ, Turns: turns}
		for range turns {
			a.TurnContext = append(a.TurnContext, 10)
			a.Total += 10
		}
		return a
	}
	agents := []*AgentContextUse{run("worker", 82), run("worker", 80), run("worker", 38), run("general", 103)}
	f := Aggregate([]Audited{{Path: "s.jsonl", Jhon: true, Result: &Result{Status: "clean", ContextUse: &ContextUse{Agents: agents}}}},
		map[string]int{"worker": 80, "general": 100}, time.Time{})
	shares := map[string][2]float64{}
	for _, a := range f.AgentTypes {
		shares[a.AgentType] = [2]float64{float64(a.OverCap), a.OverCapTokenShare}
	}
	eq(t, "worker: one run past, its turns 81-82 only", shares["worker"], [2]float64{1, 20.0 / 2000})
	eq(t, "review mark: turns 101-103 only", shares["general"], [2]float64{1, 30.0 / 1030})
}

// A session that runs on past the cutoff brings only the sub-agents it
// started after it.
func TestAggregateCountsOnlyTheAgentsStartedSinceTheCutoff(t *testing.T) {
	agents := []*AgentContextUse{
		{AgentType: "worker", Turns: 61, Total: 1000, Start: "2026-10-27T02:15:30.000Z"},
		{AgentType: "worker", Turns: 25, Total: 500, Start: "2026-10-28T06:00:00.000Z"},
	}
	since := time.Date(2026, 10, 28, 5, 0, 0, 0, time.UTC)
	f := Aggregate([]Audited{{Path: "s.jsonl", Jhon: true, Result: &Result{Status: "clean", ContextUse: &ContextUse{Agents: agents}}}}, map[string]int{"worker": 40}, since)
	eq(t, "workers since the cutoff", [3]int{f.AgentTypes[0].Agents, f.AgentTypes[0].MaxTurns, f.AgentTypes[0].OverCap}, [3]int{1, 25, 0})
}

// A sub-agent transcript that cannot be read (a link whose target is gone)
// is an audit-error of that file, never a crash of the whole audit.
func TestAnUnreadableSubagentTranscriptIsAnAuditError(t *testing.T) {
	main := sessionWith(t, user("go"), delegate("d01", "general", chg, push()))
	dir := filepath.Join(filepath.Dir(main), sessionID, "subagents")
	must(t, os.Symlink(filepath.Join(t.TempDir(), "gone.jsonl"), filepath.Join(dir, "agent-dead0001.jsonl")))
	must(t, os.WriteFile(filepath.Join(dir, "agent-dead0001.meta.json"), []byte(`{"agentType":"general","toolUseId":"toolu_d01"}`), 0o644))
	r := Audit(main)
	if len(r.Errors) != 1 || !strings.Contains(r.Errors[0].Message, "cannot be read") || !strings.HasSuffix(r.Errors[0].Transcript, "agent-dead0001.jsonl") {
		t.Fatalf("errors = %+v", r.Errors)
	}
}

// jhon-shell appends a jhon-ui-use entry each time a command or shortcut of
// its UI runs (shell/lib/ui-use.ts); the fleet counts them by feature.
func TestUIUsesCountJhonShellEntries(t *testing.T) {
	f := filepath.Join(t.TempDir(), "s.jsonl")
	lines := `{"type":"session","id":"s"}
{"type":"custom","customType":"jhon-ui-use","data":{"feature":"/jhon:stats"}}
{"type":"custom","customType":"web-search-results","data":{"feature":"/not-ours"}}
{"type":"custom","customType":"jhon-ui-use","data":{"feature":"/jhon:stats"}}
{"type":"custom","customType":"jhon-ui-use","data":{"feature":"key ctrl+t"}}
not json
`
	must(t, os.WriteFile(f, []byte(lines), 0o644))
	eq(t, "uses", UIUses(f), map[string]int{"/jhon:stats": 2, "key ctrl+t": 1})
	fleet := Aggregate([]Audited{{Path: f, Jhon: true, Result: &Result{Status: "clean"}, UIUse: UIUses(f)}, {Path: f, Jhon: true, Result: &Result{Status: "clean"}, UIUse: map[string]int{"/plan": 1}}}, nil, time.Time{})
	eq(t, "fleet uses", fleet.UIUse, map[string]int{"/jhon:stats": 2, "key ctrl+t": 1, "/plan": 1})
}

// From Claude Code 2.1.310 Jhon's hooks reach a session only through its
// plugin, whose SessionStart and Stop hooks the transcript records even when
// they print nothing; a Jhon session that ran tools with none recorded ran
// without its guards.
func TestUnguardedSessionsRanToolsWithNoJhonHook(t *testing.T) {
	tool := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"git push"}}]}}`
	engram := `{"type":"system","subtype":"stop_hook_summary","hookInfos":[{"command":"\"${CLAUDE_PLUGIN_ROOT}/scripts/session-stop.sh\""}]}`
	plugin := `{"type":"system","subtype":"stop_hook_summary","hookInfos":[{"command":"\"${CLAUDE_PLUGIN_ROOT}/../../../build/jhon\" hook agent-hooks jhon-orchestrator"}]}`
	start := `{"type":"attachment","attachment":{"type":"hook_success","hookEvent":"SessionStart","command":"'/home/u/.local/share/jhon/current/build/jhon' 'hook' 'agent-hooks' 'jhon-orchestrator' '--part' '1'"}}`
	file := func(lines ...string) string {
		f := filepath.Join(t.TempDir(), "s.jsonl")
		must(t, os.WriteFile(f, []byte(strings.Join(lines, "\n")+"\n"), 0o644))
		return f
	}
	claude := func(v string) *Result { return &Result{Status: "clean", Runtime: "claude-code", Version: &v} }
	for _, c := range []struct {
		name  string
		path  string
		r     *Result
		wants bool
	}{
		{"tools and only another plugin's hooks", file(tool, engram), claude("2.1.310"), true},
		{"the plugin's Stop hook ran", file(tool, plugin), claude("2.1.310"), false},
		{"the plugin's SessionStart hook ran", file(start, tool), claude("2.1.311"), false},
		{"no tool called", file(engram), claude("2.1.310"), false},
		{"before 2.1.310 a silent frontmatter hook leaves no trace", file(tool, engram), claude("2.1.304"), false},
		{"not Claude Code", file(tool, engram), &Result{Status: "clean", Runtime: "pi"}, false},
	} {
		eq(t, c.name, Unguarded(c.path, c.r), c.wants)
	}
	f := Aggregate([]Audited{{Path: "a.jsonl", Jhon: true, Result: &Result{Status: "clean"}, Unguarded: true}, {Path: "b.jsonl", Jhon: true, Result: &Result{Status: "clean"}}}, nil, time.Time{})
	eq(t, "fleet", f.Unguarded, []string{"a.jsonl"})
}

// LDD's measures: each guard's refusals since the cutoff, one row per card
// (a guard that refused nothing shows zero), and each change once, its
// archive and the sessions it spans folded in.
func TestAggregateMeasuresGuardsAndChanges(t *testing.T) {
	since := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	r := &Result{Status: "clean",
		Guards: []GuardRefusal{
			{Guard: "safety-guard", At: "2026-10-28T08:00:00.000Z", LostMs: 20e3},
			{Guard: "safety-guard", At: "2026-10-28T09:00:00.000Z", LostMs: 40e3},
			{Guard: "safety-guard", At: "2026-10-27T09:00:00.000Z", LostMs: 99e3},
			{Guard: "route-guard", At: "2026-10-28T10:00:00.000Z", LostMs: math.NaN()},
		},
		LDD: []ChangeLDD{
			{Change: "c1", Start: "2026-10-28T05:00:00.000Z", Slices: 3, MaxTasks: 5, OwnerMessages: 6, ActiveMs: 60e3, Commits: 2, ByHand: 2},
			{Change: "archive/2026-10-28-c1", Start: "2026-10-28T06:00:00.000Z", Slices: 3, MaxTasks: 7, OwnerMessages: 1, ActiveMs: 30e3, Merges: 1, LastMerge: "2026-10-28T07:00:00.000Z", Commits: 1, ByHand: 1},
			{Change: "old", Start: "2026-10-27T05:00:00.000Z", Slices: 1},
		}}
	f := Aggregate([]Audited{{Path: "s.jsonl", Jhon: true, Result: r}, {Path: "t.jsonl", Result: r}}, nil, since)
	by := map[string]GuardStats{}
	for _, g := range f.Guards {
		by[g.Guard] = g
	}
	eq(t, "one row per card", len(f.Guards), len(guard.Cards))
	eq(t, "safety-guard since the cutoff, in Jhon's sessions only", [3]float64{float64(by["safety-guard"].Refusals), by["safety-guard"].LostMedianMs, by["safety-guard"].LostSumMs}, [3]float64{2, 20e3, 60e3})
	eq(t, "a refusal the agent never got past", by["route-guard"].Stuck, 1)
	eq(t, "a guard that refused nothing", by["turn-guard"].Refusals, 0)
	if len(f.LDD) != 1 {
		t.Fatalf("one change since the cutoff, its archive folded in: %+v", f.LDD)
	}
	c := f.LDD[0]
	eq(t, "the change", [4]any{c.Change, c.Slices, c.OwnerMessages, c.Merges}, [4]any{"c1", 3, 7, 1})
	eq(t, "its commits", c.Commits, 3)
	eq(t, "its test runs by hand", c.ByHand, 3)
	eq(t, "the largest slice of either row", c.MaxTasks, 7)
	eq(t, "to main: from its start to its last merge", c.ToMainMs(), 2*3600e3)
}

// A refusal names its guard after the runtime's own prefix.
func TestGuardNameReadsTheRefusal(t *testing.T) {
	text := `PreToolUse:Bash hook error: ["/home/u/.local/share/jhon/current/build/jhon" hook agent-hooks jhon-orchestrator]: safety-guard: unreadable: the command cannot be read safely`
	if m := guardName.FindStringSubmatch(text); m == nil || m[1] != "safety-guard" {
		t.Errorf("guard of %q: %v", text, m)
	}
	if m := guardName.FindStringSubmatch(`'pre-tool' '--agent' 'worker' 'safety-guard' 'vcs-guard'`); m != nil {
		t.Errorf("a command's guard words are no refusal: %v", m)
	}
}

// `jhon audit --all` carries a sub-agent's hand runs in the change's row
// (T133): the count of the audited session reaches the fleet's LDD table.
func TestAggregateCarriesHandRunsOfSubagents(t *testing.T) {
	run := bash("jhon executions run "+chg, executionsOut(c1, t1, []string{"pass"}))
	r := auditOf(t, user("go"), phased("apply", delegate("o01", "worker", chg, run, bash("uv run pytest -q", "3 passed"), bash("uv run pytest -q", "3 passed"))))
	f := Aggregate([]Audited{{Path: "s.jsonl", Jhon: true, Result: r}}, nil, time.Time{})
	if len(f.LDD) != 1 {
		t.Fatalf("one change: %+v", f.LDD)
	}
	eq(t, "the change", f.LDD[0].Change, chg)
	eq(t, "its test runs by hand, the worker's two", f.LDD[0].ByHand, 2)
}
