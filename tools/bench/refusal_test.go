package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The refusals fixtures are trimmed from real runs (T105, L682):
// smoke-3a80febe-sonnet two-modules-plain-jhon-1 (a hook error and an ask
// the headless runtime denied, both in the result's permission_denials) and
// smoke-9f8daebf-sonnet two-modules-plain-pi-jhon-1 (pi's safety-guard and
// route-guard refusals), each with a failure, a declined dialog or a
// sub-agent refusal that is no top-level refusal.
func refusalFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "refusals", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A run's refusals come from its stream on both runtimes: Claude Code's
// hook errors, permission refusals and the denials its result lists of a
// tool Jhon's guards judge; pi's guard refusals, not a declined dialog
// (counted as a dialog), an ordinary error, nor Claude Code's denial of
// the bench HOME's engram tool (no Jhon guard judges it, L761).
func TestRefusalsAreCountedFromTheStream(t *testing.T) {
	for _, c := range []struct {
		file string
		want int
	}{{"claude.jsonl", 2}, {"pi.jsonl", 2}} {
		if got := streamRefusals(refusalFixture(t, c.file)); got == nil || *got != c.want {
			t.Errorf("%s: refusals %v, want %d", c.file, deref(got), c.want)
		}
	}
	if got := streamRefusals(nil); got == nil || *got != 0 {
		t.Errorf("an empty stream: %v, want 0", got)
	}
}

// metrics.json carries the stream's refusals even when the transcript
// shows none (the run of L682 reported 0).
func TestAFakeRunRecordsTheRefusalsOfItsStream(t *testing.T) {
	for _, c := range []struct {
		cond, file string
		want       int
	}{{"jhon", "claude.jsonl", 2}, {"pi-jhon", "pi.jsonl", 2}} {
		f := newFake(t, "0.40")
		t.Setenv("FAKE_STREAM", filepath.Join(mustAbs(t, "testdata"), "refusals", c.file))
		o, out := options(t, f, "pagination-bug", []string{c.cond}, 1)
		if err := Execute(o); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		m := metricsOf(t, o, "pagination-bug-plain-"+c.cond+"-1")
		if m.Refusals == nil || *m.Refusals != c.want {
			t.Errorf("%s: refusals %v, want %d", c.cond, deref(m.Refusals), c.want)
		}
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// The replay reads the same refusals metrics.json counts: no disagreement.
func TestTheReplayAgreesWithTheStreamCount(t *testing.T) {
	for _, file := range []string{"claude.jsonl", "pi.jsonl"} {
		b := refusalFixture(t, file)
		top := 0
		for _, e := range guardEvents(b) {
			if e.Kind == "refusal" && e.Agent == "" {
				top++
			}
		}
		if n := streamRefusals(b); n == nil || *n != top {
			t.Errorf("%s: replay sees %d top-level refusals, metrics %v", file, top, deref(n))
		}
	}
}

// two-modules' prompt names which count <n> is: the products the importer
// returns, before the exporter drops the free samples (L682, L712), as the
// hidden check expects ("4 products, 4 rejected").
func TestTheTwoModulesPromptNamesTheCount(t *testing.T) {
	tasks, err := LoadTasks("tasks", []string{"two-modules"})
	if err != nil {
		t.Fatal(err)
	}
	p := strings.Join(strings.Fields(tasks[0].Prompts[0]), " ")
	want := `prints "<n> products, <m> rejected": <n> is the number of products import_products returned (before the export drops any) and <m> the number of rows it rejected.`
	if !strings.Contains(p, want) {
		t.Errorf("prompt %q\nwant it to contain %q", p, want)
	}
	b, err := os.ReadFile(filepath.Join(tasks[0].Dir, "hidden", "test_acceptance.py"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"4 products, 4 rejected"`) {
		t.Errorf("the hidden check no longer expects the imported count")
	}
}
