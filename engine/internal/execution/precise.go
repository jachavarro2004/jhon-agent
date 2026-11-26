package execution

import (
	"encoding/json"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// Precise is a project's test command narrowed to the tests a change
// reaches, for an affected run: a `uv run ... pytest` command runs under
// pytest-testmon, which records the code each test executes and then runs
// only the tests whose executed code changed (or that failed last time). A
// project with no record yet runs every test once, and that run is its record
// (Collect).
//
// The rootdir is the repository's top level, so testmon also tracks the
// code of the projects this one depends on (a library beside it in a
// monorepo): with the project's own directory as rootdir, a change there
// would select no test. The record lives in Jhon's cache, one file per
// repository and project that every worktree of the repository shares
// (TestmonPath), never in the project; a run writes a private copy of it
// (CheckOut), never the record itself. ok is false for any other command,
// and outside a git repository.
//
// cov says the project runs pytest-cov (PytestCov): testmon refuses to run
// beside it when branch coverage is on, so the run passes `--no-cov`; a
// selected run's coverage would be partial anyway.
func Precise(command, top, projectPath string, cov bool) (string, bool) {
	words := strings.Fields(command)
	if top == "" || len(words) < 3 || words[0] != "uv" || words[1] != "run" || !slices.Contains(words[2:], "pytest") {
		return "", false
	}
	return underTestmon(command, top, TestmonPath(top, projectPath), "--testmon", cov), true
}

// underTestmon is a `uv run ... pytest` command run under pytest-testmon
// with flag, its record at data.
func underTestmon(command, top, data, flag string, cov bool) string {
	rest := uvRun.ReplaceAllString(command, "")
	// testmon does not create its record's directory.
	out := "mkdir -p " + shq(filepath.Dir(data)) + " && TESTMON_DATAFILE=" + shq(data) + " uv run --with pytest-testmon " + rest + " " + flag + " --rootdir=" + shq(top)
	if cov {
		out += " --no-cov"
	}
	return out
}

// Collect is a project's whole test command run once under pytest-testmon
// with selection off (`--testmon-noselect` deselects nothing and still
// records the code each test executes): the first run of a project whose
// record is missing or holds no fingerprints, where narrowing starts. It
// writes to BuildFile, beside the record and this process's own, which
// FinishBuild makes the record only when pytest ran to its end, so a
// half-written file is never read as one, and two first runs at once (two
// worktrees) never write or remove each other's. ok is as Precise's.
func Collect(command, top, projectPath string, cov bool) (string, bool) {
	if _, ok := Precise(command, top, projectPath, cov); !ok {
		return "", false
	}
	return underTestmon(command, top, BuildFile(TestmonPath(top, projectPath)), "--testmon-noselect", cov), true
}

// BuildFile is where this process's first run (Collect) writes the record
// data will be: `<data>.<pid>.building`.
func BuildFile(data string) string { return data + "." + strconv.Itoa(os.Getpid()) + ".building" }

// sqliteBeside are the files sqlite keeps beside a database in WAL mode,
// which pytest-testmon's is.
var sqliteBeside = []string{"-wal", "-shm"}

// StartBuild removes what an earlier first run of this process left at
// data's BuildFile, and what a killed run left beside the records (sweep).
func StartBuild(data string) {
	drop(BuildFile(data))
	sweep(filepath.Dir(data))
}

// FinishBuild ends a first run whose command exited with code, nil when it
// was cut (its deadline, a signal, sh not starting), and whose output w
// watched (nil when none was): the file it wrote becomes the record at data,
// atomically, when pytest ran to its end, its tests passing (exit 0) or
// failing (exit 1), unless its failures are the environment's (refused,
// Watch.Refusal), and is removed otherwise. kept is whether data is now a
// record that narrows (not RecordNone or RecordUnusable); a kept record
// clears FirstRunCut.
func FinishBuild(data string, code *int, w *Watch) (kept bool, refused string) {
	tmp := BuildFile(data)
	defer StartBuild(data)
	if !ranToEnd(code) || !project.Exists(tmp) {
		return false, ""
	}
	if refused = w.Refusal(); refused != "" {
		return false, refused
	}
	if !publish(tmp, data) {
		return false, ""
	}
	if s := TestmonRecord(data); s == RecordNone || s == RecordUnusable {
		return false, ""
	}
	os.Remove(cutFile(data))
	return true, ""
}

// cutFile is the marker of a first run that built no record (MarkFirstRunCut).
func cutFile(data string) string { return data + ".cut" }

// MarkFirstRunCut remembers, beside the record data would be, that a first
// run built none and why ("hit its 20 min deadline"), so the next runs do not
// run the whole suite again to no end.
func MarkFirstRunCut(data, why string) {
	if os.MkdirAll(filepath.Dir(data), 0o777) == nil {
		_ = os.WriteFile(cutFile(data), []byte(why+"\n"), 0o666)
	}
}

// FirstRunCut is why the last first run of the record at data built none;
// ok is false when none was cut, or a record was built since.
func FirstRunCut(data string) (why string, ok bool) {
	b, err := os.ReadFile(cutFile(data))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

// The states of a testmon record (TestmonRecord).
const (
	RecordNone     = "none"
	RecordUsable   = "usable"
	RecordUnusable = "unusable"
	RecordUnknown  = "unknown"
)

// testmonProbe prints how many of a testmon record's files carry a
// fingerprint: a run that crashed (testmon refusing branch coverage, exit 3)
// leaves every file without one, and then every test is selected.
const testmonProbe = `import sqlite3, sys
c = sqlite3.connect("file:" + sys.argv[1] + "?mode=ro", uri=True)
print(c.execute("select count(fsha) from file_fp").fetchone()[0])`

// TestmonRecord is whether the record at data can narrow a run: RecordNone
// when there is none (the run is every test, once, and becomes the record),
// RecordUsable when it fingerprints files, RecordUnusable when it
// fingerprints none (the run is every test again, and repairs it), and
// RecordUnknown when python3 cannot read it.
func TestmonRecord(data string) string {
	if !project.Exists(data) {
		return RecordNone
	}
	out, err := exec.Command("python3", "-I", "-c", testmonProbe, data).Output()
	if err != nil {
		return RecordUnknown
	}
	if n, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil && n > 0 {
		return RecordUsable
	} else if err == nil {
		return RecordUnusable
	}
	return RecordUnknown
}

// testmonReach prints, for a record and the files named after it: how many
// tests ran any of the files, how many the record holds, the seconds each
// set took when last recorded, how many tests it marks failed, how many of
// those that ran the files it does not, and how many tests and seconds
// pytest-testmon selects: those that ran the files and those marked failed.
const testmonReach = `import sqlite3, sys
c = sqlite3.connect("file:" + sys.argv[1] + "?mode=ro", uri=True)
files = sys.argv[2:]
last = "select t.id, t.duration, t.failed from test_execution t join (select max(id) id from test_execution group by test_name) l on l.id = t.id"
rows = c.execute(last).fetchall()
tests = {r[0]: r[1] for r in rows}
failed = {r[0] for r in rows if r[2]}
hit = set()
if files:
    q = "select distinct x.test_execution_id from test_execution_file_fp x join file_fp f on f.id = x.fingerprint_id where f.filename in (" + ",".join("?" * len(files)) + ")"
    hit = {r[0] for r in c.execute(q, files)} & tests.keys()
run = hit | (failed & tests.keys())
print(len(hit), len(tests), sum(tests[i] or 0 for i in hit), sum(d or 0 for d in tests.values()), len(failed), len(hit - failed), len(run), sum(tests[i] or 0 for i in run))`

// WideShare is the share of a project's test time past which a narrowed
// run is wide: it runs about what the PR's CI runs whole anyway.
const WideShare = 0.5

// WideSeconds is the narrowed test time past which a run is wide however
// small its share: the PR's CI runs those tests too, so a long local run
// only delays the delivery.
const WideSeconds = 600

// Reach is what a testmon record says a run would select: the tests that
// executed a changed file, out of all the record holds, and the seconds
// each set took when last recorded. By file, so at most what testmon, which
// selects by method, would run.
type Reach struct {
	Tests, Total     int
	Seconds, AllTime float64
	// Failed is how many tests the record marks failed, which
	// pytest-testmon runs again whatever changed; Passing how many of Tests
	// it does not mark failed.
	Failed, Passing int
	// Selects is how many tests pytest-testmon would run, Tests and the
	// failed ones, and SelectSeconds their last recorded time (T256).
	Selects       int
	SelectSeconds float64
}

// MostlyFailed is whether the record marks most of its tests failed, at
// least EnvFailureMin: what a run while the test services were down leaves
// (T255, L1725), and why every run then selects them all.
func (r Reach) MostlyFailed() bool {
	return r.Failed >= EnvFailureMin && 2*r.Failed > r.Total
}

// OnlyFailed is whether every test pytest-testmon would select is one the
// record marks failed: the change reaches none it marks passing.
func (r Reach) OnlyFailed() bool { return r.Failed > 0 && r.Passing == 0 }

// Wide is whether the run is wide: at least WideShare of the time, or more
// than WideSeconds.
func (r Reach) Wide() bool {
	return r.Seconds >= WideSeconds || (r.AllTime > 0 && r.Seconds >= WideShare*r.AllTime)
}

// String is what the change reaches as a note reads it: the tests that
// ran a changed file.
func (r Reach) String() string {
	return fmt.Sprintf("about %d of its %d tests, %s of %s by the last recorded durations", r.Tests, r.Total, minutes(r.Seconds), minutes(r.AllTime))
}

// Selection is the estimate of what pytest-testmon would run as a note reads
// it: the tests the change reaches and those the record marks failed, so it
// agrees with the note on the failed ones (T256).
func (r Reach) Selection() string {
	return fmt.Sprintf("about %d of its %d tests, %s of %s by the last recorded durations", r.Selects, r.Total, minutes(r.SelectSeconds), minutes(r.AllTime))
}

func minutes(s float64) string {
	if s < 60 {
		return fmt.Sprintf("%d s", int(s+0.5))
	}
	return fmt.Sprintf("%d min", int(s/60+0.5))
}

// TestmonReach is what the record at data says a run would select when
// files (relative to the record's rootdir, the repository's top level)
// changed; ok is false when python3 cannot read the record.
func TestmonReach(data string, files []string) (Reach, bool) {
	out, err := exec.Command("python3", append([]string{"-I", "-c", testmonReach, data}, files...)...).Output()
	if err != nil {
		return Reach{}, false
	}
	f := strings.Fields(string(out))
	if len(f) != 8 {
		return Reach{}, false
	}
	var r Reach
	var errs [8]error
	r.Tests, errs[0] = strconv.Atoi(f[0])
	r.Total, errs[1] = strconv.Atoi(f[1])
	r.Seconds, errs[2] = strconv.ParseFloat(f[2], 64)
	r.AllTime, errs[3] = strconv.ParseFloat(f[3], 64)
	r.Failed, errs[4] = strconv.Atoi(f[4])
	r.Passing, errs[5] = strconv.Atoi(f[5])
	r.Selects, errs[6] = strconv.Atoi(f[6])
	r.SelectSeconds, errs[7] = strconv.ParseFloat(f[7], 64)
	for _, e := range errs {
		if e != nil {
			return Reach{}, false
		}
	}
	return r, true
}

// testFile is a pytest test module.
var testFile = lazyre.New(`^test_.*\.py$|_test\.py$`)

// OwnTests is the test modules a change since base touched in the project at
// projectPath, relative to that project, that still exist under root: what a
// wide run (WideShare) still runs here, the rest left to the PR's CI.
func OwnTests(root, projectPath string, changed []string) []string {
	var out []string
	for _, f := range changed {
		rel, err := filepath.Rel(projectPath, f)
		if err != nil || strings.HasPrefix(rel, "..") || !testFile.MatchString(filepath.Base(f)) {
			continue
		}
		if st, err := os.Stat(filepath.Join(root, f)); err == nil && !st.IsDir() {
			out = append(out, rel)
		}
	}
	return out
}

// AlwaysTests are the test modules of the project at projectPath that
// `executions_always` lists (paths or globs relative to the repository
// root), relative to that project, that exist under root and are not in
// own: a narrowed run runs them too, since a test that checks the whole
// project (every route, every permission) is reached by no change's own
// modules.
func AlwaysTests(root, projectPath string, always, own []string) []string {
	var out []string
	for _, pattern := range always {
		matches, _ := filepath.Glob(filepath.Join(root, pattern))
		for _, abs := range matches {
			rel, err := filepath.Rel(filepath.Join(root, projectPath), abs)
			if err != nil || strings.HasPrefix(rel, "..") || slices.Contains(own, rel) || slices.Contains(out, rel) {
				continue
			}
			if st, err := os.Stat(abs); err == nil && !st.IsDir() {
				out = append(out, rel)
			}
		}
	}
	return out
}

// OwnTestsCommand is a pytest test command narrowed to files, relative to
// its project: without testmon, and with `--no-cov` when the project runs
// pytest-cov, since coverage of a few modules would fail a coverage floor.
func OwnTestsCommand(command string, files []string, cov bool) string {
	out := command
	if cov {
		out += " --no-cov"
	}
	for _, f := range files {
		out += " " + shq(f)
	}
	return out
}

// TestmonModulesCommand is a pytest-testmon command (Precise) narrowed to
// files, test modules relative to its project: pytest collects only those,
// and pytest-testmon keeps only their tests its record marks affected.
func TestmonModulesCommand(precise string, files []string) string {
	out := precise
	for _, f := range files {
		out += " " + shq(f)
	}
	return out
}

// BothCommand is first and then second, second run even when first fails,
// exiting with the higher (worse) of their statuses: a wide step's modules
// run whole and its pytest-testmon part (T145c, L1191 H1), so the failures
// of both are reported in one pass. CommandParts reads it back.
func BothCommand(first, second string) string {
	return first + bothThen + second + bothEnd
}

const (
	bothThen = "; s1=$?; "
	bothEnd  = "; s2=$?; [ $s1 -gt $s2 ] && exit $s1; exit $s2"
)

// CommandParts is the test runs a command the engine wrote is made of, in
// order, and whether each runs whatever those before it did (BothCommand,
// or a single run) or only when they passed (an `&&` chain, as a document's
// readers before pytest-testmon). A pytest-testmon run (Precise) keeps the
// `mkdir -p` that makes its record's directory.
func CommandParts(command string) (parts []string, each bool) {
	if body, ok := strings.CutSuffix(command, bothEnd); ok {
		if first, second, ok := strings.Cut(body, bothThen); ok {
			return []string{first, second}, true
		}
	}
	for _, s := range strings.Split(command, " && ") {
		if n := len(parts); n > 0 && strings.HasPrefix(parts[n-1], "mkdir -p ") && !strings.Contains(parts[n-1], " && ") {
			parts[n-1] += " && " + s
			continue
		}
		parts = append(parts, s)
	}
	return parts, len(parts) == 1
}

// PytestCov is whether the Python project at dir declares pytest-cov in its
// pyproject.toml.
func PytestCov(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "pyproject.toml"))
	return err == nil && strings.Contains(string(b), "pytest-cov")
}

