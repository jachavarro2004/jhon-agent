package execution

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	root, _ := filepath.Abs("../../..")
	os.Setenv("JHON_ROOT", root)
	os.Exit(m.Run())
}

func write(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// gitT runs a test git command in dir, without inherited GIT_* variables.
func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=jhon-test", "-c", "user.email=jhon-test@example.invalid",
		"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main", "-C", dir}, args...)...)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func tempDir(t *testing.T) string {
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// ── affected ────────────────────────────────────────────────────────────────

func TestManifests(t *testing.T) {
	node, err := nodeManifest(`{"name":"app","dependencies":{"b":"1","2":"file:../two","1":"link:../one"},"devDependencies":["x"]}`)
	if err != nil || !reflect.DeepEqual(node, manifest{[]string{"app"}, []ref{{"1", "../one", true}, {"2", "../two", true}, {"b", "", true}, {"0", "", true}}}) {
		t.Errorf("nodeManifest = %+v, %v (index keys first, an array keyed by its indices)", node, err)
	}
	for src, msg := range map[string]string{"null": "Cannot read properties of null (reading 'name')", `{"a":`: "Unexpected end of JSON input", "{} {}": "Unexpected non-whitespace character after JSON"} {
		if _, err := nodeManifest(src); err == nil || err.Error() != msg {
			t.Errorf("nodeManifest(%q) error = %v", src, err)
		}
	}
	if m, err := nodeManifest(`[1]`); err != nil || len(m.refs)+len(m.names) != 0 {
		t.Errorf("a package.json that is not an object: %+v, %v", m, err)
	}

	gomod, _ := goManifest("module example.com/api // the api\nrequire example.com/core v1\nrequire (\n\texample.com/x v1 // x\n)\nreplace example.com/core => ../core v0\nreplace example.com/y => example.com/z v1\n")
	if !reflect.DeepEqual(gomod, manifest{[]string{"example.com/api"}, []ref{{"example.com/core", "", true}, {"example.com/x", "", true}, {"", "../core", false}}}) {
		t.Errorf("goManifest = %+v", gomod)
	}
	// /\/\/.*$/ cannot cross a \r, so a CRLF line keeps its comment.
	if crlf, _ := goManifest("require a // c\r\n"); !reflect.DeepEqual(crlf.refs, []ref{{"a", "", true}}) {
		t.Errorf("goManifest CRLF = %+v", crlf)
	}

	cargo, _ := cargoManifest("[package]\nname = \"app\"\n[dependencies]\nlib = { path = \"../lib\" }\nrenamed = { package = \"real\" }\n[target.'cfg(unix)'.dev-dependencies]\nu = \"1\"\n")
	if !reflect.DeepEqual(cargo, manifest{[]string{"app"}, []ref{{"lib", "../lib", true}, {"real", "", true}, {"u", "", true}}}) {
		t.Errorf("cargoManifest = %+v", cargo)
	}
	if _, err := cargoManifest("[package"); err == nil {
		t.Error("a malformed Cargo.toml is an error")
	}

	py, _ := pythonManifest("[project]\nname = \"My_App\"\ndependencies = [\"Lib.Core>=1\", \"loc @ file:///abs/loc\"]\n[project.optional-dependencies]\nx = [\"Extra\"]\n[tool.poetry.group.dev.dependencies]\nPoet = { path = \"../poet\" }\n[tool.uv.sources]\nsrc = { path = \"../src\" }\nnone = { path = \"\" }\n")
	if !reflect.DeepEqual(py, manifest{[]string{"my-app"}, []ref{{"lib-core", "", true}, {"loc", "/abs/loc", true}, {"extra", "", true}, {"poet", "../poet", true}, {"src", "../src", true}}}) {
		t.Errorf("pythonManifest = %+v", py)
	}
}

func TestDependencies(t *testing.T) {
	root := tempDir(t)
	write(t, filepath.Join(root, "lib/package.json"), `{"name":"lib"}`)
	write(t, filepath.Join(root, "app/package.json"), `{"name":"app","dependencies":{"other":"1","lib":"file:../lib"}}`)
	write(t, filepath.Join(root, "other/go.mod"), "module other\n")
	write(t, filepath.Join(root, "bad/Cargo.toml"), "[package\n")
	deps, incomplete, err := Dependencies(root, []string{"lib", "app", "other", "bad"})
	if err != nil || !reflect.DeepEqual(deps["app"], []string{"other", "lib"}) || len(deps["lib"]) != 0 {
		t.Errorf("deps = %v, %v", deps, err)
	}
	if len(incomplete) != 1 || !strings.HasPrefix(incomplete[0], "bad/Cargo.toml cannot be read for dependencies (") {
		t.Errorf("incomplete = %q", incomplete)
	}
}

// nodeWorkspace is lib, app (file: dependency on lib), tool (named
// dependency on lib), and other, committed and tagged base.
func nodeWorkspace(t *testing.T) string {
	root := tempDir(t)
	for rel, content := range map[string]string{
		"lib/package.json":   `{"name":"lib"}`,
		"app/package.json":   `{"name":"app","dependencies":{"lib":"file:../lib"}}`,
		"tool/package.json":  `{"name":"tool","devDependencies":{"lib":"^1"}}`,
		"other/package.json": `{"name":"other"}`,
		"README.md":          "# w\n",
		"openspec/x.md":      "x\n",
	} {
		write(t, filepath.Join(root, rel), content)
	}
	gitT(t, root, "init", "-q")
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "base")
	gitT(t, root, "tag", "base")
	return root
}

