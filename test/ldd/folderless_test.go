package ldd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// A small change has no folder (T31, L254): the branch names it, a slash
// naming a dash as in BranchChange, and its record is its RED and GREEN
// runs in `jhon executions`, kept under the git common dir so they never
// dirty the tree (L315).

const (
	smallBranch = "feat/small-fix"
	smallChange = "feat-small-fix"
	smallRecord = ".git/jhon/changes/" + smallChange + "/execution-record.json"
)

// smallRepo is a committed root with project `a`, whose tests fail while
// `red` is in the root, on branch feat/small-fix with a code change and no
// folder under openspec/changes/.
func smallRepo(t *testing.T) string {
	t.Helper()
	root := support.GitRepo(t, temp(t))
	support.Write(t, root, map[string]string{
		"a/go.mod": "module example.com/a\n",
		"a/a.go":   "package a\n",
		"openspec/config.yaml": "test_command_override:\n  a: \"test ! -f ../red\"\n" +
			"lint_command_override:\n  a: \"true\"\nformat_command_override:\n  a: \"true\"\n",
	})
	commit(t, root, "base")
	support.Git(t, root, "checkout", "-q", "-b", smallBranch)
	support.Write(t, root, map[string]string{"a/b.go": "package a\n\nvar B = 1\n", "a/b_test.go": "package a\n"})
	return root
}

func TestFolderlessChangeIsTheBranchs(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	for _, args := range [][]string{{"status", "--json"}, {"status", smallChange, "--json"}} {
		r := jhonGit(t, root, args...)
		equal(t, r.Code, 0, r.Stderr)
		s := object(t, r.Stdout)
		equal(t, s["ok"], true, args)
		equal(t, s["change"], smallChange, args)
		equal(t, s["directory"], nil, args)
		equal(t, s["folder"], false, args)
		equal(t, anyMatch(s["notes"], `^feat-small-fix has no folder: branch feat/small-fix names it`), true, s["notes"])
		k := keys(t, []byte(r.Stdout))
		equal(t, k[len(k)-2:], []string{"needs", "folder"}, args)
	}
	human := jhonGit(t, root, "status").Stdout
	match(t, human, `(?m)^change      feat-small-fix  \(no folder: branch feat/small-fix names it\)$`)
}

// The idle case stays: the default branch, or a branch that changes no
// file outside openspec/.
func TestFolderlessIdle(t *testing.T) {
	t.Parallel()
	onMain := smallRepo(t)
	support.Git(t, onMain, "checkout", "-q", "main")
	empty := smallRepo(t)
	os.Remove(filepath.Join(empty, "a/b.go"))
	os.Remove(filepath.Join(empty, "a/b_test.go"))
	for _, root := range []string{onMain, empty} {
		r := jhonGit(t, root, "status", "--json")
		equal(t, r.Code, 0, r.Stderr)
		s := object(t, r.Stdout)
		equal(t, s["change"], nil)
		equal(t, s["next"], "start-change")
		_, has := s["folder"]
		equal(t, has, false, "an idle status has no folder")
	}
}

// A change with a folder says so, its record where it always was.
func TestFolderChangeSaysFolder(t *testing.T) {
	t.Parallel()
	s := statusJSON(t, cycle, "tasks-partial")
	equal(t, s["folder"], true)
}

// `jhon executions run` takes the branch's folderless change and keeps its
// record under the git common dir: the tree is never dirtied, and status
// and delivery read it there.
func TestFolderlessRecordUnderGitDir(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	before := support.Git(t, root, "status", "--porcelain")
	support.Write(t, root, map[string]string{"red": ""})
	r := jhonGit(t, root, "executions", "run", smallChange, "--json")
	equal(t, r.Code, 1, r.Stderr)
	equal(t, object(t, r.Stdout)["record"], smallRecord)
	os.Remove(filepath.Join(root, "red"))
	r = jhonGit(t, root, "executions", "run", smallChange, "--json")
	equal(t, r.Code, 0, r.Stderr)
	equal(t, exists(filepath.Join(root, smallRecord)), true)
	equal(t, exists(filepath.Join(root, "openspec/changes", smallChange)), false, "no folder is made")
	equal(t, support.Git(t, root, "status", "--porcelain"), before, "the tree is not dirtied")
	tests := object(t, string(field(t, []byte(jhonGit(t, root, "status", "--json").Stdout), "tests")))
	equal(t, tests["red_before_green"], true, "status reads the record")

	// Delivery reads and writes beside it.
	commit(t, root, "small fix")
	equal(t, jhonGit(t, root, "executions", "run", smallChange, "--json").Code, 0)
	d := jhonGit(t, root, "deliver", "check", smallChange, "--json")
	equal(t, d.Code, 0, d.Out())
	equal(t, object(t, d.Stdout)["ready"], true, d.Stdout)
	equal(t, exists(filepath.Join(root, ".git/jhon/changes", smallChange, "delivery.json")), true)
	equal(t, strings.TrimSpace(support.Git(t, root, "status", "--porcelain")), "", "delivery leaves the tree clean")
}

