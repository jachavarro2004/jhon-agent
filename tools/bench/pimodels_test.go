package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeModelsSecret stands in for a provider apiKey command: it must never
// run or reach an output.
const fakeModelsSecret = "!fake-secret-tool lookup provider fake sk-never-printed"

// fakePiModels is an owner's pi models.json with a fake provider (T146 F5-b).
const fakePiModels = `{
  "providers": {
    "fake": {
      "baseUrl": "http://127.0.0.1:9/v1",
      "api": "openai-completions",
      "apiKey": "` + fakeModelsSecret + `",
      "models": [{"id": "glm-main"}, {"id": "glm-flash"}]
    }
  }
}
`

// piModelsFile writes body to a fresh models file and returns its path.
func piModelsFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// benchModels is where pi-jhon, its sub-agents included, reads the models.
func benchModels(home string) string { return filepath.Join(home, ".pi", "jhon", "models.json") }

// noSecret fails when s carries the provider key's command.
func noSecret(t *testing.T, what, s string) {
	t.Helper()
	if strings.Contains(s, "sk-never-printed") || strings.Contains(s, "fake-secret-tool") {
		t.Errorf("%s carries the models file's apiKey:\n%s", what, s)
	}
}

// private checks file is want byte for byte with mode 0600.
func private(t *testing.T, file, want string) {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil || string(b) != want {
		t.Errorf("%s: %q %v", file, b, err)
		return
	}
	if fi, err := os.Stat(file); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("%s mode %v %v, want 0600", file, fi.Mode().Perm(), err)
	}
}

func piModelsLog(t *testing.T, f fake) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(f.dir, "pi-models.log"))
	return string(b)
}

