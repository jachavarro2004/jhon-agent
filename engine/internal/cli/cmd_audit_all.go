package cli

import (
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/audit"
	"github.com/jachavarro2004/jhon-agent/engine/internal/dashboard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/manifest"
	"github.com/jachavarro2004/jhon-agent/engine/internal/session"
)

// `jhon audit --all [--since <n>d | <YYYY-MM-DD>] [--json] [--draft [<dir>]
// [--min <n>] [--refresh]]`: every Claude Code session under
// ~/.claude/projects and every Jhon pi session under ~/.pi/jhon/sessions
// changed since then (30 days by default), audited in parallel and
// aggregated (audit.Aggregate), the owner's dialogue summed
// (dialogueLines), and each agent role's use of memory, CodeGraph and the
// todo list summed last (integrationLines). Read-only, except --draft (T55): the
// findings that repeat at least --min times (2 by default), grouped by
// check, anchor, runtime, agent type, and cause, each drafted as
// <dir>/<check>-<key>.md (audit.GroupDrafts), by default under
// $XDG_STATE_HOME/jhon/audit-drafts; an existing draft is kept, and
// --refresh rewrites only its counts and its Evidence. Exit 0, or 2 on a
// usage error.
const auditAllUsage = "usage: jhon audit --all [--since <n>d | <YYYY-MM-DD>] [--json] [--draft [<dir>] [--min <n>] [--refresh]]"

// auditAllOptions are `jhon audit --all`'s options; draft is "" without --draft.
type auditAllOptions struct {
	json, refresh bool
	since, draft  string
	min           int
}

// draftsDir is --draft's default directory: $XDG_STATE_HOME/jhon/audit-drafts.
func draftsDir() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" || !filepath.IsAbs(state) {
		home, _ := os.UserHomeDir()
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "jhon", "audit-drafts")
}

// parseAuditAll reads argv; ok is false on a usage error.
func parseAuditAll(argv []string) (auditAllOptions, bool) {
	o := auditAllOptions{since: "30d", min: 2}
	drafting, minSet := false, false
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--all":
		case "--json":
			o.json = true
		case "--refresh":
			o.refresh = true
		case "--draft":
			drafting, o.draft = true, draftsDir()
			if i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "-") {
				i++
				o.draft = argv[i]
			}
		case "--since", "--min":
			if i+1 >= len(argv) {
				return auditAllOptions{}, false
			}
			i++
			if argv[i-1] == "--since" {
				o.since = argv[i]
				continue
			}
			n, err := strconv.Atoi(argv[i])
			if err != nil || n < 1 {
				return auditAllOptions{}, false
			}
			o.min, minSet = n, true
		default:
			return auditAllOptions{}, false
		}
	}
	if !drafting && (minSet || o.refresh) {
		return auditAllOptions{}, false
	}
	return o, true
}

