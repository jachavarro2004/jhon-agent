// CHANGELOG.md's [Unreleased] section describes only what exists (L1485): a
// shell view, command or key the release removed is named under its
// "### Removed" heading and nowhere else in the section, so no Added, Changed
// or Fixed line reads as if it were still there. The file is read from the
// repository root.
package hygiene

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// removedNames are the removed features' names (T169, T174 S1).
var removedNames = []string{"/jhon:changes", "/jhon:usage", "/jhon:commands", "/jhon:panel", "alt+PgUp", "click changes"}

func TestUnreleasedNamesRemovedFeaturesOnlyUnderRemoved(t *testing.T) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(out)), "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	text := string(data)
	start := strings.Index(text, "## [Unreleased]")
	if start < 0 {
		t.Fatal("CHANGELOG.md has no ## [Unreleased] section")
	}
	section := text[start+len("## [Unreleased]"):]
	if end := strings.Index(section, "\n## ["); end >= 0 {
		section = section[:end]
	}
	// Drop the ### Removed block: from its heading to the next ### heading.
	if at := strings.Index(section, "\n### Removed\n"); at >= 0 {
		rest := section[at+len("\n### Removed\n"):]
		if next := strings.Index(rest, "\n### "); next >= 0 {
			section = section[:at] + rest[next:]
		} else {
			section = section[:at]
		}
	}
	if !strings.Contains(section, "### Added") {
		t.Fatal("the [Unreleased] slice lost its ### Added block; the slicing is wrong")
	}
	for _, name := range removedNames {
		for i, line := range strings.Split(section, "\n") {
			if strings.Contains(line, name) {
				t.Errorf("[Unreleased] names %q outside ### Removed (section line %d): %.120s", name, i+1, line)
			}
		}
	}
}
