package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// git runs git in dir, failing the test on an error.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir, c.Env = dir, append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// A plain run is clean only with no ceremony (L482, L483): no openspec/ or
// ldd/ created, no jhon executions, archive or deliver, no branch and no
// commit its prompt did not ask for. Each kind is counted.
func TestCeremonyCountsWhatAPlainRunDidUnasked(t *testing.T) {
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "pager.py"), []byte("x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gitRepo(repo, env); err != nil {
		t.Fatal(err)
	}
	start, err := headCommit(repo, env)
	if err != nil {
		t.Fatal(err)
	}
	git(t, repo, "switch", "-q", "-c", "fix/pager")
	for _, d := range []string{"ldd/changes/fix", "openspec/changes/fix"} {
		if err := os.MkdirAll(filepath.Join(repo, d), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, d, "tasks.md"), []byte("- [x] 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "-m", "one")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "two")
	transcript := claudeTranscript(
		step{tool: "Bash", command: "jhon executions run fix -- python3 -m unittest"},
		step{tool: "Bash", command: "cd . && JHON_X=1 jhon deliver check"},
		step{tool: "Bash", command: `git commit -m "jhon archive fix"`},
		step{tool: "Bash", command: "jhon status"},
		step{tool: "Bash", command: "jhon archive fix | tail -3"},
	)
	task := Task{ID: "pagination-bug", Scope: []string{"pager.py"}, Tests: "test_*.py", TestCommand: "python3 -m unittest"}
	m := &Metrics{}
	m.fromCeremony(task, "plain", repo, start, env)
	m.fromTranscript(transcript, task.TestCommand)
	// L488 "Commits sí, el resto no": the two commits are recorded, never ceremony.
	want := Ceremony{Layouts: []string{"ldd", "openspec"}, Executions: 1, Archive: 1, Deliver: 1, Branches: 1, Commits: 2}
	if m.Ceremony == nil || !ceremonyEqual(*m.Ceremony, want) || m.Ceremony.Total() != 6 {
		t.Fatalf("ceremony %+v, want %+v (total 6)", m.Ceremony, want)
	}
	b, _ := json.Marshal(m.Ceremony)
	if !bytes.Contains(b, []byte(`"total":6`)) {
		t.Errorf("ceremony json lacks its total: %s", b)
	}
	if strings.Contains(m.Ceremony.String(), "commits") {
		t.Errorf("commits shown as ceremony: %s", m.Ceremony)
	}
	// An LDD variant has no ceremony to count: its record is what it asks for.
	m = &Metrics{}
	m.fromCeremony(task, "ldd-ask", repo, start, env)
	m.fromTranscript(transcript, task.TestCommand)
	if m.Ceremony != nil {
		t.Errorf("ldd-ask counted ceremony %+v", m.Ceremony)
	}
}

func ceremonyEqual(a, b Ceremony) bool {
	return strings.Join(a.Layouts, ",") == strings.Join(b.Layouts, ",") && a.Executions == b.Executions && a.Archive == b.Archive &&
		a.Deliver == b.Deliver && a.Branches == b.Branches && a.Commits == b.Commits
}

// L488: a commit per finished slice keeps a plain run clean; a new branch
// does not.
func TestACommitAloneKeepsAPlainRunCleanAndABranchDoesNot(t *testing.T) {
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	repo := t.TempDir()
	if err := gitRepo(repo, env); err != nil {
		t.Fatal(err)
	}
	start, err := headCommit(repo, env)
	if err != nil {
		t.Fatal(err)
	}
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "slice 1")
	task := Task{ID: "x", Tests: "test_*.py", TestCommand: "python3 -m unittest"}
	m := &Metrics{}
	m.fromCeremony(task, "plain", repo, start, env)
	if m.Ceremony == nil || m.Ceremony.Total() != 0 || m.Ceremony.Commits != 1 {
		t.Fatalf("a commit alone: %+v", m.Ceremony)
	}
	git(t, repo, "switch", "-q", "-c", "feature")
	m = &Metrics{}
	m.fromCeremony(task, "plain", repo, start, env)
	if m.Ceremony.Total() != 1 || m.Ceremony.Branches != 1 {
		t.Fatalf("a branch: %+v", m.Ceremony)
	}
}

func TestAPlainFakeRunHasNoCeremonyAndAnLDDOneHasNone(t *testing.T) {
	f := newFake(t, "0.40")
	o, out := options(t, f, "pagination-bug", []string{"jhon", "clean"}, 1)
	o.Variants = []string{"plain", "ldd"}
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if m := metricsOf(t, o, "pagination-bug-plain-clean-1"); m.Ceremony == nil || m.Ceremony.Total() != 0 || m.LddLayout != nil {
		t.Errorf("plain clean: ceremony %+v layout %+v", m.Ceremony, m.LddLayout)
	}
	// Jhon always works under LDD (T72b): a Jhon plain run's layout and
	// record are read. It starts set up (T146 FB2), so its layout is ldd
	// although the fake writes nothing.
	if m := metricsOf(t, o, "pagination-bug-plain-jhon-1"); m.Ceremony == nil || m.LddLayout == nil || m.LddLayout.Layout != "ldd" {
		t.Errorf("plain jhon: ceremony %+v layout %+v", m.Ceremony, m.LddLayout)
	}
	if m := metricsOf(t, o, "pagination-bug-ldd-jhon-1"); m.Ceremony != nil || m.LddLayout == nil || m.LddLayout.Layout != "openspec" {
		t.Errorf("ldd: ceremony %+v layout %+v", m.Ceremony, m.LddLayout)
	}
}

