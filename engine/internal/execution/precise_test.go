package execution

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPreciseRunsPytestUnderTestmon(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/cache")
	got, ok := Precise("uv run --extra dev pytest", "/repo", "backend/apps/svc", false)
	if !ok {
		t.Fatal("a uv-run pytest command runs under testmon")
	}
	data := filepath.Join("/cache", "jhon", "testmon")
	for _, want := range []string{"mkdir -p '" + data + "' && TESTMON_DATAFILE='" + data, "uv run --with pytest-testmon --extra dev pytest", "--testmon --rootdir='/repo'"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q lacks %q", got, want)
		}
	}
	// One project's data never answers for another's, nor another repository's.
	other, _ := Precise("uv run --extra dev pytest", "/repo", "backend/apps/other", false)
	elsewhere, _ := Precise("uv run --extra dev pytest", "/fork", "backend/apps/svc", false)
	if datafile(got) == datafile(other) || datafile(got) == datafile(elsewhere) {
		t.Errorf("data files are shared: %q %q %q", got, other, elsewhere)
	}
	if q, _ := Precise(`uv run --extra dev pytest -k "a b"`, "/repo", "p", false); !strings.Contains(q, `pytest -k "a b" --testmon`) {
		t.Errorf("the command's own quoting is kept: %q", q)
	}
	for _, c := range []string{"npm test", "go test ./...", "poetry run pytest", "uv run --extra dev ruff check .", "pytest"} {
		if _, ok := Precise(c, "/repo", "p", false); ok {
			t.Errorf("%q is not a uv-run pytest command", c)
		}
	}
	if _, ok := Precise("uv run --extra dev pytest", "", "p", false); ok {
		t.Error("outside a git repository there is no root to track")
	}
}

// testmon refuses to run beside pytest-cov with branch coverage on (a client
// project's services: `TestmonException ... Please disable branch coverage`, exit 3),
// so a project that declares pytest-cov runs its selection with --no-cov.
func TestPreciseTurnsPytestCovOffForTestmon(t *testing.T) {
	dir := t.TempDir()
	if PytestCov(dir) {
		t.Error("a directory without pyproject.toml runs no pytest-cov")
	}
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[project.optional-dependencies]\ndev = [\"pytest-cov>=6.0.0\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !PytestCov(dir) {
		t.Error("a project that declares pytest-cov runs it")
	}
	with, _ := Precise("uv run --extra dev pytest", "/repo", "p", true)
	without, _ := Precise("uv run --extra dev pytest", "/repo", "p", false)
	if !strings.HasSuffix(with, " --no-cov") || strings.Contains(without, "--no-cov") {
		t.Errorf("--no-cov only beside pytest-cov: %q, %q", with, without)
	}
}