// NxAffected is an Nx workspace's run-many test command narrowed, for an
// affected run, to `nx affected` from the same base: the projects Nx's own
// graph reaches from the change, with uncommitted and untracked files, as Nx
// counts them when given a base alone. ok is false for any other command.
func NxAffected(command, base string) (string, bool) {
	m := nxRunMany.FindStringSubmatch(command)
	if m == nil {
		return "", false
	}
	return m[1] + "affected" + m[2] + " --base=" + shq(base), true
}

// VitestRelated is an `nx affected` test command narrowed, inside each
// project Nx selects, to the tests whose module graph reaches files
// (absolute paths, from RelatedFiles): Vitest's own `--related`, passed
// through Nx to every project's test executor, with `--passWithNoTests`
// since a project none of whose tests reach them has none to run. Only for
// a workspace whose tests are Vitest's (VitestOnly).
func VitestRelated(affected string, files []string) string {
	var b strings.Builder
	b.WriteString(affected + " -- --passWithNoTests")
	for _, f := range files {
		b.WriteString(" --related=" + shq(f))
	}
	return b.String()
}

// sourceFile is a JS or TS module, which Vitest's module graph tracks.
var sourceFile = lazyre.New(`\.(ts|tsx|mts|cts|js|jsx|mjs|cjs)$`)

