package ldd

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// T85a (L609, L615): a worktree per change is a per-project setting,
// `worktree_per_change` in the LDD config, default off. Only when true do
// the status and the session brief name it; off, both stay byte-identical.

const worktreeBriefLine = "- Worktree per change: `worktree_per_change: true`. Each change works in its own git worktree outside the repository (`<repo>-worktrees/<change>`) on its branch: create it once, then start each command with `cd <its absolute path> &&` (the shell may reset the directory between calls) and nothing else repeated; run RED once. A change building on another's undelivered work branches from that change's branch, and the final message names the branch holding the whole delivery. Delivery puts the work on the default branch (merged locally when there is no remote or PR flow); after delivery and archive, `git worktree remove <that path>` and delete the merged branch: never remove the worktrees' container or anything outside the change's own worktree. Handoff files go outside the repository."

func TestWorktreePerChangeConfig(t *testing.T) {
	for _, c := range []struct {
		config string
		want   bool
		note   string
	}{
		{"", false, ""},
		{"worktree_per_change: false\n", false, ""},
		{"worktree_per_change: true\n", true, ""},
		{"worktree_per_change: true  # owner asked\n", true, ""},
		{"worktree_per_change: yes\n", false, "openspec/config.yaml sets worktree_per_change to 'yes'; it takes true or false, so a worktree per change is off"},
	} {
		got, note := WorktreePerChange(c.config, "openspec/config.yaml")
		if got != c.want || note != c.note {
			t.Errorf("%q: got %t %q, want %t %q", c.config, got, note, c.want, c.note)
		}
	}
}

func TestWorktreePerChangeBriefOnlyWhenTrue(t *testing.T) {
	jhonRoot(t)
	off := ProjectBrief(configuredRepo(t, "delivery: change\n", "src/app.py"))
	explicitOff := ProjectBrief(configuredRepo(t, "delivery: change\nworktree_per_change: false\n", "src/app.py"))
	on := ProjectBrief(configuredRepo(t, "delivery: change\nworktree_per_change: true\n", "src/app.py"))
	if strings.Contains(off, "orktree") || explicitOff != off {
		t.Errorf("off, the brief is unchanged:\n%s\n---\n%s", off, explicitOff)
	}
	if !strings.Contains(on, "\n"+worktreeBriefLine) {
		t.Errorf("on, the brief names the lifecycle:\n%s", on)
	}
	if strings.Replace(on, "\n"+worktreeBriefLine, "", 1) != off {
		t.Errorf("on adds only its line:\n%s", on)
	}
}

func TestWorktreePerChangeStatusOnlyWhenTrue(t *testing.T) {
	jhonRoot(t)
	view := func(config string) (Status, string, string) {
		s := statusOf(t, configuredRepo(t, config, "src/app.py"), nil)
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		// Each fixture is its own commit: its hash is no difference.
		head := regexp.MustCompile(`HEAD resolves to [0-9a-f]+`)
		return s, head.ReplaceAllString(Human(s), "HEAD"), head.ReplaceAllString(string(b), "HEAD")
	}
	_, offText, offJSON := view("delivery: change\n")
	_, falseText, falseJSON := view("delivery: change\nworktree_per_change: false\n")
	if strings.Contains(offText+offJSON, "worktree") || falseText != offText || falseJSON != offJSON {
		t.Errorf("off, status is unchanged:\n%s\n%s\n---\n%s\n%s", offText, offJSON, falseText, falseJSON)
	}
	s, onText, onJSON := view("delivery: change\nworktree_per_change: true\n")
	if !s.WorktreePerChange || !strings.Contains(onText, "\nworktree    per change  (worktree_per_change: true)\n") || !strings.HasSuffix(onJSON, `,"worktree_per_change":true}`) {
		t.Errorf("on, status names it:\n%s\n%s", onText, onJSON)
	}
	bad, badText, badJSON := view("delivery: change\nworktree_per_change: yes\n")
	if bad.WorktreePerChange || strings.Contains(badJSON, `"worktree_per_change"`) || !strings.Contains(badText, "sets worktree_per_change to 'yes'; it takes true or false") {
		t.Errorf("an invalid value is off and noted:\n%s\n%s", badText, badJSON)
	}
}
