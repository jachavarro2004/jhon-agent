// `jhon executions run <change>` (contracts/ldd.md, "Test runs").
//
// For every selected testable project it runs the resolved command through
// `sh -c` in the project's directory, sequentially, continuing after a
// failure or a timeout, streaming the output, and records each exit code. The
// record lands at a fixed path in the change directory, rewritten after every
// project. `--dry-run` runs and writes nothing.
//
// Every command in these fixtures is harmless shell run inside a temporary
// directory: `pwd`, `exit 3`, `echo ok`, `touch`.
package ldd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

const record = "openspec/changes/c1/execution-record.json"

// workspace has four projects: `a` (go, discovered command replaced), `b`
// (unknown made testable by its override, which fails), `c` (rust,
// override), and `d` (unknown, no override: outside R, never run). overrides
// is a list of name, command pairs, in the order config.yaml lists them.
// The code checks go and cargo discover in `a` and `c` are overridden by
// harmless commands (contracts/ldd.md, "Code checks").
func workspace(t *testing.T, overrides ...string) string {
	t.Helper()
	if overrides == nil {
		overrides = []string{"a", "pwd", "b", "pwd; exit 3", "c", "echo ok"}
	}
	config := "test_command_override:\n"
	for i := 0; i < len(overrides); i += 2 {
		config += "  " + overrides[i] + ": " + overrides[i+1] + "\n"
	}
	config += "lint_command_override:\n  a: echo lint-a\n  c: \"true\"\nformat_command_override:\n  a: \"true\"\n  c: \"true\"\n"
	return tree(t, map[string]string{
		"a/go.mod":                     "module example.com/a\n",
		"b/pyproject.toml":             "[project]\nname = \"b\"\n",
		"b/app.py":                     "print('b')\n",
		"c/Cargo.toml":                 "[package]\nname = \"c\"\n",
		"d/pyproject.toml":             "[project]\nname = \"d\"\n",
		"d/app.py":                     "print('d')\n",
		"openspec/changes/c1/tasks.md": "- [x] done\n",
		"openspec/config.yaml":         config,
	})
}

func readRecord(t *testing.T, root string) map[string]any {
	t.Helper()
	return object(t, support.Read(t, filepath.Join(root, record)))
}

func executions(rec map[string]any) []map[string]any {
	var out []map[string]any
	for _, e := range rec["executions"].([]any) {
		out = append(out, e.(map[string]any))
	}
	return out
}

// testRuns are the record's test-command executions, its code checks left out.
func testRuns(rec map[string]any) []map[string]any {
	var out []map[string]any
	for _, e := range executions(rec) {
		if _, check := e["kind"]; !check {
			out = append(out, e)
		}
	}
	return out
}

func TestExecutionsRunsEachProject(t *testing.T) {
	t.Parallel()
	root := workspace(t)
	r := jhonGit(t, root, "executions", "run", "c1", "--json")
	equal(t, r.Code, 1, "a failed execution is a non-zero exit, after the record is written")
	// `pwd` proves the working directory; in --json mode the commands' own
	// output streams to stderr so stdout stays one JSON document.
	match(t, r.Stderr, `(?m)^`+regexp.QuoteMeta(filepath.Join(root, "a"))+`$`)
	match(t, r.Stderr, `(?m)^`+regexp.QuoteMeta(filepath.Join(root, "b"))+`$`)
	match(t, r.Stderr, `(?m)^ok$`, "the project after the failing one still ran")
	out := object(t, r.Stdout)
	equal(t, out["record"], record)
	rec := readRecord(t, root)
	equal(t, rec["schema"], strings.Join(vocab(t, "execution_record_schema"), ", "))
	ts, _ := rec["timestamp"].(string)
	if _, err := time.Parse(time.RFC3339, ts); err != nil {
		t.Errorf("timestamp %q: %v", ts, err)
	}
	var got []map[string]any
	for _, e := range executions(rec) {
		if d, ok := e["duration_ms"].(float64); !ok || d != math.Trunc(d) {
			t.Errorf("duration_ms %v is not an integer", e["duration_ms"])
		}
		e = maps.Clone(e)
		delete(e, "duration_ms")
		got = append(got, e)
	}
	equal(t, got, []map[string]any{
		{"project": "a", "command": "pwd", "cwd": "a", "exit_code": 0.0, "status": "pass"},
		{"project": "a", "kind": "lint", "command": "echo lint-a", "cwd": "a", "exit_code": 0.0, "status": "pass"},
		{"project": "a", "kind": "format", "command": "true", "cwd": "a", "exit_code": 0.0, "status": "pass"},
		{"project": "b", "command": "pwd; exit 3", "cwd": "b", "exit_code": 3.0, "status": "fail", "output_tail": filepath.Join(root, "b")},
		{"project": "c", "command": "echo ok", "cwd": "c", "exit_code": 0.0, "status": "pass"},
		{"project": "c", "kind": "lint", "command": "true", "cwd": "c", "exit_code": 0.0, "status": "pass"},
		{"project": "c", "kind": "format", "command": "true", "cwd": "c", "exit_code": 0.0, "status": "pass"},
	})
	equal(t, out["executions"], rec["executions"])
}

