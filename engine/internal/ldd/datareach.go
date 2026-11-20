package ldd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// A project's openspec/config.yaml may declare where its code reaches the
// database and which of its tests run against a real one (T138, L1054 D1):
//
//	executions_data_access: [backend/app/repositories/**]
//	executions_database_tests: [backend/tests/db/**]
//
// both globs over paths relative to the repository's top level (`**` any
// depth, `*` within one segment). A slice commit of a `delivery: change` whose
// slice changes a file under a data-access path is then refused unless the
// change's recorded run on that tree executed a declared real-database test
// whose last execution in pytest-testmon's record ran that file (by file, as
// execution.TestmonReach counts them): a unit test that compiles the query
// against a fake session never reaches the database. Whether that test had
// data to read stays the arbiter's judgment.

// quotedPy is a test module an own run names (execution.OwnTestsCommand).
var quotedPy = lazyre.New(`'[^']*\.py'`)

// dataReach is, for the slice in progress of change in the repository whose
// top level is top, the files it changes under a declared data-access path
// that no declared real-database test reached in the change's recorded run
// (unreached), or, when no test project of that run has a usable
// pytest-testmon record, all of them (unchecked). Both are empty without the
// declaration, without a recorded run, or when the slice changes none.
func (g gitReads) dataReach(top, change string) (unreached, unchecked []string) {
	config, err := project.ReadLddConfig(top)
	if err != nil {
		return nil, nil
	}
	access := contract.InlineList(contract.Scalar(config, "executions_data_access"))
	if len(access) == 0 {
		return nil, nil
	}
	tests := contract.InlineList(contract.Scalar(config, "executions_database_tests"))
	base, ok := g.sliceBase(top, change)
	if !ok {
		return nil, nil
	}
	files, _ := g.addedOutsideRecord(top, base)
	var touched []string
	for _, f := range files {
		// A deleted file is reached by no test.
		if !slices.Contains(touched, f) && project.Exists(filepath.Join(top, f)) && matchesAny(access, f) {
			touched = append(touched, f)
		}
	}
	if len(touched) == 0 {
		return nil, nil
	}
	b, err := os.ReadFile(execution.RecordFile(top, change))
	var record struct {
		Executions []struct {
			Project string `json:"project"`
			Kind    string `json:"kind"`
			Command string `json:"command"`
			Status  string `json:"status"`
		} `json:"executions"`
	}
	if err != nil || json.Unmarshal(b, &record) != nil {
		return nil, nil
	}
	reached, checked := map[string]bool{}, false
	for _, e := range record.Executions {
		data := execution.TestmonData(top, e.Project)
		if e.Kind != "" || execution.TestmonRecord(data) != execution.RecordUsable {
			continue
		}
		reach, ok := execution.TestmonReachers(data, touched)
		if !ok {
			continue
		}
		checked = true
		for f, names := range reach {
			for _, n := range names {
				module, _, _ := strings.Cut(n, "::")
				if matchesAny(tests, module) && ranModule(e.Command, e.Status == execution.Pass, e.Project, module) {
					reached[f] = true
				}
			}
		}
	}
	if !checked {
		return nil, touched
	}
	for _, f := range touched {
		if !reached[f] {
			unreached = append(unreached, f)
		}
	}
	return unreached, nil
}

// matchesAny is whether one of globs (GlobRegex) matches the path f.
func matchesAny(globs []string, f string) bool {
	return slices.ContainsFunc(globs, func(g string) bool { return GlobRegex(g).MatchString(f) })
}

// ranModule is whether a run of command in the project at projectPath,
// passed or not, executed the test module (relative to the repository's top
// level) in one of its parts (execution.CommandParts) that ran: under
// pytest-testmon, which selects every test whose executed code changed,
// unless it is narrowed to named modules (execution.TestmonModulesCommand);
// a whole run; or an own run (execution.OwnTestsCommand) that names it. The
// parts of a wide step (execution.BothCommand) all ran, in either order; of
// an `&&` chain that failed, only the first surely did (T145c, L1191 H1).
func ranModule(command string, passed bool, projectPath, module string) bool {
	rel, err := filepath.Rel(projectPath, module)
	parts, each := execution.CommandParts(command)
	for i, part := range parts {
		if i > 0 && !each && !passed {
			break
		}
		if !quotedPy.MatchString(part) || err == nil && strings.Contains(part, "'"+filepath.ToSlash(rel)+"'") {
			return true
		}
	}
	return false
}

// dataReachRefusal is why a commit that closes the slices closed of change is
// refused for its data-access files no real-database test reached, "" when
// none.
func (g gitReads) dataReachRefusal(top, change string, closed []string) string {
	unreached, _ := g.dataReach(top, change)
	if len(unreached) == 0 {
		return ""
	}
	it, each := "it", "it"
	if len(unreached) > 1 {
		it, each = "them", "each"
	}
	return fmt.Sprintf("this commit closes %s of %s, and it changes %s under a declared data-access path (executions_data_access), and no test declared as real-database (executions_database_tests) reached %s in the recorded run on this tree, by pytest-testmon's record: a test declared as real-database must reach %s in the recorded run. Write or select one that runs it against the database, run `jhon executions run %s --affected <base>` on the slice as it will be committed, then commit.",
		strings.Join(closed, ", "), change, strings.Join(unreached, ", "), it, each, change)
}

// DataReachNote is, for a `delivery: change` change in the repository whose
// top level is top, the note that its slice's data-access files could not be
// checked for a real-database test's reach, since no test project of its
// recorded run has a pytest-testmon record: the run and `jhon status` say it,
// and a slice commit is not refused for it. "" otherwise.
func DataReachNote(top, change string) string {
	return gitReads(nil).dataReachNote(top, change)
}

func (g gitReads) dataReachNote(top, change string) string {
	if DeliveryUnit(top) != "change" {
		return ""
	}
	_, unchecked := g.dataReach(top, change)
	if len(unchecked) == 0 {
		return ""
	}
	is, it := "is", "it"
	if len(unchecked) > 1 {
		is, it = "are", "them"
	}
	return fmt.Sprintf("%s %s under a declared data-access path (executions_data_access), and whether a test declared as real-database (executions_database_tests) reached %s in the recorded run could not be checked for lack of a pytest-testmon record of its tests; a slice commit is not refused for it",
		strings.Join(unchecked, ", "), is, it)
}