// configFile is a tool's configuration (vite.config.ts, eslint.config.mjs):
// source by its extension, read by no test's module graph.
var configFile = lazyre.New(`(^|[./])config\.[a-z]+$`)

// componentFile is an Angular component's template or style, which Vitest's
// module graph does not track: a changed template selects no test.
var componentFile = lazyre.New(`\.(html|css|scss|sass|less)$`)

// maxRelated bounds the files RelatedFiles names, which become arguments.
const maxRelated = 200

// RelatedFiles is the files, absolute, through which Vitest's module graph
// reaches the tests a change since base touches: each changed file under
// root (relative to it, as ChangedSince gives them) but the change's record
// (openspec/) that is JS or TS source and not configuration, and for a
// template or a style, the component beside it (x.component.html is
// x.component.ts's). False when any changed file is neither, is gone, or
// has no such component, when nothing changed, and past maxRelated files:
// Nx then runs every affected project whole.
func RelatedFiles(root string, changed []string) ([]string, bool) {
	var out []string
	for _, f := range changed {
		if layout.For(root).Ignored(f) {
			continue
		}
		base := filepath.Base(f)
		switch {
		case sourceFile.MatchString(base) && !configFile.MatchString(base):
		case componentFile.MatchString(base):
			f = componentFile.ReplaceAllString(f, ".ts")
		default:
			return nil, false
		}
		abs := filepath.Join(root, f)
		if st, err := os.Stat(abs); err != nil || st.IsDir() {
			return nil, false
		}
		if !slices.Contains(out, abs) {
			out = append(out, abs)
		}
	}
	return out, len(out) > 0 && len(out) <= maxRelated
}

