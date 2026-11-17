package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// `bench certify` (T60) is the gate before `jhon sync` (L482, L483): a
// candidate build's runs against a baseline's, per task × variant ×
// condition. Quality has no margin: each quality metric must hold at least
// the baseline's, and the absolute ones (no regression, nothing out of
// scope, a Jhon plain repo set up with ldd/ and a record, the LDD record
// kept) must hold
// outright. Cost — quota-weighted tokens, turns, active time — is judged
// over every compared cell (L567): the geometric mean of the per-cell
// candidate/baseline median ratios may be at most 1.15; a cell over 1.5 is
// flagged for review. RED before GREEN, tests before the end and out of
// scope are pooled over every run of every compared cell and FAIL only
// when the candidate is worse beyond chance (one-sided Fisher exact,
// p < 0.05). Fewer than 3 repeats on either side is FAIL. The
// deterministic gates (--gates) run once and count too.

// MinRepeats are the runs a cell needs on each side (L483: 3 runs).
const MinRepeats = 3

// CostMargin is how far the geometric mean of the cells' cost ratios may
// rise over the baseline's (L483: 15 %, over every cell since L567).
const CostMargin = 1.15

// ReviewRatio is the cell cost ratio flagged for review, not failed (L567).
const ReviewRatio = 1.5

// FisherAlpha is the one-sided Fisher p below which pooled discipline fails (L567).
const FisherAlpha = 0.05

// costMetrics are judged by the geometric mean over the cells,
// pooledMetrics over every run with Fisher's exact test (L567).
var (
	costMetrics   = []string{"quota-weighted tokens", "turns", "active time"}
	pooledMetrics = []string{"red before green", "tests run before end", "out of scope"}
)

// certMetrics are the metrics certify judges, in print order.
var certMetrics = []string{
	"repeats", "final checks", "per-message checks", "regressions", "rework", "red before green", "tests run before end",
	"out of scope", "refusals and declined dialogs", "plain sets up ldd with a record", "ldd-ask layout and record", "ldd-native active with a record",
	"quota-weighted tokens", "turns", "active time",
}

// CertCheck is one metric of one cell.
type CertCheck struct {
	Metric    string `json:"metric"`
	Task      string `json:"task"`
	Variant   string `json:"variant"`
	Condition string `json:"condition"`
	Pass      bool   `json:"pass"`
	Detail    string `json:"detail"`
	// Review flags a judged cost cell over ReviewRatio × the baseline.
	Review bool `json:"review,omitempty"`
	// ratio is a judged cost cell's candidate/baseline ratio (0 when
	// informational or unknown); pool a discipline cell's good runs and
	// known runs, candidate then baseline; worse when the candidate's
	// cell rate is below the baseline's.
	ratio float64
	pool  *[4]int
	worse bool
}

// cell is "task variant condition".
func (c CertCheck) cell() string { return c.Task + " " + c.Variant + " " + c.Condition }

// MetricVerdict is one metric over every cell: PASS when no cell failed,
// and for the cost and pooled metrics when Detail's aggregate holds.
type MetricVerdict struct {
	Metric string `json:"metric"`
	Pass   bool   `json:"pass"`
	Failed int    `json:"failed"`
	Of     int    `json:"of"`
	Detail string `json:"detail,omitempty"`
}

// lchoose is log C(n, k).
func lchoose(n, k int) float64 {
	a, _ := math.Lgamma(float64(n + 1))
	b, _ := math.Lgamma(float64(k + 1))
	c, _ := math.Lgamma(float64(n - k + 1))
	return a - b - c
}

// fisherLower is the one-sided Fisher exact p that a/n1 is this much lower
// than b/n2 by chance: P(X ≤ a), X hypergeometric with a+b successes in
// n1+n2 runs, n1 drawn.
func fisherLower(a, n1, b, n2 int) float64 {
	n, k := n1+n2, a+b
	if n1 == 0 || n2 == 0 {
		return 1
	}
	p := 0.0
	for x := max(0, n1-(n-k)); x <= a; x++ {
		p += math.Exp(lchoose(k, x) + lchoose(n-k, n1-x) - lchoose(n, n1))
	}
	return min(p, 1)
}

// Certification is certify.json.
type Certification struct {
	Baseline  []string        `json:"baseline"`
	Candidate []string        `json:"candidate"`
	Notes     []string        `json:"notes"`
	Metrics   []MetricVerdict `json:"metrics"`
	Checks    []CertCheck     `json:"checks"`
	Gates     []GateVerdict   `json:"gates"`
	Verdict   string          `json:"verdict"`
	// Replay is --replay-guard's report, absent without it.
	Replay *ReplayReport `json:"replay,omitempty"`
}

// GateVerdict is one deterministic gate's result.
type GateVerdict struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Pass    bool   `json:"pass"`
	Exit    int    `json:"exit"`
	Tail    string `json:"tail"`
}

