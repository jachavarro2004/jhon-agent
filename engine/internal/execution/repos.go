package execution

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// A workspace's member runs its tests for the primary's change (T57 slice
// 6, L1315): `jhon executions run <change> --repo <name>` records them in
// the primary's record of the change, under `repos.<name>`, beside the
// primary's own run, which keeps every entry as it keeps the history. A
// record with no member's run has no `repos` key, as before.

// RepoRun is one member's last run of the change: the working tree it
// tested and that tree read again at its end (equal, or the run is
// disturbed), its executions, and whether it ended and every execution
// passed.
type RepoRun struct {
	WorkingTree      string      `json:"working_tree"`
	AfterWorkingTree string      `json:"after_working_tree"`
	Executions       []Execution `json:"executions"`
	Passed           bool        `json:"passed"`
	Complete         bool        `json:"complete"`
	// Timestamp is when the run ended; an entry an earlier binary wrote
	// has none.
	Timestamp string `json:"timestamp,omitempty"`
	// History is the outcome of each earlier complete run of the member
	// (RepoHistoryOf, T223), the newest HistoryLimit: its RED before GREEN
	// counts as the primary's does (RedBeforeGreenOnRecord).
	History []PastRun `json:"history,omitempty"`
}

// RedBeforeGreen is whether the member's run passed after an earlier run
// of it failed.
func (r RepoRun) RedBeforeGreen() bool { return redBeforeGreen(r.Complete && r.Passed, r.History) }

// RepoHistoryOf is the history member name's next run of change keeps: its
// entry's history and, when that entry's run is complete, its outcome.
func RepoHistoryOf(root, change, name string) []PastRun {
	r, ok := RepoRunOf(root, change, name)
	if !ok {
		return nil
	}
	out := r.History
	if r.Complete {
		out = append(out, PastRun{Timestamp: r.Timestamp, Passed: r.Passed, WorkingTree: r.WorkingTree})
	}
	return out[max(0, len(out)-HistoryLimit):]
}

// MemberRedBeforeGreen is the newest member run of change, in root's
// record, that passed after an earlier failing run of the same member, and
// the member's name; nil when none did.
func MemberRedBeforeGreen(root, change string) (*RepoRun, string) {
	var best *RepoRun
	name := ""
	for n, r := range ReposOf(root, change) {
		if !r.RedBeforeGreen() {
			continue
		}
		if best == nil || r.Timestamp > best.Timestamp || r.Timestamp == best.Timestamp && n < name {
			r := r
			best, name = &r, n
		}
	}
	return best, name
}

// RedBeforeGreenOnRecord is whether change has a RED before GREEN on
// record: its own (RedBeforeGreen), or a workspace member's in its record
// (repos.<name>, T223), which counts the same way for archive and status.
func RedBeforeGreenOnRecord(root, change string) bool {
	if RedBeforeGreen(root, change) {
		return true
	}
	r, _ := MemberRedBeforeGreen(root, change)
	return r != nil
}

// Dirty is whether the tree the run tested changed while it ran.
func (r RepoRun) Dirty() bool { return r.Complete && r.AfterWorkingTree != r.WorkingTree }

// ReposOf is the members' runs root's record of change holds, as written;
// nil when it holds none or cannot be read.
func ReposOf(root, change string) map[string]RepoRun {
	b, err := os.ReadFile(RecordFile(root, change))
	var r struct {
		Repos map[string]RepoRun `json:"repos"`
	}
	if err != nil || json.Unmarshal(b, &r) != nil {
		return nil
	}
	return r.Repos
}

// RepoRunOf is member name's run in root's record of change, and whether
// there is one.
func RepoRunOf(root, change, name string) (RepoRun, bool) {
	r, ok := ReposOf(root, change)[name]
	return r, ok
}

// WriteRepoRun sets member name's run in root's record of change,
// atomically, keeping everything else the record holds. With no record yet
// it starts one that holds no run of the primary's own (`executions`
// null, not complete), which status reads as no run.
func WriteRepoRun(root, change, name string, run RepoRun) error {
	abs := RecordFile(root, change)
	r := Record{Schema: Schema()}
	b, err := os.ReadFile(abs)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(abs), 0o777); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		if err := json.Unmarshal(b, &r); err != nil {
			return err
		}
	}
	if r.Repos == nil {
		r.Repos = map[string]RepoRun{}
	}
	r.Repos[name] = run
	return WriteRecord(abs, &r)
}
