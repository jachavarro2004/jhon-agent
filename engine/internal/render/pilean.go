package render

import (
	"fmt"
	"path/filepath"
	"strings"
)

// pi's appended orchestrator text carries what a pi session uses (T42c):
// detail read only when its case arises sits behind a line naming the file
// that holds it, and a rule the appended text states twice is stated once.
// Claude Code's definition is rendered from the same prose, untouched. Each
// edit names text of the rendered prompt; validation reports an edit whose
// text is gone or repeated, and a reference that does not resolve.

// piEdit replaces old, which the rendered pi prompt holds exactly once, with
// new; with upTo, the text from old up to the next upTo goes too. refs are
// the prose files new names, relative to prose/.
type piEdit struct {
	name, old, new, upTo string
	refs                 []string
}

// piEdits are the edits, with path the prose directory the agent reads.
func piEdits(path string) []piEdit {
	at := func(rel string) string { return "`" + filepath.Join(path, rel) + "`" }
	return []piEdit{
		{
			name: "Check proportionately",
			old: "- **Check proportionately.** Iterate with a unit's run,\n" +
				"  `jhon executions run <change> --affected`; before delivery, the\n" +
				"  affected tests: `jhon executions run <change> --affected\n" +
				"  <base>` narrows each project to the tests the change reaches, a change\n" +
				"  of shared behavior included. A suite the PR's CI runs whole is never\n" +
				"  run whole here too: its green checks test the delivery. When the run\n" +
				"  says a project's selection is wide, only the change's own test modules\n" +
				"  run here; merge only after the PR's CI is green. Wait for that CI as\n" +
				"  one background job, `gh run watch <run-id> --exit-status`, whose exit is\n" +
				"  the notice; never a `sleep` loop that polls it. So before its commit,\n" +
				"  a slice's tests run as `jhon executions run <change> --affected <base>`\n" +
				"  from the worktree that holds the change (each lane's from its own),\n" +
				"  never as a loose `pytest` or `npm test`: only its record lets\n" +
				"  `jhon deliver check` deliver. A whole suite run by hand gets a note,\n" +
				"  never a refusal. A command that did not pass keeps its failed tests\n" +
				"  (`failed_tests`) and the end of its output (`output_tail`) in the\n" +
				"  record: read them there. For a wide or long run, tell the owner the\n" +
				"  `--dry-run` estimate, and when it should end, as you start the run,\n" +
				"  never a guess from the files changed. A suite longer than a few minutes\n" +
				"  runs once, recorded (`jhon executions run`), in the background; never\n" +
				"  pipe it through `tail`, `head`, or `grep` (the pipe hides its exit code\n" +
				"  and failures): read its summary and record.\n",
			new: "- **Check proportionately.** Iterate with a unit's run,\n" +
				"  `jhon executions run <change> --affected`; before delivery, a\n" +
				"  slice's tests run as `jhon executions run <change> --affected <base>`\n" +
				"  from the worktree that holds the change, never as a loose `pytest` or\n" +
				"  `npm test`: only its record lets `jhon deliver check` deliver. Before a\n" +
				"  delivery run, read \"Check proportionately\" in " + at("orchestrator.md") + "\n" +
				"  (a wide selection, the PR's CI and waiting on it, a failed run's record,\n" +
				"  a long run's estimate). A suite longer than a few minutes runs once,\n" +
				"  recorded (`jhon executions run`), in the background; never pipe it\n" +
				"  through `tail`, `head`, or `grep` (the pipe hides its exit code and\n" +
				"  failures): read its summary and record.\n",
			refs: []string{"orchestrator.md"},
		},
		{
			name: "Optional tools table",
			old: "| Tool | Pays when | Where |\n" +
				"|---|---|---|\n" +
				"| LDD record | always, for a change that alters behaviour, only what it needs; built in slices outlined first, each tasked after the last is delivered, and delivered alone or, with `delivery: change` in `openspec/config.yaml`, committed on the change's branch and merged once with rebase after the last | `contracts/ldd.md`, \"Slices\" |\n" +
				"| 4R review | automatic on a change whose risk reaches the verified tier, with only the lenses it touches (security, credentials, data, contracts: `risk`; failures, retries, concurrency, processes: `resilience`; logic the tests must pin: `reliability`; public API, messages, docs: `readability`); otherwise when the owner asks or `review_required` names it | `contracts/review-4r.md` |\n" +
				"| Judgment Day | only when the owner asks, or `review_required` names it: a defect hunt or a design decision benefits from two blind judges who deliberate | `contracts/judgment.md` |\n",
			new: "Before running one, read its contract: the LDD record's slices, " + at("contracts/ldd.md") +
				" (\"Slices\"); a 4R and its lenses, " + at("contracts/review-4r.md") +
				"; Judgment Day, " + at("contracts/judgment.md") + ".\n",
			refs: []string{"contracts/ldd.md", "contracts/review-4r.md", "contracts/judgment.md"},
		},
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
		{
			// The opening rule stays; what asks and what passes is read when a
			// remote operation arises (and the guard asks regardless).
			name: "Remote operations",
			old:  "ask and stay local, without probing the destination.\n\n- Ambient SSH agents",
			new: "ask and stay local, without probing the destination. Before any remote\n" +
				"access, push, `gh` write, publish, or deploy, read " + at("contracts/remote-auth.md") + "\n" +
				"(what asks, what passes, grants and modes).",
			upTo: "\n\n## Runtime boundaries\n",
			refs: []string{"contracts/remote-auth.md"},
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
