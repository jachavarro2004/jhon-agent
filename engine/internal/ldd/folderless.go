package ldd

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// A small change has no folder (T31, L254): it needs no tasks.md, proposal
// or spec, so the branch names it, and its record is its RED and GREEN runs
// in `jhon executions` plus the commit or PR text with what was asked.

// FolderlessChange is the change the checked-out branch in the repository
// holding root names when it has no folder under openspec/changes/: the
// branch's name, a slash naming a dash as in BranchChange (feat/small-fix
// names feat-small-fix). "" outside a project that is its repository's top
// level with openspec/, with no branch, on the default branch, when
// the branch changes no file outside openspec/, or when that name has a
// folder.
func FolderlessChange(root string) string {
	name, _ := gitReads(nil).folderless(root)
	return name
}

// FolderlessBranch is FolderlessChange and the branch that names it.
func FolderlessBranch(root string) (name, branch string) {
	return gitReads(nil).folderless(root)
}

// FolderlessPending is the change the checked-out branch would name, and
// the branch, when it is no change only because no file outside the layout
// differs from the base or is new yet (T84, L606): "" otherwise.
func FolderlessPending(root string) (name, branch string) {
	g := gitReads(nil)
	if n, _ := g.folderless(root); n != "" {
		return "", ""
	}
	return g.candidate(root)
}

// FolderlessPendingNote says what FolderlessPending finds: the branch would
// name a change that has no file of its own yet, and its failing test
// starts it. "" when FolderlessPending finds none.
func FolderlessPendingNote(root string) string {
	name, branch := FolderlessPending(root)
	if name == "" {
		return ""
	}
	return "branch " + branch + " names change " + name + ", which has no changes yet: no file outside " + layout.For(root).Dir() + "/ differs from the base or is new; write the failing test first (a new untracked file counts), then run `jhon executions run " + name + "`"
}

// changedOutsideRecord is whether a file outside the layout differs from
// the base or is new in root's repository; true when git cannot say.
func (g gitReads) changedOutsideRecord(root string) bool {
	top := vcs.GitTopLevel(root)
	base, _, ok := g.baseOf(top, nil)
	if top == "" || !ok {
		return true
	}
	files, _ := g.addedOutsideRecord(top, base)
	return len(files) > 0
}

// folderless is FolderlessChange and the branch that names it.
func (g gitReads) folderless(root string) (name, branch string) {
	if name, branch = g.candidate(root); name == "" {
		return "", ""
	}
	top, _ := g.git(root, "rev-parse", "--show-toplevel")
	base, _, ok := g.baseOf(js.Trim(top), nil)
	if !ok {
		return "", ""
	}
	if files, _ := g.addedOutsideRecord(js.Trim(top), base); len(files) == 0 {
		return "", ""
	}
	return name, branch
}

// candidate is the change the checked-out branch names, and the branch,
// before asking whether the branch changes anything: "" outside a project
// that is its repository's top level with a layout, with no branch, on the
// default branch, or when that name has a folder.
func (g gitReads) candidate(root string) (name, branch string) {
	out, _ := g.git(root, "branch", "--show-current")
	if branch = js.Trim(out); branch == "" || slices.Contains(g.defaultBranches(root), branch) {
		return "", ""
	}
	name = strings.ReplaceAll(branch, "/", "-")
	if name == "archive" || strings.HasPrefix(name, ".") || strings.Contains(name, `\`) {
		return "", ""
	}
	if st, err := os.Stat(filepath.Join(root, changesOf(root), name)); err == nil && st.IsDir() {
		return "", ""
	}
	// Only a project that is its repository's top level and uses LDD
	// (openspec/) has one, as only such a project has a route of its own.
	top, _ := g.git(root, "rev-parse", "--show-toplevel")
	top = js.Trim(top)
	if top == "" || project.Realish(top) != project.Realish(root) || !layout.For(root).Detect(root) {
		return "", ""
	}
	if _, _, ok := g.baseOf(top, nil); !ok {
		return "", ""
	}
	return name, branch
}

// defaultBranches are the names the default branch goes by: origin/HEAD's
// branch, main, and master.
func (g gitReads) defaultBranches(root string) []string {
	out := []string{"main", "master"}
	if ref, ok := g.git(root, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); ok {
		if b := strings.TrimPrefix(js.Trim(ref), "origin/"); b != "" {
			out = append(out, b)
		}
	}
	return out
}

// folderlessNote is the note that says where a folderless change's name
// comes from and what its record is.
func folderlessNote(change, branch string) string {
	return change + " has no folder: branch " + branch + " names it (a slash names a dash); its record is its RED and GREEN runs in `jhon executions` and the commit or PR text saying what was asked"
}
