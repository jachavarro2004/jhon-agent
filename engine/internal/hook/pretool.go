package hook

import (
	"bytes"
	"encoding/json"
	"fmt"
	goio "io"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
)

type preGuard struct {
	name    string
	tools   []string
	handler Handler
}

// preToolHandlers are the handler of each guard guard.Matchers names.
var preToolHandlers = map[string]Handler{
	"inspect-guard": InspectGuard, "safety-guard": SafetyGuard, "vcs-guard": VCSGuard,
	"framework-guard": FrameworkGuard, "secrets-guard": SecretsGuard, "scope-guard": ScopeGuard, "route-guard": RouteGuard,
}

// preToolGuards are the guards pre-tool runs, in the order it runs them,
// each with the tools its matcher takes (guard.Matchers).
var preToolGuards = func() []preGuard {
	var out []preGuard
	for _, m := range guard.Matchers {
		h, ok := preToolHandlers[m.Guard]
		if !ok {
			panic("hook: guard.Matchers names " + m.Guard + ", which has no handler")
		}
		out = append(out, preGuard{m.Guard, m.Tools, h})
	}
	return out
}()

// PreTool is one agent's guards in one process (docket AY B4): `pre-tool
// --agent <owner> <guard> <args>... <guard> <args>...`. Each named guard
// whose matcher takes the call judges the one payload with `--agent
// <owner>` and its own args, in preToolGuards' order. The first refusal is
// the answer, exit code and its guard's own message unchanged; otherwise the
// first ask; otherwise the call is allowed, with every guard's note for the
// agent joined in one. A command line it cannot read is a broken render,
// and refuses. With sandbox.mode on, a Bash or Monitor call of Claude Code
// that was not refused is answered with its command wrapped (sandboxWrap),
// or refused where it cannot be. An Agent call of the main session the
// guards allowed is answered with its brief followed by the decisions on
// record and the code map (launchInput), when either has something.
func PreTool(args []string, io IO) int {
	refuse := func(reason string) int {
		fmt.Fprintf(io.Err, "pre-tool: %s; the render is broken\n", reason)
		return 2
	}
	// --matchers prints each guard and the tools it judges, one per line.
	if len(args) == 1 && args[0] == "--matchers" {
		for _, g := range preToolGuards {
			fmt.Fprintf(io.Out, "%s\t%s\n", g.name, strings.Join(g.tools, "|"))
		}
		return 0
	}
	scoped, name, rest := owner(args)
	if !scoped || name == "" {
		return refuse("no --agent <owner>")
	}
	given := map[string][]string{}
	current := ""
	for _, a := range rest {
		if strings.HasSuffix(a, "-guard") && !strings.ContainsAny(a, "/ ") {
			if !slices.ContainsFunc(preToolGuards, func(g preGuard) bool { return g.name == a }) {
				return refuse("unknown guard " + a)
			}
			current, given[a] = a, []string{}
			continue
		}
		if current == "" {
			return refuse("argument " + a + " names no guard")
		}
		given[current] = append(given[current], a)
	}
	if len(given) == 0 {
		return refuse("no guard named")
	}
	payload, err := goio.ReadAll(io.In)
	if err != nil {
		return refuse("stdin could not be read")
	}
	tool, plan := "", false
	if p, ok := read(bytes.NewReader(payload)); ok {
		tool, plan = p.Str("tool_name"), p.Str("permission_mode") == "plan"
	}
	var ask []byte
	var notes []string
	for _, g := range preToolGuards {
		gargs, named := given[g.name]
		// A payload that is not JSON goes to every guard, which refuses it.
		if !named || (tool != "" && !slices.Contains(g.tools, tool) && !slices.Contains(g.tools, guard.All)) {
			continue
		}
		var out, errs bytes.Buffer
		code := g.handler(append([]string{"--agent", name}, gargs...), IO{bytes.NewReader(payload), &out, &errs})
		if code != 0 {
			io.Out.Write(out.Bytes())
			io.Err.Write(errs.Bytes())
			return code
		}
		if note, ok := noteOf(out.Bytes()); ok {
			notes = append(notes, note)
		} else if ask == nil && out.Len() > 0 {
			ask = out.Bytes()
		}
	}
	// An Agent call of the main session that route-guard judged and no guard
	// refused or asked about launches with its brief followed by the
	// decisions on record and the code map (T134, L1036): a deny or an ask
	// stands as it came, and with nothing to add the answer is today's.
	if _, judged := given["route-guard"]; judged && ask == nil && tool == "Agent" {
		if updated := launchInput(name, payload); updated != nil {
			p, _ := read(bytes.NewReader(payload))
			fmt.Fprint(io.Out, launchJSON(notes, updated, p.Str("permission_mode")))
			return 0
		}
	}
	// The guards judged the command as the model wrote it; with sandbox.mode
	// on, Claude Code runs it wrapped, under the decision they reached.
	updated, refusal := sandboxWrap(name, payload)
	if updated != nil {
		answer, ok := sandboxedJSON(ask, notes, updated, plan)
		if ok {
			fmt.Fprint(io.Out, answer)
			return 0
		}
		refusal = "sandbox: a guard's answer for the wrapped command could not be read; the call is refused"
	}
	if refusal != "" {
		fmt.Fprintln(io.Err, refusal)
		return 2
	}
	// With no wrap (the mode off), the command still runs as the agent's:
	// it carries the marker git's merge gate reads (T219 S9), handed back
	// with no decision of pre-tool's own, so Claude Code's permissions decide
	// as they would.
	if marked := agentMarked(name, payload); marked != nil {
		if answer, ok := sandboxedJSON(ask, notes, marked, true); ok {
			fmt.Fprint(io.Out, answer)
			return 0
		}
	}
	if ask == nil && len(notes) > 0 {
		fmt.Fprint(io.Out, noteJSON(strings.Join(notes, "; ")))
		return 0
	}
	io.Out.Write(ask)
	return 0
}

// noteOf is the note of a guard's output that allows the call with a note
// for the agent and decides nothing else (noteJSON).
func noteOf(out []byte) (string, bool) {
	var j map[string]map[string]any
	if json.Unmarshal(out, &j) != nil || len(j) != 1 || len(j["hookSpecificOutput"]) != 2 {
		return "", false
	}
	note, ok := j["hookSpecificOutput"]["additionalContext"].(string)
	return note, ok && note != "" && j["hookSpecificOutput"]["hookEventName"] == "PreToolUse"
}