var projects = []string{"app", "lib", "other", "tool"}

func TestSelectAffected(t *testing.T) {
	root := nodeWorkspace(t)
	write(t, filepath.Join(root, "lib/index.js"), "x\n")
	write(t, filepath.Join(root, "openspec/x.md"), "y\n")
	s, msg, err := SelectAffected(root, "base", projects, projects, nil)
	want := Selection{Affected, []Selected{{"app", "depends on lib"}, {"lib", "changed: lib/index.js"}, {"tool", "depends on lib"}},
		[]string{"1 changed file(s) under openspec/ ignored: the change's record is not project input"}}
	if msg != "" || err != nil || !reflect.DeepEqual(s, want) {
		t.Errorf("SelectAffected = %+v, %q, %v", s, msg, err)
	}
	write(t, filepath.Join(root, "other/a.js"), "")
	if s, _, _ := SelectAffected(root, "base", projects, projects, nil); s.Scope != Full || s.Reasons[len(s.Reasons)-1] != "every project is affected" {
		t.Errorf("every project affected: %+v", s)
	}
	write(t, filepath.Join(root, "Makefile"), "changed\n")
	if s, _, _ := SelectAffected(root, "base", projects, []string{"app"}, nil); s.Scope != Full || !reflect.DeepEqual(s.Selected, []Selected{{"app", "all projects selected"}}) ||
		s.Reasons[1] != "every project selected: Makefile is outside every project (root configuration, a lockfile, CI, or a shared file)" {
		t.Errorf("a file outside every project: %+v", s)
	}

	inert := []string{"Makefile", "docs/"}
	write(t, filepath.Join(root, "docs/a.json"), "x\n")
	if s, _, _ := SelectAffected(root, "base", projects, projects, inert); s.Scope != Full || s.Reasons[len(s.Reasons)-1] != "every project is affected" ||
		!slices.Contains(s.Reasons, "2 changed file(s) ignored: executions_inert declares them no test input") {
		t.Errorf("inert files outside every project: %+v", s)
	}

	// Documentation selects only the projects whose tests name it, wherever it is.
	docs := nodeWorkspace(t)
	write(t, filepath.Join(docs, "README.md"), "changed\n")
	write(t, filepath.Join(docs, "lib/docs/api.md"), "changed\n")
	if s, _, _ := SelectAffected(docs, "base", projects, projects, nil); s.Scope != Affected || len(s.Selected) != 0 ||
		!slices.Contains(s.Reasons, "2 changed documentation file(s) ignored: no test module names them") {
		t.Errorf("documentation no test reads: %+v", s)
	}
	write(t, filepath.Join(docs, "other/tests/test_docs.py"), "DOC = 'api.md'\n")
	gitT(t, docs, "add", "other/tests/test_docs.py")
	gitT(t, docs, "commit", "-qm", "a test that reads the api doc")
	gitT(t, docs, "tag", "-f", "base")
	write(t, filepath.Join(docs, "lib/docs/api.md"), "changed again\n")
	if s, _, _ := SelectAffected(docs, "base", projects, projects, nil); s.Scope != Affected || len(s.Selected) != 0 {
		t.Errorf("a project's document named by another project's test is that project's own: %+v", s)
	}
	write(t, filepath.Join(docs, "lib/tests/test_api.py"), "DOC = 'docs/api.md'\n")
	if s, _, _ := SelectAffected(docs, "base", projects, projects, nil); !reflect.DeepEqual(s.Selected, []Selected{{"app", "depends on lib"}, {"lib", "changed: lib/docs/api.md, lib/tests/test_api.py"}, {"tool", "depends on lib"}}) {
		t.Errorf("a project's document its own test reads: %+v", s)
	}
	write(t, filepath.Join(docs, "other/tests/test_readme.py"), "DOC = 'README.md'\n")
	if s, _, _ := SelectAffected(docs, "base", projects, projects, nil); !slices.Contains(s.Selected, Selected{"other", "changed: README.md, other/tests/test_readme.py"}) {
		t.Errorf("a document outside every project, read by any project's test: %+v", s)
	}

	// A changed source file selects another project whose test modules name
	// its whole path or its module import path, never a fragment of either
	// (T145 E3): a directory, a bare file name, or a path in a comment that
	// is only a piece of it.
	for _, named := range []string{"lib/app/routers/items.py", "ROOT / '../lib/app/routers/items.py'", "from app.routers.items import ITEMS", "import app.routers.items as items"} {
		src := nodeWorkspace(t)
		write(t, filepath.Join(src, "other/tests/test_infra.py"), "ROUTES = '"+named+"'\n")
		gitT(t, src, "add", "-A")
		gitT(t, src, "commit", "-qm", "a test that reads lib's routers")
		gitT(t, src, "tag", "-f", "base")
		write(t, filepath.Join(src, "lib/app/routers/items.py"), "x\n")
		if s, _, _ := SelectAffected(src, "base", projects, projects, nil); s.Scope != Full ||
			!slices.Contains(s.Selected, Selected{"other", "changed: lib/app/routers/items.py"}) {
			t.Errorf("a source path %q named by another project's test: %+v", named, s)
		}
	}
	for _, named := range []string{"items.py", "routers/", "app/", "lib/app/routers", "app/routers/", "# fixtures live under tests/unit/ and app/routers/", "lib/app/routers/items.pyc", "xlib/app/routers/items.py", "app.routers.items_v2", "routers.items"} {
		bare := nodeWorkspace(t)
		write(t, filepath.Join(bare, "other/tests/test_infra.py"), "ROUTES = '"+named+"'\n")
		gitT(t, bare, "add", "-A")
		gitT(t, bare, "commit", "-qm", "a test that names a bare file")
		gitT(t, bare, "tag", "-f", "base")
		write(t, filepath.Join(bare, "lib/app/routers/items.py"), "x\n")
		if s, _, _ := SelectAffected(bare, "base", projects, projects, nil); slices.ContainsFunc(s.Selected, func(x Selected) bool { return x.Project == "other" }) {
			t.Errorf("a bare name %q selects nothing: %+v", named, s)
		}
	}

	quiet := nodeWorkspace(t)
	if s, _, _ := SelectAffected(quiet, "base", projects, projects, nil); s.Scope != Affected || len(s.Selected) != 0 || s.Reasons[0] != "no project is affected since base" {
		t.Errorf("no change: %+v", s)
	}
	if _, msg, _ := SelectAffected(quiet, "nope", projects, projects, nil); msg != "the base 'nope' does not resolve to a commit" {
		t.Errorf("a base that does not resolve: %q", msg)
	}
	plain := tempDir(t)
	if _, msg, _ := SelectAffected(plain, "base", nil, nil, nil); msg != plain+" is not inside a git work tree; --affected needs one" {
		t.Errorf("outside a repository: %q", msg)
	}
}

