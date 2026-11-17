package ldd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// T80b (L586, L589, L1074): after a change is archived, `jhon status` names
// the linked worktree and the branch it leaves behind and the commands that
// remove them; the branch's only when it is merged into the default branch.
// Status only reports: it removes nothing.

// archivedRepo is a repository at <dir>/repo on main holding the LDD
// config, with change shipping-cost worked in the linked worktree
// <dir>/repo-worktrees/shipping-cost on branch feat/shipping-cost, archived
// there and merged into main: the state a session leaves after delivery.
func archivedRepo(t *testing.T) (root, wt string) {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	root, wt = filepath.Join(dir, "repo"), filepath.Join(dir, "repo-worktrees", "shipping-cost")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, root, "init", "-q")
	write(t, filepath.Join(root, "openspec/config.yaml"), "delivery: change\n")
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "base")
	gitT(t, root, "worktree", "add", "-q", "-b", "feat/shipping-cost", wt)
	write(t, filepath.Join(wt, "openspec/changes/archive/2026-11-16-shipping-cost/tasks.md"), "## Slice 1: a\n- [x] 1.1 a\n")
	write(t, filepath.Join(wt, "src/shipping.py"), "COST = 1\n")
	gitT(t, wt, "add", "-A")
	gitT(t, wt, "commit", "-q", "-m", "shipping cost")
	gitT(t, root, "merge", "-q", "--ff-only", "feat/shipping-cost")
	return root, wt
}

// cleanupView is the status's cleanup notes, its text, and its JSON.
func cleanupView(t *testing.T, root string) (s Status, text, raw string) {
	t.Helper()
	s = statusOf(t, root, nil)
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return s, Human(s), string(b)
}

func cleanupNotes(s Status) []string {
	var out []string
	for _, n := range s.Notes {
		if strings.HasPrefix(n, "archived change ") {
			out = append(out, n)
		}
	}
	return out
}

func TestArchivedChangeNamesItsWorktreeAndMergedBranch(t *testing.T) {
	root, wt := archivedRepo(t)
	s, text, raw := cleanupView(t, root)
	note := "archived change shipping-cost still has its worktree " + wt + " and its branch feat/shipping-cost, merged into main: remove them with `git worktree remove " + wt + "` and `git branch -d feat/shipping-cost` (jhon removes nothing)"
	if got := cleanupNotes(s); len(got) != 1 || got[0] != note {
		t.Errorf("notes:\n got %q\nwant %q", got, note)
	}
	if !strings.Contains(text, "\n  - "+note) {
		t.Errorf("the text names it:\n%s", text)
	}
	want := `,"cleanup":[{"change":"shipping-cost","worktree":` + jsonString(t, wt) + `,"branch":"feat/shipping-cost","merged":true,"commands":["git worktree remove ` + strings.Trim(jsonString(t, wt), `"`) + `","git branch -d feat/shipping-cost"]}]}`
	if !strings.HasSuffix(raw, want) {
		t.Errorf("the JSON names it, the last key:\n got %s\nwant suffix %s", raw, want)
	}
	// Status removes nothing (K3).
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("the worktree was removed: %v", err)
	}
	if !branchExists(t, root, "feat/shipping-cost") {
		t.Error("the branch was deleted")
	}
	// From the worktree itself, the same.
	if got := cleanupNotes(statusOf(t, wt, nil)); len(got) != 1 || got[0] != note {
		t.Errorf("from the worktree:\n got %q\nwant %q", got, note)
	}
}

func TestArchivedChangeUnmergedBranchHasNoDeletion(t *testing.T) {
	root, wt := archivedRepo(t)
	write(t, filepath.Join(wt, "src/late.py"), "LATE = 1\n")
	gitT(t, wt, "add", "-A")
	gitT(t, wt, "commit", "-q", "-m", "late")
	s, _, raw := cleanupView(t, root)
	note := "archived change shipping-cost still has its worktree " + wt + " and its branch feat/shipping-cost, not merged into main: remove the worktree with `git worktree remove " + wt + "`; the branch is not merged, so no command deletes it (jhon removes nothing)"
	if got := cleanupNotes(s); len(got) != 1 || got[0] != note {
		t.Errorf("notes:\n got %q\nwant %q", got, note)
	}
	if strings.Contains(raw, "git branch -d") || !strings.Contains(raw, `"merged":false,"commands":["git worktree remove `) {
		t.Errorf("no deletion command for an unmerged branch:\n%s", raw)
	}
	// The worktree gone, the branch alone.
	gitT(t, root, "worktree", "remove", wt)
	s, _, raw = cleanupView(t, root)
	note = "archived change shipping-cost still has its branch feat/shipping-cost, not merged into main: the branch is not merged, so no command deletes it (jhon removes nothing)"
	if got := cleanupNotes(s); len(got) != 1 || got[0] != note {
		t.Errorf("branch alone:\n got %q\nwant %q", got, note)
	}
	if !strings.HasSuffix(raw, `,"cleanup":[{"change":"shipping-cost","worktree":null,"branch":"feat/shipping-cost","merged":false,"commands":[]}]}`) {
		t.Errorf("branch alone, JSON:\n%s", raw)
	}
}

