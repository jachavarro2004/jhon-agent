package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// DefaultAllowedTools lets Bash and the edit tools run with no prompt in
// print mode, in both Claude Code conditions (SPIKE.md: Bash needs it), with
// the agent and background tools for parity with the shell's subagent and
// bash_background (T46; names from claude 2.1.310).
const DefaultAllowedTools = "Bash,Edit,MultiEdit,Write,Read,Glob,Grep,Agent,Monitor,TaskOutput,TaskStop"

// AllConditions are the conditions a run compares, in plan order:
//   - jhon: Claude Code with Jhon (`jhon start -p`);
//   - clean: Claude Code with no customization (`claude -p --safe-mode`);
//   - pi-jhon: pi with the Jhon shell on the claude-bridge provider
//     (`jhon start pi --mode json`, L390).
//
// pi-clean, pi with no Jhon on the same provider (L552), runs only when
// named in --conditions.
var AllConditions = []string{"jhon", "clean", "pi-jhon"}

// KnownConditions are every condition, in report order.
var KnownConditions = []string{"jhon", "clean", "pi-jhon", "pi-clean"}

// noJhon are the conditions with no Jhon: each Jhon condition's counterpart
// (`bench value`) and, like clean, run in the plain variant only.
var noJhon = map[string]string{"jhon": "clean", "pi-jhon": "pi-clean"}

// isClean is whether cond runs with no Jhon.
func isClean(cond string) bool { return cond == "clean" || cond == "pi-clean" }

// Condition is how one condition starts its agent, where the session it
// leaves lands, and how its stream reads.
type Condition interface {
	Name() string
	// Argv is the command line of r, its program first: the one command of
	// a single-message run, the first of a multi-message one.
	Argv(r *Run, o *Options) []string
	// Resume is the command that sends message n (2 or more) of r to the
	// same session; nil when every message goes to Argv's stdin (RPC).
	Resume(r *Run, o *Options, n int) []string
	// Transcript is the session file r left, or why it is missing.
	Transcript(r *Run, o *Options) (string, error)
	// Stream reads r's stdout.
	Stream(b []byte) StreamFacts
	// CapsTurns is true when the runtime has no --max-turns, so the runner
	// stops it after the task's turns, counted from the stream.
	CapsTurns() bool
}

// conditionNamed is the condition called name.
func conditionNamed(name string) (Condition, error) {
	switch name {
	case "jhon":
		return claudeCode{jhon: true}, nil
	case "clean":
		return claudeCode{}, nil
	case "pi-jhon":
		return piJhon{}, nil
	case "pi-clean":
		return piClean{}, nil
	}
	return nil, fmt.Errorf("unknown condition %q (known: %s)", name, strings.Join(KnownConditions, ", "))
}

// benchJhon is the bench HOME's jhon, the one `bench prepare` installs.
func benchJhon(home string) string { return filepath.Join(home, ".local", "bin", "jhon") }

// claudeCode runs Claude Code headless, with Jhon or clean.
type claudeCode struct{ jhon bool }

func (c claudeCode) Name() string {
	if c.jhon {
		return "jhon"
	}
	return "clean"
}

func (c claudeCode) Argv(r *Run, o *Options) []string {
	return c.argv(r, o, r.Task.Prompt, "--session-id")
}

// Resume sends message n with --resume, which keeps the session's id
// (claude --help: only --fork-session makes a new one), so the transcript
// stays <session id>.jsonl (T46).
func (c claudeCode) Resume(r *Run, o *Options, n int) []string {
	return c.argv(r, o, r.Task.Prompts[n-1], "--resume")
}

// argv is a -p call with prompt on r's session, named with flag.
func (c claudeCode) argv(r *Run, o *Options, prompt, flag string) []string {
	var argv []string
	if c.jhon {
		// The bench's engram, as an MCP config: jhon start refuses
		// --plugin-dir (L424, L431).
		argv = []string{benchJhon(o.Home), "start", "-p", prompt, "--mcp-config", engramMCPConfig(o.Home)}
		if o.DebugPrompt {
			argv = append(argv, "--debug-file", filepath.Join(r.promptDump(), "claude-debug.log"))
		}
	} else {
		argv = []string{o.claude(), "-p", prompt, "--safe-mode", "--setting-sources", "project,local", "--strict-mcp-config"}
	}
	return append(argv, "--output-format", "stream-json", "--verbose", "--max-turns", strconv.Itoa(r.Task.MaxTurns),
		"--model", r.Model, "--permission-mode", "acceptEdits", "--allowedTools", o.AllowedTools, flag, r.SessionID)
}

