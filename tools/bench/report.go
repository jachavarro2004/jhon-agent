package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
)

// Row is one task × variant × condition of a report: its runs, how many
// passed the hidden test, and the median of each measure over the runs that
// have it.
type Row struct {
	Task      string `json:"task"`
	Variant   string `json:"variant"`
	Condition string `json:"condition"`
	// Model is the runs' model (pi: the effective provider/model, T77); a
	// cell holds one model's runs only.
	Model string `json:"model,omitempty"`
	// Profile is the runs' Jhon profile (T77b): part of the cell's model
	// identity, so two profiles never share a cell.
	Profile string `json:"profile,omitempty"`
	// ProjectSettings are the runs' project settings (T86): part of the
	// cell's identity too.
	ProjectSettings []string           `json:"project_settings,omitempty"`
	Runs            int                `json:"runs"`
	Passed          int                `json:"passed"`
	Medians         map[string]float64 `json:"medians"`
	Process         Process            `json:"process"`
	Quality         Quality            `json:"quality"`
	// MessageMedians are the medians of each owner message's measures, by
	// message (index 0 is message 1), over the runs that reached it.
	MessageMedians []map[string]float64 `json:"message_medians,omitempty"`
}

// Quality is a row's results beyond the final hidden check (T46): the
// per-message checks run after the last message, summed over the runs;
// regressions summed over the runs; rework summed over the runs that know
// it (nil when none does).
type Quality struct {
	FinalChecks Rate `json:"final_checks"`
	Regressions int  `json:"regressions"`
	Rework      *int `json:"rework"`
	// Ceremony is the plain runs' ceremony summed (nil when no run has
	// one), recorded only; Ldd the layouts and records of the LDD runs and
	// of the Jhon plain runs, which set LDD up (T60, L554-L559).
	Ceremony *Ceremony   `json:"ceremony,omitempty"`
	Ldd      *LddSummary `json:"ldd,omitempty"`
}

// LddSummary is a row's LDD runs: how many ended in each layout, and how
// many kept the record the route asks for (ceremony.go), of the runs that
// know it; Unknown runs could not tell.
type LddSummary struct {
	Layouts map[string]int `json:"layouts"`
	Records int            `json:"records"`
	Known   int            `json:"known"`
	Unknown int            `json:"unknown"`
}

// String is "layout:n …  (record r/k)".
func (l *LddSummary) String() string {
	if l == nil {
		return "—"
	}
	var parts []string
	for _, k := range slices.Sorted(maps.Keys(l.Layouts)) {
		parts = append(parts, fmt.Sprintf("%s:%d", k, l.Layouts[k]))
	}
	if l.Unknown > 0 {
		return fmt.Sprintf("%s (record %d/%d, %d unknown)", strings.Join(parts, " "), l.Records, l.Known+l.Unknown, l.Unknown)
	}
	return fmt.Sprintf("%s (record %d/%d)", strings.Join(parts, " "), l.Records, l.Known)
}

// quality is the Quality of ms.
func quality(ms []Metrics) Quality {
	var q Quality
	for _, m := range ms {
		fc := finalChecks(m.ChecksPerMessage)
		q.FinalChecks.Yes, q.FinalChecks.Known = q.FinalChecks.Yes+fc.Yes, q.FinalChecks.Known+fc.Known
		if m.Regressions != nil {
			q.Regressions += *m.Regressions
		}
		if m.Rework != nil {
			q.Rework = ip(deref(q.Rework) + *m.Rework)
		}
		if m.Ceremony != nil {
			if q.Ceremony == nil {
				q.Ceremony = &Ceremony{Layouts: []string{}}
			}
			q.Ceremony.add(*m.Ceremony)
		}
		if l := m.LddLayout; l != nil {
			if q.Ldd == nil {
				q.Ldd = &LddSummary{Layouts: map[string]int{}}
			}
			q.Ldd.Layouts[l.Layout]++
			switch {
			case l.Record == nil:
				q.Ldd.Unknown++
			case *l.Record:
				q.Ldd.Known++
				q.Ldd.Records++
			default:
				q.Ldd.Known++
			}
		}
	}
	return q
}

func ip(v int) *int { return &v }