// Any other name without a folder is refused as before.
func TestFolderlessOtherNameRefused(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	r := jhonGit(t, root, "executions", "run", "feat-other")
	equal(t, r.Code, 2)
	equal(t, r.Stderr, "jhon executions: openspec/changes/feat-other/ does not exist or is not an active change under "+root+"\n")
	equal(t, exists(filepath.Join(root, ".git/jhon")), false)
}

// A high-risk change needs tasks.md, so a folder: delivery says where its
// independent verification goes instead of naming a folder that is not there.
func TestFolderlessHighRiskDeliverNamesTheFolder(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	os.Remove(filepath.Join(root, "a/b_test.go"))
	commit(t, root, "untested code")
	equal(t, jhonGit(t, root, "executions", "run", smallChange, "--json").Code, 0)
	d := jhonGit(t, root, "deliver", "check", smallChange, "--json")
	equal(t, d.Code, 1, d.Out())
	equal(t, anyMatch(object(t, d.Stdout)["reasons"], `^the change is high risk \(a/b\.go: .*\) and has no folder: risk high needs its tasks\.md, so write it under openspec/changes/feat-small-fix/ and record there a verify-report\.md stating verdict: pass \(the verify agent\), or a Judgment Day under openspec/changes/feat-small-fix/judgments/ closed APPROVED$`), true, d.Stdout)
}

// T68 (L550): the folderless change's branch name is taken as its name, so
// `executions run feat/small-fix` is `executions run feat-small-fix`.
func TestFolderlessBranchNameRunsTheChange(t *testing.T) {
	t.Parallel()
	byBranch, byName := smallRepo(t), smallRepo(t)
	b := jhonGit(t, byBranch, "executions", "run", smallBranch, "--json")
	n := jhonGit(t, byName, "executions", "run", smallChange, "--json")
	equal(t, b.Code, 0, b.Stderr)
	equal(t, b.Code, n.Code)
	equal(t, b.Stderr, n.Stderr)
	equal(t, object(t, b.Stdout)["record"], smallRecord)
	equal(t, exists(filepath.Join(byBranch, smallRecord)), true)
	equal(t, exists(filepath.Join(byBranch, ".git/jhon/changes/feat")), false, "no record under the slash")
	bh := jhonGit(t, byBranch, "executions", "run", smallBranch, "--dry-run")
	nh := jhonGit(t, byName, "executions", "run", smallChange, "--dry-run")
	equal(t, bh.Code, nh.Code)
	equal(t, strings.ReplaceAll(bh.Stdout, byBranch, "R"), strings.ReplaceAll(nh.Stdout, byName, "R"))
}

// The branch name without its prefix is not taken, but the error names the
// change to use.
func TestFolderlessBareNameNamesTheChange(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	r := jhonGit(t, root, "executions", "run", "small-fix")
	equal(t, r.Code, 2)
	equal(t, r.Stderr, "jhon executions: openspec/changes/small-fix/ does not exist or is not an active change under "+root+"\n"+
		"jhon executions: branch feat/small-fix names change feat-small-fix: run `jhon executions run feat-small-fix`\n")
	equal(t, exists(filepath.Join(root, ".git/jhon")), false)
}