// A run that did not pass keeps the end of its output and the tests it
// names as failed, so what failed is read from the record, not found again
// by a run outside jhon.
func TestExecutionsRecordWhatFailed(t *testing.T) {
	t.Parallel()
	root := workspace(t, "a", "printf 'collected 3 items\\nFAILED tests/test_a.py::test_x - assert 1 == 2\\nERROR tests/test_b.py::test_y\\n'; exit 1", "b", "echo fine")
	equal(t, jhonGit(t, root, "executions", "run", "c1").Code, 1)
	for _, e := range executions(readRecord(t, root)) {
		if e["project"] == "a" && e["kind"] == nil {
			equal(t, e["failed_tests"], []any{"tests/test_a.py::test_x", "tests/test_b.py::test_y"})
			match(t, e["output_tail"].(string), `FAILED tests/test_a.py::test_x - assert 1 == 2`)
		}
		if e["status"] == "pass" && (e["output_tail"] != nil || e["failed_tests"] != nil) {
			t.Errorf("a pass keeps no output: %v", e)
		}
	}
}

func TestExecutionsHumanReport(t *testing.T) {
	t.Parallel()
	root := workspace(t, "a", "pwd", "c", "true", "b", "true")
	r := jhonGit(t, root, "executions", "run", "c1")
	equal(t, r.Code, 0, r.Stderr)
	match(t, r.Stdout, `(?m)^`+regexp.QuoteMeta(filepath.Join(root, "a"))+`$`, "command output reaches the terminal")
	m := regexp.MustCompile(`(?m)^record\s+(\S+)$`).FindStringSubmatch(r.Stdout)
	if m == nil {
		t.Fatalf("no record line:\n%s", r.Stdout)
	}
	equal(t, m[1], record, r.Stdout)
}

func TestExecutionsDryRun(t *testing.T) {
	t.Parallel()
	root := workspace(t, "a", "touch ran-a", "b", "touch ran-b", "c", "true")
	r := jhonGit(t, root, "executions", "run", "c1", "--dry-run")
	equal(t, r.Code, 0, r.Stderr)
	match(t, r.Stdout, `touch ran-a`)
	match(t, r.Stdout, regexp.QuoteMeta(filepath.Join(root, "b")))
	match(t, r.Stdout, regexp.QuoteMeta(record))
	equal(t, exists(filepath.Join(root, "a", "ran-a")), false, "nothing ran")
	equal(t, exists(filepath.Join(root, "b", "ran-b")), false, "nothing ran")
	equal(t, exists(filepath.Join(root, record)), false, "nothing was written")
}

func TestExecutionsUnknownChange(t *testing.T) {
	t.Parallel()
	root := workspace(t, "a", "touch ran-a", "b", "true", "c", "true")
	for _, change := range []string{"ghost", "archive", "../c1"} {
		equal(t, jhonGit(t, root, "executions", "run", change).Code, 2, change)
		equal(t, exists(filepath.Join(root, "a", "ran-a")), false, change)
	}
	// T146 (L1211): with none named, the change status selects runs, named first.
	r := jhonGit(t, root, "executions", "run")
	equal(t, r.Code, 0, "the selected change runs", r.Out())
	match(t, r.Stdout, `^jhon executions: change c1\b`)
}

// ── the tested revision ─────────────────────────────────────────────────────
// The record names what was tested: HEAD, HEAD's tree, and whether the working
// tree was clean. Identity is the tree; a dirty tree never enables delivery.

// committed is the workspace above, committed: a git repository whose
// working tree is clean.
func committed(t *testing.T, overrides ...string) string {
	t.Helper()
	root := workspace(t, overrides...)
	support.GitRepo(t, root)
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "workspace")
	return root
}

var allTrue = []string{"a", "true", "b", "true", "c", "true"}

func TestExecutionsRecordsCandidate(t *testing.T) {
	t.Parallel()
	root := committed(t, allTrue...)
	head := strings.TrimSpace(support.Git(t, root, "rev-parse", "HEAD"))
	treeID := strings.TrimSpace(support.Git(t, root, "rev-parse", "HEAD^{tree}"))
	r := jhonGit(t, root, "executions", "run", "c1", "--json")
	equal(t, r.Code, 0, r.Stderr)
	for _, got := range []map[string]any{object(t, r.Stdout), readRecord(t, root)} {
		equal(t, got["candidate_commit"], head)
		equal(t, got["candidate_tree"], treeID)
		equal(t, got["working_tree_clean"], true)
	}
}

func TestExecutionsUncommittedFileUnclean(t *testing.T) {
	t.Parallel()
	root := committed(t, allTrue...)
	support.Write(t, root, map[string]string{"a/new.go": "package a\n"})
	r := jhonGit(t, root, "executions", "run", "c1", "--json")
	equal(t, r.Code, 0, r.Stderr)
	equal(t, object(t, r.Stdout)["working_tree_clean"], false)
}

func TestExecutionsOutsideGit(t *testing.T) {
	t.Parallel()
	root := workspace(t, allTrue...)
	r := jhonGit(t, root, "executions", "run", "c1", "--json")
	equal(t, r.Code, 0, r.Stderr)
	for _, got := range []map[string]any{object(t, r.Stdout), readRecord(t, root)} {
		equal(t, got["candidate_commit"], "unknown")
		equal(t, got["candidate_tree"], "unknown")
		equal(t, got["working_tree_clean"], "unknown")
	}
}

// ── proportional verification: deadline, affected selection, post-run identity ──

