package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// putBridge leaves pi-claude-bridge's extension and the provider wrapper
// where `bench prepare` installs them in the bench HOME.
func putBridge(t *testing.T, f fake) {
	t.Helper()
	for _, p := range []string{piBridgeExtension(f.home), piProviderWrapper(f.home)} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("// fake\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPiCleanStartsPiWithNoJhon(t *testing.T) {
	f := newFake(t)
	pi, err := exec.LookPath("pi")
	if err != nil {
		t.Fatal(err)
	}
	o, _ := options(t, f, "pagination-bug", []string{"pi-clean"}, 1)
	runs, err := Plan(o)
	if err != nil || len(runs) != 1 {
		t.Fatalf("plan %v %v", runs, err)
	}
	r := runs[0]
	bridge := filepath.Join(f.home, ".pi", "jhon", "npm", "node_modules", "pi-claude-bridge", "src", "index.ts")
	want := []string{pi, "--no-extensions", "--extension", bridge, "--no-skills", "--no-prompt-templates", "--no-themes", "--no-mcp",
		"--mode", "json", "--provider", "claude-bridge", "--model", "haiku", "--session-id", r.SessionID, "--", r.Task.Prompt}
	if !slices.Equal(r.Argv, want) {
		t.Errorf("argv\n%q\nwant\n%q", r.Argv, want)
	}
	agent := filepath.Join(r.Dir, "pi-agent")
	if !slices.Equal(r.Env, []string{"PI_CODING_AGENT_DIR=" + agent}) {
		t.Errorf("env %q", r.Env)
	}
	for _, a := range append(slices.Clone(r.Argv), r.Env...) {
		for _, jhon := range []string{benchJhon(f.home), "jhon-orchestrator", "append-system-prompt", "/shell", "prose/skills", "mcp.json", "--plugin-dir"} {
			if strings.Contains(a, jhon) {
				t.Errorf("pi-clean carries %q in %q", jhon, a)
			}
		}
	}
	o.Tasks = []Task{multiTask(t, "")}
	runs, err = Plan(o)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{pi, "--no-extensions", "--extension", bridge, "--no-skills", "--no-prompt-templates", "--no-themes", "--no-mcp",
		"--mode", "rpc", "--provider", "claude-bridge", "--model", "haiku", "--session-id", runs[0].SessionID}; !slices.Equal(runs[0].Argv, want) || !runs[0].RPC {
		t.Errorf("multi-message argv %q rpc %v", runs[0].Argv, runs[0].RPC)
	}
}

// The conditions that were there before pi-clean start as they did.
func TestTheOtherConditionsArgvIsUnchanged(t *testing.T) {
	f := newFake(t)
	o, _ := options(t, f, "pagination-bug", AllConditions, 1)
	o.claudePath = "/usr/bin/claude"
	runs, err := Plan(o)
	if err != nil || len(runs) != 3 {
		t.Fatalf("plan %v %v", runs, err)
	}
	jhon, p := benchJhon(f.home), runs[0].Task.Prompt
	tail := func(id string) []string {
		return []string{"--output-format", "stream-json", "--verbose", "--max-turns", strconv.Itoa(runs[0].Task.MaxTurns), "--model", "haiku", "--permission-mode", "acceptEdits", "--allowedTools", DefaultAllowedTools, "--session-id", id}
	}
	want := [][]string{
		append([]string{jhon, "start", "-p", p, "--mcp-config", engramMCPConfig(f.home)}, tail(runs[0].SessionID)...),
		append([]string{"/usr/bin/claude", "-p", p, "--safe-mode", "--setting-sources", "project,local", "--strict-mcp-config"}, tail(runs[1].SessionID)...),
		{jhon, "start", "pi", "--mode", "json", "--provider", "claude-bridge", "--model", "haiku", "--session-id", runs[2].SessionID, "--", p},
	}
	for i, r := range runs {
		// jhon's env keeps the owner's CLAUDE.md and auto-memory out (T146 FB4).
		wantEnv := map[string][]string{"jhon": isolatedClaudeEnv}[r.Cond.Name()]
		if !slices.Equal(r.Argv, want[i]) || !slices.Equal(r.Env, wantEnv) {
			t.Errorf("%s argv\n%q\nwant\n%q\nenv %q", r.Name(), r.Argv, want[i], r.Env)
		}
	}
	if !slices.Equal(AllConditions, []string{"jhon", "clean", "pi-jhon"}) {
		t.Errorf("default conditions %v", AllConditions)
	}
}

func TestPiCleanRunsPlainOnlyAndIsAcceptedByRun(t *testing.T) {
	f := newFake(t)
	var out, errOut bytes.Buffer
	code := cli([]string{"run", "--home", f.home, "--results", filepath.Join(t.TempDir(), "r"), "--tasks", "pagination-bug", "--variants", "plain,ldd",
		"--conditions", "pi-jhon,pi-clean", "--repeats", "1", "--dry-run"}, &out, &errOut, time.Now())
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, s := range []string{"plan: 3 runs", "pagination-bug-plain-pi-clean-1", "--no-extensions"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("dry run lacks %q:\n%s", s, out.String())
		}
	}
	if strings.Contains(out.String(), "ldd-pi-clean") {
		t.Errorf("pi-clean ran an ldd variant:\n%s", out.String())
	}
}