// encodeCwd is Claude Code's project folder name for dir: every / and . a -.
func encodeCwd(dir string) string { return strings.NewReplacer("/", "-", ".", "-").Replace(dir) }

func (claudeCode) Transcript(r *Run, o *Options) (string, error) {
	repo := r.Repo
	if real, err := filepath.EvalSymlinks(repo); err == nil {
		repo = real
	}
	p := filepath.Join(o.Home, ".claude", "projects", encodeCwd(repo), r.SessionID+".jsonl")
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("no transcript at %s", p)
	}
	return p, nil
}

func (claudeCode) Stream(b []byte) StreamFacts { return claudeStream(b) }

func (claudeCode) CapsTurns() bool { return false }

// piJhon runs pi through `jhon start pi` (PI_CODING_AGENT_DIR=<HOME>/.pi/jhon)
// in JSON mode on the claude-bridge provider, so the model runs on the bench
// HOME's Claude Code login. pi has no --max-turns: the runner counts its
// turn_start events. Its tools are pi's defaults (read, bash, edit, write).
type piJhon struct{}

func (piJhon) Name() string { return "pi-jhon" }

func (piJhon) Argv(r *Run, o *Options) []string {
	if r.Task.MultiMessage() {
		// RPC mode: the messages go to stdin, one per agent_settled (T46).
		return append(append([]string{benchJhon(o.Home), "start", "pi", "--mode", "rpc"}, piModelArgs(r)...), "--session-id", r.SessionID)
	}
	return append(append([]string{benchJhon(o.Home), "start", "pi", "--mode", "json"}, piModelArgs(r)...), "--session-id", r.SessionID, "--", r.Task.Prompt)
}

// Transcript is pi's session file for the run's id, under the Jhon agent
// dir's sessions/<encoded cwd>/<timestamp>_<id>.jsonl; the newest if several.
func (piJhon) Resume(*Run, *Options, int) []string { return nil }

func (piJhon) Transcript(r *Run, o *Options) (string, error) {
	matches, _ := filepath.Glob(filepath.Join(o.Home, ".pi", "jhon", "sessions", "*", "*_"+r.SessionID+".jsonl"))
	if len(matches) == 0 {
		return "", fmt.Errorf("no pi session %s under %s", r.SessionID, filepath.Join(o.Home, ".pi", "jhon", "sessions"))
	}
	slices.Sort(matches)
	return matches[len(matches)-1], nil
}

func (piJhon) Stream(b []byte) StreamFacts { return piStream(b) }

func (piJhon) CapsTurns() bool { return true }

// piModel is a pi run's provider, model and thinking level, from its
// effective model provider/model[@thinking] (T77).
func piModel(model string) (provider, id, thinking string) {
	provider, id, _ = strings.Cut(model, "/")
	id, thinking, _ = strings.Cut(id, "@")
	return provider, id, thinking
}

// parsePiModel checks --pi-model's provider/model[@thinking].
func parsePiModel(s string) error {
	p, id, th := piModel(s)
	if p == "" || id == "" || strings.HasSuffix(s, "@") && th == "" || !strings.Contains(s, "/") {
		return fmt.Errorf("--pi-model %q: give provider/model or provider/model@thinking (e.g. zai/glm-5.3, zai/glm-5.3@high)", s)
	}
	return nil
}

// bridgeModel is the effective pi model of a run with no --pi-model.
func bridgeModel(model string) string { return BridgeProvider + "/" + model }

// BridgeProvider is pi-claude-bridge's provider, the pi conditions' default.
const BridgeProvider = "claude-bridge"

// onBridge is whether r's pi runs on claude-bridge.
func onBridge(r *Run) bool { p, _, _ := piModel(r.Model); return p == BridgeProvider }