func runAuditAll(argv []string) (int, error) {
	o, ok := parseAuditAll(argv)
	if !ok {
		fmt.Fprintln(os.Stderr, auditAllUsage)
		return 2, nil
	}
	json, since := o.json, o.since
	cutoff, ok := sinceCutoff(since, time.Now())
	if !ok {
		fmt.Fprintf(os.Stderr, "jhon audit: --since takes <n>d or a YYYY-MM-DD date, not '%s'\n%s\n", since, auditAllUsage)
		return 2, nil
	}
	home, _ := os.UserHomeDir()
	type target struct {
		path string
		pi   bool
	}
	var targets []target
	for _, s := range dashboard.Sessions(filepath.Join(home, ".claude", "projects"), 1<<30, false) {
		if t, ok := isodate.Parse(s.Modified); ok && !t.Before(cutoff) {
			targets = append(targets, target{s.Path, false})
		}
	}
	pi, _ := filepath.Glob(filepath.Join(home, ".pi", "jhon", "sessions", "*", "*.jsonl"))
	for _, p := range pi {
		if st, err := os.Stat(p); err == nil && !st.ModTime().Before(cutoff) {
			targets = append(targets, target{p, true})
		}
	}

	results := make([]audit.Audited, len(targets))
	next := make(chan int)
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Go(func() {
			for i := range next {
				r := audit.Audit(targets[i].path)
				jhon := targets[i].pi || r.Session != nil && r.Session.RunsAs(session.MainAgents)
				results[i] = audit.Audited{Path: targets[i].path, Jhon: jhon, Result: r}
				if targets[i].pi {
					results[i].UIUse = audit.UIUses(targets[i].path)
				} else if jhon {
					results[i].Unguarded = audit.Unguarded(targets[i].path, r)
				}
			}
		})
	}
	for i := range targets {
		next <- i
	}
	close(next)
	wg.Wait()

	if o.draft != "" {
		out, err := audit.WriteDrafts(o.draft, audit.GroupDrafts(results, o.min), o.refresh)
		if err != nil {
			return 1, err
		}
		// On stderr, so --json's output stays one document.
		fmt.Fprintf(os.Stderr, "drafts in %s (groups of %d or more): %d written, %d refreshed, %d kept\n", o.draft, o.min, len(out.Written), len(out.Refreshed), len(out.Kept))
		for _, p := range out.Written {
			fmt.Fprintln(os.Stderr, "  written  "+p)
		}
		for _, p := range out.Refreshed {
			fmt.Fprintln(os.Stderr, "  refreshed  "+p)
		}
	}

	caps := turnCaps()
	f := audit.Aggregate(results, caps, cutoff)
	allowed := audit.ModeAllowed(cutoff)
	if json {
		agents := []any{}
		for _, a := range f.AgentTypes {
			agents = append(agents, js.Obj("agent_type", a.AgentType, "agents", float64(a.Agents), "median_turns", float64(a.MedianTurns),
				"p90_turns", float64(a.P90Turns), "max_turns", float64(a.MaxTurns), "cap", capValue(a.Cap), "over_cap", float64(a.OverCap),
				"tokens", a.Tokens, "over_cap_token_share", a.OverCapTokenShare))
		}
		fmt.Println(js.StringifyValue(js.Obj("since", isodate.Format(cutoff), "sessions", float64(f.Sessions), "jhon_sessions", float64(f.JhonSessions),
			"audit_errors", float64(f.Errors), "turn_caps", capsValue(caps), "findings", counts(f.Findings), "jhon_findings", counts(f.JhonFindings),
			"agent_types", agents, "ui_use", uiUse(f.UIUse), "unguarded", unguarded(f.Unguarded),
			"guards", guardsValue(f.Guards), "mode_allowed", js.Obj("yolo", float64(allowed["yolo"]), "super-yolo", float64(allowed["super-yolo"])),
			"integrations", f.Integrations.Value(), "jhon_integrations", f.JhonIntegrations.Value(), "ldd", lddValue(f.LDD), "dialogue", f.Dialogue.Value(), "jhon_dialogue", f.JhonDialogue.Value())))
		return 0, nil
	}
	lines := []string{fmt.Sprintf("audit --all  since %s  %d session(s), %d Jhon, %d audit-error(s)", cutoff.Format("2006-01-02"), f.Sessions, f.JhonSessions, f.Errors), "", "findings (all sessions | Jhon's)"}
	checks := slices.Sorted(func(yield func(string) bool) {
		for c := range f.Findings {
			if !yield(c) {
				return
			}
		}
	})
	for _, c := range checks {
		lines = append(lines, fmt.Sprintf("  %s  %s | %s", js.PadEnd(c, 20), severities(f.Findings[c]), severities(f.JhonFindings[c])))
	}
	if len(checks) == 0 {
		lines = append(lines, "  none")
	}
	lines = append(lines, "", subAgentsHeader(),
		"  type                agents  median  p90   max  mark      past  tokens      past share")
	for _, a := range f.AgentTypes {
		cap, over, share := "-", "-", "-"
		if a.Cap > 0 {
			cap, over, share = strconv.Itoa(a.Cap), strconv.Itoa(a.OverCap), fmt.Sprintf("%.0f%%", 100*a.OverCapTokenShare)
		}
		lines = append(lines, fmt.Sprintf("  %s  %6d  %6d  %4d  %4d  %4s  %8s  %-10s  %s", js.PadEnd(a.AgentType, 18), a.Agents, a.MedianTurns,
			a.P90Turns, a.MaxTurns, cap, over, tokens(a.Tokens), share))
	}
	if len(f.AgentTypes) == 0 {
		lines = append(lines, "  none")
	}
	lines = append(append(lines, ""), lddLines(f.LDD)...)
	lines = append(lines, "", "guards in Jhon's sessions since the cutoff: what each refused and what it cost (lost: until the agent's next tool call went through)",
		"  guard            refusals  sessions  lost median  lost total  stuck  covers")
	for _, g := range f.Guards {
		card, _ := guard.CardOf(g.Guard)
		lines = append(lines, fmt.Sprintf("  %s  %8d  %8d  %-11s  %-10s  %5d  %s", js.PadEnd(g.Guard, 15), g.Refusals, g.Sessions, duration(g.LostMedianMs), duration(g.LostSumMs), g.Stuck, card.Covers))
	}
	// The calls the owner's mode let through, from the modes' log (`jhon mode`).
	lines = append(lines, fmt.Sprintf("  allowed by mode: %d (yolo), %d (super-yolo)", allowed["yolo"], allowed["super-yolo"]))
	lines = append(lines, "  A guard that refuses nothing for rounds, or whose refusals cost more than they prevent, comes up to be removed or replaced (contracts/ldd.md, principle 3).")
	lines = append(lines, "", fmt.Sprintf("Jhon sessions that ran tools with none of Jhon's hooks (Claude Code %s or later): %d", audit.PluginOnly, len(f.Unguarded)))
	for _, p := range slices.Sorted(slices.Values(f.Unguarded)) {
		lines = append(lines, "  "+p)
	}
	pis := 0
	for _, t := range targets {
		if t.pi {
			pis++
		}
	}
	lines = append(lines, "", fmt.Sprintf("jhon-shell UI in pi (%d Jhon pi session(s)): uses of its commands and shortcuts", pis))
	if len(f.UIUse) == 0 {
		lines = append(lines, "  none recorded")
	}
	for _, k := range slices.Sorted(maps.Keys(f.UIUse)) {
		lines = append(lines, fmt.Sprintf("  %s  %d", js.PadEnd(k, 28), f.UIUse[k]))
	}
	lines = append(append(lines, ""), dialogueLines(f.Dialogue, f.JhonDialogue)...)
	lines = append(append(lines, ""), integrationLines(f.Integrations, f.JhonIntegrations)...)
	fmt.Println(strings.Join(lines, "\n"))
	return 0, nil
}

