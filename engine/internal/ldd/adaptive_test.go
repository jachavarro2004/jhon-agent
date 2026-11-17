package ldd

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func text(s string) *string { return &s }

// The proposal states what verify checks against, its acceptance criteria,
// and restates nothing the core reads elsewhere: the risk tier is jhon
// risk's, the slices are tasks.md's sections.
func TestProposalNotesAskForAcceptanceCriteriaOnly(t *testing.T) {
	enough := "# Proposal: x\n## Why\nw\n## What Changes\nc\n## Acceptance Criteria\n- a\n"
	if got := ProposalNotes(text(enough)); len(got) != 0 {
		t.Errorf("a proposal with acceptance criteria and no Risk or Slices has no note, got %q", got)
	}
	if got := ProposalNotes(nil); len(got) != 0 {
		t.Errorf("no proposal, no note: %q", got)
	}
	for _, src := range []string{"# Proposal: x\n## Why\nw\n", "# Proposal: x\n## Acceptance Criteria\n\n## Why\nw\n"} {
		if got := strings.Join(ProposalNotes(text(src)), "\n"); !strings.Contains(got, "## Acceptance Criteria") {
			t.Errorf("missing or empty acceptance criteria are noted: %q", got)
		}
	}
	eq := func(src string, want bool) {
		if HasAcceptanceCriteria(src) != want {
			t.Errorf("HasAcceptanceCriteria(%q) != %v", src, want)
		}
	}
	eq(enough, true)
	eq("## Acceptance Criteria\n\n## Why\n", false)
}

// tasks.md is planned slice by slice; a task need not cite a requirement.
func TestTasksNotesAskForSlices(t *testing.T) {
	good := "# Tasks\n## Slice 1: login\nDelivers: CA-01\n- [ ] 1.1 RED a_test.go: rejects bad password\n- [x] 1.2 GREEN a.go: check\n"
	if got := TasksNotes(text(good)); len(got) != 0 {
		t.Errorf("tasks in a slice have no note, cited or not: %q", got)
	}
	if got := strings.Join(TasksNotes(text(strings.Replace(good, "Delivers: CA-01\n", "", 1))), "\n"); !strings.Contains(got, "Slice 1: login name(s) no value it delivers") {
		t.Errorf("a slice without its value is noted: %q", got)
	}
	flat := "# Tasks\n- [ ] 1.1 do a\n- [ ] 1.2 do b\n"
	if got := strings.Join(TasksNotes(text(flat)), "\n"); !strings.Contains(got, "## Slice <n>:") {
		t.Errorf("tasks without slices are noted: %q", got)
	}
}