// loadSide is the rows of one side: the runs/*/metrics.json of each dir
// together, or a dir's report.json when it has no runs. A cell may come
// from one report.json only: medians do not merge. With rp, each run's
// refusals and dialogs are replayed first (replay.go) as side's.
func loadSide(dirs []string, rp *replayer, side string) (map[[3]string]Row, error) {
	rows, _, err := loadSideNotes(dirs, rp, side)
	return rows, err
}

// loadSideNotes is loadSide with its notes: each run's refusals are
// recounted from its stream (streamRefusals), so both sides count them the
// same way whatever build wrote their metrics.json (L761); a run with no
// stream keeps metrics.json's count. Denials of tools no Jhon guard judges
// are listed, not counted.
func loadSideNotes(dirs []string, rp *replayer, side string) (map[[3]string]Row, []string, error) {
	groups := map[[3]string][]Metrics{}
	out := map[[3]string]Row{}
	from := map[[3]string]string{}
	runs, recounted := 0, 0
	denials := map[string][]string{}
	for _, dir := range dirs {
		each := func(runDir string, m *Metrics) {
			runs++
			// A stream with no record (unreadable) cannot recount.
			if b, err := os.ReadFile(filepath.Join(runDir, "stream.jsonl")); err == nil && len(records(b)) > 0 {
				recounted++
				m.Refusals = streamRefusals(b)
				for _, e := range guardEvents(b) {
					if e.Kind == "denial" {
						denials[e.Tool] = append(denials[e.Tool], filepath.Base(runDir))
					}
				}
			}
			if rp != nil {
				rp.run(side, runDir, m)
			}
		}
		if g, err := groupRunsWith(dir, each); err == nil {
			for k, ms := range g {
				groups[k] = append(groups[k], ms...)
			}
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, "report.json"))
		if err != nil {
			return nil, nil, fmt.Errorf("%s has neither runs/*/metrics.json nor report.json", dir)
		}
		if rp != nil {
			rp.notes = append(rp.notes, fmt.Sprintf("%s has no runs, only report.json: its refusals and dialogs are not replayed and count as they are", dir))
		}
		var rep struct{ Rows []Row }
		if err := json.Unmarshal(b, &rep); err != nil {
			return nil, nil, fmt.Errorf("%s: %v", filepath.Join(dir, "report.json"), err)
		}
		for _, r := range rep.Rows {
			k := [3]string{r.Task, cmpOr(r.Variant, "plain"), cellKey(r.Condition, identity(r.Model, r.Profile, r.ProjectSettings))}
			if prev, dup := from[k]; dup {
				return nil, nil, fmt.Errorf("%s is in both %s and %s: give their run dirs", cellName(k), prev, dir)
			}
			from[k], out[k] = dir, r
		}
	}
	for _, r := range rowsOf(groups) {
		k := [3]string{r.Task, r.Variant, cellKey(r.Condition, identity(r.Model, r.Profile, r.ProjectSettings))}
		if prev, dup := from[k]; dup {
			return nil, nil, fmt.Errorf("%s is in both %s's report.json and the run dirs", cellName(k), prev)
		}
		out[k] = r
	}
	var notes []string
	if runs > 0 {
		note := fmt.Sprintf("%s: refusals recounted from the stream in %d of %d runs", side, recounted, runs)
		if recounted < runs {
			note += fmt.Sprintf("; %d have no readable stream.jsonl and keep metrics.json's count", runs-recounted)
		}
		notes = append(notes, note)
	}
	if len(denials) > 0 {
		var list []string
		for _, tool := range slices.Sorted(maps.Keys(denials)) {
			list = append(list, fmt.Sprintf("%s %d (%s)", tool, len(denials[tool]), strings.Join(denials[tool], ", ")))
		}
		notes = append(notes, fmt.Sprintf("%s: Claude Code permission denials of tools no Jhon guard judges, not counted as refusals: %s", side, strings.Join(list, "; ")))
	}
	return out, notes, nil
}

// frac is a/b, 0 when b is 0.
func frac(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// num2 is v to two decimals, trailing zeros dropped.
func num2(v float64) string { return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64) }

// weighted is a row's median quota-weighted tokens; a report from before
// T60 has none, so it is weighed from the token medians instead.
func weighted(r Row) (float64, string) {
	if v, ok := r.Medians["quota_weighted"]; ok {
		return v, ""
	}
	m := r.Medians
	return m["input_tokens"] + 1.25*m["cache_write_tokens"] + 0.1*m["cache_read_tokens"] + 5*m["output_tokens"], " (weighed from the token medians)"
}

// weightedKnown is whether a row knows its quota-weighted tokens.
func weightedKnown(r Row) bool {
	_, q := r.Medians["quota_weighted"]
	_, i := r.Medians["input_tokens"]
	return q || i
}

// perRun are a row's refusals and declined dialogs per run, and whether its
// refusals are known.
func perRun(r Row) (float64, bool) {
	return frac(deref(r.Process.Refusals)+r.Process.UIRequestsDeclined, r.Runs), r.Process.Refusals != nil
}

