package main

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// cellRun is one fake run of a certify fixture: a long task with three
// message checks, every quality fact good, and its cost.
func cellRun(variant, cond string, rep int, cost float64) Metrics {
	m := Metrics{Task: "invoice-slices", Variant: variant, Condition: cond, Repeat: rep, Success: true,
		Turns: int(20 * cost), ActiveMs: 100000 * cost, InputTokens: 100 * cost, CacheWriteTokens: 10000 * cost, CacheReadTokens: 500000 * cost, OutputTokens: 5000 * cost,
		TestChanged: bp(true), TestsRunBeforeEnd: bp(true), RedBeforeGreen: bp(true), OutOfScopeFiles: ip(0), Regressions: ip(0), Rework: ip(1),
		Refusals: ip(0), Slices: f64(1), ChecksPerMessage: []MessageCheck{{After: 3, Check: 1, Passed: true}, {After: 3, Check: 2, Passed: true}, {After: 3, Check: 3, Passed: true}}}
	switch {
	case variant == "plain" && isClean(cond):
		m.Ceremony = &Ceremony{Layouts: []string{}}
	case variant == "plain":
		// A Jhon plain run sets LDD up (L554-L559): ldd/ with a record.
		m.Ceremony = &Ceremony{Layouts: []string{"ldd"}}
		m.LddLayout = &LddLayout{Layout: "ldd", TasksMD: true, TestRecord: true, RouteRequiresFolder: "true", Record: bp(true)}
	case variant == "ldd":
		m.LddLayout = &LddLayout{Layout: "openspec", TasksMD: true, TestRecord: true, RouteRequiresFolder: "true", Record: bp(true)}
	default:
		m.LddLayout = &LddLayout{Layout: "ldd", TasksMD: true, TestRecord: true, RouteRequiresFolder: "true", Record: bp(true)}
	}
	return m
}

// putRuns writes ms under dir/runs/, one directory each.
func putRuns(t *testing.T, dir string, ms ...Metrics) {
	t.Helper()
	for _, m := range ms {
		d := filepath.Join(dir, "runs", m.Task+"-"+m.Variant+"-"+m.Condition+"-"+string(rune('0'+m.Repeat)))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := writeJSON(filepath.Join(d, "metrics.json"), m); err != nil {
			t.Fatal(err)
		}
	}
}

// fixture is a baseline (plain and ldd, as the installed build runs) and a
// candidate (every variant), both jhon and pi-jhon, 3 repeats, the
// candidate's cost scaled by cost; edit changes candidate runs first.
func fixture(t *testing.T, cost float64, edit func(m *Metrics)) (base, cand string) {
	t.Helper()
	base, cand = t.TempDir(), t.TempDir()
	for rep := 1; rep <= 3; rep++ {
		for _, cond := range []string{"jhon", "pi-jhon"} {
			for _, v := range AllVariants {
				if v == "plain" || v == "ldd" {
					putRuns(t, base, cellRun(v, cond, rep, 1))
				}
				m := cellRun(v, cond, rep, cost)
				if edit != nil {
					edit(&m)
				}
				putRuns(t, cand, m)
			}
		}
	}
	return base, cand
}

// certify runs `bench certify` with args and a gate runner, its exit code
// and output.
func certify(t *testing.T, runner GateRunner, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := certifyCLI(args, &out, &errOut, runner)
	return code, out.String(), errOut.String()
}

// line is the output line of metric, "" when there is none.
func line(out, metric string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "PASS  "+metric) || strings.HasPrefix(l, "FAIL  "+metric) {
			return l
		}
	}
	return ""
}

func TestCertifyPassesACandidateThatHoldsQualityWithinTheCostMargin(t *testing.T) {
	base, cand := fixture(t, 1.10, nil)
	code, out, errOut := certify(t, nil, "--baseline", base, "--candidate", cand)
	if code != 0 {
		t.Fatalf("exit %d\n%s%s", code, out, errOut)
	}
	for _, metric := range certMetrics {
		if l := line(out, metric); !strings.HasPrefix(l, "PASS") {
			t.Errorf("%s: %q", metric, l)
		}
	}
	for _, s := range []string{"verdict: PASS", "baseline has no ldd-native", "baseline has no ldd-ask", "the baseline's ldd", "absolute quality rules"} {
		if !strings.Contains(out, s) {
			t.Errorf("output lacks %q:\n%s", s, out)
		}
	}
	var c Certification
	b, err := os.ReadFile(filepath.Join(cand, "certify.json"))
	if err != nil || json.Unmarshal(b, &c) != nil || c.Verdict != "PASS" || len(c.Checks) == 0 {
		t.Fatalf("certify.json: %v %s", err, b)
	}
}

