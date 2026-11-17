package ldd

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// T136 (L1054 D7, L1056): each slice commit of a `delivery: change` names,
// by a test, every scenario the slice adds or edits in the change's spec
// deltas; a scenario copied unchanged into a MODIFIED block is counted
// apart and never refuses one.

const (
	scenarioOpen   = "## Slice 1: a\n- [ ] 1.1 a\n\n## Slice 2: b\n- [ ] 2.1 b\n"
	scenarioFirst  = "## Slice 1: a\n- [x] 1.1 a\n\n## Slice 2: b\n- [ ] 2.1 b\n"
	scenarioSecond = "## Slice 1: a\n- [x] 1.1 a\n\n## Slice 2: b\n- [x] 2.1 b\n"
	scenarioDelta  = "openspec/changes/c1/specs/auth/spec.md"
	scenarioMain   = "## Requirement: Login\nThe system SHALL let a user in.\n\n### Scenario: Rejects a bad password\n- WHEN the password is wrong\n- THEN the login fails\n"
)

// sliceScenarioMessage is the refusal of a slice commit whose scenarios a
// test does not all name, in the delivery message's words.
func sliceScenarioMessage(slice string, covered, total int, missing ...string) string {
	quoted := make([]string, len(missing))
	for i, m := range missing {
		quoted[i] = `"` + m + `"`
	}
	return "this commit closes " + slice + " of c1, and of the scenarios the slice adds or edits in the change's spec deltas, scenarios named by a test: " +
		strconv.Itoa(covered) + "/" + strconv.Itoa(total) + "; not yet: " + strings.Join(quoted, ", ") +
		": a test names the scenario it proves (its test name, or a `scenario: <name>` comment), then commit."
}

// scenarioRepo is a repository whose main branch holds the auth main spec
// and `delivery: change`, on a feature branch whose change c1 planned two
// slices.
func scenarioRepo(t *testing.T) string {
	t.Helper()
	jhonRoot(t)
	root, _ := filepath.EvalSymlinks(t.TempDir())
	gitT(t, root, "init", "-q")
	write(t, filepath.Join(root, "a/a.go"), "package a\n")
	write(t, filepath.Join(root, "openspec/config.yaml"), "delivery: change\n")
	write(t, filepath.Join(root, "openspec/specs/auth/spec.md"), scenarioMain)
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "base")
	gitT(t, root, "checkout", "-q", "-b", "feature")
	write(t, filepath.Join(root, "openspec/changes/c1/tasks.md"), scenarioOpen)
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "plan")
	return root
}

// sliceTested records a passing full run of the working tree as it is, so
// only the scenarios can refuse the slice commit.
func sliceTested(t *testing.T, root string) {
	t.Helper()
	tree := vcs.WorkingTree(root, execution.ChangeRecordPaths(root, "c1"))
	if tree == "" {
		t.Fatal("the working tree cannot be read")
	}
	write(t, execution.RecordFile(root, "c1"), `{"complete": true, "scope": "full", "working_tree": "`+tree+`", "after_working_tree": "`+tree+`", "executions": [{"project": ".", "command": "go test ./...", "status": "pass"}]}`+"\n")
}

// closeSlice checks off tasks, writes the test file, records the run, and
// asks whether the slice commit may run.
func closeSlice(t *testing.T, root, tasks, tests string) string {
	t.Helper()
	write(t, filepath.Join(root, "openspec/changes/c1/tasks.md"), tasks)
	write(t, filepath.Join(root, "a/a_test.go"), "package a\n\n"+tests)
	sliceTested(t, root)
	return SliceCommitRefusal(root)
}

const twoAdded = "## ADDED Requirements\n### Requirement: Lockout\nThe system SHALL lock an account.\n#### Scenario: Locks after five tries\n- WHEN five logins fail\n- THEN the account locks\n#### Scenario: Unlocks after an hour\n- WHEN an hour passes\n- THEN the account unlocks\n"

// K1: a slice adding two scenarios, one named by a test, is refused, the
// message naming the other verbatim.
func TestASliceCommitNamesTheScenariosItAddsThatNoTestNames(t *testing.T) {
	root := scenarioRepo(t)
	write(t, filepath.Join(root, scenarioDelta), twoAdded)
	got := closeSlice(t, root, scenarioFirst, "// scenario: Locks after five tries\n")
	if want := sliceScenarioMessage("Slice 1: a", 1, 2, "Unlocks after an hour"); got != want {
		t.Errorf("one of two scenarios named:\n got %q\nwant %q", got, want)
	}
	// A commit that closes no slice is never refused for its scenarios.
	if got := closeSlice(t, root, scenarioOpen, "// scenario: Locks after five tries\n"); got != "" {
		t.Errorf("a commit inside a slice: %q", got)
	}
	// Nor is any commit without `delivery: change`: the delivery check holds each slice.
	write(t, filepath.Join(root, "openspec/config.yaml"), "delivery: slice\n")
	if got := closeSlice(t, root, scenarioFirst, "// scenario: Locks after five tries\n"); got != "" {
		t.Errorf("delivery by slice: %q", got)
	}
}

