// route-guard holds every agent to LDD and the rigor its change's route
// sets: the record (a spec delta and tasks, a proposal or a design) is never
// refused, and a change that alters behaviour opens its PR only with it; a
// change whose diff reaches the verified tier (risk high) waits for verify
// before its PR, and size alone only gets a note (S25). The verify agent may
// always be launched (S22); Prism reviewers run only with the owner's
// permission, and `delivery: change` opens one PR per change and
// merges with rebase.

package guards

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

type rgEnv struct {
	p, grants string
	env       []string
}

func newRG(t *testing.T, config string) rgEnv {
	t.Helper()
	scratch := realTemp(t)
	r := rgEnv{p: filepath.Join(scratch, "proj")}
	mkdir(t, filepath.Join(r.p, "openspec", "changes"))
	support.Write(t, r.p, map[string]string{"openspec/config.yaml": config, "src/app.py": "X = 1\n"})
	support.GitRepo(t, r.p)
	support.Git(t, r.p, "add", "-A")
	support.Git(t, r.p, "commit", "-q", "-m", "base")
	state := filepath.Join(scratch, "state")
	r.grants = filepath.Join(state, "jhon", "grants")
	r.env = support.Env("HOME=/home/jhon-route-test", "XDG_STATE_HOME="+state, "JHON_ROOT="+support.Root())
	return r
}

// call is a tool call by a sub-agent (agent_id set) or, with top, by the
// owner's session in the default permission mode.
func (r rgEnv) call(t *testing.T, tool string, input map[string]any, top bool) support.Result {
	t.Helper()
	payload := map[string]any{"tool_name": tool, "tool_input": input, "cwd": r.p, "permission_mode": "bypassPermissions"}
	if top {
		payload["permission_mode"] = "default"
	} else {
		payload["agent_id"], payload["agent_type"] = "a1", "worker"
	}
	return hook(t, r.env, "route-guard", nil, payload)
}

func (r rgEnv) write(t *testing.T, rel string) support.Result {
	t.Helper()
	return r.call(t, "Write", map[string]any{"file_path": filepath.Join(r.p, rel), "content": "x"}, false)
}

