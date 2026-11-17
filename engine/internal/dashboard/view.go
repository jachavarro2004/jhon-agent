package dashboard

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// What the dashboard shows of one audit report.
// `jhon audit --json --context` answers per transcript and per grouping; a
// reader asks per change and per agent. View joins the two: each change with
// the agent types it launched, its time, its verify and test results,
// its tokens, and the reasons it needs attention; each agent type and each
// sub-agent with its turns, peak context, tokens, and share; the findings
// grouped by check, each check explained in a sentence. Pure: it reads only
// the report it is given.

// A Check is what a check looks for, in a sentence a reader without the
// contracts understands.
type Check struct {
	Title   string `json:"title"`
	Explain string `json:"explain"`
}

// Checks explains each audit check, in the order the page lists them.
var Checks = []struct {
	Name string
	Check
}{
	{"refusal-bypass", Check{"Refusal bypassed", "A guard or the owner refused an action, and the session later reached the same effect another way. A refusal is a stop."}},
	{"isolation-bypass", Check{"Isolation left", "An agent confined to a worktree or a sandbox wrote or ran outside it."}},
	{"delivered-untested", Check{"Delivered untested", "A commit, push, merge, or release went out without a passing test run of that exact revision."}},
}

// Severities explains each severity, for the legend.
var Severities = struct {
	Violation string `json:"violation"`
	Suspect   string `json:"suspect"`
}{
	"A verified check found the rule broken.",
	"A heuristic or not yet validated check matched; it needs a person to look.",
}

func checkOf(name any) (Check, bool) {
	for _, c := range Checks {
		if s, ok := name.(string); ok && c.Name == s {
			return c.Check, true
		}
	}
	return Check{}, false
}

// readFields is each read source and its field in tool_result_bytes.
var readFields = [][2]string{
	{"full", "full_read_bytes"}, {"ranged", "ranged_read_bytes"}, {"shell", "shell_read_bytes"}, {"codegraph", "codegraph_bytes"},
	{"engram", "engram_bytes"}, {"search", "search_bytes"}, {"other", "other_bytes"},
}

func reads(b any) *js.Object {
	o := js.Obj()
	for _, f := range readFields {
		o.Set(f[0], or(js.Prop(b, f[1]), 0.0))
	}
	return o
}

// round is Math.round: halves toward +Infinity.
func round(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	f := math.Floor(x)
	if x-f >= 0.5 {
		return f + 1
	}
	return f
}

func ratio(n, total any) float64 {
	if t := num(total); t > 0 {
		return round(num(n)/t*1e4) / 1e4
	}
	return 0
}

// plus is a + b: text when either is a string or an object, else a sum.
func plus(a, b any) any {
	isText := func(v any) bool {
		switch v.(type) {
		case string, []any, *js.Object:
			return true
		}
		return false
	}
	if isText(a) || isText(b) {
		return js.String(a) + js.String(b)
	}
	return num(a) + num(b)
}

// phases is the agent types a change launched, by name, each with its count.
func phases(launches any) []any {
	out := []any{}
	var keys []string
	if o, ok := launches.(*js.Object); ok {
		keys = slices.Clone(o.Keys())
	}
	slices.SortStableFunc(keys, js.Compare)
	for _, k := range keys {
		out = append(out, []any{k, js.Prop(launches, k)})
	}
	return out
}

// rewrites is a change's planning rewrites after its first verify: the
// report's post_verify_rewrites, or replans in a report kept from before.
func rewrites(c any) any { return or(js.Prop(c, "post_verify_rewrites"), js.Prop(c, "replans")) }