func TestExecutionsTimeout(t *testing.T) {
	t.Parallel()
	root := workspace(t, "a", "(sleep 1; touch late) & wait", "b", "true", "c", "echo ok")
	r := jhonGit(t, root, "executions", "run", "c1", "--json", "--timeout", "0.01")
	equal(t, r.Code, 1, r.Stderr)
	rec := readRecord(t, root)
	equal(t, rec["timeout_minutes"], 0.01)
	equal(t, rec["complete"], true)
	var got [][]any
	for _, e := range testRuns(rec) {
		got = append(got, []any{e["project"], e["status"], e["exit_code"]})
	}
	equal(t, got, [][]any{{"a", "timeout", nil}, {"b", "pass", 0.0}, {"c", "pass", 0.0}})
	time.Sleep(1500 * time.Millisecond)
	equal(t, exists(filepath.Join(root, "a", "late")), false, "the timed-out project's children were killed")
}

func TestExecutionsDefaultAndInvalidTimeout(t *testing.T) {
	t.Parallel()
	root := workspace(t, allTrue...)
	equal(t, jhonGit(t, root, "executions", "run", "c1", "--json").Code, 0)
	equal(t, readRecord(t, root)["timeout_minutes"], 20.0)
	for _, bad := range []string{"0", "-1", "soon"} {
		equal(t, jhonGit(t, root, "executions", "run", "c1", "--timeout", bad).Code, 2, bad)
	}
}

func TestExecutionsPartialResults(t *testing.T) {
	t.Parallel()
	root := workspace(t, "a", "true", "b", "exit 3", "c", "cp ../openspec/changes/c1/execution-record.json ../seen.json")
	// One at a time, so c sees what ran before it.
	jhonGit(t, root, "executions", "run", "c1", "--json", "--jobs", "1")
	seen := object(t, support.Read(t, filepath.Join(root, "seen.json")))
	equal(t, seen["complete"], false)
	var projects []any
	for _, e := range testRuns(seen) {
		projects = append(projects, e["project"])
	}
	equal(t, projects, []any{"a", "b"})
	equal(t, readRecord(t, root)["complete"], true)
}

func TestExecutionsFullScope(t *testing.T) {
	t.Parallel()
	root := workspace(t, allTrue...)
	jhonGit(t, root, "executions", "run", "c1", "--json")
	equal(t, readRecord(t, root)["scope"], "full")
	equal(t, readRecord(t, root)["base"], nil)
}

func TestExecutionsCandidateReadAfterRun(t *testing.T) {
	t.Parallel()
	clean := committed(t, allTrue...)
	equal(t, jhonGit(t, clean, "executions", "run", "c1", "--json").Code, 0)
	rc := readRecord(t, clean)
	equal(t, rc["dirty"], false, "the record's own file never makes the run dirty")
	equal(t, rc["after"], map[string]any{"candidate_commit": rc["candidate_commit"], "candidate_tree": rc["candidate_tree"], "working_tree_clean": true})

	root := committed(t, "a", "true", "b", "touch made-by-b", "c", "true")
	equal(t, jhonGit(t, root, "executions", "run", "c1", "--json").Code, 0)
	rd := readRecord(t, root)
	equal(t, rd["working_tree_clean"], true)
	equal(t, rd["after"].(map[string]any)["working_tree_clean"], false)
	equal(t, rd["dirty"], true)
}

func pkgJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// nodeWorkspace is three Node packages, committed: `app` depends on `lib`
// through a file: reference, `tool` on it by name, and `other` on nothing.
// Every command is overridden to `true`.
func nodeWorkspace(t *testing.T, extra map[string]string) string {
	t.Helper()
	files := map[string]string{
		"lib/package.json":             pkgJSON(map[string]any{"name": "lib"}),
		"lib/index.js":                 "module.exports = 1;\n",
		"app/package.json":             `{"name":"app","dependencies":{"lib":"file:../lib"}}`,
		"app/index.js":                 "require('lib');\n",
		"tool/package.json":            `{"name":"tool","devDependencies":{"lib":"^1.0.0"}}`,
		"tool/index.js":                "require('lib');\n",
		"other/package.json":           pkgJSON(map[string]any{"name": "other"}),
		"other/index.js":               "module.exports = 2;\n",
		"README.md":                    "# workspace\n",
		"openspec/changes/c1/tasks.md": "- [x] done\n",
		"openspec/config.yaml":         "test_command_override:\n  lib: true\n  app: true\n  tool: true\n  other: true\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	root := tree(t, files)
	support.GitRepo(t, root)
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "base")
	support.Git(t, root, "tag", "base")
	return root
}

func ran(t *testing.T, root string) []string {
	t.Helper()
	out := []string{}
	for _, e := range executions(readRecord(t, root)) {
		out = append(out, e["project"].(string))
	}
	slices.Sort(out)
	return out
}

func TestExecutionsAffected(t *testing.T) {
	t.Parallel()
	root := nodeWorkspace(t, nil)
	support.Write(t, root, map[string]string{"lib/index.js": "module.exports = 3;\n"})
	support.Git(t, root, "commit", "-qam", "change lib")
	r := jhonGit(t, root, "executions", "run", "c1", "--json", "--affected", "base")
	equal(t, r.Code, 0, r.Stderr)
	equal(t, ran(t, root), []string{"app", "lib", "tool"})
	rec := readRecord(t, root)
	equal(t, rec["scope"], "affected")
	equal(t, rec["base"], "base")
	why := map[string]string{}
	for _, s := range rec["selection"].([]any) {
		m := s.(map[string]any)
		why[m["project"].(string)], _ = m["reason"].(string)
	}
	match(t, why["lib"], `lib/index\.js`)
	match(t, why["app"], `depends on lib`)
	match(t, why["tool"], `depends on lib`)
}

