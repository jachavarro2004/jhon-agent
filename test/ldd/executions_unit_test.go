// A unit's test run and the pytest-testmon record it needs (T133, L1003).
//
// A pytest project with no usable record runs whole once, collecting into
// Jhon's record (E1); a first run its deadline cut is not tried again on
// every run (E2); `jhon executions prime` builds the missing records (E3); a
// baseless `--affected` is what changed since the change's last passing run
// (E4); and a slice's close and a delivery need the wide run (E5).
//
// No pytest runs here: `uv` is a script on PATH that records its arguments
// and the record it was told to write, writes one, and exits as told.
package ldd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// fakeUvScript stands in for uv: one line per call in FAKE_UV_LOG (the record
// it was told to write, then its arguments), a copy of FAKE_UV_RECORD at
// TESTMON_DATAFILE, a wait of FAKE_UV_SLEEP seconds, and pytest's exit code,
// FAKE_UV_EXIT.
const fakeUvScript = `#!/bin/sh
printf '%s|%s\n' "${TESTMON_DATAFILE-}" "$*" >> "$FAKE_UV_LOG"
if [ -n "${TESTMON_DATAFILE-}" ]; then cp "$FAKE_UV_RECORD" "$TESTMON_DATAFILE"; fi
if [ -n "${FAKE_UV_SLEEP-}" ]; then sleep "$FAKE_UV_SLEEP"; fi
exit "${FAKE_UV_EXIT:-0}"
`

// pytestProject is nodeWorkspace with `lib` a uv-run pytest project, on a
// branch `feat` off main (tagged base) whose one commit changes lib/util.py
// and its test module, with the fake uv first on PATH and a cache of its own.
type pytestProject struct {
	root string
	// data is where Jhon keeps lib's pytest-testmon record.
	data string
	log  string
	env  []string
}

func newPytestProject(t *testing.T) *pytestProject {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 reads the pytest-testmon record")
	}
	root := nodeWorkspace(t, map[string]string{"openspec/config.yaml": "test_command_override:\n  lib: uv run --extra dev pytest\n  app: true\n  tool: true\n  other: true\n"})
	support.Git(t, root, "checkout", "-q", "-b", "feat")
	support.Write(t, root, map[string]string{"lib/util.py": "X = 3\n", "lib/tests/test_util.py": "def test_x(): pass\n"})
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "the change")
	scratch := temp(t)
	support.Write(t, scratch, map[string]string{"bin/uv": fakeUvScript})
	if err := os.Chmod(filepath.Join(scratch, "bin", "uv"), 0o755); err != nil {
		t.Fatal(err)
	}
	// What a run of pytest-testmon leaves: the change reaches 1 of 4 tests.
	made := filepath.Join(scratch, "made.db")
	script := `import sqlite3, sys
c = sqlite3.connect(sys.argv[1])
c.executescript("create table test_execution (id integer primary key, environment_id integer, test_name text, duration float, failed bit, forced bit); create table file_fp (id integer primary key, filename text, method_checksums blob, mtime float, fsha text); create table test_execution_file_fp (test_execution_id integer, fingerprint_id integer)")
c.execute("insert into file_fp values (1, 'lib/util.py', x'00', 0, 'a'), (2, 'lib/other.py', x'00', 0, 'b')")
for i in range(4):
    c.execute("insert into test_execution values (?, 1, ?, 10, 0, 0)", (i, "t%d" % i))
    c.execute("insert into test_execution_file_fp values (?, ?)", (i, 1 if i < 1 else 2))
c.commit()`
	if out, err := exec.Command("python3", "-I", "-c", script, made).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	cache := filepath.Join(scratch, "cache")
	sum := sha256.Sum256([]byte(root + "\x00lib"))
	p := &pytestProject{root: root, data: filepath.Join(cache, "jhon", "testmon", hex.EncodeToString(sum[:8])+".db"), log: filepath.Join(scratch, "uv.log")}
	p.env = []string{"PATH=" + filepath.Join(scratch, "bin") + string(os.PathListSeparator) + os.Getenv("PATH"),
		"XDG_CACHE_HOME=" + cache, "FAKE_UV_LOG=" + p.log, "FAKE_UV_RECORD=" + made, "FAKE_UV_EXIT=0", "FAKE_UV_SLEEP="}
	return p
}