// VitestOnly is whether the workspace at dir tests with Vitest and not Jest:
// its package.json lists vitest and no jest, since Nx hands `--related` to
// every project's executor and Jest does not take it.
func VitestOnly(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return false
	}
	var pkg struct {
		Dependencies, DevDependencies map[string]string
	}
	if json.Unmarshal(b, &pkg) != nil {
		return false
	}
	has := func(name string) bool {
		_, a := pkg.Dependencies[name]
		_, b := pkg.DevDependencies[name]
		return a || b
	}
	return has("vitest") && !has("jest")
}

// nxRunMany is `[npx [--no-install]] nx run-many -t test` and whatever follows.
var nxRunMany = lazyre.New(`^(\s*(?:npx\s+(?:--no-install\s+)?)?nx\s+)run-many(\s+(?:-t|--target[=\s]|--targets[=\s])\s*test\b.*)$`)

// uvRun is the leading `uv run`, which Precise rewrites; the rest of the
// command stays as written, quoting included.
var uvRun = lazyre.New(`^\s*uv\s+run\s+`)

// shq is s quoted for sh.
func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// prose are the extensions of documentation: text no code imports, which
// pytest-testmon cannot track and no build compiles.
var prose = []string{".md", ".mdx", ".rst", ".adoc"}

// Prose is whether the changed file f is documentation.
func Prose(f string) bool {
	return slices.Contains(prose, strings.ToLower(filepath.Ext(f)))
}

