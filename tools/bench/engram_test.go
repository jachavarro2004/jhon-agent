package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// Each run has an engram store of its own, under its cache: no run recalls
// what an earlier run saved (L425).
func TestBenchEnvPinsEngramToTheRunsStore(t *testing.T) {
	env := benchEnv([]string{"HOME=/o", "ENGRAM_DATA_DIR=/o/.engram", "ENGRAM_URL=http://127.0.0.1:7437", "ENGRAM_PORT=7437", "ENGRAM_PROJECT=mine",
		"ENGRAM_CLOUD_AUTOSYNC=1", "PATH=/usr/bin"}, "/b", "/r/cache")
	for _, kv := range env {
		if strings.HasPrefix(kv, "ENGRAM_") && kv != "ENGRAM_DATA_DIR=/r/cache/engram" {
			t.Errorf("env carries %s", kv)
		}
	}
	if !slices.Contains(env, "ENGRAM_DATA_DIR=/r/cache/engram") {
		t.Errorf("env lacks ENGRAM_DATA_DIR=/r/cache/engram: %q", env)
	}
}

func TestBothJhonConditionsGetTheSameIsolatedEngram(t *testing.T) {
	f := newFake(t)
	t.Setenv("ENGRAM_DATA_DIR", "/owner/.engram")
	t.Setenv("ENGRAM_URL", "http://127.0.0.1:7437")
	o, out := options(t, f, "pagination-bug", []string{"jhon", "pi-jhon"}, 1)
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	// jhon start refuses --plugin-dir (it sets its own), so engram comes in
	// through --mcp-config (L431).
	cfg := engramMCPConfig(f.home)
	if !strings.HasPrefix(cfg, f.home+string(filepath.Separator)) {
		t.Fatalf("the engram MCP config %s is outside the bench HOME", cfg)
	}
	b, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var mcp struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &mcp); err != nil {
		t.Fatal(err)
	}
	// The server pi carries, as pi carries it, less pi's own exposure key.
	want := map[string]any{"command": "engram", "args": []any{"mcp", "--tools=agent"}}
	if got := mcp.MCPServers["engram"]; len(mcp.MCPServers) != 1 || got["command"] != want["command"] || !slices.Equal(got["args"].([]any), want["args"].([]any)) || len(got) != 2 {
		t.Errorf("MCP config servers %s", b)
	}
	// The prompt spans lines, so each call's env is matched in the whole log.
	calls := strings.Join(f.calls(t), "\n")
	store := regexp.MustCompile(`\|JHON_ROOT=\|ENGRAM_DATA_DIR=\S*/runs/[^/|]+-jhon-1/cache/engram\|ENGRAM_URL=\|ENGRAM_PORT=\n`)
	if !strings.Contains(calls, "--mcp-config "+cfg+" --output-format") || !store.MatchString(calls+"\n") {
		t.Errorf("Claude Code + Jhon ran without the bench engram:\n%s", calls)
	}
	piStore := regexp.MustCompile(regexp.QuoteMeta("|PI_CODING_AGENT_DIR="+filepath.Join(f.home, ".pi", "jhon")) + `\|ENGRAM_DATA_DIR=\S*/runs/[^/|]+-pi-jhon-1/cache/engram\|ENGRAM_URL=\|ENGRAM_PORT=\|`)
	if !piStore.MatchString(calls) {
		t.Errorf("pi ran without the bench engram store:\n%s", calls)
	}
}