// jhon runs `jhon <args>` in the project with uv faked; env overrides it.
func (p *pytestProject) jhon(t *testing.T, env []string, args ...string) support.Result {
	t.Helper()
	return support.Jhon(t, p.root, support.Env(append(append([]string{}, p.env...), env...)...), args...)
}

// calls are the fake uv's calls so far, oldest first.
func (p *pytestProject) calls(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(p.log)
	if err != nil {
		return []string{}
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// plan is a dry run's command for lib's tests, how they are narrowed, and its notes.
func (p *pytestProject) plan(t *testing.T, args ...string) (command, tests, notes string) {
	t.Helper()
	r := p.jhon(t, nil, append([]string{"executions", "run", "c1", "--dry-run", "--json"}, args...)...)
	equal(t, r.Code, 0, r.Stderr)
	var out struct {
		Plan     []struct{ Project, Kind, Command string }
		Projects []struct{ Project, Tests string }
		Notes    []string
	}
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		t.Fatal(err, r.Stdout)
	}
	for _, s := range out.Plan {
		if s.Project == "lib" && s.Kind == "" {
			command = s.Command
		}
	}
	for _, s := range out.Projects {
		if s.Project == "lib" {
			tests = s.Tests
		}
	}
	return command, tests, strings.Join(out.Notes, "\n")
}

// collecting is lib's whole test command under pytest-testmon with selection
// off, writing beside the record; narrowed is its command once it has one.
func (p *pytestProject) collecting() string {
	return "mkdir -p '" + filepath.Dir(p.data) + "' && TESTMON_DATAFILE='" + p.data + ".building' uv run --with pytest-testmon --extra dev pytest --testmon-noselect --rootdir='" + p.root + "'"
}

func (p *pytestProject) narrowed() string {
	return "mkdir -p '" + filepath.Dir(p.data) + "' && TESTMON_DATAFILE='" + p.data + "' uv run --with pytest-testmon --extra dev pytest --testmon --rootdir='" + p.root + "'"
}

const firstRunNote = `pytest-testmon has no record yet, so every test runs once and becomes it; later runs are narrowed: lib`

// E1: with no usable record, the project's whole test command runs once with
// pytest-testmon collecting into a file beside Jhon's record, which becomes
// the record when pytest ran to its end, its tests passing (exit 0) or
// failing (exit 1: a unit's first run is RED), and is dropped when the run
// was cut (2 interrupted, 3 an internal error).
func TestExecutionsFirstRunBuildsTheTestmonRecord(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		exit string
		code int
		kept bool
	}{
		{"tests pass", "0", 0, true},
		{"tests fail", "1", 1, true},
		{"interrupted", "2", 1, false},
		{"internal error", "3", 1, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p := newPytestProject(t)
			command, tests, notes := p.plan(t, "--affected", "base")
			equal(t, command, p.collecting(), "the whole command, collecting")
			equal(t, tests, "whole")
			match(t, notes, regexp.QuoteMeta(firstRunNote))
			equal(t, p.calls(t), []string{}, "a dry run runs nothing")

			r := p.jhon(t, []string{"FAKE_UV_EXIT=" + c.exit}, "executions", "run", "c1", "--affected", "base", "--json")
			equal(t, r.Code, c.code, r.Stderr)
			equal(t, p.calls(t), []string{p.data + ".building|run --with pytest-testmon --extra dev pytest --testmon-noselect --rootdir=" + p.root},
				"uv ran once, whole, told to write beside the record")
			match(t, strings.Join(notesOf(t, r.Stdout), "\n"), regexp.QuoteMeta(firstRunNote))
			equal(t, exists(p.data), c.kept, "the record")
			left, _ := filepath.Glob(p.data + ".building*")
			equal(t, len(left), 0, "nothing half-written is left beside it")

			command, _, _ = p.plan(t, "--affected", "base")
			if c.kept {
				equal(t, command, p.narrowed(), "later runs are narrowed by the record")
			} else {
				equal(t, command, p.collecting(), "a run pytest did not finish is no first run")
			}
		})
	}
}

