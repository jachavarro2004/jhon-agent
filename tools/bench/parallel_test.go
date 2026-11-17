package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// T83 (L600, L601): bench run --parallel N and --smoke.

// sequentialOutput is a two-run clean batch's output with the results dir
// and wall times blanked, and its calls with the results dir blanked.
func sequentialOutput(t *testing.T, parallel int) (string, []string, []string) {
	t.Helper()
	f := newFake(t, "0.40", "0.41")
	o, out := options(t, f, "pagination-bug", []string{"clean"}, 2)
	o.Parallel = parallel
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	files, _ := filepath.Glob(filepath.Join(o.Results, "runs", "*", "*"))
	for i, p := range files {
		files[i], _ = filepath.Rel(o.Results, p)
	}
	var calls []string
	for _, c := range f.calls(t) {
		calls = append(calls, strings.NewReplacer(o.Results, "<R>", encodeCwd(o.Results), "<E>", f.home, "<H>", f.dir, "<F>").Replace(c))
	}
	text := out.String()
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if i := strings.Index(l, " wall="); i >= 0 {
			l = l[:i] + " wall=<w>"
		}
		lines = append(lines, l)
	}
	return strings.Join(lines, "\n"), calls, files
}

// --parallel 1, and no --parallel, is today's batch: one run after another,
// the same output, calls and files.
func TestParallelOneIsTheSequentialBatch(t *testing.T) {
	want := "run 1/2 pagination-bug-plain-clean-1 ...\n  success=false turns=7 output_tokens=900 wall=<w>\nrun 2/2 pagination-bug-plain-clean-2 ...\n  success=false turns=7 output_tokens=900 wall=<w>\n"
	out0, calls0, files0 := sequentialOutput(t, 0)
	out1, calls1, files1 := sequentialOutput(t, 1)
	if out0 != want || out1 != want {
		t.Errorf("output\n%s\n--parallel 1\n%s\nwant\n%s", out0, out1, want)
	}
	var kinds []string
	for _, c := range calls0 {
		if k, _, _ := strings.Cut(c, " "); k == "claude" || k == "jhon" {
			kinds = append(kinds, c[:min(len(c), 10)])
		}
	}
	if !slices.Equal(calls0, calls1) || !slices.Equal(kinds, []string{"jhon versi", "claude -p ", "jhon audit", "claude -p ", "jhon audit"}) {
		t.Errorf("calls\n%s\n--parallel 1\n%s", strings.Join(calls0, "\n"), strings.Join(calls1, "\n"))
	}
	if !slices.Equal(files0, files1) || len(files0) == 0 {
		t.Errorf("files %v, --parallel 1 %v", files0, files1)
	}
}

func readInt(t *testing.T, file string) int {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestParallelRunsAtMostNAtOnceAndFinishesThemAll(t *testing.T) {
	f := newFake(t)
	t.Setenv("FAKE_HOLD", "0.5")
	o, out := options(t, f, "pagination-bug", []string{"clean"}, 6)
	o.Parallel = 3
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if mx := readInt(t, filepath.Join(f.dir, "max")); mx != 3 {
		t.Errorf("%d claude runs at once, want 3", mx)
	}
	for i := 1; i <= 6; i++ {
		name := "pagination-bug-plain-clean-" + strconv.Itoa(i)
		metricsOf(t, o, name)
		// Each line whole: the launch line, and the result line naming its run.
		for _, want := range []string{"run " + strconv.Itoa(i) + "/6 " + name + " ...\n", "run " + strconv.Itoa(i) + "/6 " + name + ": success=false turns=7 output_tokens=900 wall="} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("output lacks %q:\n%s", want, out)
			}
		}
	}
	// bench report reads the runs in plan order, whatever order they finished in.
	var rep bytes.Buffer
	if err := Report(o.Results, &rep); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(o.Results, "report.json")); err != nil {
		t.Fatal(err)
	}
}

func TestParallelDryRunShowsIt(t *testing.T) {
	f := newFake(t)
	o, out := options(t, f, "pagination-bug", []string{"clean"}, 2)
	o.DryRun, o.Parallel = true, 4
	if err := Execute(o); err != nil {
		t.Fatal(err)
	}
	if want := "parallel: up to 4 runs at once"; !strings.Contains(out.String(), want) {
		t.Errorf("dry run lacks %q:\n%s", want, out)
	}
	o, _ = options(t, f, "pagination-bug", []string{"clean"}, 2)
	o.Parallel = -1
	if _, err := Plan(o); err == nil || err.Error() != "--parallel must be 1 or more" {
		t.Errorf("--parallel -1: %v", err)
	}
}

