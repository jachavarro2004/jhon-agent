package ldd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// The one delivery verdict (T154, L1394): a commit that closes a slice
// (SliceCommitRefusal), `jhon deliver check` (checkDelivery) and a PR under
// `delivery: change` (route-guard) ask the same two questions through these
// two functions, so one tree gets one answer, in the same words: is this the
// tree the change's last run tested (TestedTree), and is every slice done
// (SlicesLeft).

// Tested is TestedTree's answer: Why is "" when the tree is the tested one;
// Full is whether only a full run clears it (the last run holds no
// execution); Paths are the paths outside the change's record that changed
// after the run.
type Tested struct {
	Why   string
	Full  bool
	Paths []string
}

// TestedTree is whether tree, the tree a delivery carries in root (the
// working tree a commit records, HEAD's tree for deliver check), is the one
// change's last run tested: a run that passed complete and undisturbed, not
// a unit's, its working tree the same at its start and its end, and no path
// outside the change's record changed since (outsideRecord).
func TestedTree(root, change, tree string) Tested {
	run, err := execution.ReadLastRun(root, change)
	if err != nil {
		run = nil
	}
	return testedTree(root, change, run, tree)
}

func testedTree(root, change string, run *execution.LastRun, tree string) Tested {
	switch {
	case run == nil:
		return Tested{Why: "the change has no recorded test run"}
	case run.Empty():
		return Tested{Why: "its last test run holds no execution", Full: true}
	case !run.Passed || run.Dirty:
		return Tested{Why: "its last test run did not pass complete and undisturbed"}
	case run.Unit:
		return Tested{Why: "its last test run was a unit's (`--affected` with no base), which runs only what changed since the change's last passing run"}
	case !isSha(run.WorkingTree):
		return Tested{Why: "its last test run records no working tree"}
	case run.AfterWorkingTree != run.WorkingTree:
		// Only a run whose working tree read again at its end is the start's
		// tested one tree (T146f): a record an earlier binary wrote has none.
		return Tested{Why: "its last test run recorded no working tree at its end, or another than at its start"}
	case tree == "":
		return Tested{Why: "the working tree cannot be read"}
	}
	paths, ok := outsideRecord(root, change, run.WorkingTree, tree)
	if !ok {
		return Tested{Why: "the tree its last test run tested is not in the repository"}
	}
	if len(paths) == 0 {
		return Tested{}
	}
	return Tested{Why: fmt.Sprintf("%d file(s) changed after its last test run (%s)", len(paths), strings.Join(paths[:min(5, len(paths))], ", ")), Paths: paths}
}

// outsideRecord is the paths that differ between trees a and b in root, but
// for change's record files, its judgments/ and the rest of its own folder,
// written after the run (T150); ok is false when a tree does not resolve.
func outsideRecord(root, change, a, b string) ([]string, bool) {
	diff, ok := vcs.TreeDiff(root, a, b)
	exempt := execution.ChangeRecordPaths(root, change)
	folder := execution.ChangeDirInRepo(root, change) + "/"
	judged := layout.For(root).Artifact(change, "judgments") + "/"
	outside := []string{}
	for _, p := range diff {
		own := change != "" && strings.HasPrefix(p, folder)
		if !exempt[p] && !own && !strings.HasPrefix(p, judged) && !strings.Contains(p, "/"+judged) {
			outside = append(outside, p)
		}
	}
	return outside, ok
}

// SlicesLeft are why change in root is not done under `delivery: change`,
// which merges the change once, after its last slice: its unchecked tasks,
// then its slices outlined with no task yet (T127); none when every slice is
// tasked and done.
func SlicesLeft(root, change string) []string {
	lay := layout.For(root)
	var src *string
	if b, err := os.ReadFile(filepath.Join(root, execution.ChangesOf(root), change, lay.Tasks())); err == nil {
		text := string(b)
		src = &text
	}
	t := ProgressOf(lay, src)
	const lead = "delivery: change in openspec/config.yaml merges the change once, after its last slice; "
	const tail = ": commit each slice on the change's branch and merge when every slice is done"
	var out []string
	if t.Pending > 0 {
		out = append(out, fmt.Sprintf(lead+"%d task(s) of tasks.md are unchecked (%s)"+tail, t.Pending, listed(t.Unchecked)))
	}
	if outlined := t.Outlined(); len(outlined) > 0 {
		out = append(out, fmt.Sprintf(lead+"%d slice(s) of tasks.md are outlined with no tasks yet (%s)"+tail, len(outlined), listed(outlined)))
	}
	return out
}
