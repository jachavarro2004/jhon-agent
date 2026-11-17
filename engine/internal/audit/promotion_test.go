package audit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A record of an uncommitted tree passes as the run of the commit that then
// committed that tree (committedAs) only for a delivery of that commit: a
// push of its sha, or a merge whose last push was of it (S12, L31).

// slice is a repository whose base commit is tested uncommitted with the
// slice's change: base, the tree that was tested, and the change's directory
// holding a record of that run. The slice is not committed yet.
func slice(t *testing.T) (repo, base, tested, dir string) {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	must(t, err)
	gitT(t, repo, "init", "-q")
	must(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644))
	gitT(t, repo, "add", "-A")
	gitT(t, repo, "commit", "-qm", "base")
	base, baseTree := gitOut(t, repo, "rev-parse", "HEAD"), gitOut(t, repo, "rev-parse", "HEAD^{tree}")
	must(t, os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n\nvar X = 1\n"), 0o644))
	gitT(t, repo, "add", "-A")
	tested = gitOut(t, repo, "write-tree")
	dir = filepath.Join(repo, "openspec", "changes", chg)
	must(t, os.MkdirAll(dir, 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "execution-record.json"), []byte(`{"schema":"jhon.execution-record/v2","timestamp":"2026-09-30T09:00:00.000Z","complete":true,"candidate_commit":"`+base+
		`","candidate_tree":"`+baseTree+`","working_tree_clean":false,"working_tree":"`+tested+`","dirty":false,"scope":"full","executions":[{"project":".","command":"go test ./...","cwd":".","exit_code":0,"status":"pass","duration_ms":1000}]}`), 0o644))
	return repo, base, tested, dir
}

func TestPushedBaseIsNotPromotedByALaterCommit(t *testing.T) {
	repo, base, _, _ := slice(t)
	d := delegate("d01", "general", chg, pushOf(base))
	d.root = repo
	// The tested tree is committed as base's child after base was pushed untested.
	gitT(t, repo, "commit", "-qm", "slice 1")
	r := auditOf(t, user("go"), d)
	f := of(r.Findings, "delivered-untested")
	if len(f) != 1 || f[0].Severity != "violation" {
		t.Fatalf("the push of the untested base = %+v", f)
	}
	if r.OK {
		t.Errorf("the audit is OK with an untested push")
	}
}

// A git that does not answer in time promotes nothing, and is not waited for.
func TestCommittedAsGivesUpOnASlowGit(t *testing.T) {
	bin := t.TempDir()
	must(t, os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nsleep 3\necho 'n t c'\n"), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	was := committedAsTimeout
	committedAsTimeout = 100 * time.Millisecond
	defer func() { committedAsTimeout = was }()
	began := time.Now()
	eq(t, "the next commit", committedAs(t.TempDir(), "c", "t"), "")
	if d := time.Since(began); d > 2*time.Second {
		t.Errorf("committedAs waited %s for git", d)
	}
}

func TestAMergeIsPromotedOnlyAfterThePushOfTheNextCommit(t *testing.T) {
	repo, base, _, _ := slice(t)
	gitT(t, repo, "commit", "-qm", "slice 1")
	next := gitOut(t, repo, "rev-parse", "HEAD")
	merge := bash("gh pr merge 12 --squash", "Merged")
	mergeOf := func(fs []Finding) []Finding {
		var out []Finding
		for _, f := range fs {
			if f.Evidence == "gh pr merge 12 --squash" {
				out = append(out, f)
			}
		}
		return out
	}

	// As before: the merged revision is not shown, so it is at most a suspect.
	d := delegate("d01", "general", chg, pushOf(next), merge)
	d.root = repo
	if f := mergeOf(of(auditOf(t, user("go"), d).Findings, "delivered-untested")); len(f) > 1 || len(f) == 1 && f[0].Severity != "suspect" {
		t.Errorf("a merge after the push of the next commit = %+v", f)
	}

	d = delegate("d01", "general", chg, pushOf(base), merge)
	d.root = repo
	if f := mergeOf(of(auditOf(t, user("go"), d).Findings, "delivered-untested")); len(f) != 1 || f[0].Severity != "violation" {
		t.Errorf("a merge after the push of the untested base = %+v", f)
	}
}
