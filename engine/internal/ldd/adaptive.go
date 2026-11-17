package ldd

// This file reads what the adaptive flow (contracts/ldd.md,
// "Slices") asks of a change's artifacts and reports what is missing
// as `jhon status` notes: never a state, a gate, or a suggestion.

import (
	"encoding/json"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/discover"
	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/requirements"
)

// ProposalNotes are what proposal.md lacks of the adaptive flow's intent:
// its acceptance criteria. The risk tier is jhon risk's to read and the
// slices are tasks.md's sections, so the proposal restates neither.
func ProposalNotes(src *string) []string {
	if src == nil || HasAcceptanceCriteria(*src) {
		return nil
	}
	return []string{"proposal.md has no `## Acceptance Criteria`; the adaptive flow states it in the intent, and verify checks against it"}
}

// planTask is the one task a slice held until its turn in earlier
// releases: `<n>.1 Plan the slice`.
var planTask = lazyre.New(`(?i)^(\d+(\.\d+)*\s+)?plan the slice\b`)

// SliceToPlan is the current slice when its turn has come and it holds no
// tasks yet: an outline (its heading and Delivers: line), or only the task
// that plans it; nil otherwise.
func SliceToPlan(t TaskProgress) *string {
	if c := currentSlice(t); c >= 0 && t.Slices[c].Outline {
		return &t.Slices[c].Name
	}
	i := slices.IndexFunc(t.Slices, func(x SliceProgress) bool { return t.Slice != nil && x.Name == *t.Slice })
	if i <= 0 || t.Slices[i].Total != 1 || len(t.Unchecked) == 0 || !planTask.MatchString(t.Unchecked[0]) {
		return nil
	}
	return t.Slice
}

// TasksNotes are tasks.md's departures from the adaptive flow: tasks outside
// any `## Slice <n>:` section, tasks in a later slice than the current one, a
// slice without its value, and a slice past the advisory size. They are
// notes, never a refusal: the plan grows as the change goes, on what exists.
func TasksNotes(lay layout.Layout, src *string) []string {
	if src == nil {
		return nil
	}
	progress := ProgressOf(lay, src)
	if progress.Total == 0 {
		return nil
	}
	var out []string
	if len(progress.Slices) == 0 {
		out = append(out, "tasks.md has no `## Slice <n>:` section; the adaptive flow plans each slice's tasks under its own")
	}
	if ahead := PlanAheadOf(lay, *src); len(ahead.Slices) > 0 {
		out = append(out, fmt.Sprintf("%d task(s) in a later slice than the current one (%s); write them when its turn comes, on what exists then", ahead.Tasks, strings.Join(ahead.Slices, ", ")))
	}
	if bare := SlicesWithoutValue(lay, *src); len(bare) > 0 {
		out = append(out, fmt.Sprintf("%s name(s) no value it delivers: add a `Delivers: <acceptance criteria or observable value>` line under each heading", strings.Join(bare, ", ")))
	}
	if limit, err := ReadSliceTasks(); err == nil {
		for _, x := range progress.Slices {
			if x.Total > limit {
				out = append(out, fmt.Sprintf("%s holds %d tasks, past the advisory %d a slice holds; the rest can be the next slice", x.Name, x.Total, limit))
			}
		}
	}
	return out
}

// delivers is a slice's value line: `Delivers:` and what it delivers (the
// acceptance criteria it meets, or the value a user or caller observes).
var delivers = lazyre.New(`^\s*(?:[-*]\s+)?(?:\*\*)?Delivers:(?:\*\*)?\s*\S`)

// SlicesWithoutValue are the `## Slice <n>:` sections of tasks.md, fenced
// lines ignored, with no `Delivers:` line naming the value the slice brings
// (contracts/ldd.md, "Slices"): a slice is a piece of value, so it says
// which. `jhon status` notes them.
func SlicesWithoutValue(lay layout.Layout, src string) []string {
	var out []string
	current, level, valued := "", 0, false
	flush := func() {
		if current != "" && !valued {
			out = append(out, current)
		}
	}
	for _, l := range unfenced(src) {
		if h := lay.SliceHeading().FindStringSubmatch(l.Text); h != nil {
			flush()
			current, valued = h[1], false
			level = len(l.Text) - len(strings.TrimLeft(l.Text, "#"))
			continue
		}
		// A heading of the slice's level or above ends its section; a
		// subheading stays in it.
		if h := requirements.HeadingOf(l); current != "" && h != nil && h.Level <= level {
			flush()
			current = ""
			continue
		}
		valued = valued || delivers.MatchString(l.Text)
	}
	flush()
	return out
}

