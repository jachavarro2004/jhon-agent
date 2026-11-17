package main

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// secretValue stands in for a provider key: it must never reach an output.
const secretValue = "sk-bench-secret-never-printed"

func TestPiModelSetsThePiConditionsProviderAndModel(t *testing.T) {
	f := newFake(t)
	pi, err := exec.LookPath("pi")
	if err != nil {
		t.Fatal(err)
	}
	o, _ := options(t, f, "pagination-bug", []string{"jhon", "pi-jhon", "pi-clean"}, 1)
	o.claudePath = "/usr/bin/claude"
	o.PiModel, o.PiEnv = "zai/glm-5.3", []string{"ZAI_API_KEY"}
	runs, err := Plan(o)
	if err != nil || len(runs) != 3 {
		t.Fatalf("plan %v %v", runs, err)
	}
	p := runs[0].Task.Prompt
	if !slices.Contains(runs[0].Argv, "haiku") || slices.Contains(runs[0].Argv, "zai") || runs[0].Model != "haiku" {
		t.Errorf("jhon argv %q model %s", runs[0].Argv, runs[0].Model)
	}
	pj := []string{benchJhon(f.home), "start", "pi", "--mode", "json", "--provider", "zai", "--model", "glm-5.3", "--session-id", runs[1].SessionID, "--", p}
	if !slices.Equal(runs[1].Argv, pj) || runs[1].Env != nil || runs[1].Model != "zai/glm-5.3" || !slices.Equal(runs[1].PassEnv, []string{"ZAI_API_KEY"}) {
		t.Errorf("pi-jhon argv\n%q\nwant\n%q\nenv %q model %s pass %q", runs[1].Argv, pj, runs[1].Env, runs[1].Model, runs[1].PassEnv)
	}
	pc := []string{pi, "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-themes", "--no-mcp",
		"--mode", "json", "--provider", "zai", "--model", "glm-5.3", "--session-id", runs[2].SessionID, "--", p}
	if !slices.Equal(runs[2].Argv, pc) || !slices.Equal(runs[2].Env, []string{"PI_CODING_AGENT_DIR=" + piAgentDir(runs[2])}) || runs[2].Model != "zai/glm-5.3" {
		t.Errorf("pi-clean argv\n%q\nwant\n%q\nenv %q", runs[2].Argv, pc, runs[2].Env)
	}
	o.PiModel = "zai/glm-5.3@high"
	runs, _ = Plan(o)
	if !slices.Contains(runs[1].Argv, "--thinking") || runs[1].Argv[slices.Index(runs[1].Argv, "--thinking")+1] != "high" ||
		!slices.Contains(runs[2].Argv, "--thinking") || runs[2].Model != "zai/glm-5.3@high" {
		t.Errorf("thinking argv %q %q", runs[1].Argv, runs[2].Argv)
	}
	for _, bad := range []string{"glm-5.3", "zai/", "/glm", "zai/glm@"} {
		o.PiModel = bad
		if _, err := Plan(o); err == nil || !strings.Contains(err.Error(), "--pi-model") {
			t.Errorf("--pi-model %q: %v", bad, err)
		}
	}
	o.PiModel, o.PiEnv = "", []string{"BAD NAME"}
	if _, err := Plan(o); err == nil || !strings.Contains(err.Error(), "--pi-env") {
		t.Errorf("--pi-env bad name: %v", err)
	}
}

