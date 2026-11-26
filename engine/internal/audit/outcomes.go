package audit

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	lddlayout "github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"math"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
)

// The outcomes measurement: per change, elapsed and active time, agent
// time, main-thread time and calls, launches, verify runs, planning rewrites
// after the first verify, and test runs. Never a finding.

// planning is the phases that plan a change; a launch of one after a verify of the change is a replan.
var planning = []string{"propose", "spec", "design", "tasks"}

// planningArtifact is a change's planning artifact, relative to its directory.
var planningArtifact = lazyre.New(`^(?:proposal\.md|design\.md|tasks\.md|specs/` + js.Dot + `+/spec\.md)$`)

var checkbox = lazyre.New(`\[[ xX]\]`)

// checkOffOnly is an Edit that only ticks or unticks checkboxes: an apply's progress, not a replan.
func checkOffOnly(u ToolUse) bool {
	old, ok1 := strProp(u.Input, "old_string")
	neu, ok2 := strProp(u.Input, "new_string")
	return u.Name == "Edit" && ok1 && ok2 && old != neu &&
		checkbox.ReplaceAllLiteralString(old, "[ ]") == checkbox.ReplaceAllLiteralString(neu, "[ ]")
}

// planningRewrites is the planning artifacts of change that t rewrote
// after since: a successful Write or Edit of its proposal, a spec, its
// design, or its tasks, other than a checkbox tick. The orchestrator plans
// inline, so a replan shows as these writes rather than as a planning
// agent's launch.
func planningRewrites(t *Transcript, change string, since float64) int {
	out := map[string]bool{}
	failed := failedIDs(t.Entries)
	for _, e := range t.Entries {
		if !(dateParse(e.Timestamp) > since) {
			continue
		}
		for _, u := range e.ToolUses {
			path, _ := strProp(u.Input, "file_path")
			at, marker := -1, ""
			for _, l := range lddlayout.Layouts {
				if m := l.ChangeDir(change) + "/"; strings.LastIndex(path, m) > at {
					at, marker = strings.LastIndex(path, m), m
				}
			}
			if _, w := writeToolKey[u.Name]; !w || failed[u.ID] || at < 0 || checkOffOnly(u) {
				continue
			}
			if rel := path[at+len(marker):]; planningArtifact.MatchString(rel) {
				out[rel] = true
			}
		}
	}
	return len(out)
}

type interval [2]float64

// activity is the active intervals of a transcript, one per turn: a turn
// opens at a prompt (a user entry that is no tool result: a message, a
// notification, a sub-agent's launch or resumption) and runs to the last
// entry before the next prompt. The time between a turn's end and the next
// prompt is idle.
func activity(t *Transcript) []interval {
	var out []interval
	for _, e := range t.Entries {
		at := dateParse(e.Timestamp)
		if math.IsNaN(at) || math.IsInf(at, 0) {
			continue
		}
		if len(out) == 0 || (e.Role == "user" && len(e.ToolResults) == 0) {
			out = append(out, interval{at, at})
		} else {
			last := &out[len(out)-1]
			last[1] = math.Max(last[1], at)
		}
	}
	return out
}

// union is intervals merged: overlap counted once.
func union(xs []interval) []interval {
	sorted := slices.Clone(xs)
	slices.SortStableFunc(sorted, func(p, q interval) int { return sign(p[0] - q[0]) })
	var out []interval
	for _, x := range sorted {
		if n := len(out); n > 0 && x[0] <= out[n-1][1] {
			out[n-1][1] = math.Max(out[n-1][1], x[1])
		} else {
			out = append(out, x)
		}
	}
	return out
}

func length(xs []interval) float64 {
	n := 0.0
	for _, x := range xs {
		n += x[1] - x[0]
	}
	return n
}

// minus is xs with ys taken out.
func minus(xs, ys []interval) []interval {
	out := union(xs)
	for _, y := range union(ys) {
		c, d := y[0], y[1]
		var next []interval
		for _, x := range out {
			a, b := x[0], x[1]
			if d <= a || c >= b {
				next = append(next, x)
				continue
			}
			if c > a {
				next = append(next, interval{a, c})
			}
			if d < b {
				next = append(next, interval{d, b})
			}
		}
		out = next
	}
	return out
}

