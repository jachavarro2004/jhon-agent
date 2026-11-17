package ldd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Umbrella changes and the ranking of candidates (T37, L365-L368): a change
// that only explores, or one its .openspec.yaml marks `kind: umbrella`, is
// a starting point for other changes, never the one in progress by default.

func umbrellaProject(t *testing.T) string {
	t.Helper()
	jhonRoot(t)
	root := tree(t, map[string]string{"openspec/changes/umbrella/exploration.md": "# Inventory\n\nSee `src/auth/kms.ts`.\n"})
	return root
}

func TestAnExplorationOnlyChangeIsAnUmbrellaNeverSelectedByDefault(t *testing.T) {
	root := umbrellaProject(t)
	st := statusOf(t, root, nil)
	if !st.Ok || st.Change != nil || st.Next != "start-change" {
		t.Errorf("an umbrella alone selects nothing: %+v", st)
	}
	if !slices.Equal(st.Umbrellas, []string{"umbrella"}) || !slices.Equal(st.ActiveChanges, []string{"umbrella"}) {
		t.Errorf("listed apart, activeChanges keeps its meaning: umbrellas %q active %q", st.Umbrellas, st.ActiveChanges)
	}
	want := "no change in progress; umbrella: umbrella (start-change: split it into changes, or write its proposal or tasks.md if it is one change)"
	if !slices.Contains(st.Notes, want) || !strings.Contains(Human(st), want) {
		t.Errorf("the human output says so in plain words:\n%s", Human(st))
	}
	named := statusOf(t, root, new("umbrella"))
	if !named.Ok || named.Change == nil || *named.Change != "umbrella" || named.Next != "start-change" {
		t.Errorf("named, it is shown, its next start-change, never archive: %+v", named)
	}
	if !slices.Equal(named.Umbrellas, []string{"umbrella"}) || !strings.Contains(notes(named), "umbrella is an umbrella (start-change): split it into changes, or write its proposal or tasks.md if it is one change") {
		t.Errorf("named umbrella: %q", named.Notes)
	}
	if _, ok := named.object().get("umbrellas"); !ok {
		t.Error("a selected change's JSON lists the umbrellas too")
	}
}

func TestAnUmbrellaIsDetectedByItsMarkerOrByHoldingOnlyAnExploration(t *testing.T) {
	jhonRoot(t)
	root := tree(t, map[string]string{
		"openspec/changes/marked/.openspec.yaml":     "schema: spec-driven\nkind: umbrella\n",
		"openspec/changes/marked/proposal.md":        "## Why\nx\n## What Changes\ny\n",
		"openspec/changes/plain/.openspec.yaml":      "schema: spec-driven\n",
		"openspec/changes/plain/exploration.md":      "x\n",
		"openspec/changes/plain/research.md":         "x\n",
		"openspec/changes/explored/exploration.md":   "x\n",
		"openspec/changes/empty/.gitkeep":            "",
		"openspec/changes/ran/exploration.md":        "x\n",
		"openspec/changes/ran/execution-record.json": "{}\n",
	})
	for c, want := range map[string]bool{"marked": true, "explored": true, "plain": false, "empty": false, "ran": false} {
		if got := IsUmbrella(root, c); got != want {
			t.Errorf("IsUmbrella(%s) = %t, want %t", c, got, want)
		}
	}
	st := statusOf(t, root, nil)
	if !slices.Equal(st.Umbrellas, []string{"explored", "marked"}) || st.Change != nil {
		t.Errorf("umbrellas: %+v", st)
	}
}

func TestTheOneRealChangeBesideAnUmbrellaIsSelected(t *testing.T) {
	root := umbrellaProject(t)
	copyChange(t, root, "tasks-partial", "change-a")
	st := statusOf(t, root, nil)
	if !st.Ok || st.Change == nil || *st.Change != "change-a" || !slices.Equal(st.Umbrellas, []string{"umbrella"}) {
		t.Errorf("the only real change is the one in progress: %+v", st)
	}
}

func TestOnlyTheBranchOrANameSelectsAnUmbrella(t *testing.T) {
	root := umbrellaProject(t)
	copyChange(t, root, "tasks-partial", "change-a")
	copyChange(t, root, "tasks-partial", "change-b")
	gitT(t, root, "init", "-q")
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "init")
	write(t, filepath.Join(root, "openspec/changes/umbrella/exploration.md"), "# Inventory, edited\n")
	if st := statusOf(t, root, nil); st.Change != nil {
		t.Errorf("an umbrella being edited is not the one in progress: %+v", st)
	}
	write(t, filepath.Join(root, "openspec/changes/change-b/tasks.md"), "## Slice 1: b\n- [ ] 1.1 b\n")
	if st := statusOf(t, root, nil); st.Change == nil || *st.Change != "change-b" {
		t.Errorf("the one real change edited beside an edited umbrella: %+v", st)
	}
	gitT(t, root, "checkout", "-q", "-b", "feat/umbrella")
	if st := statusOf(t, root, nil); st.Change == nil || *st.Change != "umbrella" || st.Next != "start-change" {
		t.Errorf("the branch names the umbrella: %+v", st)
	}
}

