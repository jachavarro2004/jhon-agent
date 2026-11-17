package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// The delivered tree (T82, L597-L598): since T80 the agent works a change in
// a git worktree outside the repository and leaves it on the change's branch
// for delivery by PR, so a run's result is the final tree of that branch,
// what would be delivered. deliveredTree finds it:
//
//   - the main checkout, when it holds the work: its working tree differs
//     from the template commit outside the LDD layout's own files (ldd/,
//     openspec/; ignored files never count, T82b, L605): an agent sets LDD
//     up in the main checkout and works the change in its worktree;
//   - else the one worktree of the repository whose branch has commits
//     beyond the template commit or uncommitted changes;
//   - when several worktrees hold work, the tip: the one whose HEAD has
//     every other work-holding worktree's HEAD as an ancestor (T86b, L625:
//     a multi-message task makes several changes, often stacked or merged
//     into the last);
//   - none when several hold work and none is the tip: the check fails with
//     a note naming them (the other facts are then read on the main
//     checkout);
//   - the main checkout when nothing holds work.
//
// The hidden check, the changed and out-of-scope files, test_changed and
// the ceremony facts are read on that tree; the LDD layout and record facts
// as lddFacts says.
type checkedTree struct {
	Dir   string // the checked tree; "" when several worktrees hold work
	Label string // "main", "worktree:<branch>" or "none"
	Note  string // why no tree was checked
	// Worktrees are every worktree path of the repository but the main
	// checkout: never counted as a changed file of the checked tree.
	Worktrees []string
}

// worktree is one `git worktree list --porcelain` entry.
type worktree struct{ path, branch string }

// worktrees are repo's worktrees after the main checkout.
func worktrees(repo string, env []string) ([]worktree, error) {
	c := exec.Command("git", "worktree", "list", "--porcelain")
	c.Dir, c.Env = repo, env
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %v", err)
	}
	var all []worktree
	for _, block := range strings.Split(strings.TrimSpace(string(out)), "\n\n") {
		var w worktree
		for _, line := range strings.Split(block, "\n") {
			if p, ok := strings.CutPrefix(line, "worktree "); ok {
				w.path = p
			} else if b, ok := strings.CutPrefix(line, "branch "); ok {
				w.branch = strings.TrimPrefix(b, "refs/heads/")
			} else if line == "detached" {
				w.branch = "(detached)"
			}
		}
		if w.path != "" {
			all = append(all, w)
		}
	}
	if len(all) == 0 {
		return nil, nil
	}
	return all[1:], nil
}