// K2: both named, the slice commit runs.
func TestASliceCommitWhoseScenariosAreAllNamedRuns(t *testing.T) {
	root := scenarioRepo(t)
	write(t, filepath.Join(root, scenarioDelta), twoAdded)
	if got := closeSlice(t, root, scenarioFirst, "func TestLockout(t *testing.T) {\n\tt.Run(\"locks after five tries\", nil)\n}\n\n// scenario: Unlocks after an hour\n"); got != "" {
		t.Errorf("both scenarios named: %q", got)
	}
}

// A later slice answers for what it adds or edits since the commit that
// closed the slice before it, not for what that slice committed.
func TestASliceCommitAnswersForItsOwnScenarios(t *testing.T) {
	root := scenarioRepo(t)
	write(t, filepath.Join(root, scenarioDelta), "## ADDED Requirements\n### Requirement: Lockout\nThe system SHALL lock an account.\n#### Scenario: Locks after five tries\n- WHEN five logins fail\n")
	write(t, filepath.Join(root, "openspec/changes/c1/tasks.md"), scenarioFirst)
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "slice 1, committed with its scenario unnamed")

	write(t, filepath.Join(root, scenarioDelta), "## ADDED Requirements\n### Requirement: Lockout\nThe system SHALL lock an account.\n#### Scenario: Locks after five tries\n- WHEN five logins fail\n#### Scenario: Unlocks after an hour\n- WHEN an hour passes\n")
	if got, want := closeSlice(t, root, scenarioSecond, "// nothing named\n"), sliceScenarioMessage("Slice 2: b", 0, 1, "Unlocks after an hour"); got != want {
		t.Errorf("slice 2 answers for the scenario it adds only:\n got %q\nwant %q", got, want)
	}
	write(t, filepath.Join(root, scenarioDelta), "## ADDED Requirements\n### Requirement: Lockout\nThe system SHALL lock an account.\n#### Scenario: Locks after five tries\n- WHEN five logins fail in a minute\n#### Scenario: Unlocks after an hour\n- WHEN an hour passes\n")
	if got, want := closeSlice(t, root, scenarioSecond, "// scenario: Unlocks after an hour\n"), sliceScenarioMessage("Slice 2: b", 1, 2, "Locks after five tries"); got != want {
		t.Errorf("slice 2 answers for the scenario it edits:\n got %q\nwant %q", got, want)
	}
}

const modifiedDelta = "## MODIFIED Requirements\n### Requirement: Login\nThe system SHALL let a user in, and lock an account.\n(Previously: The system SHALL let a user in.)\n\n#### Scenario: Rejects a bad password\n- WHEN the password is wrong\n- THEN the login fails\n\n#### Scenario: Locks after five tries\n- WHEN five logins fail\n- THEN the account locks\n"

// K3: a scenario copied unchanged into a MODIFIED block, named by no test,
// never refuses a slice commit; one edited in the copy does; `jhon status`
// counts the copies apart; delivery still counts them (K4).
func TestAnUnchangedModifiedCopyNeverRefusesASliceCommit(t *testing.T) {
	root := scenarioRepo(t)
	write(t, filepath.Join(root, scenarioDelta), modifiedDelta)
	if got := closeSlice(t, root, scenarioFirst, "// scenario: Locks after five tries\n"); got != "" {
		t.Errorf("an unchanged copy refuses the slice commit: %q", got)
	}

	change := "c1"
	got := notes(statusOf(t, root, &change))
	for _, want := range []string{
		"scenarios named by a test: 1/2; not yet: \"Rejects a bad password\"",
		"of the scenarios not yet named by a test, copied unchanged from the main spec into MODIFIED blocks: 1 (\"Rejects a bad password\"); the change's own: 0. A slice commit never asks a test to name a copy; delivery does",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("status notes lack %q:\n%s", want, got)
		}
	}
	c, err := CheckDelivery(root, change, "merge", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !deliverReason(c, `^scenarios named by a test: 1/2; not yet: "Rejects a bad password": a test names the scenario it proves \(its test name, or a `+"`scenario: <name>`"+` comment\)$`) {
		t.Errorf("delivery counts the copy as before: %q", c.Reasons)
	}

	edited := strings.Replace(modifiedDelta, "- THEN the login fails", "- THEN the login fails and says why", 1)
	write(t, filepath.Join(root, scenarioDelta), edited)
	if got, want := closeSlice(t, root, scenarioFirst, "// scenario: Locks after five tries\n"), sliceScenarioMessage("Slice 1: a", 1, 2, "Rejects a bad password"); got != want {
		t.Errorf("a copy the slice edits:\n got %q\nwant %q", got, want)
	}
	got = notes(statusOf(t, root, &change))
	if regexp.MustCompile(`copied unchanged`).MatchString(got) {
		t.Errorf("an edited copy is the change's own:\n%s", got)
	}
}