func notesOf(t *testing.T, stdout string) []string {
	t.Helper()
	var out struct{ Notes []string }
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatal(err, stdout)
	}
	return out.Notes
}

// cut runs lib's first run under a deadline it cannot meet.
func (p *pytestProject) cut(t *testing.T) support.Result {
	t.Helper()
	return p.jhon(t, []string{"FAKE_UV_SLEEP=30"}, "executions", "run", "c1", "--affected", "base", "--json", "--timeout", "0.01")
}

// E2: a first run cut by its deadline keeps no record and is not tried again
// on every run: later runs run the test modules the change touched and say
// how to build the record with a longer deadline.
func TestExecutionsFirstRunCutByItsDeadlineIsNotTriedAgain(t *testing.T) {
	t.Parallel()
	p := newPytestProject(t)
	r := p.cut(t)
	equal(t, r.Code, 1, r.Stderr)
	var status any
	for _, e := range testRuns(readRecord(t, p.root)) {
		if e["project"] == "lib" {
			status = e["status"]
		}
	}
	equal(t, status, "timeout")
	match(t, r.Stderr, "(?m)^\\.\\.\\. lib: the first run under pytest-testmon hit its 0\\.01 min deadline, so no record was kept and later runs run only the test modules the change touched; `jhon executions prime --timeout <minutes>` builds it with a longer deadline$")
	equal(t, exists(p.data), false, "a cut run is no record")
	left, _ := filepath.Glob(p.data + ".building*")
	equal(t, len(left), 0, "nor is its half-written file kept")

	command, tests, notes := p.plan(t, "--affected", "base")
	equal(t, command, "uv run --extra dev pytest 'tests/test_util.py'", "the modules the change touched")
	equal(t, tests, "own")
	match(t, notes, "lib: pytest-testmon has no record, since the first run that builds it hit its 0\\.01 min deadline \\(`jhon executions prime --timeout <minutes>` builds it with a longer deadline\\); only the 1 test module\\(s\\) the change touched run here, and the rest is left to the PR's CI")
	noMatch(t, notes, `every test runs once`)
	equal(t, p.jhon(t, nil, "executions", "run", "c1", "--affected", "base", "--json").Code, 0)
	equal(t, p.calls(t)[1:], []string{"|run --extra dev pytest tests/test_util.py"}, "the next run does not try the whole suite again")
}

// E3: `jhon executions prime` builds the record of each pytest project that
// lacks one, a cut first run's included, and writes no execution record.
func TestExecutionsPrimeBuildsTheMissingRecords(t *testing.T) {
	t.Parallel()
	p := newPytestProject(t)
	r := p.jhon(t, nil, "executions", "prime", "--timeout", "5")
	equal(t, r.Code, 0, r.Out())
	equal(t, p.calls(t), []string{p.data + ".building|run --with pytest-testmon --extra dev pytest --testmon-noselect --rootdir=" + p.root})
	equal(t, r.Stdout, "==> lib: "+p.collecting()+"\n<== lib: exit 0; its pytest-testmon record is kept\njhon executions prime: 1 of 1 record(s) built\n")
	equal(t, exists(p.data), true, "the record")
	equal(t, exists(filepath.Join(p.root, record)), false, "prime writes no execution record")
	command, _, _ := p.plan(t, "--affected", "base")
	equal(t, command, p.narrowed())

	r = p.jhon(t, nil, "executions", "prime")
	equal(t, r.Code, 0, r.Out())
	equal(t, r.Stdout, "jhon executions prime: every pytest project has its pytest-testmon record; nothing to build\n")
	equal(t, len(p.calls(t)), 1, "a project with a record is not run again")

	// A first run its deadline cut is built by prime, with its own deadline.
	q := newPytestProject(t)
	q.cut(t)
	before := support.Read(t, filepath.Join(q.root, record))
	r = q.jhon(t, []string{"FAKE_UV_SLEEP=30"}, "executions", "prime", "--timeout", "0.01")
	equal(t, r.Code, 1, r.Out())
	equal(t, r.Stdout, "==> lib: "+q.collecting()+"\n<== lib: timeout after 0.01 min; its process group was killed and no record was kept\njhon executions prime: 0 of 1 record(s) built; run it again with a longer `--timeout <minutes>`\n")
	equal(t, exists(q.data), false)
	equal(t, q.jhon(t, nil, "executions", "prime", "--timeout", "5").Code, 0)
	command, _, notes := q.plan(t, "--affected", "base")
	equal(t, command, q.narrowed(), "the built record narrows the next run")
	noMatch(t, notes, `prime`)
	equal(t, support.Read(t, filepath.Join(q.root, record)), before, "prime leaves the execution record as it was")

	for _, bad := range [][]string{{"prime", "--timeout", "0"}, {"prime", "--timeout"}, {"prime", "c1"}} {
		r := q.jhon(t, nil, append([]string{"executions"}, bad...)...)
		equal(t, r.Code, 2, bad)
		match(t, r.Stderr, `usage: jhon executions prime \[--timeout <minutes>\] \[--root <project>\]`, bad)
	}
	match(t, jhonGit(t, q.root, "help").Stdout, `(?m)^  jhon executions prime \[--timeout <minutes>\]$`)
}