func TestExecutionsAffectedUncommittedAndOpenspec(t *testing.T) {
	t.Parallel()
	root := nodeWorkspace(t, nil)
	support.Write(t, root, map[string]string{
		"other/new.js":                 "x\n",
		"openspec/changes/c1/tasks.md": "- [x] done\n- [x] more\n",
	})
	equal(t, jhonGit(t, root, "executions", "run", "c1", "--json", "--affected", "base").Code, 0)
	equal(t, ran(t, root), []string{"other"})

	quiet := nodeWorkspace(t, nil)
	support.Write(t, quiet, map[string]string{"openspec/changes/c1/tasks.md": "- [x] done\n- [x] more\n"})
	jhonGit(t, quiet, "executions", "run", "c1", "--json", "--affected", "base")
	equal(t, ran(t, quiet), []string{}, "a change confined to openspec/ affects no project")
	equal(t, readRecord(t, quiet)["scope"], "affected")
}

func TestExecutionsAffectedOutsideEveryProject(t *testing.T) {
	t.Parallel()
	root := nodeWorkspace(t, nil)
	support.Write(t, root, map[string]string{"Makefile": "all:\n"})
	equal(t, jhonGit(t, root, "executions", "run", "c1", "--json", "--affected", "base").Code, 0)
	equal(t, ran(t, root), []string{"app", "lib", "other", "tool"})
	rec := readRecord(t, root)
	equal(t, rec["scope"], "full")
	equal(t, anyMatch(rec["scope_reasons"], `Makefile`), true, pkgJSON(rec["scope_reasons"]))

	// Documentation outside every project is test input only to the tests that name it.
	docs := nodeWorkspace(t, nil)
	support.Write(t, docs, map[string]string{"README.md": "# changed\n"})
	equal(t, jhonGit(t, docs, "executions", "run", "c1", "--json", "--affected", "base").Code, 0)
	equal(t, ran(t, docs), []string{}, "a document no test reads affects no project")
}

func TestExecutionsAffectedIncompleteDependencies(t *testing.T) {
	t.Parallel()
	root := nodeWorkspace(t, map[string]string{"other/package.json": "{ not json"})
	support.Write(t, root, map[string]string{"lib/index.js": "module.exports = 4;\n"})
	equal(t, jhonGit(t, root, "executions", "run", "c1", "--json", "--affected", "base").Code, 0)
	equal(t, ran(t, root), []string{"app", "lib", "other", "tool"})
	rec := readRecord(t, root)
	equal(t, rec["scope"], "full")
	equal(t, anyMatch(rec["scope_reasons"], `other/package\.json`), true, pkgJSON(rec["scope_reasons"]))
}

func TestExecutionsAffectedUnresolvedBase(t *testing.T) {
	t.Parallel()
	root := nodeWorkspace(t, nil)
	r := jhonGit(t, root, "executions", "run", "c1", "--affected", "no-such-ref")
	equal(t, r.Code, 2)
	match(t, r.Stderr, `no-such-ref`, "the refusal names the base")
	equal(t, exists(filepath.Join(root, record)), false)
}

// ── interruption ────────────────────────────────────────────────────────────
// SIGINT or SIGTERM stops the run: the running project's process group is
// sent SIGTERM, waited for (bounded), escalated to SIGKILL, and only then
// does the executor write its partial record and exit.

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func until(cond func() bool, d time.Duration) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if cond() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return cond()
}

func TestExecutionsInterruptKillsStubbornProject(t *testing.T) {
	t.Parallel()
	root := workspace(t, "a", "sh stubborn.sh", "b", "true", "c", "true")
	pidFile := filepath.Join(root, "child.pid")
	// The project ignores SIGTERM and SIGINT, and records its pid.
	support.Write(t, root, map[string]string{"a/stubborn.sh": "trap '' TERM INT\necho $$ > " + pidFile + "\nwhile :; do sleep 0.1; done\n"})
	child := exec.Command(support.Bin(t), "executions", "run", "c1")
	child.Dir, child.Env = root, support.Env()
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	pid := 0
	exited := false
	defer func() {
		if pid != 0 && alive(pid) {
			syscall.Kill(pid, syscall.SIGKILL)
		}
		if !exited {
			child.Process.Kill()
			child.Wait()
		}
	}()
	if !until(func() bool { b, err := os.ReadFile(pidFile); return err == nil && strings.TrimSpace(string(b)) != "" }, 10*time.Second) {
		t.Fatal("the project never started")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(support.Read(t, pidFile)))
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	err = child.Wait()
	exited = true
	var exit *exec.ExitError
	code := 0
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	}
	equal(t, code, 128+15)
	if !until(func() bool { return !alive(pid) }, 2*time.Second) {
		t.Errorf("the project %d survived the executor", pid)
	}
	equal(t, readRecord(t, root)["complete"], false)
}