func TestCertifyFailsEachRuleItBreaks(t *testing.T) {
	for _, c := range []struct {
		metric string
		cost   float64
		edit   func(m *Metrics)
		note   string
	}{
		{"quota-weighted tokens", 1.20, nil, "> 1.15 ×"},
		{"turns", 1, func(m *Metrics) { m.Turns = 30 }, "> 1.15 ×"},
		{"active time", 1, func(m *Metrics) { m.ActiveMs = 200000 }, "> 1.15 ×"},
		{"final checks", 1, func(m *Metrics) { m.Success = m.Repeat != 2 }, "2/3"},
		{"per-message checks", 1, func(m *Metrics) { m.Slices = f64(2.0 / 3) }, "0.67"},
		{"regressions", 1, func(m *Metrics) { m.Regressions = ip(1) }, "regressions 3"},
		{"rework", 1, func(m *Metrics) { m.Rework = ip(4) }, "4"},
		{"red before green", 1, func(m *Metrics) { m.RedBeforeGreen = bp(m.Repeat != 1) }, "2/3"},
		{"tests run before end", 1, func(m *Metrics) { m.TestsRunBeforeEnd = nil }, "unknown"},
		{"out of scope", 1, func(m *Metrics) { m.OutOfScopeFiles = ip(1) }, "3 files"},
		{"refusals and declined dialogs", 1, func(m *Metrics) { m.UIRequestsDeclined = 1 }, "1"},
		{"plain sets up ldd with a record", 1, func(m *Metrics) {
			if m.Variant == "plain" {
				m.LddLayout = &LddLayout{Layout: "none", RouteRequiresFolder: "unknown", Record: bp(false)}
			}
		}, "none:3 (record 0/3), need ldd:3 (record 3/3)"},

		{"ldd-ask layout and record", 1, func(m *Metrics) {
			if m.Variant == "ldd-ask" {
				m.LddLayout = &LddLayout{Layout: "openspec", TasksMD: true, RouteRequiresFolder: "true", Record: bp(false)}
			}
		}, "openspec:3"},
		{"ldd-native active with a record", 1, func(m *Metrics) {
			if m.Variant == "ldd-native" {
				m.LddLayout.TestRecord, m.LddLayout.Record = false, bp(false)
			}
		}, "record 0/3"},
		{"ldd-native active with a record", 1, func(m *Metrics) {
			if m.Variant == "ldd-native" {
				m.LddLayout.TasksMD, m.LddLayout.RouteRequiresFolder, m.LddLayout.Record = false, "unknown", nil
			}
		}, "record 0/3, 3 unknown"},
	} {
		t.Run(c.metric, func(t *testing.T) {
			base, cand := fixture(t, c.cost, c.edit)
			code, out, _ := certify(t, nil, "--baseline", base, "--candidate", cand)
			if code != 1 || !strings.Contains(out, "verdict: FAIL") {
				t.Fatalf("exit %d\n%s", code, out)
			}
			if l := line(out, c.metric); !strings.HasPrefix(l, "FAIL") {
				t.Errorf("%s: %q\n%s", c.metric, l, out)
			}
			_, detail, _ := strings.Cut(out, line(out, c.metric))
			detail, _, _ = strings.Cut(detail, "\nPASS")
			if !strings.Contains(detail, c.note) {
				t.Errorf("the failing cells lack %q:\n%s", c.note, detail)
			}
		})
	}
}