// E4: a baseless `--affected` selects what changed since the change's last
// complete, undisturbed passing run, else since the change's base, and says
// which; the record says the run was a unit's and what it measured from.
func TestExecutionsBaselessAffectedIsSinceTheLastPassingRun(t *testing.T) {
	t.Parallel()
	// tool's tests fail while openspec/red exists, which selects no project.
	root := nodeWorkspace(t, map[string]string{"openspec/config.yaml": "test_command_override:\n  lib: true\n  app: true\n  tool: test ! -f ../openspec/red\n  other: true\n"})
	support.Git(t, root, "checkout", "-q", "-b", "feat")
	support.Write(t, root, map[string]string{"lib/index.js": "module.exports = 3;\n"})
	support.Git(t, root, "commit", "-qam", "change lib")
	unit := func(code int) (map[string]any, string) {
		t.Helper()
		r := jhonGit(t, root, "executions", "run", "c1", "--affected", "--json")
		equal(t, r.Code, code, r.Stderr)
		return readRecord(t, root), strings.Join(notesOf(t, r.Stdout), "\n")
	}

	rec, notes := unit(0)
	equal(t, ran(t, root), []string{"app", "lib", "tool"}, "no passing run yet: the whole change")
	equal(t, rec["unit"], true)
	equal(t, rec["since"], "main")
	equal(t, rec["base"], nil)
	match(t, notes, `a unit's run \(--affected with no base\): no passing run of this change is on record, so what changed since the change's base \(main\) is selected`)
	first := rec["working_tree"]

	support.Write(t, root, map[string]string{"other/index.js": "module.exports = 4;\n"})
	rec, notes = unit(0)
	equal(t, ran(t, root), []string{"other"}, "only what changed since the passing run")
	equal(t, rec["since"], first, "the tree the last passing run tested")
	match(t, notes, `a unit's run \(--affected with no base\): what changed since the change's last passing run \(\d{4}-[^)]+\) is selected`)
	second := rec["working_tree"]

	// Nothing changed since: nothing runs, and the record stays the last run's.
	before := support.Read(t, filepath.Join(root, record))
	_, notes = unit(0)
	equal(t, support.Read(t, filepath.Join(root, record)), before, "a unit's run of nothing replaces no record")
	match(t, notes, `no project selected; the record is left as the last run wrote it`)

	// A failing run is no passing run: the next one still measures from the
	// last that passed, so what the failing run tested runs again.
	support.Write(t, root, map[string]string{"openspec/red": "", "tool/index.js": "require('lib'); // edited\n"})
	rec, _ = unit(1)
	equal(t, ran(t, root), []string{"tool"})
	equal(t, rec["since"], second)
	os.Remove(filepath.Join(root, "openspec", "red"))
	support.Write(t, root, map[string]string{"other/index.js": "module.exports = 5;\n"})
	rec, _ = unit(0)
	equal(t, ran(t, root), []string{"other", "tool"}, "measured from the last run that passed, not from the failing one")
	equal(t, rec["since"], second)

	// A tree git no longer holds: the change's base, said so.
	gone := strings.ReplaceAll(support.Read(t, filepath.Join(root, record)), rec["working_tree"].(string), strings.Repeat("0", 40))
	support.Write(t, root, map[string]string{record: gone})
	rec, notes = unit(0)
	equal(t, ran(t, root), []string{"app", "lib", "other", "tool"})
	equal(t, rec["since"], "main")
	match(t, notes, `a unit's run \(--affected with no base\): the tree of the change's last passing run is no longer in git, so what changed since the change's base \(main\) is selected`)

	// A named base stays the wide run, and its record says nothing new.
	equal(t, jhonGit(t, root, "executions", "run", "c1", "--affected", "main", "--json", "--force").Code, 0)
	rec = readRecord(t, root)
	equal(t, rec["base"], "main")
	_, isUnit := rec["unit"]
	_, hasSince := rec["since"]
	equal(t, []bool{isUnit, hasSince}, []bool{false, false}, "a wide run's record")
	r := jhonGit(t, root, "executions", "run", "c1", "--affected", "--dry-run")
	match(t, r.Stdout, `(?m)^scope  affected \(affected since the change's last passing run\), `)
}

