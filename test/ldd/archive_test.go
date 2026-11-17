// Tests for `jhon archive`: the mechanical move of a change directory into
// the archive (the archive step, step 4).
//
// The method is decided before anything moves: a change directory holding a
// tracked file moves with `git mv`, one holding none moves with `mv`. A failed
// `git mv` is reported with git's own message and never falls back. Every
// project is a temporary git repository with one commit, built outside any
// other repository, and the command runs with no inherited GIT_* variable.
package ldd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

const (
	archiveChange = "add-login"
	archiveDate   = "2026-09-23"
)

var (
	archiveSource = filepath.Join("openspec", "changes", archiveChange)
	archiveDest   = filepath.Join("openspec", "changes", "archive", archiveDate+"-"+archiveChange)
	archiveFiles  = map[string]string{
		"proposal.md":        "# Proposal\n",
		"tasks.md":           "- [x] 1.1 done\n",
		"specs/auth/spec.md": "# Auth\n",
	}
)

// archiveProject is a git repository with one commit and the change
// directory written, untracked.
func archiveProject(t *testing.T) string {
	t.Helper()
	root := support.GitRepo(t, temp(t))
	support.Write(t, filepath.Join(root, archiveSource), archiveFiles)
	return root
}

func archiveMove(t *testing.T, root string, args ...string) support.Result {
	t.Helper()
	return support.Jhon(t, root, support.Env(), append([]string{"archive"}, args...)...)
}

// assertArchived checks that the archived files carry exactly the source
// content.
func assertArchived(t *testing.T, root string) {
	t.Helper()
	equal(t, exists(filepath.Join(root, archiveSource)), false, "the source directory is gone")
	for rel, content := range archiveFiles {
		equal(t, support.Read(t, filepath.Join(root, archiveDest, rel)), content, rel)
	}
}

func commitSource(t *testing.T, root string, paths ...string) {
	t.Helper()
	support.Git(t, root, append([]string{"add"}, paths...)...)
	support.Git(t, root, "commit", "-q", "-m", "change")
}

func TestArchiveTrackedUsesGitMv(t *testing.T) {
	t.Parallel()
	root := archiveProject(t)
	commitSource(t, root, archiveSource)
	r := archiveMove(t, root, archiveChange, archiveDate)
	equal(t, r.Code, 0, r.Out())
	match(t, r.Stdout, `(?m)^method: git-mv$`)
	assertArchived(t, root)
	match(t, support.Git(t, root, "ls-files", "--", archiveDest), `proposal\.md`)
	equal(t, support.Git(t, root, "ls-files", "--", archiveSource), "")
}

func TestArchiveUntrackedUsesMv(t *testing.T) {
	t.Parallel()
	root := archiveProject(t)
	r := archiveMove(t, root, archiveChange, archiveDate)
	equal(t, r.Code, 0, r.Out())
	match(t, r.Stdout, `(?m)^method: mv$`)
	noMatch(t, r.Stderr, `fatal`)
	assertArchived(t, root)
	equal(t, support.Git(t, root, "ls-files", "--", archiveDest), "")
}

func TestArchivePartiallyTracked(t *testing.T) {
	t.Parallel()
	root := archiveProject(t)
	commitSource(t, root, filepath.Join(archiveSource, "proposal.md"))
	r := archiveMove(t, root, archiveChange, archiveDate)
	equal(t, r.Code, 0, r.Out())
	match(t, r.Stdout, `(?m)^method: git-mv$`)
	assertArchived(t, root)
}

func TestArchiveFailedGitMvNeverFallsBack(t *testing.T) {
	t.Parallel()
	root := archiveProject(t)
	commitSource(t, root, archiveSource)
	// A stale index lock makes git refuse to write the index.
	support.Write(t, root, map[string]string{".git/index.lock": ""})
	r := archiveMove(t, root, archiveChange, archiveDate)
	if r.Code == 0 {
		t.Errorf("exit 0: %s", r.Out())
	}
	match(t, r.Stderr, `fatal: Unable to create '.*index\.lock': File exists`)
	noMatch(t, r.Stdout, `(?m)^method: mv$`)
	equal(t, exists(filepath.Join(root, archiveDest)), false, "no fallback moved the directory")
	for rel, content := range archiveFiles {
		equal(t, support.Read(t, filepath.Join(root, archiveSource, rel)), content, rel)
	}
}

func TestArchiveDestinationCollision(t *testing.T) {
	t.Parallel()
	root := archiveProject(t)
	support.Write(t, filepath.Join(root, archiveDest), map[string]string{"keep.md": "keep\n"})
	r := archiveMove(t, root, archiveChange, archiveDate)
	equal(t, r.Code, 1, r.Out())
	match(t, r.Stderr, `archive destination collision`)
	equal(t, support.Read(t, filepath.Join(root, archiveDest, "keep.md")), "keep\n")
	equal(t, support.Read(t, filepath.Join(root, archiveSource, "proposal.md")), archiveFiles["proposal.md"])
}

func TestArchiveRefusesBadArguments(t *testing.T) {
	t.Parallel()
	root := archiveProject(t)
	for _, args := range [][]string{{}, {archiveChange}, {archiveChange, "23-09-2026"}, {"../x", archiveDate}, {"archive", archiveDate}} {
		r := archiveMove(t, root, args...)
		equal(t, r.Code, 2, fmt.Sprintf("%q: %s", args, r.Out()))
		match(t, r.Stderr, `usage:`)
	}
	equal(t, exists(filepath.Join(root, archiveSource, "proposal.md")), true)
}

// Docket AJ: tdd-evidence.md, the apply's append-only TDD evidence, lives in
// the change directory, so the archive move carries it with the change,
// byte-identical, whichever method moves it.
func TestArchiveCarriesTDDEvidence(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		method  string
		tracked bool
	}{{"git mv", true}, {"mv", false}} {
		t.Run("tdd-evidence.md moves with the change, byte-identical ("+c.method+")", func(t *testing.T) {
			t.Parallel()
			root := archiveProject(t)
			evidence := "# TDD Evidence — add-login\n\n## Run 1 — 2026-09-20T10:00:00Z\nsession: unknown\ncommit: unknown\n"
			support.Write(t, filepath.Join(root, archiveSource), map[string]string{"tdd-evidence.md": evidence})
			if c.tracked {
				commitSource(t, root, archiveSource)
			}
			r := archiveMove(t, root, archiveChange, archiveDate)
			equal(t, r.Code, 0, r.Out())
			equal(t, support.Read(t, filepath.Join(root, archiveDest, "tdd-evidence.md")), evidence)
			_, err := os.Stat(filepath.Join(root, archiveSource, "tdd-evidence.md"))
			equal(t, os.IsNotExist(err), true, "the source evidence is gone")
		})
	}
}