// HasAcceptanceCriteria is whether proposal.md states its acceptance
// criteria: a `## Acceptance Criteria` section with content, which verify
// checks against. `jhon status` notes a proposal without them.
func HasAcceptanceCriteria(src string) bool {
	return slices.ContainsFunc(section(src, "Acceptance Criteria"), func(l requirements.Line) bool { return strings.TrimSpace(l.Text) != "" })
}

// PlanAhead is how much of tasks.md is planned before its turn: the tasks
// outside any `## Slice <n>:` section, and every task past the first of a
// slice after the current one (the first not done, an outline included); an
// outline slice, its heading and Delivers: line, is no planning ahead. Slices names those slices.
type PlanAhead struct {
	Tasks  int
	Slices []string
}

// PlanAheadOf is the planning ahead of tasks.md, which `jhon status` notes.
func PlanAheadOf(lay layout.Layout, src string) PlanAhead {
	t := ProgressOf(lay, &src)
	var out PlanAhead
	out.Tasks = t.Total
	for _, x := range t.Slices {
		out.Tasks -= x.Total
	}
	current := currentSlice(t)
	if current < 0 {
		return out
	}
	for _, x := range t.Slices[current+1:] {
		if x.Total > 1 {
			out.Tasks += x.Total - 1
			out.Slices = append(out.Slices, x.Name)
		}
	}
	return out
}

// SliceTasksOf is the advisory number of tasks a slice holds, nested ones
// included, that vocab declares (`slice_limits.tasks`); the error names a
// number absent or not positive.
func SliceTasksOf(vocab string) (int, error) {
	_, m := contract.Map(vocab, "slice_limits")
	n, err := strconv.Atoi(m["tasks"])
	if err != nil || n < 1 {
		return 0, fmt.Errorf("contracts/vocabulary.yaml: slice_limits.tasks must be a positive number, found %q", m["tasks"])
	}
	return n, nil
}

// sliceNumber is the number of a slice's name (`Slice 3: x` is 3).
var sliceNumber = lazyre.New(`^\S+\s+(\d+)`)

// ReadSliceTasks is the release's advisory slice size.
func ReadSliceTasks() (int, error) {
	src, err := contract.Contract("vocabulary.yaml")
	if err != nil {
		return 0, err
	}
	return SliceTasksOf(src)
}

// Coverage is how many of a change's delta-spec scenarios some test names.
type Coverage struct {
	Total     int      `json:"total"`
	Covered   int      `json:"covered"`
	Uncovered []string `json:"uncovered"`
}

// normalized is text lowered with every run of non-word characters (neither
// a letter nor a digit) made one space, padded with one space each side. It
// walks the runes once: status runs it over every test file of the project.
func normalized(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte(' ')
	gap := true
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(unicode.ToLower(r))
			gap = false
		} else if !gap {
			b.WriteByte(' ')
			gap = true
		}
	}
	if !gap || b.Len() == 1 {
		b.WriteByte(' ')
	}
	return b.String()
}

// ScenarioCoverage is the scenarios of the specs a test names: its name
// appears in a test file, in a test's title or a `scenario: <name>` comment,
// matched case- and punctuation-insensitively. tests visits every test file.
// The error is a broken delta contract.
func ScenarioCoverage(l layout.Layout, specs []string, tests func(visit func(path, src string))) (Coverage, error) {
	if _, err := requirements.Delta(); err != nil {
		return Coverage{}, err
	}
	var names []string
	for _, src := range specs {
		reqs, err := l.Scenarios(src, 0)
		if err != nil {
			return Coverage{}, err
		}
		for _, r := range reqs {
			for _, s := range r.Scenarios {
				if s.Name != "" && !slices.Contains(names, s.Name) {
					names = append(names, s.Name)
				}
			}
		}
	}
	return namedCoverage(names, tests), nil
}