// A change whose only remaining slices are outlined (T127, L845) still has
// work to do: it ranks with the pending changes, ahead of a finished one
// that moved later.
func TestAnOutlinedSliceLeftIsPendingWork(t *testing.T) {
	root := umbrellaProject(t)
	write(t, filepath.Join(root, "openspec/changes/change-done/tasks.md"), "## Slice 1: a\n- [x] 1.1 a\n")
	write(t, filepath.Join(root, "openspec/changes/change-outlined/tasks.md"), "## Slice 1: a\n- [x] 1.1 a\n\n## Slice 2: b\nDelivers: B\n")
	write(t, filepath.Join(root, "openspec/changes/change-plain/tasks.md"), "## Intent\nDelivers: C\n")
	now := time.Now()
	for rel, ago := range map[string]time.Duration{"change-outlined": 3 * time.Hour, "change-plain": 2 * time.Hour, "change-done": time.Minute} {
		when := now.Add(-ago)
		if err := os.Chtimes(filepath.Join(root, "openspec/changes", rel, "tasks.md"), when, when); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"change-outlined", "change-done", "change-plain"}
	if got := RankChanges(root, []string{"change-done", "change-plain", "change-outlined"}); !slices.Equal(got, want) {
		t.Errorf("ranked: got %q, want %q", got, want)
	}
}

func TestCandidatesAreRankedByPendingWorkThenMovementThenName(t *testing.T) {
	root := umbrellaProject(t)
	now := time.Now()
	at := func(rel string, ago time.Duration) {
		when := now.Add(-ago)
		if err := os.Chtimes(filepath.Join(root, rel), when, when); err != nil {
			t.Fatal(err)
		}
	}
	pending, done := "## Slice 1: a\n- [ ] 1.1 a\n", "## Slice 1: a\n- [x] 1.1 a\n"
	write(t, filepath.Join(root, "openspec/changes/change-done/tasks.md"), done)
	write(t, filepath.Join(root, "openspec/changes/change-old/tasks.md"), pending)
	write(t, filepath.Join(root, "openspec/changes/change-new/tasks.md"), pending)
	write(t, filepath.Join(root, "openspec/changes/change-ran/tasks.md"), pending)
	write(t, filepath.Join(root, "openspec/changes/change-ran/execution-record.json"), "{}\n")
	write(t, filepath.Join(root, "openspec/changes/change-idle/proposal.md"), "## Why\nx\n")
	write(t, filepath.Join(root, "openspec/changes/change-same/tasks.md"), pending)
	at("openspec/changes/change-done/tasks.md", time.Minute)
	at("openspec/changes/change-old/tasks.md", 3*time.Hour)
	at("openspec/changes/change-same/tasks.md", 3*time.Hour)
	at("openspec/changes/change-new/tasks.md", time.Hour)
	at("openspec/changes/change-ran/tasks.md", 5*time.Hour)
	at("openspec/changes/change-ran/execution-record.json", 2*time.Minute)
	st := statusOf(t, root, nil)
	want := []string{"change-ran", "change-new", "change-old", "change-same", "change-done", "change-idle"}
	if st.Ok || st.Change != nil || st.Next != "select-change" || !slices.Equal(st.RankedChanges, want) {
		t.Errorf("ranked: got %q, want %q (%+v)", st.RankedChanges, want, st)
	}
	if !slices.Contains(st.Notes, "6 active changes; name one: "+strings.Join(want, ", ")) {
		t.Errorf("the note names them in that order: %q", st.Notes)
	}
	if !slices.Contains(st.ActiveChanges, "umbrella") || !slices.IsSortedFunc(st.ActiveChanges, strings.Compare) {
		t.Errorf("activeChanges keeps its meaning, every active change by name: %q", st.ActiveChanges)
	}
	if !strings.Contains(Human(st), "umbrella    umbrella (start-change: split it into changes, or write its proposal or tasks.md if it is one change)") {
		t.Errorf("human output lists the umbrella apart:\n%s", Human(st))
	}
}

// A new change that only has its exploration yet reads as an umbrella; its
// note names both ways on: split it, or give it its own record (T37 verify).
func TestNewChangeWithOnlyExplorationIsAnUmbrella(t *testing.T) {
	jhonRoot(t)
	root := configuredRepo(t, "")
	write(t, filepath.Join(root, "openspec/changes/new-thing/exploration.md"), "# x\n")
	st := statusOf(t, root, nil)
	if !st.Ok || st.Change != nil || st.Next != "start-change" || !slices.Equal(st.Umbrellas, []string{"new-thing"}) {
		t.Errorf("got ok=%v change=%v next=%s umbrellas=%q", st.Ok, st.Change, st.Next, st.Umbrellas)
	}
	if !strings.Contains(strings.Join(st.Notes, "\n"), "write its proposal or tasks.md if it is one change") {
		t.Errorf("notes %q do not name the change's own record", st.Notes)
	}
}

// Editing an umbrella does not make it the change in progress (T37 verify).
func TestEditedUmbrellaIsNotSelected(t *testing.T) {
	jhonRoot(t)
	root := configuredRepo(t, "")
	// On the default branch: no lane decides, only the edit could.
	gitT(t, root, "checkout", "-q", "-")
	write(t, filepath.Join(root, "openspec/changes/umbrella/exploration.md"), "# x\n")
	write(t, filepath.Join(root, "openspec/changes/change-a/tasks.md"), "- [ ] 1\n")
	write(t, filepath.Join(root, "openspec/changes/change-b/tasks.md"), "- [ ] 1\n")
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-qm", "c")
	write(t, filepath.Join(root, "openspec/changes/umbrella/exploration.md"), "# y\n")
	st := statusOf(t, root, nil)
	if st.Change != nil || slices.Contains(st.RankedChanges, "umbrella") || len(st.RankedChanges) != 2 {
		t.Errorf("change=%v ranked=%q notes=%q", deref(st.Change), st.RankedChanges, st.Notes)
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
