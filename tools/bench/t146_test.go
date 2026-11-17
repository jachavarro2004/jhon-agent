package main

import (
	"bytes"
	"encoding/json"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// T146: Jhon's value against no Jhon, gated per task size (FB1), on a repo
// already set up for LDD with the set-up measured apart (FB2), costed per
// provider (FB3), as isolated as clean (FB4), with spread and repeats per
// size (FB5).

// sizedRuns writes 3 runs of task in every condition: the Jhon ones at
// jhonCost, the clean ones at cost 1; size goes in each run's task_size.
func sizedRuns(t *testing.T, task, size string, jhonCost float64, edit, cleanEdit func(m *Metrics)) string {
	t.Helper()
	dir := t.TempDir()
	for rep := 1; rep <= 3; rep++ {
		for _, cond := range KnownConditions {
			cost, e := jhonCost, edit
			if isClean(cond) {
				cost, e = 1, cleanEdit
			}
			m := cellRun("plain", cond, rep, cost)
			m.Task, m.TaskSize = task, size
			if isClean(cond) {
				m.Rework = ip(2)
				if rep == 1 {
					m.RedBeforeGreen, m.TestsRunBeforeEnd = bp(false), bp(false)
				}
			}
			if e != nil {
				e(&m)
			}
			putRuns(t, dir, m)
		}
	}
	return dir
}

func TestEveryTaskHasItsSize(t *testing.T) {
	ts, err := LoadTasks("tasks", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"pagination-bug": "small", "shipping-rules": "small", "account-import": "small",
		"search-accents": "large", "contacts-migration": "large", "late-fee": "large", "invoice-slices": "large", "slow-suite": "large", "two-modules": "large"}
	got := map[string]string{}
	for _, task := range ts {
		got[task.ID] = task.Size
	}
	if len(got) != len(want) {
		t.Errorf("tasks %v, want %v", got, want)
	}
	for id, size := range want {
		if got[id] != size {
			t.Errorf("%s: size %q, want %q", id, got[id], size)
		}
	}
	if sizes := taskSizes("tasks"); sizes["late-fee"] != "large" || sizes[SetupTaskID] != "small" {
		t.Errorf("taskSizes %v", sizes)
	}
}

func TestATaskSizeIsSmallOrLarge(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "task.yaml"), []byte("id: x\nkind: bug\nsize: medium\nmodel: m\nmax_turns: 5\ntimeout: 1m\ncheck: true\nprompt: |\n  fix\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTask(dir); err == nil || !strings.Contains(err.Error(), `size "medium": give small or large`) {
		t.Errorf("err %v", err)
	}
}

// The thresholds live in one place, with the owner's decision (L1205).
func TestTheSizeGatesAreTheOwners(t *testing.T) {
	want := map[string]SizeGate{"small": {Cost: 1.1, Time: 1.1}, "large": {Cost: 1.25, Time: 1.25, BetterQuality: true}}
	if len(valueGates) != 2 || valueGates["small"] != want["small"] || valueGates["large"] != want["large"] {
		t.Errorf("valueGates %+v", valueGates)
	}
}

func TestValueGatesASmallTaskAtOnePointOne(t *testing.T) {
	for _, c := range []struct {
		name             string
		cost             float64
		edit             func(m *Metrics)
		code             int
		want, wantAbsent []string
	}{
		{"at the limit", 1.1, nil, 0, []string{"PASS  pagination-bug plain jhon vs clean plain (small)", "PASS  weighted tokens", "(1.1×, limit 1.1× for a small task, L1205)",
			"PASS  active time", "110 s (110-110) vs clean 100 s (100-100) (1.1×, limit 1.1× for a small task, L1205)", "verdict: PASS"}, []string{"better quality"}},
		{"over in cost", 1.15, nil, 1, []string{"FAIL  pagination-bug plain jhon vs clean plain (small)", "FAIL  weighted tokens", "(1.15×, limit 1.1× for a small task", "FAIL  active time", "verdict: FAIL"}, nil},
		{"over in time only", 1, func(m *Metrics) { m.ActiveMs = 120000 }, 1, []string{"PASS  weighted tokens", "FAIL  active time", "120 s (120-120) vs clean 100 s (100-100) (1.2×, limit 1.1×"}, nil},
		{"under while discipline is still judged", 0.9, func(m *Metrics) { m.TestsRunBeforeEnd = bp(false) }, 1, []string{"PASS  weighted tokens", "PASS  active time", "FAIL  tests run before end"}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := sizedRuns(t, "pagination-bug", "small", c.cost, c.edit, nil)
			code, out, errOut := value(t, dir)
			if code != c.code {
				t.Errorf("exit %d, want %d: %s", code, c.code, errOut)
			}
			for _, s := range c.want {
				if !strings.Contains(out, s) {
					t.Errorf("output lacks %q:\n%s", s, out)
				}
			}
			for _, s := range c.wantAbsent {
				if strings.Contains(out, s) {
					t.Errorf("output has %q:\n%s", s, out)
				}
			}
		})
	}
}

