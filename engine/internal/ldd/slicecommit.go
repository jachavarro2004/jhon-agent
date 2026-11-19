package ldd

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// SliceLines is the lines of code a change's slice in progress adds,
// counted as the route counts them, since the last commit that closed a
// slice of the change on its branch (the merge base before the first), and
// the advisory size of a slice, the route's verified line count
// (contracts/ldd.md, "Slices"; orchestrator.md, "Proportion").
type SliceLines struct {
	// Slice is the number of the slice in progress, "1" without slices.
	Slice    string `json:"slice"`
	Lines    int    `json:"lines"`
	Advisory int    `json:"advisory"`
}

// Over is whether the slice adds more lines of code than its advisory size.
func (s SliceLines) Over() bool { return s.Lines > s.Advisory }

// Note is the advice for a slice over its advisory size: never a refusal.
func (s SliceLines) Note() string {
	return fmt.Sprintf("slice %s adds %d lines of code, over the advisory %d: say so to the owner and offer to split it into slices that each build, test, and deliver", s.Slice, s.Lines, s.Advisory)
}

// SliceLinesOf is the size of the slice in progress of change in the
// repository whose top level is top, whose route is r: the first slice
// HEAD's tasks.md has not closed, measured from the last commit that closed
// one.
func SliceLinesOf(top, change string, r Route) SliceLines {
	return gitReads(nil).sliceLines(top, change, r)
}

func (g gitReads) sliceLines(top, change string, r Route) SliceLines {
	s := SliceLines{Slice: "1", Advisory: r.VerifiedLines}
	lay := layout.For(top)
	rel := lay.Artifact(change, lay.Tasks())
	head, _ := g.git(top, "show", "HEAD:"+rel)
	if head == "" {
		if b, err := os.ReadFile(filepath.Join(top, rel)); err == nil {
			head = string(b)
		}
	}
	for _, x := range ProgressOf(lay, &head).Slices {
		if x.Total == 0 || x.Completed < x.Total {
			if m := sliceNumber.FindStringSubmatch(x.Name); m != nil {
				s.Slice = m[1]
			}
			break
		}
	}
	base, ok := g.sliceBase(top, change)
	if !ok {
		return s
	}
	_, added := g.addedOutsideRecord(top, base)
	s.Lines = codeLines(added, r.risk)
	return s
}

// sliceBase is the commit the slice in progress of change is measured from:
// the last commit on HEAD's branch that closed a slice of it in its tasks.md,
// the change's base (ChangeBase) before the first. ok is false when HEAD is
// not a commit.
func (g gitReads) sliceBase(top, change string) (string, bool) {
	base, _, ok := g.baseOf(top, nil)
	if !ok {
		return "", false
	}
	lay := layout.For(top)
	rel := lay.Artifact(change, lay.Tasks())
	log, _ := g.git(top, "log", "--format=%H", base+"..HEAD", "--", rel)
	for _, c := range nonEmpty(log) {
		after, _ := g.git(top, "show", c+":"+rel)
		before, _ := g.git(top, "show", c+"^:"+rel)
		if len(newlyClosed(lay, before, after)) > 0 {
			return c, true
		}
	}
	return base, true
}

// ClosedSliceLines are the sizes of the slices a commit in the repository
// whose top level is top closes, one per active change of its branch that
// closes one, under route r.
func ClosedSliceLines(top string, r Route) []SliceLines {
	active, _ := BranchChanges(top)
	var out []SliceLines
	for _, c := range active {
		if len(closedSlices(top, c)) > 0 {
			out = append(out, SliceLinesOf(top, c, r))
		}
	}
	return out
}

