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
