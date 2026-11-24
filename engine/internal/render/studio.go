package render

import (
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
)

// T238 (odd/tasks/studio-integration-v1.md S5): a cli integration is a
// program of its own (jhon-studio), with no capability for the render to
// drop. The main session's fixed text, lean (prose/lean.md) and full
// (prose/orchestrator.md), says on a line of its own what a request for its
// work means (a video, "jhon:video": its video skill, {{runtime:video-skill}},
// and its page, docs/jhon-studio.md, which an install carries). That line
// names the tool by its display name, and is rendered only while its
// setting is on: off, the text is what it was without it. The render reads
// the setting at sync, as context7's (context7.go); `jhon start` keeps a
// tool that does not fit the running jhon out of the session itself.

// toolsOff are the display names of the optional cli integrations whose
// setting on says is off.
func toolsOff(on func(setting string) bool) []string {
	var out []string
	for _, m := range integrations.Optional() {
		if m.Kind == "cli" && m.Display != "" && !on(m.Setting) {
			out = append(out, m.Display)
		}
	}
	return out
}

// withoutOffTools is body without the lines that name one of off, each a
// display name, so a sentence about a tool that is off is not rendered.
func withoutOffTools(body string, off []string) string {
	named := func(line string) bool {
		return slices.ContainsFunc(off, func(name string) bool { return strings.Contains(line, name) })
	}
	if !named(body) {
		return body
	}
	lines := strings.Split(body, "\n")
	return strings.Join(slices.DeleteFunc(lines, named), "\n")
}