// outOfScope is a row's out-of-scope files: the total when known, else the
// median of a report from before T60.
func outOfScope(r Row) (float64, bool) {
	if r.Process.OutOfScopeTotal != nil {
		return float64(*r.Process.OutOfScopeTotal), true
	}
	if r.Process.OutOfScopeFiles != nil {
		return *r.Process.OutOfScopeFiles, true
	}
	return 0, false
}

// outOfScopeRuns are a row's runs with at least one out-of-scope file and
// the runs that know it. A report from before T73 has only the total (or
// the median): 0 means none of its runs, more counts every run.
func outOfScopeRuns(r Row) (bad, known int, ok bool) {
	if q := r.Process.OutOfScopeRuns; q != nil {
		return q.Yes, q.Known, q.Known > 0
	}
	n, ok := outOfScope(r)
	if !ok {
		return 0, 0, false
	}
	if n > 0 {
		return r.Runs, r.Runs, true
	}
	return 0, r.Runs, true
}

// A taskRate is the hidden-check pass count of a task under a condition:
// the candidate's new-variant runs pooled, and the baseline's runs of every
// variant pooled (L545).
type taskRate struct{ candPassed, candRuns, basePassed, baseRuns int }

// judge is every check of candidate cell c against its baseline cell b;
// absolute is true when b is another variant's (a variant the baseline
// lacks), so the relative quality rules hold outright instead, except the
// hidden check, which pool judges: the new variants' pooled pass rate may not
// fall below the baseline's rate for the same task and condition.
func judge(c, b Row, absolute bool, pool ...taskRate) []CertCheck {
	var out []CertCheck
	add := func(metric string, pass bool, format string, args ...any) {
		out = append(out, CertCheck{Metric: metric, Task: c.Task, Variant: c.Variant, Condition: c.Condition, Pass: pass, Detail: fmt.Sprintf(format, args...)})
	}
	vs := func(cv, bv string) string {
		if absolute {
			return cv + ", need all (absolute: new variant)"
		}
		return cv + " vs baseline " + bv
	}
	if c.Runs < MinRepeats || b.Runs < MinRepeats {
		add("repeats", false, "not enough runs: candidate %d, baseline %d, need %d each", c.Runs, b.Runs, MinRepeats)
	} else {
		add("repeats", true, "candidate %d, baseline %d", c.Runs, b.Runs)
	}
	// final checks: the hidden test, and the per-message checks after the
	// last message where the task has them.
	cf, bf := frac(c.Passed, c.Runs), frac(b.Passed, b.Runs)
	ok := cf >= bf && (!absolute || cf == 1)
	pooled := ""
	if absolute && len(pool) == 1 {
		r := pool[0]
		ok = frac(r.candPassed, r.candRuns) >= frac(r.basePassed, r.baseRuns)
		pooled = fmt.Sprintf("; new variants of the task %d/%d vs the baseline's task %d/%d", r.candPassed, r.candRuns, r.basePassed, r.baseRuns)
	}
	if q := c.Quality.FinalChecks; q.Known > 0 {
		ok = ok && frac(q.Yes, q.Known) >= frac(b.Quality.FinalChecks.Yes, b.Quality.FinalChecks.Known) && (!absolute || q.Yes == q.Known)
	}
	detail := vs(fmt.Sprintf("%d/%d passed, final message checks %s", c.Passed, c.Runs, c.Quality.FinalChecks),
		fmt.Sprintf("%d/%d, %s", b.Passed, b.Runs, b.Quality.FinalChecks))
	if pooled != "" {
		detail = fmt.Sprintf("%d/%d passed, final message checks %s%s (new variant: no worse than the task's baseline rate)", c.Passed, c.Runs, c.Quality.FinalChecks, pooled)
	}
	add("final checks", ok, "%s", detail)
	cs, cok := c.Medians["slices"]
	bs, bok := b.Medians["slices"]
	switch {
	case !cok && !bok && c.Quality.FinalChecks.Known == 0:
		add("per-message checks", true, "no per-message checks")
	case !cok:
		add("per-message checks", false, "slices unknown in the candidate (a run from before T60)")
	default:
		add("per-message checks", (absolute && cs == 1) || (!absolute && (!bok || cs >= bs)), "%s", vs("slices median "+num2(cs), num2(bs)))
	}
	reg := c.Quality.Regressions
	add("regressions", reg == 0, "regressions %d, need 0", reg)
	switch {
	case absolute:
		add("rework", true, "rework %s (no baseline of this variant)", count(c.Quality.Rework))
	case c.Quality.Rework == nil:
		add("rework", false, "rework unknown in the candidate")
	default:
		cr, br := frac(deref(c.Quality.Rework), c.Runs), frac(deref(b.Quality.Rework), b.Runs)
		add("rework", b.Quality.Rework == nil || cr <= br, "%s per run vs baseline %s", num2(cr), num2(br))
	}
	// Discipline is judged pooled over every compared cell (L567, verdicts);
	// a cell shows its rates and adds its runs, new variants against the
	// baseline cell they are compared with.
	addPooled := func(metric string, cy, ck, by, bk int, format string, args ...any) {
		add(metric, true, format, args...)
		out[len(out)-1].pool = &[4]int{cy, ck, by, bk}
		out[len(out)-1].worse = frac(cy, ck) < frac(by, bk)
	}
	rate := func(metric string, cr, br Rate) {
		if cr.Known == 0 {
			add(metric, false, "unknown in the candidate")
			return
		}
		addPooled(metric, cr.Yes, cr.Known, br.Yes, br.Known, "%s vs baseline %s", cr.String(), br.String())
	}
	rate("red before green", c.Process.RedBeforeGreen, b.Process.RedBeforeGreen)
	rate("tests run before end", c.Process.TestsRunBeforeEnd, b.Process.TestsRunBeforeEnd)
	if cbad, cknown, ok := outOfScopeRuns(c); !ok {
		add("out of scope", false, "out of scope unknown in the candidate")
	} else {
		n, _ := outOfScope(c)
		bbad, bknown, _ := outOfScopeRuns(b)
		bn, _ := outOfScope(b)
		addPooled("out of scope", cknown-cbad, cknown, bknown-bbad, bknown, "%s files in %d/%d runs vs baseline %s files in %d/%d runs", num2(n), cbad, cknown, num2(bn), bbad, bknown)
	}
	cp, cknown := perRun(c)
	bp, bknown := perRun(b)
	note := ""
	if cknown && !bknown {
		// The baseline knows only its declined dialogs: compare those.
		cp, note = frac(c.Process.UIRequestsDeclined, c.Runs), " (dialogs only: the baseline has no refusal count)"
	}
	switch {
	case !cknown:
		add("refusals and declined dialogs", false, "refusals unknown in the candidate (a run from before T60)")
	case absolute:
		add("refusals and declined dialogs", cknown && cp == 0, "%s per run, need 0 (absolute: new variant)", num2(cp))
	default:
		add("refusals and declined dialogs", cp <= bp, "%s per run vs baseline %s%s", num2(cp), num2(bp), note)
	}
	// Jhon always works under LDD (L554-L559): a Jhon plain run sets ldd/
	// up, as ldd-ask does when asked; the clean conditions are not judged.
	for _, v := range []struct{ variant, metric string }{{"plain", "plain sets up ldd with a record"}, {"ldd-ask", "ldd-ask layout and record"}, {"ldd-native", "ldd-native active with a record"}} {
		if c.Variant != v.variant || isClean(c.Condition) {
			continue
		}
		l := c.Quality.Ldd
		add(v.metric, l != nil && l.Unknown == 0 && l.Known == c.Runs && l.Layouts["ldd"] == c.Runs && l.Records == c.Runs, "%s, need ldd:%d (record %d/%d)", l, c.Runs, c.Runs, c.Runs)
	}
	// A run that sets LDD up pays what the baseline's cell never paid: its
	// cost is shown, not judged (L548, the owner's rule; plain: L554-L559).
	informational := map[string]string{
		"ldd-ask": "; informational: ldd-ask sets LDD up, which the baseline's ldd never pays (L548)",
		"plain":   "; informational: plain now sets LDD up (L554-L559), which the baseline's plain never paid (L548)",
	}[c.Variant]
	if !setsUpLDD(c.Condition, c.Variant) {
		informational = ""
	}
	// Cost is judged by the geometric mean over the cells (L567,
	// verdicts); a cell shows its ratio, is flagged over ReviewRatio, and
	// fails only when its cost is unknown.
	cost := func(metric string, cv float64, cok bool, bv float64, bok bool, unit, note string) {
		if informational == "" && (!cok || !bok || cv <= 0 || bv <= 0) {
			side := map[bool]string{true: "the baseline", false: "the candidate"}[cok && cv > 0]
			add(metric, false, "%s unknown in %s (candidate %s%s, baseline %s%s)", metric, side, num2(cv), unit, num2(bv), unit)
			return
		}
		r := cv / math.Max(bv, 1e-9)
		add(metric, true, "%s%s vs baseline %s%s (%s×)%s", num2(cv), unit, num2(bv), unit, num2(r), note)
		if informational != "" {
			out[len(out)-1].Detail += informational
			return
		}
		out[len(out)-1].ratio = r
		if r > ReviewRatio {
			out[len(out)-1].Review = true
			out[len(out)-1].Detail += fmt.Sprintf("; review: > %g × the baseline", ReviewRatio)
		}
	}
	medianOf := func(r Row, k string) (float64, bool) { v, ok := r.Medians[k]; return v, ok }
	cw, cnote := weighted(c)
	bw, bnote := weighted(b)
	cost("quota-weighted tokens", cw, weightedKnown(c), bw, weightedKnown(b), "", cmpOr(cnote, bnote))
	ct, ctok := medianOf(c, "turns")
	bt, btok := medianOf(b, "turns")
	cost("turns", ct, ctok, bt, btok, "", "")
	ca, caok := medianOf(c, "active_ms")
	ba, baok := medianOf(b, "active_ms")
	cost("active time", ca/1000, caok, ba/1000, baok, " s", "")
	return out
}

