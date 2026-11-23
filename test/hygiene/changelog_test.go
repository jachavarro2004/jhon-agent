// CHANGELOG.md's newest section describes only what exists (L1485): a shell
// view, command or key the release removed is named under its "### Removed"
// heading and nowhere else in the section, so no Added, Changed or Fixed line
// reads as if it were still there. The newest section is [Unreleased] while it
// has entries; right after a release, when [Unreleased] is empty, it is the
// first `## [x.y.z]` section below it (the release just cut, [0.4.0] now).
// The file is read from the repository root.
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
// (L1493-L1495): the newest section's ### Removed block must not name them.
var restoredNames = []string{"/jhon:usage", "alt+u", "alt+t", "alt+PgUp", "shell.keys.usage", "shell.keys.todos", "JHON_SHELL_USAGE_KEY", "JHON_SHELL_TODO_KEY"}

// unreleased is CHANGELOG.md's newest section with content, split into its
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
	return newestSection(t, string(data))
}

// newestSection slices text: [Unreleased] when it has a non-blank line, else
// the first `## [` section after it; then splits off the ### Removed block.
func newestSection(t *testing.T, text string) (rest, removed string) {
	t.Helper()
	start := strings.Index(text, "## [Unreleased]")
	if start < 0 {
		t.Fatal("CHANGELOG.md has no ## [Unreleased] section")
	}
	section := text[start+len("## [Unreleased]"):]
	if end := strings.Index(section, "\n## ["); end >= 0 {
		if strings.TrimSpace(section[:end]) == "" {
			// Empty right after a release: the release just cut is the newest.
			section = section[end+len("\n## ["):]
			if eol := strings.Index(section, "\n"); eol >= 0 {
				section = section[eol:]
			}
			if next := strings.Index(section, "\n## ["); next >= 0 {
				section = section[:next]
			}
		} else {
			section = section[:end]
		}
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

// restoredUnderRemoved lists each line of removed that names a restored feature.
func restoredUnderRemoved(removed string) []string {
	var bad []string
	for _, name := range restoredNames {
		for _, line := range strings.Split(removed, "\n") {
			if strings.Contains(line, name) {
				bad = append(bad, name+": "+line)
			}
		}
	}
	return bad
}

// removedOutsideRemoved lists each line of section that names a removed feature.
func removedOutsideRemoved(section string) []string {
	var bad []string
	for _, name := range removedNames {
		for _, line := range strings.Split(section, "\n") {
			if strings.Contains(line, name) {
				bad = append(bad, name+": "+line)
			}
		}
	}
	return bad
}

// A newest section with no ### Removed block (an [Unreleased] that only
// fixes, T217) names nothing there; TestNewestSectionSlicing pins the
// slicing itself.
func TestUnreleasedRemovedNamesNothingThatCameBack(t *testing.T) {
	section, removed := unreleased(t)
	if !strings.Contains(section+removed, "\n### ") {
		t.Fatal("the newest section has no ### block; the slicing is wrong")
	}
	if strings.Contains(section, "### Removed") && strings.TrimSpace(removed) == "" {
		t.Fatal("the newest section has a ### Removed heading but its block was not sliced")
	}
	for _, bad := range restoredUnderRemoved(removed) {
		t.Errorf("the newest section's ### Removed names a feature that is back: %.160s", bad)
	}
}

func TestUnreleasedNamesRemovedFeaturesOnlyUnderRemoved(t *testing.T) {
	section, _ := unreleased(t)
	if !strings.Contains(section, "\n### ") {
		t.Fatal("the newest section has no ### block outside ### Removed; the slicing is wrong")
	}
	for _, bad := range removedOutsideRemoved(section) {
		t.Errorf("the newest section names a removed feature outside ### Removed: %.160s", bad)
	}
}

// The slicing picks the newest section with content, and the checks still
// catch a name in the wrong block there.
func TestNewestSectionSlicing(t *testing.T) {
	const head = "# Changelog\n\n"
	const older = "## [0.3.7] - 2026-11-18\n\n### Added\n\n- `/jhon:panel` old.\n\n### Removed\n\n- `/jhon:usage` old.\n"
	for _, c := range []struct {
		name                     string
		text                     string
		outside, restored, added bool
	}{
		{
			name:  "empty unreleased reads the release just cut",
			text:  head + "## [Unreleased]\n\n## [0.4.0] - 2026-11-23\n\nLead.\n\n### Added\n\n- New.\n\n### Removed\n\n- `/jhon:changes` is gone.\n\n" + older,
			added: true,
		},
		{
			name:    "a removed name outside ### Removed of the release just cut",
			text:    head + "## [Unreleased]\n\n## [0.4.0] - 2026-11-23\n\n### Added\n\n- `/jhon:changes` opens.\n\n### Removed\n\n- `/jhon:commands` is gone.\n\n" + older,
			outside: true, added: true,
		},
		{
			name:     "a restored name under ### Removed of the release just cut",
			text:     head + "## [Unreleased]\n\n## [0.4.0] - 2026-11-23\n\n### Added\n\n- New.\n\n### Removed\n\n- `alt+u` is gone.\n\n" + older,
			restored: true, added: true,
		},
		{
			name:    "unreleased with entries is read, not the release below",
			text:    head + "## [Unreleased]\n\n### Added\n\n- `/jhon:panel` opens.\n\n### Removed\n\n- x.\n\n## [0.4.0] - 2026-11-23\n\n### Added\n\n- New.\n\n### Removed\n\n- `alt+u` is gone.\n",
			outside: true, added: true,
		},
		{
			name: "only an empty unreleased",
			text: head + "## [Unreleased]\n",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			section, removed := newestSection(t, c.text)
			if got := len(removedOutsideRemoved(section)) > 0; got != c.outside {
				t.Errorf("removed name outside ### Removed found = %v, want %v (section %q)", got, c.outside, section)
			}
			if got := len(restoredUnderRemoved(removed)) > 0; got != c.restored {
				t.Errorf("restored name under ### Removed found = %v, want %v (removed %q)", got, c.restored, removed)
			}
			if got := strings.Contains(section, "### Added"); got != c.added {
				t.Errorf("### Added in the section = %v, want %v (section %q)", got, c.added, section)
			}
		})
	}
}
