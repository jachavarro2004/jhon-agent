package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// `certify --replay-guard <jhon>` (T64d, L531): a guard refusal or a
// declined guard dialog usually comes from guard behaviour the installed
// build shares, so it tracks how the agent phrased a command, not a
// regression. Each one of every run, on both sides, goes as the payload it
// was through the reference build's guard: when the reference refuses or
// asks too, it is today's behaviour, excluded from the comparison and
// listed as baseline-shared (for T62). When the reference passes it, or the
// replay cannot judge it, it counts as before, with no margin.

// guardedTools are the Claude Code tools Jhon's guards judge
// (engine/internal/guard/matchers.go ToolOrder): a denial of any other tool
// is Claude Code's own, not a guard's.
var guardedTools = []string{"Read", "Grep", "Glob", "Edit", "MultiEdit", "Write", "NotebookEdit", "Bash", "Agent", "SendMessage", "Task"}

// replayEntry is the hook entry the bench conditions run, pi's and Claude
// Code's alike (shell/lib/guards.ts, the render's `hook pre-tool`), without
// framework-guard, whose protected paths are the bench HOME's.
var replayEntry = []string{"hook", "pre-tool", "--agent", "*", "safety-guard", "vcs-guard", ".git", "secrets-guard", "route-guard"}

// replayTimeout is how long one replayed payload may take.
const replayTimeout = 30 * time.Second

// piGuardTitle starts the title of pi's guard dialog; its reason follows.
const piGuardTitle = "Jhon safety guard · "

// guardEvent is one guard refusal (Claude Code) or declined guard dialog
// (pi) of a run's stream.jsonl: the Bash command, "" when the stream does
// not name it, and the sub-agent type that ran it ("" at the top level).
// Kind "denial" is Claude Code refusing a tool no Jhon guard judges (Tool
// names it): the bench HOME's engram, which --allowedTools leaves out, is
// denied by Claude Code's own permissions, not by Jhon (L761), so it is no
// refusal and is reported apart.
type guardEvent struct {
	Kind, Command, Agent, Tool string
}

// guardEvents are the guard refusals and dialogs of a stream.jsonl:
// Claude Code's tool_result errors refusalText matches or a result record
// lists in permission_denials (an ask the headless runtime denied reads
// only the guard's reason, T105), with their tool_use's command and, under
// a parent_tool_use_id, the sub-agent's type; pi's select dialogs titled
// piGuardTitle, each with the bash call whose error result carries the
// dialog's reason, and pi's other error results refusalText matches.
func guardEvents(b []byte) []guardEvent {
	var out []guardEvent
	cmds := map[string]string{}
	tools := map[string]string{}
	agents := map[string]string{}
	var dialogs []string
	recs := records(b)
	denied := map[string]bool{}
	for _, r := range recs {
		if str(r["type"]) == "result" {
			for _, d := range list(r["permission_denials"]) {
				denied[str(at(d, "tool_use_id"))] = true
			}
		}
	}
	for _, r := range recs {
		switch str(r["type"]) {
		case "tool_execution_start":
			if str(r["toolName"]) == "bash" {
				cmds[str(r["toolCallId"])] = str(at(r, "args", "command"))
			}
			continue
		case "extension_ui_request":
			if reason, ok := strings.CutPrefix(str(r["title"]), piGuardTitle); ok && str(r["method"]) == "select" {
				dialogs = append(dialogs, reason)
			}
			continue
		case "tool_execution_end":
			text := contentText(at(r, "result", "content"))
			if i := slices.IndexFunc(dialogs, func(d string) bool { return strings.HasPrefix(text, d) }); i >= 0 && r["isError"] == true {
				out = append(out, guardEvent{Kind: "dialog", Command: cmds[str(r["toolCallId"])]})
				dialogs = slices.Delete(dialogs, i, i+1)
			} else if r["isError"] == true && refusalText.MatchString(text) {
				out = append(out, guardEvent{Kind: "refusal", Command: cmds[str(r["toolCallId"])]})
			}
			continue
		}
		parent := str(r["parent_tool_use_id"])
		for _, p := range list(at(r, "message", "content")) {
			switch str(at(p, "type")) {
			case "tool_use":
				tools[str(at(p, "id"))] = str(at(p, "name"))
				switch id := str(at(p, "id")); str(at(p, "name")) {
				case "Bash":
					cmds[id] = str(at(p, "input", "command"))
				case "Agent", "Task":
					agents[id] = str(at(p, "input", "subagent_type"))
				}
			case "tool_result":
				if at(p, "is_error") != true || !refusalText.MatchString(contentText(at(p, "content"))) && !denied[str(at(p, "tool_use_id"))] {
					continue
				}
				e := guardEvent{Kind: "refusal", Command: cmds[str(at(p, "tool_use_id"))]}
				if tool := tools[str(at(p, "tool_use_id"))]; tool != "" && !slices.Contains(guardedTools, tool) {
					e = guardEvent{Kind: "denial", Tool: tool}
				}
				if parent != "" {
					e.Agent = cmpOr(agents[parent], "general-purpose")
				}
				out = append(out, e)
			}
		}
	}
	// A dialog whose call never ended names no command: unknown.
	for range dialogs {
		out = append(out, guardEvent{Kind: "dialog"})
	}
	return out
}

