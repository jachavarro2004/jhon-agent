package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// statusBin is the jhon whose status reads an ldd copy: JHON_TEST_BIN, else
// the checkout's build/jhon, else the tree's own, built once.
func statusBin(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("JHON_TEST_BIN"); p != "" {
		return p
	}
	if p := filepath.Join(support.Root(), "build", "jhon"); fileExists(p) {
		return p
	}
	return support.Bin(t)
}

func TestThePlanIsRepeatMajorAcrossTasksVariantsAndConditions(t *testing.T) {
	f := newFake(t)
	o, out := options(t, f, "pagination-bug", AllConditions, 2)
	more, err := LoadTasks("tasks", []string{"shipping-rules"})
	if err != nil {
		t.Fatal(err)
	}
	o.Tasks, o.Variants = append(o.Tasks, more...), []string{"plain", "ldd"}
	runs, err := Plan(o)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range runs {
		names = append(names, r.Name())
	}
	var want []string
	for _, rep := range []string{"1", "2"} {
		for _, task := range []string{"pagination-bug", "shipping-rules"} {
			for _, cell := range []string{"plain-jhon", "plain-clean", "plain-pi-jhon", "ldd-jhon", "ldd-pi-jhon"} {
				want = append(want, task+"-"+cell+"-"+rep)
			}
		}
	}
	if !slices.Equal(names, want) {
		t.Fatalf("plan\n%v\nwant\n%v", names, want)
	}
	if runs[3].Variant != "ldd" || runs[3].Dir != filepath.Join(o.Results, "runs", "pagination-bug-ldd-jhon-1") {
		t.Errorf("run 4: variant %q dir %s", runs[3].Variant, runs[3].Dir)
	}
	o.DryRun = true
	if err := Execute(o); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "clean runs the plain variant only (it ignores openspec)") {
		t.Errorf("the plan does not say clean runs plain only:\n%s", out)
	}
}

func TestThePlanRefusesAnUnknownVariant(t *testing.T) {
	f := newFake(t)
	o, _ := options(t, f, "pagination-bug", AllConditions, 1)
	o.Variants = []string{"plain", "other"}
	if _, err := Plan(o); err == nil || !strings.Contains(err.Error(), `unknown variant "other" (known: plain, ldd, ldd-native, ldd-ask)`) {
		t.Fatalf("err %v", err)
	}
}

func TestTheFullRunPlansFortyFiveRunsWithBothVariantsByDefault(t *testing.T) {
	newFake(t)
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := cli([]string{"run", "--tasks", "pagination-bug,shipping-rules,account-import", "--repeats", "3", "--model", "opus", "--budget-points", "10", "--dry-run"},
		&out, &errOut, time.Date(2026, 11, 9, 8, 30, 0, 0, time.UTC))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, s := range []string{"plan: 45 runs", "run 1/45 pagination-bug-plain-jhon-1", "run 15/45 account-import-ldd-pi-jhon-1",
		"run 16/45 pagination-bug-plain-jhon-2", "run 45/45 account-import-ldd-pi-jhon-3", "clean runs the plain variant only"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("dry run lacks %q", s)
		}
	}
	errOut.Reset()
	if code := cli([]string{"run", "--variants", "ldd", "--conditions", "clean", "--dry-run"}, &out, &errOut, time.Now()); code == 0 || !strings.Contains(errOut.String(), "no runs") {
		t.Errorf("clean on ldd only: exit %d, stderr %q", code, errOut.String())
	}
}

// status is `jhon status --json` in repo, with a scratch HOME.
func status(t *testing.T, repo string) map[string]any {
	t.Helper()
	c := exec.Command(statusBin(t), "status", "--json")
	// A test build lives in TMPDIR, away from prose/: JHON_ROOT names the tree.
	c.Dir, c.Env = repo, []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "JHON_ROOT=" + support.Root()}
	b, err := c.Output()
	if err != nil {
		t.Fatalf("jhon status: %v\n%s", err, b)
	}
	var s map[string]any
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	return s
}

