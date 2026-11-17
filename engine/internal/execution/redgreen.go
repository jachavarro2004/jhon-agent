package execution

import (
	"encoding/json"
	"os"
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
// history and, when that record's run is complete, its outcome, the newest
// HistoryLimit. Nil when there is no record or it cannot be read.
func HistoryOf(root, change string) []PastRun {
	last, err := ReadLastRun(root, change)
	if err != nil || last == nil {
		return nil
	}
	out := pastRuns(root, change)
	if last.Complete {
		run := PastRun{Timestamp: last.Timestamp, Passed: last.Passed}
		var r Record
		if b, err := os.ReadFile(RecordFile(root, change)); err == nil && json.Unmarshal(b, &r) == nil {
			run.WorkingTree, run.Tests = r.WorkingTree, testRunsOf(r.Executions)
		}
		out = append(out, run)
	}
	return out[max(0, len(out)-HistoryLimit):]
}

// pastRuns is the record's history as written; none when it has none.
func pastRuns(root, change string) []PastRun {
	b, err := os.ReadFile(RecordFile(root, change))
	var r struct {
		History []PastRun `json:"history"`
	}
	if err != nil || json.Unmarshal(b, &r) != nil {
		return nil
	}
	return r.History
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
