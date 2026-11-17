package audit

// `jhon audit --all`: many sessions' audits aggregated into the numbers that
// decide the next change (docket AY backlog item 8): the findings by check
// and severity, Jhon's own sessions apart from the rest, and each sub-agent
// type's turns against the turn cap with the share of its tokens the agents
// past the cap spent.

import (
	"encoding/json"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
)

// Audited is one session's audit: its main transcript, whether it ran Jhon
// (a Jhon main agent, or Jhon's pi agent directory), and the result.
type Audited struct {
	Path   string
	Jhon   bool
	Result *Result
	// UIUse counts the jhon-shell UI features a pi session ran (UIUses).
	UIUse map[string]int
	// Unguarded is a Jhon Claude Code session that ran tools with none of
	// Jhon's hooks (Unguarded).
	Unguarded bool
}

// AgentTypeStats is one sub-agent type of Jhon's sessions: how many ran,
// their turns, how many passed the type's own cap (Cap, 0 for none), and the
// share of the type's tokens those spent.
type AgentTypeStats struct {
	AgentType                     string
	Agents, MedianTurns, P90Turns int
	MaxTurns, OverCap, Cap        int
	Tokens, OverCapTokenShare     float64
}

// Fleet is the aggregate.
type Fleet struct {
	Sessions, JhonSessions, Errors int
	// Findings counts findings by check, then severity.
	Findings map[string]map[string]int
	// JhonFindings counts the findings of Jhon's sessions alone.
	JhonFindings map[string]map[string]int
	// AgentTypes are the sub-agent types of Jhon's sessions, most tokens first.
	AgentTypes []AgentTypeStats
	// Caps are each capped agent type's turn cap; a type absent from them has none.
	Caps map[string]int
	// UIUse counts each jhon-shell UI feature's runs across the sessions.
	UIUse map[string]int
	// Unguarded are the paths of the Jhon sessions that ran without Jhon's hooks.
	Unguarded []string
	// Guards are each guard's refusals in Jhon's sessions since the cutoff,
	// one row per card (guard.Cards), in its order.
	Guards []GuardStats
	// LDD are the changes of Jhon's sessions started since the cutoff.
	LDD []ChangeLDD
	// Dialogue is the owner's dialogue of every session read, summed whole
	// whatever the cutoff, and JhonDialogue that of Jhon's sessions alone
	// (dialogue.go).
	Dialogue, JhonDialogue FleetDialogue
	// Integrations is each agent role's use of memory, CodeGraph and the
	// todo list in every session read, summed whole whatever the cutoff, and
	// JhonIntegrations that of Jhon's sessions alone (integrations.go).
	Integrations, JhonIntegrations FleetIntegrations
}

// GuardStats is what one guard did: its refusals, the sessions they fell in,
// and the time the agents took to get a tool call through after them (the
// median and the sum over those that did; Stuck counts those that never did).
type GuardStats struct {
	Guard                   string
	Refusals, Sessions      int
	LostMedianMs, LostSumMs float64
	Stuck                   int
}

// uiUseEntry is the session entry jhon-shell appends each time a command or
// shortcut of its UI runs (shell/lib/ui-use.ts).
const uiUseEntry = "jhon-ui-use"

// UIUses counts the jhon-shell UI features a pi session's file records
// running, by feature; none for a file that cannot be read.
func UIUses(path string) map[string]int {
	out := map[string]int{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.Contains(line, uiUseEntry) {
			continue
		}
		var e struct {
			Type, CustomType string
			Data             struct{ Feature string }
		}
		if json.Unmarshal([]byte(line), &e) == nil && e.Type == "custom" && e.CustomType == uiUseEntry && e.Data.Feature != "" {
			out[e.Data.Feature]++
		}
	}
	return out
}

// PluginOnly is the first Claude Code version that runs no agent
// frontmatter hooks: from it on, Jhon's hooks reach a session only through
// its plugin, whose SessionStart and Stop hooks the transcript records even
// when they print nothing. Before it the frontmatter ran them and a silent
// guard left no trace, so an older session cannot be judged.
const PluginOnly = "2.1.310"

// jhonHook is a hook command that runs Jhon: its binary's `hook`, or a
// guard script of the frontmatter's older render.
var jhonHook = lazyre.New(`jhon['"]?\s+['"]?hook\b|/jhon/\S*/hooks/[\w-]+\.mjs`)

