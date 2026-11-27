package layout

import (
	"slices"
	"testing"
)

// T268: ToolData is jhon-studio's output and each integration's data
// folder, at any depth, as a folder or a path in one; a name that only
// starts like one, or a file named after the tool, is project input.
func TestToolData(t *testing.T) {
	for p, want := range map[string]bool{
		".codegraph":                      true,
		".codegraph/codegraph.db":         true,
		".codegraph/":                     true,
		"/home/u/p/.codegraph/x":          true,
		"sub/.codegraph-wsl/codegraph.db": true,
		".engram/engram.db":               true,
		".jhon/studio/tour/brief.md":      true,
		"web/.jhon/studio/tour/out/a.mp4": true,
		".codegraphx/a":                   false,
		"src/codegraph.go":                false,
		"docs/.codegraph.md":              false,
		".jhon/studio.yaml":               false,
		"out-1/build.log":                 false,
		"openspec/changes/c/tasks.md":     false,
	} {
		if got := ToolData(p); got != want {
			t.Errorf("ToolData(%q) = %v, want %v", p, got, want)
		}
	}
	if got := ToolDataFolders(); !slices.Equal(got, []string{".jhon/studio", ".codegraph", ".codegraph-wsl", ".engram"}) {
		t.Errorf("ToolDataFolders() = %q", got)
	}
	if got := ToolDataExcludes(); got[0] != ":(top,exclude,glob)**/.jhon/studio/**" || len(got) != 4 {
		t.Errorf("ToolDataExcludes() = %q", got)
	}
	// Every layout leaves it out as no code of the project's.
	if !OpenSpec.Ignored(".codegraph/codegraph.db") || !Native.Ignored("sub/.codegraph-wsl/x") || OpenSpec.Ignored("src/a.go") {
		t.Error("Ignored does not leave tool data out")
	}
}
