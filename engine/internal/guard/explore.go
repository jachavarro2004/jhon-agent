package guard

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
)

// ExploreAgent is the agent whose writes ExploreMayWrite bounds
// (prose/agents.yaml: "Writes only the change's exploration.md or
// research.md, never code"; T123).
const ExploreAgent = "explore"

// ExploreRecords are the only files the explore agent writes, in its change's folder.
var ExploreRecords = []string{"exploration.md", "research.md"}

// ExploreScope is the rule as a refusal states it.
const ExploreScope = "explore writes only its change's exploration.md or research.md (<layout>/changes/<change>/exploration.md or research.md, openspec/ or ldd/)"

// ExploreMayWrite is whether p, an absolute path, is a change's
// exploration.md or research.md directly under root, in either layout:
// <root>/<layout>/changes/<change>/<record>, never under the archive nor
// under a nested <dir>/<layout>/changes/. It reads p's text only; the
// guard also holds p's real path to it (vcs-guard, T123).
func ExploreMayWrite(p, root string) bool {
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(p))
	if err != nil {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 4 || !slices.Contains(ExploreRecords, parts[3]) {
		return false
	}
	change := parts[2]
	if change == "" || change == "archive" || strings.HasPrefix(change, ".") {
		return false
	}
	changes := parts[0] + "/" + parts[1]
	for _, l := range layout.Layouts {
		if changes == l.ChangesDir() {
			return true
		}
	}
	return false
}