// ldd_layout (L483): which layout an LDD run's repo has at the end, and
// whether the record exists: a change's tasks.md and a test record.
func TestTheLDDLayoutOfAskAndNativeRuns(t *testing.T) {
	f := newFake(t, "0.40", "0.40")
	sol := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sol, "ldd", "changes", "fix-pager"), 0o755); err != nil {
		t.Fatal(err)
	}
	for file, body := range map[string]string{"ldd/config.yaml": "strict_tdd_policy: require\n", "ldd/changes/fix-pager/tasks.md": "- [x] 1.1 fix\n"} {
		if err := os.WriteFile(filepath.Join(sol, file), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("FAKE_SOLUTION", sol)
	if err := os.WriteFile(filepath.Join(f.dir, "status.json"), []byte(`{"ok":true,"tests":{"passed":true,"red_before_green":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	o, out := options(t, f, "pagination-bug", []string{"jhon"}, 1)
	o.Variants = []string{"ldd-native", "ldd-ask"}
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	// The fake status has a test record and no route: tasks.md is there, so
	// whatever the route asks is kept.
	want := LddLayout{Layout: "ldd", TasksMD: true, TestRecord: true, RouteRequiresFolder: "unknown", Record: bp(true)}
	for _, name := range []string{"pagination-bug-ldd-native-jhon-1", "pagination-bug-ldd-ask-jhon-1"} {
		if m := metricsOf(t, o, name); m.LddLayout == nil || !layoutEqual(*m.LddLayout, want) {
			t.Errorf("%s: %+v, want %+v", name, m.LddLayout, want)
		}
	}

	// With nothing written and no test record: none, no record.
	t.Setenv("FAKE_SOLUTION", "")
	if err := os.Remove(filepath.Join(f.dir, "status.json")); err != nil {
		t.Fatal(err)
	}
	o, out = options(t, f, "pagination-bug", []string{"jhon"}, 1)
	o.Variants = []string{"ldd-ask"}
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if m := metricsOf(t, o, "pagination-bug-ldd-ask-jhon-1"); m.LddLayout == nil || !layoutEqual(*m.LddLayout, LddLayout{Layout: "none", RouteRequiresFolder: "unknown", Record: bp(false)}) {
		t.Errorf("empty ldd-ask: %+v", m.LddLayout)
	}
}

func layoutEqual(a, b LddLayout) bool {
	return a.Layout == b.Layout && a.TasksMD == b.TasksMD && a.TestRecord == b.TestRecord && a.RouteRequiresFolder == b.RouteRequiresFolder &&
		(a.Record == nil) == (b.Record == nil) && (a.Record == nil || *a.Record == *b.Record)
}

// The LDD record follows the route (owner, after L493: "Sí, que siga la
// ruta"): the test record, plus a folder with tasks.md only when the
// change's route needs it — `jhon status --json` needs.tasks.
func TestTheLDDRecordFollowsTheRoute(t *testing.T) {
	const record = `"tests":{"passed":true,"red_before_green":true}`
	small := `{"change":"late-fee",` + record + `,"needs":{"tasks":false,"spec":false,"proposal":false,"design":false},"folder":false}`
	large := `{"change":"two-modules",` + record + `,"needs":{"tasks":true,"spec":false,"proposal":false,"design":false},"folder":true}`
	for _, c := range []struct {
		name, status string
		tasksMD      bool
		want         LddLayout
	}{
		{"a small change with only a test record", small, false, LddLayout{Layout: "openspec", TestRecord: true, RouteRequiresFolder: "false", Record: bp(true)}},
		{"a route that needs a folder, no tasks.md", large, false, LddLayout{Layout: "openspec", TestRecord: true, RouteRequiresFolder: "true", Record: bp(false)}},
		{"a route that needs a folder, tasks.md and a record", large, true, LddLayout{Layout: "openspec", TasksMD: true, TestRecord: true, RouteRequiresFolder: "true", Record: bp(true)}},
		{"no route after the run, no tasks.md", `{"change":null,` + record + `}`, false, LddLayout{Layout: "openspec", TestRecord: true, RouteRequiresFolder: "unknown"}},
		{"a route that needs a folder, no test record", `{"change":"two-modules","tests":null,"needs":{"tasks":true}}`, true, LddLayout{Layout: "openspec", TasksMD: true, RouteRequiresFolder: "true", Record: bp(false)}},
	} {
		repo := t.TempDir()
		dir := filepath.Join(repo, "openspec", "changes", "two-modules")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if c.tasksMD {
			if err := os.WriteFile(filepath.Join(dir, "tasks.md"), []byte("- [x] 1\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if got := lddLayoutOf(repo, []byte(c.status)); !layoutEqual(*got, c.want) {
			t.Errorf("%s: %+v (record %v), want %+v", c.name, *got, got.Record, c.want)
		}
	}
}

// slices: per message, its check passed at that message (integrable and
// tested) and no later message broke it; one fraction per run.
func TestSlicesAreTheMessagesWhoseCheckPassedAndHeld(t *testing.T) {
	checks := []MessageCheck{
		{After: 1, Check: 1, Passed: true},
		{After: 2, Check: 1, Passed: true}, {After: 2, Check: 2, Passed: false},
		{After: 3, Check: 1, Passed: false}, {After: 3, Check: 2, Passed: true}, {After: 3, Check: 3, Passed: true},
	}
	if got := slicesHeld(checks, 3); got == nil || *got != 1.0/3 {
		t.Fatalf("slices %v, want 1/3 (1 broke later, 2 failed at its message, 3 held)", got)
	}
	if got := slicesHeld(checks[:1], 1); got == nil || *got != 1 {
		t.Errorf("one held slice: %v", got)
	}
	if got := slicesHeld(checks[:1], 2); got == nil || *got != 0.5 {
		t.Errorf("a message the run never reached is no slice: %v", got)
	}
	if slicesHeld(nil, 0) != nil {
		t.Errorf("no checks is unknown")
	}
}

// A refusal is a tool call the safety layer or a permission rule denied.
func TestRefusalsAreCountedFromTheTranscript(t *testing.T) {
	for name, b := range map[string][]byte{
		"claude": claudeTranscript(
			step{tool: "Bash", command: "rm -rf build", failed: true, output: "PreToolUse:Bash hook error: Jhon refused it"},
			step{tool: "Bash", command: "curl x", failed: true, output: "Permission to use Bash with command curl x has been denied."},
			step{tool: "Bash", command: "python3 -m unittest", failed: true, output: "FAILED (failures=1)"},
		),
		"pi": piTranscript(
			step{tool: "bash", command: "rm -rf build", failed: true, output: "PreToolUse:Bash hook error: no"},
			step{tool: "bash", command: "git push", failed: true, output: "Jhon safety guard refused: git push"},
		),
	} {
		m := &Metrics{}
		m.fromTranscript(b, "python3 -m unittest")
		if m.Refusals == nil || *m.Refusals != 2 {
			t.Errorf("%s: refusals %v, want 2", name, m.Refusals)
		}
	}
}

// report.json carries the new metrics per row, so certify can read it
// alone: ceremony summed, the LDD layouts and records, the slices and
// refusals medians, and the quota-weighted tokens (L432's weighting).
func TestTheReportCarriesTheCertifyMetrics(t *testing.T) {
	dir := t.TempDir()
	cer := func(n int) *Ceremony { return &Ceremony{Executions: n} }
	writeMetrics(t, dir,
		Metrics{Task: "late-fee", Variant: "plain", Condition: "jhon", Repeat: 1, Ceremony: cer(0), Slices: f64(1), Refusals: ip(0),
			InputTokens: 100, CacheWriteTokens: 1000, CacheReadTokens: 10000, OutputTokens: 10},
		Metrics{Task: "late-fee", Variant: "plain", Condition: "jhon", Repeat: 2, Ceremony: cer(2), Slices: f64(0.5), Refusals: ip(1),
			InputTokens: 100, CacheWriteTokens: 1000, CacheReadTokens: 10000, OutputTokens: 10},
	)
	d := filepath.Join(dir, "runs", "ask")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(d, "metrics.json"), Metrics{Task: "late-fee", Variant: "ldd-ask", Condition: "pi-jhon", Repeat: 1,
		LddLayout: &LddLayout{Layout: "ldd", TasksMD: true, TestRecord: true, RouteRequiresFolder: "true", Record: bp(true)}}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Report(dir, &out); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "report.json"))
	var rep struct{ Rows []Row }
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	plain, ask := rep.Rows[0], rep.Rows[1]
	if plain.Quality.Ceremony == nil || plain.Quality.Ceremony.Executions != 2 || plain.Medians["slices"] != 0.75 || plain.Medians["refusals"] != 0.5 ||
		plain.Medians["quota_weighted"] != 100+1250+1000+50 {
		t.Errorf("plain row: %+v %+v", plain.Quality, plain.Medians)
	}
	if ask.Quality.Ldd == nil || ask.Quality.Ldd.Layouts["ldd"] != 1 || ask.Quality.Ldd.Records != 1 || ask.Quality.Ldd.Known != 1 || ask.Quality.Ceremony != nil {
		t.Errorf("ldd-ask row: %+v", ask.Quality)
	}
	for _, s := range []string{"ceremony", "ldd layout", "ldd:1 (record 1/1)", "executions:2"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("table lacks %q:\n%s", s, out.String())
		}
	}
}
