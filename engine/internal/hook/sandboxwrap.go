package hook

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/sandbox"
)

// sandboxTools are Claude Code's tools that start a shell command of the
// model's: Bash, and Monitor, whose `command` is one too (a Monitor call
// with a `ws` input opens a WebSocket and starts none).
var sandboxTools = []string{"Bash", "Monitor"}

// SandboxInput is the sandbox of a work command started in dir in the pinned
// project: what sandboxWrap probes before it hands a wrapped command back.
// The cli sets it to the one `jhon sandbox run` builds, with what the
// settings and the integrations add: telemetry is an integration the hooks
// never import (T55).
var SandboxInput = sandbox.Current

// sandboxHost is the machine the wrap reads the mode for and probes; a test
// gives another system's.
var sandboxHost = sandbox.ThisHost

// sandboxAllowReason goes with the allow of a wrapped command; Claude Code
// writes an allow's reason to its debug log only.
const sandboxAllowReason = "Jhon's guards allow the command; it runs in Jhon's sandbox"

// sandboxWrap is the tool input Claude Code runs in place of the payload's
// when sandbox.mode is on as sandbox.Mode reads it, by default on Linux
// where the sandbox can run (T128c, L894, design section 3; T128f, L1375):
// the same input
// with only its command replaced by
//
//	<jhon> sandbox run --project '<folder>' --profile work -- <shell> -c '<command>'
//
// where <jhon> is this binary, the folder is the one `jhon start` pinned
// (sandbox.ProjectEnv) and the command is single-quoted, every other field
// kept byte for byte. A line of git network commands or gh whose git works
// inside the project, or inside a git repository in a sandbox.allow_write
// folder (sandbox.ProfileIn, T128g, L1303 G1, L1306, T251), of docker,
// or of the owner's sandbox.deploy_tools (in.DeployTools) is handed
// back as
//
//	<jhon> sandbox run --project '<folder>' --profile unsandboxed -- <shell> -c '<command>'
//
// which runs it with no sandbox (git over SSH and gh cannot run in one,
// L1220, L1302) after `jhon sandbox run` checks the line again. Its
// directory is not followed: the only cd such a line may hold is a leading
// one into the project (T181, L1503), and Claude Code's next call starts
// where this one did. Either way `jhon
// sandbox run` ends a read-only failure with the stop note on stderr and
// records a jhon.sandbox event (G3, G4). It is nil for a call the wrap does not take: the mode
// off, a tool that starts no command, and pi's calls, which come scoped to
// every agent (`--agent *`) and are wrapped on pi's own side (T128b). A
// command is wrapped whatever it starts with: one that already runs `jhon
// sandbox run` gets a sandbox inside the sandbox, which cannot start.
//
// refusal is why a call the wrap takes cannot be wrapped, on one line: no
// pinned project, or a sandbox that cannot run here (the reason and the fix
// `jhon sandbox status` gives). Such a call is refused, never run as it came.
func sandboxWrap(agent string, payload []byte) (updated map[string]json.RawMessage, refusal string) {
	if agent == "*" {
		return nil, ""
	}
	// The payload as the guards read it (read): a key is taken by its exact
	// name, so no payload reads as one tool to the guards and another here
	// (L917, C6). The input's fields are then taken as their own bytes.
	p, ok := read(bytes.NewReader(payload))
	tool := p.Str("tool_name")
	if !ok || !slices.Contains(sandboxTools, tool) {
		return nil, ""
	}
	var top, input map[string]json.RawMessage
	if json.Unmarshal(payload, &top) == nil {
		_ = json.Unmarshal(top["tool_input"], &input)
	}
	raw, has := input["command"]
	if !has && tool == "Monitor" {
		return nil, ""
	}
	// The sandbox of the call, built once and only when needed: by the mode,
	// which probes it where no owner value decides, else by the probe below.
	folder, pinned := sandbox.ProjectFolder(os.Getenv(sandbox.ProjectEnv))
	cwd := p.Cwd()
	var built *sandbox.Input
	current := func() sandbox.Input {
		if built == nil {
			in := SandboxInput(cwd, folder)
			built = &in
		}
		return *built
	}
	effective := sandbox.Mode(sandboxHost(), current)
	if effective.Mode != "on" {
		return nil, ""
	}
	var command string
	if !has || json.Unmarshal(raw, &command) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || strings.ContainsRune(command, 0) {
		return nil, "sandbox: sandbox.mode is on and the " + tool + " call's command is not a text a shell can take; the call is refused"
	}
	if !pinned {
		return nil, "sandbox: sandbox.mode is on and " + sandbox.ProjectEnv + " names no project folder (" + pinnedAs(os.Getenv(sandbox.ProjectEnv)) +
			"), so the command would run in no pinned project; the call is refused; fix: start the session with `jhon start` from inside the project (a git repository)"
	}
	in := current()
	// bubblewrap starts at most once a call: the mode's probe is the verdict
	// when it probed. A probe that timed out is none (L1382): the command is
	// wrapped as usual, and `jhon sandbox run` fails visibly if bubblewrap
	// cannot start.
	problem := effective.Problem
	if !effective.Probed {
		problem = sandbox.Probe(sandboxHost(), in)
	}
	if problem != nil && !problem.TimedOut {
		return nil, "sandbox: sandbox.mode is on and the sandbox cannot run here: " + problem.Reason + "; the call is refused; fix: " + problem.Fix
	}
	jhon, err := os.Executable()
	if err != nil || !filepath.IsAbs(jhon) {
		return nil, "sandbox: sandbox.mode is on and jhon cannot name its own program; the call is refused; fix: run `jhon doctor`"
	}
	shell := shellWord(sandboxShell())
	profile := sandbox.ProfileIn(in, command)
	// The pinned project goes to the run as the session wrote it, not as it
	// read here through every link: the run opens that spelling with no
	// link followed and refuses one a swap changed (T248 L4's second
	// verify), so a swap while this hook read it pins nothing else.
	run := shellWord(jhon) + " sandbox run --project " + shellQuote(os.Getenv(sandbox.ProjectEnv)) + " --profile " + profile + " -- " + shell + " -c "
	line := run + shellQuote(command)
	// A command that opens with - or + would be read as the shell's options.
	if strings.HasPrefix(command, "-") || strings.HasPrefix(command, "+") {
		line = run + "-- " + shellQuote(command)
	}
	if file := lastDirFile(); file != "" && profile == sandbox.Work && tool == "Bash" && foreground(input["run_in_background"]) {
		line = run + shellQuote(fmt.Sprintf(reportLastDir, file)) + " " + shell + " " + shellQuote(command) + fmt.Sprintf(followLastDir, file)
	}
	updated = map[string]json.RawMessage{}
	for k, v := range input {
		updated[k] = v
	}
	updated["command"] = json.RawMessage(marshal(line))
	return updated, ""
}

