package hook

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"slices"
	"strings"
)

const shape = "End with the return shape of contracts/efficiency.md, as text:\n" +
	"status: success | partial | blocked\nsummary: <what changed and why, the task or requirement ids covered>\n" +
	"files_changed:\n  - <path>: <change>\nvalidation:\n  - <exact command>: <observed result>\n" +
	"risks: <what remains, or none>\ndecision: <only when the owner must choose>"

var (
	statusLine     = lazyre.New(`(?m)^\s*status:\s*(?:success|partial|blocked)\b`)
	validationLine = lazyre.New(`(?m)^\s*validation:`)
)

// ReturnShaped is whether a writer's final text carries contracts/efficiency.md's return shape.
func ReturnShaped(text string) bool {
	return statusLine.MatchString(text) && validationLine.MatchString(text)
}

// ReturnShape is the SubagentStop hook that asks a bounded writer, once,
// for the return shape before it ends (return-shape).
func ReturnShape(args []string, io IO) int {
	var writers []string
	for _, a := range args {
		if a = strings.TrimSpace(a); a != "" {
			writers = append(writers, a)
		}
	}
	p, ok := read(io.In)
	if !ok {
		return 0
	}
	if p["hook_event_name"] == "SubagentStop" && slices.Contains(writers, strings.TrimSpace(jsString(p["agent_type"]))) &&
		p["stop_hook_active"] != true && !ReturnShaped(jsString(p["last_assistant_message"])) {
		fmt.Fprint(io.Out, marshal(map[string]string{"decision": "block", "reason": shape}))
	}
	return 0
}