// Jhon always works under LDD (L554-L559): a Jhon plain run sets ldd/ up
// with a record, and what it did besides (a branch, commits, `jhon
// executions`) is no longer judged as ceremony.
func TestCertifyNoLongerJudgesCeremonyInJhonPlainRuns(t *testing.T) {
	base, cand := fixture(t, 1, func(m *Metrics) {
		if m.Ceremony != nil {
			m.Ceremony.Commits, m.Ceremony.Branches, m.Ceremony.Executions = 2, 1, 1
		}
	})
	code, out, _ := certify(t, nil, "--baseline", base, "--candidate", cand)
	if code != 0 || !strings.HasPrefix(line(out, "plain sets up ldd with a record"), "PASS") || strings.Contains(out, "ceremony") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

// Fewer than 3 repeats on either side is FAIL, never a silent PASS (L483).
func TestCertifyFailsWithNotEnoughRuns(t *testing.T) {
	base, cand := fixture(t, 1, nil)
	for _, dir := range []string{cand, base} {
		gone, _ := filepath.Glob(filepath.Join(dir, "runs", "*-3", "metrics.json"))
		for _, g := range gone {
			if err := os.Remove(g); err != nil {
				t.Fatal(err)
			}
		}
		code, out, _ := certify(t, nil, "--baseline", base, "--candidate", cand)
		if code != 1 || !strings.HasPrefix(line(out, "repeats"), "FAIL") || !strings.Contains(out, "not enough runs") {
			t.Fatalf("exit %d\n%s", code, out)
		}
	}
}

func TestCertifyFailsACellTheCandidateLacks(t *testing.T) {
	base, cand := fixture(t, 1, nil)
	gone, _ := filepath.Glob(filepath.Join(cand, "runs", "invoice-slices-ldd-jhon-*", "metrics.json"))
	for _, g := range gone {
		if err := os.Remove(g); err != nil {
			t.Fatal(err)
		}
	}
	code, out, _ := certify(t, nil, "--baseline", base, "--candidate", cand)
	if code != 1 || !strings.HasPrefix(line(out, "repeats"), "FAIL") || !strings.Contains(out, "invoice-slices ldd jhon: missing in the candidate") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

// A side may be its report.json alone, and several run dirs make one side.
func TestCertifyReadsReportJSONOrSeveralRunDirs(t *testing.T) {
	base, cand := fixture(t, 1.05, nil)
	if err := Report(base, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(base, "runs")); err != nil {
		t.Fatal(err)
	}
	half := t.TempDir()
	moved, _ := filepath.Glob(filepath.Join(cand, "runs", "*pi-jhon*"))
	for _, d := range moved {
		if err := os.MkdirAll(filepath.Join(half, "runs"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(d, filepath.Join(half, "runs", filepath.Base(d))); err != nil {
			t.Fatal(err)
		}
	}
	code, out, _ := certify(t, nil, "--baseline", base, "--candidate", cand+","+half)
	if code != 0 || !strings.Contains(out, "verdict: PASS") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestCertifyJSONPrintsTheCertification(t *testing.T) {
	base, cand := fixture(t, 1.2, nil)
	code, out, _ := certify(t, nil, "--baseline", base, "--candidate", cand, "--json")
	var c Certification
	if err := json.Unmarshal([]byte(out), &c); err != nil || code != 1 || c.Verdict != "FAIL" {
		t.Fatalf("exit %d %v\n%s", code, err, out)
	}
	i := slices.IndexFunc(c.Metrics, func(m MetricVerdict) bool { return m.Metric == "quota-weighted tokens" })
	// 8 cells, the two ldd-ask cells' cost informational (L548) and the two
	// plain ones' (plain now sets LDD up, L554-L559): the other 4 make the
	// geometric mean (L567), and no cell fails alone.
	if i < 0 || c.Metrics[i].Pass || c.Metrics[i].Failed != 0 || c.Metrics[i].Detail != "geometric mean 1.2 over 4 cells, limit 1.15 × (4 informational, 0 review)" {
		t.Errorf("metrics %+v", c.Metrics)
	}
}

// One real report.json (20261110T191929Z, the 45 long-task runs, no names
// or paths in it): certify reads its shape. Against itself every relative
// rule holds; it predates T60 and always-LDD, so its plain cells have no
// layout record:
// FAIL for that reason, never for the runs. Its clean cells are left out.
func TestCertifyReadsARealReport(t *testing.T) {
	real := filepath.Join("testdata", "certify", "real")
	code, out, _ := certify(t, nil, "--baseline", real, "--candidate", real, "--out", t.TempDir())
	if code != 1 || !strings.Contains(line(out, "repeats"), "PASS") || !strings.Contains(line(out, "repeats"), "12/12 cells") ||
		!strings.HasPrefix(line(out, "final checks"), "PASS") || !strings.HasPrefix(line(out, "plain sets up ldd with a record"), "FAIL") || !strings.Contains(out, "invoice-slices plain jhon: —, need ldd:3 (record 3/3)") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

// fakeGates records the gates and fails the ones named.
type fakeGates struct {
	ran  []Gate
	fail string
}

func (g *fakeGates) Run(gate Gate) GateResult {
	g.ran = append(g.ran, gate)
	if gate.Name == g.fail {
		return GateResult{Exit: 1, Tail: "--- FAIL: TestX"}
	}
	return GateResult{Tail: "ok"}
}

// --gates runs the deterministic checks once, isolated and gentle on the
// CPU (owner's rule), and the verdict includes them.
func TestCertifyGatesRunOnceIsolatedAndCount(t *testing.T) {
	repo := t.TempDir()
	g := &fakeGates{}
	code, out, errOut := certify(t, g, "--gates", repo, "--out", t.TempDir())
	if code != 0 || !strings.Contains(out, "verdict: PASS") {
		t.Fatalf("exit %d\n%s%s", code, out, errOut)
	}
	var names []string
	for _, gate := range g.ran {
		names = append(names, gate.Name)
		if gate.Argv[0] != "nice" || gate.Argv[1] != "-n" || gate.Argv[2] != "19" {
			t.Errorf("%s not niced: %v", gate.Name, gate.Argv)
		}
		if !strings.HasPrefix(gate.Dir, repo) {
			t.Errorf("%s runs in %s", gate.Name, gate.Dir)
		}
		env := strings.Join(gate.Env, "\n")
		for _, want := range []string{"GOMAXPROCS=2", "GOPROXY=off", "GIT_CONFIG_NOSYSTEM=1", "GOTOOLCHAIN=local"} {
			if !slices.Contains(gate.Env, want) {
				t.Errorf("%s env lacks %s", gate.Name, want)
			}
		}
		if home := os.Getenv("HOME"); home != "" && slices.Contains(gate.Env, "HOME="+home) {
			t.Errorf("%s runs with the owner's HOME", gate.Name)
		}
		if strings.Contains(env, "ANTHROPIC") || strings.Contains(env, "JHON_ROOT=") {
			t.Errorf("%s inherits the owner's env:\n%s", gate.Name, env)
		}
	}
	if want := []string{"mode matrix", "go suite", "shell deps", "shell suite", "tsc"}; !slices.Equal(names, want) {
		t.Fatalf("gates %v, want %v", names, want)
	}
	// The shell's devDependencies are installed by the gates themselves (L564),
	// from npm's cache when it holds them.
	if deps := strings.Join(g.ran[2].Argv, " "); !strings.HasSuffix(deps, "sh "+filepath.Join(repo, "tools", "shell-deps.sh")+" "+filepath.Join(repo, "shell")) {
		t.Errorf("shell deps gate %q", deps)
	}
	if !slices.ContainsFunc(g.ran[2].Env, func(kv string) bool { return strings.HasPrefix(kv, "npm_config_cache=") }) {
		t.Errorf("shell deps gate has no npm cache: %v", g.ran[2].Env)
	}
	cmd := strings.Join(g.ran[0].Argv, " ")
	if !strings.Contains(cmd, "go test -p 1 -count=1 -run TestModeMatrix ./test/guards/") || !strings.Contains(strings.Join(g.ran[1].Argv, " "), "go test -p 1 ./...") {
		t.Errorf("gate commands %v / %v", g.ran[0].Argv, g.ran[1].Argv)
	}

	base, cand := fixture(t, 1, nil)
	g = &fakeGates{fail: "shell suite"}
	code, out, _ = certify(t, g, "--baseline", base, "--candidate", cand, "--gates", repo)
	if code != 1 || !strings.HasPrefix(line(out, "gate shell suite"), "FAIL") || !strings.HasPrefix(line(out, "final checks"), "PASS") || len(g.ran) != 5 {
		t.Fatalf("exit %d, %d gates\n%s", code, len(g.ran), out)
	}
}

func TestCertifyNeedsSomethingToCertify(t *testing.T) {
	code, _, errOut := certify(t, nil, "--candidate", t.TempDir())
	if code != 2 || !strings.Contains(errOut, "--baseline") {
		t.Fatalf("exit %d %s", code, errOut)
	}
}

// A new variant (ldd-native, ldd-ask) has no baseline cell: its final checks
// hold when its runs, pooled over the new variants of the task and condition,
// pass at least as often as the baseline's runs of that task and condition
// (L545, the owner's rule). A flaky task fails the baseline too.
func TestCertifyJudgesNewVariantsAgainstTheTasksBaselineRate(t *testing.T) {
	failing := func(v string, rep int) Metrics {
		m := cellRun(v, "jhon", rep, 1)
		m.Success = false
		return m
	}
	t.Run("a failure the baseline's task rate allows", func(t *testing.T) {
		base, cand := fixture(t, 1, nil)
		putRuns(t, base, failing("plain", 3))
		putRuns(t, cand, failing("ldd-ask", 3))
		_, out, errOut := certify(t, nil, "--baseline", base, "--candidate", cand)
		if l := line(out, "final checks"); !strings.HasPrefix(l, "PASS") {
			t.Fatalf("final checks: %q\n%s%s", l, out, errOut)
		}
		b, err := os.ReadFile(filepath.Join(cand, "certify.json"))
		if err != nil || !strings.Contains(string(b), "new variants of the task 5/6 vs the baseline's task 5/6") {
			t.Errorf("certify.json lacks the pooled rates: %v", err)
		}
	})
	t.Run("more failures than the baseline's task rate", func(t *testing.T) {
		base, cand := fixture(t, 1, nil)
		putRuns(t, base, failing("plain", 3))
		putRuns(t, cand, failing("ldd-ask", 2), failing("ldd-ask", 3))
		_, out, _ := certify(t, nil, "--baseline", base, "--candidate", cand)
		if l := line(out, "final checks"); !strings.HasPrefix(l, "FAIL") {
			t.Fatalf("final checks: %q\n%s", l, out)
		}
	})
	t.Run("a perfect baseline still needs every new-variant run", func(t *testing.T) {
		base, cand := fixture(t, 1, nil)
		putRuns(t, cand, failing("ldd-native", 1))
		_, out, _ := certify(t, nil, "--baseline", base, "--candidate", cand)
		if l := line(out, "final checks"); !strings.HasPrefix(l, "FAIL") {
			t.Fatalf("final checks: %q\n%s", l, out)
		}
	})
}

// ldd-ask sets LDD up in the run, which the baseline's ldd cell it is
// compared with never pays: its cost is shown but does not fail the
// certification (L548, the owner's rule). Every other cell keeps the margin.
func TestCertifyShowsLddAskCostWithoutFailingOnIt(t *testing.T) {
	base, cand := fixture(t, 1, nil)
	putRuns(t, cand, cellRun("ldd-ask", "jhon", 1, 2), cellRun("ldd-ask", "jhon", 2, 2), cellRun("ldd-ask", "jhon", 3, 2))
	_, out, errOut := certify(t, nil, "--baseline", base, "--candidate", cand)
	for _, metric := range []string{"quota-weighted tokens", "turns", "active time"} {
		if l := line(out, metric); !strings.HasPrefix(l, "PASS") {
			t.Errorf("%s: %q\n%s%s", metric, l, out, errOut)
		}
	}
	b, err := os.ReadFile(filepath.Join(cand, "certify.json"))
	if err != nil || !strings.Contains(string(b), "informational: ldd-ask sets LDD up") {
		t.Errorf("certify.json does not say the ldd-ask cost is informational: %v", err)
	}

	base, cand = fixture(t, 1, nil)
	putRuns(t, cand, cellRun("ldd-native", "jhon", 1, 2), cellRun("ldd-native", "jhon", 2, 2), cellRun("ldd-native", "jhon", 3, 2))
	_, out, _ = certify(t, nil, "--baseline", base, "--candidate", cand)
	if l := line(out, "turns"); !strings.HasPrefix(l, "FAIL") {
		t.Errorf("ldd-native's cost lost its margin: %q", l)
	}
}

// plainCell puts a cond plain cell at cost in dir.
func plainCell(t *testing.T, dir, cond string, cost float64) {
	t.Helper()
	putRuns(t, dir, cellRun("plain", cond, 1, cost), cellRun("plain", cond, 2, cost), cellRun("plain", cond, 3, cost))
}

// checksOf are the certify.json checks of one cell, by metric.
func checksOf(t *testing.T, dir, variant, cond string) map[string]CertCheck {
	t.Helper()
	var c Certification
	b, err := os.ReadFile(filepath.Join(dir, "certify.json"))
	if err != nil || json.Unmarshal(b, &c) != nil {
		t.Fatalf("certify.json: %v", err)
	}
	out := map[string]CertCheck{}
	for _, ch := range c.Checks {
		if ch.Variant == variant && ch.Condition == cond {
			out[ch.Metric] = ch
		}
	}
	return out
}

// Jhon always works under LDD (T72b, L554-L559): a Jhon plain cell must set
// ldd/ up with a record, like ldd-ask, and its cost is informational against
// the baseline's plain cell, which worked without LDD (L548). Its quality is
// still compared with the baseline's plain cell.
func TestCertifyJudgesAJhonPlainCellBySetUpWithCostInformational(t *testing.T) {
	base, cand := fixture(t, 1, nil)
	plainCell(t, cand, "jhon", 2)
	code, out, errOut := certify(t, nil, "--baseline", base, "--candidate", cand)
	if code != 0 {
		t.Fatalf("exit %d\n%s%s", code, out, errOut)
	}
	for _, metric := range []string{"plain sets up ldd with a record", "quota-weighted tokens", "turns", "active time"} {
		if l := line(out, metric); !strings.HasPrefix(l, "PASS") {
			t.Errorf("%s: %q\n%s", metric, l, out)
		}
	}
	if !strings.Contains(out, "note: plain now sets LDD up (L554-L559)") {
		t.Errorf("no note on plain:\n%s", out)
	}
	ch := checksOf(t, cand, "plain", "jhon")
	if d := ch["turns"].Detail; !strings.Contains(d, "40 vs baseline 20") || !strings.Contains(d, "informational: plain now sets LDD up (L554-L559)") {
		t.Errorf("turns detail %q", d)
	}
	if d := ch["plain sets up ldd with a record"].Detail; d != "ldd:3 (record 3/3), need ldd:3 (record 3/3)" {
		t.Errorf("set-up detail %q", d)
	}
	if d := ch["final checks"].Detail; d != "3/3 passed, final message checks 9/9 vs baseline 3/3, 9/9" {
		t.Errorf("final checks detail %q", d)
	}
	if _, ok := ch["ceremony (plain)"]; ok {
		t.Errorf("a Jhon plain cell is still judged on ceremony")
	}
}

// A Jhon plain cell that sets nothing up fails the set-up check.
func TestCertifyFailsAJhonPlainCellThatSetsNothingUp(t *testing.T) {
	base, cand := fixture(t, 1, func(m *Metrics) {
		if m.Variant == "plain" && m.Condition == "pi-jhon" {
			m.Ceremony = &Ceremony{Layouts: []string{}}
			m.LddLayout = &LddLayout{Layout: "none", RouteRequiresFolder: "unknown", Record: bp(false)}
		}
	})
	code, out, _ := certify(t, nil, "--baseline", base, "--candidate", cand)
	if code != 1 || line(out, "plain sets up ldd with a record") != "FAIL  plain sets up ldd with a record  1/2 cells" ||
		!strings.Contains(out, "invoice-slices plain pi-jhon: none:3 (record 0/3), need ldd:3 (record 3/3)") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

// The clean conditions are the no-Jhon baseline (L552): their plain cell is
// judged as before, no set-up check, its cost in the geometric mean (L567).
// The no-Jhon conditions measure the model, not Jhon: certify leaves them
// out of every metric, on either side, and names them (L575).
func TestCertifyLeavesTheNoJhonConditionsOut(t *testing.T) {
	ref, refCand := fixture(t, 1.10, nil)
	_, want, _ := certify(t, nil, "--baseline", ref, "--candidate", refCand)
	for _, inBase := range []bool{false, true} {
		base, cand := fixture(t, 1.10, nil)
		for _, cond := range []string{"clean", "pi-clean"} {
			// Three times the cost of every other cell, alone over ×1.15:
			// judged, they would fail.
			plainCell(t, cand, cond, 3)
			if inBase {
				plainCell(t, base, cond, 1)
			}
		}
		code, out, errOut := certify(t, nil, "--baseline", base, "--candidate", cand)
		if code != 0 {
			t.Fatalf("baseline cells %v: exit %d\n%s%s", inBase, code, out, errOut)
		}
		for _, metric := range certMetrics {
			if got, w := line(out, metric), line(want, metric); got != w {
				t.Errorf("baseline cells %v: %s %q, want %q", inBase, metric, got, w)
			}
		}
		if !strings.HasPrefix(line(out, "repeats"), "PASS  repeats") || strings.Contains(out, "clean:") {
			t.Errorf("output:\n%s", out)
		}
		note := "  note: skipped 6 clean and pi-clean runs (candidate 6, baseline 0): they are not certification cells, `bench value` compares them"
		if inBase {
			note = "  note: skipped 12 clean and pi-clean runs (candidate 6, baseline 6): they are not certification cells, `bench value` compares them"
		}
		if !strings.Contains(out, note+"\n") || strings.Contains(want, "skipped") {
			t.Errorf("note %q lacking:\n%s", note, out)
		}
		if ch := checksOf(t, cand, "plain", "clean"); len(ch) != 0 {
			t.Errorf("clean checks %v", ch)
		}
	}
}

// ldd, ldd-native and ldd-ask are judged as before T72b.
func TestCertifyKeepsTheLDDVariantsAsTheyWere(t *testing.T) {
	base, cand := fixture(t, 1, nil)
	if code, out, _ := certify(t, nil, "--baseline", base, "--candidate", cand); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	quality := []string{"repeats", "final checks", "per-message checks", "regressions", "rework", "red before green", "tests run before end",
		"out of scope", "refusals and declined dialogs"}
	cost := []string{"quota-weighted tokens", "turns", "active time"}
	for v, extra := range map[string][]string{"ldd": nil, "ldd-native": {"ldd-native active with a record"}, "ldd-ask": {"ldd-ask layout and record"}} {
		ch := checksOf(t, cand, v, "jhon")
		var got []string
		for _, m := range certMetrics {
			if _, ok := ch[m]; ok {
				got = append(got, m)
			}
		}
		want := append(append(slices.Clone(quality), extra...), cost...)
		if !slices.Equal(got, want) {
			t.Errorf("%s: metrics %v, want %v", v, got, want)
		}
		d := ch["turns"].Detail
		if v == "ldd-ask" && d != "20 vs baseline 20 (1×); informational: ldd-ask sets LDD up, which the baseline's ldd never pays (L548)" ||
			v != "ldd-ask" && d != "20 vs baseline 20 (1×)" {
			t.Errorf("%s turns %q", v, d)
		}
	}
	if d := checksOf(t, cand, "ldd-ask", "jhon")["ldd-ask layout and record"].Detail; d != "ldd:3 (record 3/3), need ldd:3 (record 3/3)" {
		t.Errorf("ldd-ask detail %q", d)
	}
}

// wideTasks are the tasks of a wide fixture: 3 tasks × 2 conditions × 4
// variants × 3 repeats are 72 candidate runs, each paired with its
// comparison cell's baseline runs (72 too).
var wideTasks = []string{"invoice-slices", "slow-suite", "two-modules"}

// wideFixture is fixture over wideTasks; costOf scales each candidate
// cell's cost, edit changes candidate runs.
func wideFixture(t *testing.T, costOf func(task, variant, cond string) float64, edit func(m *Metrics)) (base, cand string) {
	t.Helper()
	base, cand = t.TempDir(), t.TempDir()
	for _, task := range wideTasks {
		for rep := 1; rep <= 3; rep++ {
			for _, cond := range []string{"jhon", "pi-jhon"} {
				for _, v := range AllVariants {
					if v == "plain" || v == "ldd" {
						m := cellRun(v, cond, rep, 1)
						m.Task = task
						putRuns(t, base, m)
					}
					cost := 1.0
					if costOf != nil {
						cost = costOf(task, v, cond)
					}
					m := cellRun(v, cond, rep, cost)
					m.Task = task
					if edit != nil {
						edit(&m)
					}
					putRuns(t, cand, m)
				}
			}
		}
	}
	return base, cand
}

// metricOf is certify.json's verdict of metric in dir.
func metricOf(t *testing.T, dir, metric string) MetricVerdict {
	t.Helper()
	var c Certification
	b, err := os.ReadFile(filepath.Join(dir, "certify.json"))
	if err != nil || json.Unmarshal(b, &c) != nil {
		t.Fatalf("certify.json: %v", err)
	}
	for _, m := range c.Metrics {
		if m.Metric == metric {
			return m
		}
	}
	t.Fatalf("no metric %s", metric)
	return MetricVerdict{}
}

// Cost is judged over every compared cell (L567): the geometric mean of the
// per-cell candidate/baseline median ratios may be at most 1.15.
func TestCertifyJudgesCostByTheGeometricMeanOfTheCells(t *testing.T) {
	t.Run("cells vary and the mean holds", func(t *testing.T) {
		base, cand := wideFixture(t, func(task, _, _ string) float64 {
			return map[string]float64{"invoice-slices": 1.3, "slow-suite": 0.8, "two-modules": 1}[task]
		}, nil)
		code, out, errOut := certify(t, nil, "--baseline", base, "--candidate", cand)
		if code != 0 {
			t.Fatalf("exit %d\n%s%s", code, out, errOut)
		}
		for _, metric := range []string{"quota-weighted tokens", "turns", "active time"} {
			l := line(out, metric)
			if !strings.HasPrefix(l, "PASS") || !strings.Contains(l, "geometric mean 1.01") || !strings.Contains(l, "limit 1.15") {
				t.Errorf("%s: %q", metric, l)
			}
		}
		var c Certification
		b, _ := os.ReadFile(filepath.Join(cand, "certify.json"))
		_ = json.Unmarshal(b, &c)
		for _, x := range c.Checks {
			if x.Task == "invoice-slices" && x.Variant == "ldd" && x.Condition == "jhon" && x.Metric == "turns" && x.Detail != "26 vs baseline 20 (1.3×)" {
				t.Errorf("invoice-slices ldd jhon turns %q", x.Detail)
			}
		}
	})
	t.Run("a mean of 1.2 fails", func(t *testing.T) {
		base, cand := wideFixture(t, func(string, string, string) float64 { return 1.2 }, nil)
		code, out, _ := certify(t, nil, "--baseline", base, "--candidate", cand)
		if code != 1 || !strings.HasPrefix(line(out, "turns"), "FAIL") || !strings.Contains(line(out, "turns"), "geometric mean 1.2") {
			t.Fatalf("exit %d\n%s", code, out)
		}
	})
	t.Run("a cell over 1.5 is flagged for review, not failed", func(t *testing.T) {
		base, cand := wideFixture(t, func(task, v, cond string) float64 {
			if task == "slow-suite" && v == "ldd" && cond == "jhon" {
				return 1.6
			}
			return 1
		}, nil)
		code, out, _ := certify(t, nil, "--baseline", base, "--candidate", cand)
		if code != 0 || !strings.HasPrefix(line(out, "turns"), "PASS") || !strings.Contains(out, "review slow-suite ldd jhon: 32 vs baseline 20 (1.6×)") {
			t.Fatalf("exit %d\n%s", code, out)
		}
		ch := checksOf(t, cand, "ldd", "jhon")
		var c Certification
		b, _ := os.ReadFile(filepath.Join(cand, "certify.json"))
		_ = json.Unmarshal(b, &c)
		reviewed := 0
		for _, x := range c.Checks {
			if x.Review {
				reviewed++
				if x.Task != "slow-suite" || x.Variant != "ldd" || x.Condition != "jhon" || !x.Pass {
					t.Errorf("review on %+v", x)
				}
			}
		}
		if reviewed != 3 || ch["turns"].Pass != true {
			t.Errorf("%d review checks", reviewed)
		}
	})
	t.Run("an unknown cost fails", func(t *testing.T) {
		base, cand := wideFixture(t, nil, func(m *Metrics) {
			if m.Task == "two-modules" && m.Variant == "ldd" && m.Condition == "pi-jhon" {
				m.ActiveMs = 0
			}
		})
		code, out, _ := certify(t, nil, "--baseline", base, "--candidate", cand)
		if code != 1 || !strings.HasPrefix(line(out, "active time"), "FAIL") || !strings.Contains(out, "two-modules ldd pi-jhon: active time unknown") {
			t.Fatalf("exit %d\n%s", code, out)
		}
	})
}

// RED before GREEN, tests before the end and out of scope are judged over
// every run of every compared cell (L567): FAIL only when the candidate is
// worse beyond chance, one-sided Fisher exact p < 0.05.
func TestCertifyJudgesDisciplinePooledWithFisher(t *testing.T) {
	// drop marks the first n candidate runs (in fixture order) of the
	// judged variants.
	drop := func(n int, set func(m *Metrics)) func(m *Metrics) {
		seen := 0
		return func(m *Metrics) {
			if seen < n {
				seen++
				set(m)
			}
		}
	}
	for _, c := range []struct {
		metric string
		set    func(m *Metrics)
	}{
		{"red before green", func(m *Metrics) { m.RedBeforeGreen = bp(false) }},
		{"tests run before end", func(m *Metrics) { m.TestsRunBeforeEnd = bp(false) }},
		{"out of scope", func(m *Metrics) { m.OutOfScopeFiles = ip(1) }},
	} {
		t.Run(c.metric+": 1 of 72 is chance", func(t *testing.T) {
			base, cand := wideFixture(t, nil, drop(1, c.set))
			code, out, errOut := certify(t, nil, "--baseline", base, "--candidate", cand)
			l := line(out, c.metric)
			if code != 0 || !strings.HasPrefix(l, "PASS") || !strings.Contains(l, "pooled") || !strings.Contains(l, "p=0.5") {
				t.Fatalf("exit %d %q\n%s%s", code, l, out, errOut)
			}
			if !strings.Contains(out, "invoice-slices plain jhon:") {
				t.Errorf("the worse cell is not shown:\n%s", out)
			}
		})
		t.Run(c.metric+": 20 of 72 fails", func(t *testing.T) {
			base, cand := wideFixture(t, nil, drop(20, c.set))
			code, out, _ := certify(t, nil, "--baseline", base, "--candidate", cand)
			l := line(out, c.metric)
			if code != 1 || !strings.HasPrefix(l, "FAIL") || !strings.Contains(l, "p<0.001") {
				t.Fatalf("exit %d %q\n%s", code, l, out)
			}
			if m := metricOf(t, cand, c.metric); m.Pass || !strings.Contains(m.Detail, "Fisher") {
				t.Errorf("certify.json %+v", m)
			}
		})
	}
}

func TestFisherLower(t *testing.T) {
	for _, c := range []struct {
		a, n1, b, n2 int
		want         float64
	}{
		{71, 72, 72, 72, 0.5},
		{72, 72, 72, 72, 1},
		{2, 3, 3, 3, 0.5},
		{0, 3, 3, 3, 0.05},
		{1, 4, 4, 4, 0.0714285714},
	} {
		if got := fisherLower(c.a, c.n1, c.b, c.n2); math.Abs(got-c.want) > 1e-6 {
			t.Errorf("fisherLower(%d/%d vs %d/%d) = %v, want %v", c.a, c.n1, c.b, c.n2, got, c.want)
		}
	}
}