func TestPreparePutsThePiModelsFileInTheBenchHome(t *testing.T) {
	f := newFake(t)
	src := piModelsFile(t, fakePiModels)
	var out bytes.Buffer
	if err := PutPiModels(f.home, src, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	private(t, benchModels(f.home), fakePiModels)
	if !strings.Contains(out.String(), "pi models file "+src+" copied to "+benchModels(f.home)) {
		t.Errorf("output:\n%s", out.String())
	}
	noSecret(t, "prepare's output", out.String())
	// A models file already there, readable by all, is replaced and private.
	if err := os.Chmod(benchModels(f.home), 0o644); err != nil {
		t.Fatal(err)
	}
	other := piModelsFile(t, `{"providers":{}}`)
	if err := PutPiModels(f.home, other, &out); err != nil {
		t.Fatal(err)
	}
	private(t, benchModels(f.home), `{"providers":{}}`)
	if b, _ := os.ReadFile(src); string(b) != fakePiModels {
		t.Errorf("the owner's file changed: %s", b)
	}
}

func TestPiCleanRunsGetThePiModelsFileBeforePiStarts(t *testing.T) {
	f := newFake(t)
	src := piModelsFile(t, fakePiModels)
	t.Setenv("FAKE_PI_MODELS", src)
	if err := PutPiModels(f.home, src, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	o, out := options(t, f, "pagination-bug", []string{"pi-jhon", "pi-clean"}, 2)
	o.PiModel, o.PiModels = "fake/glm-main", src
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	log := piModelsLog(t, f)
	for _, name := range []string{"pagination-bug-plain-pi-clean-1", "pagination-bug-plain-pi-clean-2"} {
		agent := filepath.Join(o.Results, "runs", name, "pi-agent")
		if !strings.Contains(log, agent+" models=same\n") {
			t.Errorf("%s's pi did not see the models file:\n%s", name, log)
		}
		private(t, filepath.Join(agent, "models.json"), fakePiModels)
		m, _ := os.ReadFile(filepath.Join(o.Results, "runs", name, "metrics.json"))
		noSecret(t, name+"'s metrics.json", string(m))
	}
	if n := strings.Count(log, filepath.Join(f.home, ".pi", "jhon")+" models=same\n"); n != 2 {
		t.Errorf("pi-jhon saw the bench HOME's models file %d times, want 2:\n%s", n, log)
	}
	noSecret(t, "the run's output", out.String())
}

func TestWithoutPiModelsNothingChanges(t *testing.T) {
	f := newFake(t)
	src := piModelsFile(t, fakePiModels)
	plan := func(models string) []*Run {
		o, _ := options(t, f, "pagination-bug", []string{"pi-jhon", "pi-clean"}, 1)
		o.PiModel, o.PiModels, o.Results = "fake/glm-main", models, "/results"
		runs, err := Plan(o)
		if err != nil {
			t.Fatal(err)
		}
		return runs
	}
	if err := PutPiModels(f.home, src, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	with, without := plan(src), plan("")
	for i := range with {
		if !slices.Equal(with[i].Argv, without[i].Argv) || !slices.Equal(with[i].Env, without[i].Env) || with[i].Model != without[i].Model {
			t.Errorf("--pi-models changed %s's command:\n%q %q\n%q %q", with[i].Name(), with[i].Argv, with[i].Env, without[i].Argv, without[i].Env)
		}
	}
	// A bench HOME prepared without the option holds no models file, and a
	// run without it copies none.
	f = newFake(t)
	o, out := options(t, f, "pagination-bug", []string{"pi-jhon", "pi-clean"}, 1)
	o.PiModel = "fake/glm-main"
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	agent := filepath.Join(o.Results, "runs", "pagination-bug-plain-pi-clean-1", "pi-agent")
	if log := piModelsLog(t, f); !strings.Contains(log, agent+" models=absent\n") || !strings.Contains(log, filepath.Join(f.home, ".pi", "jhon")+" models=absent\n") {
		t.Errorf("a models file appeared:\n%s", log)
	}
	if _, err := os.Stat(benchModels(f.home)); !os.IsNotExist(err) {
		t.Errorf("the bench HOME has a models file: %v", err)
	}
	o, out = options(t, f, "pagination-bug", []string{"pi-jhon", "pi-clean"}, 1)
	o.PiModel, o.DryRun = "fake/glm-main", true
	if err := Execute(o); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "models file") || strings.Contains(out.String(), "models per condition") {
		t.Errorf("the dry run without --pi-models changed:\n%s", out.String())
	}
}

func TestAPiModelsFileThatIsNoObjectIsRefusedNamingIt(t *testing.T) {
	f := newFake(t)
	for _, body := range []string{`["` + fakeModelsSecret + `"]`, `null`, `"` + fakeModelsSecret + `"`, `{"apiKey": "` + fakeModelsSecret + `"`} {
		src := piModelsFile(t, body)
		var out, errOut bytes.Buffer
		code := cli([]string{"run", "--home", f.home, "--tasks", "pagination-bug", "--conditions", "pi-jhon,pi-clean", "--pi-model", "fake/glm-main",
			"--pi-models", src, "--dry-run"}, &out, &errOut, time.Now())
		if code != 1 || !strings.Contains(errOut.String(), "--pi-models "+src+" is not a JSON object") {
			t.Errorf("run with %s: exit %d: %s", body, code, errOut.String())
		}
		noSecret(t, "run's refusal", out.String()+errOut.String())
		// prepare refuses it before any install.
		home := filepath.Join(t.TempDir(), "home")
		out.Reset()
		errOut.Reset()
		code = cli([]string{"prepare", "--home", home, "--pi-models", src}, &out, &errOut, time.Now())
		if code != 1 || !strings.Contains(errOut.String(), "--pi-models "+src+" is not a JSON object") {
			t.Errorf("prepare with %s: exit %d: %s", body, code, errOut.String())
		}
		noSecret(t, "prepare's refusal", out.String()+errOut.String())
		if _, err := os.Stat(home); !os.IsNotExist(err) {
			t.Errorf("prepare made %s before refusing: %v", home, err)
		}
		if err := PutPiModels(f.home, src, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), src) {
			t.Errorf("PutPiModels %s: %v", body, err)
		}
	}
	absent := filepath.Join(t.TempDir(), "absent.json")
	var out, errOut bytes.Buffer
	if code := cli([]string{"run", "--home", f.home, "--tasks", "pagination-bug", "--conditions", "pi-clean", "--pi-models", absent, "--dry-run"}, &out, &errOut, time.Now()); code != 1 || !strings.Contains(errOut.String(), absent) {
		t.Errorf("absent file: exit %d: %s", code, errOut.String())
	}
	if _, err := os.Stat(benchModels(f.home)); !os.IsNotExist(err) {
		t.Errorf("a refused file reached the bench HOME: %v", err)
	}
	if c := f.calls(t); c != nil {
		t.Errorf("something ran: %v", c)
	}
}

// pi-jhon reads the bench HOME's models file: a run whose --pi-models is not
// the one there refuses to start, so both conditions resolve the same provider.
func TestARunRefusesPiJhonOnAnotherModelsFile(t *testing.T) {
	f := newFake(t)
	src := piModelsFile(t, fakePiModels)
	for _, there := range []string{"", `{"providers":{}}`} {
		if there != "" {
			if err := PutPiModels(f.home, piModelsFile(t, there), &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
		}
		o, out := options(t, f, "pagination-bug", []string{"pi-jhon", "pi-clean"}, 1)
		o.PiModel, o.PiModels = "fake/glm-main", src
		err := Execute(o)
		if err == nil || !strings.Contains(err.Error(), benchModels(f.home)) || !strings.Contains(err.Error(), "bench prepare --pi-models "+src) {
			t.Errorf("bench HOME %q: %v\n%s", there, err, out)
		}
		if log := piModelsLog(t, f); log != "" {
			t.Errorf("pi started:\n%s", log)
		}
	}
}

func TestDryRunNamesThePiModelsFileAndEachConditionsModel(t *testing.T) {
	f := newFake(t)
	src := piModelsFile(t, fakePiModels)
	agents := filepath.Join(f.home, ".pi", "jhon", "agents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, fm := range map[string]string{"explore": "model: fake/glm-flash", "worker": "model: fake/glm-flash", "verify": "model: fake/glm-main\nthinking: high"} {
		body := "---\nname: " + name + "\ndescription: \"x\"\n" + fm + "\ntools:\n  - read\n---\nbody\n"
		if err := os.WriteFile(filepath.Join(agents, name+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"run", "--home", f.home, "--tasks", "pagination-bug", "--variants", "plain", "--model", "haiku",
		"--conditions", "jhon,pi-jhon,pi-clean", "--pi-model", "fake/glm-main", "--pi-models", src, "--dry-run"}
	var out, errOut bytes.Buffer
	if code := cli(args, &out, &errOut, time.Now()); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, s := range []string{
		"pi models file: " + src + " (a JSON object; its contents are never printed)",
		"pi-jhon and its sub-agents read " + benchModels(f.home) + ": absent; run `bench prepare --pi-models " + src + "` first",
		"pi-clean: copied to each run's pi-agent/models.json (mode 0600) before pi starts",
		"models per condition:",
		"  jhon: haiku (Claude Code)",
		"  pi-jhon: main session fake/glm-main; sub-agents fake/glm-flash (explore, worker), fake/glm-main@high (verify)",
		"  pi-clean: fake/glm-main, no sub-agents",
	} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("dry run lacks %q:\n%s", s, out.String())
		}
	}
	noSecret(t, "the dry run", out.String())
	if err := PutPiModels(f.home, src, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := cli(args, &out, &errOut, time.Now()); code != 0 || !strings.Contains(out.String(), "pi-jhon and its sub-agents read "+benchModels(f.home)+": the same file") {
		t.Errorf("exit %d, dry run:\n%s", code, out.String())
	}
	if c := f.calls(t); c != nil {
		t.Errorf("a dry run started something: %v", c)
	}
}