// newVariants are the variants a baseline from before T60 lacks, judged
// against its ldd variant for cost and on the absolute quality rules.
var newVariants = []string{"ldd-native", "ldd-ask"}

// Certify judges candidate against baseline, cell by cell.
func Certify(baseline, candidate map[[3]string]Row) (checks []CertCheck, notes []string) {
	// The no-Jhon conditions measure the model, not Jhon: `bench value`
	// compares them, certify leaves them out of every metric (L575).
	baseline, baseSkipped := jhonCells(baseline)
	candidate, candSkipped := jhonCells(candidate)
	if baseSkipped+candSkipped > 0 {
		notes = append(notes, fmt.Sprintf("skipped %d clean and pi-clean runs (candidate %d, baseline %d): they are not certification cells, `bench value` compares them", baseSkipped+candSkipped, candSkipped, baseSkipped))
	}
	keys := slices.Collect(func(yield func([3]string) bool) {
		for k := range candidate {
			if !yield(k) {
				return
			}
		}
	})
	for k := range baseline {
		if _, ok := candidate[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a, b [3]string) int {
		return cmpKeys(a, b)
	})
	// The hidden-check rate per task and condition: the baseline's every
	// variant, and the candidate's new variants (L545).
	rates := map[[2]string]*taskRate{}
	rateOf := func(task, cond string) *taskRate {
		k := [2]string{task, cond}
		if rates[k] == nil {
			rates[k] = &taskRate{}
		}
		return rates[k]
	}
	for k, b := range baseline {
		r := rateOf(k[0], k[2])
		r.basePassed, r.baseRuns = r.basePassed+b.Passed, r.baseRuns+b.Runs
	}
	for k, c := range candidate {
		if _, inBase := baseline[k]; !inBase && slices.Contains(newVariants, k[1]) {
			r := rateOf(k[0], k[2])
			r.candPassed, r.candRuns = r.candPassed+c.Passed, r.candRuns+c.Runs
		}
	}
	lacking, jhonPlain := map[string]bool{}, false
	for _, k := range keys {
		_, inCand := candidate[k]
		jhonPlain = jhonPlain || inCand && setsUpLDD(k[2], k[1]) && k[1] == "plain"
	}
	if jhonPlain {
		notes = append(notes, "plain now sets LDD up (L554-L559): a Jhon plain cell must end with ldd/ and its record, its quality is compared with the baseline's plain cell (which worked without LDD), and its cost is shown, not judged (L548)")
	}
	for _, k := range keys {
		c, inCand := candidate[k]
		b, inBase := baseline[k]
		switch {
		case !inCand:
			checks = append(checks, CertCheck{Metric: "repeats", Task: k[0], Variant: k[1], Condition: k[2], Detail: "missing in the candidate"})
			continue
		case inBase:
			checks = append(checks, judge(c, b, false)...)
			continue
		}
		ldd, ok := baseline[[3]string{k[0], "ldd", k[2]}]
		if !slices.Contains(newVariants, k[1]) || !ok {
			checks = append(checks, CertCheck{Metric: "repeats", Task: k[0], Variant: k[1], Condition: k[2], Detail: "no baseline cell"})
			continue
		}
		lacking[k[1]] = true
		checks = append(checks, judge(c, ldd, true, *rateOf(k[0], k[2]))...)
	}
	for _, v := range newVariants {
		if lacking[v] {
			notes = append(notes, fmt.Sprintf("the baseline has no %s: its cells are compared with the baseline's ldd of the same task and condition for cost, and judged on the absolute quality rules (the hidden check passes at least as often, pooled over the new variants of the task and condition, as the baseline's runs of that task and condition; every slice holds, no refusal; RED before GREEN, tests before the end and out of scope count in the pooled discipline against the baseline's ldd runs)", v))
		}
	}
	return checks, notes
}

