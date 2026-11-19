package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/grant"
	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// SafetyGuard is the one PreToolUse Bash guard for destructive and
// outward-facing effects (safety-guard): a deny refuses, in every mode; an
// ask asks the owner unless a live grant covers it, or the owner's mode for
// its project (`jhon mode`), which passes it with a note whether or not the
// owner can be asked; everything else passes.
func SafetyGuard(args []string, io IO) int {
	scoped, name, _ := owner(args)
	var p Payload
	block := func(reason string) int {
		fmt.Fprintf(io.Err, "safety-guard: %s\n%s", reason, staleNote(p))
		return 2
	}
	p, ok := read(io.In)
	if !ok {
		return block("hook payload is not valid JSON")
	}
	if scoped && name == "" {
		return block("--agent was given without an agent name; the render is broken")
	}
	if othersCall(scoped, name, p) {
		return 0
	}
	if p["tool_name"] != "Bash" {
		return 0
	}
	command, isStr := p.Obj("tool_input")["command"].(string)
	if !isStr || strings.TrimSpace(command) == "" {
		return block("Bash call carries no command")
	}
	// A sub-agent that ends or is stopped leaves an unbounded wait sleeping
	// forever; the main session hears of its background commands ending.
	if strings.TrimSpace(p.Str("agent_id")) != "" && guard.UnboundedWait(command) {
		return block("refused: an `until` or `while` loop that sleeps with nothing bounding it. " +
			"A sub-agent that ends or is stopped leaves it running, and only the owner can stop it. " +
			"Bound the wait: `timeout 600 sh -c '<loop>'`, or `for i in $(seq 1 60); do <check> && break; sleep 10; done`; " +
			"or run the command in the foreground with a timeout.")
	}
	cwd := p.Str("cwd")
	if !filepath.IsAbs(cwd) {
		cwd, _ = os.Getwd()
	}
	ctx := guard.ContextOf(cwd)
	findings := guard.Classify(command, ctx)

	var lines, alts []string
	for _, f := range findings {
		if f.Level != "deny" {
			continue
		}
		if l := f.What + ": " + f.Why; !slices.Contains(lines, l) {
			lines = append(lines, l)
		}
		if f.Alt != "" && !slices.Contains(alts, f.Alt) {
			alts = append(alts, f.Alt)
		}
	}
	if len(lines) > 0 {
		instead := ""
		if len(alts) > 0 {
			instead = "Allowed instead: " + strings.Join(alts, "; ") + ". "
		}
		return block("refused: " + strings.Join(lines, "; ") + ". This is destructive or irreversible, and never granted to an agent. " +
			instead + "Stop and report this refusal verbatim; if it is intended, the owner runs it with `!` in Claude Code, or in their own terminal in pi.")
	}
	// An ask stays open unless a live grant of its class covers the
	// repository it targets, or the owner's mode does (modeFor): the
	// session's own project's for a finding that names no other repository,
	// and both projects' for one in another, so a cd into a super-yolo
	// project lends a normal session nothing (L157).
	own := project.Root(cwd)
	var open []guard.Finding
	var passed []modePass
	for _, f := range findings {
		if f.Level != "ask" || (slices.Contains(grant.Classes, f.Cls) && f.Root != guard.UnknownRoot && grant.Live(f.Root, f.Cls)) {
			continue
		}
		if m, ok := modeFor(own, f.Root, f.Cls); ok {
			passed = append(passed, modePass{m, f.Cls, f.What})
			continue
		}
		open = append(open, f)
	}
	if len(open) == 0 {
		passByMode(io, p, "safety-guard", passed)
		return 0
	}
	var classes []string
	for _, f := range open {
		if !slices.Contains(classes, f.Cls) {
			classes = append(classes, f.Cls)
		}
	}
	what := func(cls string) string {
		var ws []string
		for _, f := range open {
			if f.Cls == cls && !slices.Contains(ws, f.What) {
				ws = append(ws, f.What)
			}
		}
		return strings.Join(ws, ", ")
	}
	if canAsk(p) {
		var parts []string
		for _, c := range classes {
			parts = append(parts, c+": "+what(c))
		}
		fmt.Fprintln(io.Out, askJSON(strings.Join(parts, "; ")))
		return 0
	}
	var parts []string
	for _, c := range classes {
		if slices.Contains(grant.Classes, c) {
			parts = append(parts, what(c)+": "+grantHint(c))
		} else {
			parts = append(parts, c+": "+what(c)+"; it is refused outside a prompt the owner can answer")
		}
	}
	var instead []string
	for _, f := range open {
		if f.Alt != "" && !slices.Contains(instead, f.Alt) {
			instead = append(instead, f.Alt)
		}
	}
	if len(instead) > 0 {
		parts = append(parts, "Allowed instead: "+strings.Join(instead, "; ")+".")
	}
	return block(strings.Join(parts, " "))
}