// readerFile is a test module a reader of documentation can be: pytest's,
// or a JavaScript or TypeScript test.
var readerFile = lazyre.New(`^test_.*\.py$|_test\.py$|\.(?:test|spec)\.[cm]?[jt]sx?$`)

// skippedDirs are the directories a search for readers never enters.
var skippedDirs = []string{".git", ".hg", ".svn", ".venv", "venv", "node_modules", "__pycache__", ".mypy_cache", ".pytest_cache", ".ruff_cache", "dist", "build"}

// ProseReaders is the test modules of the project at projectPath, relative
// to it, whose source names one of the changed documentation files docs
// (paths relative to root) by its file name: a test that reads a document
// (one that checks the docs list every route), which no coverage tool sees.
// A document inside one of projects is read only by that project's tests,
// so another project's own README.md is not taken for it; one outside every
// project, by any project's.
func ProseReaders(root, projectPath string, docs, projects []string) []string {
	var names []string
	for _, d := range docs {
		if o := owner(d, projects); Prose(d) && (o == "" || o == projectPath) {
			names = append(names, filepath.Base(d))
		}
	}
	if len(names) == 0 {
		return nil
	}
	var out []string
	testModules(root, projectPath, func(rel, src string) {
		if slices.ContainsFunc(names, func(n string) bool { return strings.Contains(src, n) }) {
			out = append(out, rel)
		}
	})
	return out
}

// maxReaderBytes bounds the test modules a search for readers reads: a larger
// file is generated data, not a test that names a path.
const maxReaderBytes = 1 << 20

