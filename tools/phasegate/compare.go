package main

import (
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
)

// The tolerances compare holds the new record to.
const (
	// slowerPct and slowerMS: a median time is worse when it grows by more
	// than slowerPct percent and by more than slowerMS milliseconds, so that
	// jitter on a fast command (a few ms on 20) never fails the gate.
	slowerPct = 25
	slowerMS  = 100
	// hookCeilingMS: a hook median over it is worse even with no base: the
	// hook runs on every tool call, and the shell refuses one past 10 s.
	hookCeilingMS = 1000
	// statusCeilingMS: a status median over it is worse even with no base:
	// the agent runs it before a change, and the owner waits for it.
	statusCeilingMS = 3000
	// stopsPct: stops per 100 tool results is worse when it grows by more
	// than this percent of the base rate.
	stopsPct = 10
	// budgetFloor: a budget is worse when its headroom (limit minus bytes)
	// is below this, whatever the base.
	budgetFloor = 0
	// leftoverGrowth: hygiene is worse when the suite's TMPDIR leftovers grow
	// by more than this.
	leftoverGrowth = 0
)

// row is one line of the table.
type row struct {
	measure, base, new, change, verdict string
	worse                               bool
}

// compareRecords is every measure of either record, base against new.
func compareRecords(base, new *Record) []row {
	var rows []row
	rows = append(rows, speedRows(base, new)...)
	rows = append(rows, frictionRows(base, new)...)
	rows = append(rows, budgetRows(base, new)...)
	rows = append(rows, hygieneRows(base, new)...)
	return append(rows, resultRows(base, new)...)
}

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func signed(v float64) string {
	if v >= 0 {
		return "+" + num(v)
	}
	return num(v)
}

// growth is the change from b to n in percent, rounded.
func growth(b, n float64) string { return signed(math.Round(100*(n-b)/b)) + "%" }

func project(s *Speed, index int) *ProjectSpeed {
	if s == nil {
		return nil
	}
	for i := range s.Projects {
		if s.Projects[i].Index == index {
			return &s.Projects[i]
		}
	}
	return nil
}

func ceiling(command string) int {
	switch {
	case strings.HasPrefix(command, "hook-"):
		return hookCeilingMS
	case command == "status":
		return statusCeilingMS
	}
	return 0
}

func speedRows(base, new *Record) []row {
	var indexes []int
	for _, r := range []*Record{base, new} {
		if r.Speed != nil {
			for _, p := range r.Speed.Projects {
				indexes = append(indexes, p.Index)
			}
		}
	}
	slices.Sort(indexes)
	var rows []row
	for _, i := range slices.Compact(indexes) {
		b, n := project(base.Speed, i), project(new.Speed, i)
		for _, cmd := range speedCommands {
			bt, bok := timing(b, cmd)
			nt, nok := timing(n, cmd)
			if !bok && !nok && skipped(b, cmd) == "" && skipped(n, cmd) == "" {
				continue
			}
			r := row{measure: fmt.Sprintf("p%d %s median ms", i, cmd), base: timingCell(b, cmd), new: timingCell(n, cmd)}
			if bok && nok {
				r.change = signed(round(nt.MedianMS-bt.MedianMS, 1)) + " ms"
				if bt.MedianMS > 0 {
					r.change += " (" + growth(bt.MedianMS, nt.MedianMS) + ")"
				}
			}
			switch c := ceiling(cmd); {
			case !nok && skipped(n, cmd) != "":
				r.verdict = "skipped: " + skipped(n, cmd)
			case !nok:
				r.verdict = "not measured"
			case c > 0 && nt.MedianMS > float64(c):
				r.verdict, r.worse = fmt.Sprintf("worse: over %d ms", c), true
			case !bok:
				r.verdict = "no base"
			case nt.MedianMS-bt.MedianMS > slowerMS && (bt.MedianMS == 0 || 100*(nt.MedianMS-bt.MedianMS)/bt.MedianMS > slowerPct):
				r.verdict, r.worse = "worse", true
			default:
				r.verdict = "ok"
			}
			rows = append(rows, r)
		}
		r := row{measure: fmt.Sprintf("p%d objects", i), base: objectsCell(b), new: objectsCell(n)}
		switch {
		case n == nil:
			r.verdict = "not measured"
		case n.ObjectsGrew:
			r.verdict, r.worse = "worse: objects grew", true
		default:
			r.verdict = "ok"
		}
		rows = append(rows, r)
	}
	return rows
}

