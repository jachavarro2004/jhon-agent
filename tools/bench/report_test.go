package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMetrics(t *testing.T, dir string, ms ...Metrics) {
	t.Helper()
	for _, m := range ms {
		d := filepath.Join(dir, "runs", m.Task+"-"+m.Condition+"-"+string(rune('0'+m.Repeat)))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := writeJSON(filepath.Join(d, "metrics.json"), m); err != nil {
			t.Fatal(err)
		}
	}
}

func f64(v float64) *float64 { return &v }

func TestTheReportIsTheMediansPerTaskAndCondition(t *testing.T) {
	dir := t.TempDir()
	writeMetrics(t, dir,
		Metrics{Task: "pagination-bug", Condition: "jhon", Repeat: 1, Success: true, Turns: 10, OutputTokens: 100, InputTokens: 5, CacheReadTokens: 1000, CacheWriteTokens: 50, ActiveMs: 1000, WallMs: 4000, CacheHit: f64(0.5), AgentsLaunched: 2, VerifyRuns: 1, Findings: map[string]int{"a": 1}, QuotaPoints: nil},
		Metrics{Task: "pagination-bug", Condition: "jhon", Repeat: 2, Success: false, Turns: 30, OutputTokens: 300, InputTokens: 7, CacheReadTokens: 3000, CacheWriteTokens: 70, ActiveMs: 3000, WallMs: 8000, CacheHit: f64(0.7), AgentsLaunched: 4, VerifyRuns: 3, Findings: map[string]int{"a": 2, "b": 1}, QuotaPoints: f64(2)},
		Metrics{Task: "pagination-bug", Condition: "jhon", Repeat: 3, Success: true, Turns: 20, OutputTokens: 200, InputTokens: 6, CacheReadTokens: 2000, CacheWriteTokens: 60, ActiveMs: 2000, WallMs: 6000, CacheHit: nil, AgentsLaunched: 3, VerifyRuns: 2, QuotaPoints: f64(1)},
		Metrics{Task: "pagination-bug", Condition: "clean", Repeat: 1, Success: true, Turns: 8, OutputTokens: 80, ActiveMs: 500, WallMs: 900, QuotaPoints: f64(1)},
		Metrics{Task: "pagination-bug", Condition: "clean", Repeat: 2, Success: true, Turns: 12, OutputTokens: 120, ActiveMs: 700, WallMs: 1100, QuotaPoints: f64(3)},
	)
	var out bytes.Buffer
	if err := Report(dir, &out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep struct{ Rows []Row }
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	rows := rep.Rows
	if len(rows) != 2 || rows[0].Condition != "jhon" || rows[1].Condition != "clean" || rows[1].Variant != "plain" {
		t.Fatalf("rows %+v", rows)
	}
	jhon, clean := rows[0], rows[1]
	if jhon.Runs != 3 || jhon.Passed != 2 || jhon.Medians["turns"] != 20 || jhon.Medians["output_tokens"] != 200 || jhon.Medians["cache_read_tokens"] != 2000 ||
		jhon.Medians["cache_hit"] != 0.6 || jhon.Medians["agents_launched"] != 3 || jhon.Medians["verify_runs"] != 2 || jhon.Medians["findings"] != 1 ||
		jhon.Medians["quota_points"] != 1.5 || jhon.Medians["wall_ms"] != 6000 || jhon.Medians["active_ms"] != 2000 {
		t.Errorf("jhon %+v", jhon)
	}
	if clean.Runs != 2 || clean.Passed != 2 || clean.Medians["turns"] != 10 || clean.Medians["output_tokens"] != 100 || clean.Medians["quota_points"] != 2 {
		t.Errorf("clean %+v", clean)
	}
	table := out.String()
	for _, s := range []string{"task", "condition", "passed", "pagination-bug", "2/3", "2/2", "20", "1.5"} {
		if !strings.Contains(table, s) {
			t.Errorf("table lacks %q:\n%s", s, table)
		}
	}
}

func TestAReportOfNoRunsSaysSo(t *testing.T) {
	err := Report(t.TempDir(), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "no runs") {
		t.Fatalf("err %v", err)
	}
}

func bp(v bool) *bool { return &v }

func TestTheReportShowsTheProcessRatesAndOldRunsCountAsUnknown(t *testing.T) {
	dir := t.TempDir()
	writeMetrics(t, dir,
		Metrics{Task: "late-fee", Condition: "jhon", Repeat: 1, Success: true, TestChanged: bp(true), TestsRunBeforeEnd: bp(true), RedBeforeGreen: bp(true),
			RedBeforeGreenSource: "ldd", OutOfScopeFiles: ip(0), Findings: map[string]int{"tdd-order": 1}},
		Metrics{Task: "late-fee", Condition: "jhon", Repeat: 2, Success: false, TestChanged: bp(true), TestsRunBeforeEnd: bp(false), RedBeforeGreen: bp(false),
			RedBeforeGreenSource: "transcript", OutOfScopeFiles: ip(3), Findings: map[string]int{"tdd-order": 1, "scope": 2}},
		Metrics{Task: "late-fee", Condition: "jhon", Repeat: 3, Success: true, TestChanged: bp(false), OutOfScopeFiles: ip(1)},
		// Recorded before T41: every process fact is unknown, none is false.
		Metrics{Task: "late-fee", Condition: "clean", Repeat: 1, Success: true},
	)
	var out bytes.Buffer
	if err := Report(dir, &out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep struct{ Rows []Row }
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	jhon, clean := rep.Rows[0].Process, rep.Rows[1].Process
	if jhon.TestChanged != (Rate{Yes: 2, Known: 3}) || jhon.TestsRunBeforeEnd != (Rate{Yes: 1, Known: 2}) || jhon.RedBeforeGreen != (Rate{Yes: 1, Known: 2}) ||
		jhon.OutOfScopeFiles == nil || *jhon.OutOfScopeFiles != 1 || jhon.FindingsByCheck["tdd-order"] != 2 || jhon.FindingsByCheck["scope"] != 2 {
		t.Errorf("jhon process %+v", jhon)
	}
	if clean.TestChanged != (Rate{}) || clean.RedBeforeGreen != (Rate{}) || clean.OutOfScopeFiles != nil {
		t.Errorf("an old run counted as known: %+v", clean)
	}
	table := out.String()
	for _, s := range []string{"test changed", "tests run before end", "red before green", "out of scope", "2/3", "1/2", "—", "scope:2 tdd-order:2"} {
		if !strings.Contains(table, s) {
			t.Errorf("table lacks %q:\n%s", s, table)
		}
	}
}

// The dialogs the pi driver declined are summed in the fidelity section (T47).
func TestTheReportShowsTheDeclinedDialogs(t *testing.T) {
	dir := t.TempDir()
	writeMetrics(t, dir,
		Metrics{Task: "late-fee", Condition: "pi-jhon", Repeat: 1, UIRequestsDeclined: 2},
		Metrics{Task: "late-fee", Condition: "pi-jhon", Repeat: 2, UIRequestsDeclined: 1})
	var out bytes.Buffer
	if err := Report(dir, &out); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "report.json"))
	var rep struct{ Rows []Row }
	if err := json.Unmarshal(b, &rep); err != nil || len(rep.Rows) != 1 || rep.Rows[0].Process.UIRequestsDeclined != 3 {
		t.Fatalf("rows %+v (%v)", rep.Rows, err)
	}
	_, fidelity, _ := strings.Cut(out.String(), "methodology fidelity")
	fidelity, _, _ = strings.Cut(fidelity, "by condition")
	if !strings.Contains(fidelity, "dialogs declined") || !strings.Contains(fidelity, "\t3\n") && !strings.Contains(fidelity, " 3\n") {
		t.Errorf("fidelity section lacks the declined dialogs:\n%s", fidelity)
	}
}