func TestTheLDDVariantTurnsJhonsLDDOnAndThePlainOneDoesNot(t *testing.T) {
	tasks, err := LoadTasks("tasks", taskIDs)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		t.Run(task.ID, func(t *testing.T) {
			plain, ldd := filepath.Join(t.TempDir(), "repo"), filepath.Join(t.TempDir(), "repo")
			if err := prepareRepo(task, "plain", plain); err != nil {
				t.Fatal(err)
			}
			if err := prepareRepo(task, "ldd", ldd); err != nil {
				t.Fatal(err)
			}
			if fileExists(filepath.Join(plain, "openspec")) || fileExists(filepath.Join(plain, "pyproject.toml")) {
				t.Errorf("the plain copy has LDD files")
			}
			for _, repo := range []string{plain, ldd} {
				if err := gitRepo(repo, append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")); err != nil {
					t.Fatal(err)
				}
			}
			s := status(t, ldd)
			tdd, _ := s["strict_tdd"].(map[string]any)
			projects, _ := s["projects"].([]any)
			if tdd["value"] != true || tdd["evidence"] != "ok" || len(projects) != 1 {
				t.Fatalf("ldd copy: strict_tdd %v projects %v", tdd, projects)
			}
			p := projects[0].(map[string]any)
			if p["path"] != "." || p["testability"] != "testable" || p["command"] != "python3 -m unittest" {
				t.Errorf("ldd copy project %v", p)
			}
			if tdd := status(t, plain)["strict_tdd"].(map[string]any); tdd["value"] == true && tdd["source"] == "policy" {
				t.Errorf("plain copy resolves a policy: %v", tdd)
			}
			if ok, out, err := task.Accept(ldd, os.Environ()); err != nil || ok {
				t.Errorf("the hidden test passes on the untouched ldd copy (%v):\n%s", err, out)
			}
		})
	}
}

func TestAnLDDFakeRunWorksInAnLDDCopyAndCarriesItsVariant(t *testing.T) {
	f := newFake(t, "0.40")
	o, out := options(t, f, "pagination-bug", []string{"jhon"}, 1)
	o.Variants = []string{"ldd"}
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	m := metricsOf(t, o, "pagination-bug-ldd-jhon-1")
	repo := filepath.Join(o.Results, "runs", "pagination-bug-ldd-jhon-1", "repo")
	if m.Variant != "ldd" || m.Condition != "jhon" || !fileExists(filepath.Join(repo, "openspec", "config.yaml")) {
		t.Fatalf("variant %q condition %q, openspec in %s", m.Variant, m.Condition, repo)
	}
}