// piModelArgs are pi's --provider, --model and, when given, --thinking for r.
func piModelArgs(r *Run) []string {
	p, id, th := piModel(r.Model)
	argv := []string{"--provider", p, "--model", id}
	if th != "" {
		argv = append(argv, "--thinking", th)
	}
	return argv
}

// isPi is whether cond runs pi.
func isPi(cond string) bool { return strings.HasPrefix(cond, "pi-") }

// piClean runs pi with no Jhon (L552): the pi on PATH, not `jhon start pi`,
// with a fresh agent dir of the run's own (PI_CODING_AGENT_DIR=<run>/pi-agent:
// no Jhon settings, packages, prompt, agents, guards or MCP servers), every
// discovery off, and only pi-claude-bridge loaded, so it runs on the same
// provider and model as pi-jhon. The bridge is the one `bench prepare`
// installed in the bench HOME, and its Claude Code the same hooks-off
// provider wrapper pi-jhon's runs (claude-bridge.json in the agent dir).
// Its tools are pi's defaults (read, bash, edit, write).
type piClean struct{}

func (piClean) Name() string { return "pi-clean" }

// piBridgeExtension is pi-claude-bridge's extension in the bench HOME
// (its package.json's pi.extensions).
func piBridgeExtension(home string) string {
	return filepath.Join(home, ".pi", "jhon", "npm", "node_modules", "pi-claude-bridge", "src", "index.ts")
}

// piProviderWrapper is the bench HOME's Claude Code wrapper for the bridge,
// which turns hooks off when Claude Code runs as the model provider.
func piProviderWrapper(home string) string {
	return filepath.Join(home, ".pi", "jhon", "claude-code-provider")
}

// piAgentDir is a pi-clean run's own agent dir.
func piAgentDir(r *Run) string { return filepath.Join(r.Dir, "pi-agent") }

func (piClean) Argv(r *Run, o *Options) []string {
	argv := []string{o.pi(), "--no-extensions"}
	if onBridge(r) {
		argv = append(argv, "--extension", piBridgeExtension(o.Home))
	}
	argv = append(argv, "--no-skills", "--no-prompt-templates", "--no-themes", "--no-mcp")
	if r.Task.MultiMessage() {
		return append(append(append(argv, "--mode", "rpc"), piModelArgs(r)...), "--session-id", r.SessionID)
	}
	return append(append(append(argv, "--mode", "json"), piModelArgs(r)...), "--session-id", r.SessionID, "--", r.Task.Prompt)
}

func (piClean) Resume(*Run, *Options, int) []string { return nil }

// env is the agent dir pi reads in place of Jhon's.
func (piClean) env(r *Run) []string { return []string{"PI_CODING_AGENT_DIR=" + piAgentDir(r)} }

// prepare makes the run's agent dir: empty on a built-in provider (its key
// comes from the environment, --pi-env); on claude-bridge with only
// claude-bridge.json, after checking the bench HOME has the bridge and its
// wrapper.
func (piClean) prepare(r *Run, o *Options) error {
	if !onBridge(r) {
		return os.MkdirAll(piAgentDir(r), 0o755)
	}
	for _, p := range []string{piBridgeExtension(o.Home), piProviderWrapper(o.Home)} {
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("pi-clean needs pi-claude-bridge and its provider wrapper in the bench HOME: no %s (run `bench prepare`)", p)
		}
	}
	if err := os.MkdirAll(piAgentDir(r), 0o755); err != nil {
		return err
	}
	return writeJSON(filepath.Join(piAgentDir(r), "claude-bridge.json"), map[string]any{"provider": map[string]any{"pathToClaudeCodeExecutable": piProviderWrapper(o.Home)}})
}

func (piClean) Transcript(r *Run, o *Options) (string, error) {
	sessions := filepath.Join(piAgentDir(r), "sessions")
	matches, _ := filepath.Glob(filepath.Join(sessions, "*", "*_"+r.SessionID+".jsonl"))
	if len(matches) == 0 {
		return "", fmt.Errorf("no pi session %s under %s", r.SessionID, sessions)
	}
	slices.Sort(matches)
	return matches[len(matches)-1], nil
}

func (piClean) Stream(b []byte) StreamFacts { return piStream(b) }

func (piClean) CapsTurns() bool { return true }