// Unguarded is whether a Claude Code session of Claude Code PluginOnly or
// later, whose main file is path, called a tool and records no Jhon hook
// that ran: Claude Code dropped the hooks, as 2.1.310 did the frontmatter's,
// or the plugin was off. r is the session's audit.
func Unguarded(path string, r *Result) bool {
	if r == nil || r.Runtime != "claude-code" || r.Version == nil || !atLeast(*r.Version, PluginOnly) {
		return false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	tools := false
	for _, line := range strings.Split(string(b), "\n") {
		var e struct {
			Type, Subtype string
			Attachment    struct{ Type, Command string }
			HookInfos     []struct{ Command string }
			Message       struct {
				Content json.RawMessage
			}
		}
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		commands := []string{}
		if strings.HasPrefix(e.Attachment.Type, "hook_") {
			commands = append(commands, e.Attachment.Command)
		}
		for _, h := range e.HookInfos {
			commands = append(commands, h.Command)
		}
		if slices.ContainsFunc(commands, jhonHook.MatchString) {
			return false
		}
		var blocks []struct{ Type string }
		if e.Type == "assistant" && json.Unmarshal(e.Message.Content, &blocks) == nil {
			tools = tools || slices.ContainsFunc(blocks, func(c struct{ Type string }) bool { return c.Type == "tool_use" })
		}
	}
	return tools
}

// atLeast is whether dotted version v is at least min, part by number.
func atLeast(v, min string) bool {
	a, b := strings.Split(v, "."), strings.Split(min, ".")
	for i := range b {
		if i >= len(a) {
			return false
		}
		x, errX := strconv.Atoi(a[i])
		y, _ := strconv.Atoi(b[i])
		if errX != nil || x != y {
			return errX == nil && x > y
		}
	}
	return true
}

// Aggregate is the fleet of results, each agent type counted against its own
// cap in caps. A sub-agent that started before since is left out, so a
// session that runs on past a cutoff brings only its later agents.
func Aggregate(results []Audited, caps map[string]int, since time.Time) Fleet {
	f := Fleet{Findings: map[string]map[string]int{}, JhonFindings: map[string]map[string]int{}, Caps: caps, UIUse: map[string]int{}}
	count := func(m map[string]map[string]int, x Finding) {
		if m[x.Check] == nil {
			m[x.Check] = map[string]int{}
		}
		m[x.Check][x.Severity]++
	}
	type acc struct {
		turns        []int
		tokens, over float64
		overCap      int
	}
	byType := map[string]*acc{}
	type guardAcc struct {
		refusals, sessions, stuck int
		lost                      []float64
	}
	guards := map[string]*guardAcc{}
	for _, a := range results {
		f.Sessions++
		if a.Jhon {
			f.JhonSessions++
		}
		for feature, n := range a.UIUse {
			f.UIUse[feature] += n
		}
		if a.Unguarded {
			f.Unguarded = append(f.Unguarded, a.Path)
		}
		r := a.Result
		if r == nil || r.Status == "audit-error" {
			f.Errors++
			continue
		}
		if r.Dialogue != nil {
			f.Dialogue.add(r.Dialogue)
			if a.Jhon {
				f.JhonDialogue.add(r.Dialogue)
			}
		}
		if r.Integrations != nil {
			f.Integrations.add(r.Integrations)
			if a.Jhon {
				f.JhonIntegrations.add(r.Integrations)
			}
		}
		for _, x := range r.Findings {
			count(f.Findings, x)
			if a.Jhon {
				count(f.JhonFindings, x)
			}
		}
		if a.Jhon {
			seenIn := map[string]bool{}
			for _, g := range r.Guards {
				if t, ok := isodate.Parse(g.At); !since.IsZero() && (!ok || t.Before(since)) {
					continue
				}
				b := guards[g.Guard]
				if b == nil {
					b = &guardAcc{}
					guards[g.Guard] = b
				}
				b.refusals++
				if !seenIn[g.Guard] {
					seenIn[g.Guard] = true
					b.sessions++
				}
				if math.IsNaN(g.LostMs) {
					b.stuck++
				} else {
					b.lost = append(b.lost, g.LostMs)
				}
			}
			for _, c := range r.LDD {
				if t, ok := isodate.Parse(c.Start); since.IsZero() || ok && !t.Before(since) {
					f.LDD = append(f.LDD, c)
				}
			}
		}
		if !a.Jhon || r.ContextUse == nil {
			continue
		}
		for _, u := range r.ContextUse.Agents {
			if u.AgentType == "" || u.AgentType == "main" {
				continue
			}
			if t, ok := isodate.Parse(u.Start); !since.IsZero() && ok && t.Before(since) {
				continue
			}
			b := byType[u.AgentType]
			if b == nil {
				b = &acc{}
				byType[u.AgentType] = b
			}
			b.turns = append(b.turns, u.Turns)
			b.tokens += u.Total
			if cap := caps[u.AgentType]; cap > 0 && u.Turns > cap {
				b.overCap++
				for _, c := range u.TurnContext[min(cap, len(u.TurnContext)):] {
					b.over += c
				}
			}
		}
	}
	for typ, b := range byType {
		slices.Sort(b.turns)
		s := AgentTypeStats{AgentType: typ, Agents: len(b.turns), MedianTurns: b.turns[(len(b.turns)-1)/2],
			P90Turns: b.turns[(len(b.turns)*9-1)/10], MaxTurns: b.turns[len(b.turns)-1], OverCap: b.overCap, Cap: caps[typ], Tokens: b.tokens}
		if b.tokens > 0 {
			s.OverCapTokenShare = b.over / b.tokens
		}
		f.AgentTypes = append(f.AgentTypes, s)
	}
	for _, c := range guard.Cards {
		s := GuardStats{Guard: c.Guard}
		if b := guards[c.Guard]; b != nil {
			s.Refusals, s.Sessions, s.Stuck = b.refusals, b.sessions, b.stuck
			slices.Sort(b.lost)
			for _, ms := range b.lost {
				s.LostSumMs += ms
			}
			if len(b.lost) > 0 {
				s.LostMedianMs = b.lost[(len(b.lost)-1)/2]
			}
		}
		f.Guards = append(f.Guards, s)
	}
	f.LDD = MergeLDD(f.LDD)
	slices.SortFunc(f.AgentTypes, func(x, y AgentTypeStats) int {
		if x.Tokens != y.Tokens {
			if x.Tokens > y.Tokens {
				return -1
			}
			return 1
		}
		return strings.Compare(x.AgentType, y.AgentType)
	})
	return f
}