func TestTheReportGroupsByVariantAndShowsTheOverheadVsClean(t *testing.T) {
	dir := t.TempDir()
	put := func(name string, m Metrics) {
		d := filepath.Join(dir, "runs", name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := writeJSON(filepath.Join(d, "metrics.json"), m); err != nil {
			t.Fatal(err)
		}
	}
	tok := func(task, variant, cond string, rep int, total, active float64) Metrics {
		return Metrics{Task: task, Variant: variant, Condition: cond, Repeat: rep, Success: true,
			InputTokens: 10, OutputTokens: 90, CacheReadTokens: total - 150, CacheWriteTokens: 50, ActiveMs: active}
	}
	put("a-plain-clean-1", tok("pagination-bug", "plain", "clean", 1, 1000, 10000))
	put("a-plain-clean-2", tok("pagination-bug", "plain", "clean", 2, 3000, 30000))
	put("a-plain-jhon-1", tok("pagination-bug", "plain", "jhon", 1, 8000, 90000))
	put("a-ldd-jhon-1", tok("pagination-bug", "ldd", "jhon", 1, 12000, 100000))
	put("a-ldd-jhon-2", tok("pagination-bug", "ldd", "jhon", 2, 8000, 60000))
	// A run from before variants reads as plain.
	put("b-clean-1", tok("shipping-rules", "", "clean", 1, 500, 1000))
	put("b-pi-jhon-1", Metrics{Task: "shipping-rules", Condition: "pi-jhon", Repeat: 1, InputTokens: 1000, ActiveMs: 3000})
	var out bytes.Buffer
	if err := Report(dir, &out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Rows     []Row      `json:"rows"`
		Overhead []Overhead `json:"overhead"`
	}
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	var cells []string
	for _, r := range rep.Rows {
		cells = append(cells, r.Task+"/"+r.Variant+"/"+r.Condition)
	}
	want := []string{"pagination-bug/plain/jhon", "pagination-bug/plain/clean", "pagination-bug/ldd/jhon", "shipping-rules/plain/clean", "shipping-rules/plain/pi-jhon"}
	if !slices.Equal(cells, want) {
		t.Fatalf("rows %v, want %v", cells, want)
	}
	if rep.Rows[1].Medians["tokens_processed"] != 2000 || rep.Rows[2].Medians["tokens_processed"] != 10000 {
		t.Errorf("tokens processed %v %v", rep.Rows[1].Medians, rep.Rows[2].Medians)
	}
	wantOver := []Overhead{
		{Task: "pagination-bug", Variant: "plain", Condition: "jhon", Tokens: f64(4), Active: f64(4.5)},
		{Task: "pagination-bug", Variant: "ldd", Condition: "jhon", Tokens: f64(5), Active: f64(4)},
		{Task: "shipping-rules", Variant: "plain", Condition: "pi-jhon", Tokens: f64(2), Active: f64(3)},
	}
	if len(rep.Overhead) != len(wantOver) {
		t.Fatalf("overhead %+v", rep.Overhead)
	}
	for i, w := range wantOver {
		g := rep.Overhead[i]
		if g.Task != w.Task || g.Variant != w.Variant || g.Condition != w.Condition || *g.Tokens != *w.Tokens || *g.Active != *w.Active {
			t.Errorf("overhead %d: %+v (%v, %v), want %+v", i, g, *g.Tokens, *g.Active, w)
		}
	}
	table := out.String()
	for _, s := range []string{"task            variant  condition", "pagination-bug  ldd      jhon", "overhead vs clean", "4×", "4.5×", "5×"} {
		if !strings.Contains(table, s) {
			t.Errorf("table lacks %q:\n%s", s, table)
		}
	}
}

// By default a run takes every task: the six short ones, the three long
// ones (T46) and the workspace one (T192), 150 runs at 3 repeats, and the
// one-time set-up once per Jhon runtime (T146 FB2), 152; the six short ones
// by name are 90.
func TestTheNextFullRunIsEveryTaskByDefault(t *testing.T) {
	newFake(t)
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := cli([]string{"run", "--repeats", "3", "--model", "opus", "--budget-points", "10", "--dry-run"}, &out, &errOut, time.Date(2026, 11, 9, 8, 30, 0, 0, time.UTC))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, s := range []string{"plan: 152 runs", "run 1/152 account-import-plain-jhon-1", "run 45/152 two-modules-ldd-pi-jhon-1",
		"run 50/152 workspace-budgets-ldd-pi-jhon-1", "run 51/152 ldd-setup-plain-jhon-1", "run 52/152 ldd-setup-plain-pi-jhon-1", "run 152/152 workspace-budgets-ldd-pi-jhon-3"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("dry run lacks %q", s)
		}
	}
	out.Reset()
	six := "--tasks=" + strings.Join(taskIDs, ",")
	if code := cli([]string{"run", six, "--repeats", "3", "--model", "opus", "--dry-run"}, &out, &errOut, time.Now()); code != 0 ||
		!strings.Contains(out.String(), "plan: 90 runs") || !strings.Contains(out.String(), "run 90/90 shipping-rules-ldd-pi-jhon-3") {
		t.Errorf("the six: exit %d %s", code, errOut.String())
	}
	out.Reset()
	if code := cli([]string{"run", "--tasks", "search-accents,contacts-migration,late-fee", "--repeats", "3", "--model", "opus", "--dry-run"}, &out, &errOut, time.Now()); code != 0 ||
		!strings.Contains(out.String(), "plan: 45 runs") {
		t.Errorf("trap-only: exit %d %s", code, errOut.String())
	}
}