// jsMin and jsMax are Math.min and Math.max: NaN when any value is.
func jsMin(xs ...float64) float64 {
	out := math.Inf(1)
	for _, x := range xs {
		if math.IsNaN(x) {
			return x
		}
		out = math.Min(out, x)
	}
	return out
}

func jsMax(xs ...float64) float64 {
	out := math.Inf(-1)
	for _, x := range xs {
		if math.IsNaN(x) {
			return x
		}
		out = math.Max(out, x)
	}
	return out
}

// TestRuns is a change's `jhon executions run` calls: Unknown counts those whose output the transcript does not hold.
type TestRuns struct {
	Count                     int
	TotalMs, MaxMs            float64
	Timeouts, Failed, Unknown int
}

// Record is a change's execution record on disk: its time, result, summed durations, and timeouts.
type Record struct {
	Timestamp string
	// Passed is a run delivery can rest on (RunVerdict): every project passed,
	// complete, on a committed tree that did not change during the run.
	Passed bool
	// TestsPassed is that every project passed and the run completed,
	// whatever the tree: a run on uncommitted changes passed its tests
	// without being one delivery can rest on. Why says what Passed lacks.
	TestsPassed bool
	Why         string
	Executions  int
	DurationMs  float64
	Timeouts    int
}

// ChangeOutcome is one change's outcome: "" change collects the agents no
// change can be attributed to. ElapsedMs runs from its first agent's or
// main-thread span's start to the last one's end; ActiveMs is the union of
// its agents' and its main-thread active intervals; AgentMs sums each
// agent's own active time, so parallel agents add up; MainMs is the main
// thread's.
type ChangeOutcome struct {
	Change     string
	Agents     int
	Launches   *js.Object
	Start, End string
	ElapsedMs  float64
	ActiveMs   float64
	AgentMs    float64
	MainMs     float64
	// MainCommands is the main thread's tool calls attributed to the change (mainAttribution); 0 for "".
	MainCommands int
	// MainFiles is the files the main thread wrote itself for the change:
	// beside MainMs, the direct side of what delegating cost (WriterEfficiency).
	MainFiles int
	// MainTokens is the context the main thread processed in its turns on the
	// change (mainAttribution), as its time is MainMs.
	MainTokens float64
	VerifyRuns int
	Verdicts   [3]int // pass, fail, unknown
	// Replans counts the planning after the change's first verify: each
	// planning agent launched after it, and each planning artifact the main
	// thread rewrote after it (planningRewrites). The report names it
	// post_verify_rewrites.
	Replans    int
	Unfinished int
	Relaunches int
	TestRuns   TestRuns
	Record     *Record
}

// Outcomes is the session's outcome: its elapsed span, the union of every
// active interval, the part of it the attributed changes account for, and
// the rest, unattributed; Resumes counts the times the owner reopened the
// session (Session.Resumes), not a sub-agent's continuations.
type Outcomes struct {
	Start, End                                        *string
	ElapsedMs, ActiveMs, AttributedMs, UnattributedMs float64
	Resumes                                           int
	Changes                                           []*ChangeOutcome
}

var (
	verdictLiteral = lazyre.New(ci(`\bverdict` + "`?" + `\**` + space + `*:` + space + `*\**` + "`?" + `(\w+)`))
	verdictLoose   = lazyre.New(ci(`\bverdict\b[^\n.]{0,30}?[` + js.SpaceChars + "`" + `*](pass|fail)\b`))
)

// verifyVerdict is the verdict a verify reported: the `verdict:` of the
// verify-report.md its own transcript last wrote whole, else the verdict its
// result states when every statement agrees; "" when neither is readable.
func verifyVerdict(a *Subagent, result *string, verdicts []string) string {
	results := map[string]ToolResult{}
	for _, e := range a.Entries {
		for _, r := range e.ToolResults {
			results[r.ID] = r
		}
	}
	var known *string
	for _, e := range a.Entries {
		for _, u := range e.ToolUses {
			path, _ := strProp(u.Input, "file_path")
			if u.Name != "Write" || !strings.HasSuffix(path, "/verify-report.md") || results[u.ID].IsError {
				continue
			}
			known = optString(prop(u.Input, "content"))
		}
	}
	if known != nil {
		if d := ldd.ReportVerdict(*known); d != "" && slices.Contains(verdicts, d) {
			return d
		}
	}
	if result == nil || *result == "" {
		return ""
	}
	agreed := func(matches [][]string) string {
		var values []string
		for _, m := range matches {
			if v := strings.ToLower(m[1]); slices.Contains(verdicts, v) {
				values = append(values, v)
			}
		}
		if len(values) == 0 || slices.ContainsFunc(values, func(v string) bool { return v != values[0] }) {
			return ""
		}
		return values[0]
	}
	literal := verdictLiteral.FindAllStringSubmatch(*result, -1)
	if slices.ContainsFunc(literal, func(m []string) bool { return slices.Contains(verdicts, strings.ToLower(m[1])) }) {
		return agreed(literal)
	}
	return agreed(verdictLoose.FindAllStringSubmatch(*result, -1))
}

