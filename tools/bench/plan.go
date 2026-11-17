package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// The certification's run matrix (L482, L483): every task, short and long
// (two-modules among them), on the two Jhon conditions, in every variant,
// 3 repeats, with opus (L395). The baseline of the installed build runs
// only the variants it has, plain and ldd.
var (
	certConditions       = []string{"jhon", "pi-jhon"}
	certBaselineVariants = []string{"plain", "ldd"}
)

// certModel is the model of the measured runs (L395: opus).
const certModel = "opus"

// history is what the bench's past runs say per cell: each run's
// quota-weighted tokens and wall time, and the batches whose weekly
// utilization moved.
type history struct {
	weighted, wall map[[3]string][]float64
	all            [2][]float64
	rise, tokens   float64
	batches        []string
}

// readHistory reads every <root>/<batch>/runs/*/metrics.json, read only;
// opus runs only when there are any. A batch with two weekly readings or
// more gives its rise in points (max − min) over every run's weighted tokens.
func readHistory(root string) history {
	h := history{weighted: map[[3]string][]float64{}, wall: map[[3]string][]float64{}}
	batches, _ := filepath.Glob(filepath.Join(root, "*", "runs"))
	read := map[string][]Metrics{}
	opus := false
	for _, b := range batches {
		files, _ := filepath.Glob(filepath.Join(b, "*", "metrics.json"))
		for _, file := range files {
			data, err := os.ReadFile(file)
			var m Metrics
			if err != nil || json.Unmarshal(data, &m) != nil {
				continue
			}
			read[b] = append(read[b], m)
			opus = opus || m.Model == certModel
		}
	}
	for _, b := range slices.Sorted(func(yield func(string) bool) {
		for k := range read {
			if !yield(k) {
				return
			}
		}
	}) {
		var lo, hi *float64
		tokens, readings := 0.0, 0
		for _, m := range read[b] {
			if opus && m.Model != certModel {
				continue
			}
			k := [3]string{m.Task, cmpOr(m.Variant, "plain"), m.Condition}
			w := quotaWeighted(m)
			h.weighted[k], h.wall[k] = append(h.weighted[k], w), append(h.wall[k], m.WallMs)
			h.all[0], h.all[1] = append(h.all[0], w), append(h.all[1], m.WallMs)
			tokens += w
			if u := m.Utilization; u != nil {
				readings++
				if lo == nil || *u < *lo {
					lo = u
				}
				if hi == nil || *u > *hi {
					hi = u
				}
			}
		}
		if readings >= 2 && tokens > 0 {
			h.rise += (*hi - *lo) * 100
			h.tokens += tokens
			h.batches = append(h.batches, filepath.Base(filepath.Dir(b)))
		}
	}
	return h
}

// cell is the median of a planned cell from h: the cell itself, else the
// same task and condition's ldd (a new variant), else any of its variants,
// else every run.
func (h history) cell(i int, task, variant, cond string) float64 {
	of := [2]map[[3]string][]float64{h.weighted, h.wall}[i]
	keys := [][3]string{{task, variant, cond}}
	if slices.Contains(newVariants, variant) {
		keys = append(keys, [3]string{task, "ldd", cond})
	}
	for _, k := range keys {
		if vs := of[k]; len(vs) > 0 {
			return median(vs)
		}
	}
	var vs []float64
	for k, v := range of {
		if k[0] == task && k[2] == cond {
			vs = append(vs, v...)
		}
	}
	if len(vs) > 0 {
		return median(vs)
	}
	if len(h.all[i]) > 0 {
		return median(h.all[i])
	}
	return 0
}

// CertifyPlan prints the certification's matrix, the baseline's, and their
// estimate from the records under results. It runs nothing and writes nothing.
//
// Each side is one sequence: prepare the bench HOME with that commit's build
// (`prepare --ref`), check the login, and run with --expect-commit, so a run
// refuses a bench HOME holding the other side's jhon.
func CertifyPlan(tasksDir, results, baseRef, candRef string, out io.Writer) error {
	tasks, err := LoadTasks(tasksDir, nil)
	if err != nil {
		return err
	}
	var short, long, ids []string
	for _, t := range tasks {
		ids = append(ids, t.ID)
		if t.Kind == "long" {
			long = append(long, t.ID)
		} else {
			short = append(short, t.ID)
		}
	}
	h := readHistory(results)
	fmt.Fprintf(out, "certification matrix (L482, L483); --plan runs nothing\n  short tasks: %s\n  long tasks: %s\n  conditions: %s\n  variants: %s (ldd-ask's first message starts with %q)\n  repeats: %d, model: %s\n",
		strings.Join(short, ", "), strings.Join(long, ", "), strings.Join(certConditions, ", "), strings.Join(AllVariants, ", "), AskSentence, MinRepeats, certModel)
	dirOf := func(label, ref string) string { return filepath.Join(results, label+"-"+ref) }
	estimate := func(label, ref, dir string, variants []string) {
		n := len(ids) * len(certConditions) * len(variants) * MinRepeats
		var tokens, wall float64
		for _, t := range ids {
			for _, v := range variants {
				for _, c := range certConditions {
					tokens += MinRepeats * h.cell(0, t, v, c)
					wall += MinRepeats * h.cell(1, t, v, c)
				}
			}
		}
		fmt.Fprintf(out, "\n%s: %d runs\n  $ go run ./tools/bench prepare --ref %s\n  $ go run ./tools/bench login-check\n  $ go run ./tools/bench run --conditions %s --variants %s --repeats %d --model %s --expect-commit %s --results %s\n",
			label, n, ref, strings.Join(certConditions, ","), strings.Join(variants, ","), MinRepeats, certModel, ref, dir)
		if h.tokens == 0 {
			fmt.Fprintf(out, "  no estimate: no runs with a weekly reading under %s\n", results)
			return
		}
		fmt.Fprintf(out, "  estimate: ≈ %s quota points, ≈ %s of wall time (runs one at a time)\n", num2(tokens*h.rise/h.tokens), (time.Duration(wall) * time.Millisecond).Round(time.Minute))
	}
	baseDir, candDir := dirOf("baseline", baseRef), dirOf("candidate", candRef)
	estimate("baseline of the installed build", baseRef, baseDir, certBaselineVariants)
	estimate("candidate", candRef, candDir, AllVariants)
	if h.tokens > 0 {
		fmt.Fprintf(out, "\nmethod: from %d batch(es) under %s with two weekly readings or more (%s): %s points of weekly utilization over %s quota-weighted tokens (input + 1.25 × cache write + 0.1 × cache read + 5 × output, L432) gives the points per token; each planned cell's median weighted tokens and wall time come from its own past runs, else the same task and condition's ldd (ldd-native, ldd-ask), else any variant of it, else every run; %s runs only. The weekly reading moves in whole points, so the estimate is rough.\n",
			len(h.batches), results, strings.Join(h.batches, ", "), num2(h.rise), num2(h.tokens), certModel)
	}
	fmt.Fprintf(out, "\nthen:\n  $ go run ./tools/bench certify --baseline %s --candidate %s --gates <checkout>\n", baseDir, candDir)
	return nil
}
