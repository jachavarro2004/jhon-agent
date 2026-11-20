package execution

import (
	"encoding/json"
	"errors"
	"os"
)

// A change's light verify (T31.5, L311 (1)): when a slice of a code change
// closes, the jhon-checker agent reads the diff and the tests, runs the focused
// tests, and records its verdict as a note in the change's execution record,
// beside its RED and GREEN runs, with or without a folder. A later run keeps
// the notes, as it keeps the history.

// The verdicts of a light verify.
const (
	VerdictPass = "pass"
	VerdictFail = "fail"
)

// VerifyNote is one light verify: when it was recorded, its verdict, the
// HEAD commit and tree it judged, and what the verifier wrote.
type VerifyNote struct {
	Timestamp string `json:"timestamp"`
	Verdict   string `json:"verdict"`
	Commit    string `json:"commit"`
	Tree      string `json:"tree"`
	Note      string `json:"note,omitempty"`
}

// LightVerifyOf is the change's light verify notes, the newest
// HistoryLimit, as written; none when there is no record or it cannot be read.
func LightVerifyOf(root, change string) []VerifyNote {
	b, err := os.ReadFile(RecordFile(root, change))
	var r struct {
		LightVerify []VerifyNote `json:"light_verify"`
	}
	if err != nil || json.Unmarshal(b, &r) != nil {
		return nil
	}
	return r.LightVerify[max(0, len(r.LightVerify)-HistoryLimit):]
}

// lastLightVerify is the change's newest light verify note, or nil.
func lastLightVerify(root, change string) *VerifyNote {
	notes := LightVerifyOf(root, change)
	if len(notes) == 0 {
		return nil
	}
	return &notes[len(notes)-1]
}

// ErrNoRun is a light verify of a change with no run on record: it judges a
// tested change.
var ErrNoRun = errors.New("no run on record")

// AddLightVerify appends n to the change's record, atomically, keeping
// everything else the record holds.
func AddLightVerify(root, change string, n VerifyNote) error {
	abs := RecordFile(root, change)
	b, err := os.ReadFile(abs)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNoRun
	}
	if err != nil {
		return err
	}
	var r Record
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	r.LightVerify = append(r.LightVerify, n)
	r.LightVerify = r.LightVerify[max(0, len(r.LightVerify)-HistoryLimit):]
	return WriteRecord(abs, &r)
}

// LightVerifyDue is whether last, a RED before GREEN run, is newer than its
// last light verify, or has none.
func LightVerifyDue(last *LastRun) bool {
	return last != nil && last.RedBeforeGreen && (last.LightVerify == nil || last.LightVerify.Timestamp < last.Timestamp)
}

// LightVerified is whether change's newest light verify passed on the tree
// its last run tested (the working tree it ran on, else HEAD's), and that
// run passed: once a slice commit commits the tested tree, HEAD's tree is it.
func LightVerified(root, change string) bool {
	last, err := ReadLastRun(root, change)
	if err != nil || last == nil || !last.Passed || last.LightVerify == nil {
		return false
	}
	return last.LightVerify.Verdict == VerdictPass && last.LightVerify.Tree == testedTree(root, change, last)
}

// testedTree is the tree the record's last run tested: its working tree,
// else its candidate tree.
func testedTree(root, change string, last *LastRun) string {
	b, _ := os.ReadFile(RecordFile(root, change))
	var r struct {
		WorkingTree string `json:"working_tree"`
	}
	if json.Unmarshal(b, &r) == nil && r.WorkingTree != "" {
		return r.WorkingTree
	}
	return last.Tree
}