// E4: with a usable record a unit's run is the pytest-testmon command.
func TestExecutionsBaselessAffectedRunsUnderTestmon(t *testing.T) {
	t.Parallel()
	p := newPytestProject(t)
	equal(t, p.jhon(t, nil, "executions", "prime").Code, 0)
	command, tests, _ := p.plan(t, "--affected")
	equal(t, command, p.narrowed())
	equal(t, tests, "testmon")
}

// E5: a slice's close needs the wide run: the commit that closes a slice is
// refused when the change's last run was a unit's, and passes after
// `--affected <base>` or a full run.
func TestASliceCommitNeedsTheWideRun(t *testing.T) {
	t.Parallel()
	root := workspace(t, "a", "true", "b", "true", "c", "true", "d", "true")
	config := filepath.Join(root, "openspec", "config.yaml")
	support.Write(t, root, map[string]string{
		"openspec/config.yaml":         support.Read(t, config) + "delivery: change\n",
		"openspec/changes/c1/tasks.md": "## Slice 1: a\n\nDelivers: CA-01\n\n- [ ] 1.1 a\n",
	})
	support.GitRepo(t, root)
	support.Git(t, root, "checkout", "-q", "-B", "main")
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "base")
	support.Git(t, root, "checkout", "-q", "-b", "feat")
	commit := func() support.Result {
		payload, _ := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "git add -A && git commit -m 'slice 1'"},
			"cwd": root, "permission_mode": "default"})
		return support.Run(t, support.Cmd{Args: []string{support.Bin(t), "hook", "route-guard"}, Env: support.Env(), Stdin: string(payload)})
	}
	support.Write(t, root, map[string]string{"a/feature.go": "package a\n"})
	equal(t, jhonGit(t, root, "executions", "run", "c1", "--affected").Code, 0, "the unit's tests pass")
	support.Write(t, root, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n\nDelivers: CA-01\n\n- [x] 1.1 a\n"})
	r := commit()
	equal(t, r.Code, 2, "a slice closed on a unit's run")
	match(t, r.Stderr, "this commit closes Slice 1: a of c1, and its last test run was a unit's \\(`--affected` with no base\\), which runs only what changed since the change's last passing run\\. Run `jhon executions run c1 --affected <base>` on the slice as it will be committed, then commit", r.Stderr)

	equal(t, jhonGit(t, root, "executions", "run", "c1", "--affected", "main").Code, 0)
	equal(t, commit().Code, 0, "the wide run")
	equal(t, jhonGit(t, root, "executions", "run", "c1", "--affected").Code, 0)
	equal(t, commit().Code, 0, "a unit's run of nothing after it leaves the wide record")
	support.Write(t, root, map[string]string{"a/feature.go": "package a\n\nvar X = 1\n"})
	equal(t, jhonGit(t, root, "executions", "run", "c1", "--affected").Code, 0)
	equal(t, commit().Code, 2, "a unit's run of a later edit")
	equal(t, jhonGit(t, root, "executions", "run", "c1").Code, 0)
	equal(t, commit().Code, 0, "a full run")
}

