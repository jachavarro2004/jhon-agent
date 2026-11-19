package render

import (
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T31 (D, L252, L254, L258, L311): the LDD prose states the proportional,
// binding record the engine applies (ldd.Needs, Route.String): start with
// `jhon status` and `jhon risk`, the needed record and the RED test before
// the first behaviour-changing edit, tasks.md and a spec delta only when
// needed, no folder for a small change, archive only with a record.
func TestLDDProseIsProportionalAndBinding(t *testing.T) {
	read := func(p string) string { return flat(support.Read(t, treePath(repoRoot, p))) }
	wants := map[string][]string{
		"contracts/ldd.md": {
			"Where the session says LDD is active, every change starts the same way",
			"Run `jhon status` and `jhon risk` in its tree.",
			"before its first behaviour-changing edit",
			"run it through `jhon executions run <change> --affected <base>` while it fails (RED)",
			"over about 400 lines of code",
			"Create it yourself, without being asked, when you see several rules or cases to cover or a design decision on the way.",
			"A spec delta only when a main spec under `openspec/specs/` changes or is added.",
			"only when the route asks for one",
			"A small change has no folder: the branch names it",
			"nothing to archive",
			"Archive only a change with a folder and its RED before GREEN on record",
			// T127 (L837): every slice outlined from the first plan, only the current one tasked.
			"A tasks.md, if any, outlines every slice first, `## Slice <n>: <name>` and `Delivers: <acceptance criteria or observable value>`",
			"Only the current slice holds tasks, written after the one before is delivered; evidence revises the outline.",
			"tasks outside a `## Slice <n>:` section, in a later slice than the current one (an outline is expected), or past 12 in a slice",
		},
		"contracts/tdd.md": {
			"TDD always.",
			"through `jhon executions run <change> --affected <base>` while it fails (RED), unit runs (`--affected`) between",
		},
		"orchestrator.md": {
			"start with `jhon status` and `jhon risk`",
			"before the first behaviour-changing edit",
			"`jhon executions run <change> --affected <base>` while it fails (RED)",
			"Unit runs between (`contracts/tdd.md`)",
			// T146 FA3 (L1217): the delivery rules are read on demand.
			"read `contracts/delivery.md`",
			// T64: the session says whether LDD is active; a record LDD cannot keep never stops the work.
			"**Where the session says LDD is active, these bind.**",
			"When LDD's tools cannot record a run, still do the task test-first (RED, then GREEN) with the project's own test command and say the record is missing; never stop with the work undone.",
		},
		"contracts/delivery.md": {
			"Create tasks.md yourself when you see several rules or cases to cover or a design decision on the way.",
			"built in slices outlined first, each tasked after the last is delivered",
		},
		"skills/ldd/references/init.md": {
			"Use the project's layout: `openspec/` when it exists, else `ldd/`.",
			"when `jhon init` reports only `.`, with no marker",
			"and the config has no override for it, run the command its tests use (`python3 -m unittest`) once; when it runs, write it as `test_command_override` for `\".\"`",
		},
		"skills/ldd/SKILL.md": {"what a change needs is in `contracts/ldd.md`, \"The record\""},
		"skills/ldd/references/tasks.md": {"Write tasks.md only when the change needs one", "Create it yourself when you see several rules or cases to cover or a design decision on the way.",
			// T126 (L797): a slice that crosses layers lists its passes.
			"A slice that crosses layers lists its passes as its tasks, one layer each", "each naming its files and its one commit; tick each",
			// T127 (L837): the outline of later slices.
			"Then outline every slice, `## Slice <n>: <name>`",
			"Only the current slice gets tasks, written once the one before is delivered; revise the outline when evidence says so.",
			"Tasks in a later slice, over 12 in a slice, or a slice without `Delivers:` get a note in `jhon status`.",
			"## Slice 2: {name} Delivers: {CA-03} ```"},
		"skills/ldd/references/apply.md":   {"`jhon executions run {change-name} --affected <base-ref>` while it fails (RED)"},
		"skills/ldd/references/archive.md": {"Archive only a change with a folder and its RED before GREEN on record"},
		"agents/worker.md":                 {"`jhon executions run <change> --affected <base>` while it fails (RED)"},
		// T76: the explore reports the standing decisions on what it covers.
		"agents/explore.md": {"for prior decisions on each entity you cover, and report each by id"},
	}
	// Each sentence that made every change carry tasks.md, a spec, a
	// proposal, or an archive.
	stale := []string{
		"one-line tasks.md", "one-line `tasks.md`", "Specs and tasks first", "tasks.md is the record",
		"intent opens `tasks.md`", "A preference about outcomes, never a gate",
		"Each change of value leaves its why, its slices, and its tasks",
		"can be the whole record", "Archive runs once, after the last slice is merged.",
		// T127 (L837): slices are outlined from the first plan.
		"there is no upfront plan", "never upfront", "Plan the next slice after this one is delivered",
		"each planned after the last is delivered", "names the first slice", "Tasks planned ahead",
		// T145b (L1203 N2): RED and GREEN follow the inner loop of docs/ldd.md, "Test runs".
		"`jhon executions run <change>` while it fails", "`jhon executions run {change-name}` while it fails",
		"focused commands stay for iterating",
	}
	for file, phrases := range wants {
		body := read(file)
		for _, p := range phrases {
			if !strings.Contains(body, p) {
				t.Errorf("%s does not say %q", file, p)
			}
		}
	}
	for _, file := range []string{"contracts/ldd.md", "contracts/tdd.md", "contracts/routing.md", "orchestrator.md",
		"skills/ldd/SKILL.md", "skills/ldd/references/tasks.md", "skills/ldd/references/apply.md",
		"skills/ldd/references/archive.md", "skills/ldd/references/propose.md", "agents/worker.md"} {
		body := read(file)
		for _, s := range stale {
			if strings.Contains(body, s) {
				t.Errorf("%s still says %q", file, s)
			}
		}
	}
	// One table (T154): delivery.md's tier table; ldd.md points to it.
	if ldd := read("contracts/ldd.md"); strings.Contains(ldd, "| Route |") || !strings.Contains(ldd, "is the one table, `contracts/delivery.md`'s") {
		t.Error("contracts/ldd.md keeps its own route table instead of pointing to delivery.md's")
	}
}