// ldd-native is the ldd variant in Jhon's native layout (L483): the same
// config at ldd/config.yaml, no openspec/; ldd-ask is the plain template
// with the ask sentence before the first message, so before the run it has
// no layout at all.
func TestTheNativeAndAskVariantsPrepareTheirRepos(t *testing.T) {
	tasks, err := LoadTasks("tasks", taskIDs)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		t.Run(task.ID, func(t *testing.T) {
			native, ask := filepath.Join(t.TempDir(), "repo"), filepath.Join(t.TempDir(), "repo")
			if err := prepareRepo(task, "ldd-native", native); err != nil {
				t.Fatal(err)
			}
			if err := prepareRepo(task, "ldd-ask", ask); err != nil {
				t.Fatal(err)
			}
			if !fileExists(filepath.Join(native, "ldd", "config.yaml")) || !fileExists(filepath.Join(native, "pyproject.toml")) || fileExists(filepath.Join(native, "openspec")) {
				t.Errorf("the ldd-native copy is not the native layout")
			}
			for _, d := range []string{"openspec", "ldd", "pyproject.toml"} {
				if fileExists(filepath.Join(ask, d)) {
					t.Errorf("the ldd-ask copy has %s before the run", d)
				}
			}
			for _, repo := range []string{native, ask} {
				if err := gitRepo(repo, append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")); err != nil {
					t.Fatal(err)
				}
			}
			s := status(t, native)
			ldd, _ := s["ldd"].(map[string]any)
			tdd, _ := s["strict_tdd"].(map[string]any)
			if ldd["active"] != true || tdd["value"] != true || tdd["evidence"] != "ok" || !strings.Contains(str(tdd["reason"]), "ldd/config.yaml") {
				t.Fatalf("ldd-native copy: ldd %v strict_tdd %v", ldd, tdd)
			}
			if got := repoLayout(native); got != "ldd" {
				t.Errorf("ldd-native layout %q", got)
			}
			if ldd, _ := status(t, ask)["ldd"].(map[string]any); ldd["active"] == true {
				t.Errorf("ldd-ask copy is LDD-active before the run: %v", ldd)
			}
			if got := repoLayout(ask); got != "none" {
				t.Errorf("ldd-ask layout %q before the run", got)
			}
		})
	}
}

func TestTheAskVariantAsksForLDDInTheFirstMessageOnly(t *testing.T) {
	f := newFake(t)
	o, _ := options(t, f, "invoice-slices", []string{"jhon", "clean", "pi-jhon"}, 1)
	o.Variants = []string{"ldd-ask", "ldd-native"}
	runs, err := Plan(o)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range runs {
		names = append(names, r.Name())
	}
	if want := []string{"invoice-slices-ldd-ask-jhon-1", "invoice-slices-ldd-ask-pi-jhon-1", "invoice-slices-ldd-native-jhon-1", "invoice-slices-ldd-native-pi-jhon-1"}; !slices.Equal(names, want) {
		t.Fatalf("plan %v, want %v (clean runs plain only)", names, want)
	}
	orig := o.Tasks[0].Prompts
	ask := runs[0].Task.Prompts
	if AskSentence != "Use LDD for this work." || ask[0] != AskSentence+"\n\n"+orig[0] || !slices.Equal(ask[1:], orig[1:]) || runs[0].Task.Prompt != ask[0] {
		t.Fatalf("ldd-ask prompts %q", ask)
	}
	if !slices.Contains(runs[0].Argv, ask[0]) || !strings.Contains(runs[0].Next[0][3], orig[1]) {
		t.Errorf("argv %v", runs[0].Argv)
	}
	if runs[2].Task.Prompts[0] != orig[0] || o.Tasks[0].Prompt != orig[0] {
		t.Errorf("the sentence leaked into ldd-native or the task: %q", runs[2].Task.Prompts[0])
	}
}

func TestAnAskFakeRunSendsTheSentenceAndKeepsItsRecordInScope(t *testing.T) {
	f := newFake(t, "0.40")
	o, out := options(t, f, "pagination-bug", []string{"jhon"}, 1)
	o.Variants = []string{"ldd-ask"}
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	calls := strings.Join(f.calls(t), "\n")
	if !strings.Contains(calls, "-p Use LDD for this work.\n\nBug report:") {
		t.Errorf("the first message lacks the sentence:\n%s", calls)
	}
	if m := metricsOf(t, o, "pagination-bug-ldd-ask-jhon-1"); m.Variant != "ldd-ask" {
		t.Errorf("variant %q", m.Variant)
	}
	task := o.Tasks[0]
	for _, v := range []string{"ldd-ask", "ldd-native"} {
		var m Metrics
		m.fromChanges(task, v, "jhon", []string{"ldd/changes/x/tasks.md", "openspec/changes/x/tasks.md", "pager.py", "other.py"})
		if !slices.Equal(m.OutOfScope, []string{"other.py"}) {
			t.Errorf("%s: out of scope %v", v, m.OutOfScope)
		}
	}
}