// holdsWork is whether dir's HEAD or working tree differs from start;
// ignore leaves out paths that are not the agent's (the hidden test
// already copied in by an earlier check).
func holdsWork(dir, start string, env []string, ignore func(string) bool) (bool, error) {
	files, err := changedFiles(dir, start, env)
	if err != nil {
		return false, err
	}
	for _, f := range files {
		if ignore == nil || !ignore(f) {
			return true, nil
		}
	}
	c := exec.Command("git", "rev-list", "--count", start+"..HEAD")
	c.Dir, c.Env = dir, env
	out, err := c.Output()
	if err != nil {
		return false, fmt.Errorf("git rev-list %s..HEAD: %v", start, err)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n > 0, nil
}

// layoutFile is whether f is one of the LDD layout's own files.
func layoutFile(f string) bool {
	return strings.HasPrefix(f, "ldd/") || strings.HasPrefix(f, "openspec/")
}

// mainHoldsWork is whether the main checkout's working tree differs from
// start outside the LDD layout's own files (committed or not).
func mainHoldsWork(repo, start string, env []string, ignore func(string) bool) (bool, error) {
	files, err := changedFiles(repo, start, env)
	if err != nil {
		return false, err
	}
	for _, f := range files {
		if !layoutFile(f) && (ignore == nil || !ignore(f)) {
			return true, nil
		}
	}
	return false, nil
}

// deliveredTree is the tree a run's result is read on (see tree).
func deliveredTree(repo, start string, env []string, ignore func(string) bool) (checkedTree, error) {
	wts, err := worktrees(repo, env)
	if err != nil {
		return checkedTree{}, err
	}
	t := checkedTree{Dir: repo, Label: "main"}
	for _, w := range wts {
		t.Worktrees = append(t.Worktrees, w.path)
	}
	if main, err := mainHoldsWork(repo, start, env, ignore); err != nil || main {
		return t, err
	}
	var held []worktree
	for _, w := range wts {
		if _, err := os.Stat(w.path); err != nil {
			continue
		}
		if ok, err := holdsWork(w.path, start, env, ignore); err != nil {
			return t, err
		} else if ok {
			held = append(held, w)
		}
	}
	switch len(held) {
	case 0:
		return t, nil
	case 1:
		t.Dir, t.Label = held[0].path, "worktree:"+held[0].branch
		return t, nil
	}
	if tip, err := tipOf(held, env); err != nil || tip != nil {
		if tip != nil {
			t.Dir, t.Label = tip.path, "worktree:"+tip.branch
		}
		return t, err
	}
	var names []string
	for _, w := range held {
		names = append(names, w.branch+" ("+w.path+")")
	}
	t.Dir, t.Label = "", "none"
	t.Note = "several worktrees hold work, none is the delivered tree: " + strings.Join(names, ", ")
	return t, nil
}

// tipOf is the worktree of held whose HEAD has every other one's HEAD as
// an ancestor, nil when none or several (branches on one commit) have.
func tipOf(held []worktree, env []string) (*worktree, error) {
	heads := make([]string, len(held))
	for i, w := range held {
		h, err := headCommit(w.path, env)
		if err != nil {
			return nil, err
		}
		heads[i] = h
	}
	var found *worktree
	for i := range held {
		tip := true
		for j := range held {
			if j == i {
				continue
			}
			c := exec.Command("git", "merge-base", "--is-ancestor", heads[j], heads[i])
			c.Dir, c.Env = held[i].path, env
			if err := c.Run(); err != nil {
				if _, ok := err.(*exec.ExitError); !ok {
					return nil, fmt.Errorf("git merge-base: %v", err)
				}
				tip = false
				break
			}
		}
		if tip {
			if found != nil {
				return nil, nil
			}
			found = &held[i]
		}
	}
	return found, nil
}

// factsDir is where the tree's facts are read: the main checkout when no
// tree is checked.
func (t checkedTree) factsDir(repo string) string { return cmpOr(t.Dir, repo) }

// header is check.txt's first line.
func (t checkedTree) header() string {
	if t.Dir == "" {
		return "checked tree: none\n" + t.Note + "\nFAIL\n"
	}
	return "checked tree: " + t.Label + " (" + t.Dir + ")\n\n"
}

// withoutWorktrees are files without any path inside one of t's worktrees,
// when one lies inside dir: a worktree is never a changed file.
func (t checkedTree) withoutWorktrees(dir string, files []string) []string {
	var out []string
	for _, f := range files {
		inside := false
		for _, w := range t.Worktrees {
			rel, err := filepath.Rel(dir, w)
			if err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
				if p := filepath.ToSlash(rel); f == p || strings.HasPrefix(f, p+"/") {
					inside = true
				}
			}
		}
		if !inside {
			out = append(out, f)
		}
	}
	return out
}

// treeFacts fills m's facts that depend on the delivered tree, runs the
// hidden check on it and returns check.txt's text: execute's after-run path,
// and recheck's.
func (m *Metrics) treeFacts(o *Options, r *Run, start string, env []string, ignore func(string) bool) string {
	t, err := deliveredTree(r.Repo, start, env, ignore)
	if err != nil {
		m.Problems = append(m.Problems, "delivered tree: "+err.Error())
		t = checkedTree{Dir: r.Repo, Label: "main"}
	}
	m.CheckedTree = t.Label
	dir := t.factsDir(r.Repo)
	// The repository facts come before the hidden test is copied in.
	if files, err := changedFiles(dir, start, env); err != nil {
		m.Problems = append(m.Problems, err.Error())
	} else {
		files = t.withoutWorktrees(dir, files)
		if ignore != nil {
			kept := files[:0]
			for _, f := range files {
				if !ignore(f) {
					kept = append(kept, f)
				}
			}
			files = kept
		}
		m.fromChanges(r.Task, r.Variant, r.Cond.Name(), files)
	}
	m.fromCeremony(r.Task, r.Variant, dir, start, env)
	if (isLDD(r.Variant) || setsUpLDD(r.Cond.Name(), r.Variant)) && !isClean(r.Cond.Name()) {
		m.lddFacts(o, r, t, dir, start, env)
	}
	if t.Dir == "" {
		m.Success = false
		m.Problems = append(m.Problems, "hidden test: "+t.Note)
		return t.header()
	}
	ok, out, err := r.Task.Accept(t.Dir, env)
	if err != nil {
		m.Problems = append(m.Problems, "hidden test: "+err.Error())
	}
	m.Success = ok
	return t.header() + out
}