func timing(p *ProjectSpeed, cmd string) (Timing, bool) {
	if p == nil {
		return Timing{}, false
	}
	t, ok := p.Commands[cmd]
	return t, ok
}

func skipped(p *ProjectSpeed, cmd string) string {
	if p == nil {
		return ""
	}
	return p.Skipped[cmd]
}

func timingCell(p *ProjectSpeed, cmd string) string {
	if t, ok := timing(p, cmd); ok {
		return fmt.Sprintf("%s (max %s)", num(t.MedianMS), num(t.MaxMS))
	}
	if skipped(p, cmd) != "" {
		return "skipped"
	}
	return ""
}

func objectsCell(p *ProjectSpeed) string {
	switch {
	case p == nil:
		return ""
	case p.ObjectsGrew:
		return "grew"
	}
	return "kept"
}

// countingRule is the rule f was counted by; a record without one was
// counted by rule 1.
func countingRule(f *Friction) int { return max(1, f.Counting) }

// countingNote is the line that says base's and new's friction were
// counted by different rules, "" when they were not.
func countingNote(base, new *Record) string {
	b, n := base.Friction, new.Friction
	if b == nil || n == nil || countingRule(b) == countingRule(n) {
		return ""
	}
	return fmt.Sprintf("phasegate: friction counted by rule %d in base and rule %d in new: its rows are not comparable", countingRule(b), countingRule(n))
}

// frictionRows: friction counted by different rules is shown, not held to
// the base ("not comparable").
func frictionRows(base, new *Record) []row {
	b, n := base.Friction, new.Friction
	if b == nil && n == nil {
		return nil
	}
	comparable := countingNote(base, new) == ""
	rate := func(f *Friction) (float64, bool) {
		if f == nil || f.ToolResults == 0 {
			return 0, false
		}
		return f.Per100, true
	}
	since := func(f *Friction) string {
		if f == nil {
			return ""
		}
		return f.Since
	}
	rows := []row{{measure: "friction since", base: since(b), new: since(n), verdict: "info"}}
	rateRow := func(measure string, bv float64, bok bool, nv float64, nok bool, gated bool) row {
		r := row{measure: measure}
		if bok {
			r.base = num(bv)
		}
		if nok {
			r.new = num(nv)
		}
		if bok && nok {
			r.change = signed(round(nv-bv, 3))
			if bv > 0 {
				r.change += " (" + growth(bv, nv) + ")"
			}
		}
		switch {
		case !nok:
			r.verdict = "not measured"
		case !comparable:
			r.verdict = "not comparable"
		case !gated:
			r.verdict = "info"
		case !bok:
			r.verdict = "no base"
		case nv > bv*(1+stopsPct/100.0):
			r.verdict, r.worse = "worse", true
		default:
			r.verdict = "ok"
		}
		return r
	}
	bv, bok := rate(b)
	nv, nok := rate(n)
	rows = append(rows, rateRow("stops per 100 tool results", bv, bok, nv, nok, true))
	var classes []string
	for _, f := range []*Friction{b, n} {
		if f != nil {
			for c := range f.PerClass {
				classes = append(classes, c)
			}
		}
	}
	slices.Sort(classes)
	for _, c := range slices.Compact(classes) {
		bc, bin := classOf(b, c)
		nc, nin := classOf(n, c)
		rows = append(rows, rateRow("stops per 100: "+c, bc, bok && bin, nc, nok && nin, false))
	}
	return append(rows, runtimeRows(b, n, comparable)...)
}

// runtimeRows say which runtime the friction is in: each runtime's stops,
// tool results and stops per 100, as information.
func runtimeRows(b, n *Friction, comparable bool) []row {
	var names []string
	for _, f := range []*Friction{b, n} {
		if f != nil {
			for rt := range f.PerRuntime {
				names = append(names, rt)
			}
		}
	}
	slices.Sort(names)
	get := func(f *Friction, rt string) *Count {
		if f == nil {
			return nil
		}
		return f.PerRuntime[rt]
	}
	cell := func(c *Count) string {
		if c == nil {
			return ""
		}
		return fmt.Sprintf("%d/%d (%s)", c.Stops, c.ToolResults, num(c.Per100))
	}
	var rows []row
	for _, rt := range slices.Compact(names) {
		bc, nc := get(b, rt), get(n, rt)
		r := row{measure: "runtime " + rt + " stops/tool results", base: cell(bc), new: cell(nc), verdict: "info"}
		if !comparable {
			r.verdict = "not comparable"
		}
		if bc != nil && nc != nil {
			r.change = signed(round(nc.Per100-bc.Per100, 3))
			if bc.Per100 > 0 {
				r.change += " (" + growth(bc.Per100, nc.Per100) + ")"
			}
		}
		rows = append(rows, r)
	}
	return rows
}

