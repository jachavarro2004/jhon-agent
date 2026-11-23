package ldd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T208 (L1643): a change that gets its folder mid-change keeps the runs it
// recorded before it under the git common dir: its RED before GREEN stays
// known once its record moves to the folder, each earlier run kept once,
// and the record under the git common dir is left as it was.

const folderRecord = "openspec/changes/" + smallChange + "/execution-record.json"

// historyOf is the record at rel's history, each run's timestamp and outcome.
func historyOf(t *testing.T, root, rel string) (stamps []string, passed []bool) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Timestamp string `json:"timestamp"`
		History   []struct {
			Timestamp string `json:"timestamp"`
			Passed    bool   `json:"passed"`
		} `json:"history"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	for _, h := range r.History {
		stamps, passed = append(stamps, h.Timestamp), append(passed, h.Passed)
	}
	return stamps, passed
}

// timestampOf is the record at rel's timestamp.
func timestampOf(t *testing.T, root, rel string) string {
	t.Helper()
	return object(t, support.Read(t, filepath.Join(root, rel)))["timestamp"].(string)
}

// runSmall runs the small change's full run, red or not, and is its summary.
func runSmall(t *testing.T, root string, red bool) map[string]any {
	t.Helper()
	want := 0
	if red {
		want = 1
		support.Write(t, root, map[string]string{"red": ""})
	} else {
		os.Remove(filepath.Join(root, "red"))
	}
	r := jhonGit(t, root, "executions", "run", smallChange, "--json")
	equal(t, r.Code, want, r.Out())
	return object(t, r.Stdout)
}

// statusRedGreen is status's tests.red_before_green for the small change.
func statusRedGreen(t *testing.T, root string) any {
	t.Helper()
	r := jhonGit(t, root, "status", smallChange, "--json")
	equal(t, r.Code, 0, r.Out())
	return object(t, string(field(t, []byte(r.Stdout), "tests")))["red_before_green"]
}

// The RED run recorded with no folder, then the folder, then the GREEN run:
// the GREEN's record carries the RED, status says red before green, and a
// later run keeps each earlier run once.
func TestFolderKeepsRedRecordedBeforeIt(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	red := runSmall(t, root, true)
	equal(t, red["record"], smallRecord)
	redAt := timestampOf(t, root, smallRecord)
	before := support.Read(t, filepath.Join(root, smallRecord))

	support.Write(t, root, map[string]string{"openspec/changes/" + smallChange + "/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n"})
	green := runSmall(t, root, false)
	equal(t, green["record"], folderRecord)
	equal(t, green["red_before_green"], true, "the run's summary")
	equal(t, statusRedGreen(t, root), true, "status's tests")
	stamps, passed := historyOf(t, root, folderRecord)
	equal(t, stamps, []string{redAt}, "the RED run is carried into the folder's record")
	equal(t, passed, []bool{false})
	equal(t, support.Read(t, filepath.Join(root, smallRecord)), before, "the record under the git common dir is left as it was")

	greenAt := timestampOf(t, root, folderRecord)
	again := runSmall(t, root, false)
	equal(t, again["red_before_green"], true)
	stamps, passed = historyOf(t, root, folderRecord)
	equal(t, stamps, []string{redAt, greenAt}, "each earlier run once")
	equal(t, passed, []bool{false, true})
}

// The certification's case: RED and GREEN under the git common dir, then a
// folder whose record an earlier binary wrote with no history. Status reads
// the RED from the git common dir; the next run carries it, once.
func TestFolderRecordWithoutHistoryReadsRedBeforeIt(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	runSmall(t, root, true)
	runSmall(t, root, false)
	stamps, _ := historyOf(t, root, smallRecord)
	commonAt := timestampOf(t, root, smallRecord)
	equal(t, len(stamps), 1, "the RED is the common record's history")
	redAt := stamps[0]

	folderAt := "2099-01-01T00:00:00.000Z"
	support.Write(t, root, map[string]string{
		"openspec/changes/" + smallChange + "/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n",
		folderRecord: `{"schema":"jhon.execution-record/v2","timestamp":"` + folderAt + `","complete":true,"scope":"full","base":null,"scope_reasons":[],"selection":[],"timeout_minutes":20,"candidate_commit":"","candidate_tree":"","working_tree_clean":false,"after":null,"dirty":false,"executions":[{"project":"a","command":"true","cwd":"a","exit_code":0,"status":"pass","duration_ms":1}]}` + "\n",
	})
	equal(t, statusRedGreen(t, root), true, "status's tests")

	next := runSmall(t, root, false)
	equal(t, next["red_before_green"], true)
	stamps, passed := historyOf(t, root, folderRecord)
	equal(t, stamps, []string{redAt, commonAt, folderAt}, "each earlier run once, oldest first")
	equal(t, passed, []bool{false, true, true})
}

// Passes alone under the git common dir are no RED once the folder comes.
func TestFolderCarriesNoRedFromPasses(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	runSmall(t, root, false)
	support.Write(t, root, map[string]string{"openspec/changes/" + smallChange + "/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n"})
	green := runSmall(t, root, false)
	equal(t, green["red_before_green"], false)
	equal(t, statusRedGreen(t, root), false)
	_, passed := historyOf(t, root, folderRecord)
	equal(t, passed, []bool{true}, "the earlier pass is kept")
}

// A folder change on a branch that does not name it carries no RED its
// name recorded on another branch under the git common dir.
func TestFolderOnAnotherBranchCarriesNoRedOfItsName(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	runSmall(t, root, true)
	support.Git(t, root, "checkout", "-q", "-b", "feat/other")
	support.Write(t, root, map[string]string{"openspec/changes/" + smallChange + "/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n"})
	green := runSmall(t, root, false)
	equal(t, green["red_before_green"], false, "a RED from another branch")
	equal(t, statusRedGreen(t, root), false)
}
