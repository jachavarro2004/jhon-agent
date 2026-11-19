package ldd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T57 slice 6 (L1315): the tests of a workspace's member are run by
// `jhon executions run <change> --repo <name>`, in the primary or in any
// member, and recorded in the primary's record of the change under
// `repos.<name>`; `jhon deliver check <change>` in the member reads that
// entry with the tested-tree rules; tasks.md slices name their repository
// with `Repo: <name>`.

// wsFixture is a primary app (project p, change c, two slices, the second
// in lib) and a member lib that joined it (project a, whose test passes
// while a/a.go says good), each committed.
type wsFixture struct {
	app, lib string
	env      []string
}

const wsTasks = "## Slice 1: core\n\nDelivers: the core\n\n- [ ] core task\n\n## Slice 2: lib side\n\nRepo: lib\n\n- [ ] lib task\n"

func workspaceFixture(t *testing.T) wsFixture {
	t.Helper()
	dir := temp(t)
	f := wsFixture{app: filepath.Join(dir, "app"), lib: filepath.Join(dir, "lib"),
		env: support.Env("XDG_STATE_HOME=" + filepath.Join(dir, "state"))}
	for _, p := range []string{f.app, f.lib} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		support.GitRepo(t, p)
	}
	support.Write(t, f.app, map[string]string{
		"p/go.mod": "module example.com/p\n", "p/p.go": "package p\n",
		"ldd/config.yaml":        "test_command_override:\n  p: \"true\"\nworkspace:\n  lib: ../lib\n",
		"ldd/changes/c/tasks.md": wsTasks,
	})
	support.Write(t, f.lib, map[string]string{
		"a/go.mod": "module example.com/a\n", "a/a.go": "package a // good\n",
		"ldd/config.yaml": "test_command_override:\n  a: \"grep -q good a.go\"\n",
	})
	commit(t, f.app, "app")
	commit(t, f.lib, "lib")
	r := support.Jhon(t, f.lib, f.env, "workspace", "join", f.app)
	equal(t, r.Code, 0, r.Out())
	return f
}

func (f wsFixture) run(t *testing.T, dir string, args ...string) support.Result {
	t.Helper()
	return support.Jhon(t, dir, f.env, args...)
}

// record is the primary's execution record of change c.
func (f wsFixture) record(t *testing.T) map[string]any {
	t.Helper()
	return object(t, support.Read(t, filepath.Join(f.app, "ldd", "changes", "c", "execution-record.json")))
}

func headTree(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(support.Git(t, dir, "rev-parse", "HEAD^{tree}"))
}