func TestArchivedChangeNamesWhatIsLeftAndNothingOnceGone(t *testing.T) {
	root, wt := archivedRepo(t)
	_, cleanText, cleanJSON := func() (Status, string, string) {
		// The same repository with neither: the view a change leaves cleaned up.
		gitT(t, root, "worktree", "remove", wt)
		gitT(t, root, "branch", "-d", "feat/shipping-cost")
		return cleanupView(t, root)
	}()
	if strings.Contains(cleanText+cleanJSON, "archived change") || strings.Contains(cleanJSON, `"cleanup"`) {
		t.Errorf("both gone, nothing is named (K2):\n%s\n%s", cleanText, cleanJSON)
	}

	// A merged branch whose worktree is gone.
	root, wt = archivedRepo(t)
	gitT(t, root, "worktree", "remove", wt)
	s, _, raw := cleanupView(t, root)
	note := "archived change shipping-cost still has its branch feat/shipping-cost, merged into main: remove it with `git branch -d feat/shipping-cost` (jhon removes nothing)"
	if got := cleanupNotes(s); len(got) != 1 || got[0] != note {
		t.Errorf("branch alone:\n got %q\nwant %q", got, note)
	}
	if !strings.HasSuffix(raw, `,"cleanup":[{"change":"shipping-cost","worktree":null,"branch":"feat/shipping-cost","merged":true,"commands":["git branch -d feat/shipping-cost"]}]}`) {
		t.Errorf("branch alone, JSON:\n%s", raw)
	}

	// A detached worktree, its branch gone: the worktree alone, by its path.
	root, wt = archivedRepo(t)
	gitT(t, wt, "checkout", "-q", "--detach")
	gitT(t, root, "branch", "-d", "feat/shipping-cost")
	s, _, raw = cleanupView(t, root)
	note = "archived change shipping-cost still has its worktree " + wt + ": remove it with `git worktree remove " + wt + "` (jhon removes nothing)"
	if got := cleanupNotes(s); len(got) != 1 || got[0] != note {
		t.Errorf("worktree alone:\n got %q\nwant %q", got, note)
	}
	if !strings.Contains(raw, `"branch":null,"merged":null,"commands":["git worktree remove `) {
		t.Errorf("worktree alone, JSON:\n%s", raw)
	}
}

func TestCleanupNamesOnlyArchivedChanges(t *testing.T) {
	root, _ := archivedRepo(t)
	// A branch naming an active change, and one naming none, are not named.
	write(t, filepath.Join(root, "openspec/changes/rates/tasks.md"), "## Slice 1: a\n- [ ] 1.1 a\n")
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "plan rates")
	gitT(t, root, "branch", "feat/rates")
	gitT(t, root, "branch", "chore/tidy")
	// An archived change a branch only contains, not ends with, is not its.
	write(t, filepath.Join(root, "openspec/changes/archive/2026-11-01-old/tasks.md"), "- [x] a\n")
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "old")
	gitT(t, root, "branch", "feat/hold")
	gitT(t, root, "branch", "feat/old-rates")
	s, _, raw := cleanupView(t, root)
	got := cleanupNotes(s)
	if len(got) != 1 || !strings.HasPrefix(got[0], "archived change shipping-cost ") {
		t.Errorf("only the archived change:\n%q", got)
	}
	if strings.Contains(raw, "feat/rates") || strings.Contains(raw, "chore/tidy") || strings.Contains(raw, "hold") || strings.Contains(raw, "old-rates") {
		t.Errorf("an active change's branch or an unrelated one is named:\n%s", raw)
	}
	// A project nested in a repository has no branches of its own.
	write(t, filepath.Join(root, "sub/openspec/changes/archive/2026-11-16-shipping-cost/tasks.md"), "- [x] a\n")
	if n := cleanupNotes(statusOf(t, filepath.Join(root, "sub"), nil)); len(n) != 0 {
		t.Errorf("a nested project, no cleanup: %q", n)
	}
	// Outside a repository: nothing.
	plain, _ := filepath.EvalSymlinks(t.TempDir())
	write(t, filepath.Join(plain, "openspec/changes/archive/2026-11-16-x/tasks.md"), "- [x] a\n")
	if n := cleanupNotes(statusOf(t, plain, nil)); len(n) != 0 {
		t.Errorf("no repository, no cleanup: %q", n)
	}
}

func jsonString(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func branchExists(t *testing.T, root, branch string) bool {
	t.Helper()
	return exec.Command("git", "-C", root, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil
}