// attention is why a change needs a person's look, in plain words; empty
// when nothing stands out. Post-verify rewrites are not among them: planning
// as the change goes rewrites the plan, information and never a cost.
func attention(c any) []any {
	out := []any{}
	if f := js.Prop(js.Prop(c, "verify_verdicts"), "fail"); js.Truthy(f) {
		out = append(out, fmt.Sprintf("verify failed %s time(s)", js.String(f)))
	}
	if n := js.Prop(c, "unfinished"); js.Truthy(n) {
		out = append(out, js.String(n)+" agent(s) stopped short")
	}
	if n := js.Prop(c, "relaunches"); js.Truthy(n) {
		out = append(out, js.String(n)+" relaunch(es)")
	}
	record := js.Prop(c, "record")
	if js.Truthy(record) && !js.Truthy(js.Prop(record, "passed")) {
		// A run whose tests passed on uncommitted changes is not one that failed.
		if js.Truthy(js.Prop(record, "tests_passed")) {
			out = append(out, "the last test record passed on uncommitted changes")
		} else {
			out = append(out, "the last test record failed")
		}
	}
	if t := plus(or(js.Prop(js.Prop(c, "test_runs"), "timeouts"), 0.0), or(js.Prop(record, "timeouts"), 0.0)); js.Truthy(t) {
		out = append(out, js.String(t)+" test timeout(s)")
	}
	if n := js.Prop(js.Prop(c, "test_runs"), "failed"); js.Truthy(n) {
		out = append(out, js.String(n)+" failed test run(s)")
	}
	return out
}

// entry is a Map entry; a Map keeps its keys in insertion order.
type entry struct {
	key  any
	vals []any
}

// group is items grouped by key(item), as a Map of arrays.
func group(items []any, key func(any) any) []*entry {
	var out []*entry
	for _, it := range items {
		k := key(it)
		i := slices.IndexFunc(out, func(e *entry) bool { return sameValueZero(e.key, k) })
		if i < 0 {
			out = append(out, &entry{key: k})
			i = len(out) - 1
		}
		out[i].vals = append(out[i].vals, it)
	}
	return out
}

// lookup is new Map(pairs).js.Prop(k): the last pair with the key wins.
func lookup(items []any, key, val func(any) any, k any) (any, bool) {
	for i := len(items) - 1; i >= 0; i-- {
		if sameValueZero(key(items[i]), k) {
			return val(items[i]), true
		}
	}
	return js.Undefined{}, false
}

// cmp is a comparator's number as Array.prototype.sort reads it.
func cmp(d float64) int {
	switch {
	case d > 0:
		return 1
	case d < 0:
		return -1
	}
	return 0 // NaN too
}

func count(items []any, pred func(any) bool) float64 {
	n := 0
	for _, it := range items {
		if pred(it) {
			n++
		}
	}
	return float64(n)
}

func severityIs(s string) func(any) bool {
	return func(f any) bool { return strictEqual(js.Prop(f, "severity"), s) }
}