// streamRefusals are the top-level refusals of a run's stream, as
// metrics.json counts them (T105): the replay reads the same events.
func streamRefusals(b []byte) *int {
	n := 0
	for _, e := range guardEvents(b) {
		if e.Kind == "refusal" && e.Agent == "" {
			n++
		}
	}
	return &n
}

// ReplayEvent is one replayed refusal or dialog: its side, cell, run,
// kind, command and the reference's verdict (refused, asked, passed, or
// "replay error: …").
type ReplayEvent struct {
	Side      string `json:"side"`
	Cell      string `json:"cell"`
	Run       string `json:"run"`
	Kind      string `json:"kind"`
	Command   string `json:"command"`
	Reference string `json:"reference"`
}

// shared is whether the reference gives the same verdict: it refuses or asks.
func (e ReplayEvent) shared() bool { return e.Reference == "refused" || e.Reference == "asked" }

// ReplayReport is the replay's part of certify.json: the reference guard,
// the refusals and dialogs it shares (excluded), and the ones that count.
type ReplayReport struct {
	Guard          string        `json:"guard"`
	BaselineShared []ReplayEvent `json:"baseline_shared"`
	Counted        []ReplayEvent `json:"counted"`
}

// replayer replays runs through guard, in an isolated env under scratch.
type replayer struct {
	guard string
	env   []string
	rep   *ReplayReport
	notes []string
}

// newReplayer prepares the scratch HOME, XDG dirs and TMPDIR under dir.
func newReplayer(guard, dir string) (*replayer, error) {
	abs, err := filepath.Abs(guard)
	if err != nil {
		return nil, err
	}
	if fi, err := os.Stat(abs); err != nil || fi.IsDir() {
		return nil, fmt.Errorf("--replay-guard %s is no binary", guard)
	}
	scratch := filepath.Join(dir, "replay")
	env := []string{"PATH=/usr/local/bin:/usr/bin:/bin", "LANG=C.UTF-8", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + filepath.Join(scratch, "gitconfig")}
	for _, d := range []struct{ name, path string }{{"HOME", "home"}, {"TMPDIR", "tmp"}, {"XDG_CONFIG_HOME", "xdg/config"},
		{"XDG_DATA_HOME", "xdg/data"}, {"XDG_STATE_HOME", "xdg/state"}, {"XDG_CACHE_HOME", "xdg/cache"}} {
		p := filepath.Join(scratch, d.path)
		if err := os.MkdirAll(p, 0o755); err != nil {
			return nil, err
		}
		env = append(env, d.name+"="+p)
	}
	if err := os.WriteFile(filepath.Join(scratch, "gitconfig"), nil, 0o644); err != nil {
		return nil, err
	}
	return &replayer{guard: abs, env: env, rep: &ReplayReport{Guard: abs, BaselineShared: []ReplayEvent{}, Counted: []ReplayEvent{}}}, nil
}

