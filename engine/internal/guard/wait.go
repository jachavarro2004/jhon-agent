package guard

import "github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"

// A wait loop is an `until` or `while` loop that sleeps: it polls for an
// output, a process, or a check to change. Unbounded, it outlives the agent
// that started it: a sub-agent that ends or is stopped leaves it sleeping
// forever, and only the owner can stop it (measured: two workers in one client
// session, 2026-10-27).
var (
	// loopHead is `until` or `while` where a command starts, in bash or in
	// fish, and not `while read`, which ends with its input.
	loopHead   = lazyre.New(`(?:^|[;&|(\n'"]|\bdo\b|\bthen\b|\belse\b)\s*(?:until|while)\s+`)
	readsInput = lazyre.New(`^(?:read|IFS)\b`)
	// loopBody is a bash `do ... done` or a fish `; ... end` after the head.
	loopBody = lazyre.New(`(?s)\bdo\b.*\bdone\b|;\s*.*\bend\b`)
	sleeps   = lazyre.New(`\bsleep\s`)
	// bounded is what ends a wait however long the condition takes: a
	// `timeout <n>` around it.
	bounded = lazyre.New(`\btimeout\s+(?:-[a-zA-Z]+\s+\S+\s+)*\d`)
)

// UnboundedWait is whether command runs a wait loop that nothing bounds: an
// `until` or `while` loop that sleeps, with no `timeout` around it. A `for`
// loop over a fixed range is bounded, so it is never one.
func UnboundedWait(command string) bool {
	if !sleeps.MatchString(command) || bounded.MatchString(command) {
		return false
	}
	for _, m := range loopHead.FindAllStringIndex(command, -1) {
		if readsInput.MatchString(command[m[1]:]) {
			continue
		}
		if loopBody.MatchString(command[m[1]:]) {
			return true
		}
	}
	return false
}