// E1: a run with --repo, in the primary or in the member, runs the member's
// own test command and records it under repos.<name>; the primary's own run
// keeps that entry, and the entry keeps the primary's run.
func TestWorkspaceExecutionsRunPerRepo(t *testing.T) {
	t.Parallel()
	f := workspaceFixture(t)

	r := f.run(t, f.app, "executions", "run", "c", "--repo", "lib")
	equal(t, r.Code, 0, r.Out())
	match(t, r.Stdout, `(?m)^==> a: grep -q good a\.go$`)
	match(t, r.Stdout, `(?m)^jhon executions: pass, exit 0 \(3 of 3 passed; record ldd/changes/c/execution-record\.json, repos\.lib\)$`)
	rec := f.record(t)
	repos, _ := rec["repos"].(map[string]any)
	lib, _ := repos["lib"].(map[string]any)
	ks := []string{}
	for k := range lib {
		ks = append(ks, k)
	}
	equal(t, len(ks), 5, ks)
	equal(t, lib["working_tree"], headTree(t, f.lib))
	equal(t, lib["after_working_tree"], headTree(t, f.lib))
	equal(t, lib["passed"], true)
	equal(t, lib["complete"], true)
	ex := lib["executions"].([]any)[0].(map[string]any)
	equal(t, ex["project"], "a")
	equal(t, ex["command"], "grep -q good a.go")
	equal(t, ex["status"], "pass")
	// The primary ran nothing of its own: no last run in its status.
	equal(t, rec["executions"], nil)
	equal(t, statusOf(t, f, f.app)["tests"], nil)

	// The primary's own run, as today, keeps the member's entry.
	r = f.run(t, f.app, "executions", "run", "c")
	equal(t, r.Code, 0, r.Out())
	rec = f.record(t)
	equal(t, rec["executions"].([]any)[0].(map[string]any)["project"], "p")
	equal(t, rec["repos"].(map[string]any)["lib"], lib)

	// From the member, a failing run replaces the entry; the primary's run stays.
	support.Write(t, f.lib, map[string]string{"a/a.go": "package a // bad\n"})
	r = f.run(t, f.lib, "executions", "run", "c", "--repo", "lib", "--json")
	equal(t, r.Code, 1, r.Out())
	sum := object(t, r.Stdout)
	equal(t, sum["repo"], "lib")
	equal(t, sum["passed"], false)
	again := f.record(t)
	equal(t, again["executions"], rec["executions"])
	lib = again["repos"].(map[string]any)["lib"].(map[string]any)
	equal(t, lib["passed"], false)
	equal(t, lib["executions"].([]any)[0].(map[string]any)["status"], "fail")
	equal(t, lib["working_tree"] == headTree(t, f.lib), false)

	// Refusals write nothing.
	before := support.Read(t, filepath.Join(f.app, "ldd", "changes", "c", "execution-record.json"))
	for _, c := range []struct {
		dir    string
		args   []string
		stderr string
	}{
		{f.app, []string{"executions", "run", "c", "--repo", "web"}, "jhon executions: the workspace of " + f.app + " lists no member web (members: lib)\n"},
		{f.lib, []string{"executions", "run", "nope", "--repo", "lib"}, "jhon executions: ldd/changes/nope/ does not exist or is not an active change under " + f.app + "\n"},
		{f.lib, []string{"executions", "run", "--repo", "lib"}, "jhon executions: --repo needs the change named: jhon executions run <change> --repo <name>\n"},
		{f.lib, []string{"executions", "run", "c", "--repo", "lib", "--affected", "main"}, "jhon executions: --repo runs the member's whole test command: --affected, --dry-run and --jobs are not taken with it\n"},
	} {
		r := f.run(t, c.dir, c.args...)
		equal(t, r.Code, 2, c.args, r.Out())
		equal(t, r.Stderr, c.stderr, c.args)
	}
	equal(t, support.Read(t, filepath.Join(f.app, "ldd", "changes", "c", "execution-record.json")), before)

	// Outside a workspace --repo is refused, and nothing is written.
	plain := deliverProject(t, "true")
	r = jhonGit(t, plain, "executions", "run", deliverChange, "--repo", "lib")
	equal(t, r.Code, 2, r.Out())
	equal(t, r.Stderr, "jhon executions: --repo needs a multi-repository workspace, and "+plain+" is in none (no workspace pointer)\n")
	equal(t, exists(filepath.Join(plain, deliverDir, "execution-record.json")), false)
	// A run with no member's entry writes no repos key.
	r = jhonGit(t, plain, "executions", "run", deliverChange)
	equal(t, r.Code, 0, r.Out())
	_, has := object(t, support.Read(t, filepath.Join(plain, deliverDir, "execution-record.json")))["repos"]
	equal(t, has, false)
}

func statusOf(t *testing.T, f wsFixture, dir string) map[string]any {
	t.Helper()
	return object(t, f.run(t, dir, "status", "c", "--json").Stdout)
}

// E2: a slice's `Repo: <name>` line is its repository, in status's JSON
// and in its slices line.
func TestWorkspaceSliceRepo(t *testing.T) {
	t.Parallel()
	f := workspaceFixture(t)
	s := statusOf(t, f, f.app)
	sl := s["tasks"].(map[string]any)["slices"].([]any)
	_, has := sl[0].(map[string]any)["repo"]
	equal(t, has, false, sl[0])
	equal(t, sl[1].(map[string]any)["repo"], "lib")
	equal(t, sl[0].(map[string]any)["delivers"], "the core")
	r := f.run(t, f.app, "status", "c")
	match(t, r.Stdout, `(?m)^slices      0/2 done, current Slice 1: core \(0/1 checked\), next Slice 2: lib side \(0/1 checked, repo lib\)$`)
}