// lddFacts fills the LDD layout and record facts (T82b, L605):
//
//   - the layout is read where it is: the checked tree, else the main
//     checkout (an agent sets LDD up there and works in the change's
//     worktree);
//   - the record is the run's change's, wherever it lives: `jhon status
//     --json` in the checked tree when it names a change; else the run's
//     change (runChanges) is evaluated on its own branch in a throwaway clone
//     of the repository (recordStatus), as the agent may have merged and
//     left the branch, which status in the checked tree no longer sees.
func (m *Metrics) lddFacts(o *Options, r *Run, t checkedTree, dir, start string, env []string) {
	layoutDir := dir
	if repoLayout(dir) == "none" && repoLayout(r.Repo) != "none" {
		layoutDir = r.Repo
	}
	status := lddStatus(o.Home, dir, env)
	if statusChange(status) == "" {
		// Each change the run made is evaluated; a run that split its work
		// into several kept a record only when every one did (L606), so the
		// facts are the first change's that lacks one, else the first's.
		var chosen []byte
		changes := runChanges(r.Repo, dir, t, start, env)
		for _, change := range changes {
			s, err := recordStatus(o.Home, r, change, layoutDir, start, env)
			if err != nil {
				m.Problems = append(m.Problems, "ldd record: "+err.Error())
				continue
			}
			if s == nil {
				// No branch or tested commit stands for this change. Alone,
				// the checked tree's status stays (as before L606); beside
				// others, it is a change that kept no record.
				if len(changes) > 1 {
					chosen = []byte(`{}`)
					break
				}
				continue
			}
			if chosen == nil {
				chosen = s
			}
			if l := lddLayoutOf(layoutDir, s); l.Record == nil || !*l.Record {
				chosen = s
				break
			}
		}
		if chosen != nil {
			status = chosen
		}
	}
	m.LddLayout = lddLayoutOf(layoutDir, status)
	if v := lddRedBeforeGreen(status); v != nil {
		m.RedBeforeGreen, m.RedBeforeGreenSource = v, "ldd"
	}
}

// statusChange is the change a `jhon status --json` selected, "" for none.
func statusChange(status []byte) string {
	var s map[string]any
	if json.Unmarshal(status, &s) != nil {
		return ""
	}
	c, _ := s["change"].(string)
	return c
}

// dashed is the change name a branch gives (a slash names a dash).
func dashed(branch string) string { return strings.ReplaceAll(branch, "/", "-") }