// subAgentsHeader is the line above the sub-agents table.
func subAgentsHeader() string {
	return "sub-agents of Jhon's sessions started since the cutoff, each against the review mark of " + strconv.Itoa(reviewTurns) + " turns, or its turn cap where its manifest names one (past it: counted; pi stops no run there)"
}

// sinceCutoff is when --since starts: <n>d days before now, or a date.
func sinceCutoff(since string, now time.Time) (time.Time, bool) {
	if days, ok := strings.CutSuffix(since, "d"); ok {
		n, err := strconv.Atoi(days)
		return now.AddDate(0, 0, -n), err == nil && n >= 0
	}
	t, err := time.ParseInLocation("2006-01-02", since, time.Local)
	return t, err == nil
}

// reviewTurns is the mark an agent with no turn cap is measured against:
// past it, it is counted for the owner to look at, never stopped. No agent
// of the release has a cap (T131), and pi stops no run at a manifest's.
const reviewTurns = 100

// turnCaps are each agent's mark in the release's agents.yaml.
func turnCaps() map[string]int { return turnMarks(manifest.MustLoadRelease().Agents) }

// turnMarks are each agent's turn cap, or reviewTurns where it has none.
func turnMarks(agents []manifest.Agent) map[string]int {
	marks := map[string]int{}
	for _, a := range agents {
		marks[a.Name] = reviewTurns
		if n, err := strconv.Atoi(a.MaxTurns); err == nil && n > 0 {
			marks[a.Name] = n
		}
	}
	return marks
}

// capValue is a cap as the JSON prints it: null for an agent with none.
func capValue(cap int) any {
	if cap <= 0 {
		return nil
	}
	return float64(cap)
}

func capsValue(caps map[string]int) any {
	out := js.Obj()
	for _, name := range slices.Sorted(maps.Keys(caps)) {
		out.Set(name, float64(caps[name]))
	}
	return out
}

