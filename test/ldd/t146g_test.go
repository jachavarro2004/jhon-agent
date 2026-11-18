package ldd

import (
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T146g (L1273, L1274): the F5 bench measured small tasks at 20-28 turns
// against 5-8 without Jhon. G1: on the default branch with no change, status
// and a bare `jhon executions run` name the exact commands a small change
// starts with, never `start-change`, which is no command. G2: a small change
// is delivered as GREEN run, commit, `jhon deliver check`; a check before the
// commit says so, never to run the tests again. G3: a small change keeps no
// tasks.md, archive, light verify or todo list.

// smallStart is the command line status prints on the default branch.
const smallStart = "git switch -c <name>, then jhon executions run"

// smallStartNote is the start of the note that says it.
const smallStartNote = "no active change under openspec/changes/: a small change (one component, under the advisory size, not high risk) starts on a branch of its own: `git switch -c <name>`, then `jhon executions run` while its new test fails (RED) and again once it passes (GREEN); a change that is not small (several components, over the advisory size, or high risk): before you finish, start one (tasks.md) and record a run (`jhon executions run`)"

// onMain is smallRepo back on main: no change is selected.
func onMain(t *testing.T) string {
	t.Helper()
	root := smallRepo(t)
	support.Git(t, root, "checkout", "-q", "main")
	return root
}

func TestT146gStatusOnTheDefaultBranchNamesTheSmallChangeCommands(t *testing.T) {
	t.Parallel()
	root := onMain(t)
	h := jhonGit(t, root, "status")
	equal(t, h.Code, 0, h.Stderr)
	match(t, h.Stdout, `(?m)^command     `+regexp.QuoteMeta(smallStart)+`$`)
	match(t, h.Stdout, `(?m)^  - `+regexp.QuoteMeta(smallStartNote)+`$`)
	noMatch(t, h.Stdout, `start-change`)
	b := jhonGit(t, root, "status", "--brief")
	equal(t, b.Code, 0, b.Stderr)
	match(t, b.Stdout, `(?m)^command     `+regexp.QuoteMeta(smallStart)+`$`)
	noMatch(t, b.Stdout, `start-change`)
	// The JSON keeps its vocabulary's next action; its note names the commands.
	s := object(t, jhonGit(t, root, "status", "--json").Stdout)
	equal(t, s["next"], "start-change")
	equal(t, anyMatch(s["notes"], "^"+regexp.QuoteMeta(smallStartNote)+"$"), true, s["notes"])
}

func TestT146gExecutionsRunOnTheDefaultBranchNamesTheSmallChangeCommands(t *testing.T) {
	t.Parallel()
	root := onMain(t)
	r := jhonGit(t, root, "executions", "run")
	equal(t, r.Code, 2, r.Out())
	equal(t, r.Stdout, "")
	equal(t, r.Stderr, "jhon executions: no change named, and jhon status selects none on this branch: "+smallStartNote+"\n")
}

func TestT146gHelpNamesNoStartChange(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"--help"}, {"status", "--help"}} {
		r := jhonGit(t, t.TempDir(), args...)
		equal(t, r.Code, 0, args, r.Out())
		match(t, r.Stdout, `jhon status`, args)
		noMatch(t, r.Stdout, `start-change`, args)
	}
}

// commitThenCheck is the reason a check before the commit gives when the
// last GREEN run tested the working tree as it is.
const commitThenCheck = "the working tree is not clean, and the last test run passed on it as it is: commit it, then `jhon deliver check feat-small-fix`, with no re-run"

func TestT146gDeliverCheckBeforeTheCommitSaysCommitThenCheck(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	redThenGreen(t, root)
	d := jhonGit(t, root, "deliver", "check", smallChange, "--json")
	equal(t, d.Code, 1, d.Out())
	c := object(t, d.Stdout)
	equal(t, c["reasons"], []any{commitThenCheck})
	h := jhonGit(t, root, "deliver", "check", smallChange)
	noMatch(t, h.Stdout+h.Stderr, "jhon executions run", "no re-run asked")
	commit(t, root, "small fix")
	equal(t, jhonGit(t, root, "deliver", "check", smallChange).Code, 0, "ready once committed")

	// Edited after its GREEN run: the run did not test this tree.
	edited := smallRepo(t)
	redThenGreen(t, edited)
	support.Write(t, edited, map[string]string{"a/b.go": "package a\n\nvar B = 2\n"})
	e := object(t, jhonGit(t, edited, "deliver", "check", smallChange, "--json").Stdout)
	equal(t, anyMatch(e["reasons"], "^"+regexp.QuoteMeta(commitThenCheck)), false, e["reasons"])
	equal(t, anyMatch(e["reasons"], `order: a passing run \(`+"`jhon executions run`"+`\), commit the tree it tested, then `+"`jhon deliver check`"), true, e["reasons"])
	for _, r := range e["reasons"].([]any) {
		if strings.Contains(r.(string), "on the clean tree") {
			t.Errorf("a reason asks for a run after the commit: %s", r)
		}
	}
}

// smallKeepsNo is the words every text that exempts a small change shares.
const smallKeepsNo = "keeps no tasks.md, archive, light verify or todo list"

func TestT146gSmallChangeKeepsNoLightVerify(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	redGreenRuns(t, root, smallChange)
	s := statusJSON(t, root, smallChange)
	equal(t, anyMatch(s["notes"], `^light verify due`), false, s["notes"])
	equal(t, anyMatch(s["notes"], `^small change .*`+regexp.QuoteMeta(smallKeepsNo)), true, s["notes"])
}
