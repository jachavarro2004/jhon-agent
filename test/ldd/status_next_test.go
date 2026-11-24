package ldd

import (
	"regexp"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// A change whose record is all it holds has applied nothing (T96, L537):
// status suggests applying it, never archiving it. A change that needs no
// tests and has applied its work is archived as before.

const recordOnlyNote = "nothing applied yet for c1: no file outside openspec/ differs from the base or is new; apply the change (one that alters behaviour starts with its failing test)"

// recordRepo is a committed root with project `a` on branch feat/c1, its
// folder openspec/changes/c1 holding an exploration and a proposal, and
// files outside it as given.
func recordRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := support.GitRepo(t, temp(t))
	support.Write(t, root, map[string]string{
		"a/go.mod": "module example.com/a\n", "a/a.go": "package a\n",
		"openspec/config.yaml": "test_command_override:\n  a: \"true\"\n",
	})
	commit(t, root, "base")
	support.Git(t, root, "checkout", "-q", "-b", "feat/c1")
	support.Write(t, root, map[string]string{
		"openspec/changes/c1/exploration.md": "# x\n",
		"openspec/changes/c1/proposal.md":    "## Why\n",
	})
	support.Write(t, root, files)
	return root
}

func TestRecordOnlyChangeIsNotArchived(t *testing.T) {
	t.Parallel()
	root := recordRepo(t, nil)
	r := jhonGit(t, root, "status", "c1", "--json")
	equal(t, r.Code, 0, r.Stderr)
	s := object(t, r.Stdout)
	equal(t, s["next"], "apply", s["notes"])
	equal(t, anyMatch(s["notes"], "^"+regexp.QuoteMeta(recordOnlyNote)+"$"), true, s["notes"])
	h := jhonGit(t, root, "status", "c1")
	equal(t, h.Code, 0, h.Stderr)
	match(t, h.Stdout, `(?m)^next        apply  \(suggestion\)$`)
	match(t, h.Stdout, `(?m)^  - `+regexp.QuoteMeta(recordOnlyNote)+`$`)
}

// A docs-only change, applied, needs no tests: archived as before, with no
// RED before GREEN, while its diff is not empty (T223, L4).
func TestAppliedDocsOnlyChangeIsArchived(t *testing.T) {
	t.Parallel()
	root := recordRepo(t, map[string]string{"docs/guide.md": "# Guide\n\nHow to use it.\n"})
	r := jhonGit(t, root, "status", "c1", "--json")
	equal(t, r.Code, 0, r.Stderr)
	s := object(t, r.Stdout)
	equal(t, s["route"].(map[string]any)["behaviour"], false, s["route"])
	equal(t, s["next"], "archive", s["notes"])
	equal(t, anyMatch(s["notes"], "^nothing applied yet"), false, s["notes"])
}

// A branch that would name a folderless change with no files of its own
// yet: status says so, as `jhon executions run` does (T84).
func TestStatusSaysFolderlessPending(t *testing.T) {
	t.Parallel()
	_, wt := shippingWorktree(t)
	note := "branch feat/shipping-cost names change feat-shipping-cost, which has no changes yet: no file outside ldd/ differs from the base or is new; write the failing test first (a new untracked file counts), then run `jhon executions run`" // T146h L4: the bare run
	r := jhonGit(t, wt, "status", "--json")
	equal(t, r.Code, 0, r.Stderr)
	s := object(t, r.Stdout)
	equal(t, s["change"], nil, s)
	equal(t, s["next"], "start-change", s)
	equal(t, anyMatch(s["notes"], "^"+regexp.QuoteMeta(note)+"$"), true, s["notes"])
	h := jhonGit(t, wt, "status")
	equal(t, h.Code, 0, h.Stderr)
	match(t, h.Stdout, `(?m)^  - `+regexp.QuoteMeta(note)+`$`)
	// The default branch says nothing of it.
	root, _ := shippingWorktree(t)
	support.Write(t, root, map[string]string{"ldd/config.yaml": "test_command_override:\n  a: \"true\"\n"})
	noMatch(t, jhonGit(t, root, "status").Stdout, "has no changes yet")
}