func TestPlanAheadCountsTasksPlannedBeforeTheirTurn(t *testing.T) {
	for _, c := range []struct {
		name, src string
		want      PlanAhead
	}{
		{"only the current slice is planned", "## Slice 1: a\n- [ ] 1.1 RED\n- [ ] 1.2 GREEN\n## Slice 2: b\n- [ ] 2.1 Plan the slice\n", PlanAhead{}},
		{"the next slice planned once the current is done", "## Slice 1: a\n- [x] 1.1 RED\n## Slice 2: b\n- [ ] 2.1 RED\n- [ ] 2.2 GREEN\n## Slice 3: c\n- [ ] 3.1 Plan the slice\n", PlanAhead{}},
		{"later slices planned ahead", "## Slice 1: a\n- [ ] 1.1 RED\n## Slice 2: b\n- [ ] 2.1 RED\n- [ ] 2.2 GREEN\n## Slice 3: c\n- [ ] 3.1 x\n- [ ] 3.2 y\n- [ ] 3.3 z\n", PlanAhead{3, []string{"Slice 2: b", "Slice 3: c"}}},
		{"tasks outside any slice", "# Tasks\n## PR 1\n- [ ] a\n- [ ] b\n", PlanAhead{2, nil}},
		{"every slice done", "## Slice 1: a\n- [x] 1.1\n## Slice 2: b\n- [x] 2.1\n- [x] 2.2\n", PlanAhead{}},
		{"an outline of later slices is no planning ahead", "## Slice 1: a\nDelivers: x\n- [ ] 1.1 RED\n- [ ] 1.2 GREEN\n## Slice 2: b\nDelivers: y\n## Slice 3: c\nDelivers: z\n", PlanAhead{}},
		{"tasks in slice 5 of an outline are planned ahead", "## Slice 1: a\n- [x] 1.1\n## Slice 2: b\n- [x] 2.1\n## Slice 3: c\n- [ ] 3.1\n## Slice 4: d\n## Slice 5: e\n- [ ] 5.1 x\n- [ ] 5.2 y\n## Slice 6: f\n", PlanAhead{1, []string{"Slice 5: e"}}},
		{"an outline slice before them is the current one", "## Slice 1: a\n- [x] 1.1\n## Slice 2: b\n## Slice 3: c\n- [ ] 3.1 x\n- [ ] 3.2 y\n", PlanAhead{1, []string{"Slice 3: c"}}},
	} {
		if got := PlanAheadOf(c.src); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
	if got := strings.Join(TasksNotes(text("## Slice 1: a\n- [ ] 1.1 x\n## Slice 2: b\n- [ ] 2.1 y\n- [ ] 2.2 z\n")), "\n"); !strings.Contains(got, "in a later slice than the current one") {
		t.Errorf("tasks planned ahead are noted: %q", got)
	}
}

// Every departure from the adaptive flow is a note `jhon status` shows,
// never a refusal (S20): a flat list, tasks planned ahead, a slice without
// its value, a slice past the advisory size, a proposal without acceptance
// criteria.
func TestEveryDepartureIsANote(t *testing.T) {
	deliverEnv(t)
	for _, c := range []struct {
		name, src string
		want      []string
	}{
		{"a flat list", "# Tasks\n- [ ] 1.1 do a\n- [ ] 1.2 do b\n",
			[]string{"tasks.md has no `## Slice <n>:` section; the adaptive flow plans each slice's tasks under its own"}},
		{"a later slice planned ahead", "## Slice 1: a\nDelivers: x\n- [ ] 1.1 x\n## Slice 2: b\nDelivers: y\n- [ ] 2.1 y\n- [ ] 2.2 z\n",
			[]string{"1 task(s) in a later slice than the current one (Slice 2: b); write them when its turn comes, on what exists then"}},
		{"a slice without its value", "## Slice 1: login\n- [ ] 1.1 x\n",
			[]string{"Slice 1: login name(s) no value it delivers: add a `Delivers: <acceptance criteria or observable value>` line under each heading"}},
		{"a slice past the advisory size, nested tasks counted", "## Slice 1: catalog\nDelivers: CA-01\n" + checkboxes(10, "") + checkboxes(3, "  "),
			[]string{"Slice 1: catalog holds 13 tasks, past the advisory 12 a slice holds; the rest can be the next slice"}},
		{"a slice at the advisory size", "## Slice 1: catalog\nDelivers: CA-01\n" + checkboxes(12, "") + "```\n- [ ] fenced\n```\n", nil},
		{"a task nested past one level", "## Slice 1: a\nDelivers: x\n- [ ] 1.1 a\n  - [ ] 1.1.1 b\n    - [ ] 1.1.1.1 c\n", nil},
	} {
		if got := TasksNotes(text(c.src)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	if got, want := ProposalNotes(text("# Proposal: x\n## Why\nw\n")), []string{"proposal.md has no `## Acceptance Criteria`; the adaptive flow states it in the intent, and verify checks against it"}; !reflect.DeepEqual(got, want) {
		t.Errorf("a proposal without acceptance criteria: %q, want %q", got, want)
	}
}

// checkboxes is n unchecked tasks, each line starting with prefix.
func checkboxes(n int, prefix string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "%s- [ ] t%d\n", prefix, i)
	}
	return b.String()
}

// The advisory size is the vocabulary's, not the code's.
func TestSliceTasksAreReadFromTheVocabulary(t *testing.T) {
	if got, err := SliceTasksOf("slice_limits:\n  tasks: 12\n"); err != nil || got != 12 {
		t.Errorf("SliceTasksOf = %d, %v", got, err)
	}
	for _, bad := range []string{"", "slice_limits:\n  tasks: twelve\n", "slice_limits:\n  depth: 1\n", "slice_limits:\n  tasks: 0\n"} {
		if _, err := SliceTasksOf(bad); err == nil {
			t.Errorf("SliceTasksOf(%q) read a broken declaration", bad)
		}
	}
	deliverEnv(t)
	if n, err := ReadSliceTasks(); err != nil || n != 12 {
		t.Errorf("contracts/vocabulary.yaml slice_limits.tasks = %d, %v", n, err)
	}
}

func TestScenarioCoverageCountsTheScenariosATestNames(t *testing.T) {
	deliverEnv(t)
	spec := "## ADDED Requirements\n### Requirement: Login\nThe system SHALL.\n#### Scenario: Rejects a bad password\n- WHEN x\n#### Scenario: Locks after five tries\n- WHEN y\n"
	tests := map[string]string{
		"a_test.go": `t.Run("rejects a bad password", func(t *testing.T) {})`,
		"b.test.ts": "// scenario: something else",
	}
	c, err := ScenarioCoverage([]string{spec}, func(visit func(path, src string)) {
		for p, s := range tests {
			visit(p, s)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.Total != 2 || c.Covered != 1 || !reflect.DeepEqual(c.Uncovered, []string{"Locks after five tries"}) {
		t.Errorf("coverage = %+v", c)
	}
	tests["b.test.ts"] = "// scenario: Locks after five tries"
	if c, _ := ScenarioCoverage([]string{spec}, func(visit func(path, src string)) {
		for p, s := range tests {
			visit(p, s)
		}
	}); c.Covered != 2 {
		t.Errorf("a scenario: comment covers it: %+v", c)
	}
}

func TestVerifyNotesCrossCheckTheCitedRunsWithTheRecord(t *testing.T) {
	report := "verdict: pass\n\n## Test runs\n| Command | Exit | Tree |\n|---|---|---|\n| `go test ./...` | 0 | abc |\n| `npm test` | 0 | abc |\n"
	record := []RecordedRun{{Command: "go test ./...", ExitCode: 1}, {Command: "pytest", ExitCode: 0}}
	got := strings.Join(VerifyNotes(report, record), "\n")
	if !strings.Contains(got, "`go test ./...`") || !strings.Contains(got, "exit 1") {
		t.Errorf("a cited exit the record contradicts is noted: %q", got)
	}
	if strings.Contains(got, "npm test") {
		t.Errorf("a command run outside the record is not contradicted: %q", got)
	}
	if got := VerifyNotes(report, nil); len(got) != 0 {
		t.Errorf("without a record nothing is cross-checked: %q", got)
	}
}

func TestStatusNotesTheAdaptiveFlowsGaps(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "openspec", "changes", "login")
	files := map[string]string{
		"proposal.md":           "# Proposal: login\n## Why\nw\n## What Changes\nc\n",
		"tasks.md":              "# Tasks\n- [x] 1.1 do a [req: Login]\n",
		"specs/auth/spec.md":    "## ADDED Requirements\n### Requirement: Login\nThe system SHALL.\n#### Scenario: Rejects a bad password\n- WHEN x\n#### Scenario: Locks after five tries\n- WHEN y\n",
		"verify-report.md":      "verdict: pass\n\n## Test runs\n| Command | Exit | Tree |\n|---|---|---|\n| `go test ./...` | 0 | abc |\n",
		"execution-record.json": `{"schema":"jhon.execution-record/v2","executions":[{"project":".","command":"go test ./...","cwd":".","exit_code":1,"status":"fail"}]}`,
	}
	for f, src := range files {
		write(t, filepath.Join(dir, f), src)
	}
	write(t, filepath.Join(root, "login_test.go"), `t.Run("rejects a bad password", nil)`)
	change := "login"
	got := notes(statusOf(t, root, &change))
	for _, want := range []string{
		"proposal.md has no `## Acceptance Criteria`",
		"tasks.md has no `## Slice <n>:` section",
		"scenarios named by a test: 1/2; not yet: \"Locks after five tries\"",
		"verify-report.md cites `go test ./...` exiting 0; execution-record.json recorded exit 1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("status notes lack %q:\n%s", want, got)
		}
	}
}