// jhonCells is side without its clean and pi-clean cells, and how many runs
// those held.
func jhonCells(side map[[3]string]Row) (map[[3]string]Row, int) {
	out, skipped := map[[3]string]Row{}, 0
	for k, r := range side {
		if isClean(k[2]) {
			skipped += r.Runs
			continue
		}
		out[k] = r
	}
	return out, skipped
}

func cmpKeys(a, b [3]string) int {
	if a[0] != b[0] {
		return strings.Compare(a[0], b[0])
	}
	if ra, rb := rank(AllVariants, a[1]), rank(AllVariants, b[1]); ra != rb {
		return ra - rb
	}
	if ra, rb := rank(KnownConditions, condOf(a[2])), rank(KnownConditions, condOf(b[2])); ra != rb {
		return ra - rb
	}
	return strings.Compare(a[2], b[2])
}

// cellName is k as "task variant condition[ model]".
func cellName(k [3]string) string {
	name := k[0] + " " + k[1] + " " + condOf(k[2])
	if m := modelOf(k[2]); m != "" {
		name += " " + m
	}
	return name
}

// alignCells re-keys both sides by task × variant × condition, so certify
// compares one cell with the same cell of the other side. A side may hold
// one model per cell; a cell whose model differs between the sides is an
// error unless allow, then a note (T77). A row with no model (an old
// report.json) matches any.
//
// A cell whose project settings differ between the sides (T86) is refused
// the same way unless allowSettings, then noted.
func alignCells(base, cand map[[3]string]Row, allow, allowSettings bool) (b, c map[[3]string]Row, notes []string, err error) {
	rekey := func(side map[[3]string]Row, name string) (map[[3]string]Row, error) {
		out := map[[3]string]Row{}
		for k, r := range side {
			plain := [3]string{k[0], k[1], condOf(k[2])}
			if prev, dup := out[plain]; dup {
				return nil, fmt.Errorf("%s: the %s holds models %s and %s; give one model's runs per side", strings.Join(plain[:], " "), name, cmpOr(identity(prev.Model, prev.Profile, prev.ProjectSettings), "unknown"), cmpOr(identity(r.Model, r.Profile, r.ProjectSettings), "unknown"))
			}
			out[plain] = r
		}
		return out, nil
	}
	if b, err = rekey(base, "baseline"); err != nil {
		return nil, nil, nil, err
	}
	if c, err = rekey(cand, "candidate"); err != nil {
		return nil, nil, nil, err
	}
	keys := slices.SortedFunc(func(yield func([3]string) bool) {
		for k := range c {
			if !yield(k) {
				return
			}
		}
	}, cmpKeys)
	for _, k := range keys {
		br, ok := b[k]
		// A profile is part of the model (T77b).
		if !ok {
			continue
		}
		if cs, bs := strings.Join(c[k].ProjectSettings, ","), strings.Join(br.ProjectSettings, ","); cs != bs {
			if !allowSettings {
				return nil, nil, nil, fmt.Errorf("%s: the baseline ran project settings %s, the candidate %s; certify compares cells with the same project settings (--allow-setting-change to compare them anyway)", strings.Join(k[:], " "), cmpOr(bs, "none"), cmpOr(cs, "none"))
			}
			notes = append(notes, fmt.Sprintf("the project settings changed: %s %s → %s", strings.Join(k[:], " "), cmpOr(bs, "none"), cmpOr(cs, "none")))
		}
		ci, bi := ident(c[k].Model, c[k].Profile), ident(br.Model, br.Profile)
		cm, bm := normModel(ci), normModel(bi)
		if cm == "" || bm == "" || cm == bm {
			continue
		}
		if !allow {
			return nil, nil, nil, fmt.Errorf("%s: the baseline ran %s, the candidate %s; certify compares a model with itself (--allow-model-change to compare them anyway)", strings.Join(k[:], " "), bi, ci)
		}
		notes = append(notes, fmt.Sprintf("the model changed: %s %s → %s", strings.Join(k[:], " "), bi, ci))
	}
	return b, c, notes, nil
}

