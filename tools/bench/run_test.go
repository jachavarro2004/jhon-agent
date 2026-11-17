package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// options are a run of task over conds against the fake f, results under a scratch dir.
func options(t *testing.T, f fake, task string, conds []string, repeats int) (*Options, *bytes.Buffer) {
	t.Helper()
	tasks, err := LoadTasks("tasks", []string{task})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	return &Options{
		Tasks: tasks, Variants: []string{"plain"}, Conditions: conds, Repeats: repeats, Model: "haiku", BudgetPoints: 10,
		Home: f.home, Results: filepath.Join(t.TempDir(), "results"), AllowedTools: DefaultAllowedTools,
		Out: &out, NewID: ids(),
	}, &out
}

func TestThePlanIsTaskByConditionByRepeat(t *testing.T) {
	f := newFake(t)
	o, _ := options(t, f, "pagination-bug", AllConditions, 2)
	runs, err := Plan(o)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range runs {
		names = append(names, r.Name())
	}
	want := []string{"pagination-bug-plain-jhon-1", "pagination-bug-plain-clean-1", "pagination-bug-plain-pi-jhon-1", "pagination-bug-plain-jhon-2", "pagination-bug-plain-clean-2", "pagination-bug-plain-pi-jhon-2"}
	if !slices.Equal(names, want) {
		t.Fatalf("plan %v, want %v", names, want)
	}
	prompt := runs[0].Task.Prompt
	jhon, clean, pi := runs[0].Argv, runs[1].Argv, runs[2].Argv
	wantJhon := []string{filepath.Join(f.home, ".local", "bin", "jhon"), "start", "-p", prompt, "--mcp-config", engramMCPConfig(f.home), "--output-format", "stream-json", "--verbose",
		"--max-turns", "60", "--model", "haiku", "--permission-mode", "acceptEdits", "--allowedTools", DefaultAllowedTools, "--session-id", runs[0].SessionID}
	if !slices.Equal(jhon, wantJhon) {
		t.Errorf("jhon argv\n%q\nwant\n%q", jhon, wantJhon)
	}
	wantClean := []string{"claude", "-p", prompt, "--safe-mode", "--setting-sources", "project,local", "--strict-mcp-config", "--output-format", "stream-json", "--verbose",
		"--max-turns", "60", "--model", "haiku", "--permission-mode", "acceptEdits", "--allowedTools", DefaultAllowedTools, "--session-id", runs[1].SessionID}
	if !slices.Equal(clean[1:], wantClean[1:]) || filepath.Base(clean[0]) != "claude" {
		t.Errorf("clean argv\n%q\nwant\n%q", clean, wantClean)
	}
	wantPi := []string{filepath.Join(f.home, ".local", "bin", "jhon"), "start", "pi", "--mode", "json", "--provider", "claude-bridge", "--model", "haiku",
		"--session-id", runs[2].SessionID, "--", prompt}
	if !slices.Equal(pi, wantPi) {
		t.Errorf("pi argv\n%q\nwant\n%q", pi, wantPi)
	}
	if runs[0].Dir != filepath.Join(o.Results, "runs", "pagination-bug-plain-jhon-1") || runs[0].Repo != filepath.Join(runs[0].Dir, "repo") || runs[0].Cache != filepath.Join(runs[0].Dir, "cache") {
		t.Errorf("dirs %s %s %s", runs[0].Dir, runs[0].Repo, runs[0].Cache)
	}
	if runs[0].Timeout != 30*time.Minute || runs[0].SessionID == runs[1].SessionID {
		t.Errorf("timeout %s ids %s %s", runs[0].Timeout, runs[0].SessionID, runs[1].SessionID)
	}
}

func TestThePlanRefusesAnUnknownConditionAndNoRepeats(t *testing.T) {
	f := newFake(t)
	o, _ := options(t, f, "pagination-bug", []string{"other"}, 1)
	if _, err := Plan(o); err == nil || !strings.Contains(err.Error(), `unknown condition "other"`) {
		t.Fatalf("err %v", err)
	}
	o, _ = options(t, f, "pagination-bug", AllConditions, 0)
	if _, err := Plan(o); err == nil {
		t.Fatal("repeats 0 accepted")
	}
}

func TestDryRunPrintsThePlanAndStartsNothing(t *testing.T) {
	f := newFake(t)
	o, out := options(t, f, "pagination-bug", AllConditions, 2)
	o.DryRun = true
	if err := Execute(o); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"6 runs", "pagination-bug-plain-pi-jhon-2", "--safe-mode", "start pi --mode json", "HOME=" + f.home,
		"XDG_CACHE_HOME=" + filepath.Join(o.Results, "runs", "pagination-bug-plain-clean-1", "cache"), "budget: 10 points"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("dry run lacks %q:\n%s", s, out)
		}
	}
	if c := f.calls(t); c != nil {
		t.Errorf("dry run started %v", c)
	}
	if _, err := os.Stat(o.Results); !os.IsNotExist(err) {
		t.Errorf("dry run created %s", o.Results)
	}
}