func TestValueGatesALargeTaskAtOnePointTwentyFiveWithBetterQuality(t *testing.T) {
	for _, c := range []struct {
		name            string
		cost            float64
		edit, cleanEdit func(m *Metrics)
		code            int
		want            []string
	}{
		{"at the limit, fewer rework", 1.25, nil, nil, 0, []string{"PASS  search-accents plain jhon vs clean plain (large)", "(1.25×, limit 1.25× for a large task, L1205)",
			"PASS  better quality", "both pass every run; regressions and rework per run 1 vs clean 2, need fewer"}},
		{"over the limit", 1.3, nil, nil, 1, []string{"FAIL  weighted tokens", "(1.3×, limit 1.25× for a large task", "FAIL  active time"}},
		{"the same quality", 1, nil, func(m *Metrics) { m.Rework = ip(1) }, 1, []string{"FAIL  better quality", "regressions and rework per run 1 vs clean 1, need fewer", "verdict: FAIL"}},
		{"fewer regressions", 1, func(m *Metrics) { m.Rework = ip(2) }, func(m *Metrics) { m.Regressions = ip(1) }, 0, []string{"PASS  better quality", "per run 2 vs clean 3"}},
		{"a higher pass rate", 1, nil, func(m *Metrics) { m.Success, m.Rework = m.Repeat != 1, ip(0) }, 0, []string{"PASS  better quality", "hidden tests 3/3 vs clean 2/3"}},
		{"the same pass rate short of every run", 1, func(m *Metrics) { m.Success = m.Repeat != 1 }, func(m *Metrics) { m.Success = m.Repeat != 1 },
			1, []string{"FAIL  better quality", "hidden tests 2/3 vs clean 2/3, need a higher pass rate"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := sizedRuns(t, "search-accents", "large", c.cost, c.edit, c.cleanEdit)
			code, out, _ := value(t, dir)
			if code != c.code {
				t.Errorf("exit %d, want %d:\n%s", code, c.code, out)
			}
			for _, s := range c.want {
				if !strings.Contains(out, s) {
					t.Errorf("output lacks %q:\n%s", s, out)
				}
			}
		})
	}
}

// A run with no task_size takes its task.yaml's size; a task with neither
// fails its cell.
func TestValueSizeComesFromTheRunOrTheTask(t *testing.T) {
	dir := sizedRuns(t, "late-fee", "", 1.2, nil, nil)
	if code, out, _ := value(t, dir); code != 0 || !strings.Contains(out, "late-fee plain jhon vs clean plain (large)") {
		t.Errorf("exit %d:\n%s", code, out)
	}
	dir = sizedRuns(t, "mystery", "", 1, nil, nil)
	code, out, _ := value(t, dir)
	if code != 1 || !strings.Contains(out, "FAIL  size") || !strings.Contains(out, "task mystery has no size: its task.yaml needs size: small or large (T146)") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}