// verdicts are checks and gates by metric, and the overall verdict.
func verdicts(checks []CertCheck, gates []GateVerdict) ([]MetricVerdict, string) {
	var out []MetricVerdict
	pass := true
	for _, metric := range certMetrics {
		v := MetricVerdict{Metric: metric, Pass: true}
		for _, c := range checks {
			if c.Metric == metric {
				v.Of++
				if !c.Pass {
					v.Failed++
					v.Pass = false
				}
			}
		}
		switch {
		case v.Of == 0:
		case slices.Contains(costMetrics, metric):
			gm, n, review := geomean(checks, metric)
			v.Detail = fmt.Sprintf("geometric mean %s over %d cells, limit %g × (%d informational, %d review)", num2(gm), n, CostMargin, v.Of-v.Failed-n, review)
			if n == 0 {
				v.Detail = fmt.Sprintf("no judged cells (%d informational)", v.Of-v.Failed)
			}
			v.Pass = v.Pass && gm <= CostMargin
		case slices.Contains(pooledMetrics, metric):
			var t [4]int
			for _, c := range checks {
				if c.Metric == metric && c.pool != nil {
					for i := range t {
						t[i] += c.pool[i]
					}
				}
			}
			p := fisherLower(t[0], t[1], t[2], t[3])
			if metric == "out of scope" {
				v.Detail = fmt.Sprintf("pooled %d/%d runs with files out of scope vs baseline %d/%d", t[1]-t[0], t[1], t[3]-t[2], t[3])
			} else {
				v.Detail = fmt.Sprintf("pooled %d/%d vs baseline %d/%d", t[0], t[1], t[2], t[3])
			}
			ps := "p=" + strconv.FormatFloat(math.Round(p*1000)/1000, 'f', -1, 64)
			if p < 0.001 {
				ps = "p<0.001"
			}
			v.Detail += fmt.Sprintf(", one-sided Fisher %s (FAIL below %g)", ps, FisherAlpha)
			v.Pass = v.Pass && p >= FisherAlpha
		}
		if v.Of > 0 {
			out = append(out, v)
			pass = pass && v.Pass
		}
	}
	for _, g := range gates {
		out = append(out, MetricVerdict{Metric: "gate " + g.Name, Pass: g.Pass, Of: 1, Failed: map[bool]int{true: 0, false: 1}[g.Pass]})
		pass = pass && g.Pass
	}
	if len(out) == 0 {
		pass = false
	}
	return out, map[bool]string{true: "PASS", false: "FAIL"}[pass]
}