func TestRouteGuardStandardRoute(t *testing.T) {
	t.Parallel()
	r := newRG(t, "delivery: change\n")
	// A small, low-risk change (code with its test, as TDD leaves it) is
	// standard: its record is written, and so are a proposal and a design
	// when the change needs one or the owner asks (S21); verify may always
	// be launched (S22).
	support.Write(t, r.p, map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n"})
	code(t, r.write(t, "openspec/changes/c1/tasks.md"), 0, "tasks.md on the standard route")
	code(t, r.write(t, "openspec/changes/c1/specs/x/spec.md"), 0, "a spec on the standard route")
	code(t, r.write(t, "openspec/changes/c1/exploration.md"), 0, "notes always pass")
	code(t, r.write(t, "src/new.py"), 0, "code passes")
	for _, f := range []string{"design.md", "proposal.md"} {
		res := r.write(t, "openspec/changes/c1/"+f)
		code(t, res, 0, "a "+f+" on the standard route")
		if res.Stderr != "" || res.Stdout != "" {
			t.Errorf("a %s on the standard route passes silently: %q %q", f, res.Stdout, res.Stderr)
		}
	}
	code(t, r.call(t, "Bash", cmd("D=openspec/changes/c1; cat > $D/design.md <<'EOF'\nx\nEOF"), false), 0, "a design written from Bash")
	code(t, r.call(t, "Bash", cmd("cat openspec/changes/c1/tasks.md 2>/dev/null"), false), 0, "a read whose errors go to /dev/null")
	// verify may always be launched, at the top level or from a sub-agent.
	for _, top := range []bool{true, false} {
		res := r.call(t, "Agent", map[string]any{"subagent_type": "verify", "prompt": "x"}, top)
		code(t, res, 0, "verify on the standard route")
		if res.Stdout != "" {
			t.Errorf("verify on the standard route is not asked for: %q", res.Stdout)
		}
	}
	code(t, r.call(t, "Agent", map[string]any{"subagent_type": "worker", "prompt": "x"}, true), 0, "a worker")
	// A record that exists is edited on any route.
	support.Write(t, r.p, map[string]string{"openspec/changes/old/design.md": "x\n"})
	code(t, r.write(t, "openspec/changes/old/design.md"), 0, "an existing design")
}

// A change that alters behaviour opens its PR only with its record, on any
// route; one that touches only tests and documents needs none, and one whose
// behaviour stays states why its spec does not change.
func TestRouteGuardRecordBeforeThePR(t *testing.T) {
	t.Parallel()
	pr := cmd("gh pr create --title x --body y")
	r := newRG(t, "")
	support.Write(t, r.p, map[string]string{"src/app.py": "X = 2\n"})
	res := r.call(t, "Bash", pr, true)
	code(t, res, 2, "a code change with no record")
	matches(t, res.Stderr, `this change alters behaviour, and the branch carries no change with its record`, "why")
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n"})
	code(t, r.call(t, "Bash", pr, true), 2, "tasks without a spec delta")
	redGreen(t, r.p, "openspec/changes/c1/execution-record.json")
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/tasks.md": "No spec change: a rename that keeps every behaviour.\n\n## Slice 1: a\n- [x] 1.1 a\n"})
	// Untested code is risk high, the verified route: its verification is on record.
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/verify-report.md": "verdict: pass\n"})
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/review-prism.md": "facets: safety\nverdict: pass\n"})
	code(t, r.call(t, "Bash", pr, true), 0, "tasks stating why no spec changes")
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n", "openspec/changes/c1/specs/a/spec.md": "# a\n"})
	code(t, r.call(t, "Bash", pr, true), 0, "tasks and a spec delta")

	docs := newRG(t, "")
	support.Write(t, docs.p, map[string]string{"docs/guide.md": "# guide\n", "tests/test_app.py": "def test_x(): pass\n"})
	code(t, docs.call(t, "Bash", pr, true), 0, "tests and documents alone need no record")
}

// At risk passive or medium a one-line tasks.md, an `## Intent` line and a
// `Delivers:` line, is a complete record: a spec delta is due only when a
// scenario changes (S23). At risk high the record is today's.
func TestRouteGuardOneLineRecord(t *testing.T) {
	t.Parallel()
	pr := cmd("gh pr create --title x --body y")
	oneLine := map[string]string{"openspec/changes/c1/tasks.md": "## Intent\nDelivers: X is 2 in the report\n"}

	std := newRG(t, "delivery: change\n")
	support.Write(t, std.p, map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n"})
	res := std.call(t, "Bash", pr, true)
	code(t, res, 2, "a standard change with no record")
	// It needs no tasks.md (T31): its record is its RED before GREEN.
	matches(t, res.Stderr, "^route-guard: this change alters behaviour, and its RED before GREEN is not on record \\(LDD, contracts/ldd.md, TDD always\\): run `jhon executions run <change> --affected <base>` while its new test fails \\(RED\\), a unit's run \\(`--affected`\\) between, and `--affected <base>` again once it passes \\(GREEN\\), where <base> is the commit the slice started from \\(with none, a full `jhon executions run <change>`\\), before the PR. Route: standard: ", "the refusal names the RED and GREEN runs")
	support.Write(t, std.p, map[string]string{"openspec/changes/c1/tasks.md": "## Intent\nWhy: X is wrong\n"})
	code(t, std.call(t, "Bash", pr, true), 2, "an Intent without Delivers, and no runs")
	redGreen(t, std.p, "openspec/changes/c1/execution-record.json")
	support.Write(t, std.p, oneLine)
	res = std.call(t, "Bash", pr, true)
	code(t, res, 0, "an Intent and a Delivers line at risk medium")
	if res.Stderr != "" {
		t.Errorf("a one-line record passes: %q", res.Stderr)
	}
	support.Write(t, std.p, map[string]string{"openspec/changes/c1/tasks.md": "No spec change: a rename.\n"})
	code(t, std.call(t, "Bash", pr, true), 0, "No spec change still stands")
	support.Write(t, std.p, map[string]string{"openspec/changes/c1/tasks.md": "## Intent\nDelivers: X is 2\n\n## Slice 1: a\n- [ ] 1.1 a\n"})
	code(t, std.call(t, "Bash", pr, true), 2, "delivery: change still waits for every task")

	high := newRG(t, "")
	support.Write(t, high.p, map[string]string{"src/app.py": "X = 2\n", "openspec/changes/c1/verify-report.md": "verdict: pass\n", "openspec/changes/c1/review-prism.md": "facets: safety\nverdict: pass\n"})
	support.Write(t, high.p, oneLine)
	res = high.call(t, "Bash", pr, true)
	code(t, res, 2, "a one-line record at risk high")
	matches(t, res.Stderr, "^route-guard: this change alters behaviour, and the branch carries no change with its record \\(LDD, contracts/ldd.md\\): write its tasks.md and spec delta under openspec/changes/<change>/ before the PR, or state `No spec change: <why>` in tasks.md when its behaviour stays. Route: verified: ", "risk high keeps today's rule")
	support.Write(t, high.p, map[string]string{"openspec/changes/c1/specs/a/spec.md": "# a\n"})
	redGreen(t, high.p, "openspec/changes/c1/execution-record.json")
	code(t, high.call(t, "Bash", pr, true), 0, "a spec delta at risk high")
}

// With docs_with_code, a project that keeps its documentation in docs/
// updates it with its code before the PR, or the change states why not.
func TestRouteGuardDocsWithCode(t *testing.T) {
	t.Parallel()
	pr := cmd("gh pr create --title x --body y")
	r := newRG(t, "docs_with_code: true\n")
	pyproject := "[project]\nname = \"x\"\n[tool.pytest.ini_options]\n"
	support.Write(t, r.p, map[string]string{"svc/pyproject.toml": pyproject, "svc/docs/api.md": "# api\n", "lib/pyproject.toml": pyproject})
	support.Git(t, r.p, "add", "-A")
	support.Git(t, r.p, "commit", "-q", "-m", "projects")
	record := map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\nDelivers: CA-01\n- [x] 1.1 a\n", "openspec/changes/c1/specs/a/spec.md": "# a\n", "openspec/changes/c1/execution-record.json": redGreenRecord}
	support.Write(t, r.p, record)
	support.Write(t, r.p, map[string]string{"svc/src/api.py": "X = 2\n", "lib/src/util.py": "Y = 1\n"})
	res := r.call(t, "Bash", pr, true)
	code(t, res, 2, "svc's code changed and its docs did not")
	matches(t, res.Stderr, `docs_with_code: svc change\(s\) code but not its docs/`, "the refusal names only the project with docs/")
	support.Write(t, r.p, map[string]string{"svc/docs/api.md": "# api\n\nX is 2.\n"})
	code(t, r.call(t, "Bash", pr, true), 0, "its docs changed with it")
	support.Write(t, r.p, map[string]string{"svc/docs/api.md": "# api\n", "openspec/changes/c1/tasks.md": "No docs change: X is internal.\n\n## Slice 1: a\nDelivers: CA-01\n- [x] 1.1 a\n"})
	code(t, r.call(t, "Bash", pr, true), 0, "the change states why its docs stay")
}

func TestRouteGuardVerifiedRoute(t *testing.T) {
	t.Parallel()
	r := newRG(t, "delivery: change\n")
	// A migration is high risk: the change is verified.
	support.Write(t, r.p, map[string]string{"src/app.py": "X = 2\n", "src/migrations/0001_add.sql": "alter table a drop column b;\n", "tests/test_app.py": "def test_a(): pass\n"})
	code(t, r.write(t, "openspec/changes/c1/tasks.md"), 0, "tasks.md on the verified route")
	code(t, r.write(t, "openspec/changes/c1/specs/a/spec.md"), 0, "a spec on the verified route")
	code(t, r.write(t, "openspec/changes/c1/proposal.md"), 0, "a proposal for a one-project change")
	code(t, r.call(t, "Agent", map[string]any{"subagent_type": "verify", "prompt": "x"}, true), 0, "verify on the verified route")

	// The PR opens once the branch's change has a spec and tasks, all checked.
	pr := cmd("gh pr create --title x --body y")
	res := r.call(t, "Bash", pr, true)
	code(t, res, 2, "no spec or tasks yet")
	matches(t, res.Stderr, `carries no change with its record`, "why")
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n- [ ] 1.2 b\n", "openspec/changes/c1/specs/a/spec.md": "# a\n"})
	redGreen(t, r.p, "openspec/changes/c1/execution-record.json")
	res = r.call(t, "Bash", pr, true)
	code(t, res, 2, "an unchecked task under delivery: change")
	matches(t, res.Stderr, `^route-guard: c1: delivery: change in openspec/config\.yaml merges the change once, after its last slice; 1 task\(s\) of tasks\.md are unchecked \(1\.2 b\)`, "why")
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n- [x] 1.2 b\n"})
	res = r.call(t, "Bash", pr, true)
	code(t, res, 2, "every task checked, but no independent verification")
	matches(t, res.Stderr, `the route is verified \(.*\): verify is due before delivery`, "why")
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/verify-report.md": "verdict: pass\n"})
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/review-prism.md": "facets: safety\nverdict: pass\n"})
	code(t, r.call(t, "Bash", pr, true), 0, "every task checked")
	code(t, r.call(t, "Bash", cmd("gh pr merge 5 --squash"), true), 2, "a merge without rebase")
	code(t, r.call(t, "Bash", cmd("gh pr merge 5 --rebase --delete-branch"), true), 0, "a merge with rebase")
}

// The verify agent may always be launched (S22): a verify the session
// launches from the main checkout, which sits on the default branch with
// nothing changed, runs whether or not it names a lane worktree's change.
func TestRouteGuardVerifyOfALaneChange(t *testing.T) {
	t.Parallel()
	r := newRG(t, "delivery: change\n")
	lane := filepath.Join(filepath.Dir(r.p), "proj-worktrees", "lane")
	support.Git(t, r.p, "worktree", "add", "-q", "-b", "chore/lane", lane)
	support.Write(t, lane, map[string]string{"openspec/changes/fix-lane/tasks.md": "## Slice 1: a\nDelivers: CA-01\n- [ ] 1.1 a\n",
		"src/migrations/0001_add.sql": "alter table a drop column b;\n", "tests/test_app.py": "def test_a(): pass\n"})
	support.Git(t, lane, "add", "-A")
	support.Git(t, lane, "commit", "-q", "-m", "a migration")
	verify := func(prompt string) support.Result {
		return r.call(t, "Agent", map[string]any{"subagent_type": "verify", "prompt": prompt}, true)
	}
	code(t, verify("Verify the LDD change `fix-lane` before its PR."), 0, "verify of the lane's verified change")
	code(t, verify("Verify the change before its PR."), 0, "a verify that names no change")
	code(t, verify("Verify fix-lane-2."), 0, "a verify that names another change")
}

// A proposal or design is never refused (S21), whatever the projects a
// change spans: a few seed rows in a second project, or 100 lines or more.
func TestRouteGuardDesignSpans(t *testing.T) {
	t.Parallel()
	r := newRG(t, "delivery: change\n")
	pyproject := "[project]\nname = \"x\"\n[tool.pytest.ini_options]\n"
	support.Write(t, r.p, map[string]string{"svc_a/pyproject.toml": pyproject, "svc_b/pyproject.toml": pyproject})
	support.Git(t, r.p, "add", "-A")
	support.Git(t, r.p, "commit", "-q", "-m", "projects")
	lines := func(n int) string { return strings.Repeat("X = 1\n", n) }
	support.Write(t, r.p, map[string]string{
		"svc_a/migrations/0001_add.sql": "alter table a drop column b;\n", "svc_a/src/a.py": lines(150),
		"svc_b/src/seed.py": lines(5), "svc_b/tests/test_seed.py": lines(300),
	})
	code(t, r.write(t, "openspec/changes/c1/design.md"), 0, "a design for a change that only touches its second project")
	support.Write(t, r.p, map[string]string{"svc_b/src/seed.py": lines(120)})
	code(t, r.write(t, "openspec/changes/c1/design.md"), 0, "a design for a change that spans both projects")
}

// A bounded writer is launched with its scope, in any project: without
// `## Allowed edit surfaces`, scope-guard has nothing to hold it to.
func TestRouteGuardBoundedWriterNeedsItsScope(t *testing.T) {
	t.Parallel()
	launch := func(dir, kind, prompt string) support.Result {
		return hook(t, nil, "route-guard", []string{"--agent", "jhon-orchestrator", "worker"},
			map[string]any{"tool_name": "Agent", "tool_input": map[string]any{"subagent_type": kind, "prompt": prompt}, "cwd": dir, "permission_mode": "default"})
	}
	scoped := "Do 1.1.\n\n## Allowed edit surfaces\n\n- src/app.py\n"
	// src/ exists in both, so the listed file passes the launch check.
	folder := realTemp(t)
	mkdir(t, filepath.Join(folder, "src"))
	for _, dir := range []string{newRG(t, "").p, folder} {
		res := launch(dir, "worker", "Do 1.1.")
		code(t, res, 2, "a worker with no scope")
		matches(t, res.Stderr, "launched with its scope", "the refusal names the heading")
		code(t, launch(dir, "worker", scoped), 0, "a worker with its scope")
		code(t, launch(dir, "explore", "Look."), 0, "an agent that is not bounded")
	}
}

// A bounded writer's `## Allowed edit surfaces` names paths that exist
// (T153, L1364 b: a brief named a path that did not, and scope-guard then
// refused the real file): a launch listing one that matches nothing is
// refused before it starts, naming that path. A path matches when a file or
// folder exists there, a glob when it matches one; a writer creating a new
// file lists it, so a path whose parent folder exists matches too. Test
// files and the change's own record are always in scope, so they pass.
func TestRouteGuardWriterSurfacesExist(t *testing.T) {
	t.Parallel()
	r := newRG(t, "")
	outside := realTemp(t)
	support.Write(t, outside, map[string]string{"notes/a.md": "x\n"})
	launch := func(dir string, surfaces ...string) support.Result {
		prompt := "Do 1.1.\n\n## Allowed edit surfaces\n\n- " + strings.Join(surfaces, "\n- ") + "\n"
		payload := map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Agent", "cwd": dir, "session_id": "s", "permission_mode": "default",
			"tool_input": map[string]any{"subagent_type": "worker", "description": "d", "prompt": prompt}}
		return hook(t, r.env, "pre-tool", []string{"--agent", "jhon-orchestrator", "route-guard", "worker"}, payload)
	}
	for label, surfaces := range map[string][]string{
		"an existing file":                    {"src/app.py"},
		"a new file in an existing folder":    {"src/new_module.py"},
		"a file in a new folder (L1413)":      {"src/cache/cache.py", "src/cache/**"},
		"a new top-level file":                {"Makefile"},
		"an existing folder":                  {"src/"},
		"a glob that matches":                 {"src/**"},
		"a glob over an existing folder":      {"src/*.go"},
		"an absolute path":                    {filepath.Join(outside, "notes", "a.md")},
		"a new file in an absolute folder":    {filepath.Join(outside, "notes", "b.md")},
		"a test file in a new folder":         {"tests/unit/test_new.py"},
		"the change's record in a new folder": {"openspec/changes/c9/tasks.md"},
		"a listed note after the path":        {"`src/app.py`: the module"},
	} {
		res := launch(r.p, surfaces...)
		code(t, res, 0, label)
		if res.Stderr != "" {
			t.Errorf("%s: stderr %q", label, res.Stderr)
		}
	}
	for label, c := range map[string]struct {
		surfaces []string
		named    string
	}{
		"a file in a missing folder":         {[]string{"src/app.py", "srcs/parse/app.py"}, "srcs/parse/app.py (its folder srcs does not exist)"},
		"a folder in a missing folder":       {[]string{"pkg/parse/"}, "pkg/parse/ (its folder pkg does not exist)"},
		"a glob that matches nothing":        {[]string{"lib/**/*.py"}, "lib/**/*.py (its folder lib does not exist)"},
		"an absolute path that is not there": {[]string{filepath.Join(outside, "gone", "a.md")}, filepath.Join(outside, "gone", "a.md") + " (its folder " + filepath.Join(outside, "gone") + " does not exist)"},
	} {
		res := launch(r.p, c.surfaces...)
		code(t, res, 2, label)
		want := "route-guard: a jhon-builder is launched with surfaces that exist: its `## Allowed edit surfaces` names " + c.named + ", which matches nothing in " + r.p +
			". A path matches when a file or folder exists there, a glob when it matches one, and a new path when its top-level folder exists. Fix the path in the brief before the launch: scope-guard holds the writer to the paths listed and refuses the real file a wrong one stands for (contracts/efficiency.md, \"Writer scope\").\n"
		if res.Stderr != want {
			t.Errorf("%s: stderr\n%q\nwant\n%q", label, res.Stderr, want)
		}
	}
	two := launch(r.p, "nope/a.py", "src/app.py", "gone/b.py")
	code(t, two, 2, "two missing paths")
	matches(t, two.Stderr, `names nope/a\.py \(its folder nope does not exist\), gone/b\.py \(its folder gone does not exist\), which match nothing in `, "two missing paths")
	// A folder in no repository: its paths are read against the folder.
	folder := realTemp(t)
	support.Write(t, folder, map[string]string{"a/x.py": "x\n"})
	code(t, launch(folder, "a/x.py", "a/y.py"), 0, "a folder's existing and new files")
	code(t, launch(folder, "b/x.py"), 2, "a folder's missing folder")
	// An agent that is not bounded is not checked.
	explore := map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Agent", "cwd": r.p, "session_id": "s", "permission_mode": "default",
		"tool_input": map[string]any{"subagent_type": "explore", "prompt": "Look.\n\n## Allowed edit surfaces\n- nope/a.py\n"}}
	code(t, hook(t, r.env, "pre-tool", []string{"--agent", "jhon-orchestrator", "route-guard", "worker"}, explore), 0, "explore")
}