func TestAPiCleanRunIsMeasuredLikePiJhon(t *testing.T) {
	f := newFake(t)
	putBridge(t, f)
	sol, _ := filepath.Abs(filepath.Join("testdata", "solutions", "pagination-bug"))
	t.Setenv("FAKE_SOLUTION", sol)
	o, out := options(t, f, "pagination-bug", []string{"pi-jhon", "pi-clean"}, 1)
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	clean, pj := metricsOf(t, o, "pagination-bug-plain-pi-clean-1"), metricsOf(t, o, "pagination-bug-plain-pi-jhon-1")
	agent := filepath.Join(o.Results, "runs", "pagination-bug-plain-pi-clean-1", "pi-agent")
	if !clean.Success || clean.Condition != "pi-clean" || clean.Turns != pj.Turns || clean.OutputTokens != pj.OutputTokens || clean.InputTokens != pj.InputTokens ||
		clean.ActiveMs != pj.ActiveMs || len(clean.Problems) != 0 {
		t.Errorf("pi-clean %+v\npi-jhon %+v", clean, pj)
	}
	if !strings.HasPrefix(clean.Transcript, filepath.Join(agent, "sessions")) || !strings.HasSuffix(clean.Transcript, "_"+clean.SessionID+".jsonl") {
		t.Errorf("transcript %s", clean.Transcript)
	}
	var cfg struct {
		Provider struct {
			Path string `json:"pathToClaudeCodeExecutable"`
		} `json:"provider"`
	}
	b, err := os.ReadFile(filepath.Join(agent, "claude-bridge.json"))
	if err != nil || json.Unmarshal(b, &cfg) != nil || cfg.Provider.Path != piProviderWrapper(f.home) {
		t.Errorf("claude-bridge.json %v %s", err, b)
	}
	calls := f.calls(t)
	for _, c := range calls {
		if strings.HasPrefix(c, "jhon start") && strings.Contains(c, "--no-extensions") {
			t.Errorf("pi-clean went through jhon: %s", c)
		}
	}
	// A call's line breaks where its prompt does: its env is on a later line.
	log := strings.Join(calls, "\n")
	if !strings.Contains(log, "pi --no-extensions") || !strings.Contains(log, "|PI_CODING_AGENT_DIR="+agent+"|") || !strings.Contains(log, "|PI_CODING_AGENT_DIR="+filepath.Join(f.home, ".pi", "jhon")+"|") {
		t.Errorf("pi calls:\n%s", log)
	}
}

func TestPiCleanRefusesABenchHomeWithNoBridge(t *testing.T) {
	f := newFake(t)
	o, out := options(t, f, "pagination-bug", []string{"pi-clean"}, 1)
	err := Execute(o)
	if err == nil || !strings.Contains(err.Error(), "pi-claude-bridge") || !strings.Contains(err.Error(), "bench prepare") {
		t.Fatalf("err %v\n%s", err, out)
	}
	if c := f.calls(t); slices.ContainsFunc(c, func(s string) bool { return strings.HasPrefix(s, "pi ") }) {
		t.Errorf("pi started with no bridge: %v", c)
	}
}