// run replays the refusals and dialogs of the run in dir and takes the
// shared ones off m: the top-level refusals metrics.json counts and the
// dialogs it counts as declined. A stream that disagrees with metrics.json
// excludes nothing.
func (rp *replayer) run(side, dir string, m *Metrics) {
	cell, name := m.Task+" "+cmpOr(m.Variant, "plain")+" "+m.Condition, filepath.Base(dir)
	refusals, dialogs := deref(m.Refusals), m.UIRequestsDeclined
	add := func(kind, command, ref string) ReplayEvent {
		e := ReplayEvent{Side: side, Cell: cell, Run: name, Kind: kind, Command: command, Reference: ref}
		if e.shared() {
			rp.rep.BaselineShared = append(rp.rep.BaselineShared, e)
		} else {
			rp.rep.Counted = append(rp.rep.Counted, e)
		}
		return e
	}
	b, err := os.ReadFile(filepath.Join(dir, "stream.jsonl"))
	if err != nil {
		for range refusals {
			add("refusal", "", "replay error: no stream.jsonl")
		}
		for range dialogs {
			add("dialog", "", "replay error: no stream.jsonl")
		}
		return
	}
	events := guardEvents(b)
	top := len(slices.DeleteFunc(slices.Clone(events), func(e guardEvent) bool { return e.Kind != "refusal" || e.Agent != "" }))
	shown := len(slices.DeleteFunc(slices.Clone(events), func(e guardEvent) bool { return e.Kind != "dialog" }))
	problem := map[string]string{}
	if top != refusals {
		problem["refusal"] = fmt.Sprintf("replay error: the stream has %d top-level refusals, metrics.json %d", top, refusals)
	}
	if shown > dialogs {
		problem["dialog"] = fmt.Sprintf("replay error: the stream has %d guard dialogs, metrics.json %d declined", shown, dialogs)
	}
	repo := filepath.Join(dir, "repo")
	_, repoErr := os.Stat(repo)
	excluded := map[string]int{}
	for _, e := range events {
		if e.Kind == "denial" {
			continue
		}
		kind, counted := e.Kind, e.Kind == "dialog" || e.Agent == ""
		if !counted {
			kind += " (" + e.Agent + ", not counted in metrics.json)"
		}
		command := rewriteRepo(e.Command, name, repo)
		var ref string
		switch {
		case counted && problem[e.Kind] != "":
			ref = problem[e.Kind]
		case repoErr != nil:
			ref = "replay error: no repo"
		case command == "":
			ref = "replay error: the stream names no command"
		default:
			ref = rp.verdict(command, repo, e.Agent)
		}
		if add(kind, command, ref).shared() && counted {
			excluded[e.Kind]++
		}
	}
	// Refusals metrics.json counts that the stream does not show.
	for range refusals - top {
		add("refusal", "", problem["refusal"])
	}
	if m.Refusals != nil {
		m.Refusals = ip(refusals - excluded["refusal"])
	}
	m.UIRequestsDeclined -= excluded["dialog"]
}

// rewriteRepo points a command's paths to the run's repo where it is read
// now, when the run's results dir moved since.
func rewriteRepo(command, run, repo string) string {
	re := regexp.MustCompile(`[^\s'"=:;&|()<>]*/runs/` + regexp.QuoteMeta(run) + `/repo\b`)
	return re.ReplaceAllLiteralString(command, repo)
}

// verdict is the reference's verdict on command run in repo, at the top
// level (permission mode default) or in a sub-agent of type agent: exit 2
// refused, an ask (or deny) decision asked (refused), exit 0 otherwise
// passed; anything else a replay error.
func (rp *replayer) verdict(command, repo, agent string) string {
	p := map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": map[string]any{"command": command},
		"cwd": repo, "permission_mode": "default"}
	if agent != "" {
		p["agent_id"], p["agent_type"] = "certify-replay", agent
	}
	payload, _ := json.Marshal(p)
	ctx, cancel := context.WithTimeout(context.Background(), replayTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, rp.guard, replayEntry...)
	cmd.Dir, cmd.Env, cmd.Stdin = repo, rp.env, bytes.NewReader(payload)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case ctx.Err() != nil:
		return "replay error: no answer in " + replayTimeout.String()
	case err == nil:
		var j map[string]any
		_ = json.Unmarshal(out.Bytes(), &j)
		switch str(at(j, "hookSpecificOutput", "permissionDecision")) {
		case "ask":
			return "asked"
		case "deny":
			return "refused"
		}
		return "passed"
	case errors.As(err, &exit) && exit.ExitCode() == 2 && !strings.Contains(errOut.String(), "the render is broken"):
		return "refused"
	case errors.As(err, &exit):
		first, _, _ := strings.Cut(strings.TrimSpace(errOut.String()), "\n")
		return fmt.Sprintf("replay error: exit %d: %s", exit.ExitCode(), first)
	}
	return "replay error: " + err.Error()
}

// printReplay writes the replay's lists and whether the baseline's
// refusals and dialogs are all shared.
func printReplay(w io.Writer, r *ReplayReport) {
	line := func(e ReplayEvent) {
		verdict := e.Reference
		if !strings.HasPrefix(verdict, "replay error") {
			verdict = "reference " + verdict
		}
		command := strings.ReplaceAll(cmpOr(e.Command, "(command unknown)"), "\n", " ⏎ ")
		fmt.Fprintf(w, "        %s %s %s: %s, %s: %s\n", e.Side, e.Cell, e.Run, e.Kind, verdict, command)
	}
	fmt.Fprintf(w, "replay: reference guard %s\n", r.Guard)
	fmt.Fprintf(w, "  baseline-shared (excluded): %d\n", len(r.BaselineShared))
	for _, e := range r.BaselineShared {
		line(e)
	}
	fmt.Fprintf(w, "  counted: %d\n", len(r.Counted))
	for _, e := range r.Counted {
		line(e)
	}
	shared, of := 0, 0
	for _, e := range slices.Concat(r.BaselineShared, r.Counted) {
		if e.Side == "baseline" && !strings.Contains(e.Kind, "not counted") {
			of++
			if e.shared() {
				shared++
			}
		}
	}
	fmt.Fprintf(w, "  baseline: %d of %d refusals and dialogs shared\n", shared, of)
}