func TestRouteGuardReviewers(t *testing.T) {
	t.Parallel()
	r := newRG(t, "")
	for _, kind := range []string{"prism-safety", "prism-challenger"} {
		input := map[string]any{"subagent_type": kind, "prompt": "x"}
		top := r.call(t, "Agent", input, true)
		code(t, top, 0, kind+": the owner is asked")
		if d, why, ok := decision(t, top.Stdout); !ok || d != "ask" || !strings.HasPrefix(why, "review") {
			t.Errorf("%s: no ask for review: %q", kind, top.Stdout)
		}
		code(t, r.call(t, "Agent", input, false), 2, kind+": a sub-agent is refused")
	}
	// Judgment Day is removed (T172): a jd-* agent is no reviewer the guard
	// asks for; no runtime renders one to launch.
	for _, top := range []bool{true, false} {
		if res := r.call(t, "Agent", map[string]any{"subagent_type": "jd-judge-a", "prompt": "x"}, top); res.Code != 0 || res.Stdout != "" {
			t.Errorf("a removed Judgment Day agent is no reviewer: %d %q", res.Code, res.Stdout)
		}
	}
	writeGrant(t, r.grants, "review", r.p, 30, nil)
	code(t, r.call(t, "Agent", map[string]any{"subagent_type": "prism-safety", "prompt": "x"}, false), 2, "a review grant is retired: a sub-agent's reviewer is still refused")
}

// The grant classes review and verified are retired (S19, L19, L22): a
// record of either left on disk opens nothing. A reviewer is still asked
// for at the top level, and a standard route opens its PR without verify; a
// design and verify, which no grant ever gated again (S21, S22), pass.
func TestRouteGuardRetiredGrants(t *testing.T) {
	t.Parallel()
	r := newRG(t, "")
	support.Write(t, r.p, map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n",
		"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n", "openspec/changes/c1/specs/a/spec.md": "# a\n", "openspec/changes/c1/execution-record.json": redGreenRecord})
	writeGrant(t, r.grants, "verified", r.p, 30, nil)
	writeGrant(t, r.grants, "review", r.p, 30, nil)

	code(t, r.write(t, "openspec/changes/c1/design.md"), 0, "a design under a retired verified grant")
	code(t, r.write(t, "openspec/changes/c1/proposal.md"), 0, "a proposal under a retired verified grant")
	code(t, r.call(t, "Agent", map[string]any{"subagent_type": "verify", "prompt": "x"}, true), 0, "verify under retired grants on the standard route")

	code(t, r.call(t, "Bash", cmd("gh pr create --title x --body y"), true), 0, "a standard route's PR under a retired verified grant")

	for _, kind := range []string{"prism-safety", "prism-challenger"} {
		input := map[string]any{"subagent_type": kind, "prompt": "Fix it.\n\n## Allowed edit surfaces\n\n- src/app.py\n"}
		if d, why, ok := decision(t, r.call(t, "Agent", input, true).Stdout); !ok || d != "ask" || !strings.HasPrefix(why, "review") {
			t.Errorf("%s under a retired review grant is still asked for", kind)
		}
		res := r.call(t, "Agent", input, false)
		code(t, res, 2, kind+" from a sub-agent under a retired review grant")
		if strings.Contains(res.Stderr, "jhon grant") {
			t.Errorf("%s: the refusal points to no grant: %q", kind, res.Stderr)
		}
	}
}

// `jhon grant review` and `jhon grant verified` are usage errors that write
// nothing, and the usage no longer lists them (S19).
func TestGrantRetiredClasses(t *testing.T) {
	t.Parallel()
	s := newGrantSetup(t)
	for _, cls := range []string{"review", "verified"} {
		res := s.grant(t, "", cls)
		code(t, res, 2, "jhon grant "+cls)
		if strings.Contains(res.Stderr, ", "+cls+",") {
			t.Errorf("the usage lists the retired class %s: %q", cls, res.Stderr)
		}
	}
	code(t, s.grant(t, "", "push", "review"), 2, "a retired class beside a live one")
	if exists(s.grants) {
		t.Error("a retired class wrote the grants directory")
	}
}

// Outside a project with openspec/config.yaml there is no route.
func TestRouteGuardOutsideSDD(t *testing.T) {
	t.Parallel()
	r := newRG(t, "")
	os.Remove(filepath.Join(r.p, "openspec", "config.yaml"))
	code(t, r.write(t, "openspec/changes/c1/tasks.md"), 0, "no config")
	code(t, r.call(t, "Agent", map[string]any{"subagent_type": "verify", "prompt": "x"}, true), 0, "no config, no route")
}

