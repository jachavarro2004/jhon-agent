package hook

import (
	"fmt"
	"os"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
)

// InspectGuard keeps an inspecting agent's Bash read-only by construction
// (inspect-guard, T271): a line passes when every construct in it and every
// program it runs is on the read-only list (guard.ReadOnly), and no reader
// in it reaches a credentials file (S10, as the safety guard reads it). The
// arguments are Jhon's own readers it may run besides (`codegraph
// callers`), each the leading words of one program of the line. It is a
// filter, not a container: the sandbox contains.
func InspectGuard(args []string, io IO) int {
	scoped, name, rest := owner(args)
	var forms [][]string
	for _, a := range rest {
		if f := strings.Fields(a); len(f) > 0 {
			forms = append(forms, f)
		}
	}
	var p Payload
	block := func(reason string) int {
		fmt.Fprintf(io.Err, "inspect-guard: %s\n%s", reason, staleNote(p))
		return 2
	}
	p, ok := read(io.In)
	if !ok {
		return block("hook payload is not valid JSON")
	}
	if scoped && name == "" {
		return block("--agent was given without an agent name")
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
	inEnv := func(n string) bool { _, ok := os.LookupEnv(n); return ok }
	if why := guard.ReadOnly(command, inEnv, forms); why != "" {
		return block(why)
	}
	for _, f := range guard.Classify(command, guard.ContextOf(p.Cwd())) {
		if f.Level == "deny" {
			fix := f.Alt
			if fix == "" {
				fix = "read files with the Read, Grep or Glob tool"
			}
			return block(f.What + ": " + strings.TrimSuffix(f.Why, ".") + ". Instead: " + fix)
		}
	}
	return 0
}