// A project's code checks run after its test command and are recorded with
// their kind; a failing one fails the run as a failing test does, and a
// project with nothing to test still runs its checks (contracts/ldd.md,
// "Code checks").
func TestExecutionsRunsTheCodeChecks(t *testing.T) {
	t.Parallel()
	root := tree(t, map[string]string{
		"package.json":                 `{"name": "root", "workspaces": ["svc"]}`,
		"svc/package.json":             `{"name": "svc", "scripts": {"test": "x", "lint": "x"}}`,
		"svc/index.test.js":            "",
		"openspec/changes/c1/tasks.md": "- [x] done\n",
		"openspec/config.yaml":         "test_command_override:\n  svc: \"true\"\nlint_command_override:\n  svc: echo linted; exit 2\nformat_command_override:\n  \".\": \"true\"\n",
	})
	dry := jhonGit(t, root, "executions", "run", "c1", "--dry-run")
	match(t, dry.Stdout, `would run  svc \(lint\): sh -c 'echo linted; exit 2'`)
	r := jhonGit(t, root, "executions", "run", "c1")
	equal(t, r.Code, 1, "a failing check fails the run")
	match(t, r.Stdout, `(?m)^==> svc \(lint\): echo linted; exit 2$`)
	match(t, r.Stdout, `(?m)^fail\s+svc \(lint\)$`)
	var got []string
	for _, e := range executions(readRecord(t, root)) {
		kind, _ := e["kind"].(string)
		got = append(got, e["project"].(string)+"/"+kind+"/"+e["status"].(string))
	}
	equal(t, got, []string{"./format/pass", "svc//pass", "svc/lint/fail"})
}

// An affected run narrows a uv-run pytest project to the tests its change
// reaches: pytest-testmon, rooted at the repository so a library beside the
// project is tracked too, its record in Jhon's cache. A full run is unchanged.
//
// A wide run, one testmon would not narrow (a changed file it does not
// track) or would narrow to half the project's test time or more,
// or past ten minutes, by the record's durations, runs only the test modules
// the change touched: the PR's CI runs the suite whole, and its green checks
// test the delivery. A narrowed run notes its estimate. A file
// executions_inert declares is no test input.
func TestExecutionsAffectedRunsPytestUnderTestmon(t *testing.T) {
	t.Parallel()
	root := nodeWorkspace(t, map[string]string{"openspec/config.yaml": "test_command_override:\n  lib: uv run --extra dev pytest\n  app: true\n  tool: true\n  other: true\n"})
	support.Write(t, root, map[string]string{"lib/util.py": "X = 3\n"})
	cache := t.TempDir()
	type dry struct {
		Plan     []struct{ Project, Kind, Command string }
		Projects []struct {
			Project, Tests string
			Modules        int
			Estimate       *struct {
				Tests, Total int
				Seconds      float64
				AllSeconds   float64 `json:"all_seconds"`
			}
		}
		Notes []string
	}
	run := func(args ...string) dry {
		r := support.Jhon(t, root, support.Env("XDG_CACHE_HOME="+cache), append([]string{"executions", "run", "c1", "--dry-run", "--json"}, args...)...)
		equal(t, r.Code, 0, r.Stderr)
		var out dry
		if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
			t.Fatal(err, r.Stdout)
		}
		return out
	}
	command := func(args ...string) string {
		for _, p := range run(args...).Plan {
			if p.Project == "lib" && p.Kind == "" {
				return p.Command
			}
		}
		return ""
	}
	equal(t, command(), "uv run --extra dev pytest", "a full run")
	// The panel reads how each project's tests are narrowed as data, too.
	lib := func(args ...string) string {
		for _, p := range run(args...).Projects {
			if p.Project == "lib" {
				e := ""
				if p.Estimate != nil {
					e = fmt.Sprintf(" %d/%d %g/%g", p.Estimate.Tests, p.Estimate.Total, p.Estimate.Seconds, p.Estimate.AllSeconds)
				}
				return fmt.Sprintf("%s %d%s", p.Tests, p.Modules, e)
			}
		}
		return "absent"
	}
	equal(t, lib(), "whole 0")

	// No record: every test runs once, collecting it (executions_unit_test.go).
	match(t, command("--affected", "base"), `^mkdir -p '[^']+/jhon/testmon' && TESTMON_DATAFILE='[^']+\.db\.building' uv run --with pytest-testmon --extra dev pytest --testmon-noselect --rootdir=`)
	equal(t, lib("--affected", "base"), "whole 0")
	support.Write(t, root, map[string]string{"lib/tests/test_util.py": "def test_x(): pass\n"})
	notes := strings.Join(run("--affected", "base").Notes, "\n")
	match(t, notes, `pytest-testmon has no record yet, so every test runs once and becomes it; later runs are narrowed: lib`)

	// A record where the change reaches 1 of 4 tests narrows; 3 of 4 is wide.
	sum := sha256.Sum256([]byte(root + "\x00lib"))
	data := filepath.Join(cache, "jhon", "testmon", hex.EncodeToString(sum[:8])+".db")
	record := func(reached int, seconds float64) {
		os.Remove(data)
		os.MkdirAll(filepath.Dir(data), 0o755)
		script := `import sqlite3, sys
c = sqlite3.connect(sys.argv[1])
c.executescript("create table test_execution (id integer primary key, environment_id integer, test_name text, duration float, failed bit, forced bit); create table file_fp (id integer primary key, filename text, method_checksums blob, mtime float, fsha text); create table test_execution_file_fp (test_execution_id integer, fingerprint_id integer)")
c.execute("insert into file_fp values (1, 'lib/util.py', x'00', 0, 'a'), (2, 'lib/other.py', x'00', 0, 'b')")
for i in range(4):
    c.execute("insert into test_execution values (?, 1, ?, ?, 0, 0)", (i, "t%d" % i, float(sys.argv[3])))
    c.execute("insert into test_execution_file_fp values (?, ?)", (i, 1 if i < int(sys.argv[2]) else 2))
c.commit()`
		if out, err := exec.Command("python3", "-I", "-c", script, data, strconv.Itoa(reached), strconv.FormatFloat(seconds, 'f', -1, 64)).CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	record(1, 10)
	match(t, strings.Join(run("--affected", "base").Notes, "\n"), `lib: pytest-testmon would run about 1 of its 4 tests, 10 s of 40 s by the last recorded durations`)
	equal(t, lib("--affected", "base"), "testmon 0 1/4 10/40")
	match(t, command("--affected", "base"), `^mkdir -p '[^']+/jhon/testmon' && TESTMON_DATAFILE='[^']+/jhon/testmon/[0-9a-f]+\.db' uv run --with pytest-testmon --extra dev pytest --testmon --rootdir=`+regexp.QuoteMeta("'"+root+"'")+`$`)
	record(3, 10)
	equal(t, command("--affected", "base"), "uv run --extra dev pytest 'tests/test_util.py'", "the change reaches 3 of 4 tests' time")
	match(t, strings.Join(run("--affected", "base").Notes, "\n"), `lib: the change reaches about 3 of its 4 tests, 30 s of 40 s`)
	// A quarter of the time, but past ten minutes, is wide too.
	record(1, 700)
	equal(t, command("--affected", "base"), "uv run --extra dev pytest 'tests/test_util.py'", "a narrowed run past ten minutes")

	// testmon tracks Python files only: any other changed file makes the run
	// wide, unless executions_inert declares it no test input.
	record(1, 10)
	support.Write(t, root, map[string]string{"lib/fixtures.json": "{}\n"})
	equal(t, command("--affected", "base"), "uv run --extra dev pytest 'tests/test_util.py'", "a non-Python change")
	support.Write(t, root, map[string]string{"openspec/config.yaml": support.Read(t, filepath.Join(root, "openspec/config.yaml")) + "executions_inert: [lib/fixtures.json]\n"})
	match(t, command("--affected", "base"), `--testmon`)

	// Documentation is not untracked input: testmon still narrows, and a test
	// module that names a changed document runs first, beside its selection.
	support.Write(t, root, map[string]string{"lib/docs/api.md": "# API\n"})
	match(t, command("--affected", "base"), `^mkdir -p .* --testmon `)
	support.Write(t, root, map[string]string{"lib/tests/test_api_doc.py": "DOC = 'docs/api.md'\ndef test_doc(): pass\n"})
	match(t, command("--affected", "base"), `^uv run --extra dev pytest 'tests/test_api_doc\.py' && mkdir -p .* --testmon `)
	match(t, strings.Join(run("--affected", "base").Notes, "\n"), `lib: 1 test module\(s\) name a changed document, and run beside testmon's selection`)
	os.Remove(filepath.Join(root, "lib/docs/api.md"))
	os.Remove(filepath.Join(root, "lib/tests/test_api_doc.py"))

	// A test that checks the whole project (every route) is touched by no
	// change: executions_always keeps it in a narrowed run.
	record(3, 10)
	support.Write(t, root, map[string]string{
		"lib/tests/test_contract.py": "def test_routes(): pass\n",
		"openspec/config.yaml":       support.Read(t, filepath.Join(root, "openspec/config.yaml")) + "executions_always: [lib/tests/test_contract.py, lib/tests/test_util.py, other/tests/*.py]\n",
	})
	support.Git(t, root, "add", "lib/tests/test_contract.py")
	support.Git(t, root, "commit", "-qm", "the contract test is on the base")
	support.Git(t, root, "tag", "-f", "base")
	equal(t, command("--affected", "base"), "uv run --extra dev pytest 'tests/test_util.py' 'tests/test_contract.py'", "the change's module and the one executions_always declares, once each")
	match(t, strings.Join(run("--affected", "base").Notes, "\n"), `only the 1 test module\(s\) the change touched and the 1 executions_always declares run here`)
}

