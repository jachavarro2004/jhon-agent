package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// stacked adds <run>/repo-worktrees/<name> on branch <name> from base with
// file committed.
func (w *treeRun) stacked(name, base, file string) string {
	path := filepath.Join(w.r.Dir, "repo-worktrees", name)
	w.git(w.r.Repo, "worktree", "add", "-q", "-b", name, path, base)
	w.write(path, file, name+"\n")
	w.git(path, "add", "-A")
	w.git(path, "commit", "-q", "-m", name)
	return path
}

// A change stacked on another's branch: the tip holds both and is checked
// (T86b, L625).
func TestTreeStackedWorktreesTipChecked(t *testing.T) {
	w := newTreeRun(t)
	w.worktree("feat/a")
	tip := w.stacked("feat/b", "feat/a", "b.py")
	m, out := w.facts()
	if !m.Success || m.CheckedTree != "worktree:feat/b" || !strings.HasPrefix(out, "checked tree: worktree:feat/b ("+tip+")") {
		t.Errorf("success=%t checked_tree=%q out=%q problems=%v", m.Success, m.CheckedTree, out, m.Problems)
	}
}

// A stack with a parallel branch merged into its tip: the tip is checked.
func TestTreeStackWithMergedBranchTipChecked(t *testing.T) {
	w := newTreeRun(t)
	w.worktree("feat/a")
	w.stacked("feat/c", w.start, "c.py")
	tip := w.stacked("feat/b", "feat/a", "b.py")
	w.git(tip, "merge", "-q", "--no-edit", "feat/c")
	m, out := w.facts()
	if !m.Success || m.CheckedTree != "worktree:feat/b" {
		t.Errorf("success=%t checked_tree=%q out=%q problems=%v", m.Success, m.CheckedTree, out, m.Problems)
	}
}

// Two parallel unmerged branches on a common base: still no tip.
func TestTreeParallelStackedBranchesFail(t *testing.T) {
	w := newTreeRun(t)
	w.worktree("feat/a")
	w.stacked("feat/b", "feat/a", "b.py")
	w.stacked("feat/c", "feat/a", "c.py")
	m, out := w.facts()
	if m.Success || m.CheckedTree != "none" || !strings.Contains(out, "several worktrees hold work, none is the delivered tree") {
		t.Errorf("success=%t checked_tree=%q out=%q", m.Success, m.CheckedTree, out)
	}
}

// Per-message checks read the tree delivered after each message: the tip
// worktree, not the template left in the main checkout.
func TestMessageChecksReadDeliveredTree(t *testing.T) {
	w := newTreeRun(t)
	w.r.Task.MessageChecks = []string{"grep -q fixed pager.py && test -f hidden_check", "test -f b.py"}
	var checks []MessageCheck
	if err := messageChecks(w.r, w.env, w.start, 1, &checks); err != nil {
		t.Fatal(err)
	}
	w.worktree("feat/a")
	if err := messageChecks(w.r, w.env, w.start, 1, &checks); err != nil {
		t.Fatal(err)
	}
	w.stacked("feat/b", "feat/a", "b.py")
	if err := messageChecks(w.r, w.env, w.start, 2, &checks); err != nil {
		t.Fatal(err)
	}
	want := []MessageCheck{{1, 1, false}, {1, 1, true}, {2, 1, true}, {2, 2, true}}
	if len(checks) != len(want) {
		t.Fatalf("checks %+v", checks)
	}
	for i := range want {
		if checks[i] != want[i] {
			t.Errorf("checks %+v, want %+v", checks, want)
			break
		}
	}
	if n := slicesHeld(checks, 2); n == nil || *n != 1 {
		t.Errorf("slices %v", *n)
	}
}
