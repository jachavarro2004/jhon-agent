package guard

import "strings"

// A Matcher is one guard and the tools whose calls it judges.
type Matcher struct {
	Guard string
	Tools []string
}

// Matchers are every guard `jhon hook pre-tool` runs, in the order it runs
// them, with the tools each judges: the one source the hook, the render, and
// (through `jhon hook pre-tool --matchers`) the conformance suite read.
var Matchers = []Matcher{
	{"inspect-guard", []string{"Bash"}},
	{"safety-guard", []string{"Bash"}},
	{"vcs-guard", []string{"Edit", "MultiEdit", "Write", "NotebookEdit"}},
	{"framework-guard", []string{"Edit", "MultiEdit", "Write", "Bash"}},
	{"secrets-guard", []string{"Read", "Grep", "Glob", "Edit", "MultiEdit", "Write", "NotebookEdit"}},
	{"scope-guard", []string{"Edit", "MultiEdit", "Write", "NotebookEdit", "Bash"}},
	{"route-guard", []string{"Edit", "MultiEdit", "Write", "NotebookEdit", "Bash", "Agent"}},
}

// ToolOrder is the order a combined Claude Code matcher names its tools in.
var ToolOrder = []string{"Read", "Grep", "Glob", "Edit", "MultiEdit", "Write", "NotebookEdit", "Bash", "Agent", "SendMessage"}

// ToolsOf is the tools guard judges; none for an unknown guard.
func ToolsOf(guard string) []string {
	for _, m := range Matchers {
		if m.Guard == guard {
			return m.Tools
		}
	}
	return nil
}

// All is the tool a guard names to judge every tool.
const All = "*"

// MatcherOf is guard's tools as a hook matcher: `Edit|MultiEdit|Write|NotebookEdit`.
func MatcherOf(guard string) string { return strings.Join(ToolsOf(guard), "|") }