// Projects run in parallel, up to --jobs at a time (executions_jobs in
// openspec/config.yaml, else the CPUs, at most 4); each project's own
// commands stay in order, and each project's output is printed whole. Each
// project here passes only when it sees the other start, so a pass proves
// they overlapped and a serial run fails.
func TestExecutionsRunProjectsInParallel(t *testing.T) {
	t.Parallel()
	meet := func(me, other string) string {
		return "touch ../" + me + "-started; i=0; while [ ! -f ../" + other + "-started ]; do i=$((i+1)); [ $i -gt 100 ] && exit 1; sleep 0.05; done; echo " + me + "-done"
	}
	parallel := workspace(t, "a", meet("a", "b"), "b", meet("b", "a"), "c", "echo ok")
	r := jhonGit(t, parallel, "executions", "run", "c1", "--jobs", "2")
	equal(t, r.Code, 0, r.Stdout+r.Stderr)
	// Each project's output is one block: its command's output follows its own start line.
	match(t, r.Stdout, `(?s)==> a: [^\n]*\n(?:[^=][^\n]*\n)*?a-done\n`)
	match(t, r.Stdout, `(?s)==> b: [^\n]*\n(?:[^=][^\n]*\n)*?b-done\n`)
	equal(t, ran(t, parallel), []string{"a", "a", "a", "b", "c", "c", "c"}, "every command recorded, in plan order")

	serial := workspace(t, "a", meet("a", "b"), "b", meet("b", "a"), "c", "echo ok")
	support.Write(t, serial, map[string]string{"openspec/config.yaml": support.Read(t, filepath.Join(serial, "openspec/config.yaml")) + "executions_jobs: 1\n"})
	equal(t, jhonGit(t, serial, "executions", "run", "c1").Code, 1, "executions_jobs: 1 runs one project at a time")

	bad := jhonGit(t, parallel, "executions", "run", "c1", "--jobs", "0")
	equal(t, bad.Code, 2, bad.Stderr)
}

