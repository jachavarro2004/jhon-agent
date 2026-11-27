package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const baseJSON = `{
  "label": "base", "created": "2026-11-20T10:00:00Z", "jhon": {"version": "0.4.0", "commit": "aaa"},
  "speed": {"runs": 5, "projects": [
    {"index": 1, "objects_before": {"project": 10, "common": 10}, "objects_after": {"project": 10, "common": 10}, "objects_grew": false,
     "commands": {
       "status": {"runs": 5, "median_ms": 1000, "max_ms": 1100},
       "risk": {"runs": 5, "median_ms": 400, "max_ms": 450},
       "hook-git-status": {"runs": 5, "median_ms": 300, "max_ms": 320},
       "hook-edit": {"runs": 5, "median_ms": 200, "max_ms": 210},
       "hook-gh-pr-create": {"runs": 5, "median_ms": 900, "max_ms": 950},
       "sandbox-run": {"runs": 5, "median_ms": 50, "max_ms": 60}}}]},
  "friction": {"since": "2026-11-01", "sessions": 10, "tool_results": 1000, "stops": 5, "per_100": 0.5,
    "per_runtime": {"pi": {"sessions": 4, "tool_results": 400, "stops": 2, "per_100": 0.5}, "claude-code": {"sessions": 6, "tool_results": 600, "stops": 3, "per_100": 0.5}},
    "per_class": {"credentials match": {"stops": 3, "per_100": 0.3}, "review due": {"stops": 2, "per_100": 0.2}}},
  "budgets": {"lean text pi": {"value": 5000, "limit": 5120}, "fixed context claude-code": {"value": 20000, "limit": 23015}},
  "hygiene": {"suite_tmp_leftovers": 0},
  "results": {"go-suite": "pass", "doctor": "pass"}
}`

const newJSON = `{
  "label": "new", "created": "2026-11-27T10:00:00Z", "jhon": {"version": "0.5.0", "commit": "bbb"},
  "speed": {"runs": 5, "projects": [
    {"index": 1, "objects_before": {"project": 10, "common": 10}, "objects_after": {"project": 11, "common": 10}, "objects_grew": true,
     "commands": {
       "status": {"runs": 5, "median_ms": 1300, "max_ms": 1400},
       "risk": {"runs": 5, "median_ms": 480, "max_ms": 500},
       "hook-git-status": {"runs": 5, "median_ms": 360, "max_ms": 400},
       "hook-edit": {"runs": 5, "median_ms": 280, "max_ms": 290},
       "hook-gh-pr-create": {"runs": 5, "median_ms": 1150, "max_ms": 1200}},
     "skipped": {"sandbox-run": "the sandbox cannot run here"}},
    {"index": 2, "objects_before": {"project": 0, "common": 0}, "objects_after": {"project": 0, "common": 0}, "objects_grew": false,
     "commands": {
       "status": {"runs": 5, "median_ms": 3500, "max_ms": 3600},
       "risk": {"runs": 5, "median_ms": 200, "max_ms": 210},
       "hook-git-status": {"runs": 5, "median_ms": 1200, "max_ms": 1300}}}]},
  "friction": {"since": "2026-11-01", "sessions": 12, "tool_results": 2000, "stops": 11, "per_100": 0.56,
    "per_runtime": {"pi": {"sessions": 5, "tool_results": 800, "stops": 6, "per_100": 0.75}, "claude-code": {"sessions": 7, "tool_results": 1200, "stops": 5, "per_100": 0.417}},
    "per_class": {"credentials match": {"stops": 9, "per_100": 0.45}, "review due": {"stops": 2, "per_100": 0.1}}},
  "budgets": {"lean text pi": {"value": 5130, "limit": 5120}, "fixed context claude-code": {"value": 20100, "limit": 23015}},
  "hygiene": {"suite_tmp_leftovers": 2},
  "results": {"go-suite": "fail", "doctor": "pass", "ci-main": "pass"}
}`