// A crashed run (testmon refusing branch coverage) left a client project's records with
// no file fingerprint, and every test was selected again: a dry run says so
// instead of leaving the agent to guess whether a run narrows.
func TestTestmonRecord(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 reads the record")
	}
	dir := t.TempDir()
	db := filepath.Join(dir, "t.db")
	if got := TestmonRecord(db); got != RecordNone {
		t.Errorf("no file: %s", got)
	}
	write := func(fsha string) {
		os.Remove(db)
		script := "import sqlite3,sys\nc=sqlite3.connect(sys.argv[1])\nc.execute('create table file_fp (id integer primary key, filename text, method_checksums blob, mtime float, fsha text)')\nc.execute('insert into file_fp (filename, fsha) values (?, ?)', ('a.py', sys.argv[2] or None))\nc.commit()"
		if out, err := exec.Command("python3", "-I", "-c", script, db, fsha).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	write("abc")
	if got := TestmonRecord(db); got != RecordUsable {
		t.Errorf("fingerprinted: %s", got)
	}
	write("")
	if got := TestmonRecord(db); got != RecordUnusable {
		t.Errorf("no fingerprint: %s", got)
	}
	if err := os.WriteFile(db, []byte("not a database"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := TestmonRecord(db); got != RecordUnknown {
		t.Errorf("unreadable: %s", got)
	}
}

// A long run is announced with how long the project's tests last took, so
// the owner is told when to expect it instead of asking.
func TestLastTestRun(t *testing.T) {
	root := t.TempDir()
	rec := func(change, body string, age time.Duration) {
		f := filepath.Join(root, "openspec", "changes", change, "execution-record.json")
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-age)
		if err := os.Chtimes(f, at, at); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := LastTestRun(root, "svc"); ok {
		t.Error("no record, no duration")
	}
	rec("archive/2026-10-01-old", `{"executions": [{"project": "svc", "status": "pass", "duration_ms": 60000}]}`, 2*time.Hour)
	rec("new", `{"executions": [{"project": "svc", "kind": "lint", "status": "pass", "duration_ms": 5}, {"project": "svc", "status": "fail", "duration_ms": 2460000}]}`, time.Hour)
	rec("reused", `{"executions": [{"project": "svc", "status": "pass", "reused": true, "duration_ms": 1}]}`, 0)
	if ms, ok := LastTestRun(root, "svc"); !ok || ms != 2460000 {
		t.Errorf("the newest record that ran the tests: %d %v", ms, ok)
	}
}

func datafile(command string) string { return strings.Fields(command)[4] }

func TestNxAffectedNarrowsRunMany(t *testing.T) {
	for in, want := range map[string]string{
		"npx --no-install nx run-many -t test":       "npx --no-install nx affected -t test --base='main'",
		"npx nx run-many --target=test --parallel=3": "npx nx affected --target=test --parallel=3 --base='main'",
		"nx run-many -t test --configuration=ci":     "nx affected -t test --configuration=ci --base='main'",
	} {
		if got, ok := NxAffected(in, "main"); !ok || got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	for _, c := range []string{"npm test", "npx nx run-many -t lint", "npx nx run-many -t testing", "nx affected -t test"} {
		if _, ok := NxAffected(c, "main"); ok {
			t.Errorf("%q is not a run-many of the test target", c)
		}
	}
}

// Inside each project Nx selects, vitest --related runs only the tests whose
// module graph reaches a changed file. It reads no configuration, and no
// Angular template or style, so a template stands for its component and any
// other change keeps the projects whole.
func TestVitestNarrowing(t *testing.T) {
	if got, _ := NxAffected("npx --no-install nx run-many -t test", "main"); got != "npx --no-install nx affected -t test --base='main'" {
		t.Errorf("unnarrowed: %q", got)
	}
	if got := VitestRelated("npx --no-install nx affected -t test --base='main'", []string{"/w/a.ts", "/w/b.ts"}); got != "npx --no-install nx affected -t test --base='main' -- --passWithNoTests --related='/w/a.ts' --related='/w/b.ts'" {
		t.Errorf("narrowed: %q", got)
	}
	root := t.TempDir()
	for _, f := range []string{"libs/a/src/a.ts", "apps/b/src/b.component.ts", "apps/b/src/b.component.html", "apps/b/src/b.component.scss", "apps/b/src/_vars.scss", "tools/x.mjs", "libs/a/vite.config.ts"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	at := func(fs ...string) []string {
		var out []string
		for _, f := range fs {
			out = append(out, filepath.Join(root, f))
		}
		return out
	}
	for _, c := range []struct {
		files []string
		want  []string
	}{
		{[]string{"libs/a/src/a.ts", "tools/x.mjs", "openspec/changes/c/tasks.md"}, at("libs/a/src/a.ts", "tools/x.mjs")},
		{[]string{"apps/b/src/b.component.html", "apps/b/src/b.component.scss", "apps/b/src/b.component.ts"}, at("apps/b/src/b.component.ts")},
		{[]string{"libs/a/src/a.ts", "libs/a/vite.config.ts"}, nil},
		{[]string{"apps/b/src/_vars.scss"}, nil},
		{[]string{"libs/a/src/gone.ts"}, nil},
		{[]string{"tsconfig.base.json"}, nil},
		{[]string{"openspec/changes/c/tasks.md"}, nil},
	} {
		got, ok := RelatedFiles(root, c.files)
		if ok != (c.want != nil) || ok && !slices.Equal(got, c.want) {
			t.Errorf("RelatedFiles(%q) = %q, %v", c.files, got, ok)
		}
	}
	dir := t.TempDir()
	for pkg, want := range map[string]bool{
		`{"devDependencies":{"nx":"22","vitest":"4.0.9"}}`:             true,
		`{"devDependencies":{"nx":"22","vitest":"4.0.9","jest":"30"}}`: false,
		`{"devDependencies":{"nx":"22"}}`:                              false,
	} {
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := VitestOnly(dir); got != want {
			t.Errorf("VitestOnly(%s) = %v", pkg, got)
		}
	}
}

// T145c H1 (L1191): a wide step's two parts both run, the second even when
// the first fails, and the step exits with the worse (higher) of their
// statuses, so the failures of both are reported in one pass.
func TestBothCommandRunsEachPartAndExitsWithTheWorse(t *testing.T) {
	dir := t.TempDir()
	for i, c := range []struct{ a, b, want int }{{0, 0, 0}, {1, 0, 1}, {0, 1, 1}, {2, 1, 2}, {1, 2, 2}} {
		log := filepath.Join(dir, strconv.Itoa(i))
		part := func(name string, code int) string {
			return "(echo " + name + " >> " + shq(log) + "; exit " + strconv.Itoa(code) + ")"
		}
		err := exec.Command("sh", "-c", BothCommand(part("a", c.a), part("b", c.b))).Run()
		got := 0
		if x, ok := err.(*exec.ExitError); ok {
			got = x.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(log)
		if got != c.want || string(b) != "a\nb\n" {
			t.Errorf("parts exiting %d, %d: exit %d, ran %q; want exit %d, both run", c.a, c.b, got, b, c.want)
		}
	}
}

// T145c H1: the test runs a command the engine writes is made of, and
// whether each runs whatever those before it did (BothCommand) or only when
// they passed (`&&`); a pytest-testmon run keeps the prefix that makes its
// record's directory.
func TestCommandParts(t *testing.T) {
	testmon := "mkdir -p '/c' && TESTMON_DATAFILE='/c/x.db' uv run --with pytest-testmon pytest --testmon --rootdir='/r'"
	for _, c := range []struct {
		command string
		parts   []string
		each    bool
	}{
		{"uv run pytest", []string{"uv run pytest"}, true},
		{testmon + " 'tests/a.py'", []string{testmon + " 'tests/a.py'"}, true},
		{BothCommand("uv run pytest 'tests/a.py'", testmon+" 'tests/b.py'"), []string{"uv run pytest 'tests/a.py'", testmon + " 'tests/b.py'"}, true},
		{BothCommand(testmon+" 'tests/b.py'", "uv run pytest 'tests/a.py'"), []string{testmon + " 'tests/b.py'", "uv run pytest 'tests/a.py'"}, true},
		{"uv run pytest 'tests/r.py' && " + testmon, []string{"uv run pytest 'tests/r.py'", testmon}, false},
		{testmon + " && uv run pytest 'tests/r.py'", []string{testmon, "uv run pytest 'tests/r.py'"}, false},
	} {
		parts, each := CommandParts(c.command)
		if !slices.Equal(parts, c.parts) || each != c.each {
			t.Errorf("CommandParts(%q) = %q, %v; want %q, %v", c.command, parts, each, c.parts, c.each)
		}
	}
}