// Projects in one executions_exclusive group (they share state, such as a
// test database) never run at the same time; every other project still runs
// beside them. a and b each fail when they see the other running; c passes
// only when it overlaps one of them.
func TestExecutionsExclusiveGroups(t *testing.T) {
	t.Parallel()
	alone := func(me, other string) string {
		return "touch ../" + me + "-started ../" + me + "-running; sleep 0.5; [ -f ../" + other + "-running ] && exit 1; rm ../" + me + "-running"
	}
	beside := "i=0; until [ -f ../a-started ] || [ -f ../b-started ]; do i=$((i+1)); [ $i -gt 100 ] && exit 1; sleep 0.05; done"
	group := func(config string) string {
		root := workspace(t, "a", alone("a", "b"), "b", alone("b", "a"), "c", beside)
		support.Write(t, root, map[string]string{"openspec/config.yaml": support.Read(t, filepath.Join(root, "openspec/config.yaml")) + config})
		return root
	}
	r := jhonGit(t, group("executions_exclusive:\n  shared-db: [a, b]\n"), "executions", "run", "c1", "--jobs", "3")
	equal(t, r.Code, 0, r.Stdout+r.Stderr)
	equal(t, jhonGit(t, group(""), "executions", "run", "c1", "--jobs", "3").Code, 1, "without the group, a and b overlap")

	unknown := jhonGit(t, group("executions_exclusive:\n  shared-db: [a, ghost]\n"), "executions", "run", "c1", "--jobs", "3", "--dry-run")
	equal(t, unknown.Code, 0, unknown.Stderr)
	match(t, unknown.Stdout, `executions_exclusive\.shared-db names ghost, which is not a project`)
}

// An executions_exclusive group holds across processes too: two runs at the
// same time (two sessions, a worker and its verifier) take a lock per group
// and repository, so the group's projects still run one at a time. In the
// group, a fails when another a is running; without it, each a waits for the
// other and fails when they meet, so a pass of the second case proves the
// two runs' a ran at the same time, however slowly each run started.
func TestExecutionsExclusiveGroupsAcrossRuns(t *testing.T) {
	t.Parallel()
	// With the lock, a never sees another a running: the hold only gives a
	// broken lock the chance to show.
	alone := "mkdir ../a-running || exit 1; sleep 3; rmdir ../a-running"
	// Without it, a barrier rather than a race: each a leaves its mark and
	// waits up to 60 s for the other's (a sleep-based overlap flaked at 0.5 s
	// under the pre-push suite, L385, and at 3 s on a 2-core runner, L1142).
	// The marks stay, so the second to look still sees the first.
	meet := "mkdir ../a-met.$$; i=0; until [ $(ls -d ../a-met.* | wc -l) -ge 2 ]; do i=$((i+1)); [ $i -gt 1200 ] && exit 0; sleep 0.05; done; exit 1"
	both := func(a, config string) [2]int {
		root := workspace(t, "a", a, "b", "true", "c", "true")
		support.Write(t, root, map[string]string{"openspec/config.yaml": support.Read(t, filepath.Join(root, "openspec/config.yaml")) + config})
		support.GitRepo(t, root)
		var codes [2]int
		var wg sync.WaitGroup
		for i := range codes {
			wg.Go(func() { codes[i] = jhonGit(t, root, "executions", "run", "c1", "--jobs", "1").Code })
		}
		wg.Wait()
		return codes
	}
	equal(t, both(alone, "executions_exclusive:\n  shared-db: [a]\n"), [2]int{0, 0}, "the group's lock keeps the two runs' a apart")
	equal(t, both(meet, ""), [2]int{1, 1}, "without a group, the two runs' a overlap: each saw the other")
}

// An affected run narrows an Nx workspace to the projects Nx's own graph
// reaches from the change (uncommitted and untracked files included): its
// run-many becomes `nx affected` from the same base. A full run is unchanged.
func TestExecutionsAffectedRunsNxAffected(t *testing.T) {
	t.Parallel()
	root := nodeWorkspace(t, map[string]string{"openspec/config.yaml": "test_command_override:\n  lib: npx --no-install nx run-many -t test\n  app: true\n  tool: true\n  other: true\n"})
	support.Write(t, root, map[string]string{"lib/index.js": "module.exports = 3;\n"})
	command := func(args ...string) string {
		r := jhonGit(t, root, append([]string{"executions", "run", "c1", "--dry-run", "--json"}, args...)...)
		equal(t, r.Code, 0, r.Stderr)
		var out struct {
			Plan []struct{ Project, Kind, Command string }
		}
		if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
			t.Fatal(err, r.Stdout)
		}
		for _, p := range out.Plan {
			if p.Project == "lib" && p.Kind == "" {
				return p.Command
			}
		}
		t.Fatalf("lib's tests are not planned: %s", r.Stdout)
		return ""
	}
	equal(t, command("--affected", "base"), "npx --no-install nx affected -t test --base='base'")
	equal(t, command(), "npx --no-install nx run-many -t test", "a full run")
}

