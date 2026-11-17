package ldd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// A Cleanup is what an archived change left behind (T80b, L1074): its
// linked worktree, its branch, or both, and the commands that remove them.
// The branch's deletion is offered only when it is merged into the default
// branch. Jhon runs none of them: `jhon status` only reports.
type Cleanup struct {
	Change   string  `json:"change"`
	Worktree *string `json:"worktree"`
	Branch   *string `json:"branch"`
	// Merged is whether Branch is merged into the default branch; null
	// with no branch.
	Merged   *bool    `json:"merged"`
	Commands []string `json:"commands"`
	// into is the default branch Merged is about.
	into string
}

var archivedName = lazyre.New(`^[0-9]{4}-[0-9]{2}-[0-9]{2}-(.+)$`)

// archivedChanges are the changes under root's archive directory, by
// name, that are not active again.
func archivedChanges(root string, active []string) []string {
	entries, _ := os.ReadDir(filepath.Join(root, filepath.FromSlash(layout.For(root).ArchiveDir())))
	var out []string
	for _, e := range entries {
		m := archivedName.FindStringSubmatch(e.Name())
		if m == nil || !e.IsDir() || slices.Contains(active, m[1]) || slices.Contains(out, m[1]) {
			continue
		}
		out = append(out, m[1])
	}
	slices.SortFunc(out, js.Compare)
	return out
}

// ArchivedCleanup is what root's archived changes left behind: each local
// branch that names an archived change, with the linked worktree that has
// it checked out, and each other linked worktree whose directory is named
// for one (<repo>-worktrees/<change>) and whose branch names no change.
// A branch names an archived change when it is the change's name, a slash
// naming a dash, or ends with it after a slash or a dash
// (feat/shipping-cost names shipping-cost, feat/hold names no `old`), and
// no longer active change is named by BranchChange's rule. Never the
// default branch. Only for a project that is its repository's top level,
// as only such a project has branches of its own. Ordered by change.
// Read-only: nothing is removed.
func ArchivedCleanup(root string) []Cleanup {
	active, _ := activeChanges(root)
	archived := archivedChanges(root, active)
	if len(archived) == 0 {
		return nil
	}
	if top := vcs.GitTopLevel(root); top == "" || project.Realish(top) != project.Realish(root) {
		return nil
	}
	g := gitReads{}
	owner := func(branch string) string {
		dashed := strings.ReplaceAll(branch, "/", "-")
		best := ""
		for _, c := range archived {
			if (dashed == c || strings.HasSuffix(dashed, "-"+c)) && len(c) > len(best) {
				best = c
			}
		}
		if best == "" || len(branchNames(branch, active)) >= len(best) {
			return ""
		}
		return best
	}
	into, refs := defaultRefs(g, root)
	worktrees := vcs.LinkedWorktrees(root)
	var out []Cleanup
	defaults := g.defaultBranches(root)
	heads, _ := g.git(root, "for-each-ref", "--format=%(refname)", "refs/heads/")
	for _, ref := range nonEmpty(heads) {
		b := strings.TrimPrefix(ref, "refs/heads/")
		change := owner(b)
		if change == "" || slices.Contains(defaults, b) {
			continue
		}
		c := Cleanup{Change: change, Branch: new(b), Merged: new(false), into: into}
		if i := slices.IndexFunc(worktrees, func(w vcs.Worktree) bool { return w.Branch == b }); i >= 0 {
			c.Worktree = new(worktrees[i].Path)
		}
		for _, d := range refs {
			if _, ok := g.git(root, "merge-base", "--is-ancestor", ref, d); ok {
				*c.Merged = true
				break
			}
		}
		out = append(out, c)
	}
	// A worktree whose branch names no change, or whose HEAD is detached,
	// is the archived change's by its directory (<repo>-worktrees/<change>).
	for _, w := range worktrees {
		base := filepath.Base(w.Path)
		if !slices.Contains(archived, base) || w.Branch != "" && (slices.Contains(defaults, w.Branch) || owner(w.Branch) != "" || branchNames(w.Branch, active) != "") {
			continue
		}
		out = append(out, Cleanup{Change: base, Worktree: new(w.Path), into: into})
	}
	for i := range out {
		out[i].Commands = out[i].commands()
	}
	slices.SortStableFunc(out, func(a, b Cleanup) int { return js.Compare(a.Change, b.Change) })
	return out
}

// defaultRefs is the default branch's name and the refs that hold it:
// the local branch and its origin counterpart, those that exist. The name
// is "the default branch" when none does.
func defaultRefs(g gitReads, root string) (string, []string) {
	names := g.defaultBranches(root)
	// origin/HEAD's branch, when there is one, before main and master.
	for _, n := range slices.Concat(names[2:], names[:2]) {
		var refs []string
		for _, ref := range []string{"refs/heads/" + n, "refs/remotes/origin/" + n} {
			if _, ok := g.git(root, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); ok {
				refs = append(refs, ref)
			}
		}
		if len(refs) > 0 {
			return n, refs
		}
	}
	return "the default branch", nil
}

func (c Cleanup) deletable() bool { return c.Branch != nil && *c.Merged }

func (c Cleanup) commands() []string {
	out := []string{}
	if c.Worktree != nil {
		out = append(out, "git worktree remove "+shellWord(*c.Worktree))
	}
	if c.deletable() {
		out = append(out, "git branch -d "+shellWord(*c.Branch))
	}
	return out
}

// Note is the cleanup as a `jhon status` note.
func (c Cleanup) Note() string {
	var has []string
	if c.Worktree != nil {
		has = append(has, "its worktree "+*c.Worktree)
	}
	if c.Branch != nil {
		has = append(has, "its branch "+*c.Branch)
	}
	note := "archived change " + c.Change + " still has " + strings.Join(has, " and ")
	if c.Branch != nil {
		if *c.Merged {
			note += ", merged into " + c.into
		} else {
			note += ", not merged into " + c.into
		}
	}
	cmds := c.Commands
	if cmds == nil {
		cmds = c.commands()
	}
	quoted := make([]string, len(cmds))
	for i, x := range cmds {
		quoted[i] = "`" + x + "`"
	}
	sep := ": "
	if len(cmds) > 0 {
		what := "it"
		switch {
		case len(cmds) > 1:
			what = "them"
		case c.Branch != nil && c.Worktree != nil:
			what = "the worktree"
		}
		note += sep + "remove " + what + " with " + strings.Join(quoted, " and ")
		sep = "; "
	}
	if c.Branch != nil && !*c.Merged {
		note += sep + "the branch is not merged, so no command deletes it"
	}
	return note + " (jhon removes nothing)"
}

var plainWord = lazyre.New(`^[A-Za-z0-9_./@%+=:,-]+$`)

// shellWord is s as one shell word: as it is when it needs no quoting.
func shellWord(s string) string {
	if plainWord.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