// E5: `jhon deliver check` refuses a unit's run the same way.
func TestDeliverNeedsTheWideRun(t *testing.T) {
	t.Parallel()
	root := deliverProject(t, "true")
	support.Git(t, root, "checkout", "-q", "-B", "main")
	support.Git(t, root, "checkout", "-q", "-b", "feat")
	support.Write(t, root, map[string]string{"a/a.go": "package a\n\nvar X = 1\n"})
	commit(t, root, "the change")
	run := func(args ...string) {
		t.Helper()
		equal(t, jhonGit(t, root, append([]string{"executions", "run", deliverChange, "--json"}, args...)...).Code, 0)
		commit(t, root, "record")
	}
	// The fixture's change is high risk with no verify on record, a reason of
	// its own on every check here: only the unit's reason is read.
	const unit = "the last test run was a unit's (`--affected` with no base), which runs only what changed since the change's last passing run: run `jhon executions run v --affected <base>` for the whole change, then `jhon deliver check`"
	refused := func() bool {
		t.Helper()
		r := check(t, root)
		equal(t, r.Code, 1, r.Out())
		return slices.Contains(r.json["reasons"].([]any), any(unit))
	}
	run("--affected")
	equal(t, refused(), true, "a unit's run delivers nothing")
	run("--affected", "main")
	equal(t, refused(), false, "the wide run")
	run()
	equal(t, refused(), false, "a full run")
}

// staleNote is what a wide run says of a project whose record is usable and
// whose change touches no test module.
const staleNote = `no test module of it changed, so pytest-testmon runs what its record holds stale, and the rest is left to the PR's CI, whose green checks test the delivery`

// sliceCommit is the route guard's answer to the commit that closes a slice
// of the `delivery: change` project at root.
func sliceCommit(t *testing.T, root string) support.Result {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "git add -A && git commit -m 'slice 1'"},
		"cwd": root, "permission_mode": "default"})
	return support.Run(t, support.Cmd{Args: []string{support.Bin(t), "hook", "route-guard"}, Env: support.Env(), Stdin: string(payload)})
}

const (
	openSlice   = "## Slice 1: a\n\nDelivers: CA-01\n\n- [ ] 1.1 a\n"
	closedSlice = "## Slice 1: a\n\nDelivers: CA-01\n\n- [x] 1.1 a\n"
)

// sliced makes the pytest project a `delivery: change` whose change has one
// open slice, committed and tagged `slice`: what changes after it touches no
// test module. lib is its only testable project, so a run that runs none of
// lib's tests holds no execution.
func (p *pytestProject) sliced(t *testing.T) {
	t.Helper()
	support.Write(t, p.root, map[string]string{
		"openspec/config.yaml":         "test_command_override:\n  lib: uv run --extra dev pytest\ndelivery: change\n",
		"openspec/changes/c1/tasks.md": openSlice,
		"lib/other.py":                 "Y = 1\n",
	})
	support.Git(t, p.root, "add", "-A")
	support.Git(t, p.root, "commit", "-q", "-m", "the slice starts")
	support.Git(t, p.root, "tag", "slice")
}

