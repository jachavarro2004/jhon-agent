package audit

// What LDD measures (contracts/ldd.md): each guard's refusals and what they
// cost, so a guard that only gets in the way comes up to be removed
// (principle 3); and each change's slices, the owner's messages, and the time
// to the default branch, so continuous value with no ceremonies is a number
// (principles 1 and 2).

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	lddlayout "github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
)

// GuardRefusal is one refusal a guard gave: when, and how long the agent took
// to get a tool call through afterwards (NaN when it never did).
type GuardRefusal struct {
	Guard  string
	At     string
	LostMs float64
}

// guardName is the guard a refusal names: `safety-guard: unreadable: ...`,
// after the runtime's own `PreToolUse:Bash hook error: [...]:` prefix.
var guardName = lazyre.New(`(?:^|[\s:\]"'])([a-z]+-guard): `)

// guardRefusals are the guard refusals of every transcript of s.
func guardRefusals(s *Session) []GuardRefusal {
	var out []GuardRefusal
	for _, t := range s.transcripts() {
		for i, e := range t.Entries {
			for _, r := range e.ToolResults {
				if !r.IsError {
					continue
				}
				m := guardName.FindStringSubmatch(r.Text)
				if m == nil {
					continue
				}
				g := GuardRefusal{Guard: m[1], At: e.Timestamp, LostMs: math.NaN()}
				for _, next := range t.Entries[i+1:] {
					if slices.ContainsFunc(next.ToolResults, func(x ToolResult) bool { return !x.IsError }) {
						g.LostMs = dateParse(next.Timestamp) - dateParse(e.Timestamp)
						break
					}
				}
				out = append(out, g)
			}
		}
	}
	return out
}

// ChangeLDD is one change as LDD measures it: its slices (tasks.md, active
// or archived) and the tasks of its largest (MaxTasks), so an inflated slice
// shows, the owner's messages while it ran, its active time, and its
// merges into the default branch, the last at LastMerge ("" for none).
// Its archive (`archive/<date>-<change>`) and the sessions it spans count as
// the change (MergeLDD).
//
// Commits are the change's successful `git commit` calls.
//
// ByHand are the test runs started by hand, outside `jhon executions`: the
// main session's suite-wide ones (guard.TestsByHand, a directory's
// included), and a sub-agent's of a whole project (guard.WholeSuiteByHand,
// the run route-guard notes, T133). A focused TDD run is not counted, nor a
// worker's package or directory while it iterates.
type ChangeLDD struct {
	Change string
	Start  string
	// Slices counts the `## Slice` headings of the change's tasks.md on
	// disk now (slicesOf), not the slices closed in the session.
	Slices        int
	MaxTasks      int
	OwnerMessages int
	ActiveMs      float64
	Merges        int
	LastMerge     string
	Commits       int
	ByHand        int
}

// PerSlice is the owner's messages per slice of the change, its slices
// being the `## Slice` headings of its tasks.md on disk; NaN for none.
func (c ChangeLDD) PerSlice() float64 {
	if c.Slices == 0 {
		return math.NaN()
	}
	return float64(c.OwnerMessages) / float64(c.Slices)
}

// ToMainMs is the time from the change's start to its last merge into the
// default branch; NaN when it merged none.
func (c ChangeLDD) ToMainMs() float64 {
	if c.LastMerge == "" {
		return math.NaN()
	}
	return dateParse(c.LastMerge) - dateParse(c.Start)
}

var archived = lazyre.New(`^archive/\d{4}-\d{2}-\d{2}-`)

// MergeLDD folds rows of one change (its archive row, or the sessions it
// spans) into one per change, in order of start.
func MergeLDD(rows []ChangeLDD) []ChangeLDD {
	var out []ChangeLDD
	at := map[string]int{}
	for _, r := range rows {
		r.Change = archived.ReplaceAllString(r.Change, "")
		i, ok := at[r.Change]
		if !ok {
			at[r.Change] = len(out)
			out = append(out, r)
			continue
		}
		c := &out[i]
		if r.Start < c.Start {
			c.Start = r.Start
		}
		if r.LastMerge > c.LastMerge {
			c.LastMerge = r.LastMerge
		}
		c.Slices = max(c.Slices, r.Slices)
		c.MaxTasks = max(c.MaxTasks, r.MaxTasks)
		c.OwnerMessages += r.OwnerMessages
		c.ActiveMs += r.ActiveMs
		c.Merges += r.Merges
		c.Commits += r.Commits
		c.ByHand += r.ByHand
	}
	slices.SortStableFunc(out, func(x, y ChangeLDD) int { return strings.Compare(x.Start, y.Start) })
	return out
}