// attribution is the main thread's work on one change.
type attribution struct {
	spans []interval
	calls int
	// files are the files the main thread wrote itself, absolute, once each.
	files []string
	// tokens is the context the main thread processed in its turns on the change.
	tokens float64
}

// mainAttribution is the main thread's work by change, in first-named
// order. Each tool call is the change the last call naming one named
// (namedBy), itself included; the time from an entry to the next one in the
// same turn is the change current after that entry. Work before any name
// stays unattributed.
// mainWrites is the files one main-thread tool call writes: a write tool's
// file, or what a Bash command writes (guard.WrittenFiles).
func mainWrites(u ToolUse, cwd string) []string {
	if key, ok := writeToolKey[u.Name]; ok {
		if p, ok := strProp(u.Input, key); ok {
			return []string{absolute(p, cwd)}
		}
		return nil
	}
	if c, ok := strProp(u.Input, "command"); ok && u.Name == "Bash" {
		return guard.WrittenFiles(c, cwd)
	}
	return nil
}

func mainAttribution(s *Session) ([]string, map[string]*attribution) {
	var order []string
	out := map[string]*attribution{}
	row := func(c string) *attribution {
		if out[c] == nil {
			out[c] = &attribution{}
			order = append(order, c)
		}
		return out[c]
	}
	es := s.Main.Entries
	current := ""
	// A turn's usage counts once (an assistant message can span several lines).
	counted := map[string]bool{}
	for i, e := range es {
		for _, u := range e.ToolUses {
			if n := u.named; n != "" {
				current = n
			}
			if current != "" {
				r := row(current)
				r.calls++
				for _, f := range mainWrites(u, e.Cwd) {
					addOnce(&r.files, f)
				}
			}
		}
		if current != "" && e.Role == "assistant" && e.Usage != nil {
			key := fmt.Sprintf("line:%d", e.Line)
			if e.MessageID != nil {
				key = *e.MessageID
			}
			if !counted[key] {
				counted[key] = true
				row(current).tokens += e.Usage.Input + e.Usage.CacheCreation + e.Usage.CacheRead
			}
		}
		if current == "" || i+1 >= len(es) || (es[i+1].Role == "user" && len(es[i+1].ToolResults) == 0) {
			continue
		}
		a, b := dateParse(e.Timestamp), dateParse(es[i+1].Timestamp)
		if !math.IsNaN(a) && !math.IsNaN(b) && b > a {
			row(current).spans = append(row(current).spans, interval{a, b})
		}
	}
	return order, out
}

// capRefusal is the refusal an agent past its turn cap gets on its next
// call: pi's (shell/extensions/jhon.ts), Claude Code's retired turn guard
// (in transcripts from before c7074cfa), and Claude Code's own maxTurns stop.
var capRefusal = lazyre.New(`you have spent your \d+ turns|turn-guard: .* has spent \d+ turns, past its cap|reached maxTurns`)

// contextCapRefusal is the refusal a pi child past its context cap got on
// its next call before T155 (shell/lib/child-budget.ts childCapReason at
// 3118fbc, the reason of the call's block: `jhon: your context is at 151K
// tokens, past the 150K-token sub-agent context cap. Call no more tools: …`).
// Only a call's error is one: the same words a tool printed (a file read,
// a grep of the shell's tests) are data.
var contextCapRefusal = lazyre.New(`^jhon: your context is at \d+K tokens, past the \d+K-token sub-agent context cap\. Call no more tools`)