// A PR on the verified route opens only once the branch's change carries
// its independent verification: a verify-report.md that passes; a Judgment
// Day record is none (T172). The refusal names the route and how to verify;
// a standard route opens its PR as before.
func TestRouteGuardVerifyDueBeforeThePR(t *testing.T) {
	t.Parallel()
	pr := cmd("gh pr create --title x --body y")
	// Each record carries its RED before GREEN (T31): the PR needs it.
	record := map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n", "openspec/changes/c1/specs/a/spec.md": "# a\n", "openspec/changes/c1/execution-record.json": redGreenRecord}
	r := newRG(t, "")
	support.Write(t, r.p, map[string]string{"src/app.py": "X = 2\n", "src/migrations/0001_add.sql": "alter table a drop column b;\n", "tests/test_app.py": "def test_a(): pass\n"})
	support.Write(t, r.p, record)
	res := r.call(t, "Bash", pr, true)
	code(t, res, 2, "a verified route without a verification")
	matches(t, res.Stderr, `^route-guard: the route is verified \(risk high reaches the verified tier`, "the refusal names the route's reason")
	matches(t, res.Stderr, `verify is due before delivery`, "the refusal says verify is due")
	matches(t, res.Stderr, "launch the `jhon-checker` agent and record its verify-report.md", "the refusal says how")
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/verify-report.md": "verdict: fail\n"})
	code(t, r.call(t, "Bash", pr, true), 2, "a failing verify-report.md verifies nothing")
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/verify-report.md": "# Verify\n\n**Verdict**: PASS\n"})
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/review-prism.md": "facets: safety\nverdict: pass\n"})
	code(t, r.call(t, "Bash", pr, true), 0, "a passing verify-report.md")

	jd := newRG(t, "")
	support.Write(t, jd.p, map[string]string{"src/app.py": "X = 2\n", "src/migrations/0001_add.sql": "alter table a drop column b;\n"})
	support.Write(t, jd.p, record)
	support.Write(t, jd.p, map[string]string{"openspec/changes/c1/judgments/2026-11-03.md": "## CLOSE\n\n```json\n{\"verdict\":\"APPROVED\"}\n```\n"})
	code(t, jd.call(t, "Bash", pr, true), 2, "a Judgment Day record verifies nothing (T172)")

	std := newRG(t, "")
	support.Write(t, std.p, map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n"})
	support.Write(t, std.p, record)
	code(t, std.call(t, "Bash", pr, true), 0, "a standard route needs no verification")
}

// A commit that closes a slice on the verified route, with no independent
// verification yet, passes with a note for the agent that verify is due
// before delivery; on a standard route, or once verified, it says nothing.
func TestRouteGuardSliceCommitNoticeOnVerifiedRoute(t *testing.T) {
	t.Parallel()
	commit := cmd("git commit -m 'slice 1'")
	closing := func(r rgEnv) {
		support.Write(t, r.p, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [ ] 1.1 a\n"})
		support.Git(t, r.p, "add", "-A")
		support.Git(t, r.p, "commit", "-q", "-m", "plan")
		support.Write(t, r.p, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n"})
	}
	r := newRG(t, "")
	closing(r)
	support.Write(t, r.p, map[string]string{"src/app.py": "X = 2\n", "src/migrations/0001_add.sql": "alter table a drop column b;\n"})
	res := r.call(t, "Bash", commit, false)
	code(t, res, 0, "the slice commit is never refused for verify")
	matches(t, res.Stdout, `"hookEventName":"PreToolUse"`, "a PreToolUse note")
	matches(t, res.Stdout, `"additionalContext":"route verified \(risk high reaches the verified tier[^"]*\): verify is due before delivery; launch the jhon-checker agent and record verify-report.md before the PR"`, "the note")
	code(t, r.call(t, "Bash", cmd("git commit -m 'wip'"), false), 0, "a commit")
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/verify-report.md": "verdict: pass\n"})
	res = r.call(t, "Bash", commit, false)
	code(t, res, 0, "a verified change's slice commit")
	if res.Stdout != "" {
		t.Errorf("a verified change gets no note: %q", res.Stdout)
	}

	std := newRG(t, "")
	closing(std)
	support.Write(t, std.p, map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n"})
	res = std.call(t, "Bash", commit, false)
	code(t, res, 0, "a standard slice commit")
	if res.Stdout != "" {
		t.Errorf("a standard route gets no note: %q", res.Stdout)
	}
}

// suiteNote is the one-line note a whole test suite run by hand gets in
// change c1's project.
const suiteNote = `{"hookSpecificOutput":{"additionalContext":"a whole test suite run by hand: ` + "`jhon executions run c1 --affected`" + ` runs the tests this unit's edits reach and records them (` + "`--affected <base>`" + ` at the slice's close)","hookEventName":"PreToolUse"}}`

// Running the whole test suite by hand is never refused (S24): in an active
// change's project it passes with a one-line note suggesting `jhon
// executions`, which records the run; a focused TDD run, and any run in a
// project with no active change, pass silently.
func TestRouteGuardWholeSuiteByHand(t *testing.T) {
	t.Parallel()
	r := newRG(t, "")
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [ ] 1.1 a\n"})
	silent := func(res support.Result, what string) {
		t.Helper()
		code(t, res, 0, what)
		if res.Stdout != "" || res.Stderr != "" {
			t.Errorf("%s passes silently: %q %q", what, res.Stdout, res.Stderr)
		}
	}
	for _, top := range []bool{true, false} {
		for _, c := range []string{"uv run pytest -m unit", "go test ./..."} {
			res := r.call(t, "Bash", cmd(c), top)
			code(t, res, 0, c+" by hand")
			if strings.TrimSpace(res.Stdout) != suiteNote || res.Stderr != "" {
				t.Errorf("%s: the note:\n got %q %q\nwant %s", c, res.Stdout, res.Stderr, suiteNote)
			}
		}
		silent(r.call(t, "Bash", cmd("pytest tests/test_a.py"), top), "a test file")
		silent(r.call(t, "Bash", cmd("pytest -k x"), top), "a -k filter")
		silent(r.call(t, "Bash", cmd("pytest tests/test_a.py::test_b"), top), "a node")
		silent(r.call(t, "Bash", cmd("jhon executions run c1 --affected main"), top), "jhon executions")
	}

	idle := newRG(t, "")
	silent(idle.call(t, "Bash", cmd("uv run pytest -m unit"), true), "no active change")

	outside := newRG(t, "")
	support.Write(t, outside.p, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [ ] 1.1 a\n"})
	os.Remove(filepath.Join(outside.p, "openspec", "config.yaml"))
	silent(outside.call(t, "Bash", cmd("uv run pytest -m unit"), true), "no openspec/config.yaml")
}

// The whole-suite forms T25 adds (L83) get the same note, never a refusal:
// the working directory by `$(pwd)`, a directory above it, and an option
// whose value the reader does not know.
func TestRouteGuardWholeSuiteMoreForms(t *testing.T) {
	t.Parallel()
	r := newRG(t, "")
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [ ] 1.1 a\n"})
	for _, top := range []bool{true, false} {
		for _, c := range []string{"pytest $(pwd)", "pytest ..", "pytest --log-level DEBUG"} {
			res := r.call(t, "Bash", cmd(c), top)
			code(t, res, 0, c+" by hand")
			if strings.TrimSpace(res.Stdout) != suiteNote || res.Stderr != "" {
				t.Errorf("%s: the note:\n got %q %q\nwant %s", c, res.Stdout, res.Stderr, suiteNote)
			}
		}
		res := r.call(t, "Bash", cmd("cd src && pytest tests/x.py"), top)
		code(t, res, 0, "a test file after cd")
		if res.Stdout != "" || res.Stderr != "" {
			t.Errorf("a test file after cd passes silently: %q %q", res.Stdout, res.Stderr)
		}
	}
}

// A package or a test directory is a focused run in an active change (L12);
// only a whole-project run gets the note, and it is never refused (S24).
func TestRouteGuardPackageAndDirectoryRuns(t *testing.T) {
	t.Parallel()
	r := newRG(t, "")
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [ ] 1.1 a\n"})
	for _, top := range []bool{true, false} {
		code(t, r.call(t, "Bash", cmd("go test ./engine/internal/guard -count=1"), top), 0, "one package")
		code(t, r.call(t, "Bash", cmd("uv run pytest tests/integration -q"), top), 0, "a test directory")
		code(t, r.call(t, "Bash", cmd("pytest --version"), top), 0, "a run of no test")
		res := r.call(t, "Bash", cmd("pytest "+r.p), top)
		code(t, res, 0, "the project root")
		if strings.TrimSpace(res.Stdout) != suiteNote {
			t.Errorf("the project root gets the note: %q", res.Stdout)
		}
	}
}

// Size alone never makes verify due (S25): a 500-line change to documents,
// tests, or code stays standard, and its PR needs no verify.
func TestRouteGuardSizeCountsCodeOnly(t *testing.T) {
	t.Parallel()
	pr := cmd("gh pr create --title x --body y")
	// Each record carries its RED before GREEN (T31): the PR needs it.
	record := map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n", "openspec/changes/c1/specs/a/spec.md": "# a\n", "openspec/changes/c1/execution-record.json": redGreenRecord}
	big := strings.Repeat("x = 1\n", 500)
	for _, f := range []string{"docs/guide.md", "tests/test_big.py"} {
		r := newRG(t, "")
		support.Write(t, r.p, map[string]string{f: big})
		support.Write(t, r.p, record)
		code(t, r.call(t, "Bash", pr, true), 0, f+": 500 lines outside code need no verify")
	}
	r := newRG(t, "")
	support.Write(t, r.p, map[string]string{"src/big.py": big, "tests/test_app.py": "def test_a(): pass\n"})
	support.Write(t, r.p, record)
	res := r.call(t, "Bash", pr, true)
	code(t, res, 0, "500 lines of code need no verify")
	if res.Stderr != "" {
		t.Errorf("500 lines of code are not refused: %q", res.Stderr)
	}
}

// A commit that closes a slice adding more lines of code than the advisory
// size (route_verified_lines, 400 by default) passes with a note that says
// so with the real count and offers to split it; lines of documents do not
// count (S10). Size alone makes no verify due (S25): with no verification
// on record the note is still the size's alone; with verify due by risk,
// both are one note.
func TestRouteGuardSliceCommitOverTheAdvisorySize(t *testing.T) {
	t.Parallel()
	commit := cmd("git commit -m 'slice 1'")
	size := "slice 1 adds 450 lines of code, over the advisory 400: say so to the owner and offer to split it into slices that each build, test, and deliver"
	closing := func(r rgEnv, files map[string]string) {
		support.Write(t, r.p, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [ ] 1.1 a\n"})
		support.Git(t, r.p, "add", "-A")
		support.Git(t, r.p, "commit", "-q", "-m", "plan")
		support.Write(t, r.p, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n"})
		support.Write(t, r.p, files)
	}
	big := strings.Repeat("x = 1\n", 450)

	r := newRG(t, "")
	closing(r, map[string]string{"src/big.py": big, "tests/test_big.py": "def test_a(): pass\n", "openspec/changes/c1/verify-report.md": "verdict: pass\n"})
	res := r.call(t, "Bash", commit, false)
	code(t, res, 0, "a slice over the advisory size is never refused")
	if want := `{"hookSpecificOutput":{"additionalContext":"` + size + `","hookEventName":"PreToolUse"}}`; strings.TrimSpace(res.Stdout) != want {
		t.Errorf("the size note:\n got %s\nwant %s", res.Stdout, want)
	}

	unverified := newRG(t, "")
	closing(unverified, map[string]string{"src/big.py": big, "tests/test_big.py": "def test_a(): pass\n"})
	res = unverified.call(t, "Bash", commit, false)
	code(t, res, 0, "over the advisory size with no verification")
	if want := `{"hookSpecificOutput":{"additionalContext":"` + size + `","hookEventName":"PreToolUse"}}`; strings.TrimSpace(res.Stdout) != want {
		t.Errorf("size alone makes no verify due:\n got %s\nwant %s", res.Stdout, want)
	}

	both := newRG(t, "")
	closing(both, map[string]string{"src/big.py": big, "src/migrations/0001_add.sql": "alter table a drop column b;\n", "tests/test_big.py": "def test_a(): pass\n"})
	res = both.call(t, "Bash", commit, false)
	code(t, res, 0, "verify due and over the advisory size")
	if n := strings.Count(res.Stdout, "hookSpecificOutput"); n != 1 {
		t.Errorf("one note, got %d: %s", n, res.Stdout)
	}
	matches(t, res.Stdout, `"additionalContext":"route verified \(risk high reaches the verified tier \(high\)\): verify is due before delivery; launch the jhon-checker agent and record verify-report.md before the PR; slice 1 adds 451 lines of code, over the advisory 400`, "both sentences in one note")

	docs := newRG(t, "")
	closing(docs, map[string]string{"docs/guide.md": big})
	res = docs.call(t, "Bash", commit, false)
	code(t, res, 0, "450 lines of docs")
	if res.Stdout != "" {
		t.Errorf("450 lines of docs get no note: %q", res.Stdout)
	}
}

// On a risk-high route Prism review is due with verify (S17, L9): the PR waits for a
// passing review-prism.md as well as the verification, and a Prism reviewer runs
// without asking the owner, who approved it in L9. A large change is not
// verified by its size (S25) and needs neither, and a standard route still
// asks before a reviewer.
func TestRouteGuardPrismByRisk(t *testing.T) {
	t.Parallel()
	pr := cmd("gh pr create --title x --body y")
	// Each record carries its RED before GREEN (T31): the PR needs it.
	record := map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n", "openspec/changes/c1/specs/a/spec.md": "# a\n", "openspec/changes/c1/execution-record.json": redGreenRecord}
	r := newRG(t, "")
	support.Write(t, r.p, map[string]string{"src/migrations/0001_add.sql": "alter table a drop column b;\n", "tests/test_app.py": "def test_a(): pass\n"})
	support.Write(t, r.p, record)
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/verify-report.md": "verdict: pass\n"})
	res := r.call(t, "Bash", pr, true)
	code(t, res, 2, "risk high with verify and no Prism review")
	matches(t, res.Stderr, `^route-guard: Prism review is due before delivery \(risk high reaches the verified tier \(high\)\): run the pertinent facets`, "the refusal names Prism review and why")
	matches(t, res.Stderr, "record openspec/changes/c1/review-prism.md with the facets it ran \\(`facets: safety`\\) and `verdict: pass`", "the refusal says how")
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/review-prism.md": "facets: safety\nverdict: fail\n"})
	code(t, r.call(t, "Bash", pr, true), 2, "a failing Prism review")
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/review-prism.md": "verdict: pass\n"})
	code(t, r.call(t, "Bash", pr, true), 2, "a Prism review that names no facet")
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/review-prism.md": "facets: safety, failure\nverdict: pass\n"})
	code(t, r.call(t, "Bash", pr, true), 0, "verify and Prism review on record")

	for _, top := range []bool{true, false} {
		res := r.call(t, "Agent", map[string]any{"subagent_type": "prism-safety", "prompt": "x"}, top)
		code(t, res, 0, "a Prism reviewer on a risk-high route")
		if res.Stdout != "" {
			t.Errorf("no ask for a Prism reviewer on a risk-high route: %q", res.Stdout)
		}
	}

	size := newRG(t, "")
	support.Write(t, size.p, map[string]string{"src/big.py": strings.Repeat("x = 1\n", 500), "tests/test_app.py": "def test_a(): pass\n"})
	support.Write(t, size.p, record)
	code(t, size.call(t, "Bash", pr, true), 0, "a large change: neither verify nor Prism review")
	top := size.call(t, "Agent", map[string]any{"subagent_type": "prism-safety", "prompt": "x"}, true)
	if d, _, ok := decision(t, top.Stdout); !ok || d != "ask" {
		t.Errorf("a reviewer on a large change is asked for: %q", top.Stdout)
	}

	std := newRG(t, "")
	support.Write(t, std.p, map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n"})
	top = std.call(t, "Agent", map[string]any{"subagent_type": "prism-safety", "prompt": "x"}, true)
	if d, _, ok := decision(t, top.Stdout); !ok || d != "ask" {
		t.Errorf("a reviewer on a standard route is asked for: %q", top.Stdout)
	}
}

// review_required in openspec/config.yaml always wins over the route (S11,
// L9): a verify it names for the change holds the PR until its report
// passes, even on a standard route; an area applies when the diff touches a
// path its glob matches, and not otherwise.
func TestRouteGuardReviewRequired(t *testing.T) {
	t.Parallel()
	pr := cmd("gh pr create --title x --body y")
	// Each record carries its RED before GREEN (T31): the PR needs it.
	record := map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n", "openspec/changes/c1/specs/a/spec.md": "# a\n", "openspec/changes/c1/execution-record.json": redGreenRecord}
	small := map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n"}

	r := newRG(t, "review_required:\n  - change: c1\n    review: verify\n")
	support.Write(t, r.p, small)
	support.Write(t, r.p, record)
	res := r.call(t, "Bash", pr, true)
	code(t, res, 2, "a standard route whose change requires verify")
	matches(t, res.Stderr, `^route-guard: checker is due before delivery \(review_required: change c1\): `, "the refusal names the rule")
	matches(t, res.Stderr, "record its verify-report.md \\(`verdict: pass`\\) under openspec/changes/c1/ before the PR \\(contracts/ldd.md", "the refusal says how")
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/verify-report.md": "verdict: pass\n"})
	code(t, r.call(t, "Bash", pr, true), 0, "the required verify on record")

	other := newRG(t, "review_required:\n  - change: c2\n    review: verify\n")
	support.Write(t, other.p, small)
	support.Write(t, other.p, record)
	code(t, other.call(t, "Bash", pr, true), 0, "a rule for another change")

	area := newRG(t, "review_required:\n  - area: \"src/**\"\n    review: 4r\n")
	support.Write(t, area.p, small)
	support.Write(t, area.p, record)
	res = area.call(t, "Bash", pr, true)
	code(t, res, 2, "an area the diff touches")
	matches(t, res.Stderr, `^route-guard: Prism review is due before delivery \(review_required: area src/\*\*\)`, "the refusal names the area")
	res = area.call(t, "Agent", map[string]any{"subagent_type": "prism-behavior", "prompt": "x"}, true)
	if code(t, res, 0, "a required Prism reviewer"); res.Stdout != "" {
		t.Errorf("a pertinent Prism reviewer review_required asks for runs without asking (L46): %q", res.Stdout)
	}
	support.Write(t, area.p, map[string]string{"openspec/changes/c1/review-prism.md": "facets: behavior\nverdict: pass\n"})
	code(t, area.call(t, "Bash", pr, true), 0, "the required Prism review on record")

	elsewhere := newRG(t, "review_required:\n  - area: \"lib/**\"\n    review: verify\n")
	support.Write(t, elsewhere.p, small)
	support.Write(t, elsewhere.p, record)
	code(t, elsewhere.call(t, "Bash", pr, true), 0, "an area the diff does not touch")

	verify := newRG(t, "review_required:\n  - area: \"src/**\"\n    review: verify\n")
	support.Write(t, verify.p, small)
	support.Write(t, verify.p, record)
	code(t, verify.call(t, "Agent", map[string]any{"subagent_type": "verify", "prompt": "x"}, true), 0, "a required verify on a standard route")
	res = verify.call(t, "Bash", pr, true)
	code(t, res, 2, "a required verify")
	matches(t, res.Stderr, `^route-guard: checker is due before delivery \(review_required: area src/\*\*\)`, "the refusal names the area")
}

// review_required is read in the YAML forms a person writes: a list at
// column 0, trailing comments, a flow mapping, and review names in any case
// (L17). Each holds the PR of a standard-route change until its verify
// report passes.
func TestRouteGuardReviewRequiredYAMLForms(t *testing.T) {
	t.Parallel()
	pr := cmd("gh pr create --title x --body y")
	// Each record carries its RED before GREEN (T31): the PR needs it.
	record := map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n", "openspec/changes/c1/specs/a/spec.md": "# a\n", "openspec/changes/c1/execution-record.json": redGreenRecord}
	small := map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n"}
	for _, config := range []string{
		"review_required:\n- change: c1\n  review: verify\n",
		"review_required:\n  - change: c1 # billing\n    review: verify  # owner\n",
		"review_required:\n  - {change: c1, review: verify}\n",
		"review_required:\n  - change: c1\n    review: VERIFY\n",
	} {
		r := newRG(t, config)
		support.Write(t, r.p, small)
		support.Write(t, r.p, record)
		res := r.call(t, "Bash", pr, true)
		code(t, res, 2, "review_required "+strings.ReplaceAll(config, "\n", `\n`))
		matches(t, res.Stderr, `^route-guard: checker is due before delivery \(review_required: change c1\): `, "the refusal names the rule")
		support.Write(t, r.p, map[string]string{"openspec/changes/c1/verify-report.md": "verdict: pass\n"})
		code(t, r.call(t, "Bash", pr, true), 0, "the verify report passes, "+strings.ReplaceAll(config, "\n", `\n`))
	}
}

// An entry of review_required jhon cannot read is never ignored: the PR is
// refused naming its line and why (L17); `jd` is no review since Judgment
// Day was removed (T172).
func TestRouteGuardReviewRequiredUnreadable(t *testing.T) {
	t.Parallel()
	pr := cmd("gh pr create --title x --body y")
	// Each record carries its RED before GREEN (T31): the PR needs it.
	record := map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n", "openspec/changes/c1/specs/a/spec.md": "# a\n", "openspec/changes/c1/execution-record.json": redGreenRecord}
	small := map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n"}
	for config, line := range map[string]string{
		"review_required:\n  - change: c1\n    review: 5r\n":            `line 3: review: 5r`,
		"review_required:\n  - change: c1\n    this is not yaml\n":      `line 3: this is not yaml`,
		"review_required:\n  - change: c1\n    review: jd\n":            `line 3: review: jd`,
		"review_required:\n  - review: 4r\n":                            `line 2: - review: 4r`,
		"review_required:\n  - change: c2\n    review: 4r\n  garbage\n": `line 4: garbage`,
	} {
		r := newRG(t, config)
		support.Write(t, r.p, small)
		support.Write(t, r.p, record)
		res := r.call(t, "Bash", pr, true)
		code(t, res, 2, "an unreadable entry "+line)
		matches(t, res.Stderr, `^route-guard: review_required in openspec/config.yaml has an entry jhon cannot read \(`+regexp.QuoteMeta(line)+`\): .+; fix it before the PR`, "the refusal names the line")
	}
}

// An area with a leading `/` matches from the repository root (L17).
func TestRouteGuardReviewRequiredRootedArea(t *testing.T) {
	t.Parallel()
	r := newRG(t, "review_required:\n  - area: /src/**\n    review: 4r\n")
	support.Write(t, r.p, map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n",
		"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n", "openspec/changes/c1/specs/a/spec.md": "# a\n",
		"openspec/changes/c1/execution-record.json": redGreenRecord})
	res := r.call(t, "Bash", cmd("gh pr create --title x --body y"), true)
	code(t, res, 2, "a rooted area the diff touches")
	matches(t, res.Stderr, `^route-guard: Prism review is due before delivery \(review_required: area /src/\*\*\)`, "the refusal names the area")
}

// verify confirms a Prism review (S18, L19): when Prism review is due it runs on any route,
// and with neither Prism review nor verify due it runs too (S22).
func TestRouteGuardVerifyConfirmsPrism(t *testing.T) {
	t.Parallel()
	r := newRG(t, "review_required:\n  - area: \"src/**\"\n    review: 4r\n")
	support.Write(t, r.p, map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n"})
	code(t, r.call(t, "Agent", map[string]any{"subagent_type": "verify", "prompt": "x"}, false), 0, "verify when Prism review is due on a standard route")
	std := newRG(t, "review_required:\n  - area: \"docs/**\"\n    review: 4r\n")
	support.Write(t, std.p, map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n"})
	code(t, std.call(t, "Agent", map[string]any{"subagent_type": "verify", "prompt": "x"}, false), 0, "verify when nothing is due")
}

// A Prism review runs only the facets the diff calls for (L46): a migration calls for
// risk alone, so prism-safety runs without asking, while prism-clarity
// and prism-failure are asked of the owner at the top level and refused
// from a sub-agent; the Prism refusal names the pertinent facets.
func TestRouteGuardPrismOnlyPertinentFacets(t *testing.T) {
	t.Parallel()
	r := newRG(t, "")
	support.Write(t, r.p, map[string]string{"src/migrations/0001_add.sql": "alter table a drop column b;\n", "tests/test_app.py": "def test_a(): pass\n"})
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n", "openspec/changes/c1/specs/a/spec.md": "# a\n", "openspec/changes/c1/verify-report.md": "verdict: pass\n",
		"openspec/changes/c1/execution-record.json": redGreenRecord})
	res := r.call(t, "Bash", cmd("gh pr create --title x --body y"), true)
	code(t, res, 2, "Prism review due")
	matches(t, res.Stderr, "run the pertinent facets \\(safety\\)", "the refusal names the facets the diff calls for")
	matches(t, res.Stderr, "\\(`facets: safety`\\)", "the example is the pertinent facets")
	ok := r.call(t, "Agent", map[string]any{"subagent_type": "prism-safety", "prompt": "x"}, false)
	code(t, ok, 0, "a pertinent facet")
	if ok.Stdout != "" || ok.Stderr != "" {
		t.Errorf("a pertinent facet runs silently: %q %q", ok.Stdout, ok.Stderr)
	}
	for _, facet := range []string{"prism-clarity", "prism-failure"} {
		top := r.call(t, "Agent", map[string]any{"subagent_type": facet, "prompt": "x"}, true)
		code(t, top, 0, facet+" at the top level")
		if d, why, ok := decision(t, top.Stdout); !ok || d != "ask" || !strings.Contains(why, "not pertinent to this change") || !strings.Contains(why, "(safety)") {
			t.Errorf("%s is asked of the owner: %q", facet, top.Stdout)
		}
		sub := r.call(t, "Agent", map[string]any{"subagent_type": facet, "prompt": "x"}, false)
		code(t, sub, 2, facet+" from a sub-agent")
	}
}

// Claude Code's native Prism review is one Bash call (T118d, L1150 K3): the Prism review
// refusal names it beside pi's tool, and the guard admits `jhon review run`
// silently when the Prism review is due; the facet agents stay admitted as its fallback.
func TestRouteGuardPrismNativeInClaudeCode(t *testing.T) {
	t.Parallel()
	r := newRG(t, "")
	support.Write(t, r.p, map[string]string{"src/migrations/0001_add.sql": "alter table a drop column b;\n", "tests/test_app.py": "def test_a(): pass\n"})
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n", "openspec/changes/c1/specs/a/spec.md": "# a\n", "openspec/changes/c1/verify-report.md": "verdict: pass\n",
		"openspec/changes/c1/execution-record.json": redGreenRecord})
	res := r.call(t, "Bash", cmd("gh pr create --title x --body y"), true)
	code(t, res, 2, "Prism review due")
	matches(t, res.Stderr, "run the pertinent facets \\(safety\\) with the native review \\(`jhon_review` in pi, `jhon review run` in Claude Code; the facet agents when it cannot run\\)", "the refusal names the native review per runtime")
	for _, c := range []string{"jhon review run", "jhon review run --base main", "cd " + r.p + " && jhon review run --change c1"} {
		ok := r.call(t, "Bash", cmd(c), true)
		code(t, ok, 0, c)
		if ok.Stdout != "" || ok.Stderr != "" {
			t.Errorf("%s runs silently when the Prism review is due: %q %q", c, ok.Stdout, ok.Stderr)
		}
	}
	code(t, r.call(t, "Agent", map[string]any{"subagent_type": "prism-safety", "prompt": "x"}, true), 0, "the fallback facet agent")
}

// redGreenRecord is an execution record whose last run passed after a
// failing one: the change's RED before its GREEN (T31), with a passing light
// verify of the tree it tested (T31.5), as a standard-route PR needs.
const redGreenRecord = `{"timestamp":"2026-01-01T00:01:00.000Z","complete":true,"scope":"full","candidate_tree":"t1","executions":[{"project":".","command":"true","status":"pass","exit_code":0}],"history":[{"timestamp":"2026-01-01T00:00:00.000Z","passed":false}],"light_verify":[{"timestamp":"2026-01-01T00:02:00.000Z","verdict":"pass","commit":"","tree":"t1"}]}` + "\n"

// redGreenUnverified is redGreenRecord with light_verify notes lv (JSON).
func redGreenUnverified(lv string) string {
	return `{"timestamp":"2026-01-01T00:01:00.000Z","complete":true,"scope":"full","candidate_tree":"t1","executions":[{"project":".","command":"true","status":"pass","exit_code":0}],"history":[{"timestamp":"2026-01-01T00:00:00.000Z","passed":false}]` + lv + "}\n"
}

// redGreen writes redGreenRecord at rel under root.
func redGreen(t *testing.T, root, rel string) {
	t.Helper()
	support.Write(t, root, map[string]string{rel: redGreenRecord})
}

// TDD always (T31, L252, L311): a PR of a change that alters behaviour opens
// only with its RED before GREEN in `jhon executions`, folder or not; a
// change to tests and documents alone needs none, as before.
func TestRouteGuardPRNeedsRedBeforeGreen(t *testing.T) {
	t.Parallel()
	pr := cmd("gh pr create --title x --body y")
	r := newRG(t, "")
	support.Write(t, r.p, map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n",
		"openspec/changes/c1/tasks.md": "## Intent\nDelivers: X is 2\n"})
	res := r.call(t, "Bash", pr, true)
	code(t, res, 2, "a written record and no runs")
	matches(t, res.Stderr, "^route-guard: this change alters behaviour, and its RED before GREEN is not on record \\(LDD, contracts/ldd.md, TDD always\\): run `jhon executions run c1 --affected <base>` while its new test fails \\(RED\\), a unit's run \\(`--affected`\\) between, and `--affected <base>` again once it passes \\(GREEN\\), where <base> is the commit the slice started from \\(with none, a full `jhon executions run c1`\\), before the PR. Route: standard: ", "the refusal names jhon executions")
	redGreen(t, r.p, "openspec/changes/c1/execution-record.json")
	code(t, r.call(t, "Bash", pr, true), 0, "RED before GREEN on record")

	passive := newRG(t, "")
	support.Write(t, passive.p, map[string]string{"docs/guide.md": "# guide\n", "tests/test_app.py": "def test_x(): pass\n"})
	code(t, passive.call(t, "Bash", pr, true), 0, "tests and documents alone need no runs")

	small := newRG(t, "")
	support.Git(t, small.p, "checkout", "-q", "-b", "fix/small")
	support.Write(t, small.p, map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n"})
	res = small.call(t, "Bash", pr, true)
	code(t, res, 2, "a folderless change with no RED")
	// T146h L4: a small change's refusal names the bare run, twice, nothing between.
	matches(t, res.Stderr, "run `jhon executions run` while its new test fails \\(RED\\), then `jhon executions run` again once it passes \\(GREEN\\): two runs, nothing between, before the PR", "a small change is told the bare run")
	redGreen(t, small.p, ".git/jhon/changes/fix-small/execution-record.json")
	code(t, small.call(t, "Bash", pr, true), 0, "a folderless change with its RED before GREEN")

	// T146g G3 (L1274): a small change keeps no light verify: its RED
	// before GREEN with none is enough.
	support.Write(t, small.p, map[string]string{".git/jhon/changes/fix-small/execution-record.json": redGreenUnverified("")})
	res = small.call(t, "Bash", pr, true)
	code(t, res, 0, "a small change with no light verify")
	if got := adviceOf(t, res.Stdout); got != "" {
		t.Errorf("a small change keeps no light verify, not even a note: %q", got)
	}
}

// lightVerifyNote is route-guard's advisory on a standard-route PR of change
// with no passing light verify of the tree its last run tested (T154).
func lightVerifyNote(change string) string {
	return notePrefix + "the PR runs with no passing light verify of the tree its last run tested on record for " + change +
		" (contracts/ldd.md, \"Light verify\"): an advisory, as no light verify has caught a defect on record; the `jhon-checker` agent in light mode records one with `jhon executions verify " + change + " --verdict pass|fail`"
}

// A light verify at slice close (T31.5, L311 (1)): on the standard route a
// PR of a change that alters behaviour with no passing light verify of the
// tree its last run tested runs with a note (T154, L1394: no recorded catch);
// the verified route keeps its full verify-report rule, and a passive change
// is told nothing.
func TestRouteGuardPRLightVerifyIsAnAdvisory(t *testing.T) {
	t.Parallel()
	pr := cmd("gh pr create --title x --body y")
	rec := "openspec/changes/c1/execution-record.json"
	r := newRG(t, "")
	support.Write(t, r.p, map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n",
		"openspec/changes/c1/tasks.md": "## Intent\nDelivers: X is 2\n"})
	for _, lv := range []string{
		"",
		`,"light_verify":[{"timestamp":"2026-01-01T00:02:00.000Z","verdict":"fail","commit":"","tree":"t1"}]`,
		`,"light_verify":[{"timestamp":"2026-01-01T00:02:00.000Z","verdict":"pass","commit":"","tree":"t0"}]`,
		`,"light_verify":[{"timestamp":"2026-01-01T00:02:00.000Z","verdict":"pass","commit":"","tree":"t1"},{"timestamp":"2026-01-01T00:03:00.000Z","verdict":"fail","commit":"","tree":"t1"}]`,
	} {
		support.Write(t, r.p, map[string]string{rec: redGreenUnverified(lv)})
		res := r.call(t, "Bash", pr, true)
		code(t, res, 0, "no passing light verify of the tested tree: "+lv)
		if got := adviceOf(t, res.Stdout); got != lightVerifyNote("c1") {
			t.Errorf("%s: note %q; want %q", lv, got, lightVerifyNote("c1"))
		}
	}
	support.Write(t, r.p, map[string]string{rec: redGreenUnverified(`,"light_verify":[{"timestamp":"2026-01-01T00:02:00.000Z","verdict":"pass","commit":"","tree":"t1"}]`)})
	res := r.call(t, "Bash", pr, true)
	code(t, res, 0, "a passing light verify of the tested tree")
	if got := adviceOf(t, res.Stdout); got != "" {
		t.Errorf("a passing light verify: note %q", got)
	}

	// The verified route keeps its verify-report rule: no light verify asked.
	high := newRG(t, "")
	support.Write(t, high.p, map[string]string{"src/app.py": "X = 2\n", "src/migrations/0001_add.sql": "alter table a drop column b;\n", "tests/test_app.py": "def test_a(): pass\n", "openspec/changes/c1/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n",
		"openspec/changes/c1/specs/a/spec.md": "# a\n", rec: redGreenUnverified("")})
	res = high.call(t, "Bash", pr, true)
	code(t, res, 2, "verified route without its verify-report")
	matches(t, res.Stderr, `the route is verified \(.*\): verify is due before delivery`, "the verified route's own rule")
	support.Write(t, high.p, map[string]string{"openspec/changes/c1/verify-report.md": "verdict: pass\n", "openspec/changes/c1/review-prism.md": "facets: safety\nverdict: pass\n"})
	res = high.call(t, "Bash", pr, true)
	code(t, res, 0, "the verified route needs no light verify")
	if got := adviceOf(t, res.Stdout); got != "" {
		t.Errorf("the verified route: note %q", got)
	}

	passive := newRG(t, "")
	support.Write(t, passive.p, map[string]string{"docs/guide.md": "# guide\n", "tests/test_app.py": "def test_x(): pass\n", rec: redGreenUnverified("")})
	res = passive.call(t, "Bash", pr, true)
	code(t, res, 0, "a passive change needs no light verify")
	if got := adviceOf(t, res.Stdout); got != "" {
		t.Errorf("a passive change: note %q", got)
	}
}

// A Prism review recorded by `jhon review record` (T118a) binds to its subject: the
// PR opens while a fresh freeze gives that subject, and waits again once the
// candidate changes; a record without `subject:` is read as before.
func TestRouteGuardPrismSubject(t *testing.T) {
	t.Parallel()
	pr := cmd("gh pr create --title x --body y")
	r := newRG(t, "")
	support.Write(t, r.p, map[string]string{"src/migrations/0001_add.sql": "alter table a drop column b;\n", "tests/test_app.py": "def test_a(): pass\n",
		"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n", "openspec/changes/c1/specs/a/spec.md": "# a\n",
		"openspec/changes/c1/execution-record.json": redGreenRecord, "openspec/changes/c1/verify-report.md": "verdict: pass\n"})
	review := func(stdin string, args ...string) support.Result {
		return support.Run(t, support.Cmd{Args: append([]string{support.Bin(t), "review"}, args...), Dir: r.p, Env: r.env, Stdin: stdin})
	}
	start := review("", "start", "--json")
	code(t, start, 0, "start")
	var s struct {
		Subject string
		Facets  []struct{ Facet string }
	}
	if err := json.Unmarshal([]byte(start.Stdout), &s); err != nil {
		t.Fatal(err)
	}
	var facets []any
	for _, l := range s.Facets {
		facets = append(facets, map[string]any{"subject_hash": s.Subject, "facet": l.Facet, "inspection": map[string]any{"status": "completed", "paths": []string{}},
			"findings": []any{}, "evidence": []string{"read"}})
	}
	in, _ := json.Marshal(map[string]any{"facets": facets, "challenger": map[string]any{"subject_hash": s.Subject, "results": []any{}},
		"arbiter": map[string]any{"subject_hash": s.Subject, "results": []any{}}})
	code(t, review(string(in), "record", "--subject", s.Subject), 0, "record")
	code(t, r.call(t, "Bash", pr, true), 0, "a record whose subject is the fresh one")
	support.Write(t, r.p, map[string]string{"src/migrations/0001_add.sql": "alter table a drop column c;\n"})
	res := r.call(t, "Bash", pr, true)
	code(t, res, 2, "a stale record")
	matches(t, res.Stderr, `^route-guard: Prism review is due before delivery`, "the stale record is no Prism review")
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/review-prism.md": "facets: safety\nverdict: pass\n"})
	code(t, r.call(t, "Bash", pr, true), 0, "a record without subject:")
}

// The subject's edges (T118a verify, L732): after a fresh native record,
// every change to the candidate — the change's own tasks, an untracked file,
// a delete, a rename, a binary edit, a mode change, a symlink, a stray
// review-prism.md outside the change — and every tampering with the record's
// base: or subject: lines refuses the PR; undoing it opens the PR again,
// and committing everything keeps the subject.
func TestRouteGuardPrismSubjectEdges(t *testing.T) {
	t.Parallel()
	pr := cmd("gh pr create --title x --body y")
	r := newRG(t, "")
	tasks := "## Slice 1: a\n- [x] 1.1 a\n"
	bin := "\x00\x01\x02binary\xff"
	support.Write(t, r.p, map[string]string{"src/migrations/0001_add.sql": "alter table a drop column b;\n", "tests/test_app.py": "def test_a(): pass\n",
		"openspec/changes/c1/tasks.md": tasks, "openspec/changes/c1/specs/a/spec.md": "# a\n", "src/data.bin": bin,
		"openspec/changes/c1/execution-record.json": redGreenRecord, "openspec/changes/c1/verify-report.md": "verdict: pass\n"})
	review := func(stdin string, args ...string) support.Result {
		return support.Run(t, support.Cmd{Args: append([]string{support.Bin(t), "review"}, args...), Dir: r.p, Env: r.env, Stdin: stdin})
	}
	start := review("", "start", "--json")
	code(t, start, 0, "start")
	var s struct {
		Subject string
		Facets  []struct{ Facet string }
	}
	if err := json.Unmarshal([]byte(start.Stdout), &s); err != nil {
		t.Fatal(err)
	}
	var facets []any
	for _, l := range s.Facets {
		facets = append(facets, map[string]any{"subject_hash": s.Subject, "facet": l.Facet, "inspection": map[string]any{"status": "completed", "paths": []string{}},
			"findings": []any{}, "evidence": []string{"read"}})
	}
	in, _ := json.Marshal(map[string]any{"facets": facets, "challenger": map[string]any{"subject_hash": s.Subject, "results": []any{}},
		"arbiter": map[string]any{"subject_hash": s.Subject, "results": []any{}}})
	code(t, review(string(in), "record", "--subject", s.Subject), 0, "record")
	code(t, r.call(t, "Bash", pr, true), 0, "a fresh native record")
	recPath := filepath.Join(r.p, "openspec/changes/c1/review-prism.md")
	rec := support.Read(t, recPath)
	abs := func(rel string) string { return filepath.Join(r.p, rel) }
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	setRec := func(f func(string) string) func() {
		return func() { must(os.WriteFile(recPath, []byte(f(rec)), 0o644)) }
	}
	baseLine := regexp.MustCompile(`(?m)^base: .*\n`)
	subjectLine := regexp.MustCompile(`(?m)^subject: .*$`)
	cases := []struct {
		name         string
		break_, mend func()
	}{
		{"an edit to the change's tasks.md", func() {
			support.Write(t, r.p, map[string]string{"openspec/changes/c1/tasks.md": tasks + "- [x] 1.2 b\n"})
		},
			func() { support.Write(t, r.p, map[string]string{"openspec/changes/c1/tasks.md": tasks}) }},
		{"an untracked src/new.py", func() { support.Write(t, r.p, map[string]string{"src/new.py": "Y = 1\n"}) }, func() { must(os.Remove(abs("src/new.py"))) }},
		{"a deleted file", func() { must(os.Remove(abs("src/app.py"))) }, func() { support.Write(t, r.p, map[string]string{"src/app.py": "X = 1\n"}) }},
		{"a renamed file", func() { must(os.Rename(abs("src/app.py"), abs("src/app2.py"))) }, func() { must(os.Rename(abs("src/app2.py"), abs("src/app.py"))) }},
		{"a binary edit", func() { must(os.WriteFile(abs("src/data.bin"), []byte("\x00\x01\x03binary\xff"), 0o644)) },
			func() { must(os.WriteFile(abs("src/data.bin"), []byte(bin), 0o644)) }},
		{"chmod +x", func() { must(os.Chmod(abs("src/app.py"), 0o755)) }, func() { must(os.Chmod(abs("src/app.py"), 0o644)) }},
		{"a symlink", func() { must(os.Symlink("app.py", abs("src/link.py"))) }, func() { must(os.Remove(abs("src/link.py"))) }},
		{"src/review-prism.md", func() {
			support.Write(t, r.p, map[string]string{"src/review-prism.md": "facets: safety\nverdict: pass\n"})
		},
			func() { must(os.Remove(abs("src/review-prism.md"))) }},
		{"the base: line removed", setRec(func(s string) string { return baseLine.ReplaceAllString(s, "") }), setRec(func(s string) string { return s })},
		{"base: 0000…01", setRec(func(s string) string { return baseLine.ReplaceAllString(s, "base: "+strings.Repeat("0", 39)+"1\n") }), setRec(func(s string) string { return s })},
		{"subject: abc", setRec(func(s string) string { return subjectLine.ReplaceAllString(s, "subject: abc") }), setRec(func(s string) string { return s })},
	}
	for _, c := range cases {
		c.break_()
		res := r.call(t, "Bash", pr, true)
		code(t, res, 2, c.name)
		matches(t, res.Stderr, `^route-guard: Prism review is due before delivery`, c.name+": the record is no Prism review")
		c.mend()
		code(t, r.call(t, "Bash", pr, true), 0, c.name+" undone")
	}
	support.Git(t, r.p, "add", "-A")
	support.Git(t, r.p, "commit", "-q", "-m", "c1")
	code(t, r.call(t, "Bash", pr, true), 0, "committing everything keeps the subject")
}

// rgPayloads are route-guard's Bash call as each runtime sends it: Claude
// Code's main session (permission_mode) and a pi child (agent_id, no mode).
func rgPayloads(repo, command string) map[string]map[string]any {
	claude := map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": cmd(command), "cwd": repo, "session_id": "s", "permission_mode": "default"}
	pi := map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": cmd(command), "cwd": repo, "session_id": "s", "agent_id": "pi-child"}
	return map[string]map[string]any{"claude": claude, "pi": pi}
}

// Delivery is read from the parsed command (T109 WU2, L562, L784 a): a
// command that only names a commit or a PR, or runs a git subcommand that
// is not one, is not delivery, in a repository with no layout and on the
// LDD branch alike.
func TestRouteGuardDeliveryFalseRefusals(t *testing.T) {
	t.Parallel()
	repo, env := noLayoutRepo(t)
	r := newRG(t, "")
	support.Write(t, r.p, map[string]string{"src/app.py": "X = 2\n"})
	for _, c := range []string{
		"git commit --help",
		"git commit-tree HEAD^{tree} -m x",
		`echo "git commit later"`,
		"git log --grep 'gh pr create'",
		"cat <<'EOF'\ngit commit -m x\ngh pr create\nEOF",
		"ls # then git commit and gh pr create",
		"git --help commit",
		"gh pr create --help",
		"grep -rn 'gh pr create' docs",
		"git -c alias.ci=commit log",
	} {
		for rt, p := range rgPayloads(repo, c) {
			code(t, hook(t, env, "route-guard", nil, p), 0, rt+" no layout: "+c)
		}
		for rt, p := range rgPayloads(r.p, c) {
			code(t, hook(t, r.env, "route-guard", nil, p), 0, rt+" LDD branch: "+c)
		}
	}
}

// Every spelling of a commit or a PR is delivery (L686: delivery without
// proof stays refused): git's global options, env and command wrappers,
// gh's -R and `pr new`, a shell's -c string, eval, xargs, and an alias the
// line defines; a command the reader cannot read is judged as delivery.
func TestRouteGuardDeliveryMisses(t *testing.T) {
	t.Parallel()
	repo, env := noLayoutRepo(t)
	r := newRG(t, "")
	support.Write(t, r.p, map[string]string{"src/app.py": "X = 2\n"})
	commits := []string{
		"git -c k=v commit -m x",
		"git --no-pager commit -m x",
		"git -C . -c x=y commit -m x",
		"GIT_DIR=.git git commit -m x",
		"command git commit -m x",
		"env git commit -m x",
		"echo x | xargs git commit -m",
		"git -c alias.ci=commit ci -m x",
		"git -c 'alias.ci=!git commit' ci -m x",
		"git $SUB -m x",
		"$GIT commit -m x",
	}
	prs := []string{
		"gh pr new --title x --body y",
		"gh -R owner/repo pr create --title x --body y",
		"gh pr -R owner/repo create --title x",
		"bash -c 'gh pr create --title x'",
		"sh -c \"gh pr create --title x\"",
		"eval gh pr create --title x",
		"gh pr create --title \"$(cat <<'EOF'\nx\nEOF\n)\" --body y",
		"echo \"$(\" ; gh pr create",
	}
	for _, c := range append(slices.Clone(commits), prs...) {
		for rt, p := range rgPayloads(repo, c) {
			res := hook(t, env, "route-guard", nil, p)
			code(t, res, 2, rt+" no layout: "+c)
			if !strings.Contains(res.Stderr, "LDD is not set up in this project") {
				t.Errorf("%s no layout %q: %q", rt, c, res.Stderr)
			}
		}
	}
	for _, c := range prs {
		for rt, p := range rgPayloads(r.p, c) {
			res := hook(t, r.env, "route-guard", nil, p)
			code(t, res, 2, rt+" LDD branch: "+c)
			matches(t, res.Stderr, `this change alters behaviour`, rt+" LDD branch why: "+c)
		}
	}
}

// The WU2 verify's probes (L791), pinned on both runtimes: slips past the
// parsed delivery reader stay refused, and help through a line-defined
// alias stays allowed.
func TestRouteGuardDeliveryVerifyProbes(t *testing.T) {
	t.Parallel()
	repo, env := noLayoutRepo(t)
	chain := "git"
	for i := 1; i < 10; i++ {
		chain += fmt.Sprintf(" -c alias.a%d=a%d", i, i+1)
	}
	chain += " -c alias.a10=commit a1 -m x"
	for _, c := range []string{"git commit -h -m x", "git commit -- --help", chain, "git ${X:-commit} -m x", "gh pr -R o/r create --title x", "git -P commit -m x"} {
		for rt, p := range rgPayloads(repo, c) {
			res := hook(t, env, "route-guard", nil, p)
			code(t, res, 2, rt+" refused: "+c)
			if !strings.Contains(res.Stderr, "LDD is not set up in this project") {
				t.Errorf("%s %q: %q", rt, c, res.Stderr)
			}
		}
	}
	for _, c := range []string{"git -c alias.help=commit help", "git -c alias.c=commit c --help"} {
		for rt, p := range rgPayloads(repo, c) {
			code(t, hook(t, env, "route-guard", nil, p), 0, rt+" passes: "+c)
		}
	}
}

// T146 (L1211, FA2): a small change (no folder, one component, under the
// advisory size, not high risk) is delivered with no tasks.md, no memory and
// no re-run: its commit and its PR pass once its RED before GREEN is on
// record.
func TestRouteGuardAcceptsASmallChangesFastPath(t *testing.T) {
	t.Parallel()
	small := newRG(t, "")
	support.Git(t, small.p, "checkout", "-q", "-b", "fix/small")
	support.Write(t, small.p, map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n"})
	redGreen(t, small.p, ".git/jhon/changes/fix-small/execution-record.json")
	code(t, small.call(t, "Bash", cmd("git add -A && git commit -q -m 'X is 2'"), true), 0, "the small change's commit")
	code(t, small.call(t, "Bash", cmd("gh pr create --title x --body y"), true), 0, "the small change's PR")
	if _, err := os.Stat(filepath.Join(small.p, "openspec/changes/fix-small")); err == nil {
		t.Error("no folder is asked for")
	}
}

// T146g G3 (L1274): only a change with no folder keeps no light verify; a
// branch carrying a change with a folder under another name is still told
// it has none (the verify of 2f4bd3e, L1289; a note since T154).
func TestRouteGuardFolderChangeOnOtherBranchNameNeedsALightVerify(t *testing.T) {
	t.Parallel()
	pr := cmd("gh pr create --title x --body y")
	r := newRG(t, "")
	support.Git(t, r.p, "checkout", "-q", "-b", "fix/small")
	support.Write(t, r.p, map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n",
		"openspec/changes/login-x/tasks.md":              "## Intent\nDelivers: X is 2\n",
		"openspec/changes/login-x/execution-record.json": redGreenUnverified("")})
	res := r.call(t, "Bash", pr, true)
	code(t, res, 0, "a change with a folder keeps its light verify, as a note")
	matches(t, adviceOf(t, res.Stdout), "no passing light verify of the tree its last run tested on record", "the light-verify note")
}

// T146g G3 (L1289, L1290): a change folder under another name, committed or
// archived, keeps the light verify even with the branch's folderless record.
func TestRouteGuardSmallExemptionNeedsNoFolderChangeOnTheBranch(t *testing.T) {
	t.Parallel()
	pr := cmd("gh pr create --title x --body y")
	for name, files := range map[string]map[string]string{
		"committed login-x": {"openspec/changes/login-x/tasks.md": "## Intent\nDelivers: X is 2\n",
			"openspec/changes/login-x/execution-record.json": redGreenUnverified("")},
		"archived login-x": {"openspec/changes/archive/2026-11-18-login-x/tasks.md": "## Intent\nDelivers: X is 2\n",
			"openspec/changes/archive/2026-11-18-login-x/execution-record.json": redGreenUnverified("")},
	} {
		r := newRG(t, "")
		support.Git(t, r.p, "checkout", "-q", "-b", "fix/small")
		support.Write(t, r.p, map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n"})
		support.Write(t, r.p, files)
		support.Git(t, r.p, "add", "-A")
		support.Git(t, r.p, "commit", "-q", "-m", "c")
		support.Write(t, r.p, map[string]string{".git/jhon/changes/fix-small/execution-record.json": redGreenUnverified("")})
		res := r.call(t, "Bash", pr, true)
		code(t, res, 0, name)
		matches(t, adviceOf(t, res.Stdout), "no passing light verify of the tree its last run tested on record", name)
	}
}

// T146g G3: a folder of the branch's own name deleted from the base is a
// change with a folder, not a small one.
func TestRouteGuardDeletedBaseFolderKeepsTheLightVerify(t *testing.T) {
	t.Parallel()
	r := newRG(t, "")
	support.Write(t, r.p, map[string]string{"openspec/changes/fix-small/tasks.md": "## Intent\nDelivers: X is 2\n"})
	support.Git(t, r.p, "add", "-A")
	support.Git(t, r.p, "commit", "-q", "-m", "f")
	support.Git(t, r.p, "checkout", "-q", "-b", "fix/small")
	support.Git(t, r.p, "rm", "-q", "-r", "openspec/changes/fix-small")
	support.Write(t, r.p, map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n",
		".git/jhon/changes/fix-small/execution-record.json": redGreenUnverified("")})
	res := r.call(t, "Bash", cmd("gh pr create --title x --body y"), true)
	code(t, res, 0, "deleted base folder")
	matches(t, adviceOf(t, res.Stdout), "no passing light verify", "light verify still noted")
}