// changeLDD is the LDD measure of each change o attributes in s.
func changeLDD(s *Session, o *Outcomes) []ChangeLDD {
	if o == nil {
		return nil
	}
	var out []ChangeLDD
	for _, c := range o.Changes {
		if c.Change == "" || c.Start == "" {
			continue
		}
		start, end := dateParse(c.Start), dateParse(c.End)
		x := ChangeLDD{Change: c.Change, Start: c.Start, ActiveMs: c.ActiveMs}
		x.Slices, x.MaxTasks = slicesOf(s, archived.ReplaceAllString(c.Change, ""))
		results := map[string]ToolResult{}
		for _, e := range s.Main.Entries {
			for _, r := range e.ToolResults {
				results[r.ID] = r
			}
		}
		for _, e := range s.Main.Entries {
			at := dateParse(e.Timestamp)
			if at < start || at > end {
				continue
			}
			if IsGenuineUserMessage(e) {
				x.OwnerMessages++
			}
			for _, u := range e.ToolUses {
				command, ok := strProp(u.Input, "command")
				if r, done := results[u.ID]; u.Name == "Bash" && ok && MergesToDefault(command) && done && !r.IsError {
					x.Merges++
					x.LastMerge = e.Timestamp
				}
				if u.Name == "Bash" && ok && testsByHand(command) {
					x.ByHand++
				}
			}
		}
		x.ByHand += suitesByHandInAgents(s, archived.ReplaceAllString(c.Change, ""), start, end)
		x.Commits = commitsOf(s, start, end)
		out = append(out, x)
	}
	return out
}

// testsByHand is guard.TestsByHand: a suite run by hand, a directory's
// included, counted here; route-guard refuses only the whole project's
// (guard.WholeSuiteByHand).
func testsByHand(command string) bool { return guard.TestsByHand(command) }

// suitesByHandInAgents is how many times the sub-agents of s ran a whole
// project's tests by hand (guard.WholeSuiteByHand) between start and end: the
// ones working on change, or on no change they name. Another change's
// sub-agent running at the same time is that change's.
func suitesByHandInAgents(s *Session, change string, start, end float64) int {
	n := 0
	for _, a := range s.Subagents {
		if own := archived.ReplaceAllString(a.change, ""); own != "" && own != change {
			continue
		}
		for _, e := range a.Entries {
			at := dateParse(e.Timestamp)
			if at < start || at > end {
				continue
			}
			for _, u := range e.ToolUses {
				if command, ok := strProp(u.Input, "command"); ok && u.Name == "Bash" && guard.WholeSuiteByHand(command, or(a.root, or(e.Cwd, "/"))) {
					n++
				}
			}
		}
	}
	return n
}

// committed is whether command is a `git commit`.
func committed(command string) bool {
	return slices.ContainsFunc(raw(command), func(x Invocation) bool { return x.Program == "git" && x.Sub == "commit" })
}

// commitsOf is how many successful commits every transcript of s made
// between start and end.
func commitsOf(s *Session, start, end float64) int {
	commits := 0
	for _, t := range s.transcripts() {
		failed := failedIDs(t.Entries)
		for _, e := range t.Entries {
			at := dateParse(e.Timestamp)
			if at < start || at > end {
				continue
			}
			for _, u := range e.ToolUses {
				if failed[u.ID] {
					continue
				}
				if command, ok := strProp(u.Input, "command"); ok && u.Name == "Bash" && committed(command) {
					commits++
				}
			}
		}
	}
	return commits
}

// slicesOf is how many `## Slice <n>:` sections the change's tasks.md
// holds, active or archived, in any root the session worked in (a worktree
// of its repository included), and the tasks of the largest; 0 for none.
func slicesOf(s *Session, change string) (n, maxTasks int) {
	for _, root := range changeRoots(s, change) {
		trackChange(root, change)
		l := lddlayout.For(root)
		paths := []string{lddlayout.At(root, l.Artifact(change, l.Tasks()))}
		archived, _ := filepath.Glob(filepath.Join(lddlayout.At(root, l.ArchiveDir()), "*-"+change, l.Tasks()))
		for _, p := range append(paths, archived...) {
			if b, err := os.ReadFile(p); err == nil {
				src := string(b)
				sl := ldd.ProgressOf(l, &src).Slices
				for _, x := range sl {
					maxTasks = max(maxTasks, x.Total)
				}
				return len(sl), maxTasks
			}
		}
	}
	return 0, 0
}