// capRefused is capRefusal.MatchString(text): each of its branches starts
// with a literal, so a text with none of them cannot match and is not run
// through the regex, whose `.*` is slow on a long tool result.
func capRefused(text string) bool {
	if !strings.Contains(text, "you have spent your ") && !strings.Contains(text, "turn-guard: ") && !strings.Contains(text, "reached maxTurns") {
		return false
	}
	return capRefusal.MatchString(text)
}

// budgetNotice is the notice a pi child hears once its context is past its
// budget (shell/lib/child-budget.ts childBudgetNotice, since T122), which the
// shell appends to a tool result as its last text part, before only the read
// budget's reminder (childReads, `Read budget: …`) when both are due, each a
// text part of its own after the tool's output. Only a result that ends so
// carries it (appendedNotice): the same words inside a result (a file read,
// a grep) or at the end of a tool's own output (echo -n, a file with no
// trailing newline) are data.
var budgetNotice = lazyre.New(`Context budget: this sub-agent's context is at \d+K tokens, past its \d+K-token budget\. ` +
	`Wrap up: finish the step in hand, then return what you found as path:line evidence and decisions, not raw output, and what remains\.` +
	`(Read budget: \d+ lookups, \d+ bytes without [^\n]*)?$`)

// appendedNotice is whether the shell appended the budget notice to r: the
// notice, and the read budget's reminder after it when there is one, each
// a text part of its own after the tool's output.
func appendedNotice(r ToolResult) bool {
	if len(r.parts) == 0 || !strings.Contains(r.Shown, "Context budget: ") {
		return false
	}
	loc := budgetNotice.FindStringSubmatchIndex(r.Shown)
	return loc != nil && slices.Contains(r.parts, loc[0]) && (loc[2] < 0 || slices.Contains(r.parts, loc[2]))
}

// checkpoints is the agents told to stop at the budget notice and return
// what is done and what remains (prose/agents/jhon-builder.md): a
// builder-type agent, as the shell's read budget names one
// (shell/lib/child-budget.ts readBudgetKind). A scout returns what it found
// there, its finished part; a checker or a review facet is told nothing.
// The retired jd-fix-agent, a builder in the read budget until T172 removed
// it, is not one: it never ran on pi (T263).
var checkpoints = lazyre.New(`-(?:builder|worker)$`)

// atContextBudget is whether the agent stopped at its context budget: a
// builder-type agent whose last run, after its launch or its last continue,
// heard the budget notice. What it returned then is a checkpoint, not its
// finished part.
func atContextBudget(a *Subagent) bool {
	if !checkpoints.MatchString(a.AgentType) {
		return false
	}
	heard := false
	for _, e := range a.Entries {
		if e.Role == "user" && len(e.ToolResults) == 0 {
			heard = false // a continue's message starts a new run
		}
		for _, r := range e.ToolResults {
			if appendedNotice(r) {
				heard = true
			}
		}
	}
	return heard
}

// capped is whether the agent ran into its turn cap, or its context cap
// before T155: a call of its was refused for it. What it returned then is
// what it had, not its finished work.
func capped(a *Subagent) bool {
	for _, e := range a.Entries {
		for _, r := range e.ToolResults {
			if capRefused(r.Text) || (r.IsError && contextCapRefusal.MatchString(r.Shown)) {
				return true
			}
		}
	}
	return false
}

// stoppedShort is a result that says the agent stopped short at its turn
// cap, or the note a pi parent's result ended with before T155 when its
// child stopped at its context cap (shell/lib/child-budget.ts capNote at
// 3118fbc: `[jhon: worker stopped at its sub-agent context cap; …]`), in
// its bracketed form with an agent's name only: an answer that mentions the
// words, or quotes the form with a placeholder for the name, is no stop.
var stoppedShort = lazyre.New(ci(`\bmax(?:imum)?[ -]?turns\b|\bturn (?:cap|limit)\b|\[jhon: [A-Za-z0-9][A-Za-z0-9_.-]* stopped at its sub-agent context cap;`))