func TestTheJhonConditionRefusesABenchHomeWithNoPiEngram(t *testing.T) {
	f := newFake(t)
	if err := os.WriteFile(filepath.Join(f.home, ".pi", "jhon", "mcp.json"), []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	o, out := options(t, f, "pagination-bug", []string{"jhon"}, 1)
	if err := Execute(o); err == nil || !strings.Contains(err.Error(), "no engram server") {
		t.Fatalf("err %v\n%s", err, out)
	}
	if c := f.calls(t); c != nil {
		t.Errorf("a run started: %v", c)
	}
}

func TestDebugPromptKeepsEachRunsPromptDump(t *testing.T) {
	f := newFake(t)
	o, out := options(t, f, "pagination-bug", []string{"jhon", "pi-jhon"}, 1)
	o.DebugPrompt = true
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	jhonDump := filepath.Join(o.Results, "runs", "pagination-bug-plain-jhon-1", "prompt-dump")
	piDump := filepath.Join(o.Results, "runs", "pagination-bug-plain-pi-jhon-1", "prompt-dump")
	calls := strings.Join(f.calls(t), "\n")
	if !strings.Contains(calls, "--debug-file "+filepath.Join(jhonDump, "claude-debug.log")+" ") {
		t.Errorf("Claude Code + Jhon ran with no --debug-file:\n%s", calls)
	}
	if !strings.Contains(calls, "|CLAUDE_BRIDGE_DEBUG=1|CLAUDE_BRIDGE_DEBUG_PATH="+filepath.Join(piDump, "claude-bridge.log")) {
		t.Errorf("pi ran with no bridge debug:\n%s", calls)
	}
	if b, err := os.ReadFile(filepath.Join(piDump, "claude-bridge.log")); err != nil || !strings.Contains(string(b), "the dumped prompt") {
		t.Errorf("the bridge's dump is not in the run: %s %v", b, err)
	}
}

func TestWithoutDebugPromptNothingIsDumped(t *testing.T) {
	f := newFake(t)
	t.Setenv("CLAUDE_BRIDGE_DEBUG", "1")
	t.Setenv("CLAUDE_BRIDGE_DEBUG_PATH", "/owner/claude-bridge.log")
	o, out := options(t, f, "pagination-bug", []string{"jhon", "pi-jhon"}, 1)
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	calls := strings.Join(f.calls(t), "\n")
	if strings.Contains(calls, "--debug-file") || !strings.Contains(calls, "|CLAUDE_BRIDGE_DEBUG=|CLAUDE_BRIDGE_DEBUG_PATH=\n") {
		t.Errorf("a run debugged with no --debug-prompt:\n%s", calls)
	}
	for _, name := range []string{"pagination-bug-plain-jhon-1", "pagination-bug-plain-pi-jhon-1"} {
		if _, err := os.Stat(filepath.Join(o.Results, "runs", name, "prompt-dump")); !os.IsNotExist(err) {
			t.Errorf("%s has a prompt-dump: %v", name, err)
		}
	}
}

func TestRunDebugPromptFlagShowsInTheDryRun(t *testing.T) {
	f := newFake(t)
	var out, errOut bytes.Buffer
	results := filepath.Join(t.TempDir(), "results")
	code := cli([]string{"run", "--home", f.home, "--results", results, "--tasks", "pagination-bug", "--variants", "plain", "--conditions", "jhon,pi-jhon",
		"--dry-run", "--debug-prompt"}, &out, &errOut, time.Now())
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, want := range []string{"--mcp-config " + engramMCPConfig(f.home), "ENGRAM_DATA_DIR=" + filepath.Join(results, "runs", "pagination-bug-plain-jhon-1", "cache", "engram"),
		"--debug-file " + filepath.Join(results, "runs", "pagination-bug-plain-jhon-1", "prompt-dump", "claude-debug.log"),
		"CLAUDE_BRIDGE_DEBUG=1 CLAUDE_BRIDGE_DEBUG_PATH=" + filepath.Join(results, "runs", "pagination-bug-plain-pi-jhon-1", "prompt-dump", "claude-bridge.log")} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("dry run lacks %q:\n%s", want, out.String())
		}
	}
	if _, err := os.Stat(engramMCPConfig(f.home)); !os.IsNotExist(err) {
		t.Errorf("the dry run wrote %s", engramMCPConfig(f.home))
	}
}