func TestChangedSinceFromASubdirectory(t *testing.T) {
	top := nodeWorkspace(t)
	write(t, filepath.Join(top, "lib/a.js"), "")
	write(t, filepath.Join(top, "README.md"), "changed\n")
	files, outside, msg := ChangedSince(filepath.Join(top, "lib"), "base")
	if msg != "" || !reflect.DeepEqual(files, []string{"a.js"}) || !reflect.DeepEqual(outside, []string{"README.md"}) {
		t.Errorf("ChangedSince = %q, %q, %q", files, outside, msg)
	}
}

// ── records ─────────────────────────────────────────────────────────────────

func TestRecordPaths(t *testing.T) {
	if p := RecordPath(tempDir(t), "c1"); p != "openspec/changes/c1/execution-record.json" {
		t.Errorf("RecordPath = %q", p)
	}
	top := tempDir(t)
	gitT(t, top, "init", "-q")
	os.MkdirAll(filepath.Join(top, "ws"), 0o755)
	want := map[string]bool{"ws/openspec/changes/c1/verify-report.md": true, "ws/openspec/changes/c1/execution-record.json": true, "ws/openspec/changes/c1/delivery.json": true}
	if got := ChangeRecordPaths(filepath.Join(top, "ws"), "c1"); !reflect.DeepEqual(got, want) {
		t.Errorf("ChangeRecordPaths = %v", got)
	}
	if got := ChangeDirInRepo(tempDir(t), "c1"); got != "openspec/changes/c1" {
		t.Errorf("ChangeDirInRepo outside a repository = %q", got)
	}
}