// geomean is the geometric mean of metric's judged cell ratios, how many
// there are, and how many are flagged for review; 1 when there are none.
func geomean(checks []CertCheck, metric string) (float64, int, int) {
	sum, n, review := 0.0, 0, 0
	for _, c := range checks {
		if c.Metric == metric && c.ratio > 0 {
			sum, n = sum+math.Log(c.ratio), n+1
			if c.Review {
				review++
			}
		}
	}
	if n == 0 {
		return 1, 0, 0
	}
	return math.Exp(sum / float64(n)), n, review
}

// printCertification writes the owner's reading: one line per metric,
// the failing cells under it, then the verdict.
func printCertification(w io.Writer, c Certification, file string) {
	if len(c.Candidate) > 0 {
		fmt.Fprintf(w, "certify: candidate %s against baseline %s\n", strings.Join(c.Candidate, ","), strings.Join(c.Baseline, ","))
	}
	for _, n := range c.Notes {
		fmt.Fprintf(w, "  note: %s\n", n)
	}
	if c.Replay != nil {
		printReplay(w, c.Replay)
	}
	for _, m := range c.Metrics {
		verdict := map[bool]string{true: "PASS", false: "FAIL"}[m.Pass]
		if g, ok := strings.CutPrefix(m.Metric, "gate "); ok {
			for _, gv := range c.Gates {
				if gv.Name == g {
					fmt.Fprintf(w, "%s  %-32s %s\n", verdict, m.Metric, gv.Command)
					if !gv.Pass {
						fmt.Fprintf(w, "        exit %d: %s\n", gv.Exit, strings.ReplaceAll(gv.Tail, "\n", "\n        "))
					}
				}
			}
			continue
		}
		if m.Detail != "" {
			fmt.Fprintf(w, "%s  %-32s %d/%d cells; %s\n", verdict, m.Metric, m.Of-m.Failed, m.Of, m.Detail)
		} else {
			fmt.Fprintf(w, "%s  %-32s %d/%d cells\n", verdict, m.Metric, m.Of-m.Failed, m.Of)
		}
		if gm, n, _ := geomean(c.Checks, m.Metric); n > 0 && gm > CostMargin {
			fmt.Fprintf(w, "        geometric mean %s > %g × the baseline\n", num2(gm), CostMargin)
		}
		for _, ch := range c.Checks {
			switch {
			case ch.Metric != m.Metric:
			case !ch.Pass || ch.worse:
				fmt.Fprintf(w, "        %s: %s\n", ch.cell(), ch.Detail)
			case ch.Review:
				fmt.Fprintf(w, "        review %s: %s\n", ch.cell(), ch.Detail)
			}
		}
	}
	failed := 0
	for _, m := range c.Metrics {
		if !m.Pass {
			failed++
		}
	}
	if c.Verdict == "PASS" {
		fmt.Fprintf(w, "verdict: PASS — %d metrics hold\n", len(c.Metrics))
	} else {
		fmt.Fprintf(w, "verdict: FAIL — %d of %d metrics failed\n", failed, len(c.Metrics))
	}
	fmt.Fprintf(w, "certify.json: %s\n", file)
}

// splitDirs are the comma-separated dirs of a repeated flag.
type splitDirs []string

func (s *splitDirs) String() string { return strings.Join(*s, ",") }
func (s *splitDirs) Set(v string) error {
	*s = append(*s, split(v)...)
	return nil
}

// latestResults is the newest results directory under root not in skip:
// the default candidate.
func latestResults(root string, skip []string) string {
	entries, _ := os.ReadDir(root)
	for i := len(entries) - 1; i >= 0; i-- {
		d := filepath.Join(root, entries[i].Name())
		if entries[i].IsDir() && !slices.ContainsFunc(skip, func(s string) bool { return filepath.Clean(s) == d }) {
			return d
		}
	}
	return ""
}

// installedCommit is the first 8 characters of the commit of the jhon on
// PATH (`jhon version --json`, read only), or a placeholder.
func installedCommit() string {
	b, err := exec.Command("jhon", "version", "--json").Output()
	var v jhonVersion
	if err != nil || json.Unmarshal(b, &v) != nil || len(v.Commit) < 8 {
		return "<installed commit>"
	}
	return v.Commit[:8]
}

// headOf is the first 8 characters of checkout's HEAD, or a placeholder.
func headOf(checkout string) string {
	b, err := exec.Command("git", "-C", checkout, "rev-parse", "--short=8", "HEAD").Output()
	if err != nil {
		return "<candidate commit>"
	}
	return strings.TrimSpace(string(b))
}

// outOf is where certify.json goes: --out, else the first candidate dir,
// else a new dir under the bench's certify/.
func outOf(out string, cand []string) string {
	if out == "" && len(cand) > 0 {
		out = cand[0]
	}
	if out == "" {
		out = filepath.Join(root(), "certify", time.Now().UTC().Format("20060102T150405Z"))
	}
	return out
}