// A `cd` inside a wrapped command carries to the next call, as it does in
// Claude Code without the wrap (L899 c): Claude Code asks its own shell where
// it is after the line, and the wrapped command changed directory only in the
// inner shell. So the inner shell reports where it ended, and the line moves
// the outer shell there.
//
// reportLastDir is what the inner shell runs, with the file the hook named:
// as it exits it writes its working directory to the file, and it evaluates
// the command, which it received byte for byte as its first argument, with
// no positional parameter of its own (after a blank, so a command that opens
// with - is not read as eval's option). It writes nothing where the shell
// cannot tell (a deleted directory), and never reports when the command
// replaces the shell (exec), sets an EXIT trap of its own, or is killed
// before the shell can run a trap.
//
// followLastDir is what the outer shell runs after the wrap: it reads the
// file, removes it, and moves to the directory when the file holds an
// absolute path it can enter; then the line's exit code is the command's.
// What the file holds is only ever the argument of `cd --`. Nothing of its
// own is left in the outer shell.
const (
	reportLastDir = `trap "{ command pwd -P >| %s; } 2>/dev/null" EXIT; __jhon_sandbox_command=$1; set --; eval " $__jhon_sandbox_command"`
	followLastDir = `; __jhon_sandbox_s=$?; __jhon_sandbox_cd() { unset __jhon_sandbox_s; local d=; { IFS= builtin read -r -d '' d < %[1]s; } 2>/dev/null; command rm -f -- %[1]s 2>/dev/null; ` +
		`d=${d%%$'\n'}; case $d in /*) builtin cd -- "$d" 2>/dev/null ;; esac; unset -f __jhon_sandbox_cd; return "$1"; }; __jhon_sandbox_cd "$__jhon_sandbox_s"`
)

// lastDirFile is a new name under /tmp for one call's last directory: /tmp
// is the same folder inside the sandbox and outside, and 128 random bits no
// other program guesses. "" when the system gives no random bytes: the call
// is then wrapped without following its directory.
func lastDirFile() string {
	name := make([]byte, 16)
	if _, err := rand.Read(name); err != nil {
		return ""
	}
	return "/tmp/jhon-cwd-" + hex.EncodeToString(name)
}