func classOf(f *Friction, class string) (float64, bool) {
	if f == nil {
		return 0, false
	}
	if c, ok := f.PerClass[class]; ok {
		return c.Per100, true
	}
	return 0, f.ToolResults > 0
}

func budgetRows(base, new *Record) []row {
	var keys []string
	for _, r := range []*Record{base, new} {
		for k := range r.Budgets {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	get := func(r *Record, k string) (Budget, bool) {
		b, ok := r.Budgets[k]
		return b, ok
	}
	cell := func(b Budget, ok bool) string {
		if !ok {
			return ""
		}
		return fmt.Sprintf("%d/%d", b.Value, b.Limit)
	}
	var rows []row
	for _, k := range slices.Compact(keys) {
		b, bok := get(base, k)
		n, nok := get(new, k)
		r := row{measure: "budget " + k, base: cell(b, bok), new: cell(n, nok)}
		if bok && nok {
			r.change = signed(float64(n.Value - b.Value))
		}
		switch {
		case !nok && new.Skipped["budgets"] != "":
			r.verdict = "skipped: " + new.Skipped["budgets"]
		case !nok:
			r.verdict = "not measured"
		case n.Limit-n.Value < budgetFloor:
			r.verdict, r.worse = "worse: over its limit", true
		default:
			r.verdict = "ok"
		}
		rows = append(rows, r)
	}
	return rows
}

func hygieneRows(base, new *Record) []row {
	if base.Hygiene == nil && new.Hygiene == nil {
		return nil
	}
	r := row{measure: "hygiene suite TMPDIR leftovers"}
	if base.Hygiene != nil {
		r.base = strconv.Itoa(base.Hygiene.SuiteTmpLeftovers)
	}
	if new.Hygiene != nil {
		r.new = strconv.Itoa(new.Hygiene.SuiteTmpLeftovers)
	}
	switch {
	case new.Hygiene == nil:
		r.verdict = "not measured"
	case base.Hygiene == nil:
		r.verdict = "no base"
	default:
		d := new.Hygiene.SuiteTmpLeftovers - base.Hygiene.SuiteTmpLeftovers
		r.change = signed(float64(d))
		r.verdict = "ok"
		if d > leftoverGrowth {
			r.verdict, r.worse = "worse", true
		}
	}
	return []row{r}
}

// resultRows: a result that goes from pass to fail is worse, and so is a
// fail with no base result; a fail in both is shown, not counted.
func resultRows(base, new *Record) []row {
	var names []string
	for _, r := range []*Record{base, new} {
		for k := range r.Results {
			names = append(names, k)
		}
	}
	slices.Sort(names)
	var rows []row
	for _, k := range slices.Compact(names) {
		b, n := base.Results[k], new.Results[k]
		r := row{measure: "result " + k, base: b, new: n}
		switch {
		case n == "":
			r.verdict = "not measured"
		case n == "fail" && b == "fail":
			r.verdict = "still fails"
		case n == "fail":
			r.verdict, r.worse = "worse", true
		case b == "":
			r.verdict = "no base"
		default:
			r.verdict = "ok"
		}
		rows = append(rows, r)
	}
	return rows
}

// writeTable prints rows as aligned plain text, or as a Markdown table.
func writeTable(w io.Writer, rows []row, markdown bool) {
	head := []string{"measure", "base", "new", "change", "verdict"}
	if markdown {
		fmt.Fprintln(w, "| "+strings.Join(head, " | ")+" |")
		fmt.Fprintln(w, "|---|---|---|---|---|")
		for _, r := range rows {
			fmt.Fprintln(w, "| "+strings.Join([]string{r.measure, r.base, r.new, r.change, r.verdict}, " | ")+" |")
		}
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(head, "\t"))
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join([]string{r.measure, r.base, r.new, r.change, r.verdict}, "\t"))
	}
	tw.Flush()
}