// L1011: a slice's close trusts pytest-testmon. A wide run (`--affected
// <base>`) of a project with a usable record that would run only the test
// modules the change touched (it reaches half the suite or more, or a file
// testmon does not track changed) and whose change touched none runs under
// pytest-testmon instead: what its record holds stale, which may be nothing.
// That is an execution on record, so the commit that closes the slice passes.
func TestExecutionsWideRunWithNoTestModuleRunsUnderTestmon(t *testing.T) {
	t.Parallel()
	p := newPytestProject(t)
	p.sliced(t)
	equal(t, p.jhon(t, nil, "executions", "prime").Code, 0)
	// lib/other.py reaches 3 of the record's 4 tests.
	support.Write(t, p.root, map[string]string{"lib/other.py": "Y = 2\n"})
	command, tests, notes := p.plan(t, "--affected", "slice")
	equal(t, command, p.narrowed(), "no module to run: the pytest-testmon command")
	equal(t, tests, "testmon")
	match(t, notes, `(?m)^lib: the change reaches about 3 of its 4 tests, 30 s of 40 s[^;\n]*; `+regexp.QuoteMeta(staleNote)+`$`)
	match(t, notes, `(?m)^tests narrowed by pytest-testmon to those whose executed code changed: lib$`)
	noMatch(t, notes, `none runs here`)
	noMatch(t, notes, `the record holds no execution`)

	// A file pytest-testmon does not track, and still no test module.
	support.Write(t, p.root, map[string]string{"lib/fixtures.json": "{}\n"})
	command, tests, notes = p.plan(t, "--affected", "slice")
	equal(t, command, p.narrowed(), "a non-Python change and no module to run")
	equal(t, tests, "testmon")
	match(t, notes, `(?m)^lib: lib/fixtures\.json changed, and pytest-testmon tracks Python files only; `+regexp.QuoteMeta(staleNote)+`$`)
	os.Remove(filepath.Join(p.root, "lib", "fixtures.json"))

	before := len(p.calls(t))
	r := p.jhon(t, nil, "executions", "run", "c1", "--affected", "slice", "--json")
	equal(t, r.Code, 0, r.Stderr)
	equal(t, p.calls(t)[before:], []string{p.data + "|run --with pytest-testmon --extra dev pytest --testmon --rootdir=" + p.root}, "uv ran once, under pytest-testmon")
	var status any
	for _, e := range testRuns(readRecord(t, p.root)) {
		if e["project"] == "lib" {
			status = e["status"]
		}
	}
	equal(t, status, "pass", "the run is an execution on record")
	match(t, r.Stderr, `(?m)^jhon executions: pass, exit 0 \(`)

	support.Write(t, p.root, map[string]string{"openspec/changes/c1/tasks.md": closedSlice})
	c := sliceCommit(t, p.root)
	equal(t, c.Code, 0, "the commit that closes the slice", c.Stderr)
	// The guard reads this project: an edit after the run is refused.
	support.Write(t, p.root, map[string]string{"lib/other.py": "Y = 3\n"})
	c = sliceCommit(t, p.root)
	equal(t, c.Code, 2, "an edit after the run")
	match(t, c.Stderr, `1 file\(s\) changed after its last test run \(lib/other\.py\)`)
	support.Write(t, p.root, map[string]string{"lib/other.py": "Y = 2\n"})

	// A touched test module still runs alone: the modules-touched path.
	support.Write(t, p.root, map[string]string{"lib/tests/test_util.py": "def test_x(): assert True\n"})
	command, tests, notes = p.plan(t, "--affected", "slice")
	equal(t, command, "uv run --extra dev pytest 'tests/test_util.py'", "a touched module")
	equal(t, tests, "own")
	match(t, notes, `only the 1 test module\(s\) the change touched run here, and the rest is left to the PR's CI`)
}

// A unit's run (a bare `--affected`) in the same place still runs none: a
// unit never runs half the suite.
func TestExecutionsUnitRunWithNoTestModuleRunsNone(t *testing.T) {
	t.Parallel()
	p := newPytestProject(t)
	p.sliced(t)
	equal(t, p.jhon(t, nil, "executions", "prime").Code, 0)
	equal(t, p.jhon(t, nil, "executions", "run", "c1", "--json").Code, 0, "a passing run to measure from")
	support.Write(t, p.root, map[string]string{"lib/other.py": "Y = 2\n"})
	command, tests, notes := p.plan(t, "--affected")
	equal(t, command, "")
	equal(t, tests, "none")
	match(t, notes, `(?m)^lib: the change reaches about 3 of its 4 tests, 30 s of 40 s[^;\n]*; no test module of it changed, so none runs here, and the rest is left to the PR's CI, whose green checks test the delivery$`)
	before := len(p.calls(t))
	equal(t, p.jhon(t, nil, "executions", "run", "c1", "--affected", "--json").Code, 0)
	equal(t, len(p.calls(t)), before, "uv is not run")
}