// foreground is whether a Bash call's run_in_background, as Claude Code
// sends it, leaves the command in the foreground: absent, false or null. A
// background command outlives its line, so nothing follows it.
func foreground(mark json.RawMessage) bool {
	text := string(bytes.TrimSpace(mark))
	return text == "" || text == "false" || text == "null"
}

// pinnedAs says what the pinning variable holds, for the refusal.
func pinnedAs(value string) string {
	if value == "" {
		return "it is not set"
	}
	return "it is " + value
}

// sandboxShell is the shell a wrapped command runs in: the session's $SHELL
// when it is bash or zsh, as Claude Code picks its own; else the bash on
// PATH; else /bin/sh. The owner's shell may be one that does not read the
// POSIX commands a model writes (fish, nu).
func sandboxShell() string {
	shell := os.Getenv("SHELL")
	if name := filepath.Base(shell); filepath.IsAbs(shell) && (name == "bash" || name == "zsh") {
		if fi, err := os.Stat(shell); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return shell
		}
	}
	if bash, err := exec.LookPath("bash"); err == nil && filepath.IsAbs(bash) {
		return bash
	}
	return "/bin/sh"
}

// shellQuote is s as one word of a POSIX shell (bash and zsh too): inside
// single quotes no byte is special, and a single quote is written by
// closing the quotes, an escaped quote, and opening them again.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// shellWord is a program's path as one word: bare when every byte is plain,
// else quoted.
func shellWord(s string) string {
	plain := func(r rune) bool {
		return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-", r)
	}
	if s != "" && strings.IndexFunc(s, func(r rune) bool { return !plain(r) }) < 0 {
		return s
	}
	return shellQuote(s)
}

// sandboxedJSON is what pre-tool prints for a call the guards did not
// refuse and the sandbox wraps: the guards' decision with updatedInput, so
// Claude Code never decides alone about a call that was changed. An ask
// keeps its reason; an allow says allow, with the guards' notes for the
// agent. In Claude Code's plan mode (the payload's permission_mode) an allow
// carries no decision, so plan mode's own rules decide about the command
// (L899 a). ok is false for an ask it cannot read, which then refuses.
func sandboxedJSON(ask []byte, notes []string, updated map[string]json.RawMessage, plan bool) (answer string, ok bool) {
	out := map[string]any{"hookEventName": "PreToolUse", "updatedInput": updated}
	if !plan {
		out["permissionDecision"], out["permissionDecisionReason"] = "allow", sandboxAllowReason
	}
	switch {
	case ask != nil:
		var j map[string]map[string]any
		if json.Unmarshal(ask, &j) != nil {
			return "", false
		}
		reason, isText := j["hookSpecificOutput"]["permissionDecisionReason"].(string)
		if !isText || j["hookSpecificOutput"]["permissionDecision"] != "ask" {
			return "", false
		}
		out["permissionDecision"], out["permissionDecisionReason"] = "ask", reason
	case len(notes) > 0:
		out["additionalContext"] = strings.Join(notes, "; ")
	}
	return marshal(map[string]any{"hookSpecificOutput": out}), true
}

// agentMarked is the tool input Claude Code runs for an agent's command
// call the sandbox does not wrap: the same input with `export JHON_AGENT=1`
// on a line before the command (AgentVariable, T219 S9). nil for a call
// that starts no command, and for pi's (`--agent *`), which pi's shell marks.
// Claude Code applies a PreToolUse updatedInput with no permissionDecision
// as a rewrite of the input, under its own permission rules.
func agentMarked(agent string, payload []byte) map[string]json.RawMessage {
	if agent == "*" {
		return nil
	}
	p, ok := read(bytes.NewReader(payload))
	if !ok || !slices.Contains(sandboxTools, p.Str("tool_name")) {
		return nil
	}
	var top, input map[string]json.RawMessage
	if json.Unmarshal(payload, &top) != nil || json.Unmarshal(top["tool_input"], &input) != nil {
		return nil
	}
	var command string
	if raw, has := input["command"]; !has || json.Unmarshal(raw, &command) != nil || strings.ContainsRune(command, 0) {
		return nil
	}
	updated := map[string]json.RawMessage{}
	for k, v := range input {
		updated[k] = v
	}
	updated["command"] = json.RawMessage(marshal("export " + AgentVariable + "=1\n" + command))
	return updated
}