// certifyCLI is `bench certify`; runner runs the gates (the real one when nil).
func certifyCLI(args []string, out, errOut io.Writer, runner GateRunner) int {
	fs := flag.NewFlagSet("bench certify", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var base, cand splitDirs
	fs.Var(&base, "baseline", "the baseline's results dirs (runs/ or report.json), comma-separated or repeated")
	fs.Var(&cand, "candidate", "the candidate's results dirs (default: the newest under --results-root)")
	asJSON := fs.Bool("json", false, "print certify.json instead of the lines")
	plan := fs.Bool("plan", false, "print the certification's run matrix and its estimate; run nothing")
	gates := fs.String("gates", "", "the Jhon checkout whose deterministic gates run once")
	outDir := fs.String("out", "", "where certify.json goes (default: the first candidate dir)")
	resultsRoot := fs.String("results-root", filepath.Join(root(), "results"), "the bench's results, read only")
	tasksDir := fs.String("tasks-dir", filepath.Join(source(), "tasks"), "where the tasks are")
	baseRef := fs.String("baseline-ref", "", "--plan: the baseline's commit (default: the installed jhon's)")
	allowModel := fs.Bool("allow-model-change", false, "compare cells whose model differs between the baseline and the candidate (noted), in place of refusing")
	allowSettings := fs.Bool("allow-setting-change", false, "compare cells whose project settings (bench run --project-setting) differ between the baseline and the candidate (noted), in place of refusing")
	replayGuard := fs.String("replay-guard", "", "the reference build's jhon: each refusal and declined dialog it makes too is excluded and listed as baseline-shared")
	candRef := fs.String("candidate-ref", "", "--plan: the candidate's commit (default: the checkout's HEAD)")
	if fs.Parse(args) != nil {
		return 2
	}
	if *plan {
		if *baseRef == "" {
			*baseRef = installedCommit()
		}
		if *candRef == "" {
			*candRef = headOf(filepath.Join(source(), "..", ".."))
		}
		if err := CertifyPlan(*tasksDir, *resultsRoot, *baseRef, *candRef, out); err != nil {
			fmt.Fprintf(errOut, "bench certify: %v\n", err)
			return 1
		}
		return 0
	}
	if *replayGuard != "" && len(base) == 0 {
		fmt.Fprint(errOut, "bench certify: --replay-guard needs --baseline\n")
		return 2
	}
	if len(base) == 0 && *gates == "" {
		fmt.Fprint(errOut, "bench certify: name --baseline <results dirs> (and --candidate), or --gates <checkout>, or --plan\n")
		return 2
	}
	c := Certification{Baseline: []string(base), Notes: []string{}, Checks: []CertCheck{}, Gates: []GateVerdict{}}
	if len(base) > 0 {
		if len(cand) == 0 {
			if latest := latestResults(*resultsRoot, base); latest != "" {
				cand = splitDirs{latest}
			} else {
				fmt.Fprintf(errOut, "bench certify: no --candidate and no results under %s\n", *resultsRoot)
				return 2
			}
		}
		c.Candidate = []string(cand)
		var rp *replayer
		if *replayGuard != "" {
			r, err := newReplayer(*replayGuard, outOf(*outDir, cand))
			if err != nil {
				fmt.Fprintf(errOut, "bench certify: %v\n", err)
				return 2
			}
			rp = r
		}
		b, baseNotes, err := loadSideNotes(base, rp, "baseline")
		if err != nil {
			fmt.Fprintf(errOut, "bench certify: baseline: %v\n", err)
			return 2
		}
		cd, candNotes, err := loadSideNotes(cand, rp, "candidate")
		if err != nil {
			fmt.Fprintf(errOut, "bench certify: candidate: %v\n", err)
			return 2
		}
		b, cd, modelNotes, err := alignCells(b, cd, *allowModel, *allowSettings)
		if err != nil {
			fmt.Fprintf(errOut, "bench certify: %v\n", err)
			return 2
		}
		c.Checks, c.Notes = Certify(b, cd)
		c.Notes = slices.Concat(modelNotes, c.Notes, baseNotes, candNotes)
		if c.Notes == nil {
			c.Notes = []string{}
		}
		if rp != nil {
			c.Notes, c.Replay = append(c.Notes, rp.notes...), rp.rep
		}
	}
	if *gates != "" {
		if runner == nil {
			runner = execGates{}
		}
		c.Gates = runGates(*gates, runner)
	}
	c.Metrics, c.Verdict = verdicts(c.Checks, c.Gates)
	dir := outOf(*outDir, cand)
	file := filepath.Join(dir, "certify.json")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(errOut, "bench certify: %v\n", err)
		return 2
	}
	if err := writeJSON(file, c); err != nil {
		fmt.Fprintf(errOut, "bench certify: %v\n", err)
		return 2
	}
	if *asJSON {
		b, _ := jsonIndent(c)
		_, _ = out.Write(b)
	} else {
		printCertification(out, c, file)
	}
	if c.Verdict != "PASS" {
		return 1
	}
	return 0
}
