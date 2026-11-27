package integrations

import (
	"maps"
	"slices"
)

// dataFolders are the folders each integration keeps its own data in when
// it runs in a project (T268, L1762): CodeGraph's index (.codegraph, and
// .codegraph-wsl under WSL) and Engram's data folder, should it ever land
// in one. None of it is project input. A v1 manifest has no field for them,
// and a new field is v2 (integrations/README.md), so they are named here,
// by the integration they belong to.
var dataFolders = map[string][]string{
	CodeGraph: {".codegraph", ".codegraph-wsl"},
	Engram:    {".engram"},
}

// DataFolders are the folder names every integration keeps its own data in
// inside a project, sorted: tool data, never project input, whatever the
// project's .gitignore says (layout.ToolData).
func DataFolders() []string {
	var out []string
	for _, name := range slices.Sorted(maps.Keys(dataFolders)) {
		out = append(out, dataFolders[name]...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// DataFolderOf is the display name of the integration that keeps its data
// in folder, a name DataFolders lists; "" for any other.
func DataFolderOf(folder string) string {
	for name, folders := range dataFolders {
		if slices.Contains(folders, folder) {
			for _, m := range All() {
				if m.Name == name && m.Display != "" {
					return m.Display
				}
			}
			return name
		}
	}
	return ""
}
