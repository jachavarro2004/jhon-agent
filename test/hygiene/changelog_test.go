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
var removedNames = []string{"/jhon:changes", "/jhon:commands", "/jhon:panel", "click changes"}

// restoredNames are features T174 S1 removed and T174-S5b brought back
// (L1493-L1495): the [Unreleased] ### Removed block must not name them.
var restoredNames = []string{"/jhon:usage", "alt+u", "alt+t", "alt+PgUp", "shell.keys.usage", "shell.keys.todos", "JHON_SHELL_USAGE_KEY", "JHON_SHELL_TODO_KEY"}

// unreleased is CHANGELOG.md's [Unreleased] section, split into its
// ### Removed block and everything else.
func unreleased(t *testing.T) (rest, removed string) {
	t.Helper()
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
		block := section[at+len("\n### Removed\n"):]
		if next := strings.Index(block, "\n### "); next >= 0 {
			section, removed = section[:at]+block[next:], block[:next]
		} else {
			section, removed = section[:at], block
		}
	}
	return section, removed
}

func TestUnreleasedRemovedNamesNothingThatCameBack(t *testing.T) {
	_, removed := unreleased(t)
	if removed == "" {
		t.Fatal("the [Unreleased] slice has no ### Removed block; the slicing is wrong")
	}
	for _, name := range restoredNames {
		for i, line := range strings.Split(removed, "\n") {
			if strings.Contains(line, name) {
				t.Errorf("[Unreleased] ### Removed names %q, which is back (line %d): %.120s", name, i+1, line)
			}
		}
	}
}

func TestUnreleasedNamesRemovedFeaturesOnlyUnderRemoved(t *testing.T) {
	section, _ := unreleased(t)
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