// SliceCommitRefusal is why a commit in the repository whose top level is
// top is refused, "" when it may run (contracts/ldd.md, "Slices"). With
// `delivery: change`, a slice is delivered as a commit on the change's
// branch, so a commit that closes a slice (its tasks were not all checked
// in HEAD's tasks.md and are in the working one) must commit the tree the
// change's last test run tested (TestedTree, as `jhon deliver check` judges
// it): complete, undisturbed, passing, and the wide kind, not a unit's. The
// change's own folder is left out of the comparison, since its tasks are
// checked off after the tests pass. Each scenario the slice adds or edits in the
// change's spec deltas is named by a test (unnamedSliceScenarios), as delivery
// asks of them all. A slice that changes a declared data-access path needs
// a declared real-database test that reached the changed file in that run
// (dataReach). Any other commit passes.
func SliceCommitRefusal(top string) string {
	if DeliveryUnit(top) != "change" {
		return ""
	}
	active, _ := BranchChanges(top)
	for _, change := range active {
		closed := closedSlices(top, change)
		if len(closed) == 0 {
			continue
		}
		// The scenarios first: naming them edits the tests, which a run then tests.
		if why := unnamedSliceScenarios(top, change); why != "" {
			return fmt.Sprintf("this commit closes %s of %s, and of the scenarios the slice adds or edits in the change's spec deltas, %s: a test names the scenario it proves (its test name, or a `scenario: <name>` comment), then commit.",
				strings.Join(closed, ", "), change, why)
		}
		// The tree it records is judged as deliver check judges HEAD's (TestedTree).
		if tt := TestedTree(top, change, vcs.WorkingTree(top, execution.ChangeRecordPaths(top, change))); tt.Why != "" {
			why := tt.Why
			// An affected run of nothing is what wrote an empty record: only
			// the full run clears it.
			run := "`jhon executions run " + change + " --affected <base>`"
			if tt.Full {
				run = "`jhon executions run " + change + "` (the full run)"
			}
			return fmt.Sprintf("this commit closes %s of %s, and %s. Run %s on the slice as it will be committed, then commit: each slice of a `delivery: change` is a tested tree, not only the last one.",
				strings.Join(closed, ", "), change, why, run)
		}
		// The run is the tree's: what its tests reached is this slice's.
		if why := gitReads(nil).dataReachRefusal(top, change, closed); why != "" {
			return why
		}
	}
	return ""
}

// ChangeBase is the base the change on HEAD's branch is measured from: the
// merge base of HEAD and the default branch (HEAD itself on that branch),
// and the ref that names it. A unit's test run with no passing run on record
// measures from it. ok is false when HEAD is not a commit.
func ChangeBase(top string) (commit, label string, ok bool) { return baseOf(top, nil) }

// ClosesSlice is whether a commit in the repository whose top level is top
// closes a slice of an active change of its branch, on any delivery unit.
func ClosesSlice(top string) bool {
	active, _ := BranchChanges(top)
	return slices.ContainsFunc(active, func(c string) bool { return len(closedSlices(top, c)) > 0 })
}

// closedSlices are the slices of change whose tasks are all checked in the
// working tasks.md and were not in HEAD's.
func closedSlices(top, change string) []string {
	lay := layout.For(top)
	rel := lay.Artifact(change, lay.Tasks())
	b, err := os.ReadFile(filepath.Join(top, rel))
	if err != nil {
		return nil
	}
	before, _ := riskGit(top, "show", "HEAD:"+rel)
	return newlyClosed(lay, before, string(b))
}

// newlyClosed are the slices whose tasks are all checked in the tasks.md
// after and were not in before.
func newlyClosed(lay layout.Layout, before, after string) []string {
	done := func(src string) []string {
		var out []string
		for _, x := range ProgressOf(lay, &src).Slices {
			if x.Total > 0 && x.Completed == x.Total {
				out = append(out, x.Name)
			}
		}
		return out
	}
	was := done(before)
	var out []string
	for _, x := range done(after) {
		if !slices.Contains(was, x) {
			out = append(out, x)
		}
	}
	return out
}

// DesignDueNotes are, for each active change of the branch whose commit in
// the repository whose top level is top closes a slice, the proposal and
// design route r now asks for and the change lacks: each file named, missing
// or incomplete, to write before the next slice. A note as `jhon status`
// says it, never a refusal (T101, L671).
func DesignDueNotes(top string, r Route) []string {
	if !r.Design {
		return nil
	}
	v := sv()
	active, _ := BranchChanges(top)
	var out []string
	for _, change := range active {
		if len(closedSlices(top, change)) == 0 {
			continue
		}
		var due []string
		for _, f := range []struct {
			name  string
			state func(*string) string
		}{{"proposal.md", ProposalState}, {"design.md", DesignState}} {
			rel := layout.For(top).Artifact(change, f.name)
			var src *string
			if b, err := os.ReadFile(filepath.Join(top, rel)); err == nil {
				src = new(string(b))
			}
			switch f.state(src) {
			case v.done:
			case v.missing:
				due = append(due, rel+" (missing)")
			default:
				due = append(due, rel+" (incomplete)")
			}
		}
		if len(due) > 0 {
			out = append(out, fmt.Sprintf("the route asks for a proposal and a design now (%s): write %s before the next slice; `jhon status %s` lists what each needs",
				strings.Join(r.Spans, ", "), strings.Join(due, " and "), change))
		}
	}
	return out
}
