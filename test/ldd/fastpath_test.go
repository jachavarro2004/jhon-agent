package ldd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

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
	// On the default branch, the commands a small change starts with (T146g G1).
	match(t, r.Stderr, "^jhon executions: no change named, and jhon status selects none on this branch: no active change under openspec/changes/: a small change [^\n]*`git switch -c <name>`, then `jhon executions run`[^\n]*\n$")
	equal(t, exists(filepath.Join(root, ".git/jhon")), false)
}

// Status prints the exact next command, the name and the base filled in.
func TestStatusPrintsTheNextCommand(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	h := jhonGit(t, root, "status")
	equal(t, h.Code, 0, h.Stderr)
	// T146h L4: a small change's run is the bare one.
	match(t, h.Stdout, `(?m)^next        apply  \(suggestion\)\ncommand     jhon executions run$`)
	b := jhonGit(t, root, "status", "--brief")
	match(t, b.Stdout, `(?m)^command     jhon executions run$`)

	redThenGreen(t, root)
	h = jhonGit(t, root, "status")
	match(t, h.Stdout, `(?m)^next        merge  \(suggestion\)\ncommand     jhon deliver check feat-small-fix  \(once the tested tree is committed\)$`)
	commit(t, root, "small fix")
	h = jhonGit(t, root, "status")
	match(t, h.Stdout, `(?m)^command     jhon deliver check feat-small-fix$`)
}

// smallNote is what status tells a small change.
const smallNote = "small change (no folder, one component, under the advisory size, not high risk): it keeps no tasks.md, archive, light verify or todo list, and saves no memory or session summary; the run that records its GREEN is its delivery evidence: commit the tree it tested, then `jhon deliver check feat-small-fix`, with no re-run"

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

// T146d (L1223-L1224): the committed-tree acceptance trusts only a run that
// passed, finished, and saw its working tree unchanged from its start to its
// end. The working tree is read again after the run: one edited while the
// run went on is marked dirty, so its start-of-run tree is no evidence.

// evidenceNote is the note a run accepted for its committed tree carries.
const evidenceNote = `^the last test run tested this tree before its commit`

// whileItRuns runs the small change's affected run in the background, calls
// during at about 1.2 s (the test command sleeps 3 s first, so the run's
// start is read before and its test after), and returns the run's result.
func whileItRuns(t *testing.T, root, test string, during func()) support.Result {
	t.Helper()
	// The test marks its start outside the repository, so during() runs
	// after the run took its first snapshot however slow the machine is.
	started := filepath.Join(t.TempDir(), "started")
	support.Write(t, root, map[string]string{"openspec/config.yaml": "test_command_override:\n  a: \"touch " + started + " && sleep 3 && " + test + "\"\n" +
		"lint_command_override:\n  a: \"true\"\nformat_command_override:\n  a: \"true\"\n"})
	support.Bin(t) // a build failure fails here, not in the goroutine
	done := make(chan support.Result, 1)
	go func() { done <- jhonGit(t, root, "executions", "run", smallChange, "--affected", "main") }()
	for deadline := time.Now().Add(2 * time.Minute); ; time.Sleep(50 * time.Millisecond) {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the run's test never started")
		}
	}
	during()
	select {
	case r := <-done:
		return r
	case <-time.After(2 * time.Minute):
		t.Fatal("the run did not end")
	}
	return support.Result{}
}

// refused is a deliver check that refuses with the given reason and gives
// no evidence note.
func refused(t *testing.T, root, reason string, msg ...any) {
	t.Helper()
	d := jhonGit(t, root, "deliver", "check", smallChange, "--json")
	equal(t, d.Code, 1, append(msg, d.Out())...)
	c := object(t, d.Stdout)
	equal(t, c["ready"], false, msg...)
	equal(t, anyMatch(c["notes"], evidenceNote), false, append(msg, c["notes"])...)
	if reason != "" {
		equal(t, anyMatch(c["reasons"], reason), true, append(msg, c["reasons"])...)
	}
}

// A file changed while the run went on (its test passed on the change) and
// put back before the commit: the committed tree is the start's, but the
// run did not test it alone. Refused.
func TestSmallChangeRefusesARunEditedWhileItRan(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	support.Write(t, root, map[string]string{"flag": "A\n"})
	r := whileItRuns(t, root, "grep -qx B ../flag", func() { support.Write(t, root, map[string]string{"flag": "B\n"}) })
	equal(t, r.Code, 0, r.Out())
	support.Write(t, root, map[string]string{"flag": "A\n"})
	commit(t, root, "small fix")
	refused(t, root, `^the candidate changed during the last test run`)
}

// An untracked file that appeared while the run went on (its test needs it)
// and was removed before the commit: refused the same way.
func TestSmallChangeRefusesAnUntrackedFileRemovedBeforeCommit(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	r := whileItRuns(t, root, "test -f ../untracked", func() { support.Write(t, root, map[string]string{"untracked": "x\n"}) })
	equal(t, r.Code, 0, r.Out())
	os.Remove(filepath.Join(root, "untracked"))
	commit(t, root, "small fix")
	refused(t, root, `^the candidate changed during the last test run`)
}

// HEAD moved while the run went on (its tree committed mid-run): what was
// tested is not one revision. Refused.
func TestSmallChangeRefusesARunWhoseHeadMovedWhileItRan(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	r := whileItRuns(t, root, "true", func() { commitNow(t, root, "small fix, mid-run") })
	equal(t, r.Code, 0, r.Out())
	// The premise held: the run saw HEAD move (else the test lost its race).
	equal(t, object(t, support.Read(t, filepath.Join(root, smallRecord)))["dirty"], true, "the record of a run whose HEAD moved")
	commit(t, root, "small fix")
	refused(t, root, `^the candidate changed during the last test run`)
}