// A project with no usable record (its first run was cut) and no touched
// test module still runs none in a wide run.
func TestExecutionsWideRunWithNoRecordAndNoTestModuleRunsNone(t *testing.T) {
	t.Parallel()
	p := newPytestProject(t)
	p.sliced(t)
	equal(t, p.cut(t).Code, 1)
	support.Write(t, p.root, map[string]string{"lib/other.py": "Y = 2\n"})
	command, tests, notes := p.plan(t, "--affected", "slice")
	equal(t, command, "")
	equal(t, tests, "none")
	match(t, notes, "(?m)^lib: pytest-testmon has no record, since the first run that builds it hit its 0\\.01 min deadline \\(`jhon executions prime --timeout <minutes>` builds it with a longer deadline\\); no test module of it changed, so none runs here, and the rest is left to the PR's CI, whose green checks test the delivery$")
	noMatch(t, notes, `holds stale`)
}

// L1011: a last run that holds no execution (a change of documents only
// selects no project) closes no slice, and the refusal names the command
// that clears it, the full run, not the affected run that wrote that record.
func TestASliceCommitOnARunOfNothingNamesTheFullRun(t *testing.T) {
	t.Parallel()
	root := workspace(t, "a", "true", "b", "true", "c", "true", "d", "true")
	config := filepath.Join(root, "openspec", "config.yaml")
	support.Write(t, root, map[string]string{
		"openspec/config.yaml":         support.Read(t, config) + "delivery: change\n",
		"openspec/changes/c1/tasks.md": openSlice,
	})
	support.GitRepo(t, root)
	support.Git(t, root, "checkout", "-q", "-B", "main")
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "base")
	support.Git(t, root, "checkout", "-q", "-b", "feat")
	support.Write(t, root, map[string]string{"docs/notes.md": "# Notes\n"})
	r := jhonGit(t, root, "executions", "run", "c1", "--affected", "main", "--json")
	equal(t, r.Code, 0, r.Stderr)
	match(t, strings.Join(notesOf(t, r.Stdout), "\n"), `(?m)^no project selected; the record holds no execution$`)
	support.Write(t, root, map[string]string{"openspec/changes/c1/tasks.md": closedSlice})
	c := sliceCommit(t, root)
	equal(t, c.Code, 2, "a run of nothing closes no slice")
	match(t, c.Stderr, "this commit closes Slice 1: a of c1, and its last test run holds no execution\\. Run `jhon executions run c1` \\(the full run\\) on the slice as it will be committed, then commit: each slice of a `delivery: change` is a tested tree, not only the last one\\.", c.Stderr)
	noMatch(t, c.Stderr, `--affected`)

	equal(t, jhonGit(t, root, "executions", "run", "c1").Code, 0)
	equal(t, sliceCommit(t, root).Code, 0, "the full run clears it")
}

// L1011: `jhon deliver check` names the full run the same way, for a wide
// run and for a unit's, whose own reason would name the affected run.
func TestDeliverOnARunOfNothingNamesTheFullRun(t *testing.T) {
	t.Parallel()
	root := deliverProject(t, "true")
	support.Git(t, root, "checkout", "-q", "-B", "main")
	support.Git(t, root, "checkout", "-q", "-b", "feat")
	support.Write(t, root, map[string]string{"docs/notes.md": "# Notes\n"})
	commit(t, root, "the change")
	const nothing = "the last test run did not pass: it ran no project; run `jhon executions run v` (the full run), then `jhon deliver check`"
	reasonsOf := func(code int, args ...string) []any {
		t.Helper()
		equal(t, jhonGit(t, root, append([]string{"executions", "run", deliverChange, "--json"}, args...)...).Code, 0)
		commit(t, root, "record")
		r := check(t, root)
		equal(t, r.Code, code, r.Out())
		return r.json["reasons"].([]any)
	}
	written := func() map[string]any {
		t.Helper()
		return object(t, support.Read(t, filepath.Join(root, deliverDir, "execution-record.json")))
	}
	unit := reasonsOf(1, "--affected")
	equal(t, written()["unit"], true, "a unit's run with no record before it writes one")
	equal(t, slices.Contains(unit, any(nothing)), true, unit)
	equal(t, anyMatch(unit, `--affected`), false, "no reason names the affected run", unit)
	wide := reasonsOf(1, "--affected", "main")
	_, isUnit := written()["unit"]
	equal(t, isUnit, false, "the wide run's record")
	equal(t, slices.Contains(wide, any(nothing)), true, wide)
	equal(t, anyMatch(wide, `--affected`), false, wide)
	equal(t, reasonsOf(0), []any{}, "the full run clears it")
}
