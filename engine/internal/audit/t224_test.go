package audit

import "testing"

// T224 S2: a launch counts as a relaunch after a result that reports a
// refusal only when the result holds one of Jhon's own refusal lines, by the
// exact prefix engine/internal/hook and engine/internal/sandbox print; the
// word "refused:" in a tool's ordinary output does not count.
func TestRelaunchCountsOnlyJhonRefusalLines(t *testing.T) {
	launch := func(id, result string) step {
		return step{kind: "delegate", id: id, typ: "jhon-builder", phase: "apply", change: "c1", result: result}
	}
	relaunches := func(first string) int {
		return outcomeOf(auditOf(t, user("go"), launch("s01", first), launch("s02", "done")), "c1").Relaunches
	}
	for _, text := range []string{
		// The owner's session (2026-11-23), names made neutral.
		"**Status**: success\n  - Any other ordinary entry is still refused: the new tests cover editing one onto a card.",
		"const AI_STATUS = { not_configured: 503, refused: 422, upstream: 502 } as const;",
		"The server refused: 403 Forbidden, so the client retries with a token.",
		"Refused: none. Every task is done.",
		"the sandbox: on, and nothing was refused",
		// A guard's name inside another word is no guard's line (L5).
		"x_safety-guard: refused: rm -r ~",
		"my-safety-guard: refused: rm -r ~",
		"x__safety-guard: refused: rm -r ~",
	} {
		if n := relaunches(text); n != 0 {
			t.Errorf("%q: %d relaunch(es), want 0", text, n)
		}
	}
	for _, text := range []string{
		// The owner's session: the refusal reported verbatim, as the guard tells the agent to.
		"The safety guard refused the preview command. Refusal, verbatim: \"safety-guard: refused: rm -r $(…): its target cannot be resolved. This is destructive or irreversible, and never granted to an agent.\"",
		"safety-guard: refused: an `until` or `while` loop that sleeps with nothing bounding it.",
		"PreToolUse:Bash hook error: [jhon hook safety-guard]: safety-guard: Bash call carries no command",
		"scope-guard: Write /p/src/b.ts is outside the surfaces this agent was launched with",
		"route-guard: launch it from the main session, where the owner is asked.",
		"vcs-guard: Write call names no file",
		"secrets-guard: Read of a credentials file.",
		"framework-guard: Edit of /fw/x is under the protected path /fw: the framework is the owner's",
		"inspect-guard: option -o is not allowed: it writes a file or runs an external program",
		"sandbox: sandbox.mode is on and the sandbox cannot run here: no bwrap; the call is refused; fix: install it",
		"Stopped. the sandbox refused a write to /home/u/.cache/x; that is a stop: tell the owner the path (jhon settings sandbox.allow_write), never write it elsewhere",
		// Wrapped in markdown or brackets (L4 B3).
		"Stopped. **safety-guard: refused: rm -r ~: it reaches the home directory.**",
		"Stopped (safety-guard: refused: rm -r ~).",
		"Stopped: [safety-guard: refused: rm -r ~]",
		"_safety-guard: refused: rm -r ~_",
		"__safety-guard: refused: rm -r ~__",
		// The turn cap still counts.
		"Stopped: reached maxTurns (80) before finishing tasks 4-6",
	} {
		if n := relaunches(text); n != 1 {
			t.Errorf("%q: %d relaunch(es), want 1", text, n)
		}
	}
}
