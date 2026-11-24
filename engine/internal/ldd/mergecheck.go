package ldd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// A command that moves the default branch to another commit is a merge, and
// route-guard runs it only when a ready merge check covers that commit
// (T219). A check that is not ready writes nothing, so the newest record of
// a change is its newest ready check.

// DefaultBranches are the names the default branch of the repository holding
// root goes by: origin/HEAD's branch, main, and master.
func DefaultBranches(root string) []string { return gitReads(nil).defaultBranches(root) }

// MergeChecked is whether a ready `jhon deliver check <change> --action
// merge` on record in the repository holding root covers commit, a full
// sha: the record names commit, or a commit whose tree differs from it only
// in that change's record (outsideRecord, deliver check's identity: the
// tree, never the commit). The records read are each worktree's changes,
// active and archived, and those under the git common dir (a change with no
// folder, a workspace member's); never one a commit carries.
func MergeChecked(root, commit string) bool {
	if !isSha(commit) {
		return false
	}
	for _, r := range mergeRecords(root) {
		if r.head == commit {
			return true
		}
		if paths, ok := outsideRecord(root, r.change, r.head, commit); ok && len(paths) == 0 {
			return true
		}
	}
	return false
}

// mergeRecord is a ready merge check on record: its change, and the commit it saw.
type mergeRecord struct{ change, head string }

func mergeRecords(root string) []mergeRecord {
	var out []mergeRecord
	add := func(change string, b []byte) {
		var d Delivery
		if json.Unmarshal(b, &d) != nil || d.Action != Merge() || !isSha(d.HeadCommit) {
			return
		}
		out = append(out, mergeRecord{change, d.HeadCommit})
	}
	// glob reads every <changes>/<change>/delivery.json under dir, active and archived.
	glob := func(dir string) {
		for _, pattern := range []string{"*", filepath.Join("archive", "*")} {
			files, _ := filepath.Glob(filepath.Join(dir, pattern, DeliveryFile))
			for _, f := range files {
				change, err := filepath.Rel(dir, filepath.Dir(f))
				if b, rerr := os.ReadFile(f); err == nil && rerr == nil {
					add(filepath.ToSlash(change), b)
				}
			}
		}
	}
	changes := execution.ChangesOf(root)
	// The project's folder in each worktree, as root is in its own.
	prefix := ""
	if top := vcs.GitTopLevel(root); top != "" {
		if rel, err := filepath.Rel(top, root); err == nil && rel != "." {
			prefix = rel
		}
	}
	trees := []string{root}
	if list, ok := gitOK(root, "worktree", "list", "--porcelain"); ok {
		for _, l := range strings.Split(list, "\n") {
			if wt, ok := strings.CutPrefix(l, "worktree "); ok {
				trees = append(trees, filepath.Join(wt, prefix))
			}
		}
	}
	seen := map[string]bool{}
	for _, t := range trees {
		if !seen[t] {
			seen[t] = true
			glob(filepath.Join(t, changes))
		}
	}
	if common, ok := gitOK(root, "rev-parse", "--path-format=absolute", "--git-common-dir"); ok {
		glob(filepath.Join(common, "jhon", "changes"))
	}
	// A record a commit carries is not read: a commit can carry a forged one
	// (T219 L5); a record committed after its check is still on disk.
	return out
}