// jhonRefusal is one of Jhon's own refusal lines, quoted in a result or not,
// by the exact prefix it is printed with (T224 S2): a guard's
// `<name>-guard: <reason>` (engine/internal/hook; safety-guard's is
// `safety-guard: refused: …`), pre-tool's sandbox refusal `sandbox: …; the
// call is refused` and the sandbox's note on a refused write, `the sandbox
// refused a write to …` (engine/internal/sandbox). The word "refused:"
// anywhere else in a tool's output is no refusal. The prefix starts a word,
// after markdown or a bracket too (`**safety-guard: …**`, `(…)`, `[…]`,
// `_…_`, `__…__`), never inside another (`my-safety-guard: `,
// `x_safety-guard: `, `x__safety-guard: `).
var jhonRefusal = lazyre.New(`(?:^|[^A-Za-z0-9_-])_{0,2}(?:(?:safety|scope|route|vcs|secrets|framework|inspect)-guard: |sandbox: [^\n]*; the call is refused)|the sandbox refused a write to `)

// stopped is whether a result says the agent stopped short: its turn or context cap, or a refusal of Jhon's.
func stopped(result string) bool {
	return stoppedShort.MatchString(result) || jhonRefusal.MatchString(result)
}

// relaunches is the launches among agents (one change's) that follow, for
// the same agent type, one that stopped short: no result delivered, a
// result reporting its turn cap or a refusal, a stop at its context
// budget (atContextBudget), or a last continue that never answered.
// Continuing that agent with SendMessage keeps its context; a fresh launch
// reads everything again.
func relaunches(s *Session, agents []*Subagent) int {
	start := func(a *Subagent) string {
		if len(a.Entries) == 0 {
			return ""
		}
		return a.Entries[0].Timestamp
	}
	ordered := slices.Clone(agents)
	slices.SortStableFunc(ordered, func(p, q *Subagent) int { return js.LocaleCompare(start(p), start(q)) })
	n := 0
	for i, a := range ordered {
		var prev *Subagent
		for k := i - 1; k >= 0; k-- {
			if ordered[k].AgentType == a.AgentType {
				prev = ordered[k]
				break
			}
		}
		if prev == nil {
			continue
		}
		if index, result := completion(s, prev); index < 0 || stopped(result) || atContextBudget(prev) || prev.unanswered {
			n++
		}
	}
	return n
}