// runChanges are the changes the run worked on: the checked worktree's
// branch's, else every change record created during the run (folderless
// under the git common dir's jhon/changes/, or a folder under a layout's
// changes/ in the main checkout or the checked tree, not in the template),
// in name order.
func runChanges(repo, dir string, t checkedTree, start string, env []string) []string {
	if b, ok := strings.CutPrefix(t.Label, "worktree:"); ok && b != "(detached)" {
		return []string{dashed(b)}
	}
	var names []string
	add := func(n string) {
		if n != "" && n != "archive" && !strings.HasPrefix(n, ".") && !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	if common := gitCommonDir(repo, env); common != "" {
		if es, err := os.ReadDir(filepath.Join(common, "jhon", "changes")); err == nil {
			for _, e := range es {
				if e.IsDir() {
					add(e.Name())
				}
			}
		}
	}
	for _, tree := range []string{repo, dir} {
		for _, l := range []string{"ldd", "openspec"} {
			es, err := os.ReadDir(filepath.Join(tree, l, "changes"))
			if err != nil {
				continue
			}
			for _, e := range es {
				rel := l + "/changes/" + e.Name()
				c := exec.Command("git", "cat-file", "-e", start+":"+rel)
				c.Dir, c.Env = repo, env
				if e.IsDir() && c.Run() != nil {
					add(e.Name())
				}
			}
		}
	}
	slices.Sort(names)
	return names
}

// gitCommonDir is repo's absolute git common dir, "" outside git.
func gitCommonDir(repo string, env []string) string {
	c := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	c.Dir, c.Env = repo, env
	out, err := c.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// recordStatus is `jhon status --json <change>` on change's own branch, as
// the engine sees it while the agent works there: in a throwaway clone of
// the run's repository (under the run's directory, removed after) whose
// default branch is the template commit, so the branch's diff is the
// change's even after the agent merged it; the change's folderless record
// (the git common dir's jhon/changes/<change>/) and, when the branch has
// none, the layout set up beside it are copied in. The run's own
// repository, branches and trees are only read. When no branch names the
// change any more, the commit its record last tested stands for it; nil
// when there is none.
func recordStatus(home string, r *Run, change, layoutDir, start string, env []string) ([]byte, error) {
	git := func(dir string, args ...string) (string, error) {
		c := exec.Command("git", args...)
		c.Dir, c.Env = dir, env
		out, err := c.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out)), nil
	}
	refs, err := git(r.Repo, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil, err
	}
	branch := ""
	for _, b := range strings.Split(refs, "\n") {
		if b != "" && dashed(b) == change && (branch == "" || b == change) {
			branch = b
		}
	}
	tip := ""
	if branch != "" {
		if tip, err = git(r.Repo, "rev-parse", "refs/heads/"+branch); err != nil {
			return nil, err
		}
	} else {
		// The branch is gone (merged and deleted): its last tested commit,
		// as the change's record and delivery name it, stands for it.
		branch, tip = change, recordTip(r.Repo, change, env)
		if tip == "" {
			return nil, nil
		}
		if _, err := git(r.Repo, "cat-file", "-e", tip+"^{commit}"); err != nil {
			return nil, nil
		}
	}
	tmp, err := os.MkdirTemp(r.Dir, ".record-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	clone := filepath.Join(tmp, "repo")
	if _, err := git(tmp, "clone", "-q", "--shared", "--no-checkout", r.Repo, clone); err != nil {
		return nil, err
	}
	// Detached on the tip, the clone's default branches move to the
	// template commit, and the change's branch is checked out on the tip.
	steps := [][]string{{"remote", "remove", "origin"}, {"checkout", "-q", "--detach", tip}}
	for _, def := range []string{"main", "master"} {
		if def != branch {
			steps = append(steps, []string{"branch", "-q", "-f", def, start})
		}
	}
	steps = append(steps, []string{"checkout", "-q", "-B", branch, tip})
	for _, s := range steps {
		if _, err := git(clone, s...); err != nil {
			return nil, err
		}
	}
	if common := gitCommonDir(r.Repo, env); common != "" {
		if src := filepath.Join(common, "jhon", "changes", change); fileExists(src) {
			if err := copyTree(src, filepath.Join(clone, ".git", "jhon", "changes", change)); err != nil {
				return nil, err
			}
		}
	}
	if l := repoLayout(layoutDir); repoLayout(clone) == "none" && l != "none" {
		if err := copyTree(filepath.Join(layoutDir, l), filepath.Join(clone, l)); err != nil {
			return nil, err
		}
	}
	return lddStatus(home, clone, env, change), nil
}

// recordTip is the commit change's folderless record last tested or
// delivered (execution-record.json candidate_commit, delivery.json
// head_commit), "" when neither says.
func recordTip(repo, change string, env []string) string {
	common := gitCommonDir(repo, env)
	if common == "" {
		return ""
	}
	dir := filepath.Join(common, "jhon", "changes", change)
	for _, f := range [][2]string{{"delivery.json", "head_commit"}, {"execution-record.json", "candidate_commit"}} {
		var v map[string]any
		if b, err := os.ReadFile(filepath.Join(dir, f[0])); err == nil && json.Unmarshal(b, &v) == nil {
			if c, ok := v[f[1]].(string); ok && c != "" {
				return c
			}
		}
	}
	return ""
}