func TestReadLastRun(t *testing.T) {
	root := tempDir(t)
	file := filepath.Join(root, RecordPath(root, "c"))
	if r, err := ReadLastRun(root, "c"); r != nil || err != nil {
		t.Errorf("no record: %+v, %v", r, err)
	}
	for src, want := range map[string]*LastRun{
		`{"timestamp":"t","complete":true,"scope":"affected","dirty":true,"candidate_commit":"c","candidate_tree":"tr","working_tree_clean":true,
		  "executions":[{"project":"a","status":"pass"},{"project":"b","status":"timeout","exit_code":0}]}`: {"t", false, []string{"b"}, "affected", true, true, "c", "tr", true, false, nil, false, "", ""},
		// v1: no status, no complete; read by exit codes.
		`{"executions":[{"project":"a","exit_code":0},{"project":2,"exit_code":1},{"exit_code":null}],"working_tree_clean":"unknown"}`: {"", false, []string{"2", "undefined"}, Full, true, false, "", "", false, false, nil, false, "", ""},
		`{"complete":false,"executions":[{"project":"a","status":"pass"}]}`:                                                            {"", false, []string{}, Full, false, false, "", "", false, false, nil, false, "", ""},
		`{"timestamp":1e21,"executions":[{"project":"a","exit_code":-0}]}`:                                                             {"1e+21", true, []string{}, Full, true, false, "", "", false, false, nil, false, "", ""},
		`{"executions":[]}`: {"", false, []string{}, Full, true, false, "", "", false, false, nil, false, "", ""},
		// A pass after a failing run the history keeps: RED before GREEN.
		`{"timestamp":"t","executions":[{"project":"a","status":"pass"}],"history":[{"timestamp":"s","passed":false}]}`: {"t", true, []string{}, Full, true, false, "", "", false, true, nil, false, "", ""},
		// A unit's run (T133): the narrow kind; a record without the field reads as wide.
		`{"timestamp":"t","unit":true,"since":"main","executions":[{"project":"a","status":"pass"}]}`: {"t", true, []string{}, Full, true, false, "", "", false, false, nil, true, "", ""},
		`{"executions":{}}`: nil,
		`not json`:          nil,
		`{} x`:              nil,
	} {
		write(t, file, src)
		if got, err := ReadLastRun(root, "c"); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("ReadLastRun(%s) = %+v, %v; want %+v", src, got, err, want)
		}
	}
	write(t, file, "null")
	if _, err := ReadLastRun(root, "c"); err == nil {
		t.Error("a null record throws in the TypeScript")
	}
}

