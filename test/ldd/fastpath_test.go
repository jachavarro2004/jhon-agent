package ldd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T146 (L1211, FA1-FA2): a small task takes no wasted call. `jhon executions
// run` with no change name runs the change the branch selects and says which
// on its first line; `jhon status` prints the exact next command, the
// change's name filled in; a small change (no folder, one component, under
// the line budget, not high risk) is told to keep no bookkeeping, and the one
// run that recorded its GREEN is its delivery evidence once that tree is
// committed: `jhon deliver check` needs no re-run.

const smallFirstLine = "jhon executions: change feat-small-fix (branch feat/small-fix)"

// The change is the branch's, folderless included, with or without a bare
// `--affected`; the first line names it, and the record is that change's.
func TestExecutionsRunWithNoNameRunsTheBranchsChange(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"executions", "run"}, {"executions", "run", "--affected"}} {
		root := smallRepo(t)
		r := jhonGit(t, root, args...)
		equal(t, r.Code, 0, args, r.Out())
		equal(t, strings.SplitN(r.Stdout, "\n", 2)[0], smallFirstLine, args)
		equal(t, exists(filepath.Join(root, smallRecord)), true, args)
	}
	// With --json, stdout stays one JSON document: the line goes to stderr.
	root := smallRepo(t)
	j := jhonGit(t, root, "executions", "run", "--json")
	equal(t, j.Code, 0, j.Out())
	equal(t, object(t, j.Stdout)["record"], smallRecord)
	equal(t, strings.SplitN(j.Stderr, "\n", 2)[0], smallFirstLine)

	// A change with a folder that the branch names is taken the same way.
	folder := recordRepo(t, map[string]string{"a/b.go": "package a\n\nvar B = 1\n", "a/b_test.go": "package a\n"})
	f := jhonGit(t, folder, "executions", "run")
	equal(t, f.Code, 0, f.Out())
	equal(t, strings.SplitN(f.Stdout, "\n", 2)[0], "jhon executions: change c1 (branch feat/c1)")
	equal(t, exists(filepath.Join(folder, "openspec/changes/c1/execution-record.json")), true)
}

// No change is selected: nothing runs, and the refusal says how to name one.
func TestExecutionsRunWithNoNameAndNoChange(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	support.Git(t, root, "checkout", "-q", "main")
	r := jhonGit(t, root, "executions", "run")
	equal(t, r.Code, 2, r.Out())
	equal(t, r.Stdout, "")
	match(t, r.Stderr, `^jhon executions: no change named, and jhon status selects none on this branch \(no active change under openspec/changes/;[^\n]*\): name it: jhon executions run <change>\n`)
	equal(t, exists(filepath.Join(root, ".git/jhon")), false)
}

// Status prints the exact next command, the name and the base filled in.
func TestStatusPrintsTheNextCommand(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	h := jhonGit(t, root, "status")
	equal(t, h.Code, 0, h.Stderr)
	match(t, h.Stdout, `(?m)^next        apply  \(suggestion\)\ncommand     jhon executions run feat-small-fix --affected main$`)
	b := jhonGit(t, root, "status", "--brief")
	match(t, b.Stdout, `(?m)^command     jhon executions run feat-small-fix --affected main$`)

	redThenGreen(t, root)
	h = jhonGit(t, root, "status")
	match(t, h.Stdout, `(?m)^next        merge  \(suggestion\)\ncommand     jhon deliver check feat-small-fix  \(once the tested tree is committed\)$`)
	commit(t, root, "small fix")
	h = jhonGit(t, root, "status")
	match(t, h.Stdout, `(?m)^command     jhon deliver check feat-small-fix$`)
}

// smallNote is what status tells a small change.
const smallNote = "small change (no folder, one component, under the advisory size, not high risk): write no tasks.md, save no memory or session summary, keep no todo list; the run that records its GREEN is its delivery evidence: commit the tree it tested and run `jhon deliver check feat-small-fix`, with no re-run"

// A small change is told the fast path; a change over it is not.
func TestStatusTellsASmallChangeTheFastPath(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	s := object(t, jhonGit(t, root, "status", "--json").Stdout)
	equal(t, anyMatch(s["notes"], "^"+regexp.QuoteMeta(smallNote)+"$"), true, s["notes"])
	match(t, jhonGit(t, root, "status").Stdout, `(?m)^  - `+regexp.QuoteMeta(smallNote)+`$`)

	// High risk (untested code) needs tasks.md: not small.
	high := smallRepo(t)
	os.Remove(filepath.Join(high, "a/b_test.go"))
	h := object(t, jhonGit(t, high, "status", "--json").Stdout)
	equal(t, anyMatch(h["notes"], "^small change"), false, h["notes"])
	// A change with a folder is not told it either.
	folder := recordRepo(t, map[string]string{"a/b.go": "package a\n\nvar B = 1\n", "a/b_test.go": "package a\n"})
	f := object(t, jhonGit(t, folder, "status", "c1", "--json").Stdout)
	equal(t, anyMatch(f["notes"], "^small change"), false, f["notes"])
}

// redThenGreen records the RED run (the tests fail while `red` exists) and
// the GREEN run of the small change, on its uncommitted tree.
func redThenGreen(t *testing.T, root string) {
	t.Helper()
	support.Write(t, root, map[string]string{"red": ""})
	equal(t, jhonGit(t, root, "executions", "run", smallChange, "--affected", "main").Code, 1, "RED")
	os.Remove(filepath.Join(root, "red"))
	equal(t, jhonGit(t, root, "executions", "run", smallChange, "--affected", "main").Code, 0, "GREEN")
}

// One recorded run is the small change's GREEN and its delivery evidence:
// its tree committed, the check is ready with no re-run, and names the
// tree it tested. A commit that is not that tree is still refused.
func TestSmallChangeDeliversOnItsGreenRun(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	redThenGreen(t, root)
	commit(t, root, "small fix")
	d := jhonGit(t, root, "deliver", "check", smallChange, "--json")
	equal(t, d.Code, 0, d.Out())
	c := object(t, d.Stdout)
	equal(t, c["ready"], true, c["reasons"])
	equal(t, c["tested_tree"], c["head_tree"])
	equal(t, anyMatch(c["notes"], `^the last test run tested this tree before its commit`), true, c["notes"])
	equal(t, exists(filepath.Join(root, ".git/jhon/changes", smallChange, "delivery.json")), true)

	// The committed tree differs from the tested one: refused as before.
	other := smallRepo(t)
	redThenGreen(t, other)
	support.Write(t, other, map[string]string{"a/b.go": "package a\n\nvar B = 2\n"})
	commit(t, other, "small fix, edited after its run")
	o := jhonGit(t, other, "deliver", "check", smallChange, "--json")
	equal(t, o.Code, 1, o.Out())
	equal(t, anyMatch(object(t, o.Stdout)["reasons"], `^the last test run started on a working tree that was not clean`), true, o.Stdout)
}