func unguarded(paths []string) any {
	out := []any{}
	for _, p := range slices.Sorted(slices.Values(paths)) {
		out = append(out, p)
	}
	return out
}

func uiUse(m map[string]int) any {
	o := js.NewObject()
	for _, k := range slices.Sorted(maps.Keys(m)) {
		o.Set(k, float64(m[k]))
	}
	return o
}

func counts(m map[string]map[string]int) any {
	o := js.NewObject()
	for _, c := range slices.Sorted(func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}) {
		s := js.NewObject()
		for _, sev := range []string{"violation", "suspect"} {
			if n := m[c][sev]; n > 0 {
				s.Set(sev, float64(n))
			}
		}
		o.Set(c, s)
	}
	return o
}

func severities(m map[string]int) string {
	if len(m) == 0 {
		return "-"
	}
	var out []string
	for _, sev := range []string{"violation", "suspect"} {
		if n := m[sev]; n > 0 {
			out = append(out, fmt.Sprintf("%d %s", n, sev))
		}
	}
	return strings.Join(out, ", ")
}

func tokens(n float64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.1fB", n/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.1fM", n/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.0fk", n/1e3)
	}
	return fmt.Sprintf("%.0f", n)
}

// duration is a span in milliseconds as a short human length; "-" for none.
func duration(ms float64) string {
	switch {
	case math.IsNaN(ms) || ms <= 0:
		return "-"
	case ms < 60e3:
		return fmt.Sprintf("%.0fs", ms/1e3)
	case ms < 3600e3:
		return fmt.Sprintf("%.0fm", ms/60e3)
	}
	return fmt.Sprintf("%.1fh", ms/3600e3)
}

func guardsValue(gs []audit.GuardStats) any {
	out := []any{}
	for _, g := range gs {
		card, _ := guard.CardOf(g.Guard)
		out = append(out, js.Obj("guard", g.Guard, "refusals", float64(g.Refusals), "sessions", float64(g.Sessions),
			"lost_median_ms", g.LostMedianMs, "lost_total_ms", g.LostSumMs, "stuck", float64(g.Stuck), "covers", card.Covers, "because", card.Because))
	}
	return out
}

// lddLines is the LDD table: each change's slices and the tasks of the
// largest, so an inflated slice shows.
func lddLines(cs []audit.ChangeLDD) []string {
	lines := []string{"LDD: each change of Jhon's sessions started since the cutoff (max tasks: the tasks of its largest slice; to main: from its start to its last merge into the default branch; commits: its successful git commits; by hand: suite-wide test runs outside jhon executions, the main session's and whole-project ones in sub-agents)",
		"  change                                  slices  max tasks  owner msgs  per slice  active   merges  to main  commits  by hand"}
	for _, c := range cs {
		per := "-"
		if p := c.PerSlice(); !math.IsNaN(p) {
			per = fmt.Sprintf("%.1f", p)
		}
		lines = append(lines, fmt.Sprintf("  %s  %6d  %9d  %10d  %9s  %-7s  %6d  %-7s  %7d  %d", js.PadEnd(c.Change, 38), c.Slices, c.MaxTasks, c.OwnerMessages, per, duration(c.ActiveMs), c.Merges, duration(c.ToMainMs()), c.Commits, c.ByHand))
	}
	if len(cs) == 0 {
		lines = append(lines, "  none")
	}
	return lines
}

func lddValue(cs []audit.ChangeLDD) any {
	out := []any{}
	for _, c := range cs {
		var toMain, perSlice any
		if ms := c.ToMainMs(); !math.IsNaN(ms) {
			toMain = ms
		}
		if p := c.PerSlice(); !math.IsNaN(p) {
			perSlice = p
		}
		out = append(out, js.Obj("change", c.Change, "start", c.Start, "slices", float64(c.Slices), "max_tasks", float64(c.MaxTasks), "owner_messages", float64(c.OwnerMessages),
			"owner_messages_per_slice", perSlice,
			"active_ms", c.ActiveMs, "merges", float64(c.Merges), "to_main_ms", toMain, "commits", float64(c.Commits), "tests_by_hand", float64(c.ByHand)))
	}
	return out
}

