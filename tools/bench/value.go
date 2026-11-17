package main

import (
	"flag"
	"fmt"
	"io"
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
// happen more often (or in every run of both), and its out-of-scope median
// is no higher. Cost — quota-weighted tokens, turns, active time — is shown
// as a ratio for the owner to judge, never failed. Fewer than 3 runs on
// either side, or a metric unknown on either, is FAIL (L483).

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
	Against   string       `json:"against"`
	Pass      bool         `json:"pass"`
	Checks    []ValueCheck `json:"checks"`
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
	Runtimes []RuntimeValue `json:"runtimes"`
	Verdict  string         `json:"verdict"`
}

// runtimes are the Jhon conditions by runtime, in print order.
var runtimes = []struct{ name, cond string }{{"claude-code", "jhon"}, {"pi", "pi-jhon"}}

// valueCell judges Jhon cell j against clean cell c.
func valueCell(j, c Row) []ValueCheck {
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
	cost := func(metric string, jv, cv float64, unit string) {
		out = append(out, ValueCheck{Metric: metric, Pass: true, Informational: true,
			Detail: fmt.Sprintf("%s%s vs clean %s%s (%s×)", num2(jv), unit, num2(cv), unit, num2(jv/math.Max(cv, 1e-9)))})
	}
	jw, _ := weighted(j)
	cw, _ := weighted(c)
	cost("quota-weighted tokens", jw, cw, "")
	cost("turns", j.Medians["turns"], c.Medians["turns"], "")
	cost("active time", j.Medians["active_ms"]/1000, c.Medians["active_ms"]/1000, " s")
	return out
}

// Judge is every Jhon cell of rows against its counterpart: the same task's
// plain cell of the no-Jhon condition, which runs the plain variant only.
func (v *Value) Judge(rows map[[3]string]Row) {
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
		// The counterpart runs the same model (T77).
		cell := ValueCell{Task: k[0], Variant: k[1], Condition: condOf(k[2]), Model: identity(rows[k].Model, rows[k].Profile, rows[k].ProjectSettings), Against: against}
		// The no-Jhon counterpart has no profile: its model alone (T77b).
		if c, ok := rows[[3]string{k[0], "plain", cellKey(against, withoutProfile(modelOf(k[2])))}]; ok {
			cell.Checks = valueCell(rows[k], c)
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
		fmt.Fprintf(w, "%s  %s %s %s vs %s plain%s\n", verdict(c.Pass), c.Task, c.Variant, c.Condition, c.Against, on)
		for _, ch := range c.Checks {
			mark := verdict(ch.Pass)
			if ch.Informational {
				mark = "COST"
			}
			fmt.Fprintf(w, "        %s  %-22s %s\n", mark, ch.Metric, ch.Detail)
		}
	}
	if len(v.Cells) == 0 {
		fmt.Fprint(w, "no Jhon cell (jhon or pi-jhon) in the results\n")
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
	v.Judge(rows)
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