// With no --pi-model the pi conditions run claude-bridge as before, and
// record it as the effective model.
func TestNoPiModelKeepsClaudeBridge(t *testing.T) {
	f := newFake(t)
	putBridge(t, f)
	o, out := options(t, f, "pagination-bug", []string{"pi-jhon", "pi-clean"}, 1)
	runs, err := Plan(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range runs {
		if i := slices.Index(r.Argv, "--provider"); i < 0 || r.Argv[i+1] != "claude-bridge" || r.Argv[i+3] != "haiku" || slices.Contains(r.Argv, "--thinking") || r.PassEnv != nil {
			t.Errorf("%s argv %q", r.Name(), r.Argv)
		}
	}
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, name := range []string{"pagination-bug-plain-pi-jhon-1", "pagination-bug-plain-pi-clean-1"} {
		if m := metricsOf(t, o, name); m.Model != "claude-bridge/haiku" || m.PiEnv != nil {
			t.Errorf("%s model %q pi env %q", name, m.Model, m.PiEnv)
		}
	}
}

func TestPiModelRunsPassTheKeyByNameOnly(t *testing.T) {
	f := newFake(t)
	// A pi that logs whether the key reached it, never the key.
	logKey := strings.Replace(fakePi, `>> "$FAKE_DIR/calls.log"`, `>> "$FAKE_DIR/calls.log"; [ "$ZAI_API_KEY" = `+secretValue+` ] && echo "pi has ZAI_API_KEY" >> "$FAKE_DIR/calls.log"`, 1)
	if err := os.WriteFile(filepath.Join(f.dir, "bin", "pi"), []byte(logKey), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZAI_API_KEY", secretValue)
	sol, _ := filepath.Abs(filepath.Join("testdata", "solutions", "pagination-bug"))
	t.Setenv("FAKE_SOLUTION", sol)
	o, out := options(t, f, "pagination-bug", []string{"pi-jhon", "pi-clean"}, 1)
	o.PiModel, o.PiEnv = "zai/glm-5.3", []string{"ZAI_API_KEY"}
	// No bridge in the bench HOME: pi-clean on zai needs none.
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, name := range []string{"pagination-bug-plain-pi-jhon-1", "pagination-bug-plain-pi-clean-1"} {
		m := metricsOf(t, o, name)
		if m.Model != "zai/glm-5.3" || !slices.Equal(m.PiEnv, []string{"ZAI_API_KEY"}) || !m.Success {
			t.Errorf("%s model %q pi env %q success %v problems %v", name, m.Model, m.PiEnv, m.Success, m.Problems)
		}
	}
	agent := filepath.Join(o.Results, "runs", "pagination-bug-plain-pi-clean-1", "pi-agent")
	if _, err := os.Stat(filepath.Join(agent, "claude-bridge.json")); err == nil {
		t.Errorf("pi-clean on zai wrote claude-bridge.json")
	}
	if n := strings.Count(strings.Join(f.calls(t), "\n"), "pi has ZAI_API_KEY"); n != 2 {
		t.Errorf("the key reached pi %d times, want 2:\n%s", n, strings.Join(f.calls(t), "\n"))
	}
	if strings.Contains(out.String(), secretValue) {
		t.Errorf("output carries the key")
	}
	if err := Report(o.Results, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	_ = filepath.WalkDir(o.Results, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if b, _ := os.ReadFile(p); bytes.Contains(b, []byte(secretValue)) {
				t.Errorf("%s carries the key", p)
			}
		}
		return nil
	})
}

func TestPiModelRefusesAnUnsetEnvName(t *testing.T) {
	f := newFake(t)
	t.Setenv("ZAI_API_KEY", "")
	os.Unsetenv("ZAI_API_KEY")
	o, out := options(t, f, "pagination-bug", []string{"pi-clean"}, 1)
	o.PiModel, o.PiEnv = "zai/glm-5.3", []string{"ZAI_API_KEY"}
	if err := Execute(o); err == nil || !strings.Contains(err.Error(), "--pi-env ZAI_API_KEY is not set") {
		t.Fatalf("err %v\n%s", err, out)
	}
	if f.calls(t) != nil {
		t.Errorf("a run started: %v", f.calls(t))
	}
}

func TestDryRunShowsThePiModelAndTheEnvNames(t *testing.T) {
	f := newFake(t)
	t.Setenv("ZAI_API_KEY", secretValue)
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	now := time.Date(2026, 11, 13, 1, 2, 3, 0, time.UTC)
	code := cli([]string{"run", "--home", f.home, "--tasks", "pagination-bug", "--variants", "plain",
		"--conditions", "pi-jhon,pi-clean", "--pi-model", "zai/glm-5.3", "--pi-env", "ZAI_API_KEY", "--dry-run"}, &out, &errOut, now)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, s := range []string{"pi model: zai/glm-5.3 (pi-jhon, pi-clean); passed from the environment: ZAI_API_KEY",
		"20261113T010203Z-zai-glm-5.3", "--provider zai --model glm-5.3", "pass env: ZAI_API_KEY"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("dry run lacks %q:\n%s", s, out.String())
		}
	}
	if strings.Contains(out.String(), secretValue) || strings.Contains(out.String(), "claude-bridge") {
		t.Errorf("dry run:\n%s", out.String())
	}
}

