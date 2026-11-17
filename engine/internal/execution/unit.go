package execution

import (
	"encoding/json"
	"os"
)

// A unit's run (T133): `--affected` with no base runs what changed since
// the change's last passing run, so each unit of work re-runs the tests its
// own edits reach and not every test the change has touched since its base.
// It is the narrow kind: a slice's close and a delivery need the wide run
// (`--affected <base>`, or a full one), and a record's `unit` says which its
// run was. A record written before the field existed reads as wide.

// PassedRun is a change's last complete, undisturbed run in which every
// execution passed: when it ended, and the working tree it tested
// (vcs.WorkingTree), which a unit's run measures from.
type PassedRun struct {
	Timestamp   string `json:"timestamp"`
	WorkingTree string `json:"working_tree"`
}

// LastPassOf is the last passing run a new run of change keeps: the current
// record's run when it passed complete and undisturbed on a known working
// tree, read again the same at its end, else the one that record kept. A
// complete record with no after_working_tree, an earlier binary's (T146f),
// shows neither its run nor the one it kept tested one tree: nil. Nil too
// when there is none, no record, or one that cannot be read.
func LastPassOf(root, change string) *PassedRun {
	last, err := ReadLastRun(root, change)
	if err != nil || last == nil {
		return nil
	}
	b, err := os.ReadFile(RecordFile(root, change))
	var r struct {
		WorkingTree      string     `json:"working_tree"`
		AfterWorkingTree string     `json:"after_working_tree"`
		LastPass         *PassedRun `json:"last_pass"`
	}
	if err != nil || json.Unmarshal(b, &r) != nil {
		return nil
	}
	if last.Passed && !last.Dirty && r.WorkingTree != "" && r.AfterWorkingTree == r.WorkingTree {
		return &PassedRun{last.Timestamp, r.WorkingTree}
	}
	if last.Complete && r.AfterWorkingTree == "" {
		return nil
	}
	return r.LastPass
}
