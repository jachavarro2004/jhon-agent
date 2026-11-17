package render

import (
	"fmt"
	"strings"
)

// pi's appended orchestrator text carries what a pi session uses (T42c):
// Claude Code's dedicated-tool lines are Claude Code's alone. Detail read
// only when its case arises lives, on both runtimes, in the contracts read
// on demand (ondemand.go, T146 FA3, the owner's L1217), which replaced
// pi's edits for "Check proportionately", the optional tools table and the
// remote operations list. Each edit names text of the rendered prompt;
// validation reports an edit whose text is gone or repeated, and a
// reference that does not resolve.

// piEdit replaces old, which the rendered pi prompt holds exactly once, with
// new; with upTo, the text from old up to the next upTo goes too. refs are
// the prose files new names, relative to prose/.
type piEdit struct {
	name, old, new, upTo string
	refs                 []string
}

// piEdits are the edits, given the prose directory the agent reads (no
// edit left names a file in it).
func piEdits(string) []piEdit {
	return []piEdit{
		{
			// Claude Code's dedicated tools; pi's tools carry their own guidance.
			name: "Shell tooling",
			old: "## Shell tooling\n\n" +
				"When installed, prefer `bat` over `cat`, `rg` over `grep`, `fd` over `find`,\n" +
				"`sd` over `sed`, and `eza` over `ls`; when one is missing, use the POSIX\n" +
				"tool. When a dedicated tool exists (Read, Grep, Glob, Edit), use it instead\n" +
				"of the shell.\n\n",
			new: "",
		},
	}
}

// piLean is text with every edit that applies applied, and why each other
// one does not.
func piLean(text, path string) (string, []string) {
	var missing []string
	for _, ed := range piEdits(path) {
		if n := strings.Count(text, ed.old); n != 1 {
			missing = append(missing, fmt.Sprintf("pi prompt edit %q: its text appears %d times, want 1", ed.name, n))
			continue
		}
		start := strings.Index(text, ed.old)
		end := start + len(ed.old)
		if ed.upTo != "" {
			n := strings.Index(text[start:], ed.upTo)
			if n < 0 {
				missing = append(missing, fmt.Sprintf("pi prompt edit %q: no %q after it", ed.name, strings.TrimSpace(ed.upTo)))
				continue
			}
			end = start + n
		}
		text = text[:start] + ed.new + text[end:]
	}
	return text, missing
}