// modelRuns writes 3 runs of cond on model under dir, each in a directory
// named for the model too.
func modelRuns(t *testing.T, dir, cond, model string, cost float64, edit func(m *Metrics)) {
	t.Helper()
	for rep := 1; rep <= 3; rep++ {
		m := cellRun("plain", cond, rep, cost)
		m.Model = model
		if edit != nil {
			edit(&m)
		}
		d := filepath.Join(dir, "runs", strings.NewReplacer("/", "-").Replace(m.Task+"-plain-"+cond+"-"+model)+"-"+string(rune('0'+rep)))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := writeJSON(filepath.Join(d, "metrics.json"), m); err != nil {
			t.Fatal(err)
		}
	}
}

func TestValueComparesPiJhonWithPiCleanOfTheSameModel(t *testing.T) {
	dir := t.TempDir()
	modelRuns(t, dir, "pi-jhon", "zai/glm-5.3", 1, nil)
	// Jhon on Opus leaves a file out of scope; GLM's clean runs miss RED
	// before GREEN once, Opus's never: merged, the cells would mix them.
	modelRuns(t, dir, "pi-jhon", "claude-bridge/opus", 1, func(m *Metrics) { m.OutOfScopeFiles = ip(1) })
	modelRuns(t, dir, "pi-clean", "zai/glm-5.3", 0.5, func(m *Metrics) {
		if m.Repeat == 1 {
			m.RedBeforeGreen = bp(false)
		}
	})
	modelRuns(t, dir, "pi-clean", "opus", 0.5, nil)
	code, out, errOut := value(t, dir)
	for _, s := range []string{
		"PASS  invoice-slices plain pi-jhon vs pi-clean plain, model zai/glm-5.3",
		"FAIL  invoice-slices plain pi-jhon vs pi-clean plain, model claude-bridge/opus",
		"runtime pi zai/glm-5.3 (pi-jhon vs pi-clean): PASS — 1/1 cells",
		"runtime pi claude-bridge/opus (pi-jhon vs pi-clean): FAIL — 0/1 cells",
		"verdict: FAIL",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("output lacks %q:\n%s%s", s, out, errOut)
		}
	}
	if code != 1 {
		t.Errorf("exit %d", code)
	}
	var rep bytes.Buffer
	if err := Report(dir, &rep); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"pi-jhon zai/glm-5.3", "pi-jhon claude-bridge/opus", "pi-clean opus"} {
		if !strings.Contains(rep.String(), s) {
			t.Errorf("report lacks %q:\n%s", s, rep.String())
		}
	}
}

func TestCertifyRefusesCellsWhoseModelChanged(t *testing.T) {
	base, cand := t.TempDir(), t.TempDir()
	modelRuns(t, base, "pi-jhon", "claude-bridge/opus", 1, nil)
	modelRuns(t, cand, "pi-jhon", "zai/glm-5.3", 1, nil)
	code, out, errOut := certify(t, nil, "--baseline", base, "--candidate", cand)
	want := "invoice-slices plain pi-jhon: the baseline ran claude-bridge/opus, the candidate zai/glm-5.3; certify compares a model with itself (--allow-model-change to compare them anyway)"
	if code != 2 || !strings.Contains(errOut, want) {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	code, out, errOut = certify(t, nil, "--baseline", base, "--candidate", cand, "--allow-model-change")
	if strings.Contains(errOut, "model") || !strings.Contains(out, "note: the model changed: invoice-slices plain pi-jhon claude-bridge/opus → zai/glm-5.3") ||
		!strings.HasPrefix(line(out, "repeats"), "PASS") {
		t.Errorf("exit %d\n%s\n%s", code, out, errOut)
	}
	// claude-bridge/opus and opus are one model; and the same model on both
	// sides needs no flag.
	same := t.TempDir()
	modelRuns(t, same, "pi-jhon", "opus", 1, nil)
	if code, out, errOut := certify(t, nil, "--baseline", base, "--candidate", same); code == 2 || strings.Contains(out, "model changed") {
		t.Errorf("same model: exit %d\n%s\n%s", code, out, errOut)
	}
}