// Another branch's name is refused as before; a folder change's layout is
// unchanged: its name runs, a slashed name is refused as before.
func TestFolderlessOtherBranchRefused(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	r := jhonGit(t, root, "executions", "run", "feat/other")
	equal(t, r.Code, 2)
	equal(t, r.Stderr, "jhon executions: openspec/changes/feat/other/ does not exist or is not an active change under "+root+"\n")
	equal(t, exists(filepath.Join(root, ".git/jhon")), false)

	folder := smallRepo(t)
	support.Write(t, folder, map[string]string{"openspec/changes/feat-small-fix/tasks.md": "- [ ] 1.1 x\n"})
	r = jhonGit(t, folder, "executions", "run", smallBranch)
	equal(t, r.Code, 2)
	equal(t, r.Stderr, "jhon executions: openspec/changes/feat/small-fix/ does not exist or is not an active change under "+folder+"\n")
	equal(t, jhonGit(t, folder, "executions", "run", smallChange, "--json").Code, 0)
}

// T84 (L606, L607): a change starts with its RED run, so on a fresh branch
// in a worktree, the layout set up in ldd/ untracked, a new untracked test
// is the change: status selects it and `executions run` records the RED run.
func TestFolderlessUntrackedTestInWorktree(t *testing.T) {
	t.Parallel()
	root, wt := shippingWorktree(t)
	support.Write(t, wt, map[string]string{"a/c_test.go": "package a\n", "red": ""})
	s := object(t, jhonGit(t, wt, "status", "--json").Stdout)
	equal(t, s["change"], "feat-shipping-cost", s)
	r := jhonGit(t, wt, "executions", "run", "feat-shipping-cost", "--json")
	equal(t, r.Code, 1, r.Out())
	equal(t, exists(filepath.Join(root, ".git/jhon/changes/feat-shipping-cost/execution-record.json")), true, "the RED run is recorded")
}

// A fresh branch whose only untracked files are the layout's is no change,
// and `executions run` with the branch's change says why and what to do.
func TestFolderlessLayoutOnlyBranchSaysWhy(t *testing.T) {
	t.Parallel()
	_, wt := shippingWorktree(t)
	s := object(t, jhonGit(t, wt, "status", "--json").Stdout)
	equal(t, s["change"], nil, s)
	for _, name := range []string{"feat-shipping-cost", "feat/shipping-cost"} {
		r := jhonGit(t, wt, "executions", "run", name)
		equal(t, r.Code, 2)
		equal(t, r.Stderr, "jhon executions: ldd/changes/"+name+"/ does not exist or is not an active change under "+wt+"\n"+
			"jhon executions: branch feat/shipping-cost names change feat-shipping-cost, which has no changes yet: no file outside ldd/ differs from the base or is new; write the failing test first (a new untracked file counts), then run `jhon executions run feat-shipping-cost`\n")
	}
}

// The default branch with untracked files is no change, as before.
func TestFolderlessDefaultBranchUntrackedIdle(t *testing.T) {
	t.Parallel()
	root, _ := shippingWorktree(t)
	support.Write(t, root, map[string]string{"ldd/config.yaml": "test_command_override:\n  a: \"true\"\n", "a/c_test.go": "package a\n"})
	s := object(t, jhonGit(t, root, "status", "--json").Stdout)
	equal(t, s["change"], nil, s)
	r := jhonGit(t, root, "executions", "run", "main")
	equal(t, r.Code, 2)
	equal(t, r.Stderr, "jhon executions: ldd/changes/main/ does not exist or is not an active change under "+root+"\n")
}

// shippingWorktree is a committed repository with project `a` and no
// layout, and a worktree of it on a fresh branch feat/shipping-cost whose
// ldd/ is set up but untracked; its tests fail while `red` is there.
func shippingWorktree(t *testing.T) (root, wt string) {
	t.Helper()
	dir := temp(t)
	if err := os.Mkdir(filepath.Join(dir, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	root = support.GitRepo(t, filepath.Join(dir, "repo"))
	support.Write(t, root, map[string]string{"a/go.mod": "module example.com/a\n", "a/a.go": "package a\n"})
	commit(t, root, "base")
	wt = filepath.Join(dir, "shipping-cost")
	support.Git(t, root, "worktree", "add", "-q", "-b", "feat/shipping-cost", wt)
	support.Write(t, wt, map[string]string{
		"ldd/config.yaml": "test_command_override:\n  a: \"test ! -f ../red\"\n" +
			"lint_command_override:\n  a: \"true\"\nformat_command_override:\n  a: \"true\"\n",
		"ldd/specs/.keep": "", "ldd/changes/archive/.keep": "",
	})
	return root, wt
}