// E3: deliver check in a member reads its entry in the primary's record
// with the tested-tree rules: a passing, complete, undisturbed run whose
// tested tree is HEAD's, the tree clean.
func TestWorkspaceDeliverCheckInAMember(t *testing.T) {
	t.Parallel()
	f := workspaceFixture(t)
	recFile := filepath.Join(f.app, "ldd", "changes", "c", "execution-record.json")
	check := func() (support.Result, map[string]any) {
		t.Helper()
		r := f.run(t, f.lib, "deliver", "check", "c", "--json")
		return r, object(t, r.Stdout)
	}
	reasonsOf := func(c map[string]any) string { b, _ := json.Marshal(c["reasons"]); return string(b) }

	// No entry yet.
	r, c := check()
	equal(t, r.Code, 1, r.Out())
	equal(t, c["reasons"], []any{"no recorded test run: run jhon executions run c --repo lib (" + recFile + ", repos.lib)"})

	// A pass on uncommitted work: commit it, with no re-run.
	support.Write(t, f.lib, map[string]string{"a/a.go": "package a // good, v2\n", "a/a_test.go": "package a // v2\n"})
	equal(t, f.run(t, f.lib, "executions", "run", "c", "--repo", "lib").Code, 0)
	r, c = check()
	equal(t, r.Code, 1, r.Out())
	equal(t, c["reasons"], []any{"the working tree is not clean, and the last test run passed on it as it is: commit it, then `jhon deliver check c`, with no re-run"})
	commit(t, f.lib, "v2")
	r, c = check()
	equal(t, r.Code, 0, r.Out())
	equal(t, c["ready"], true)
	equal(t, c["tested_tree"], headTree(t, f.lib))
	equal(t, c["tested_commit"], nil)
	// The ready check's record stays out of both trees.
	equal(t, strings.TrimSpace(support.Git(t, f.lib, "status", "--porcelain")), "")
	equal(t, exists(filepath.Join(f.app, "ldd", "changes", "c", "delivery.json")), false)
	// The text form names the same.
	r = f.run(t, f.lib, "deliver", "check", "c")
	equal(t, r.Code, 0, r.Out())
	match(t, r.Stdout, `(?m)^merge  ready$`)
	match(t, r.Stdout, `(?m)^tested  none  tree `+headTree(t, f.lib)+`$`)

	// A commit after the run: HEAD's tree is not the tested tree.
	support.Write(t, f.lib, map[string]string{"a/b.go": "package a\n", "a/b_test.go": "package a\n"})
	commit(t, f.lib, "more")
	r, c = check()
	equal(t, r.Code, 1, r.Out())
	match(t, reasonsOf(c), regexp.QuoteMeta("HEAD's tree is not the tested tree: 2 path(s) differ (a/b.go, a/b_test.go); test this revision"))

	// A failing run.
	support.Write(t, f.lib, map[string]string{"a/a.go": "package a // bad\n", "a/a_test.go": "package a // bad\n"})
	commit(t, f.lib, "bad")
	equal(t, f.run(t, f.lib, "executions", "run", "c", "--repo", "lib").Code, 1)
	r, c = check()
	equal(t, r.Code, 1, r.Out())
	equal(t, c["reasons"], []any{"the last test run did not pass: a"})

	// A run whose tree changed while it ran, or that did not finish.
	support.Write(t, f.lib, map[string]string{"a/a.go": "package a // good\n", "a/a_test.go": "package a // good\n"})
	commit(t, f.lib, "good")
	equal(t, f.run(t, f.lib, "executions", "run", "c", "--repo", "lib").Code, 0)
	_, c = check()
	equal(t, c["ready"], true, c)
	original := support.Read(t, recFile)
	tree := headTree(t, f.lib)
	other := strings.TrimSpace(support.Git(t, f.app, "rev-parse", "HEAD^{tree}"))
	support.Write(t, f.app, map[string]string{"ldd/changes/c/execution-record.json": strings.Replace(original, `"after_working_tree": "`+tree+`"`, `"after_working_tree": "`+other+`"`, 1)})
	r, c = check()
	equal(t, r.Code, 1, r.Out())
	equal(t, c["reasons"], []any{"the candidate changed during the last test run: what was tested is not one revision; run the tests again"})
	support.Write(t, f.app, map[string]string{"ldd/changes/c/execution-record.json": strings.Replace(original, `"complete": true`, `"complete": false`, 1)})
	r, c = check()
	equal(t, r.Code, 1, r.Out())
	equal(t, c["reasons"], []any{"the last test run did not finish: its record is partial"})

	// The primary's own check reads its own run, not the member's.
	support.Write(t, f.app, map[string]string{"ldd/changes/c/execution-record.json": original})
	r = f.run(t, f.app, "deliver", "check", "c", "--json")
	equal(t, r.Code, 1, r.Out())
	match(t, reasonsOf(object(t, r.Stdout)), regexp.QuoteMeta("no recorded test run: run jhon executions run c (ldd/changes/c/execution-record.json)"))
}