// dialogueLines is the owner's dialogue summed across the sessions, Jhon's
// apart (audit.FleetDialogue): statistics, never a finding, and counts only.
func dialogueLines(all, jhon audit.FleetDialogue) []string {
	lines := []string{"owner dialogue in the main threads, as statistics (per answer: responses, tool calls and time until the session next waits for the owner, median / max over every answer; " +
		"1q to 4+q: the ask-tool calls that put that many questions; prose questions: turns that end in a question in plain text with no ask-tool call, a lower bound; " +
		"corrections: the owner's messages that correct the agent, by kind; keep-going never counts an answer to a question)",
		fmt.Sprintf("  %s  %8s  %7s  %-9s  %-10s  %-11s  %14s  %4s  %4s  %4s  %4s  %15s", js.PadEnd("", 6),
			"sessions", "answers", "responses", "tool calls", "time", "ask-tool calls", "1q", "2q", "3q", "4+q", "prose questions")}
	pair := func(median, max float64, show func(float64) string) string {
		if math.IsNaN(median) {
			return "-"
		}
		return show(median) + " / " + show(max)
	}
	count := func(n float64) string { return strconv.FormatFloat(n, 'f', -1, 64) }
	rows := []struct {
		name string
		d    audit.FleetDialogue
	}{{"all", all}, {"Jhon's", jhon}}
	for _, row := range rows {
		d := row.d
		responses, maxResponses := d.Responses()
		calls, maxCalls := d.ToolCalls()
		ms, maxMs := d.Ms()
		q := d.QuestionsPerCall
		lines = append(lines, fmt.Sprintf("  %s  %8d  %7d  %-9s  %-10s  %-11s  %14d  %4d  %4d  %4d  %4d  %15d", js.PadEnd(row.name, 6), d.Sessions, len(d.Answers),
			pair(responses, maxResponses, count), pair(calls, maxCalls, count), pair(ms, maxMs, audit.Duration), d.AskCalls, q[0], q[1], q[2], q[3], d.ProseQuestions))
	}
	lines = append(lines, fmt.Sprintf("  %s  %8s  %14s  %10s  %10s  %6s  %11s  %8s", js.PadEnd("", 6),
		"sessions", "owner messages", "correcting", "keep-going", "repeat", "unrequested", "decision"))
	for _, row := range rows {
		c := row.d.Corrections
		lines = append(lines, fmt.Sprintf("  %s  %8d  %14d  %10d  %10d  %6d  %11d  %8d", js.PadEnd(row.name, 6), row.d.Sessions,
			c.OwnerMessages, c.Correcting, c.KeepGoing, c.Repeat, c.Unrequested, c.Decision))
	}
	return lines
}

// integrationLines is each agent role's use of memory, CodeGraph and the
// todo list summed across the sessions, Jhon's apart
// (audit.FleetIntegrations): statistics, never a finding, and counts only.
func integrationLines(all, jhon audit.FleetIntegrations) []string {
	lines := []string{fmt.Sprintf("agent integrations by role, as statistics, over %d session(s), %d Jhon's (launches: the main thread as the orchestrator, each sub-agent by its type; "+
		"memory read, memory write: the launches that called one at least once; plain reads: the read, grep, find and ls tools and the shell commands whose first program, "+
		"after an optional `cd <dir> &&`, is cat, sed -n, head, tail, grep, rg, ls or find; share: CodeGraph's of its calls and the plain reads)", all.Sessions, jhon.Sessions),
		fmt.Sprintf("  %-6s  %-20s  %8s  %11s  %12s  %9s  %11s  %6s  %4s", "", "role", "launches", "memory read", "memory write", "codegraph", "plain reads", "share", "todo")}
	for _, row := range []struct {
		name string
		f    audit.FleetIntegrations
	}{{"all", all}, {"Jhon's", jhon}} {
		for _, u := range row.f.Roles {
			lines = append(lines, fmt.Sprintf("  %-6s  %-20s  %8d  %11d  %12d  %9d  %11d  %6s  %4d", row.name, u.Role, u.Launches, u.MemoryReadLaunches,
				u.MemoryWriteLaunches, u.CodegraphCalls, u.PlainReads, audit.Percent(u.CodegraphShare()), u.TodoCalls))
		}
		if len(row.f.Roles) == 0 {
			lines = append(lines, fmt.Sprintf("  %-6s  none", row.name))
		}
	}
	return lines
}