func writeRecords(t *testing.T, records map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for label, body := range records {
		if err := os.WriteFile(filepath.Join(dir, label+".json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// tableRow is the text table's row for measure, its cells split on the
// runs of two or more spaces that separate the columns.
func tableRow(t *testing.T, out, measure string) []string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		cells := splitCells(l)
		if len(cells) > 0 && cells[0] == measure {
			return cells
		}
	}
	t.Errorf("no row %q in:\n%s", measure, out)
	return nil
}

func splitCells(l string) []string {
	var cells []string
	for _, c := range strings.Split(l, "  ") {
		if c = strings.TrimSpace(c); c != "" {
			cells = append(cells, c)
		}
	}
	return cells
}

func TestCompareAppliesItsTolerances(t *testing.T) {
	dir := writeRecords(t, map[string]string{"base": baseJSON, "new": newJSON})
	var out, errs bytes.Buffer
	code := run([]string{"compare", "--base", "base", "--new", "new", "--out", dir}, &out, &errs)
	if code != 1 {
		t.Fatalf("exit %d, want 1; stderr %s\n%s", code, errs.String(), out.String())
	}
	header := splitCells(strings.SplitN(out.String(), "\n", 2)[0])
	if strings.Join(header, "|") != "measure|base|new|change|verdict" {
		t.Errorf("header %q", header)
	}
	for _, c := range []struct{ measure, base, new, change, verdict string }{
		// +30% and +300 ms: worse.
		{"p1 status median ms", "1000 (max 1100)", "1300 (max 1400)", "+300 ms (+30%)", "worse"},
		// +20% and +80 ms: within both tolerances.
		{"p1 risk median ms", "400 (max 450)", "480 (max 500)", "+80 ms (+20%)", "ok"},
		{"p1 hook-git-status median ms", "300 (max 320)", "360 (max 400)", "+60 ms (+20%)", "ok"},
		// +40% but only +80 ms: not worse.
		{"p1 hook-edit median ms", "200 (max 210)", "280 (max 290)", "+80 ms (+40%)", "ok"},
		// +28% and +250 ms, and over the hook ceiling.
		{"p1 hook-gh-pr-create median ms", "900 (max 950)", "1150 (max 1200)", "+250 ms (+28%)", "worse: over 1000 ms"},
		{"p1 sandbox-run median ms", "50 (max 60)", "skipped", "", "skipped: the sandbox cannot run here"},
		{"p1 objects", "kept", "grew", "", "worse: objects grew"},
		// No base: only the ceilings judge.
		{"p2 status median ms", "", "3500 (max 3600)", "", "worse: over 3000 ms"},
		{"p2 risk median ms", "", "200 (max 210)", "", "no base"},
		{"p2 hook-git-status median ms", "", "1200 (max 1300)", "", "worse: over 1000 ms"},
		{"p2 objects", "", "kept", "", "ok"},
		{"friction since", "2026-11-01", "2026-11-01", "", "info"},
		{"stops per 100 tool results", "0.5", "0.56", "+0.06 (+12%)", "worse"},
		{"stops per 100: credentials match", "0.3", "0.45", "+0.15 (+50%)", "info"},
		// Where the friction is: information, no verdict.
		{"runtime claude-code stops/tool results", "3/600 (0.5)", "5/1200 (0.417)", "-0.083 (-17%)", "info"},
		{"runtime pi stops/tool results", "2/400 (0.5)", "6/800 (0.75)", "+0.25 (+50%)", "info"},
		{"budget fixed context claude-code", "20000/23015", "20100/23015", "+100", "ok"},
		{"budget lean text pi", "5000/5120", "5130/5120", "+130", "worse: over its limit"},
		{"hygiene suite TMPDIR leftovers", "0", "2", "+2", "worse"},
		{"result ci-main", "", "pass", "", "no base"},
		{"result doctor", "pass", "pass", "", "ok"},
		{"result go-suite", "pass", "fail", "", "worse"},
	} {
		got := tableRow(t, out.String(), c.measure)
		if got == nil {
			continue
		}
		var want []string
		for _, v := range []string{c.measure, c.base, c.new, c.change, c.verdict} {
			if v != "" {
				want = append(want, v)
			}
		}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("row %q = %q, want %q", c.measure, got, want)
		}
	}
	if !strings.Contains(out.String(), "phasegate: 9 worse") {
		t.Errorf("no count of worse measures:\n%s", out.String())
	}
}

func TestCompareWithinToleranceExitsZero(t *testing.T) {
	stable := strings.Replace(baseJSON, `"label": "base"`, `"label": "again"`, 1)
	stable = strings.Replace(stable, `"median_ms": 1000`, `"median_ms": 1090`, 1)
	stable = strings.Replace(stable, `"per_100": 0.5,`, `"per_100": 0.54,`, 1)
	dir := writeRecords(t, map[string]string{"base": baseJSON, "again": stable})
	var out, errs bytes.Buffer
	if code := run([]string{"compare", "--base", "base", "--new", "again", "--out", dir}, &out, &errs); code != 0 {
		t.Fatalf("exit %d, want 0; stderr %s\n%s", code, errs.String(), out.String())
	}
	if got := tableRow(t, out.String(), "p1 status median ms"); len(got) < 5 || got[len(got)-1] != "ok" {
		t.Errorf("p1 status row %q, want ok", got)
	}
	if !strings.Contains(out.String(), "phasegate: 0 worse") {
		t.Errorf("no count of worse measures:\n%s", out.String())
	}
}

func TestCompareMarkdown(t *testing.T) {
	dir := writeRecords(t, map[string]string{"base": baseJSON, "new": newJSON})
	var out, errs bytes.Buffer
	if code := run([]string{"compare", "--base", "base", "--new", "new", "--out", dir, "--markdown"}, &out, &errs); code != 1 {
		t.Fatalf("exit %d, want 1; stderr %s", code, errs.String())
	}
	lines := strings.Split(out.String(), "\n")
	if lines[0] != "| measure | base | new | change | verdict |" || lines[1] != "|---|---|---|---|---|" {
		t.Errorf("markdown head:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "| p1 status median ms | 1000 (max 1100) | 1300 (max 1400) | +300 ms (+30%) | worse |") {
		t.Errorf("markdown lacks the status row:\n%s", out.String())
	}
}

func TestCompareMissingRecordIsAUsageError(t *testing.T) {
	dir := writeRecords(t, map[string]string{"base": baseJSON})
	var out, errs bytes.Buffer
	if code := run([]string{"compare", "--base", "base", "--new", "nope", "--out", dir}, &out, &errs); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(errs.String(), "nope.json") {
		t.Errorf("stderr %q does not name the missing record", errs.String())
	}
}