// A command that passed in the last complete, undisturbed run on the same
// working tree (uncommitted bytes included) is reused, not run again: the
// record holds it as passed and reused. An edit, a failure, or --force runs
// it again.
func TestExecutionsReuseAPassOnTheSameWorkingTree(t *testing.T) {
	t.Parallel()
	counts := t.TempDir()
	tally := func(p string) string { return "echo x >> " + filepath.Join(counts, p) }
	root := workspace(t, "a", tally("a"), "b", tally("b")+"; exit 3", "c", tally("c"))
	support.GitRepo(t, root)
	runs := func(p string) int {
		b, _ := os.ReadFile(filepath.Join(counts, p))
		return strings.Count(string(b), "x")
	}
	run := func(args ...string) {
		jhonGit(t, root, append([]string{"executions", "run", "c1", "--json"}, args...)...)
	}
	run()
	run()
	equal(t, [3]int{runs("a"), runs("b"), runs("c")}, [3]int{1, 2, 1}, "a and c passed and are reused; b failed and runs again")
	reused := map[string]bool{}
	for _, e := range executions(readRecord(t, root)) {
		if e["kind"] == nil {
			reused[e["project"].(string)], _ = e["reused"].(bool)
			if e["project"] != "b" {
				equal(t, e["status"], "pass", e["project"])
			}
		}
	}
	equal(t, reused, map[string]bool{"a": true, "b": false, "c": true})

	support.Write(t, root, map[string]string{"a/notes.txt": "edited\n"})
	run()
	equal(t, runs("a"), 2, "an edit changes the working tree: a runs again")
	run("--force")
	equal(t, runs("a"), 3, "--force runs it anyway")
}

// jhon review freeze: a review's frozen candidate from the core, the same
// for every lens that runs it on the same bytes (contracts/review-4r.md).
func TestReviewFreeze(t *testing.T) {
	t.Parallel()
	root := tree(t, map[string]string{"a.txt": "a\n"})
	support.GitRepo(t, root)
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "base")
	support.Write(t, root, map[string]string{"a.txt": "b\n", "new.txt": "n\n"})
	r := jhonGit(t, root, "review", "freeze", "--json")
	equal(t, r.Code, 0, r.Stderr)
	f := object(t, r.Stdout)
	again := object(t, jhonGit(t, root, "review", "freeze", "--json").Stdout)
	equal(t, again, f, "the same bytes freeze the same")
	match(t, f["subject"].(string), `^[0-9a-f]{64}$`)
	sh := jhonGit(t, root, "review", "freeze", "--base", "HEAD")
	match(t, sh.Stdout, `(?m)^BASE=[0-9a-f]{40}\nCANDIDATE=[0-9a-f]{40}\nSUBJECT=`+f["subject"].(string)+`$`)
	equal(t, jhonGit(t, root, "review", "freeze", "--base", "no-such-ref").Code, 2)
	equal(t, jhonGit(t, root, "review", "bogus").Code, 2)
}

// An affected run of a Vitest Nx workspace narrows further, inside each
// project Nx selects, with Vitest's --related: only when every changed file
// is JS or TS source, which Vitest's module graph tracks, or an Angular
// template or style standing for its component, and the workspace tests
// with Vitest alone.
func TestExecutionsAffectedNarrowsVitestToChangedSource(t *testing.T) {
	t.Parallel()
	command := func(pkg string, changed map[string]string) string {
		root := nodeWorkspace(t, map[string]string{
			"openspec/config.yaml": "test_command_override:\n  lib: npx --no-install nx run-many -t test\n  app: true\n  tool: true\n  other: true\n",
			"lib/package.json":     pkg,
		})
		support.Write(t, root, changed)
		r := jhonGit(t, root, "executions", "run", "c1", "--dry-run", "--json", "--affected", "base")
		equal(t, r.Code, 0, r.Stderr)
		var out struct {
			Plan []struct{ Project, Kind, Command string }
		}
		if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
			t.Fatal(err, r.Stdout)
		}
		for _, p := range out.Plan {
			if p.Project == "lib" && p.Kind == "" {
				return p.Command
			}
		}
		t.Fatalf("lib's tests are not planned: %s", r.Stdout)
		return ""
	}
	vitest := `{"name":"lib","devDependencies":{"nx":"22.6.5","vitest":"4.0.9"}}`
	related := regexp.MustCompile(`^npx --no-install nx affected -t test --base='base' -- --passWithNoTests --related='/[^']+/lib/index\.js'$`)
	if c := command(vitest, map[string]string{"lib/index.js": "module.exports = 3;\n"}); !related.MatchString(c) {
		t.Errorf("a source change: %q", c)
	}
	component := regexp.MustCompile(`^npx --no-install nx affected -t test --base='base' -- --passWithNoTests --related='/[^']+/lib/x\.component\.ts'$`)
	if c := command(vitest, map[string]string{"lib/x.component.ts": "export class X {}\n", "lib/x.component.html": "<p></p>\n"}); !component.MatchString(c) {
		t.Errorf("a template stands for its component: %q", c)
	}
	equal(t, command(vitest, map[string]string{"lib/index.js": "module.exports = 3;\n", "lib/vite.config.ts": "export default {};\n"}),
		"npx --no-install nx affected -t test --base='base'", "a configuration change")
	equal(t, command(`{"name":"lib","devDependencies":{"nx":"22.6.5","vitest":"4.0.9","jest":"30.0.0"}}`, map[string]string{"lib/index.js": "module.exports = 3;\n"}),
		"npx --no-install nx affected -t test --base='base'", "a workspace with Jest too")
}