func TestTheJhonPlainRunsStartSetUpForLDD(t *testing.T) {
	f := newFake(t, "0.40")
	sol, _ := filepath.Abs(filepath.Join("testdata", "solutions", "pagination-bug"))
	t.Setenv("FAKE_SOLUTION", sol)
	o, out := options(t, f, "pagination-bug", []string{"jhon", "clean"}, 1)
	o.Variants = []string{"plain", "ldd"}
	runs, err := Plan(o)
	if err != nil {
		t.Fatal(err)
	}
	steady := map[string]bool{}
	for _, r := range runs {
		steady[r.Name()] = r.Steady
	}
	if want := map[string]bool{"pagination-bug-plain-jhon-1": true, "pagination-bug-plain-clean-1": false, "pagination-bug-ldd-jhon-1": false}; !maps.Equal(steady, want) {
		t.Errorf("steady %v, want %v", steady, want)
	}
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	config, _ := os.ReadFile(filepath.Join("ldd", "steady-config.yaml"))
	jhonRepo := filepath.Join(o.Results, "runs", "pagination-bug-plain-jhon-1", "repo")
	root, err := exec.Command("git", "-C", jhonRepo, "rev-list", "--max-parents=0", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	// The set-up is in the template's commit, before the agent starts.
	committed, err := exec.Command("git", "-C", jhonRepo, "show", strings.TrimSpace(string(root))+":ldd/config.yaml").Output()
	if err != nil || !bytes.Equal(committed, config) || !strings.Contains(string(config), "strict_tdd_policy: require") {
		t.Errorf("the template commit's ldd/config.yaml: %v\n%s", err, committed)
	}
	if m := metricsOf(t, o, "pagination-bug-plain-jhon-1"); !m.SteadyState || m.TaskSize != "small" || !m.Success {
		t.Errorf("jhon plain: steady %t size %q success %t", m.SteadyState, m.TaskSize, m.Success)
	}
	if m := metricsOf(t, o, "pagination-bug-plain-clean-1"); m.SteadyState {
		t.Error("clean plain started set up")
	}
	if fileExists(filepath.Join(o.Results, "runs", "pagination-bug-plain-clean-1", "repo", "ldd")) {
		t.Error("clean's repo has ldd/")
	}
	o.DryRun, o.Results = true, filepath.Join(t.TempDir(), "dry")
	out.Reset()
	if err := Execute(o); err != nil || strings.Count(out.String(), "steady state: ldd/config.yaml (the LDD set-up) committed with the template before the agent starts") != 1 {
		t.Errorf("dry run %v:\n%s", err, out)
	}
}

func TestTheSetupRunsOncePerJhonRuntimeOnAFreshRepo(t *testing.T) {
	f := newFake(t)
	ts, err := LoadTasks("tasks", []string{"pagination-bug", SetupTaskID})
	if err != nil {
		t.Fatal(err)
	}
	if ts[1].Dir != setupTaskDir() || ts[1].Size != "small" {
		t.Errorf("the set-up task %+v", ts[1])
	}
	o, _ := options(t, f, "pagination-bug", KnownConditions, 3)
	o.Tasks = ts
	runs, err := Plan(o)
	if err != nil {
		t.Fatal(err)
	}
	var setup []string
	for _, r := range runs {
		if r.Task.ID == SetupTaskID {
			setup = append(setup, r.Name())
			if r.Steady {
				t.Errorf("%s starts set up", r.Name())
			}
		}
	}
	if !slices.Equal(setup, []string{"ldd-setup-plain-jhon-1", "ldd-setup-plain-pi-jhon-1"}) || len(runs) != 4*3+2 {
		t.Errorf("set-up runs %v of %d", setup, len(runs))
	}
	o.Smoke = true
	runs, _ = Plan(o)
	if slices.ContainsFunc(runs, func(r *Run) bool { return r.Task.ID == SetupTaskID }) {
		t.Error("the smoke tier runs the set-up")
	}
	// Its hidden check: LDD set up and the code unchanged.
	env := os.Environ()
	for _, setUp := range []bool{false, true} {
		repo := t.TempDir()
		if err := copyTree(ts[1].Template(), repo); err != nil {
			t.Fatal(err)
		}
		if setUp {
			if err := writeSteadySetup(repo); err != nil {
				t.Fatal(err)
			}
		}
		if ok, outp, err := ts[1].Accept(repo, env); err != nil || ok != setUp {
			t.Errorf("set up %t: passed %t %v\n%s", setUp, ok, err, outp)
		}
	}
}

// The set-up runs are reported on their own line: no cell, no sum, no
// certification cell.
func TestTheSetupIsReportedApart(t *testing.T) {
	dir := valueFixture(t, nil, nil)
	for _, cond := range []string{"jhon", "pi-jhon"} {
		m := cellRun("plain", cond, 1, 0.2)
		m.Task, m.TaskSize, m.CostUSD = SetupTaskID, "small", 0.05
		putRuns(t, dir, m)
	}
	code, out, _ := value(t, dir)
	for _, s := range []string{"one-time set-up jhon  (apart from every cell): 1/1 passed, weighted tokens", "one-time set-up pi-jhon", "cost usd 0.05 (0.05-0.05)",
		"runtime claude-code (jhon vs clean): PASS — 1/1 cells", "verdict: PASS"} {
		if !strings.Contains(out, s) {
			t.Errorf("value lacks %q:\n%s", s, out)
		}
	}
	if code != 0 || strings.Contains(out, "ldd-setup plain jhon vs") {
		t.Errorf("exit %d:\n%s", code, out)
	}
	var rep bytes.Buffer
	if err := Report(dir, &rep); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rep.String(), "one-time set-up jhon (apart from every cell): 1 run(s), passed 1/1") {
		t.Errorf("report:\n%s", rep.String())
	}
	var doc struct {
		Rows        []Row              `json:"rows"`
		Setup       []Row              `json:"setup"`
		ByCondition []ConditionSummary `json:"by_condition"`
	}
	b, _ := os.ReadFile(filepath.Join(dir, "report.json"))
	if err := json.Unmarshal(b, &doc); err != nil || len(doc.Setup) != 2 || slices.ContainsFunc(doc.Rows, func(r Row) bool { return r.Task == SetupTaskID }) {
		t.Errorf("report.json %v: %d set-up rows, rows %d", err, len(doc.Setup), len(doc.Rows))
	}
	for _, c := range doc.ByCondition {
		if c.Runs != 3 {
			t.Errorf("by condition %s holds %d runs", c.Condition, c.Runs)
		}
	}
	rows, err := loadSide([]string{dir}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	checks, notes := Certify(rows, rows)
	if slices.ContainsFunc(checks, func(c CertCheck) bool { return c.Task == SetupTaskID }) ||
		!slices.Contains(notes, "skipped 4 one-time set-up runs (ldd-setup): measured apart, they are not certification cells") {
		t.Errorf("certify notes %q", notes)
	}
}

func TestTokensAreWeightedPerProvider(t *testing.T) {
	for _, c := range []struct {
		model, provider string
		known           bool
	}{
		{"opus", "claude", true}, {"claude-bridge/opus", "claude", true}, {"anthropic/claude-sonnet-5-5", "claude", true},
		{"zai/glm-5.3", "glm", true}, {"zai/glm-5.3@high", "glm", true}, {"nan/glm-5.1", "glm", true}, {"GLM-4.6", "glm", true},
		{"openai/gpt-5", "claude", false},
	} {
		if w, known := tokenWeightsFor(c.model); w.Provider != c.provider || known != c.known {
			t.Errorf("%s: %s %t, want %s %t", c.model, w.Provider, known, c.provider, c.known)
		}
	}
	m := Metrics{Model: "zai/glm-5.3", InputTokens: 600, CacheWriteTokens: 100, CacheReadTokens: 6000, OutputTokens: 60}
	if got, want := costWeighted(m), 600+100+6000*0.11/0.60+60*2.20/0.60; math.Abs(got-want) > 1e-6 {
		t.Errorf("glm weighted %v, want %v", got, want)
	}
	m.Model = "opus"
	if got := costWeighted(m); got != 600+125+600+300 || got != quotaWeighted(m) {
		t.Errorf("claude weighted %v", got)
	}
}

func TestValueComparesCostUSDWhenPresentAndShowsQuotaApart(t *testing.T) {
	dir := sizedRuns(t, "invoice-slices", "large", 1, func(m *Metrics) { m.CostUSD, m.QuotaPoints = 0.6, f64(4) }, func(m *Metrics) { m.CostUSD, m.QuotaPoints = 0.5, f64(1) })
	code, out, _ := value(t, dir)
	for _, s := range []string{"PASS  cost usd", "0.6 $ (0.6-0.6) vs clean 0.5 $ (0.5-0.5) (1.2×, limit 1.25× for a large task",
		"INFO  quota points", "4 (4-4) vs clean 1 (1-1), the subscription's weekly quota, never in the cost", "verdict: PASS"} {
		if !strings.Contains(out, s) {
			t.Errorf("output lacks %q:\n%s", s, out)
		}
	}
	if code != 0 {
		t.Errorf("exit %d", code)
	}
	dir = sizedRuns(t, "invoice-slices", "large", 1, func(m *Metrics) { m.CostUSD = 0.7 }, func(m *Metrics) { m.CostUSD = 0.5 })
	if code, out, _ := value(t, dir); code != 1 || !strings.Contains(out, "FAIL  cost usd") {
		t.Errorf("exit %d:\n%s", code, out)
	}
	// With no cost_usd in the stream there is nothing to compare.
	dir = sizedRuns(t, "invoice-slices", "large", 1, nil, nil)
	if _, out, _ := value(t, dir); strings.Contains(out, "cost usd") || strings.Contains(out, "quota points") {
		t.Errorf("output:\n%s", out)
	}
}

// The weighted tokens of a GLM cell use GLM's weights, not Claude's.
func TestValueWeighsAGLMCellByItsProvider(t *testing.T) {
	dir := t.TempDir()
	for rep := 1; rep <= 3; rep++ {
		for _, cond := range []string{"pi-jhon", "pi-clean"} {
			m := cellRun("plain", cond, rep, 1)
			m.Model, m.TaskSize = "zai/glm-5.3", "large"
			if cond == "pi-clean" {
				// Cache reads in place of output tokens: Jhon is within
				// 1.25× at GLM's prices (1.14×), over it at Claude's (1.36×).
				m.CacheReadTokens, m.OutputTokens, m.Rework = 520000, 0, ip(2)
			}
			putRuns(t, dir, m)
		}
	}
	rows, err := loadSide([]string{dir}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	var j, c Row
	for k, r := range rows {
		if condOf(k[2]) == "pi-jhon" {
			j = r
		} else {
			c = r
		}
	}
	glm := providerTokenWeights[1]
	if want := glm.weigh(100, 10000, 500000, 5000); math.Abs(j.Medians["cost_weighted"]-want) > 1e-6 {
		t.Errorf("pi-jhon cost_weighted %v, want %v", j.Medians["cost_weighted"], want)
	}
	if r := j.Medians["cost_weighted"] / c.Medians["cost_weighted"]; r > 1.25 {
		t.Fatalf("fixture: GLM ratio %v", r)
	}
	if r := j.Medians["quota_weighted"] / c.Medians["quota_weighted"]; r <= 1.25 {
		t.Fatalf("fixture: Claude ratio %v", r)
	}
	if code, out, _ := value(t, dir); code != 0 || !strings.Contains(out, "PASS  weighted tokens") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}

// The jhon condition is as isolated as clean (T146 FB4): no CLAUDE.md
// (the owner's ~/.claude/CLAUDE.md is an ancestor's of the run's repo) and no
// auto-memory, on the bench HOME, with the owner's Claude config variables
// stripped.
func TestTheJhonConditionLoadsNoUserFiles(t *testing.T) {
	f := newFake(t, "0.40")
	t.Setenv("CLAUDE_CONFIG_DIR", "/owner/.claude")
	t.Setenv("CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD", "1")
	o, out := options(t, f, "pagination-bug", []string{"jhon", "clean"}, 1)
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	calls := strings.Join(f.calls(t), "\n")
	jhonRepo, _ := filepath.EvalSymlinks(filepath.Join(o.Results, "runs", "pagination-bug-plain-jhon-1", "repo"))
	cleanRepo, _ := filepath.EvalSymlinks(filepath.Join(o.Results, "runs", "pagination-bug-plain-clean-1", "repo"))
	for _, want := range []string{
		"claude env |HOME=" + f.home + "|CLAUDE_CODE_DISABLE_CLAUDE_MDS=1|CLAUDE_CODE_DISABLE_AUTO_MEMORY=1|CLAUDE_CONFIG_DIR=|CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=|PWD=" + jhonRepo,
		// clean keeps --safe-mode, which drops them itself.
		"claude env |HOME=" + f.home + "|CLAUDE_CODE_DISABLE_CLAUDE_MDS=|CLAUDE_CODE_DISABLE_AUTO_MEMORY=|CLAUDE_CONFIG_DIR=|CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=|PWD=" + cleanRepo,
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("no call carries %q:\n%s", want, calls)
		}
	}
	if !strings.Contains(calls, "-p ") || !strings.Contains(calls, "--safe-mode") {
		t.Errorf("calls:\n%s", calls)
	}
	o.DryRun, o.Results = true, filepath.Join(t.TempDir(), "dry")
	out.Reset()
	if err := Execute(o); err != nil || !strings.Contains(out.String(), "env: HOME="+f.home+" XDG_CACHE_HOME="+filepath.Join(o.Results, "runs", "pagination-bug-plain-jhon-1", "cache")) ||
		!strings.Contains(out.String(), "CLAUDE_CODE_DISABLE_CLAUDE_MDS=1 CLAUDE_CODE_DISABLE_AUTO_MEMORY=1") {
		t.Errorf("dry run %v:\n%s", err, out)
	}
}

func TestRepeatsDefaultBySize(t *testing.T) {
	f := newFake(t)
	ts, err := LoadTasks("tasks", []string{"pagination-bug", "late-fee", SetupTaskID})
	if err != nil {
		t.Fatal(err)
	}
	count := func(repeats int) map[string]int {
		o, _ := options(t, f, "pagination-bug", []string{"jhon", "clean"}, repeats)
		o.Tasks = ts
		runs, err := Plan(o)
		if err != nil {
			t.Fatal(err)
		}
		n := map[string]int{}
		for _, r := range runs {
			n[r.Task.ID+" "+r.Cond.Name()]++
		}
		return n
	}
	want := map[string]int{"pagination-bug jhon": 5, "pagination-bug clean": 5, "late-fee jhon": 3, "late-fee clean": 3, "ldd-setup jhon": 1}
	if got := count(0); len(got) != len(want) || got["pagination-bug jhon"] != 5 || got["late-fee clean"] != 3 || got["ldd-setup jhon"] != 1 {
		t.Errorf("default repeats %v, want %v", got, want)
	}
	if got := count(2); got["pagination-bug jhon"] != 2 || got["late-fee jhon"] != 2 || got["ldd-setup jhon"] != 1 {
		t.Errorf("--repeats 2: %v", got)
	}
	var out, errOut bytes.Buffer
	if code := cli([]string{"run", "--tasks", "pagination-bug,late-fee", "--conditions", "clean", "--dry-run"}, &out, &errOut, time.Now()); code != 0 ||
		!strings.Contains(out.String(), "plan: 8 runs") || !strings.Contains(out.String(), "pagination-bug-plain-clean-5") || strings.Contains(out.String(), "late-fee-plain-clean-4") {
		t.Errorf("exit %d %s", code, errOut.String())
	}
}

func TestTheReportShowsMedianAndSpread(t *testing.T) {
	dir := t.TempDir()
	for rep, turns := range []int{10, 30, 20} {
		m := cellRun("plain", "clean", rep+1, 1)
		m.Turns, m.CostUSD = turns, 0
		putRuns(t, dir, m)
	}
	var out bytes.Buffer
	if err := Report(dir, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "median (min-max)") || !strings.Contains(out.String(), "20 (10-30)") {
		t.Errorf("report:\n%s", out.String())
	}
	var doc struct{ Rows []Row }
	b, _ := os.ReadFile(filepath.Join(dir, "report.json"))
	if err := json.Unmarshal(b, &doc); err != nil || len(doc.Rows) != 1 || doc.Rows[0].Spread["turns"] != [2]float64{10, 30} || doc.Rows[0].Medians["turns"] != 20 {
		t.Fatalf("report.json %v %s", err, b)
	}
	if _, ok := doc.Rows[0].Medians["cost_usd"]; ok {
		t.Error("a cost_usd median with no cost in the stream")
	}
}