// testModules calls fn with each test module of the project at projectPath
// that can be a reader (readerFile): its path relative to the project, and
// its source.
func testModules(root, projectPath string, fn func(rel, src string)) {
	dir := filepath.Join(root, projectPath)
	_ = filepath.WalkDir(dir, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if e.IsDir() {
			if path != dir && slices.Contains(skippedDirs, e.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !readerFile.MatchString(e.Name()) {
			return nil
		}
		if info, err := e.Info(); err != nil || info.Size() > maxReaderBytes {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if rel, err := filepath.Rel(dir, path); err == nil {
			fn(rel, string(b))
		}
		return nil
	})
}

// sourceRef is a whole reference to a changed source file in a test
// module's source (T145, L1174 E3): its path relative to the root, that
// path without its extension (bare), or, for a Python module, its import
// path.
type sourceRef struct {
	s      string
	dotted bool
	bare   bool
}

// sourcePaths is the whole references by which a test module of another
// project names the changed source file f (relative to the root) of the
// project owner: f itself; for a Python module its import path relative to
// owner, a `src/` layout's `src.` dropped (app/routers/items.py is
// app.routers.items), when that has at least two segments; for any other
// source, f without its extension, as a JS/TS import names it
// (`../svc_a/src/x` for svc_a/src/x.ts, T145c H3), when that has at least
// two segments. A fragment of any (a directory such as `tests/unit/` or
// `app/routers/`, a bare file name, a package) names nothing: a comment in
// many projects carries one.
func sourcePaths(f, owner string) []sourceRef {
	f = filepath.ToSlash(f)
	out := []sourceRef{{s: f}}
	mod, ok := strings.CutSuffix(f, ".py")
	if !ok {
		stem := strings.TrimSuffix(f, path.Ext(f))
		if stem != f && strings.Contains(stem, "/") && !strings.HasSuffix(stem, "/") {
			out = append(out, sourceRef{s: stem, bare: true})
		}
		return out
	}
	if path.Base(mod) == "__init__" {
		return out
	}
	if owner != "" {
		mod, ok = strings.CutPrefix(mod, owner+"/")
		if !ok {
			return out
		}
	}
	mod = strings.TrimPrefix(mod, "src/")
	if strings.Contains(mod, "/") && pyModule.MatchString(mod) {
		out = append(out, sourceRef{s: strings.ReplaceAll(mod, "/", "."), dotted: true})
	}
	return out
}

// pyModule is a module path whose segments are Python identifiers.
var pyModule = lazyre.New(`^[A-Za-z_][A-Za-z0-9_]*(/[A-Za-z_][A-Za-z0-9_]*)+$`)

// in is whether src names r whole: not inside a longer name. A path may
// follow a `/` (`../lib/app/x.py`) or a quote and is not followed by more of
// a name (`x.pyc`), nor, without its extension, by an extension
// (`lib/src/x.test`); an import path is not inside a longer dotted name, but
// for a name under it (`app.routers.items.ITEMS`).
func (r sourceRef) in(src string) bool {
	before := func(c byte) bool { return isNameByte(c) || c == '.' || c == '-' }
	after := func(c byte) bool { return isNameByte(c) || c == '/' || c == '-' || r.bare && c == '.' }
	if r.dotted {
		before = func(c byte) bool { return isNameByte(c) || c == '.' || c == '/' }
		after = isNameByte
	}
	for i := 0; ; {
		j := strings.Index(src[i:], r.s)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(r.s)
		if (start == 0 || !before(src[start-1])) && (end == len(src) || !after(src[end])) {
			return true
		}
		i = start + 1
	}
}

// isNameByte is a letter, digit, or underscore.
func isNameByte(c byte) bool {
	return c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// SourceReaders is, for each candidate, the changed source files (keys of
// owners, relative to the root, each mapped to its owning project) that one
// of the candidate's test modules names whole (sourcePaths): a test that
// reads another project's files (an infrastructure check over a backend's
// routers), which no manifest dependency declares. A project's own files are
// never counted for it. Each candidate's test modules are read once.
func SourceReaders(root string, candidates []string, owners map[string]string) map[string][]string {
	if len(owners) == 0 {
		return nil
	}
	files := make([]string, 0, len(owners))
	for f := range owners {
		files = append(files, f)
	}
	slices.Sort(files)
	frags := map[string][]sourceRef{}
	for _, f := range files {
		frags[f] = sourcePaths(f, owners[f])
	}
	out := map[string][]string{}
	for _, c := range candidates {
		var foreign []string
		for _, f := range files {
			if owners[f] != c {
				foreign = append(foreign, f)
			}
		}
		if len(foreign) == 0 {
			continue
		}
		read := map[string]bool{}
		testModules(root, c, func(_, src string) {
			for _, f := range foreign {
				if !read[f] && slices.ContainsFunc(frags[f], func(r sourceRef) bool { return r.in(src) }) {
					read[f] = true
				}
			}
		})
		for _, f := range foreign {
			if read[f] {
				out[c] = append(out[c], f)
			}
		}
	}
	return out
}
