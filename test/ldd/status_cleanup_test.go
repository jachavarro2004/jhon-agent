package ldd

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T80b (L1074): after `jhon archive`, `jhon status` names the change's
// linked worktree and branch and the commands that remove them, the
// branch's only once it is merged into the default branch; once both are
// gone it names nothing, and it never removes them itself.
func TestStatusNamesTheArchivedChangesCleanup(t *testing.T) {
	t.Parallel()
	dir := temp(t)
	root, wt := filepath.Join(dir, "repo"), filepath.Join(dir, "repo-worktrees", "shipping-cost")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	support.GitRepo(t, root)
	support.Write(t, root, map[string]string{"openspec/config.yaml": "delivery: change\n"})
	commit(t, root, "base")
	support.Git(t, root, "worktree", "add", "-q", "-b", "feat/shipping-cost", wt)
	support.Write(t, wt, map[string]string{
		"openspec/changes/shipping-cost/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n",
		"src/shipping.py":                         "COST = 1\n",
	})
	recordRedGreen(t, filepath.Join(wt, "openspec/changes/shipping-cost"), true)
	commit(t, wt, "shipping cost")
	a := jhonGit(t, wt, "archive", "shipping-cost", "2026-11-16")
	equal(t, a.Code, 0, a.Out())
	commit(t, wt, "archive shipping-cost")

	statusNotes := func(dir string) (text string, s map[string]any) {
		t.Helper()
		h := jhonGit(t, dir, "status")
		j := jhonGit(t, dir, "status", "--json")
		equal(t, j.Code, 0, j.Stderr)
		return h.Stdout, object(t, j.Stdout)
	}

	// Archived on its branch, not merged yet (seen from the worktree, where
	// the archive is): the worktree's command only.
	unmerged := "archived change shipping-cost still has its worktree " + wt + " and its branch feat/shipping-cost, not merged into main: remove the worktree with `git worktree remove " + wt + "`; the branch is not merged, so no command deletes it (jhon removes nothing)"
	text, s := statusNotes(wt)
	match(t, text, `(?m)^  - `+regexp.QuoteMeta(unmerged)+`$`)
	equal(t, anyMatch(s["notes"], "^"+regexp.QuoteMeta(unmerged)+"$"), true, s["notes"])
	equal(t, s["cleanup"], []any{map[string]any{
		"change": "shipping-cost", "worktree": wt, "branch": "feat/shipping-cost", "merged": false,
		"commands": []any{"git worktree remove " + wt},
	}})

	// Delivered: merged into main, both commands.
	support.Git(t, root, "merge", "-q", "--ff-only", "feat/shipping-cost")
	merged := "archived change shipping-cost still has its worktree " + wt + " and its branch feat/shipping-cost, merged into main: remove them with `git worktree remove " + wt + "` and `git branch -d feat/shipping-cost` (jhon removes nothing)"
	text, s = statusNotes(root)
	match(t, text, `(?m)^  - `+regexp.QuoteMeta(merged)+`$`)
	equal(t, s["cleanup"], []any{map[string]any{
		"change": "shipping-cost", "worktree": wt, "branch": "feat/shipping-cost", "merged": true,
		"commands": []any{"git worktree remove " + wt, "git branch -d feat/shipping-cost"},
	}})
	// Status removed nothing (K3).
	equal(t, exists(wt), true, "the worktree is still there")
	support.Git(t, root, "rev-parse", "--verify", "--quiet", "refs/heads/feat/shipping-cost")

	// Both gone: nothing named (K2).
	support.Git(t, root, "worktree", "remove", wt)
	support.Git(t, root, "branch", "-d", "feat/shipping-cost")
	text, s = statusNotes(root)
	noMatch(t, text, "archived change")
	_, present := s["cleanup"]
	equal(t, present, false, s)
	equal(t, anyMatch(s["notes"], "^archived change"), false, s["notes"])
}
