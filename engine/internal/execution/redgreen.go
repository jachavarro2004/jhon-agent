package execution

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
)

// A change's RED before GREEN (T31): the record keeps, beside its last run,
// the outcome of each earlier complete run, so a failing run that preceded
// a passing one stays known after the record is rewritten. A record written
// before the history existed has none: no RED is known from it.

// HistoryLimit is how many earlier runs a record keeps, the newest.
const HistoryLimit = 20

// PastRun is an earlier complete run of a change: when it ended, and whether
// every execution of it passed; the working tree it tested, and each
// project's test command it ran with the tests that named as failed
// (testRunsOf), which a later run's weakened tests are read from (T137).
type PastRun struct {
	Timestamp   string    `json:"timestamp"`
	Passed      bool      `json:"passed"`
	WorkingTree string    `json:"working_tree,omitempty"`
	Tests       []TestRun `json:"tests,omitempty"`
}

// HistoryOf is the history a new run of change keeps: the current record's
// history and, when that record's run is complete, its outcome, after the
// runs recorded before the change had its folder (runsBeforeFolder), the
// newest HistoryLimit. Nil when neither record is there or can be read.
func HistoryOf(root, change string) []PastRun {
	out := withRunsBefore(runsBeforeFolder(root, change), runsAt(RecordFile(root, change)))
	return out[max(0, len(out)-HistoryLimit):]
}

// runsAt is the record at abs's history and, when its run is complete, its
// outcome. Nil when there is no record or it cannot be read.
func runsAt(abs string) []PastRun {
	last, err := readRunAt(abs)
	if err != nil || last == nil {
		return nil
	}
	out := historyAt(abs)
	if last.Complete {
		run := PastRun{Timestamp: last.Timestamp, Passed: last.Passed}
		var r Record
		if b, err := os.ReadFile(abs); err == nil && json.Unmarshal(b, &r) == nil {
			run.WorkingTree, run.Tests = r.WorkingTree, testRunsOf(r.Executions)
		}
		out = append(out, run)
	}
	return out
}

// pastRuns is the record's history as written, after the runs recorded
// before the change had its folder (runsBeforeFolder); none when it has none.
func pastRuns(root, change string) []PastRun {
	return withRunsBefore(runsBeforeFolder(root, change), historyAt(RecordFile(root, change)))
}

// historyAt is the record at abs's history as written; none when it has none.
func historyAt(abs string) []PastRun {
	b, err := os.ReadFile(abs)
	var r struct {
		History []PastRun `json:"history"`
	}
	if err != nil || json.Unmarshal(b, &r) != nil {
		return nil
	}
	return r.History
}

// runsBeforeFolder is the runs of change recorded under the git common dir,
// where its record is kept while it has no folder (ChangeFile), once it has
// one (T208, L1643): a change that gets its folder mid-change keeps its RED.
// That record is only read, so the runs stay there if the folder goes. The
// git common dir is every branch's and worktree's, so they are read only
// while the checked-out branch names the change (ChangeOfBranch, L1649): a
// RED its name recorded on another branch is not this change's. None when
// the change has no folder, no such record, or the branch does not name it.
func runsBeforeFolder(root, change string) []PastRun {
	common := commonChangeFile(root, change, recordFile())
	if common == "" || common == RecordFile(root, change) {
		return nil
	}
	if _, err := os.Stat(common); err != nil || !branchNames(root, change) {
		return nil
	}
	return runsAt(common)
}

// withRunsBefore is history after the runs of before it does not hold yet,
// each run once: a record written since the folder holds them already.
// Ordered by when each run ended, so a run before the folder reads as earlier.
func withRunsBefore(before, history []PastRun) []PastRun {
	if len(before) == 0 {
		return history
	}
	type key struct {
		at, tree string
		passed   bool
	}
	held := map[key]bool{}
	for _, p := range history {
		held[key{p.Timestamp, p.WorkingTree, p.Passed}] = true
	}
	var out []PastRun
	for _, p := range before {
		if k := (key{p.Timestamp, p.WorkingTree, p.Passed}); !held[k] {
			held[k] = true
			out = append(out, p)
		}
	}
	out = append(out, history...)
	slices.SortStableFunc(out, func(a, b PastRun) int { return strings.Compare(a.Timestamp, b.Timestamp) })
	return out
}

// redBeforeGreen is whether the last run passed and an earlier one failed.
func redBeforeGreen(passed bool, history []PastRun) bool {
	if !passed {
		return false
	}
	for _, p := range history {
		if !p.Passed {
			return true
		}
	}
	return false
}

// RedBeforeGreen is whether change's last recorded run passed after an
// earlier run of it failed: its RED before its GREEN. False when there is no
// record, the last run did not pass, or no earlier failing run is known.
func RedBeforeGreen(root, change string) bool {
	last, err := ReadLastRun(root, change)
	return err == nil && last != nil && last.RedBeforeGreen
}
