package main

import (
	"encoding/json"
	"fmt"

	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// The certification metrics of T60 (L482, L483), per run:
//
//   - ceremony, plain runs only, recorded: an openspec/ or ldd/ the run
//     created, `jhon executions`, `jhon archive` or `jhon deliver` it ran, or
//     a branch it created; commits apart, outside the total (L488). Since
//     Jhon always works under LDD (L554-L559) certify judges it no more.
//   - ldd_layout, LDD variants and Jhon plain runs (which set LDD up): the
//     layout the repo has at the end (openspec, ldd or none) and whether the
//     LDD record exists: a change's tasks.md and a test record in `jhon
//     status --json`.
//   - slices, tasks with per-message checks: the fraction of messages whose
//     check passed at that message and that no later message broke.
//   - refusals: tool calls the safety layer or a permission rule refused.

// Ceremony is what a plain run did unasked, by kind.
type Ceremony struct {
	Layouts    []string `json:"layouts"`
	Executions int      `json:"executions"`
	Archive    int      `json:"archive"`
	Deliver    int      `json:"deliver"`
	Branches   int      `json:"branches"`
	// Commits are recorded, never ceremony (L488).
	Commits int `json:"commits"`
}

// Total is every kind of ceremony summed, commits left out: 0 is a clean
// plain run.
func (c Ceremony) Total() int {
	return len(c.Layouts) + c.Executions + c.Archive + c.Deliver + c.Branches
}

// add sums d into c, its layouts kept once each.
func (c *Ceremony) add(d Ceremony) {
	for _, l := range d.Layouts {
		if !strings.Contains(","+strings.Join(c.Layouts, ",")+",", ","+l+",") {
			c.Layouts = append(c.Layouts, l)
		}
	}
	c.Executions, c.Archive, c.Deliver = c.Executions+d.Executions, c.Archive+d.Archive, c.Deliver+d.Deliver
	c.Branches, c.Commits = c.Branches+d.Branches, c.Commits+d.Commits
}

// MarshalJSON writes the kinds and their total.
func (c Ceremony) MarshalJSON() ([]byte, error) {
	type plain Ceremony
	if c.Layouts == nil {
		c.Layouts = []string{}
	}
	return json.Marshal(struct {
		plain
		Total int `json:"total"`
	}{plain(c), c.Total()})
}

// String is the kinds of ceremony that happened as "kind:n", "0" when none did.
func (c Ceremony) String() string {
	var parts []string
	for _, l := range c.Layouts {
		parts = append(parts, l+"/")
	}
	for _, k := range []struct {
		name string
		n    int
	}{{"executions", c.Executions}, {"archive", c.Archive}, {"deliver", c.Deliver}, {"branches", c.Branches}} {
		if k.n > 0 {
			parts = append(parts, k.name+":"+strconv.Itoa(k.n))
		}
	}
	if len(parts) == 0 {
		return "0"
	}
	return strings.Join(parts, " ")
}

// ceremonyCommand is a `jhon executions|archive|deliver` that starts a
// segment of a shell command (its start, after ;, &, |, ( or a newline,
// with VAR=value before it), so a quoted mention does not count.
var ceremonyCommand = regexp.MustCompile(`(?:^|[;&|(\n])\s*(?:\w+=\S*\s+)*jhon\s+(executions|archive|deliver)\b`)

// fromCeremony fills the repository side of a plain run's ceremony: the
// layouts it created, its new branches, and its commits since start
// (recorded, no ceremony). The
// commands come from the transcript (fromTranscript).
func (m *Metrics) fromCeremony(t Task, variant, repo, start string, env []string) {
	if variant != "plain" {
		return
	}
	c := &Ceremony{Layouts: []string{}}
	for _, d := range []string{"ldd", "openspec"} {
		if fileExists(filepath.Join(repo, d)) {
			c.Layouts = append(c.Layouts, d)
		}
	}
	gitCount := func(args ...string) int {
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = repo, env
		out, err := cmd.Output()
		if err != nil {
			m.Problems = append(m.Problems, fmt.Sprintf("ceremony: git %s: %v", strings.Join(args, " "), err))
			return 0
		}
		s := strings.TrimSpace(string(out))
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
		if s == "" {
			return 0
		}
		return len(strings.Split(s, "\n"))
	}
	c.Branches = max(gitCount("for-each-ref", "--format=%(refname)", "refs/heads")-1, 0)
	c.Commits = gitCount("rev-list", "--count", "--all", "^"+start)
	m.Ceremony = c
}

// LddLayout is an LDD run's layout at its end and its record. The record
// follows the change's route (owner, after L493: "Sí, que siga la ruta"):
// the test record, plus a change folder with tasks.md only when the route
// requires one. RouteRequiresFolder is `jhon status --json` needs.tasks
// (ldd.NeedsTasks: a change over the advisory size or at a high tier needs
// tasks.md, so a folder; a small change has none, L254): "true", "false",
// or "unknown" when status names no change after the run. Record is nil
// when it cannot be told: a test record, no tasks.md and an unknown route.
type LddLayout struct {
	Layout              string `json:"layout"`
	TasksMD             bool   `json:"tasks_md"`
	TestRecord          bool   `json:"test_record"`
	RouteRequiresFolder string `json:"route_requires_folder"`
	Record              *bool  `json:"record"`
}

// lddLayoutOf is repo's layout and record, status its `jhon status --json`.
func lddLayoutOf(repo string, status []byte) *LddLayout {
	l := &LddLayout{Layout: repoLayout(repo), RouteRequiresFolder: "unknown"}
	if l.Layout != "none" {
		for _, p := range []string{"*", filepath.Join("archive", "*")} {
			if found, _ := filepath.Glob(filepath.Join(repo, l.Layout, "changes", p, "tasks.md")); len(found) > 0 {
				l.TasksMD = true
			}
		}
	}
	var s map[string]any
	if json.Unmarshal(status, &s) == nil {
		l.TestRecord = at(s, "tests") != nil
		if needs, ok := at(s, "needs", "tasks").(bool); ok {
			l.RouteRequiresFolder = strconv.FormatBool(needs)
		}
	}
	switch {
	case !l.TestRecord:
		l.Record = f2b(false)
	case l.TasksMD || l.RouteRequiresFolder == "false":
		l.Record = f2b(true)
	case l.RouteRequiresFolder == "true":
		l.Record = f2b(false)
	}
	return l
}

func f2b(v bool) *bool { return &v }

// slicesHeld is the fraction of a run's n message checks that passed after
// their own message and failed after no later one; nil when n is 0. A
// check that never ran (the run stopped before its message) did not hold.
func slicesHeld(checks []MessageCheck, n int) *float64 {
	if n == 0 {
		return nil
	}
	passed, broke := map[int]bool{}, map[int]bool{}
	for _, c := range checks {
		switch {
		case c.After == c.Check && c.Passed:
			passed[c.Check] = true
		case c.After > c.Check && !c.Passed:
			broke[c.Check] = true
		}
	}
	held := 0
	for k := range passed {
		if !broke[k] {
			held++
		}
	}
	return f(float64(held) / float64(n))
}

// refusalText is a refused tool call's result: a hook's denial, a
// permission rule's, or the Jhon safety guard's (audit's refusal.go); a
// permission the headless runtime never granted; pi's guard refusal, its
// reason led by the guard's name (`safety-guard: unknown: …`,
// `route-guard: refused: …`) or the guard's failure (T105).
var refusalText = regexp.MustCompile(`^PreToolUse:\w+ hook error:|^Permission to use \S+ (?s:.)* has been denied|Jhon safety guard|` +
	`^Claude requested permissions to use \S+, but you haven't granted it yet|^[a-z]+(?:-[a-z]+)*-guard(?::| failed \()`)

// messageCheckCount is how many of t's messages have a check.
func messageCheckCount(t Task) int {
	n := 0
	for _, c := range t.MessageChecks {
		if c != "" {
			n++
		}
	}
	return n
}

// lddStatus is `jhon status --json [change]` in repo with the bench's jhon.
func lddStatus(home, repo string, env []string, change ...string) []byte {
	c := exec.Command(benchJhon(home), append([]string{"status", "--json"}, change...)...)
	c.Dir, c.Env = repo, env
	out, _ := c.Output()
	return out
}