func metricsOf(t *testing.T, o *Options, name string) Metrics {
	t.Helper()
	var m Metrics
	b, err := os.ReadFile(filepath.Join(o.Results, "runs", name, "metrics.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestOneFakeRunPerConditionIsJudgedAuditedAndMeasured(t *testing.T) {
	f := newFake(t, "0.40", "0.42")
	sol, _ := filepath.Abs(filepath.Join("testdata", "solutions", "pagination-bug"))
	t.Setenv("FAKE_SOLUTION", sol)
	o, out := options(t, f, "pagination-bug", AllConditions, 1)
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	calls := f.calls(t)
	for i, name := range []string{"pagination-bug-plain-jhon-1", "pagination-bug-plain-clean-1", "pagination-bug-plain-pi-jhon-1"} {
		m := metricsOf(t, o, name)
		dir := filepath.Join(o.Results, "runs", name)
		if !m.Success || m.Exit != 0 || m.Variant != "plain" || m.Condition != AllConditions[i] || m.Task != "pagination-bug" {
			t.Errorf("%s: %+v", name, m)
		}
		if !strings.HasSuffix(m.Transcript, m.SessionID+".jsonl") || !strings.HasPrefix(m.Transcript, f.home) {
			t.Errorf("%s: transcript %s", name, m.Transcript)
		}
		if m.InputTokens != 42 || m.CacheWriteTokens != 7000 || m.CacheReadTokens != 35000 || m.ActiveMs != 90000 || m.ElapsedMs != 120000 ||
			m.CacheHit == nil || *m.CacheHit != 0.83 || m.AgentsLaunched != 1 || m.VerifyRuns != 2 || m.Findings["tdd-order"] != 2 || m.Findings["scope"] != 1 {
			t.Errorf("%s: audit metrics %+v", name, m)
		}
		audit := "jhon audit " + m.Transcript + " --json |HOME=" + f.home + "|XDG_CACHE_HOME=" + filepath.Join(dir, "cache")
		if !slices.Contains(calls, audit) {
			t.Errorf("%s: no call %q in\n%s", name, audit, strings.Join(calls, "\n"))
		}
		for _, file := range []string{"stream.jsonl", "stderr.txt", "audit.json", "check.txt"} {
			if _, err := os.Stat(filepath.Join(dir, file)); err != nil {
				t.Errorf("%s: %v", name, err)
			}
		}
	}
	jhon, clean, pi := metricsOf(t, o, "pagination-bug-plain-jhon-1"), metricsOf(t, o, "pagination-bug-plain-clean-1"), metricsOf(t, o, "pagination-bug-plain-pi-jhon-1")
	if jhon.Turns != 7 || jhon.OutputTokens != 900 || *jhon.Utilization != 0.40 || jhon.QuotaPoints != nil {
		t.Errorf("jhon stream metrics %+v", jhon)
	}
	if clean.QuotaPoints == nil || *clean.QuotaPoints != 2 {
		t.Errorf("clean quota points %v", clean.QuotaPoints)
	}
	if pi.Turns != 3 || pi.OutputTokens != 300 || pi.Utilization != nil || pi.QuotaPoints != nil {
		t.Errorf("pi stream metrics %+v", pi)
	}
	repo := filepath.Join(o.Results, "runs", "pagination-bug-plain-clean-1", "repo")
	for _, want := range []string{"|HOME=" + f.home + "|", "|XDG_CACHE_HOME=" + filepath.Join(o.Results, "runs", "pagination-bug-plain-clean-1", "cache") + "|", "|PWD=" + repo, "--safe-mode"} {
		if !strings.Contains(strings.Join(calls, "\n"), want) {
			t.Errorf("no call carries %q:\n%s", want, strings.Join(calls, "\n"))
		}
	}
	if !strings.Contains(strings.Join(calls, "\n"), "PI_CODING_AGENT_DIR="+filepath.Join(f.home, ".pi", "jhon")) {
		t.Errorf("pi ran without the bench HOME's agent dir")
	}
}

func TestAnUnsolvedRunFailsItsHiddenTest(t *testing.T) {
	f := newFake(t, "0.40")
	o, out := options(t, f, "pagination-bug", []string{"clean"}, 1)
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	m := metricsOf(t, o, "pagination-bug-plain-clean-1")
	if m.Success {
		t.Fatalf("the untouched template passed: %+v", m)
	}
	if b, _ := os.ReadFile(filepath.Join(o.Results, "runs", "pagination-bug-plain-clean-1", "check.txt")); !strings.Contains(string(b), "FAILED") {
		t.Errorf("check.txt %s", b)
	}
}

func TestTheBudgetStopsBeforeTheNextRun(t *testing.T) {
	f := newFake(t, "0.40", "0.45", "0.52", "0.60")
	o, out := options(t, f, "pagination-bug", []string{"clean"}, 4)
	err := Execute(o)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range f.calls(t) {
		if strings.HasPrefix(c, "claude -p") {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("%d runs, want 3:\n%s", n, out)
	}
	want := "budget: weekly utilization rose 12 points since the first run (40% to 52%), over the 10-point limit; stopping before run 4 of 4"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("output lacks %q:\n%s", want, out)
	}
}

func TestPiRunsPastTheTurnCapAreStopped(t *testing.T) {
	f := newFake(t)
	t.Setenv("FAKE_PI_TURNS", "61")
	o, out := options(t, f, "pagination-bug", []string{"pi-jhon"}, 1)
	start := time.Now()
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	m := metricsOf(t, o, "pagination-bug-plain-pi-jhon-1")
	if !m.TurnCapped || m.Turns != 61 || time.Since(start) > 20*time.Second {
		t.Fatalf("turn cap: %+v after %s", m, time.Since(start))
	}
}
