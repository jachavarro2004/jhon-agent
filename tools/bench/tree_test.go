package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// treeRun is a finished run's directory: repo/ with the template commit
// (pager.py) and a hidden test that passes when pager.py says "fixed".
type treeRun struct {
	t     *testing.T
	r     *Run
	env   []string
	start string
}

func newTreeRun(t *testing.T) *treeRun {
	t.Helper()
	dir := t.TempDir()
	task := Task{ID: "x", Dir: filepath.Join(dir, "task"), Hidden: "hidden", Scope: []string{"pager.py"}, Tests: "test_*.py",
		Check: `grep -q fixed pager.py && test -f hidden_check`}
	for f, body := range map[string]string{"task/hidden/hidden_check": "", "runs/run/repo/pager.py": "broken\n"} {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cond, err := conditionNamed("jhon")
	if err != nil {
		t.Fatal(err)
	}
	r := &Run{Task: task, Variant: "plain", Cond: cond, Dir: filepath.Join(dir, "runs", "run"), Repo: filepath.Join(dir, "runs", "run", "repo")}
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if err := gitRepo(r.Repo, env); err != nil {
		t.Fatal(err)
	}
	start, err := headCommit(r.Repo, env)
	if err != nil {
		t.Fatal(err)
	}
	return &treeRun{t: t, r: r, env: env, start: start}
}

func (w *treeRun) git(dir string, args ...string) {
	w.t.Helper()
	c := exec.Command("git", args...)
	c.Dir, c.Env = dir, w.env
	if out, err := c.CombinedOutput(); err != nil {
		w.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func (w *treeRun) write(dir, file, body string) {
	w.t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

// worktree adds <run>/repo-worktrees/<name> on branch <name> with its fix
// committed, plus an extra out-of-scope file.
func (w *treeRun) worktree(name string) string {
	path := filepath.Join(w.r.Dir, "repo-worktrees", name)
	w.git(w.r.Repo, "worktree", "add", "-q", "-b", name, path)
	w.write(path, "pager.py", "fixed\n")
	w.write(path, "extra.py", "x\n")
	w.git(path, "add", "-A")
	w.git(path, "commit", "-q", "-m", "fix")
	return path
}

func (w *treeRun) facts() (*Metrics, string) {
	m := &Metrics{}
	out := m.treeFacts(&Options{}, w.r, w.start, w.env, nil)
	return m, out
}

// Pin: work in the main checkout is checked there, as before T82.
func TestTreeMainCheckoutHoldsWork(t *testing.T) {
	w := newTreeRun(t)
	w.write(w.r.Repo, "pager.py", "fixed\n")
	w.write(w.r.Repo, "other.py", "x\n")
	m, out := w.facts()
	if !m.Success || m.CheckedTree != "main" || !strings.HasPrefix(out, "checked tree: main (") {
		t.Errorf("success=%t checked_tree=%q out=%q", m.Success, m.CheckedTree, out)
	}
	if !slices.Equal(m.OutOfScope, []string{"other.py"}) {
		t.Errorf("out of scope %v", m.OutOfScope)
	}
	if !fileExists(filepath.Join(w.r.Repo, "hidden_check")) {
		t.Error("the hidden test was not copied into the main checkout")
	}
}

// Work committed only in one worktree is checked there and passes; its
// directory is never out of scope, its own changed files are.
func TestTreeOneWorktreeHoldsWork(t *testing.T) {
	w := newTreeRun(t)
	path := w.worktree("feat/fix")
	m, out := w.facts()
	if !m.Success || m.CheckedTree != "worktree:feat/fix" {
		t.Fatalf("success=%t checked_tree=%q out=%q problems=%v", m.Success, m.CheckedTree, out, m.Problems)
	}
	if !strings.HasPrefix(out, "checked tree: worktree:feat/fix ("+path+")") {
		t.Errorf("check.txt header: %q", out)
	}
	if !slices.Equal(m.ChangedFiles, []string{"extra.py", "pager.py"}) || !slices.Equal(m.OutOfScope, []string{"extra.py"}) {
		t.Errorf("changed %v out of scope %v", m.ChangedFiles, m.OutOfScope)
	}
	if fileExists(filepath.Join(w.r.Repo, "hidden_check")) || !fileExists(filepath.Join(path, "hidden_check")) {
		t.Error("the hidden test went to the wrong tree")
	}
	if m.Ceremony == nil || m.Ceremony.Commits != 1 {
		t.Errorf("ceremony %+v", m.Ceremony)
	}
}

// Uncommitted work in a worktree counts as its work too.
func TestTreeWorktreeUncommittedWork(t *testing.T) {
	w := newTreeRun(t)
	path := filepath.Join(w.r.Dir, "repo-worktrees", "wip")
	w.git(w.r.Repo, "worktree", "add", "-q", "-b", "wip", path)
	w.write(path, "pager.py", "fixed\n")
	if m, out := w.facts(); !m.Success || m.CheckedTree != "worktree:wip" {
		t.Errorf("success=%t checked_tree=%q out=%q", m.Success, m.CheckedTree, out)
	}
}

// Two worktrees with work: no tree is the delivered one, the check fails
// with a note naming both.
func TestTreeSeveralWorktreesFail(t *testing.T) {
	w := newTreeRun(t)
	w.worktree("feat/a")
	w.worktree("feat/b")
	m, out := w.facts()
	if m.Success || m.CheckedTree != "none" {
		t.Errorf("success=%t checked_tree=%q", m.Success, m.CheckedTree)
	}
	for _, s := range []string{"checked tree: none", "several worktrees hold work", "feat/a (", "feat/b (", "FAIL"} {
		if !strings.Contains(out, s) {
			t.Errorf("check.txt misses %q: %q", s, out)
		}
	}
	if !slices.ContainsFunc(m.Problems, func(p string) bool { return strings.Contains(p, "feat/a") && strings.Contains(p, "feat/b") }) {
		t.Errorf("problems %v", m.Problems)
	}
}

// A worktree inside the checked tree is never one of its changed files.
func TestTreeNestedWorktreeNotOutOfScope(t *testing.T) {
	w := newTreeRun(t)
	w.write(w.r.Repo, "pager.py", "fixed\n")
	w.git(w.r.Repo, "worktree", "add", "-q", "-b", "side", filepath.Join(w.r.Repo, "wt"))
	m, _ := w.facts()
	if m.CheckedTree != "main" || !slices.Equal(m.ChangedFiles, []string{"pager.py"}) || len(m.OutOfScope) != 0 {
		t.Errorf("checked %q changed %v out of scope %v", m.CheckedTree, m.ChangedFiles, m.OutOfScope)
	}
}

// recheck reads a finished run whose work sits in a worktree again, with
// no model call: the run now passes on the worktree, the old metrics.json
// and check.txt are kept beside, and the facts that do not depend on the
// tree stay as they were.
func TestRecheckWorktreeRun(t *testing.T) {
	w := newTreeRun(t)
	tasks := filepath.Join(filepath.Dir(filepath.Dir(w.r.Dir)), "tasks")
	w.r.Task.Dir = filepath.Join(tasks, "x")
	for f, body := range map[string]string{
		"x/task.yaml":           "id: x\nkind: bug\nmodel: m\nmax_turns: 5\ntimeout: 1m\ncheck: grep -q fixed pager.py && test -f hidden_check\nscope: |\n  pager.py\nprompt: |\n  fix\n",
		"x/hidden/hidden_check": "",
	} {
		p := filepath.Join(tasks, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w.worktree("feat/fix")
	// The old check ran in the main checkout: the hidden test is there.
	w.write(w.r.Repo, "hidden_check", "")
	old := &Metrics{Task: "x", Variant: "plain", Condition: "jhon", Turns: 7, Problems: []string{"kept"}}
	if err := writeJSON(filepath.Join(w.r.Dir, "metrics.json"), old); err != nil {
		t.Fatal(err)
	}
	w.write(w.r.Dir, "check.txt", "old FAILED\n")
	var out strings.Builder
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if code := cli([]string{"recheck", "--home", t.TempDir(), "--tasks-dir", tasks, filepath.Dir(filepath.Dir(w.r.Dir))}, &out, &out, time.Now()); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "run: success false -> true, checked tree worktree:feat/fix") {
		t.Errorf("output %q", out.String())
	}
	b, _ := os.ReadFile(filepath.Join(w.r.Dir, "metrics.json"))
	m := &Metrics{}
	if err := json.Unmarshal(b, m); err != nil {
		t.Fatal(err)
	}
	if !m.Success || m.CheckedTree != "worktree:feat/fix" || m.Turns != 7 || !slices.Equal(m.Problems, []string{"kept"}) || !slices.Equal(m.OutOfScope, []string{"extra.py"}) {
		t.Errorf("metrics %s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(w.r.Dir, "check.txt.before-recheck")); string(b) != "old FAILED\n" {
		t.Errorf("check.txt backup %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(w.r.Dir, "metrics.json.before-recheck")); !strings.Contains(string(b), `"turns": 7`) {
		t.Errorf("metrics.json backup %q", b)
	}
}

// recordJhon is a bench HOME's jhon whose `status --json` models the
// engine's: it reports a folderless change, with its test record, only on
// the checked-out branch that names it when that branch changes code
// against master (outside ldd/) and the record is under the git common
// dir's jhon/changes/<name>/; else no change.
const recordJhon = `#!/bin/sh
[ "$1" = status ] || exit 2
b=$(git branch --show-current); n=$(echo "$b" | tr / -)
c=$(git rev-parse --git-common-dir)
if [ -n "$b" ] && [ "$b" != master ] && [ -f "$c/jhon/changes/$n/execution-record.json" ] &&
   [ -n "$(git diff --name-only "$(git merge-base HEAD master)" -- . ':!ldd' 2>/dev/null)" ]; then
  printf '{"ok":true,"change":"%s","tests":{"passed":true},"needs":{"tasks":false}}\n' "$n"
else
  echo '{"ok":true,"change":null,"tests":null}'
fi
`

// withJhon gives the run a bench HOME holding recordJhon.
func (w *treeRun) withJhon() *Options {
	w.t.Helper()
	home := w.t.TempDir()
	p := benchJhon(home)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(recordJhon), 0o755); err != nil {
		w.t.Fatal(err)
	}
	return &Options{Home: home}
}

// setUpLDD leaves an untracked ldd/ (config, specs, changes) in dir.
func (w *treeRun) setUpLDD(dir string) {
	w.t.Helper()
	for _, d := range []string{"ldd/specs", "ldd/changes/archive"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			w.t.Fatal(err)
		}
	}
	w.write(dir, "ldd/config.yaml", "context: x\n")
}

// record writes change's folderless test record under the common git dir.
func (w *treeRun) record(change string) {
	w.t.Helper()
	d := filepath.Join(w.r.Repo, ".git", "jhon", "changes", change)
	if err := os.MkdirAll(d, 0o755); err != nil {
		w.t.Fatal(err)
	}
	w.write(d, "execution-record.json", `{"executions":[]}`)
}

func (w *treeRun) factsWith(o *Options) (*Metrics, string) {
	m := &Metrics{}
	out := m.treeFacts(o, w.r, w.start, w.env, nil)
	return m, out
}

func recordOf(m *Metrics) string {
	if m.LddLayout == nil {
		return "no layout"
	}
	r := "nil"
	if m.LddLayout.Record != nil {
		r = strconv.FormatBool(*m.LddLayout.Record)
	}
	return m.LddLayout.Layout + " test_record=" + strconv.FormatBool(m.LddLayout.TestRecord) + " route=" + m.LddLayout.RouteRequiresFolder + " record=" + r
}

// LDD set up in the main checkout (untracked ldd/ only) is not work: the
// worktree holding the work is checked, and the set-up still counts.
func TestTreeSetUpOnlyMainIsNotWork(t *testing.T) {
	w := newTreeRun(t)
	o := w.withJhon()
	w.setUpLDD(w.r.Repo)
	w.worktree("feat/fix")
	w.record("feat-fix")
	m, out := w.factsWith(o)
	if !m.Success || m.CheckedTree != "worktree:feat/fix" {
		t.Fatalf("success=%t checked_tree=%q out=%q problems=%v", m.Success, m.CheckedTree, out, m.Problems)
	}
	if got := recordOf(m); got != "ldd test_record=true route=false record=true" {
		t.Errorf("ldd layout %s", got)
	}
}

// Pin: code changes in the main checkout beside ldd/ are its work.
func TestTreeMainCodeAndLDDIsWork(t *testing.T) {
	w := newTreeRun(t)
	w.setUpLDD(w.r.Repo)
	w.write(w.r.Repo, "pager.py", "fixed\n")
	w.worktree("feat/other")
	if m, out := w.factsWith(w.withJhon()); !m.Success || m.CheckedTree != "main" {
		t.Errorf("success=%t checked_tree=%q out=%q", m.Success, m.CheckedTree, out)
	}
}

// A folderless change's record is found after the agent merged its branch
// and left it, kept or deleted: status in the main checkout names no
// change, the record under .git/jhon/changes/<name>/ is still the run's
// (a deleted branch's tip is the commit its delivery names).
func TestTreeRecordAfterLeavingBranch(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		w := newTreeRun(t)
		o := w.withJhon()
		w.setUpLDD(w.r.Repo)
		w.git(w.r.Repo, "checkout", "-q", "-b", "fix/page")
		w.write(w.r.Repo, "pager.py", "fixed\n")
		w.git(w.r.Repo, "commit", "-q", "-am", "fix")
		w.record("fix-page")
		head, _ := headCommit(w.r.Repo, w.env)
		w.write(filepath.Join(w.r.Repo, ".git", "jhon", "changes", "fix-page"), "delivery.json", `{"action":"merge","head_commit":"`+head+`"}`)
		w.git(w.r.Repo, "checkout", "-q", "master")
		w.git(w.r.Repo, "merge", "-q", "--ff-only", "fix/page")
		if deleted {
			w.git(w.r.Repo, "branch", "-q", "-d", "fix/page")
		}
		m, out := w.factsWith(o)
		if !m.Success || m.CheckedTree != "main" {
			t.Fatalf("deleted=%t success=%t checked_tree=%q out=%q", deleted, m.Success, m.CheckedTree, out)
		}
		if got := recordOf(m); got != "ldd test_record=true route=false record=true" {
			t.Errorf("deleted=%t ldd layout %s problems %v", deleted, got, m.Problems)
		}
		// The agent's branches and trees are untouched, nothing is left behind.
		if after, _ := headCommit(w.r.Repo, w.env); after != head {
			t.Errorf("HEAD moved %s -> %s", head, after)
		}
		c := exec.Command("git", "for-each-ref", "--format=%(refname:short)", "refs/heads")
		c.Dir, c.Env = w.r.Repo, w.env
		if refs, _ := c.Output(); (string(refs) == "master\n") != deleted {
			t.Errorf("deleted=%t branches %q", deleted, refs)
		}
		if left, _ := filepath.Glob(filepath.Join(w.r.Dir, ".record-*")); len(left) != 0 {
			t.Errorf("left behind %v", left)
		}
		if wts, _ := worktrees(w.r.Repo, w.env); len(wts) != 0 {
			t.Errorf("worktrees %v", wts)
		}
	}
}

// Pin: the checked worktree's branch names the change; its record is found.
func TestTreeRecordOfCheckedWorktree(t *testing.T) {
	w := newTreeRun(t)
	o := w.withJhon()
	path := w.worktree("feat/fix")
	w.setUpLDD(path)
	w.record("feat-fix")
	m, _ := w.factsWith(o)
	if m.CheckedTree != "worktree:feat/fix" || recordOf(m) != "ldd test_record=true route=false record=true" {
		t.Errorf("checked %q layout %s", m.CheckedTree, recordOf(m))
	}
}

// A run that split its work into several changes, each merged with its
// branch deleted, kept a record only when every change kept one (L606).
func TestTreeRecordOfSeveralChanges(t *testing.T) {
	for _, bothRecorded := range []bool{true, false} {
		w := newTreeRun(t)
		o := w.withJhon()
		w.setUpLDD(w.r.Repo)
		for _, c := range []struct{ branch, file string }{{"fix/a", "a.py"}, {"fix/b", "b.py"}} {
			w.git(w.r.Repo, "checkout", "-q", "-b", c.branch)
			w.write(w.r.Repo, c.file, "fixed\n")
			w.git(w.r.Repo, "add", c.file)
			w.git(w.r.Repo, "commit", "-q", "-m", c.branch)
			name := dashed(c.branch)
			if bothRecorded || c.branch == "fix/a" {
				w.record(name)
				head, _ := headCommit(w.r.Repo, w.env)
				w.write(filepath.Join(w.r.Repo, ".git", "jhon", "changes", name), "delivery.json", `{"action":"merge","head_commit":"`+head+`"}`)
			} else if err := os.MkdirAll(filepath.Join(w.r.Repo, ".git", "jhon", "changes", name), 0o755); err != nil {
				t.Fatal(err)
			}
			w.git(w.r.Repo, "checkout", "-q", "master")
			w.git(w.r.Repo, "merge", "-q", "--ff-only", c.branch)
			w.git(w.r.Repo, "branch", "-q", "-d", c.branch)
		}
		m, _ := w.factsWith(o)
		want := "ldd test_record=true route=false record=true"
		if !bothRecorded {
			want = "ldd test_record=false route=unknown record=false"
		}
		if got := recordOf(m); got != want {
			t.Errorf("both recorded=%t: %s, want %s; problems %v", bothRecorded, got, want, m.Problems)
		}
		for _, p := range m.Problems {
			if strings.Contains(p, "ambiguous") {
				t.Errorf("both recorded=%t: %q", bothRecorded, p)
			}
		}
	}
}