func TestWriteRecord(t *testing.T) {
	abs := filepath.Join(tempDir(t), "r.json")
	code := 3
	r := &Record{Schema: "s", Scope: Full, ScopeReasons: []string{}, Selection: []Selected{}, TimeoutMinutes: 0.01,
		CandidateFields: CandidateFields{"c", "t", "unknown"}, Executions: []Execution{{Project: "a", Command: "x", Cwd: "a", ExitCode: &code, Status: Fail, DurationMs: 5}}}
	if err := WriteRecord(abs, r); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(abs)
	want := `{
  "schema": "s",
  "timestamp": "",
  "complete": false,
  "scope": "full",
  "base": null,
  "scope_reasons": [],
  "selection": [],
  "timeout_minutes": 0.01,
  "candidate_commit": "c",
  "candidate_tree": "t",
  "working_tree_clean": "unknown",
  "after": null,
  "dirty": false,
  "executions": [
    {
      "project": "a",
      "command": "x",
      "cwd": "a",
      "exit_code": 3,
      "status": "fail",
      "duration_ms": 5
    }
  ]
}
`
	if string(b) != want {
		t.Errorf("record =\n%s", b)
	}
}

// ── processes ───────────────────────────────────────────────────────────────

func TestDeadline(t *testing.T) {
	for minutes, want := range map[float64]time.Duration{20: 20 * time.Minute, 0.01: 600 * time.Millisecond, 1e-9: time.Millisecond, 40000: time.Millisecond} {
		if got := Deadline(minutes); got != want {
			t.Errorf("Deadline(%v) = %v, want %v", minutes, got, want)
		}
	}
}