// computeOutcomes is the outcomes of a loaded session.
func computeOutcomes(s *Session, ev evidence) *Outcomes {
	verdicts := contract.List(vocab(), "envelope_verdict")
	var runs []testRun
	for _, r := range ev.runs {
		if r.direct == "" {
			runs = append(runs, r)
		}
	}
	mainOrder, mainWork := mainAttribution(s)
	var rowOrder []string
	rows := map[string][]*Subagent{}
	seenRow := map[string]bool{}
	for _, a := range s.Subagents {
		if !seenRow[a.change] {
			seenRow[a.change] = true
			rowOrder = append(rowOrder, a.change)
		}
		rows[a.change] = append(rows[a.change], a)
	}
	for _, c := range mainOrder {
		if !seenRow[c] {
			seenRow[c] = true
			rowOrder = append(rowOrder, c)
		}
	}
	started := func(a *Subagent) float64 {
		if len(a.Entries) == 0 {
			return math.NaN()
		}
		return dateParse(a.Entries[0].Timestamp)
	}
	var changes []*ChangeOutcome
	for _, change := range rowOrder {
		agents := rows[change]
		var all []interval
		agentMs := 0.0
		for _, a := range agents {
			if iv := activity(&a.Transcript); len(iv) > 0 {
				all = append(all, iv...)
				agentMs += length(union(iv))
			}
		}
		var mainSpans []interval
		if change != "" && mainWork[change] != nil {
			mainSpans = mainWork[change].spans
		}
		all = append(all, mainSpans...)
		start, end := 0.0, 0.0
		if len(all) > 0 {
			start, end = math.Inf(1), math.Inf(-1)
			for _, x := range all {
				start, end = math.Min(start, x[0]), math.Max(end, x[1])
			}
		}
		c := &ChangeOutcome{Change: change, Agents: len(agents), Launches: js.NewObject(), ElapsedMs: end - start, ActiveMs: length(union(all)),
			AgentMs: agentMs, MainMs: length(union(mainSpans))}
		if len(all) > 0 {
			c.Start, c.End = iso(start), iso(end)
		}
		for _, a := range agents {
			n, _ := prop(c.Launches, a.AgentType).(float64)
			c.Launches.Set(a.AgentType, n+1)
		}
		if change != "" && mainWork[change] != nil {
			c.MainCommands = mainWork[change].calls
			c.MainFiles = len(mainWork[change].files)
			c.MainTokens = mainWork[change].tokens
		}
		var verifies []*Subagent
		var starts []float64
		for _, a := range agents {
			if a.phase == "verify" {
				verifies = append(verifies, a)
				starts = append(starts, started(a))
			}
		}
		firstVerify := jsMin(starts...)
		for _, v := range verifies {
			var result *string
			if index, r := completion(s, v); index >= 0 {
				result = &r
			} else if text, last := lastText(v.Entries); last != nil {
				result = &text
			}
			// A verify cut at its cap left the report's provisional fail: no verdict.
			verdict := verifyVerdict(v, result, verdicts)
			if capped(v) {
				verdict = ""
			}
			switch verdict {
			case "pass":
				c.Verdicts[0]++
			case "fail":
				c.Verdicts[1]++
			default:
				c.Verdicts[2]++
			}
		}
		c.VerifyRuns = len(verifies)
		for _, a := range agents {
			if slices.Contains(planning, a.phase) && started(a) > firstVerify {
				c.Replans++
			}
			if index, _ := completion(s, a); index < 0 || capped(a) || atContextBudget(a) || a.unanswered {
				c.Unfinished++
			}
		}
		if change != "" {
			c.Replans += planningRewrites(s.Main, change, firstVerify)
		}
		c.Relaunches = relaunches(s, agents)
		if change != "" {
			var durations []float64
			for _, r := range runs {
				if r.change != change {
					continue
				}
				d := 0.0
				if r.durationMs != nil {
					d = *r.durationMs
				}
				durations = append(durations, d)
				c.TestRuns.Count++
				c.TestRuns.TotalMs += d
				c.TestRuns.Timeouts += countOf(r.statuses, "timeout")
				if r.passed == nil {
					c.TestRuns.Unknown++
				} else if !*r.passed {
					c.TestRuns.Failed++
				}
			}
			c.TestRuns.MaxMs = jsMax(append([]float64{0}, durations...)...)
			disk, _ := diskEvidence(change, changeRoots(s, change))
			slices.SortStableFunc(disk, func(p, q testRun) int { return js.LocaleCompare(q.at, p.at) })
			if len(disk) > 0 {
				d := disk[0].asNext()
				testsPassed := len(d.statuses) > 0 && countOf(d.statuses, "pass") == len(d.statuses) && !strings.Contains(d.why, "did not complete")
				c.Record = &Record{Timestamp: d.at, Passed: d.passed != nil && *d.passed, TestsPassed: testsPassed, Why: d.why,
					Executions: len(d.statuses), DurationMs: *d.durationMs, Timeouts: countOf(d.statuses, "timeout")}
			}
		}
		changes = append(changes, c)
	}
	slices.SortStableFunc(changes, func(p, q *ChangeOutcome) int {
		switch {
		case p.Change == "":
			return 1
		case q.Change == "":
			return -1
		}
		return js.LocaleCompare(p.Start, q.Start)
	})
	var every []interval
	every = append(every, activity(s.Main)...)
	var attributedSpans []interval
	for _, a := range s.Subagents {
		iv := activity(&a.Transcript)
		every = append(every, iv...)
		if a.change != "" {
			attributedSpans = append(attributedSpans, iv...)
		}
	}
	for _, c := range mainOrder {
		attributedSpans = append(attributedSpans, mainWork[c].spans...)
	}
	active := union(every)
	rest := length(minus(active, union(attributedSpans)))
	o := &Outcomes{ActiveMs: length(active), AttributedMs: length(active) - rest, UnattributedMs: rest, Changes: changes}
	if len(every) > 0 {
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, x := range every {
			lo, hi = math.Min(lo, x[0]), math.Max(hi, x[1])
		}
		first, last := iso(lo), iso(hi)
		o.Start, o.End, o.ElapsedMs = &first, &last, hi-lo
	}
	o.Resumes = len(s.Resumes)
	return o
}

func countOf(xs []string, x string) int {
	n := 0
	for _, v := range xs {
		if v == x {
			n++
		}
	}
	return n
}