// namedCoverage is which of names, scenario names in order and each once, a
// test names, as ScenarioCoverage matches them. tests visits every test file.
func namedCoverage(names []string, tests func(visit func(path, src string))) Coverage {
	// A file can name a scenario only if its lowered text holds the name's
	// longest word: normalizing keeps every run of letters and digits as
	// lowering makes it. Only a file that holds one is normalized.
	type wanted struct{ name, norm, word string }
	looked := make([]wanted, len(names))
	for i, n := range names {
		looked[i] = wanted{name: n, norm: normalized(n)}
		for _, w := range strings.Fields(looked[i].norm) {
			if len(w) > len(looked[i].word) {
				looked[i].word = w
			}
		}
	}
	covered := map[string]bool{}
	tests(func(_, src string) {
		if len(covered) == len(names) {
			return
		}
		lower, text := strings.ToLower(src), ""
		for _, w := range looked {
			if covered[w.name] || !strings.Contains(lower, w.word) {
				continue
			}
			if text == "" {
				text = normalized(src)
			}
			if strings.Contains(text, w.norm) {
				covered[w.name] = true
			}
		}
	})
	out := Coverage{Total: len(names), Uncovered: []string{}}
	for _, n := range names {
		if covered[n] {
			out.Covered++
		} else {
			out.Uncovered = append(out.Uncovered, n)
		}
	}
	return out
}

// RecordedRun is one command an execution record ran and how it exited.
type RecordedRun struct {
	Command  string
	ExitCode int
}

var runRow = lazyre.New("^\\|\\s*`?([^`|]+?)`?\\s*\\|\\s*(-?\\d+)\\s*\\|")

// VerifyNotes are the test runs verify-report.md cites (its `## Test runs`
// table) whose exit code the execution record contradicts. A command the
// record does not hold was run directly, which the contract allows.
func VerifyNotes(report string, record []RecordedRun) []string {
	if len(record) == 0 {
		return nil
	}
	var out []string
	for _, l := range section(report, "Test runs") {
		m := runRow.FindStringSubmatch(strings.TrimSpace(l.Text))
		if m == nil {
			continue
		}
		cited, _ := strconv.Atoi(m[2])
		for _, r := range record {
			if r.Command == strings.TrimSpace(m[1]) && r.ExitCode != cited {
				out = append(out, fmt.Sprintf("verify-report.md cites `%s` exiting %d; execution-record.json recorded exit %d", r.Command, cited, r.ExitCode))
			}
		}
	}
	return out
}

// maxTestFiles and maxTestBytes bound the scan for scenario names.
const (
	maxTestFiles = 5000
	maxTestBytes = 1 << 20
)

// testFiles visits the repository's test files, by the stacks' conventions,
// never entering what discovery excludes or openspec/.
func testFiles(root string) func(visit func(path, src string)) {
	return func(visit func(path, src string)) {
		seen := 0
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || seen >= maxTestFiles {
				return filepath.SkipDir
			}
			if d.IsDir() {
				if p != root && (discover.SkipDir(d.Name()) || d.Name() == layout.For(root).Dir()) {
					return filepath.SkipDir
				}
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			if !d.Type().IsRegular() || !discover.IsTestPath(rel) {
				return nil
			}
			if info, err := d.Info(); err != nil || info.Size() > maxTestBytes {
				return nil
			}
			if b, err := os.ReadFile(p); err == nil {
				seen++
				visit(rel, string(b))
			}
			return nil
		})
	}
}

// recordedRuns are the commands the change's execution record ran and their
// exit codes; none when it has no readable record.
func recordedRuns(root, change string) []RecordedRun {
	b, err := os.ReadFile(execution.RecordFile(root, change))
	if err != nil {
		return nil
	}
	var r struct {
		Executions []struct {
			Command  string `json:"command"`
			ExitCode *int   `json:"exit_code"`
		} `json:"executions"`
	}
	if json.Unmarshal(b, &r) != nil {
		return nil
	}
	var out []RecordedRun
	for _, e := range r.Executions {
		if e.ExitCode != nil {
			out = append(out, RecordedRun{e.Command, *e.ExitCode})
		}
	}
	return out
}
