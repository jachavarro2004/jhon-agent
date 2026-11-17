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

// glmProfiles is an owner-supplied profiles.json: one all-GLM profile, in
// internal/profiles' shape (tiers.<runtime>.<tier>).
const glmProfiles = `{
  "profiles": {
    "glm": {
      "tiers": {
        "pi": {
          "cheap": "zai/glm-5.3-flash",
          "mid": "zai/glm-5.3-flash",
          "strong": "zai/glm-5.3",
          "frontier": "zai/glm-5.3"
        }
      }
    }
  }
}
`

// useGLM installs glmProfiles in f's bench HOME as `bench prepare
// --pi-profile --profile glm` does.
func useGLM(t *testing.T, f fake) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "profiles.json")
	if err := os.WriteFile(src, []byte(glmProfiles), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := UseProfile(f.home, src, "glm", os.Environ(), &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
}

func TestPrepareProfileCopiesTheFileAndUsesItInTheBenchHome(t *testing.T) {
	f := newFake(t)
	t.Setenv("JHON_PROFILES", "/owner/.config/jhon/profiles.json")
	src := filepath.Join(t.TempDir(), "profiles.json")
	if err := os.WriteFile(src, []byte(glmProfiles), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := UseProfile(f.home, src, "glm", os.Environ(), &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if b, err := os.ReadFile(filepath.Join(f.home, ".config", "jhon", "profiles.json")); err != nil || string(b) != glmProfiles {
		t.Errorf("the bench HOME's profiles.json: %q %v", b, err)
	}
	calls := strings.Join(f.calls(t), "\n")
	for _, want := range []string{"jhon profile use glm |HOME=" + f.home + "|",
		"profile env |XDG_CONFIG_HOME=" + filepath.Join(f.home, ".config") + "|JHON_PROFILES=|PWD=" + f.home} {
		if !strings.Contains(calls, want) {
			t.Errorf("calls lack %q:\n%s", want, calls)
		}
	}
	if !strings.Contains(out.String(), "profile glm active in the bench HOME "+f.home) {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestPrepareProfileRefusesBadInput(t *testing.T) {
	f := newFake(t)
	dir := t.TempDir()
	notJSON := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(notJSON, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ file, name, want string }{
		{notJSON, "glm", "is not a JSON object"},
		{filepath.Join(dir, "absent.json"), "glm", "absent.json"},
		{"", "", "--profile"},
	} {
		if err := UseProfile(f.home, c.file, c.name, os.Environ(), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q %q: %v", c.file, c.name, err)
		}
	}
	if f.calls(t) != nil {
		t.Errorf("jhon ran: %v", f.calls(t))
	}
	// A profile jhon refuses is an error.
	t.Setenv("FAKE_PROFILE_EXIT", "1")
	src := filepath.Join(dir, "p.json")
	_ = os.WriteFile(src, []byte(glmProfiles), 0o600)
	if err := UseProfile(f.home, src, "ghost", os.Environ(), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "profile use ghost") {
		t.Errorf("refused profile: %v", err)
	}
	// --pi-profile needs --profile; checked before any install.
	var out, errOut bytes.Buffer
	if code := cli([]string{"prepare", "--home", f.home, "--pi-profile", src}, &out, &errOut, time.Now()); code != 2 || !strings.Contains(errOut.String(), "--pi-profile needs --profile") {
		t.Errorf("exit %d: %s", code, errOut.String())
	}
}

func TestRunsRecordTheBenchHomesProfile(t *testing.T) {
	f := newFake(t)
	t.Setenv("ZAI_API_KEY", secretValue)
	useGLM(t, f)
	o, out := options(t, f, "pagination-bug", []string{"jhon", "pi-jhon", "pi-clean"}, 1)
	o.PiEnv = []string{"ZAI_API_KEY"}
	runs, err := Plan(o)
	if err != nil {
		t.Fatal(err)
	}
	// No --pi-model: pi's main session is the profile's orchestrator model.
	if i := slices.Index(runs[1].Argv, "--provider"); i < 0 || runs[1].Argv[i+1] != "zai" || runs[1].Argv[i+3] != "glm-5.3" {
		t.Errorf("pi-jhon argv %q", runs[1].Argv)
	}
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for name, want := range map[string][2]string{
		"pagination-bug-plain-jhon-1":     {"haiku", "glm"},
		"pagination-bug-plain-pi-jhon-1":  {"zai/glm-5.3", "glm"},
		"pagination-bug-plain-pi-clean-1": {"zai/glm-5.3", ""},
	} {
		if m := metricsOf(t, o, name); m.Model != want[0] || m.Profile != want[1] {
			t.Errorf("%s model %q profile %q, want %q", name, m.Model, m.Profile, want)
		}
	}
	if !strings.Contains(out.String(), "profile: glm") {
		t.Errorf("output lacks the profile:\n%s", out)
	}
}

func TestDryRunShowsTheProfileAndThatPiModelSetsTheMainSessionOnly(t *testing.T) {
	f := newFake(t)
	useGLM(t, f)
	t.Setenv("HOME", t.TempDir())
	run := func(extra ...string) string {
		var out, errOut bytes.Buffer
		args := append([]string{"run", "--home", f.home, "--tasks", "pagination-bug", "--variants", "plain", "--conditions", "jhon,pi-jhon", "--dry-run"}, extra...)
		if code := cli(args, &out, &errOut, time.Now()); code != 0 {
			t.Fatalf("exit %d: %s", code, errOut.String())
		}
		return out.String()
	}
	o := run("--pi-model", "zai/glm-5.3-flash")
	for _, s := range []string{"profile: glm (the bench HOME's active Jhon profile: the sub-agents of jhon, pi-jhon)",
		"--pi-model zai/glm-5.3-flash sets pi-jhon's main session only; its sub-agents run profile glm's models", "--provider zai --model glm-5.3-flash"} {
		if !strings.Contains(o, s) {
			t.Errorf("dry run lacks %q:\n%s", s, o)
		}
	}
	o = run()
	if s := "pi main session: zai/glm-5.3, profile glm's orchestrator model"; !strings.Contains(o, s) {
		t.Errorf("dry run lacks %q:\n%s", s, o)
	}
}

// Without a profile in the bench HOME, nothing changes: no profile in the
// metrics, the dry run or the argv.
func TestNoProfileKeepsRunsAsBefore(t *testing.T) {
	f := newFake(t)
	putBridge(t, f)
	o, out := options(t, f, "pagination-bug", []string{"jhon", "pi-jhon"}, 1)
	o.claudePath = "/usr/bin/claude"
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, name := range []string{"pagination-bug-plain-jhon-1", "pagination-bug-plain-pi-jhon-1"} {
		b, _ := os.ReadFile(filepath.Join(o.Results, "runs", name, "metrics.json"))
		if bytes.Contains(b, []byte(`"profile"`)) {
			t.Errorf("%s metrics carry a profile", name)
		}
	}
	if strings.Contains(out.String(), "profile") {
		t.Errorf("output names a profile:\n%s", out)
	}
	for _, c := range f.calls(t) {
		if strings.HasPrefix(c, "jhon profile") {
			t.Errorf("a profile command ran: %s", c)
		}
	}
	o2, dry := options(t, f, "pagination-bug", []string{"jhon", "pi-jhon"}, 1)
	o2.DryRun = true
	if err := Execute(o2); err != nil || strings.Contains(dry.String(), "profile") || !strings.Contains(dry.String(), "--provider claude-bridge --model haiku") {
		t.Errorf("%v dry run:\n%s", err, dry)
	}
}

func TestValueAndReportKeepProfilesApart(t *testing.T) {
	glm, plain := t.TempDir(), t.TempDir()
	modelRuns(t, glm, "pi-jhon", "zai/glm-5.3", 1, func(m *Metrics) { m.Profile = "glm" })
	modelRuns(t, glm, "pi-clean", "zai/glm-5.3", 1, func(m *Metrics) {
		if m.Repeat == 1 {
			m.RedBeforeGreen = bp(false)
		}
	})
	// The same main model without the profile leaves a file out of scope.
	modelRuns(t, plain, "pi-jhon", "zai/glm-5.3", 1, func(m *Metrics) { m.OutOfScopeFiles = ip(1) })
	code, out, errOut := value(t, glm, plain)
	for _, s := range []string{
		"PASS  invoice-slices plain pi-jhon vs pi-clean plain, model zai/glm-5.3 profile glm",
		"FAIL  invoice-slices plain pi-jhon vs pi-clean plain, model zai/glm-5.3 (large)\n",
		"runtime pi zai/glm-5.3 profile glm (pi-jhon vs pi-clean): PASS — 1/1 cells",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("output lacks %q:\n%s%s", s, out, errOut)
		}
	}
	if code != 1 {
		t.Errorf("exit %d", code)
	}
	var rep bytes.Buffer
	if err := Report(glm, &rep); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rep.String(), "pi-jhon zai/glm-5.3 profile glm") {
		t.Errorf("report:\n%s", rep.String())
	}
}

func TestCertifyRefusesCellsWhoseProfileChanged(t *testing.T) {
	base, cand := t.TempDir(), t.TempDir()
	modelRuns(t, base, "pi-jhon", "zai/glm-5.3", 1, nil)
	modelRuns(t, cand, "pi-jhon", "zai/glm-5.3", 1, func(m *Metrics) { m.Profile = "glm" })
	code, out, errOut := certify(t, nil, "--baseline", base, "--candidate", cand)
	want := "invoice-slices plain pi-jhon: the baseline ran zai/glm-5.3, the candidate zai/glm-5.3 profile glm; certify compares a model with itself (--allow-model-change to compare them anyway)"
	if code != 2 || !strings.Contains(errOut, want) {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	code, out, errOut = certify(t, nil, "--baseline", base, "--candidate", cand, "--allow-model-change")
	if !strings.Contains(out, "note: the model changed: invoice-slices plain pi-jhon zai/glm-5.3 → zai/glm-5.3 profile glm") {
		t.Errorf("exit %d\n%s\n%s", code, out, errOut)
	}
}