// Runs 1 and 2 read 40%, 3 and 4 read 55%: once one of them finishes the
// rise is 15 points, so run 5 never starts; the running one finishes.
func TestParallelStopsLaunchingOnceTheBudgetIsCrossed(t *testing.T) {
	f := newFake(t, "0.40", "0.40", "0.55", "0.55", "0.55", "0.55")
	t.Setenv("FAKE_HOLD", "0.4")
	o, out := options(t, f, "pagination-bug", []string{"clean"}, 6)
	o.Parallel = 2
	if err := Execute(o); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range f.calls(t) {
		if strings.HasPrefix(c, "claude -p") {
			n++
		}
	}
	if n != 4 {
		t.Fatalf("%d runs, want 4:\n%s", n, out)
	}
	for i := 1; i <= 4; i++ {
		metricsOf(t, o, "pagination-bug-plain-clean-"+strconv.Itoa(i))
	}
	want := "budget: weekly utilization rose 15 points since the first run (40% to 55%), over the 10-point limit; stopping before run 5 of 6"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("output lacks %q:\n%s", want, out)
	}
}

func allTasks(t *testing.T) []Task {
	t.Helper()
	ts, err := LoadTasks("tasks", nil)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func TestSmokeIsOnePlainRunPerTaskAndJhonCondition(t *testing.T) {
	f := newFake(t)
	o, _ := options(t, f, "pagination-bug", []string{"jhon", "clean", "pi-jhon"}, 3)
	o.Tasks, o.Variants, o.Smoke = allTasks(t), AllVariants, true
	runs, err := Plan(o)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, task := range o.Tasks {
		want = append(want, task.ID+"-plain-jhon-1", task.ID+"-plain-pi-jhon-1")
	}
	var got []string
	for _, r := range runs {
		got = append(got, r.Name())
	}
	if len(want) != 18 || !slices.Equal(got, want) {
		t.Errorf("smoke plan %v\nwant %v", got, want)
	}
	o, _ = options(t, f, "pagination-bug", []string{"clean"}, 1)
	o.Smoke = true
	if _, err := Plan(o); err == nil || !strings.Contains(err.Error(), "--smoke runs the Jhon conditions") {
		t.Errorf("smoke with no Jhon condition: %v", err)
	}
	o, out := options(t, f, "pagination-bug", []string{"jhon"}, 1)
	o.Smoke, o.DryRun = true, true
	if err := Execute(o); err != nil || !strings.Contains(out.String(), "smoke: one plain run per task and Jhon condition") {
		t.Errorf("smoke dry run %v:\n%s", err, out)
	}
}

// lddSolution is pagination-bug's solution with ldd/ and a change's tasks.md.
func lddSolution(t *testing.T) string {
	t.Helper()
	sol := t.TempDir()
	src, _ := filepath.Abs(filepath.Join("testdata", "solutions", "pagination-bug"))
	if err := copyTree(src, sol); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sol, "ldd", "changes", "fix-pager"), 0o755); err != nil {
		t.Fatal(err)
	}
	for file, body := range map[string]string{"ldd/config.yaml": "strict_tdd_policy: require\n", "ldd/changes/fix-pager/tasks.md": "- [x] 1.1 fix\n"} {
		if err := os.WriteFile(filepath.Join(sol, file), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return sol
}

func TestSmokeFailsOnTheHiddenCheckAndThePlainSetUpRule(t *testing.T) {
	f := newFake(t)
	if err := os.WriteFile(filepath.Join(f.dir, "status.json"), []byte(`{"ok":true,"tests":{"passed":true,"red_before_green":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	smoke := func(solution string) (string, error) {
		t.Setenv("FAKE_SOLUTION", solution)
		o, out := options(t, f, "pagination-bug", []string{"jhon"}, 1)
		o.Smoke = true
		err := Execute(o)
		return out.String(), err
	}
	if out, err := smoke(lddSolution(t)); err != nil {
		t.Errorf("solved with ldd/ and a record: %v\n%s", err, out)
	}
	sol, _ := filepath.Abs(filepath.Join("testdata", "solutions", "pagination-bug"))
	out, err := smoke(sol)
	if want := "smoke: 1 of 1 runs failed: pagination-bug-plain-jhon-1 (plain set-up: no ldd/ with a record)"; err == nil || err.Error() != want {
		t.Errorf("solved without ldd/: %v, want %q\n%s", err, want, out)
	}
	out, err = smoke("")
	if want := "smoke: 1 of 1 runs failed: pagination-bug-plain-jhon-1 (hidden check failed; plain set-up: no ldd/ with a record)"; err == nil || err.Error() != want {
		t.Errorf("unsolved: %v, want %q\n%s", err, want, out)
	}

	// The command exits 1 and still names the results.
	results := filepath.Join(t.TempDir(), "smoke")
	var stdout, stderr bytes.Buffer
	code := cli([]string{"run", "--home", f.home, "--results", results, "--tasks", "pagination-bug", "--conditions", "jhon", "--model", "haiku", "--smoke", "--parallel", "2"}, &stdout, &stderr, time.Now())
	if code != 1 || !strings.Contains(stderr.String(), "smoke: 1 of 1 runs failed: pagination-bug-plain-jhon-1 (hidden check failed") || !strings.Contains(stdout.String(), "results in "+results) {
		t.Errorf("exit %d\nstdout %s\nstderr %s", code, stdout.String(), stderr.String())
	}
}
