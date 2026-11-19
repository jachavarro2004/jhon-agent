package ldd

import (
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T150 (L1376-L1378): the change's own folder (tasks.md, proposal, design,
// spec deltas) is the change's record, as `--affected` reads it: written
// after the GREEN run and committed, it is no change to the tested tree, and
// `jhon deliver check` is ready with no re-run, in both layouts. An edit
// outside that folder, code, a test or another change's folder, still is.

// t150Order is the order every refusal of a run that is not the evidence names.
const t150Order = "; order: a passing run (`jhon executions run`), commit the tree it tested, then `jhon deliver check`"

// t150Repo is a repository with the layout under dir, changes c1 and c2,
// and c1's branch holding uncommitted work its GREEN run passed on.
func t150Repo(t *testing.T, dir string) string {
	t.Helper()
	root := support.GitRepo(t, temp(t))
	support.Write(t, root, map[string]string{
		"a/go.mod": "module example.com/a\n",
		"a/a.go":   "package a\n",
		dir + "/config.yaml": "test_command_override:\n  a: \"true\"\n" +
			"lint_command_override:\n  a: \"true\"\nformat_command_override:\n  a: \"true\"\n",
		dir + "/changes/c1/tasks.md": "- [ ] 1.1 add B\n",
		dir + "/changes/c2/tasks.md": "- [ ] 1.1 other\n",
	})
	commit(t, root, "base")
	support.Git(t, root, "checkout", "-q", "-b", "c1")
	support.Write(t, root, map[string]string{"a/b.go": "package a\n\nvar B = 1\n", "a/b_test.go": "package a\n"})
	r := jhonGit(t, root, "executions", "run", "c1")
	equal(t, r.Code, 0, "GREEN", r.Out())
	return root
}

func TestT150ChangeFolderEditsAfterGreenNeedNoRerun(t *testing.T) {
	t.Parallel()
	for _, dir := range []string{"openspec", "ldd"} {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()
			root := t150Repo(t, dir)
			support.Write(t, root, map[string]string{
				dir + "/changes/c1/tasks.md":    "- [x] 1.1 add B\n",
				dir + "/changes/c1/proposal.md": "## Why\nB.\n",
				dir + "/changes/c1/design.md":   "## Design\nB is 1.\n",
			})
			commit(t, root, "c1, tasks marked")
			d := jhonGit(t, root, "deliver", "check", "c1", "--json")
			equal(t, d.Code, 0, d.Out())
			c := object(t, d.Stdout)
			equal(t, c["ready"], true, c["reasons"])
			equal(t, c["reasons"], []any{})
			equal(t, anyMatch(c["notes"], evidenceNote), true, c["notes"])
		})
	}
}

func TestT150EditsOutsideTheChangeFolderStillCount(t *testing.T) {
	t.Parallel()
	for _, dir := range []string{"openspec", "ldd"} {
		for name, edit := range map[string]map[string]string{
			"code":         {"a/b.go": "package a\n\nvar B = 2\n"},
			"test":         {"a/b_test.go": "package a\n\n// edited\n"},
			"other change": {dir + "/changes/c2/tasks.md": "- [x] 1.1 other\n"},
		} {
			t.Run(dir+"/"+name, func(t *testing.T) {
				t.Parallel()
				root := t150Repo(t, dir)
				support.Write(t, root, edit)
				commit(t, root, "c1, edited after its run")
				d := jhonGit(t, root, "deliver", "check", "c1", "--json")
				equal(t, d.Code, 1, d.Out())
				c := object(t, d.Stdout)
				equal(t, c["ready"], false)
				differ := "2 path(s) differ (a/b.go, a/b_test.go)"
				if name == "other change" {
					differ = "3 path(s) differ (a/b.go, a/b_test.go, " + dir + "/changes/c2/tasks.md)"
				}
				equal(t, c["reasons"], []any{
					"the last test run started on a working tree that was not clean" + t150Order,
					"HEAD's tree is not the tested tree: " + differ + "; test this revision" + t150Order,
				})
				equal(t, anyMatch(c["notes"], evidenceNote), false, c["notes"])
			})
		}
	}
}
