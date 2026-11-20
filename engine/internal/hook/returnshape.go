package hook

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/rename"
	"slices"
	"strings"
)

var (
	statusLine     = lazyre.New(`(?m)^\s*status:\s*(?:success|partial|blocked)\b`)
	validationLine = lazyre.New(`(?m)^\s*validation:`)
)

// ReturnShaped is whether a writer's final text carries contracts/efficiency.md's return shape.
func ReturnShaped(text string) bool {
	return statusLine.MatchString(text) && validationLine.MatchString(text)
}

// ReturnShape is the SubagentStop hook that notes a bounded writer ending
// without the return shape (return-shape): the stop goes through, with the
// note as a systemMessage; an advisory since T154 (L1394), as no real case
// put the block there.
func ReturnShape(args []string, io IO) int {
	var writers []string
	for _, a := range args {
		if a = strings.TrimSpace(a); a != "" {
			writers = append(writers, rename.AgentNow(a))
		}
	}
	p, ok := read(io.In)
	if !ok {
		return 0
	}
	kind := rename.AgentNow(strings.TrimSpace(jsString(p["agent_type"])))
	if p["hook_event_name"] == "SubagentStop" && slices.Contains(writers, kind) &&
		p["stop_hook_active"] != true && !ReturnShaped(jsString(p["last_assistant_message"])) {
		fmt.Fprint(io.Out, marshal(map[string]string{"systemMessage": "note (not a refusal; the writer stopped): the " + kind +
			" ended without the return shape of contracts/efficiency.md (its `status:` and `validation:` lines): read its result for what changed and what was run"}))
	}
	return 0
}