func TestRunOne(t *testing.T) {
	dir := tempDir(t)
	run := Run{Stdout: os.Stderr, Deadline: time.Minute}
	if o := RunOne("exit 3", dir, run); o.Code == nil || *o.Code != 3 {
		t.Errorf("exit 3: %+v", o)
	}
	if o := RunOne("kill -TERM $$", dir, run); o.Code == nil || *o.Code != 128+15 {
		t.Errorf("killed by a signal is 128 + its number: %+v", o)
	}
	if o := RunOne("pwd > out; read x; echo $? >> out", dir, run); o.Code == nil || *o.Code != 0 {
		t.Errorf("pwd: %+v", o)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "out")); string(b) != dir+"\n1\n" {
		t.Errorf("runs in cwd with stdin closed: %q", b)
	}
	if o := RunOne("true", filepath.Join(dir, "missing"), run); o.StartError != "spawn sh ENOENT" {
		t.Errorf("a missing directory: %+v", o)
	}

	// Past the deadline the group is killed, children included.
	start := time.Now()
	o := RunOne("(sleep 1; touch late) & wait", dir, Run{Stdout: os.Stderr, Deadline: 100 * time.Millisecond})
	if o.Code != nil || time.Since(start) > 900*time.Millisecond {
		t.Errorf("timeout: %+v after %v", o, time.Since(start))
	}
	time.Sleep(1200 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, "late")); err == nil {
		t.Error("the timed-out project's children survived")
	}

	// A signal stops a group that ignores TERM, and returns once it is gone.
	sigs := make(chan os.Signal, 1)
	pidFile := filepath.Join(dir, "pid")
	go func() {
		for {
			if b, err := os.ReadFile(pidFile); err == nil && len(b) > 0 {
				sigs <- syscall.SIGTERM
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	o = RunOne("trap '' TERM INT; echo $$ > pid; while :; do sleep 0.1; done", dir, Run{Stdout: os.Stderr, Deadline: time.Minute, Signals: sigs})
	if o.Interrupted != syscall.SIGTERM {
		t.Errorf("interrupted: %+v", o)
	}
	b, _ := os.ReadFile(pidFile)
	var pid int
	for _, c := range strings.TrimSpace(string(b)) {
		pid = pid*10 + int(c-'0')
	}
	// SIGKILL was sent; the process is gone once it is reaped.
	for end := time.Now().Add(2 * time.Second); syscall.Kill(pid, 0) == nil && time.Now().Before(end); {
		time.Sleep(20 * time.Millisecond)
	}
	if syscall.Kill(pid, 0) == nil {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Errorf("the project %d survived the interrupt", pid)
	}
}

func TestPassesOnReusesOnlyACompleteUndisturbedRunOnTheSameTree(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ChangesOf(root), "c"), 0o755); err != nil {
		t.Fatal(err)
	}
	zero, three := 0, 3
	write := func(r Record) {
		if err := WriteRecord(filepath.Join(root, RecordPath(root, "c")), &r); err != nil {
			t.Fatal(err)
		}
	}
	base := Record{Schema: Schema(), Complete: true, WorkingTree: "t1", AfterWorkingTree: "t1", Executions: []Execution{
		{Project: "a", Command: "go test ./...", ExitCode: &zero, Status: Pass},
		{Project: "a", Kind: "lint", Command: "go vet ./...", ExitCode: &zero, Status: Pass},
		{Project: "b", Command: "npm test", ExitCode: &three, Status: Fail},
	}}
	write(base)
	got := PassesOn(root, "c", "t1")
	if _, ok := got[Key("a", "", "go test ./...")]; !ok || len(got) != 2 {
		t.Errorf("the passes on the same tree: %v", got)
	}
	if len(PassesOn(root, "c", "t2")) != 0 || len(PassesOn(root, "c", "")) != 0 || len(PassesOn(root, "gone", "t1")) != 0 {
		t.Error("another tree, an unknown one, or no record reuses nothing")
	}
	for name, r := range map[string]Record{"partial": {Complete: false}, "disturbed": {Complete: true, Dirty: true}} {
		r.Schema, r.WorkingTree, r.AfterWorkingTree, r.Executions = Schema(), "t1", "t1", base.Executions
		write(r)
		if len(PassesOn(root, "c", "t1")) != 0 {
			t.Errorf("a %s run reuses nothing", name)
		}
	}
	// T146f: a record with no end-of-run tree (an earlier binary's), or
	// another one, cannot show its passes saw the tree it started on.
	for _, after := range []string{"", "t2"} {
		r := base
		r.AfterWorkingTree = after
		write(r)
		if len(PassesOn(root, "c", "t1")) != 0 {
			t.Errorf("a run whose after_working_tree is %q reuses nothing", after)
		}
	}
}

