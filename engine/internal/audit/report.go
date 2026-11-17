package audit

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// The report: `--json` prints it with a two-space indent, built as
// js values so key order, numbers, and strings print the same every run;
// the human output is a few lines per finding and measurement.

func strs(xs []string) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

// null is s as a value: null for "".
func null(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func ptr[T any](p *T, f func(T) any) any {
	if p == nil {
		return nil
	}
	return f(*p)
}

func (f Finding) value() *js.Object {
	o := js.NewObject()
	set := func(keys ...string) {
		for _, k := range keys {
			switch k {
			case "check":
				o.Set(k, f.Check)
			case "severity":
				o.Set(k, f.Severity)
			case "transcript":
				o.Set(k, f.Transcript)
			case "line":
				o.Set(k, float64(f.Line))
			case "agent":
				o.Set(k, f.Agent)
			case "agentType":
				o.Set(k, f.AgentType)
			case "evidence":
				o.Set(k, f.Evidence)
			case "message":
				o.Set(k, f.Message)
			case "validationStatus":
				o.Set(k, f.ValidationStatus)
			case "kind":
				o.Set(k, f.Kind)
			}
		}
	}
	switch f.layout {
	case byCheck:
		set("check", "severity", "transcript", "line", "agent", "agentType", "evidence", "message", "validationStatus", "kind")
	case byBase:
		set("check", "transcript", "line", "agent", "agentType", "evidence", "severity", "message", "validationStatus", "kind")
	case byBaseKind:
		set("check", "transcript", "line", "agent", "agentType", "evidence", "severity", "kind", "message", "validationStatus")
	}
	return o
}

func (b SourceBytes) value() *js.Object {
	o := js.NewObject()
	for i, f := range b.fields() {
		o.Set(sourceNames[i], float64(*f))
	}
	return o
}

func (a *AgentContextUse) value() *js.Object {
	return js.Obj("agent", a.Agent, "agent_type", a.AgentType, "phase", null(a.Phase), "change", null(a.Change),
		"runtime", a.Runtime, "version", a.Version, "transcript", a.Transcript, "turns", float64(a.Turns), "turns_without_usage", float64(a.TurnsWithoutUsage),
		"peak_context_per_turn", a.Peak, "total_processed_context", a.Total,
		"input_tokens", a.Input, "cache_creation_input_tokens", a.CacheCreation, "cache_read_input_tokens", a.CacheRead,
		"tool_result_bytes", a.Bytes.value(), "same_file_rereads", float64(a.Rereads), "redundant_reloads", float64(a.Reloads),
		"whole_reads_over_limit", float64(a.OverLimit), "start", null(a.Start), "end", null(a.End), "reread_files", rereadFiles(a.RereadFiles))
}

func rereadFiles(fs []RereadFile) []any {
	out := []any{}
	for _, f := range fs {
		out = append(out, js.Obj("path", f.Path, "count", float64(f.Count)))
	}
	return out
}

func keyValue(k groupKey) any {
	if rv, ok := k.(runtimeVersion); ok {
		return js.Obj("runtime", rv.runtime, "version", rv.version)
	}
	return k
}

func (g *ContextUseGroup) value() *js.Object {
	return js.Obj("key", keyValue(g.Key), "agents", float64(g.Agents), "turns", float64(g.Turns), "turns_without_usage", float64(g.TurnsWithoutUsage),
		"total_processed_context", g.Total, "peak_context_per_turn_max", g.PeakMax,
		"input_tokens", g.Input, "cache_creation_input_tokens", g.CacheCreation, "cache_read_input_tokens", g.CacheRead,
		"tool_result_bytes", g.Bytes.value(), "same_file_rereads", float64(g.Rereads), "redundant_reloads", float64(g.Reloads),
		"whole_reads_over_limit", float64(g.OverLimit))
}

func groups(gs []*ContextUseGroup) []any {
	out := make([]any, len(gs))
	for i, g := range gs {
		out[i] = g.value()
	}
	return out
}

func (u *ContextUse) value() *js.Object {
	agents := make([]any, len(u.Agents))
	for i, a := range u.Agents {
		agents[i] = a.value()
	}
	long := []any{}
	for _, a := range u.LongAgents {
		long = append(long, js.Obj("agent", a.Agent, "agent_type", a.AgentType, "change", null(a.Change), "turns", float64(a.Turns), "peak", a.Peak, "why", a.Why))
	}
	return js.Obj("agents", agents, "long_agents", long, "by_phase", groups(u.ByPhase), "by_change", groups(u.ByChange),
		"by_agent_type", groups(u.ByAgentType), "by_runtime_version", groups(u.ByRuntime))
}

// value is the change as the report states it, with its slices, the
// owner's messages per slice, and its test runs by hand in any thread from
// its LDD row (l, nil for none).
func (c *ChangeOutcome) value(l *ChangeLDD) *js.Object {
	t := c.TestRuns
	slices, perSlice, byHand := 0.0, any(nil), 0.0
	if l != nil {
		slices, perSlice, byHand = float64(l.Slices), measure(l.PerSlice()), float64(l.ByHand)
	}
	return js.Obj("change", null(c.Change), "agents", float64(c.Agents), "launches", c.Launches,
		"start", c.Start, "end", c.End, "elapsed_ms", c.ElapsedMs, "active_ms", c.ActiveMs, "agent_ms", c.AgentMs,
		"main_ms", c.MainMs, "main_commands", float64(c.MainCommands), "main_files", float64(c.MainFiles), "main_tokens", c.MainTokens,
		"verify_runs", float64(c.VerifyRuns), "verify_verdicts", js.Obj("pass", float64(c.Verdicts[0]), "fail", float64(c.Verdicts[1]), "unknown", float64(c.Verdicts[2])),
		"post_verify_rewrites", float64(c.Replans), "unfinished", float64(c.Unfinished), "relaunches", float64(c.Relaunches),
		"slices", slices, "owner_messages_per_slice", perSlice, "tests_by_hand", byHand,
		"test_runs", js.Obj("count", float64(t.Count), "total_ms", t.TotalMs, "max_ms", t.MaxMs, "timeouts", float64(t.Timeouts),
			"failed", float64(t.Failed), "unknown", float64(t.Unknown)),
		"record", ptr(c.Record, func(r Record) any {
			return js.Obj("timestamp", r.Timestamp, "passed", r.Passed, "tests_passed", r.TestsPassed, "why", r.Why,
				"executions", float64(r.Executions), "duration_ms", r.DurationMs, "timeouts", float64(r.Timeouts))
		}))
}

// lddOf is the LDD row of change in ls; nil for none.
func lddOf(ls []ChangeLDD, change string) *ChangeLDD {
	for i := range ls {
		if ls[i].Change == change {
			return &ls[i]
		}
	}
	return nil
}

func (o *Outcomes) value(ls []ChangeLDD) *js.Object {
	changes := make([]any, len(o.Changes))
	for i, c := range o.Changes {
		changes[i] = c.value(lddOf(ls, c.Change))
	}
	str := func(s string) any { return s }
	return js.Obj("session", js.Obj("start", ptr(o.Start, str), "end", ptr(o.End, str), "elapsed_ms", o.ElapsedMs, "active_ms", o.ActiveMs,
		"attributed_ms", o.AttributedMs, "unattributed_ms", o.UnattributedMs, "resumes", float64(o.Resumes)), "changes", changes)
}

func (w *WriterEfficiency) value() *js.Object {
	var out any
	if w.OutOfScope != nil {
		out = strs(w.OutOfScope)
	}
	return js.Obj("agent", w.Agent, "agent_type", w.AgentType, "change", null(w.Change), "phase", null(w.Phase),
		"return_shape", w.ReturnShape, "files_written", float64(w.FilesWritten), "out_of_scope", out,
		"dependency_edits", strs(w.DependencyEdits), "max_same_failure", float64(w.MaxSameFailure), "bounded", w.Bounded, "active_ms", w.ActiveMs)
}

// Report is the `--json` report as a parsed JSON value (js.ParseJSON's form).
func (r *Result) Report() *js.Object {
	findings := make([]any, len(r.Findings))
	for i, f := range r.Findings {
		findings[i] = f.value()
	}
	errors := make([]any, len(r.Errors))
	for i, e := range r.Errors {
		errors[i] = js.Obj("transcript", e.Transcript, "line", float64(e.Line), "message", e.Message)
	}
	var contextUse, outcomes, efficiency any
	if r.ContextUse != nil {
		contextUse = r.ContextUse.value()
	}
	if r.Outcomes != nil {
		outcomes = r.Outcomes.value(r.LDD)
	}
	if r.Status != "audit-error" {
		writers := make([]any, len(r.Efficiency))
		for i, w := range r.Efficiency {
			writers[i] = w.value()
		}
		efficiency = js.Obj("writers", writers)
	}
	return js.Obj("ok", r.OK, "status", r.Status, "version", ptr(r.Version, func(v string) any { return v }),
		"session", ptr(r.Session, func(f SessionFacts) any {
			return js.Obj("sessionKind", f.SessionKind, "agentSetting", ptr(f.AgentSetting, func(v string) any { return v }))
		}),
		"findings", findings, "errors", errors, "notes", strs(r.Notes), "context_use", contextUse, "outcomes", outcomes, "efficiency", efficiency,
		"authorizations", authorizations(r.Authorizations), "worked", worked(r.Worked),
		"integrations", ptr(r.Integrations, func(x Integrations) any { return x.value() }), "timing", ptr(r.Timing, func(t Timing) any { return t.value() }),
		"dialogue", ptr(r.Dialogue, func(d Dialogue) any { return d.value() }))
}

// measure is a measure as a value: null for NaN.
func measure(x float64) any {
	if math.IsNaN(x) {
		return nil
	}
	return x
}

func (x ThreadTiming) value() *js.Object {
	return js.Obj("agent", x.Agent, "agent_type", x.AgentType, "change", null(x.Change), "turns", float64(x.Turns),
		"model_ms", x.ModelMs, "tool_ms", x.ToolMs, "first_response_ms", measure(x.FirstResponseMs),
		"processed", x.Processed, "cache_read", x.CacheRead, "cache_hit", measure(x.CacheHit))
}

func (t Timing) value() *js.Object {
	agents := []any{}
	for _, a := range t.Agents {
		agents = append(agents, a.value())
	}
	compactions := []any{}
	for _, c := range t.Compactions {
		compactions = append(compactions, js.Obj("at", c.At, "trigger", null(c.Trigger), "tokens_before", c.TokensBefore, "duration_ms", c.DurationMs))
	}
	return js.Obj("main", t.Main.value(), "agents", agents, "compactions", compactions)
}

// timingLine is one thread's timing as a person reads it.
func timingLine(x ThreadTiming) string {
	line := fmt.Sprintf("turns %d  model %s", x.Turns, Duration(x.ModelMs))
	if x.Turns > 0 {
		line += fmt.Sprintf(" (%s/turn)", Duration(x.ModelMs/float64(x.Turns)))
	}
	line += "  tools " + Duration(x.ToolMs)
	if !math.IsNaN(x.FirstResponseMs) {
		line += "  first response " + Duration(x.FirstResponseMs)
	}
	if !math.IsNaN(x.CacheHit) {
		line += fmt.Sprintf("  cache %d%%", int(math.Round(x.CacheHit*100)))
	}
	return line
}

// worked is what went right as the report lists it, in order.
func worked(ws []Worked) []any {
	out := []any{}
	for _, w := range ws {
		out = append(out, js.Obj("kind", w.Kind, "change", null(w.Change), "what", w.What, "evidence", w.Evidence))
	}
	return out
}

// authorizations are the owner's yeses as the report lists them, in order.
func authorizations(as []Authorization) []any {
	out := []any{}
	for _, a := range as {
		out = append(out, js.Obj("classes", strs(a.Classes), "scope", a.Scope, "at", a.At, "ask", a.Ask))
	}
	return out
}

// Duration is a duration as a person reads it: seconds, minutes, or hours to one decimal.
func Duration(ms float64) string {
	switch {
	case ms < 60_000:
		return js.NumberString(jsRound(ms/1000)) + "s"
	case ms < 3_600_000:
		return js.NumberString(jsRound(ms/60_000)) + "m"
	}
	return js.ToFixed(ms/3_600_000, 1) + "h"
}

// jsRound is Math.round: a half rounds up.
func jsRound(x float64) float64 {
	r := float64(int64(x))
	if x-r >= 0.5 {
		return r + 1
	}
	if x < 0 && r-x > 0.5 {
		return r - 1
	}
	return r
}

// humanOutcomes is the outcomes table: one row per change, the unattributed agents last, then the session.
func humanOutcomes(o *Outcomes) []string {
	head := []string{"change", "elapsed", "active", "agent-time", "main-time", "main-files", "launches", "verify", "replans", "relaunches", "tests"}
	var rows [][]string
	for _, c := range o.Changes {
		name := c.Change
		if name == "" {
			name = "(unknown)"
		}
		rows = append(rows, []string{name, Duration(c.ElapsedMs), Duration(c.ActiveMs), Duration(c.AgentMs), Duration(c.MainMs), fmt.Sprint(c.MainFiles), fmt.Sprint(c.Agents),
			fmt.Sprint(c.VerifyRuns), fmt.Sprint(c.Replans), fmt.Sprint(c.Relaunches), fmt.Sprint(c.TestRuns.Count)})
	}
	w := make([]int, len(head))
	for i, h := range head {
		w[i] = js.Len(h)
		for _, r := range rows {
			w[i] = max(w[i], js.Len(r[i]))
		}
	}
	format := func(r []string) string {
		parts := make([]string, len(r))
		for i, x := range r {
			parts[i] = js.PadEnd(x, w[i])
		}
		return strings.TrimRightFunc(strings.Join(parts, "  "), js.IsSpace)
	}
	out := []string{"  outcomes  " + format(head)}
	for _, r := range rows {
		out = append(out, "    "+format(r))
	}
	return append(out, fmt.Sprintf("  session  elapsed %s  active %s  unattributed %s  resumes %d",
		Duration(o.ElapsedMs), Duration(o.ActiveMs), Duration(o.UnattributedMs), o.Resumes))
}

// humanLDD is each change's slices and the owner's messages per slice
// (contracts/ldd.md, "Rounds"), and its test runs by hand when it had any.
func humanLDD(ls []ChangeLDD) []string {
	var out []string
	for _, l := range ls {
		per := "-"
		if p := l.PerSlice(); !math.IsNaN(p) {
			per = js.NumberString(p)
		}
		line := fmt.Sprintf("  ldd  %s  slices %d  owner messages %d  per slice %s", l.Change, l.Slices, l.OwnerMessages, per)
		if l.ByHand > 0 {
			line += fmt.Sprintf("  tests by hand %d", l.ByHand)
		}
		out = append(out, line)
	}
	return out
}

// bytesLine is the bytes by source, as one human-readable span.
func bytesLine(b SourceBytes) string {
	return fmt.Sprintf("read bytes full %d ranged %d shell %d  codegraph %d  engram %d  search %d  other %d",
		b.FullRead, b.RangedRead, b.ShellRead, b.Codegraph, b.Engram, b.Search, b.Other)
}

func num(f float64) string { return js.NumberString(f) }

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// humanContextUse is the context-use section `--context` adds.
func humanContextUse(u *ContextUse) []string {
	total := 0.0
	for _, a := range u.Agents {
		total += a.Total
	}
	share := func(n float64) string {
		if total == 0 {
			return "-"
		}
		return js.ToFixed(100*n/total, 1) + "%"
	}
	out := []string{"  context-use  measurement only; never a finding"}
	for _, a := range u.Agents {
		out = append(out, fmt.Sprintf("    agent  %s (%s)  phase %s  change %s  turns %d  peak %s  processed %s  rereads %d  whole reads over limit %d",
			a.Agent, a.AgentType, dash(a.Phase), dash(a.Change), a.Turns, num(a.Peak), num(a.Total), a.Rereads, a.OverLimit))
		out = append(out, "           "+bytesLine(a.Bytes))
	}
	label := func(k groupKey) string {
		switch x := k.(type) {
		case nil:
			return "-"
		case string:
			return x
		case runtimeVersion:
			return x.runtime + " " + x.version
		}
		return ""
	}
	for _, by := range []struct {
		name string
		rows []*ContextUseGroup
	}{{"phase", u.ByPhase}, {"change", u.ByChange}, {"agent type", u.ByAgentType}, {"runtime", u.ByRuntime}} {
		for _, g := range by.rows {
			out = append(out, fmt.Sprintf("    by %s  %s  agents %d  processed %s (%s)  peak max %s  %s",
				by.name, label(g.Key), g.Agents, num(g.Total), share(g.Total), num(g.PeakMax), bytesLine(g.Bytes)))
		}
	}
	return out
}

// Human is the human output of a result for the session at target.
func Human(r *Result, target string, context bool) string {
	version := "unknown"
	if r.Version != nil {
		version = *r.Version
	}
	runtime := "claude-code"
	if IsPiSession(target) {
		runtime = "pi session format"
	} else if r.Runtime != "" {
		runtime = r.Runtime
	}
	out := []string{fmt.Sprintf("audit  %s  (%s %s)", strings.TrimSuffix(filepath.Base(target), ".jsonl"), runtime, version)}
	if r.Session != nil {
		line := "  session  sessionKind: " + r.Session.SessionKind
		if r.Session.AgentSetting != nil && *r.Session.AgentSetting != "" {
			line += "  agentSetting: " + *r.Session.AgentSetting
		}
		out = append(out, line)
	}
	for _, e := range r.Errors {
		out = append(out, fmt.Sprintf("  audit-error  %s:%d  %s", e.Transcript, e.Line, e.Message))
	}
	violations := 0
	for _, f := range r.Findings {
		tag := ""
		switch f.Kind {
		case "finding":
		case "would-be-violation":
			tag = "  would-be violation"
		default:
			tag = "  " + f.Kind
		}
		if f.Severity == "violation" {
			violations++
		}
		out = append(out, fmt.Sprintf("  %s  %s  %s:%d  %s (%s)%s", js.PadEnd(f.Severity, 9), js.PadEnd(f.Check+" ["+f.ValidationStatus+"]", 42),
			f.Transcript, f.Line, f.Agent, f.AgentType, tag))
		out = append(out, "             "+f.Message)
		if f.Evidence != "" {
			out = append(out, "             evidence: "+f.Evidence)
		}
	}
	for _, n := range r.Notes {
		out = append(out, "  note  "+n)
	}
	if t := r.Timing; t != nil {
		out = append(out, "  timing  main  "+timingLine(t.Main))
		for _, a := range t.Agents {
			out = append(out, fmt.Sprintf("  timing  %s (%s)  change %s  %s", a.Agent, a.AgentType, dash(a.Change), timingLine(a)))
		}
		for _, c := range t.Compactions {
			line := fmt.Sprintf("  compaction  %s  %s  %.0fk tokens before", c.At, dash(c.Trigger), c.TokensBefore/1000)
			if c.DurationMs > 0 {
				line += "  took " + Duration(c.DurationMs)
			}
			out = append(out, line)
		}
	}
	if r.Dialogue != nil {
		out = append(out, humanDialogue(r.Dialogue)...)
	}
	if r.Integrations != nil {
		out = append(out, humanIntegrations(r.Integrations)...)
	}
	for _, w := range r.Worked {
		out = append(out, fmt.Sprintf("  worked  %s  %s  %s", js.PadEnd(w.Kind, 14), dash(w.Change), w.What))
		out = append(out, "             evidence: "+w.Evidence)
	}
	if r.Outcomes != nil && len(r.Outcomes.Changes) > 0 {
		out = append(out, humanOutcomes(r.Outcomes)...)
		out = append(out, humanLDD(r.LDD)...)
	}
	for _, w := range r.Efficiency {
		scope := "?"
		if w.OutOfScope != nil {
			scope = fmt.Sprint(len(w.OutOfScope))
		}
		out = append(out, fmt.Sprintf("  efficiency  %s (%s)  change %s  return %s  outside scope %s  dependencies %d  same failure max %d  active %s  files %d",
			w.Agent, w.AgentType, dash(w.Change), w.ReturnShape, scope, len(w.DependencyEdits), w.MaxSameFailure, Duration(w.ActiveMs), w.FilesWritten))
	}
	if r.ContextUse != nil {
		for _, a := range r.ContextUse.LongAgents {
			out = append(out, fmt.Sprintf("  long     %s (%s)  change %s  %s", a.Agent, a.AgentType, dash(a.Change), a.Why))
		}
		if context {
			out = append(out, humanContextUse(r.ContextUse)...)
		}
	}
	suspects := len(r.Findings) - violations
	switch {
	case len(r.Errors) > 0:
		out = append(out, fmt.Sprintf("audit-error: %d problem(s); no check ran", len(r.Errors)))
	case violations > 0 || suspects > 0:
		out = append(out, fmt.Sprintf("%d violation(s), %d suspect(s)", violations, suspects))
	default:
		out = append(out, r.Status)
	}
	return strings.Join(out, "\n")
}
