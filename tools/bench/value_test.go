package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// valueFixture is one results dir with 3 repeats of invoice-slices plain in
// every condition: the Jhon ones at cost 1, every quality fact good; the
// clean ones at cost 0.5, their first run with no RED before GREEN and no
// tests before the end. edit changes the Jhon runs, cleanEdit the clean ones.
func valueFixture(t *testing.T, edit, cleanEdit func(m *Metrics)) string {
	t.Helper()
	dir := t.TempDir()
	for rep := 1; rep <= 3; rep++ {
		for _, cond := range KnownConditions {
			if isClean(cond) {
				m := cellRun("plain", cond, rep, 0.5)
				if rep == 1 {
					m.RedBeforeGreen, m.TestsRunBeforeEnd = bp(false), bp(false)
				}
				if cleanEdit != nil {
					cleanEdit(&m)
				}
				putRuns(t, dir, m)
				continue
			}
			m := cellRun("plain", cond, rep, 1)
			if edit != nil {
				edit(&m)
			}
			putRuns(t, dir, m)
		}
	}
	return dir
}

// value runs `bench value` with args, its exit code and output.
func value(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := valueCLI(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestValuePassesAJhonCellBetterOnDiscipline(t *testing.T) {
	dir := valueFixture(t, nil, nil)
	code, out, errOut := value(t, dir)
	if code != 0 {
		t.Fatalf("exit %d\n%s%s", code, out, errOut)
	}
	for _, s := range []string{
		"PASS  invoice-slices plain jhon vs clean plain",
		"PASS  invoice-slices plain pi-jhon vs pi-clean plain",
		"PASS  hidden tests", "3/3 vs clean 3/3",
		"PASS  red before green", "3/3 vs clean 2/3",
		"PASS  tests run before end",
		"PASS  out of scope", "median 0 vs clean 0",
		"COST  quota-weighted tokens", "(2×)", "COST  turns", "20 vs clean 10 (2×)", "COST  active time", "100 s vs clean 50 s (2×)",
		"runtime claude-code (jhon vs clean): PASS — 1/1 cells",
		"runtime pi (pi-jhon vs pi-clean): PASS — 1/1 cells",
		"verdict: PASS", "value.json: " + filepath.Join(dir, "value.json"),
	} {
		if !strings.Contains(out, s) {
			t.Errorf("output lacks %q:\n%s", s, out)
		}
	}
	var v Value
	b, err := os.ReadFile(filepath.Join(dir, "value.json"))
	if err != nil || json.Unmarshal(b, &v) != nil || v.Verdict != "PASS" || len(v.Cells) != 2 || len(v.Runtimes) != 2 {
		t.Fatalf("value.json: %v %s", err, b)
	}
	code, js, _ := value(t, "--json", dir)
	var w Value
	if code != 0 || json.Unmarshal([]byte(js), &w) != nil || w.Verdict != "PASS" || w.Cells[0].Against != "clean" {
		t.Errorf("--json exit %d: %s", code, js)
	}
}

func TestValueFailsEachRuleItBreaks(t *testing.T) {
	for _, c := range []struct {
		name, want string
		edit       func(m *Metrics)
		cleanEdit  func(m *Metrics)
	}{
		{"worse pass rate", "FAIL  hidden tests", func(m *Metrics) { m.Success = m.Repeat != 2 }, nil},
		{"more out of scope", "FAIL  out of scope", func(m *Metrics) { m.OutOfScopeFiles = ip(1) }, nil},
		{"equal discipline short of all", "FAIL  red before green", func(m *Metrics) {
			if m.Repeat == 1 {
				m.RedBeforeGreen = bp(false)
			}
		}, nil},
		{"tests before end worse", "FAIL  tests run before end", func(m *Metrics) { m.TestsRunBeforeEnd = bp(false) }, nil},
		{"unknown discipline", "FAIL  red before green", func(m *Metrics) { m.RedBeforeGreen = nil }, nil},
		{"unknown out of scope", "FAIL  out of scope", func(m *Metrics) { m.OutOfScopeFiles = nil }, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := valueFixture(t, c.edit, c.cleanEdit)
			code, out, _ := value(t, dir)
			if code != 1 || !strings.Contains(out, c.want) || !strings.Contains(out, "FAIL  invoice-slices plain jhon vs clean plain") ||
				!strings.Contains(out, "runtime claude-code (jhon vs clean): FAIL — 0/1 cells") || !strings.Contains(out, "verdict: FAIL") {
				t.Errorf("exit %d, want %q:\n%s", code, c.want, out)
			}
		})
	}
}

func TestValueBothAtEveryRunHoldDiscipline(t *testing.T) {
	dir := valueFixture(t, nil, func(m *Metrics) { m.RedBeforeGreen, m.TestsRunBeforeEnd = bp(true), bp(true) })
	if code, out, _ := value(t, dir); code != 0 || !strings.Contains(out, "PASS  red before green") || !strings.Contains(out, "3/3 vs clean 3/3, both every run") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}

func TestValueCostIsShownNeverFailed(t *testing.T) {
	dir := valueFixture(t, func(m *Metrics) { m.Turns, m.ActiveMs, m.OutputTokens = 200, 1000000, 500000 }, nil)
	code, out, _ := value(t, dir)
	if code != 0 || !strings.Contains(out, "200 vs clean 10 (20×)") || !strings.Contains(out, "1000 s vs clean 50 s (20×)") || !strings.Contains(out, "verdict: PASS") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}

func TestValueFailsAMissingCleanCounterpart(t *testing.T) {
	dir := valueFixture(t, nil, nil)
	for rep := '1'; rep <= '3'; rep++ {
		if err := os.RemoveAll(filepath.Join(dir, "runs", "invoice-slices-plain-pi-clean-"+string(rep))); err != nil {
			t.Fatal(err)
		}
	}
	code, out, _ := value(t, dir)
	for _, s := range []string{"FAIL  invoice-slices plain pi-jhon vs pi-clean plain", "no pi-clean run of invoice-slices plain: run --conditions pi-clean",
		"runtime pi (pi-jhon vs pi-clean): FAIL — 0/1 cells", "runtime claude-code (jhon vs clean): PASS — 1/1 cells", "verdict: FAIL"} {
		if !strings.Contains(out, s) {
			t.Errorf("output lacks %q:\n%s", s, out)
		}
	}
	if code != 1 {
		t.Errorf("exit %d", code)
	}
}

func TestValueFailsFewerThanThreeRuns(t *testing.T) {
	dir := valueFixture(t, nil, nil)
	if err := os.RemoveAll(filepath.Join(dir, "runs", "invoice-slices-plain-clean-3")); err != nil {
		t.Fatal(err)
	}
	code, out, _ := value(t, dir)
	if code != 1 || !strings.Contains(out, "FAIL  repeats") || !strings.Contains(out, "not enough runs: jhon 3, clean 2, need 3 each") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}

func TestValueComparesAnLddCellWithThePlainCleanOne(t *testing.T) {
	dir := valueFixture(t, nil, nil)
	for rep := 1; rep <= 3; rep++ {
		putRuns(t, dir, cellRun("ldd", "jhon", rep, 1))
	}
	code, out, _ := value(t, dir)
	if code != 0 || !strings.Contains(out, "PASS  invoice-slices ldd jhon vs clean plain") || !strings.Contains(out, "runtime claude-code (jhon vs clean): PASS — 2/2 cells") ||
		!strings.Contains(out, "note: the no-Jhon conditions run the plain variant only") {
		t.Errorf("exit %d:\n%s", code, out)
	}
}

func TestValueNeedsAResultsDir(t *testing.T) {
	if code, _, errOut := value(t); code != 2 || !strings.Contains(errOut, "bench value: name the results dirs") {
		t.Errorf("exit %d: %s", code, errOut)
	}
	var out, errOut bytes.Buffer
	if code := cli([]string{"value"}, &out, &errOut, time.Now()); code != 2 {
		t.Errorf("cli value exit %d", code)
	}
}