// View is the dashboard's view of an audit report (a value Parse gives, or
// any Go value that marshals to the report's JSON).
func View(report any) *js.Object {
	report = toValue(report)
	cu := or(js.Prop(report, "context_use"), nil)
	oc := or(js.Prop(report, "outcomes"), nil)
	findings := list(or(js.Prop(report, "findings"), []any{}))
	all := list(or(js.Prop(cu, "agents"), []any{}))
	var total any = 0.0
	for _, a := range all {
		total = plus(total, or(js.Prop(a, "total_processed_context"), 0.0))
	}
	var subs []any
	for _, a := range all {
		if !strictEqual(js.Prop(a, "agent_type"), "main") {
			subs = append(subs, a)
		}
	}
	byChange := list(or(js.Prop(cu, "by_change"), []any{}))
	changeGroup := func(k any) any {
		g, _ := lookup(byChange, func(g any) any { return js.Prop(g, "key") }, func(g any) any { return g }, k)
		return g
	}
	longs := list(or(js.Prop(cu, "long_agents"), []any{}))
	longOf := func(agent any) (any, bool) {
		return lookup(longs, func(l any) any { return js.Prop(l, "agent") }, func(l any) any { return js.Prop(l, "why") }, agent)
	}

	type change struct {
		o          *js.Object
		attention  int
		processedV any
	}
	var cs []change
	// The main thread's tokens on a change (its turns while working on it, as
	// its time is main_ms) count for the change, not outside every change.
	mainTokens := 0.0
	for _, c := range list(or(js.Prop(oc, "changes"), []any{})) {
		if js.Prop(c, "change") == nil {
			continue
		}
		g := changeGroup(js.Prop(c, "change"))
		tr, rec := js.Prop(c, "test_runs"), js.Prop(c, "record")
		var record any
		if js.Truthy(rec) {
			record = js.Obj("passed", js.Prop(rec, "passed"), "tests_passed", js.Prop(rec, "tests_passed"), "why", js.Prop(rec, "why"), "executions", js.Prop(rec, "executions"), "duration_ms", js.Prop(rec, "duration_ms"), "timeouts", js.Prop(rec, "timeouts"))
		}
		main := num(or(js.Prop(c, "main_tokens"), 0.0))
		mainTokens += main
		processed := num(or(js.Prop(g, "total_processed_context"), 0.0)) + main
		att := attention(c)
		o := js.Obj(
			"name", js.Prop(c, "change"), "phases", phases(or(js.Prop(c, "launches"), js.Obj())), "agents", js.Prop(c, "agents"),
			"start", js.Prop(c, "start"), "end", js.Prop(c, "end"),
			"elapsed_ms", js.Prop(c, "elapsed_ms"), "active_ms", js.Prop(c, "active_ms"), "agent_ms", js.Prop(c, "agent_ms"),
			"main_ms", js.Prop(c, "main_ms"), "main_commands", js.Prop(c, "main_commands"),
			"verify", js.Prop(c, "verify_verdicts"), "replans", rewrites(c), "relaunches", js.Prop(c, "relaunches"), "unfinished", js.Prop(c, "unfinished"),
			"tests", js.Obj("count", or(js.Prop(tr, "count"), 0.0), "failed", or(js.Prop(tr, "failed"), 0.0), "timeouts", or(js.Prop(tr, "timeouts"), 0.0)),
			"record", record,
			"processed", processed, "main_tokens", main, "peak", or(js.Prop(g, "peak_context_per_turn_max"), 0.0), "share", ratio(processed, total),
			"attention", att,
		)
		cs = append(cs, change{o, len(att), processed})
	}
	slices.SortStableFunc(cs, func(a, b change) int {
		if d := b.attention - a.attention; d != 0 {
			return d
		}
		return cmp(num(b.processedV) - num(a.processedV))
	})
	changes := []any{}
	for _, c := range cs {
		changes = append(changes, c.o)
	}

	var unattributed any
	if none := changeGroup(nil); js.Truthy(none) {
		p := math.Max(0, num(or(js.Prop(none, "total_processed_context"), 0.0))-mainTokens)
		unattributed = js.Obj("agents", js.Prop(none, "agents"), "processed", p, "share", ratio(p, total))
	}

	type agentType struct {
		o         *js.Object
		processed any
	}
	var ts []agentType
	for _, e := range group(subs, func(a any) any { return js.Prop(a, "agent_type") }) {
		var processed, turns, rereads any = 0.0, 0.0, 0.0
		peak, long := math.Inf(-1), 0.0
		for _, a := range e.vals {
			processed = plus(processed, js.Prop(a, "total_processed_context"))
			turns = plus(turns, js.Prop(a, "turns"))
			if p := num(js.Prop(a, "peak_context_per_turn")); math.IsNaN(p) || math.IsNaN(peak) {
				peak = math.NaN()
			} else {
				peak = math.Max(peak, p)
			}
			if _, ok := longOf(js.Prop(a, "agent")); ok {
				long++
			}
			rereads = plus(rereads, or(js.Prop(a, "same_file_rereads"), 0.0))
		}
		ts = append(ts, agentType{js.Obj(
			"type", e.key, "count", float64(len(e.vals)), "avg_turns", round(num(turns)/float64(len(e.vals))),
			"peak", peak, "processed", processed, "share", ratio(processed, total), "long", long, "rereads", rereads,
		), processed})
	}
	slices.SortStableFunc(ts, func(a, b agentType) int { return cmp(num(b.processed) - num(a.processed)) })
	agentTypes := []any{}
	for _, t := range ts {
		agentTypes = append(agentTypes, t.o)
	}

	sorted := slices.Clone(subs)
	slices.SortStableFunc(sorted, func(a, b any) int {
		return cmp(num(js.Prop(b, "total_processed_context")) - num(js.Prop(a, "total_processed_context")))
	})
	agents := []any{}
	for _, a := range sorted {
		long, _ := longOf(js.Prop(a, "agent"))
		agents = append(agents, js.Obj(
			"id", js.Prop(a, "agent"), "type", js.Prop(a, "agent_type"), "change", or(js.Prop(a, "change"), nil), "phase", or(js.Prop(a, "phase"), nil),
			"turns", js.Prop(a, "turns"), "peak", js.Prop(a, "peak_context_per_turn"), "processed", js.Prop(a, "total_processed_context"),
			"share", ratio(js.Prop(a, "total_processed_context"), total), "rereads", or(js.Prop(a, "same_file_rereads"), 0.0),
			"long", or(long, nil), "reads", reads(js.Prop(a, "tool_result_bytes")),
			"start", or(js.Prop(a, "start"), nil), "end", or(js.Prop(a, "end"), nil), "reread_files", or(js.Prop(a, "reread_files"), []any{}),
		))
	}

	type grouped struct {
		o                  *js.Object
		violation, suspect float64
	}
	var gs []grouped
	for _, e := range group(findings, func(f any) any { return js.Prop(f, "check") }) {
		title, explain := any(e.key), ""
		if c, ok := checkOf(e.key); ok {
			title, explain = c.Title, c.Explain
		}
		fs := slices.Clone(e.vals)
		slices.SortStableFunc(fs, func(a, b any) int {
			if strictEqual(js.Prop(a, "severity"), js.Prop(b, "severity")) {
				return cmp(num(js.Prop(a, "line")) - num(js.Prop(b, "line")))
			}
			if strictEqual(js.Prop(a, "severity"), "violation") {
				return -1
			}
			return 1
		})
		items := []any{}
		for _, f := range fs {
			parts := strings.Split(js.String(js.Prop(f, "transcript")), "/")
			items = append(items, js.Obj(
				"severity", js.Prop(f, "severity"), "kind", js.Prop(f, "kind"), "agent", js.Prop(f, "agentType"),
				"where", parts[len(parts)-1]+":"+js.String(js.Prop(f, "line")), "message", js.Prop(f, "message"), "evidence", js.Prop(f, "evidence"),
			))
		}
		v, s := count(e.vals, severityIs("violation")), count(e.vals, severityIs("suspect"))
		gs = append(gs, grouped{js.Obj(
			"check", e.key, "title", title, "explain", explain, "violation", v, "suspect", s, "items", items,
		), v, s})
	}
	slices.SortStableFunc(gs, func(a, b grouped) int {
		if d := cmp(b.violation - a.violation); d != 0 {
			return d
		}
		return cmp(b.suspect - a.suspect)
	})
	findingGroups := []any{}
	for _, g := range gs {
		findingGroups = append(findingGroups, g.o)
	}

	summed := reads(js.Undefined{})
	for _, a := range all {
		r := reads(js.Prop(a, "tool_result_bytes"))
		for _, k := range r.Keys() {
			summed.Set(k, plus(js.Prop(summed, k), js.Prop(r, k)))
		}
	}

	session := js.Prop(oc, "session")
	return js.Obj(
		"status", or(js.Prop(report, "status"), nil), "version", or(js.Prop(report, "version"), nil), "session", or(js.Prop(report, "session"), nil),
		"hasContext", cu != nil, "hasOutcomes", oc != nil,
		"summary", js.Obj(
			"elapsed_ms", or(js.Prop(session, "elapsed_ms"), 0.0), "active_ms", or(js.Prop(session, "active_ms"), 0.0), "resumes", or(js.Prop(session, "resumes"), 0.0),
			"subagents", float64(len(subs)), "processed", total, "changes", float64(len(changes)),
			"findings", js.Obj("violation", count(findings, severityIs("violation")), "suspect", count(findings, severityIs("suspect"))),
		),
		"changes", changes, "unattributed", unattributed, "agentTypes", agentTypes, "agents", agents, "findings", findingGroups, "reads", summed,
		"errors", or(js.Prop(report, "errors"), []any{}), "notes", or(js.Prop(report, "notes"), []any{}),
		"authorizations", or(js.Prop(report, "authorizations"), []any{}),
		"worked", or(js.Prop(report, "worked"), []any{}),
		"timing", js.Prop(report, "timing"),
	)
}