// T146f: a unit's run measures from the last run that kept its working tree
// from start to end; a record an earlier binary wrote shows neither that
// nor that the passing run it kept did.
func TestLastPassOfTrustsOnlyARunWithItsEndOfRunTree(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ChangesOf(root), "c"), 0o755); err != nil {
		t.Fatal(err)
	}
	zero := 0
	kept := &PassedRun{"2026-01-01T00:00:00.000Z", "t0"}
	of := func(after string, dirty bool) *PassedRun {
		r := Record{Schema: Schema(), Timestamp: "2026-02-02T00:00:00.000Z", Complete: true, WorkingTree: "t1", AfterWorkingTree: after, Dirty: dirty,
			LastPass: kept, Executions: []Execution{{Project: "a", Command: "go test ./...", ExitCode: &zero, Status: Pass}}}
		if err := WriteRecord(filepath.Join(root, RecordPath(root, "c")), &r); err != nil {
			t.Fatal(err)
		}
		return LastPassOf(root, "c")
	}
	if got := of("t1", false); got == nil || *got != (PassedRun{"2026-02-02T00:00:00.000Z", "t1"}) {
		t.Errorf("a run with its end-of-run tree: %+v", got)
	}
	if got := of("t2", true); got == nil || *got != *kept {
		t.Errorf("a disturbed run keeps the pass before it: %+v", got)
	}
	if got := of("", false); got != nil {
		t.Errorf("an earlier binary's record: %+v", got)
	}
}

func TestTailKeepsTheEndFromAWholeLine(t *testing.T) {
	var tail Tail
	tail.Write([]byte("first\n"))
	if got := tail.String(); got != "first" {
		t.Errorf("short output: %q", got)
	}
	for i := 0; i < TailBytes/10; i++ {
		fmt.Fprintf(&tail, "line %04d\n", i)
	}
	got := tail.String()
	if len(got) >= TailBytes || !strings.HasPrefix(got, "line ") || !strings.HasSuffix(got, fmt.Sprintf("line %04d", TailBytes/10-1)) {
		t.Errorf("long output kept %d bytes: %q ... %q", len(got), got[:20], got[len(got)-20:])
	}
}

func TestFailedTests(t *testing.T) {
	out := "tests/a.py F.\nFAILED tests/a.py::test_x[1] - assert 1 == 2\nFAILED tests/a.py::test_x[1] - again\nERROR tests/b.py::test_y\n FAIL  src/a.test.ts > adds > one\nFAILED to start\n"
	want := []string{"tests/a.py::test_x[1]", "tests/b.py::test_y", "src/a.test.ts > adds > one"}
	if got := FailedTests(out); !reflect.DeepEqual(got, want) {
		t.Errorf("FailedTests = %q", got)
	}
}

// T145c H3 (L1191): a source file of a language whose imports drop the
// extension (JS/TS) selects another project whose test names its whole path
// without it, relative (`../../lib/src/x`) or from the root (`lib/src/x`);
// a directory fragment, a longer name or a path under it selects nothing.
func TestSelectAffectedByAnExtensionlessReference(t *testing.T) {
	for _, c := range []struct {
		named    string
		selected bool
	}{
		{"import { x } from '../../lib/src/x'", true},
		{"import { x } from 'lib/src/x'", true},
		{"const x = require(\"lib/src/x\")", true},
		{"// fixtures live under src/ and tests/unit/", false},
		{"import { x } from '../../lib/src'", false},
		{"import { x } from '../../lib/src/xy'", false},
		{"import { x } from '../../lib/src/x/y'", false},
		{"import { x } from '../../lib/src/x.test'", false},
		{"import { x } from '../../xlib/src/x'", false},
	} {
		root := nodeWorkspace(t)
		write(t, filepath.Join(root, "other/tests/x.test.ts"), c.named+"\n")
		gitT(t, root, "add", "-A")
		gitT(t, root, "commit", "-qm", "a test that imports lib's module")
		gitT(t, root, "tag", "-f", "base")
		write(t, filepath.Join(root, "lib/src/x.ts"), "export const x = 1\n")
		s, _, _ := SelectAffected(root, "base", projects, projects, nil)
		if got := slices.ContainsFunc(s.Selected, func(x Selected) bool { return x.Project == "other" }); got != c.selected ||
			got && !slices.Contains(s.Selected, Selected{"other", "changed: lib/src/x.ts"}) {
			t.Errorf("%q selects other: %v, want %v (%+v)", c.named, got, c.selected, s)
		}
	}
}
