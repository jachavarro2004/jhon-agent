package main

import (
	"flag"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// `bench value` (T70) is Jhon's plus over not using it (L552): each Jhon
// cell against its no-Jhon counterpart of the same task (jhon vs clean,
// pi-jhon vs pi-clean). The owner's criterion: the same or better quality
// and better discipline. A cell PASSes when its hidden test passes at least
// as often as the clean one's, RED before GREEN and tests before the end
// happen more often (or in every run of both), its out-of-scope median is
// no higher, and it meets its task size's gates (valueGates, T146): for a
// large task cost and active time within the size's limit of the clean
// cell's and strictly better quality; for a small task its turns within the
// clean cell's plus the LDD minimum (T146i, L1285), with cost and time shown.
// Quota points, and a large task's turns, are shown, never failed. Fewer than 3 runs on either side, or a metric unknown on either,
// is FAIL (L483). The one-time set-up run (SetupTaskID) is no cell: it is
// reported on its own line (T146 FB2).

// SizeGate is a task size's limits against the no-Jhon cell.
type SizeGate struct {
	// Cost and Time are the most the Jhon cell's median weighted tokens (and
	// cost_usd, when both sides have it) and active time may be, as a
	// multiple of the clean cell's.
	Cost, Time float64
	// BetterQuality asks for strictly better quality: a higher hidden-test
	// pass rate, or, where both pass every run, fewer regressions and rework
	// per run.
	BetterQuality bool
	// Turns, when set, is the size's cost gate in place of Cost and Time:
	// the Jhon cell's median turns at most the clean cell's plus Turns;
	// weighted tokens, cost usd and active time are then shown, never failed.
	Turns int
}

// valueGates are the size gates, the one place they live. The owner's
// decision (L1205, T146): D2 large "≤ 1,25×" with strictly better quality.
// A small task's D1 "≤ 1,1×" gave way to the owner's choice (L1285, T146i):
// "Tope de turnos: ≤ clean + 6 (recomendada)": a small task passes cost
// when Jhon median turns <= clean median turns + 6, the LDD minimum
// (branch, test-first, RED run, GREEN run, commit, deliver check); weighted
// tokens, cost and active time become INFO for small tasks.
var valueGates = map[string]SizeGate{
	SizeSmall: {Turns: lddMinimumTurns},
	SizeLarge: {Cost: 1.25, Time: 1.25, BetterQuality: true},
}

// lddMinimumTurns are the turns LDD adds to a small task at the least:
// branch, test-first, RED run, GREEN run, commit, deliver check (L1285).
const lddMinimumTurns = 6

// ValueCheck is one metric of one cell; Informational ones never fail.
type ValueCheck struct {
	Metric        string `json:"metric"`
	Pass          bool   `json:"pass"`
	Informational bool   `json:"informational,omitempty"`
	Detail        string `json:"detail"`
}

// ValueCell is one Jhon cell against its no-Jhon counterpart.
type ValueCell struct {
	Task      string       `json:"task"`
	Variant   string       `json:"variant"`
	Condition string       `json:"condition"`
	Model     string       `json:"model,omitempty"`
	Size      string       `json:"size,omitempty"`
	Against   string       `json:"against"`
	Pass      bool         `json:"pass"`
	Checks    []ValueCheck `json:"checks"`
}

// SetupValue is a Jhon runtime's one-time LDD set-up, measured apart.
type SetupValue struct {
	Condition string `json:"condition"`
	Model     string `json:"model,omitempty"`
	Runs      int    `json:"runs"`
	Passed    int    `json:"passed"`
	Detail    string `json:"detail"`
}

// RuntimeValue is a runtime's cells: PASS when every one passed.
type RuntimeValue struct {
	Runtime   string `json:"runtime"`
	Model     string `json:"model,omitempty"`
	Condition string `json:"condition"`
	Against   string `json:"against"`
	Pass      bool   `json:"pass"`
	Passed    int    `json:"passed"`
	Of        int    `json:"of"`
}

// Value is value.json.
type Value struct {
	Results  []string       `json:"results"`
	Notes    []string       `json:"notes"`
	Cells    []ValueCell    `json:"cells"`
	Setup    []SetupValue   `json:"setup,omitempty"`
	Runtimes []RuntimeValue `json:"runtimes"`
	Verdict  string         `json:"verdict"`
}

// runtimes are the Jhon conditions by runtime, in print order.
var runtimes = []struct{ name, cond string }{{"claude-code", "jhon"}, {"pi", "pi-jhon"}}

// valueCell judges Jhon cell j against clean cell c, a task of size.
func valueCell(j, c Row, size string) []ValueCheck {
	var out []ValueCheck
	add := func(metric string, pass bool, format string, args ...any) {
		out = append(out, ValueCheck{Metric: metric, Pass: pass, Detail: fmt.Sprintf(format, args...)})
	}
	if j.Runs < MinRepeats || c.Runs < MinRepeats {
		add("repeats", false, "not enough runs: %s %d, %s %d, need %d each", j.Condition, j.Runs, c.Condition, c.Runs, MinRepeats)
	} else {
		add("repeats", true, "%d vs %d", j.Runs, c.Runs)
	}
	add("hidden tests", frac(j.Passed, j.Runs) >= frac(c.Passed, c.Runs), "%d/%d vs clean %d/%d", j.Passed, j.Runs, c.Passed, c.Runs)
	rate := func(metric string, jr, cr Rate) {
		jf, cf := frac(jr.Yes, jr.Known), frac(cr.Yes, cr.Known)
		switch {
		case jr.Known == 0 || cr.Known == 0:
			add(metric, false, "unknown: %s vs clean %s", jr, cr)
		case jf == 1 && cf == 1:
			add(metric, true, "%s vs clean %s, both every run", jr, cr)
		default:
			add(metric, jf > cf, "%s vs clean %s, need more often", jr, cr)
		}
	}
	rate("red before green", j.Process.RedBeforeGreen, c.Process.RedBeforeGreen)
	rate("tests run before end", j.Process.TestsRunBeforeEnd, c.Process.TestsRunBeforeEnd)
	if jo, co := j.Process.OutOfScopeFiles, c.Process.OutOfScopeFiles; jo == nil || co == nil {
		add("out of scope", false, "unknown: the out-of-scope median of %s or clean", j.Condition)
	} else {
		add("out of scope", *jo <= *co, "median %s vs clean %s", num2(*jo), num2(*co))
	}
	gate, sized := valueGates[size]
	if !sized {
		add("size", false, "task %s has no size: its task.yaml needs size: %s or %s (T146)", j.Task, SizeSmall, SizeLarge)
	}
	// side is a measure's median (min-max) on one side, scaled.
	side := func(r Row, measure string, scale float64, unit string) string {
		s := num2(r.Medians[measure]/scale) + unit
		if mm, ok := r.Spread[measure]; ok {
			s += fmt.Sprintf(" (%s-%s)", num2(mm[0]/scale), num2(mm[1]/scale))
		}
		return s
	}
	// gated is a cost measure within the size's limit of clean's.
	gated := func(metric, measure string, limit, scale float64, unit, note string) {
		jv, jok := j.Medians[measure]
		cv, cok := c.Medians[measure]
		if !jok || !cok || cv <= 0 {
			add(metric, false, "unknown: %s vs clean %s%s", side(j, measure, scale, unit), side(c, measure, scale, unit), note)
			return
		}
		ratio := jv / cv
		detail := fmt.Sprintf("%s vs clean %s (%s×", side(j, measure, scale, unit), side(c, measure, scale, unit), num2(ratio))
		if !sized || gate.Turns > 0 {
			// No size, or a small task: shown, never failed (L1285).
			out = append(out, ValueCheck{Metric: metric, Pass: true, Informational: true, Detail: detail + ")" + note})
			return
		}
		// A ratio at the limit passes, floating-point noise aside.
		add(metric, ratio <= limit*(1+1e-9), "%s, limit %s× for a %s task, L1205)%s", detail, num2(limit), size, note)
	}
	j, c = withCostWeighted(j), withCostWeighted(c)
	note := ""
	if _, ok := tokenWeightsFor(j.Model); !ok {
		note = fmt.Sprintf("; no token weights for %s: Claude's", j.Model)
	}
	if j.Medians["cost_weighted_from_medians"] == 1 || c.Medians["cost_weighted_from_medians"] == 1 {
		note += "; weighed from the token medians"
	}
	gated("weighted tokens", "cost_weighted", gate.Cost, 1, "", note)
	_, jusd := j.Medians["cost_usd"]
	_, cusd := c.Medians["cost_usd"]
	if jusd && cusd {
		gated("cost usd", "cost_usd", gate.Cost, 1, " $", "")
	}
	gated("active time", "active_ms", gate.Time, 1000, " s", "")
	if gate.BetterQuality {
		jp, cp := frac(j.Passed, j.Runs), frac(c.Passed, c.Runs)
		jl, cl := lapses(j), lapses(c)
		switch {
		case jp > cp:
			add("better quality", true, "hidden tests %d/%d vs clean %d/%d", j.Passed, j.Runs, c.Passed, c.Runs)
		case jp == 1 && cp == 1:
			add("better quality", jl < cl, "both pass every run; regressions and rework per run %s vs clean %s, need fewer", num2(jl), num2(cl))
		default:
			add("better quality", false, "hidden tests %d/%d vs clean %d/%d, need a higher pass rate", j.Passed, j.Runs, c.Passed, c.Runs)
		}
	}
	info := func(metric, measure string, scale float64, unit string) {
		out = append(out, ValueCheck{Metric: metric, Pass: true, Informational: true,
			Detail: fmt.Sprintf("%s vs clean %s (%s×)", side(j, measure, scale, unit), side(c, measure, scale, unit), num2(j.Medians[measure]/math.Max(c.Medians[measure], 1e-9)))})
	}
	if jv, jok := j.Medians["turns"]; gate.Turns > 0 {
		// A small task's cost gate: turns at most clean's + the LDD minimum.
		cv, cok := c.Medians["turns"]
		if !jok || !cok {
			add("turns", false, "unknown: %s vs clean %s", side(j, "turns", 1, ""), side(c, "turns", 1, ""))
		} else {
			diff := num2(jv - cv)
			if jv >= cv {
				diff = "+" + diff
			}
			add("turns", jv <= cv+float64(gate.Turns)+1e-9, "%s vs clean %s (%s, limit clean + %d for a %s task, L1285)",
				side(j, "turns", 1, ""), side(c, "turns", 1, ""), diff, gate.Turns, size)
		}
	} else {
		info("turns", "turns", 1, "")
	}
	if _, ok := j.Medians["quota_points"]; ok {
		// The subscription's quota, apart from the token cost (T146 FB3).
		out = append(out, ValueCheck{Metric: "quota points", Pass: true, Informational: true,
			Detail: fmt.Sprintf("%s vs clean %s, the subscription's weekly quota, never in the cost", side(j, "quota_points", 1, ""), side(c, "quota_points", 1, ""))})
	}
	return out
}

// lapses are a row's regressions and rework per run (rework only when known).
func lapses(r Row) float64 {
	return frac(r.Quality.Regressions+deref(r.Quality.Rework), r.Runs)
}

// withCostWeighted is r with a cost_weighted median: a row from before
// T146 (a report.json) has none, so it is weighed from its token medians by
// its provider's weights, and marked so.
func withCostWeighted(r Row) Row {
	if _, ok := r.Medians["cost_weighted"]; ok {
		return r
	}
	if _, ok := r.Medians["input_tokens"]; !ok {
		return r
	}
	m := maps.Clone(r.Medians)
	w, _ := tokenWeightsFor(r.Model)
	m["cost_weighted"] = w.weigh(m["input_tokens"], m["cache_write_tokens"], m["cache_read_tokens"], m["output_tokens"])
	m["cost_weighted_from_medians"] = 1
	r.Medians = m
	return r
}

// Judge is every Jhon cell of rows against its counterpart: the same task's
// plain cell of the no-Jhon condition, which runs the plain variant only.
// sizes are the task sizes by id for rows with no size (taskSizes).
func (v *Value) Judge(rows map[[3]string]Row, sizes map[string]string) {
	keys := slices.SortedFunc(func(yield func([3]string) bool) {
		for k := range rows {
			if !yield(k) {
				return
			}
		}
	}, cmpKeys)
	other := false
	for _, k := range keys {
		against, ok := noJhon[condOf(k[2])]
		if !ok {
			continue
		}
		if k[0] == SetupTaskID {
			// The one-time set-up, apart from every cell (T146 FB2).
			r := withCostWeighted(rows[k])
			v.Setup = append(v.Setup, SetupValue{Condition: condOf(k[2]), Model: identity(r.Model, r.Profile, r.ProjectSettings), Runs: r.Runs, Passed: r.Passed,
				Detail: fmt.Sprintf("weighted tokens %s, active time %s ms, turns %s, cost usd %s, quota points %s", spread(r, "cost_weighted"), spread(r, "active_ms"), spread(r, "turns"), spread(r, "cost_usd"), spread(r, "quota_points"))})
			continue
		}
		// The counterpart runs the same model (T77).
		cell := ValueCell{Task: k[0], Variant: k[1], Condition: condOf(k[2]), Model: identity(rows[k].Model, rows[k].Profile, rows[k].ProjectSettings), Against: against}
		// The no-Jhon counterpart has no profile: its model alone (T77b).
		cell.Size = cmpOr(rows[k].Size, sizes[k[0]])
		if c, ok := rows[[3]string{k[0], "plain", cellKey(against, withoutProfile(modelOf(k[2])))}]; ok {
			cell.Checks = valueCell(rows[k], c, cell.Size)
		} else {
			on := ""
			if cell.Model != "" {
				on = " on " + cell.Model
			}
			cell.Checks = []ValueCheck{{Metric: "counterpart", Detail: fmt.Sprintf("no %s run of %s plain%s: run --conditions %s", against, k[0], on, against)}}
		}
		other = other || k[1] != "plain"
		cell.Pass = !slices.ContainsFunc(cell.Checks, func(c ValueCheck) bool { return !c.Pass })
		v.Cells = append(v.Cells, cell)
	}
	if other {
		v.Notes = append(v.Notes, "the no-Jhon conditions run the plain variant only: a Jhon cell of another variant is compared with the same task's plain no-Jhon cell")
	}
	pass := len(v.Cells) > 0
	// One verdict per runtime and model, in the order the cells name them.
	for _, rt := range runtimes {
		var models []string
		for _, c := range v.Cells {
			if c.Condition == rt.cond && !slices.ContainsFunc(models, func(m string) bool { return normModel(m) == normModel(c.Model) }) {
				models = append(models, c.Model)
			}
		}
		for _, model := range models {
			r := RuntimeValue{Runtime: rt.name, Condition: rt.cond, Against: noJhon[rt.cond], Pass: true}
			if len(models) > 1 {
				r.Model = model
			}
			for _, c := range v.Cells {
				if c.Condition == rt.cond && normModel(c.Model) == normModel(model) {
					r.Of++
					if c.Pass {
						r.Passed++
					}
				}
			}
			r.Pass = r.Passed == r.Of
			pass = pass && r.Pass
			v.Runtimes = append(v.Runtimes, r)
		}
	}
	v.Verdict = map[bool]string{true: "PASS", false: "FAIL"}[pass]
}

// printValue writes each cell with its checks, each runtime, and the verdict.
func printValue(w io.Writer, v Value, file string) {
	fmt.Fprintf(w, "value: Jhon against no Jhon in %s\n", strings.Join(v.Results, ","))
	for _, n := range v.Notes {
		fmt.Fprintf(w, "  note: %s\n", n)
	}
	verdict := func(pass bool) string { return map[bool]string{true: "PASS", false: "FAIL"}[pass] }
	// A cell names its model when its condition ran several.
	many := map[string]bool{}
	for _, r := range v.Runtimes {
		many[r.Condition] = many[r.Condition] || r.Model != ""
	}
	for _, c := range v.Cells {
		on := ""
		if many[c.Condition] {
			on = ", model " + c.Model
		}
		size := ""
		if c.Size != "" {
			size = " (" + c.Size + ")"
		}
		fmt.Fprintf(w, "%s  %s %s %s vs %s plain%s%s\n", verdict(c.Pass), c.Task, c.Variant, c.Condition, c.Against, on, size)
		for _, ch := range c.Checks {
			mark := verdict(ch.Pass)
			if ch.Informational {
				mark = "INFO"
			}
			fmt.Fprintf(w, "        %s  %-22s %s\n", mark, ch.Metric, ch.Detail)
		}
	}
	if len(v.Cells) == 0 {
		fmt.Fprint(w, "no Jhon cell (jhon or pi-jhon) in the results\n")
	}
	for _, s := range v.Setup {
		fmt.Fprintf(w, "one-time set-up %s %s (apart from every cell): %d/%d passed, %s\n", s.Condition, s.Model, s.Passed, s.Runs, s.Detail)
	}
	for _, r := range v.Runtimes {
		rt := r.Runtime
		if r.Model != "" {
			rt += " " + r.Model
		}
		fmt.Fprintf(w, "runtime %s (%s vs %s): %s — %d/%d cells\n", rt, r.Condition, r.Against, verdict(r.Pass), r.Passed, r.Of)
	}
	fmt.Fprintf(w, "verdict: %s\nvalue.json: %s\n", v.Verdict, file)
}

// valueCLI is `bench value`.
func valueCLI(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("bench value", flag.ContinueOnError)
	fs.SetOutput(errOut)
	asJSON := fs.Bool("json", false, "print value.json instead of the lines")
	outDir := fs.String("out", "", "where value.json goes (default: the first results dir)")
	tasksDir := fs.String("tasks-dir", filepath.Join(source(), "tasks"), "where the tasks are: a cell whose runs have no task_size takes its task.yaml's size")
	if fs.Parse(args) != nil {
		return 2
	}
	var dirs []string
	for _, a := range fs.Args() {
		dirs = append(dirs, split(a)...)
	}
	if len(dirs) == 0 {
		fmt.Fprint(errOut, "bench value: name the results dirs (runs/ or report.json) holding the Jhon and the no-Jhon conditions\n")
		return 2
	}
	rows, err := loadSide(dirs, nil, "")
	if err != nil {
		fmt.Fprintf(errOut, "bench value: %v\n", err)
		return 2
	}
	v := Value{Results: dirs, Notes: []string{}, Cells: []ValueCell{}, Runtimes: []RuntimeValue{}}
	v.Judge(rows, taskSizes(*tasksDir))
	dir := cmpOr(*outDir, dirs[0])
	file := filepath.Join(dir, "value.json")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(errOut, "bench value: %v\n", err)
		return 2
	}
	if err := writeJSON(file, v); err != nil {
		fmt.Fprintf(errOut, "bench value: %v\n", err)
		return 2
	}
	if *asJSON {
		b, _ := jsonIndent(v)
		_, _ = out.Write(b)
	} else {
		printValue(out, v, file)
	}
	if v.Verdict != "PASS" {
		return 1
	}
	return 0
}