func deref(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

// messageMeasures are the per-message columns.
var messageMeasures = []struct {
	name string
	of   func(m MessageMetrics) float64
}{
	{"turns", func(m MessageMetrics) float64 { return float64(m.Turns) }},
	{"tokens_processed", func(m MessageMetrics) float64 {
		return m.InputTokens + m.OutputTokens + m.CacheReadTokens + m.CacheWriteTokens
	}},
	{"cache_write_tokens", func(m MessageMetrics) float64 { return m.CacheWriteTokens }},
	{"wall_ms", func(m MessageMetrics) float64 { return m.WallMs }},
}

// messageMedians are the medians of ms's messages, by message.
func messageMedians(ms []Metrics) []map[string]float64 {
	var out []map[string]float64
	for i := 0; ; i++ {
		var reached []MessageMetrics
		for _, m := range ms {
			if i < len(m.Messages) {
				reached = append(reached, m.Messages[i])
			}
		}
		if len(reached) == 0 {
			return out
		}
		med := map[string]float64{}
		for _, x := range messageMeasures {
			var vs []float64
			for _, mm := range reached {
				vs = append(vs, x.of(mm))
			}
			med[x.name] = median(vs)
		}
		out = append(out, med)
	}
}

// ConditionSummary is one condition × variant over every task (L437): its
// results, to compare with clean, and its methodology fidelity, to compare
// jhon with pi-jhon. Rates and counts are summed over the runs.
type ConditionSummary struct {
	Condition         string   `json:"condition"`
	Model             string   `json:"model,omitempty"`
	Profile           string   `json:"profile,omitempty"`
	ProjectSettings   []string `json:"project_settings,omitempty"`
	Variant           string   `json:"variant"`
	Runs              int      `json:"runs"`
	Passed            Rate     `json:"passed"`
	FinalChecks       Rate     `json:"final_checks"`
	Regressions       int      `json:"regressions"`
	TestChanged       Rate     `json:"test_changed"`
	RedBeforeGreen    Rate     `json:"red_before_green"`
	TestsRunBeforeEnd Rate     `json:"tests_run_before_end"`
	OutOfScopeFiles   int      `json:"out_of_scope_files"`
	Findings          int      `json:"findings"`
	Rework            int      `json:"rework"`
}

// summaries are groups summed per condition × variant, in report order.
func summaries(groups map[[3]string][]Metrics) []ConditionSummary {
	by := map[[2]string]*ConditionSummary{}
	for k, ms := range groups {
		key := [2]string{k[2], k[1]}
		s := by[key]
		if s == nil {
			s = &ConditionSummary{Condition: condOf(k[2]), Model: groups[k][0].Model, Profile: groups[k][0].Profile, ProjectSettings: groups[k][0].ProjectSettings, Variant: k[1]}
			by[key] = s
		}
		for _, m := range ms {
			s.Runs++
			s.Passed.add(&m.Success)
			fc := finalChecks(m.ChecksPerMessage)
			s.FinalChecks.Yes, s.FinalChecks.Known = s.FinalChecks.Yes+fc.Yes, s.FinalChecks.Known+fc.Known
			s.Regressions += deref(m.Regressions)
			s.TestChanged.add(m.TestChanged)
			s.RedBeforeGreen.add(m.RedBeforeGreen)
			s.TestsRunBeforeEnd.add(m.TestsRunBeforeEnd)
			s.OutOfScopeFiles += deref(m.OutOfScopeFiles)
			s.Rework += deref(m.Rework)
			for _, n := range m.Findings {
				s.Findings += n
			}
		}
	}
	var out []ConditionSummary
	for _, s := range by {
		out = append(out, *s)
	}
	slices.SortFunc(out, func(a, b ConditionSummary) int {
		return cmp.Or(cmp.Compare(rank(KnownConditions, a.Condition), rank(KnownConditions, b.Condition)), cmp.Compare(a.Condition, b.Condition), cmp.Compare(a.Model, b.Model), cmp.Compare(a.Profile, b.Profile),
			cmp.Compare(rank(AllVariants, a.Variant), rank(AllVariants, b.Variant)), cmp.Compare(a.Variant, b.Variant))
	})
	return out
}

// Rate is how many runs a process fact holds for, of the runs that know it.
type Rate struct {
	Yes   int `json:"yes"`
	Known int `json:"known"`
}

func (r *Rate) add(v *bool) {
	if v != nil {
		r.Known++
		if *v {
			r.Yes++
		}
	}
}

func (r Rate) String() string {
	if r.Known == 0 {
		return "—"
	}
	return fmt.Sprintf("%d/%d", r.Yes, r.Known)
}

// Process is a row's process quality (T41): the rates of its runs' process
// facts, the median of out_of_scope_files over the runs that know it (nil
// when none does), and findings by check summed over the runs.
type Process struct {
	TestChanged       Rate           `json:"test_changed"`
	TestsRunBeforeEnd Rate           `json:"tests_run_before_end"`
	RedBeforeGreen    Rate           `json:"red_before_green"`
	OutOfScopeFiles   *float64       `json:"out_of_scope_files_median"`
	FindingsByCheck   map[string]int `json:"findings_by_check"`
	// UIRequestsDeclined are the pi dialogs the driver answered as
	// cancelled, summed over the runs (T47).
	UIRequestsDeclined int `json:"ui_requests_declined"`
	// Refusals and OutOfScopeTotal are summed over the runs that know them
	// (T60); nil when none does.
	Refusals        *int `json:"refusals"`
	OutOfScopeTotal *int `json:"out_of_scope_files_total"`
	// OutOfScopeRuns are the runs with at least one out-of-scope file, of
	// the runs that know it (T73); nil in a report from before.
	OutOfScopeRuns *Rate `json:"out_of_scope_runs,omitempty"`
}

// process is the Process of ms.
func process(ms []Metrics) Process {
	p := Process{FindingsByCheck: map[string]int{}}
	var out []float64
	for _, m := range ms {
		p.TestChanged.add(m.TestChanged)
		p.TestsRunBeforeEnd.add(m.TestsRunBeforeEnd)
		p.RedBeforeGreen.add(m.RedBeforeGreen)
		if m.OutOfScopeFiles != nil {
			out = append(out, float64(*m.OutOfScopeFiles))
			p.OutOfScopeTotal = ip(deref(p.OutOfScopeTotal) + *m.OutOfScopeFiles)
			if p.OutOfScopeRuns == nil {
				p.OutOfScopeRuns = &Rate{}
			}
			some := *m.OutOfScopeFiles > 0
			p.OutOfScopeRuns.add(&some)
		}
		if m.Refusals != nil {
			p.Refusals = ip(deref(p.Refusals) + *m.Refusals)
		}
		for c, n := range m.Findings {
			p.FindingsByCheck[c] += n
		}
		p.UIRequestsDeclined += m.UIRequestsDeclined
	}
	if len(out) > 0 {
		p.OutOfScopeFiles = f(median(out))
	}
	return p
}

// byCheck is findings as "check:n", sorted by check; "0" when none.
func byCheck(findings map[string]int) string {
	var parts []string
	for _, c := range slices.Sorted(maps.Keys(findings)) {
		parts = append(parts, fmt.Sprintf("%s:%d", c, findings[c]))
	}
	if len(parts) == 0 {
		return "0"
	}
	return strings.Join(parts, " ")
}

// measures are the report's columns, in order, each read from one run.
var measures = []struct {
	name string
	of   func(m Metrics) *float64
}{
	{"turns", func(m Metrics) *float64 { return f(float64(m.Turns)) }},
	{"input_tokens", func(m Metrics) *float64 { return f(m.InputTokens) }},
	{"output_tokens", func(m Metrics) *float64 { return f(m.OutputTokens) }},
	{"cache_read_tokens", func(m Metrics) *float64 { return f(m.CacheReadTokens) }},
	{"cache_write_tokens", func(m Metrics) *float64 { return f(m.CacheWriteTokens) }},
	{"tokens_processed", func(m Metrics) *float64 { return f(tokensProcessed(m)) }},
	{"cache_hit", func(m Metrics) *float64 { return m.CacheHit }},
	{"active_ms", func(m Metrics) *float64 { return f(m.ActiveMs) }},
	{"elapsed_ms", func(m Metrics) *float64 { return f(m.ElapsedMs) }},
	{"wall_ms", func(m Metrics) *float64 { return f(m.WallMs) }},
	{"agents_launched", func(m Metrics) *float64 { return f(float64(m.AgentsLaunched)) }},
	{"verify_runs", func(m Metrics) *float64 { return f(float64(m.VerifyRuns)) }},
	{"findings", func(m Metrics) *float64 {
		n := 0
		for _, c := range m.Findings {
			n += c
		}
		return f(float64(n))
	}},
	{"quota_points", func(m Metrics) *float64 { return m.QuotaPoints }},
	{"quota_weighted", func(m Metrics) *float64 { return f(quotaWeighted(m)) }},
	{"slices", func(m Metrics) *float64 { return m.Slices }},
	{"refusals", func(m Metrics) *float64 {
		if m.Refusals == nil {
			return nil
		}
		return f(float64(*m.Refusals))
	}},
	{"rework", func(m Metrics) *float64 {
		if m.Rework == nil {
			return nil
		}
		return f(float64(*m.Rework))
	}},
}

func f(v float64) *float64 { return &v }

// tokensProcessed is every token a run's model read or wrote: cache read,
// cache write, input and output.
func tokensProcessed(m Metrics) float64 {
	return m.CacheReadTokens + m.CacheWriteTokens + m.InputTokens + m.OutputTokens
}

// quotaWeighted are a run's tokens weighted as the weekly quota counts them
// (L432): input + 1.25 × cache write + 0.1 × cache read + 5 × output.
func quotaWeighted(m Metrics) float64 {
	return m.InputTokens + 1.25*m.CacheWriteTokens + 0.1*m.CacheReadTokens + 5*m.OutputTokens
}

// Overhead is one task × variant × condition against the task's clean plain
// row: its median tokens processed and active time as ratios to clean's, to
// two decimals; nil when either median is missing or clean's is zero.
type Overhead struct {
	Task      string   `json:"task"`
	Variant   string   `json:"variant"`
	Condition string   `json:"condition"`
	Tokens    *float64 `json:"tokens_processed_ratio"`
	Active    *float64 `json:"active_ratio"`
}

// ratio is a / b to two decimals, nil when a or b is missing or b is zero.
func ratio(a, b map[string]float64, key string) *float64 {
	x, okA := a[key]
	y, okB := b[key]
	if !okA || !okB || y == 0 {
		return nil
	}
	return f(math.Round(x/y*100) / 100)
}

// overheads are rows against their task's clean plain row of the same
// model, in row order.
func overheads(rows []Row) []Overhead {
	clean := map[string]Row{}
	for _, r := range rows {
		if r.Condition == "clean" && r.Variant == "plain" {
			clean[r.Task+"\x00"+normModel(r.Model)] = r
		}
	}
	out := []Overhead{}
	for _, r := range rows {
		c, ok := clean[r.Task+"\x00"+normModel(r.Model)]
		if !ok || r.Condition == "clean" && r.Variant == "plain" {
			continue
		}
		out = append(out, Overhead{Task: r.Task, Variant: r.Variant, Condition: r.Condition,
			Tokens: ratio(r.Medians, c.Medians, "tokens_processed"), Active: ratio(r.Medians, c.Medians, "active_ms")})
	}
	return out
}

func times(v *float64) string {
	if v == nil {
		return "—"
	}
	return strconv.FormatFloat(*v, 'f', -1, 64) + "×"
}

// median of xs, which is not empty.
func median(xs []float64) float64 {
	s := slices.Clone(xs)
	slices.Sort(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// rank orders x as all does, others last.
func rank(all []string, x string) int {
	if i := slices.Index(all, x); i >= 0 {
		return i
	}
	return len(all)
}

// cellKey is a cell's condition key: the condition and its model, so runs
// of two models never share a cell (T77). claude-bridge/X is model X.
func cellKey(cond, model string) string {
	if m := normModel(model); m != "" {
		return cond + "\x00" + m
	}
	return cond
}

// normModel is model with pi's default provider dropped: pi on
// claude-bridge runs the same model as Claude Code.
func normModel(model string) string { return strings.TrimPrefix(model, BridgeProvider+"/") }

// condOf is a cell key's condition; modelOf its normalized model.
func condOf(k string) string  { c, _, _ := strings.Cut(k, "\x00"); return c }
func modelOf(k string) string { _, m, _ := strings.Cut(k, "\x00"); return m }

// label is r's condition, with its model when the rows hold several.
func label(r Row, many bool) string {
	if many && r.Model != "" {
		return r.Condition + " " + identity(r.Model, r.Profile, r.ProjectSettings)
	}
	return r.Condition
}

// manyModels is whether rows hold more than one model.
func manyModels(rows []Row) bool {
	seen := map[string]bool{}
	for _, r := range rows {
		seen[normModel(identity(r.Model, r.Profile, r.ProjectSettings))] = true
	}
	return len(seen) > 1
}

// groupRuns are the runs/*/metrics.json under dir, by task × variant ×
// condition and model; a run from before variants is plain.
func groupRuns(dir string) (map[[3]string][]Metrics, error) { return groupRunsWith(dir, nil) }

// groupRunsWith is groupRuns with each run's metrics passed through each
// (its run directory and metrics) first, when each is not nil.
func groupRunsWith(dir string, each func(runDir string, m *Metrics)) (map[[3]string][]Metrics, error) {
	files, _ := filepath.Glob(filepath.Join(dir, "runs", "*", "metrics.json"))
	if len(files) == 0 {
		return nil, fmt.Errorf("no runs under %s (runs/*/metrics.json)", dir)
	}
	groups := map[[3]string][]Metrics{}
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var m Metrics
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("%s: %v", file, err)
		}
		if each != nil {
			each(filepath.Dir(file), &m)
		}
		k := [3]string{m.Task, cmpOr(m.Variant, "plain"), cellKey(m.Condition, identity(m.Model, m.Profile, m.ProjectSettings))}
		groups[k] = append(groups[k], m)
	}
	return groups, nil
}

// rowsOf are groups' rows, in report order.
func rowsOf(groups map[[3]string][]Metrics) []Row {
	var rows []Row
	for k, ms := range groups {
		r := Row{Task: k[0], Variant: k[1], Condition: condOf(k[2]), Model: ms[0].Model, Profile: ms[0].Profile, ProjectSettings: ms[0].ProjectSettings, Runs: len(ms), Medians: map[string]float64{}, Process: process(ms),
			Quality: quality(ms), MessageMedians: messageMedians(ms)}
		for _, m := range ms {
			if m.Success {
				r.Passed++
			}
		}
		for _, x := range measures {
			var vs []float64
			for _, m := range ms {
				if v := x.of(m); v != nil {
					vs = append(vs, *v)
				}
			}
			if len(vs) > 0 {
				r.Medians[x.name] = median(vs)
			}
		}
		rows = append(rows, r)
	}
	slices.SortFunc(rows, func(a, b Row) int {
		return cmp.Or(cmp.Compare(a.Task, b.Task), cmp.Compare(rank(AllVariants, a.Variant), rank(AllVariants, b.Variant)), cmp.Compare(a.Variant, b.Variant),
			cmp.Compare(rank(KnownConditions, a.Condition), rank(KnownConditions, b.Condition)), cmp.Compare(a.Condition, b.Condition), cmp.Compare(a.Model, b.Model), cmp.Compare(a.Profile, b.Profile))
	})
	return rows
}

// Report reads every runs/*/metrics.json under dir, writes report.json
// there ({rows, by_condition, overhead}) and prints, quality first: the
// results per task × variant × condition, their methodology fidelity, both
// by condition over every task, then the token and time medians, the
// per-message medians of multi-message tasks and the overhead vs clean. A
// run from before variants is plain.
func Report(dir string, out io.Writer) error {
	groups, err := groupRuns(dir)
	if err != nil {
		return err
	}
	rows := rowsOf(groups)
	over, byCond := overheads(rows), summaries(groups)
	if err := writeJSON(filepath.Join(dir, "report.json"), map[string]any{"rows": rows, "by_condition": byCond, "overhead": over}); err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	many := manyModels(rows)
	if many {
		fmt.Fprint(w, "models: several in this dir; each cell is one condition on one model, shown after the condition\n\n")
	}
	// Quality first (L436): results, then methodology fidelity (L437), then
	// what they cost.
	fmt.Fprint(w, "results (quality first): the final hidden check, the per-message checks after the last message, regressions of an earlier message's check\n")
	fmt.Fprint(w, "task\tvariant\tcondition\tpassed\tfinal checks\tregressions\n")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d/%d\t%s\t%d\n", r.Task, r.Variant, label(r, many), r.Passed, r.Runs, r.Quality.FinalChecks, r.Quality.Regressions)
	}
	fmt.Fprint(w, "\nmethodology fidelity (runs where it holds / runs that know it; — unknown; regressions, rework, ceremony and pi dialogs declined summed)\n")
	fmt.Fprint(w, "task\tvariant\tcondition\ttest changed\tred before green\ttests run before end\tout of scope (median)\tfindings by check\tregressions\trework\tceremony (plain)\tldd layout\tdialogs declined\n")
	for _, r := range rows {
		p, oos := r.Process, "—"
		if p.OutOfScopeFiles != nil {
			oos = strconv.FormatFloat(*p.OutOfScopeFiles, 'f', -1, 64)
		}
		cer := "—"
		if r.Quality.Ceremony != nil {
			cer = r.Quality.Ceremony.String()
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\t%d\n", r.Task, r.Variant, label(r, many), p.TestChanged, p.RedBeforeGreen, p.TestsRunBeforeEnd,
			oos, byCheck(p.FindingsByCheck), r.Quality.Regressions, count(r.Quality.Rework), cer, r.Quality.Ldd, p.UIRequestsDeclined)
	}
	fmt.Fprint(w, "\nby condition, every task (results: each condition vs clean; fidelity: jhon vs pi-jhon)\n")
	fmt.Fprint(w, "condition\tvariant\truns\tpassed\tfinal checks\tregressions\tred before green\ttests run before end\ttest changed\tout of scope\tfindings\trework\n")
	for _, c := range byCond {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%d\t%s\t%s\t%s\t%d\t%d\t%d\n", label(Row{Condition: c.Condition, Model: c.Model, Profile: c.Profile, ProjectSettings: c.ProjectSettings}, many), c.Variant, c.Runs, c.Passed, c.FinalChecks, c.Regressions,
			c.RedBeforeGreen, c.TestsRunBeforeEnd, c.TestChanged, c.OutOfScopeFiles, c.Findings, c.Rework)
	}
	fmt.Fprint(w, "\ntokens and time (medians over each task × variant × condition's runs)\ntask\tvariant\tcondition\tpassed")
	for _, x := range measures {
		fmt.Fprint(w, "\t"+x.name)
	}
	fmt.Fprintln(w)
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d/%d", r.Task, r.Variant, label(r, many), r.Passed, r.Runs)
		for _, x := range measures {
			v, ok := r.Medians[x.name]
			cell := "—"
			if ok {
				cell = strconv.FormatFloat(v, 'f', -1, 64)
			}
			fmt.Fprint(w, "\t"+cell)
		}
		fmt.Fprintln(w)
	}
	if slices.ContainsFunc(rows, func(r Row) bool { return len(r.MessageMedians) > 1 }) {
		fmt.Fprint(w, "\nper message (medians over the runs that reached it)\ntask\tvariant\tcondition\tmessage")
		for _, x := range messageMeasures {
			fmt.Fprint(w, "\t"+x.name)
		}
		fmt.Fprintln(w)
		for _, r := range rows {
			for i, med := range r.MessageMedians {
				fmt.Fprintf(w, "%s\t%s\t%s\tmessage %d", r.Task, r.Variant, label(r, many), i+1)
				for _, x := range messageMeasures {
					fmt.Fprint(w, "\t"+strconv.FormatFloat(med[x.name], 'f', -1, 64))
				}
				fmt.Fprintln(w)
			}
		}
	}
	if len(over) > 0 {
		fmt.Fprint(w, "\noverhead vs clean (median ÷ the task's clean plain median)\ntask\tvariant\tcondition\ttokens processed\tactive time\n")
		for _, o := range over {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", o.Task, o.Variant, o.Condition, times(o.Tokens), times(o.Active))
		}
	}
	fmt.Fprintf(w, "\nreport.json in %s\n", dir)
	return w.Flush()
}

// count is n, or — when unknown.
func count(n *int) string {
	if n == nil {
		return "—"
	}
	return strconv.Itoa(*n)
}
