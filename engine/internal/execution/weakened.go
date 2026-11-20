package execution

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// A weakened test (T137, L1064 KB1): a test that a recorded run of the
// change names as failed and that a later recorded run passes, while
// between the two runs its own test file changed and no other file of its
// project did, may have been made green by weakening it instead of fixing
// the code. Nothing refuses it (a test change can be right); the record
// marks it, and verify and the Prism facets are launched with it to judge.
//
//   - The runs: each history entry keeps the tree it tested and, for each
//     project whose test command it ran (not reused), the tests its output
//     named as failed (FailedTests). A failure that named none is no
//     evidence of which tests failed, and is left out.
//   - The red run of a test is the newest earlier run that named it failed;
//     the green run is a later one that ran its project's tests and did not
//     name it.
//   - The test's own file is the part of its id before `::` (pytest) or
//     ` > ` (Vitest), relative to its project, the workspace root or the
//     git top level, whichever names a file that changed between the runs.
//   - A non-test file of its project is a changed file the project owns
//     (the deepest project that holds it, as an affected run selects) that
//     is no test module (readerFile) and not the test's own file; the
//     change's own artifacts and executions_inert files are none.

// TestRun is a project's test command a run ran, and the tests it named as failed.
type TestRun struct {
	Project string   `json:"project"`
	Failed  []string `json:"failed_tests,omitempty"`
}

// TurnedGreen is a test turned green by editing only its test: the run
// that named it failed and the later one that passed it, by timestamp, and
// the trees they tested.
type TurnedGreen struct {
	Project   string `json:"project"`
	Test      string `json:"test"`
	File      string `json:"file"`
	RedRun    string `json:"red_run"`
	GreenRun  string `json:"green_run"`
	RedTree   string `json:"red_tree"`
	GreenTree string `json:"green_tree"`
}

// testRunsOf is the test commands executions ran, each with the tests it
// named as failed: a pass, or a failure that named at least one.
func testRunsOf(executions []Execution) []TestRun {
	var out []TestRun
	for _, e := range executions {
		if e.Kind != "" || e.Reused || !(e.Status == Pass || e.Status == Fail && len(e.FailedTests) > 0) {
			continue
		}
		out = append(out, TestRun{e.Project, e.FailedTests})
	}
	return out
}

// TurnedGreenOf is the tests change's record marks as turned green by
// editing only their test, which a new run keeps; none when there is no
// record or it cannot be read.
func TurnedGreenOf(root, change string) []TurnedGreen {
	b, err := os.ReadFile(RecordFile(root, change))
	var r struct {
		TurnedGreen []TurnedGreen `json:"turned_green"`
	}
	if err != nil || json.Unmarshal(b, &r) != nil {
		return nil
	}
	return r.TurnedGreen
}

// MarkTurnedGreen adds to r, a complete run, the tests it turned green by
// editing only their test (the file's comment), each once by test and red
// run, and returns those it added. projects is every discovered project
// path, and inert the executions_inert entries.
func MarkTurnedGreen(root string, r *Record, projects, inert []string) []TurnedGreen {
	var added []TurnedGreen
	if r.WorkingTree == "" {
		return nil
	}
	diffs := map[string][]string{}
	changed := func(tree string) ([]string, bool) {
		if d, ok := diffs[tree]; ok {
			return d, d != nil
		}
		files, _, ok := ChangedBetween(root, tree, r.WorkingTree)
		if !ok {
			files = nil
		} else if files == nil {
			files = []string{}
		}
		diffs[tree] = files
		return files, ok
	}
	for _, run := range testRunsOf(r.Executions) {
		for i := len(r.History) - 1; i >= 0; i-- {
			red := r.History[i]
			for _, past := range red.Tests {
				if past.Project != run.Project {
					continue
				}
				for _, test := range past.Failed {
					if slices.Contains(run.Failed, test) || red.WorkingTree == "" || marked(r.TurnedGreen, run.Project, test, red.Timestamp) ||
						newerRed(r.History[i+1:], run.Project, test) {
						continue
					}
					files, ok := changed(red.WorkingTree)
					if !ok {
						continue
					}
					file := testFileOf(root, run.Project, test, files)
					if file == "" || sourceChanged(root, run.Project, file, files, projects, inert) {
						continue
					}
					m := TurnedGreen{run.Project, test, file, red.Timestamp, r.Timestamp, red.WorkingTree, r.WorkingTree}
					r.TurnedGreen = append(r.TurnedGreen, m)
					added = append(added, m)
				}
			}
		}
	}
	return added
}

// marked is whether marks already hold test of project from the red run at stamp.
func marked(marks []TurnedGreen, project, test, stamp string) bool {
	return slices.ContainsFunc(marks, func(m TurnedGreen) bool { return m.Project == project && m.Test == test && m.RedRun == stamp })
}

// newerRed is whether a run of later names test of project as failed: that
// one, the newest, is its red run.
func newerRed(later []PastRun, project, test string) bool {
	return slices.ContainsFunc(later, func(p PastRun) bool {
		return slices.ContainsFunc(p.Tests, func(t TestRun) bool { return t.Project == project && slices.Contains(t.Failed, test) })
	})
}

// testFileOf is test's own file, relative to root, among files (the paths,
// relative to root, that changed between the runs); "" when it did not change.
func testFileOf(root, project, test string, files []string) string {
	name := test
	if i := strings.Index(name, "::"); i >= 0 {
		name = name[:i]
	} else if i := strings.Index(name, " > "); i >= 0 {
		name = name[:i]
	}
	name = strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(name)), "./")
	if name == "" {
		return ""
	}
	candidates := []string{path.Join(project, name), name}
	// pytest under testmon names tests from the git top level (Precise).
	if top := vcs.GitTopLevel(root); top != "" {
		if prefix := filepath.ToSlash(relReal(top, root)); prefix != "" && strings.HasPrefix(name, prefix+"/") {
			candidates = append(candidates, name[len(prefix)+1:])
		}
	}
	for _, c := range candidates {
		if slices.Contains(files, c) {
			return c
		}
	}
	return ""
}

// sourceChanged is whether files hold a non-test file of project other
// than test, the test's own file.
func sourceChanged(root, project, test string, files, projects, inert []string) bool {
	return slices.ContainsFunc(files, func(f string) bool {
		if f == test || readerFile.MatchString(path.Base(f)) || layout.For(root).Ignored(f) || Inert(f, inert) {
			return false
		}
		o := owner(f, projects)
		if o == "" && slices.Contains(projects, ".") {
			o = "."
		}
		return o == project
	})
}