// commitNow commits everything at once, outside support.Run's process slots,
// which the runs whileItRuns starts may all hold: a mid-run commit that waited
// for a slot would land after the run ended.
func commitNow(t *testing.T, root, message string) {
	t.Helper()
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "--allow-empty", "-m", message}} {
		c := exec.Command("git", append([]string{"-c", "user.name=jhon-test", "-c", "user.email=jhon-test@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
		c.Dir, c.Env = root, support.Env()
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
}

// T146f (L1236): an earlier jhon's record on the very tree later committed
// is reused by nothing: the re-run on the clean tree runs the test again.
func TestSmallChangeOldRecordOnTheCommittedTreeReusesNothing(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	support.Write(t, root, map[string]string{"flag": "A\n"})
	r := whileItRuns(t, root, "grep -qx B ../flag", func() { support.Write(t, root, map[string]string{"flag": "B\n"}) })
	equal(t, r.Code, 0, r.Out())
	rec := object(t, support.Read(t, filepath.Join(root, smallRecord)))
	delete(rec, "after_working_tree") // as jhon 0.3.1-0.3.5 wrote it
	rec["dirty"] = false
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	support.Write(t, root, map[string]string{smallRecord: string(b), "flag": "A\n"})
	commit(t, root, "small fix")
	again := jhonGit(t, root, "executions", "run", smallChange, "--affected", "main")
	equal(t, strings.Contains(again.Stdout, "(reused:"), false, again.Out())
	equal(t, again.Code, 1, again.Out())
	refused(t, root, `^the last test run did not pass`)
}

// A failed run, a partial record and an old record with no working_tree are
// never the committed tree's evidence: refused, with no evidence note.
func TestSmallChangeRefusesFailedPartialAndOldRuns(t *testing.T) {
	t.Parallel()
	failed := smallRepo(t)
	support.Write(t, failed, map[string]string{"red": ""})
	equal(t, jhonGit(t, failed, "executions", "run", smallChange, "--affected", "main").Code, 1, "RED")
	commit(t, failed, "a failing test committed")
	refused(t, failed, `^the last test run did not pass`, "failed")

	edited := func(name string, edit func(map[string]any)) string {
		root := smallRepo(t)
		equal(t, jhonGit(t, root, "executions", "run", smallChange, "--affected", "main").Code, 0, name)
		commit(t, root, "small fix")
		rec := object(t, support.Read(t, filepath.Join(root, smallRecord)))
		edit(rec)
		b, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		support.Write(t, root, map[string]string{smallRecord: string(b)})
		return root
	}
	partial := edited("partial", func(r map[string]any) { r["complete"] = false })
	refused(t, partial, `^the last test run did not finish`, "partial")
	old := edited("old", func(r map[string]any) { delete(r, "working_tree") })
	refused(t, old, `^the last test run started on a working tree that was not clean`, "old")
}

// T146e (L1228-L1229): a record an earlier binary wrote (0.3.1-0.3.5) keeps
// working_tree but not after_working_tree, so it cannot show its working tree
// unchanged to its end: never the committed tree's evidence. The refusal
// names the run that writes a record which can.
func TestSmallChangeRefusesARecordWithNoAfterWorkingTree(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	equal(t, jhonGit(t, root, "executions", "run", smallChange, "--affected", "main").Code, 0, "GREEN")
	commit(t, root, "small fix")
	rec := object(t, support.Read(t, filepath.Join(root, smallRecord)))
	equal(t, rec["after_working_tree"], rec["working_tree"], "the record names both")
	delete(rec, "after_working_tree")
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	support.Write(t, root, map[string]string{smallRecord: string(b)})
	refused(t, root, `^the last test run recorded no working tree at its end.*run the tests again, `+
		regexp.QuoteMeta("`jhon executions run "+smallChange+" --affected <base>`"))
}

// T146f (L1232-L1233): following that refusal, a re-run reuses nothing of
// such a record: it cannot show the tree it started on is the one its
// passes saw. Its test runs again, on the start's bytes, and fails.
func TestSmallChangeNeverReusesARecordWithNoAfterWorkingTree(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	support.Write(t, root, map[string]string{"flag": "A\n"})
	r := whileItRuns(t, root, "grep -qx B ../flag", func() { support.Write(t, root, map[string]string{"flag": "B\n"}) })
	equal(t, r.Code, 0, r.Out())
	rec := object(t, support.Read(t, filepath.Join(root, smallRecord)))
	delete(rec, "after_working_tree")
	rec["dirty"] = false
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	support.Write(t, root, map[string]string{smallRecord: string(b), "flag": "A\n"})
	again := jhonGit(t, root, "executions", "run", smallChange, "--affected", "main")
	equal(t, strings.Contains(again.Stdout, "(reused:"), false, again.Out())
	equal(t, again.Code, 1, again.Out())
	commit(t, root, "small fix")
	refused(t, root, `^the last test run did not pass`)
}

// A unit's run (`--affected` with no base) on the uncommitted tree, then
// committed: refused as a unit's, and with no evidence note.
func TestSmallChangeUnitRunGivesNoEvidenceNote(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	equal(t, jhonGit(t, root, "executions", "run", smallChange, "--affected", "main").Code, 0, "GREEN")
	support.Write(t, root, map[string]string{"extra.txt": "x\n"})
	equal(t, jhonGit(t, root, "executions", "run", smallChange, "--affected").Code, 0, "unit")
	equal(t, object(t, support.Read(t, filepath.Join(root, smallRecord)))["unit"], true, "a unit's record")
	commit(t, root, "small fix")
	refused(t, root, `^the last test run was a unit's`)
}
