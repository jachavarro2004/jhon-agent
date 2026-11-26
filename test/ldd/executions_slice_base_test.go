// T253 (odd/tasks/v0-1-release.md L1721): a slice's run measures from where
// the slice started. A bare `--affected` with no passing run on record
// measures from the commit the slice in progress started from (the last
// commit that closed a slice of the change), not the change's base on the
// default branch, and says which; the change's base stays the fallback. A
// selection that a changed shared project fills with its dependents says
// so, naming the slice's base when the run measured from further back.
package ldd

import (
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// threeSlices is a change's tasks.md with three slices, those closed checked.
func threeSlices(closed int) string {
	var b strings.Builder
	for i, name := range []string{"a", "b", "c"} {
		box := " "
		if i < closed {
			box = "x"
		}
		n := string(rune('1' + i))
		b.WriteString("## Slice " + n + ": " + name + "\n\nDelivers: CA-0" + n + "\n\n- [" + box + "] " + n + ".1 " + name + "\n\n")
	}
	return b.String()
}

// slicedWorkspace is nodeWorkspace (app and tool depend on lib) on a branch
// feat whose change c1 closed slice 1, which changed lib, then slice 2,
// which changed other; slice 3 is in progress. It returns the root and the
// commit that closed slice 2.
func slicedWorkspace(t *testing.T) (root, slice2 string) {
	t.Helper()
	root = nodeWorkspace(t, map[string]string{"openspec/changes/c1/tasks.md": threeSlices(0)})
	support.Git(t, root, "checkout", "-q", "-b", "feat")
	support.Write(t, root, map[string]string{"lib/index.js": "module.exports = 3;\n", "openspec/changes/c1/tasks.md": threeSlices(1)})
	support.Git(t, root, "commit", "-qam", "slice 1")
	support.Write(t, root, map[string]string{"other/index.js": "module.exports = 4;\n", "openspec/changes/c1/tasks.md": threeSlices(2)})
	support.Git(t, root, "commit", "-qam", "slice 2")
	return root, strings.TrimSpace(support.Git(t, root, "rev-parse", "HEAD"))
}

// T253: with two slices closed, a unit's run with no passing run on record
// measures from the commit that closed slice 2: slice 1's change to the
// shared lib, which every project depending on it would run, is not this
// slice's.
func TestExecutionsBaselessAffectedMeasuresFromTheSlicesStart(t *testing.T) {
	t.Parallel()
	root, slice2 := slicedWorkspace(t)
	support.Write(t, root, map[string]string{"other/index.js": "module.exports = 5;\n"})
	r := jhonGit(t, root, "executions", "run", "c1", "--affected", "--json")
	equal(t, r.Code, 0, r.Out())
	equal(t, ran(t, root), []string{"other"}, "only what changed since the slice started")
	rec := readRecord(t, root)
	equal(t, rec["unit"], true)
	equal(t, rec["since"], slice2, "the record says the commit it measured from")
	match(t, strings.Join(notesOf(t, r.Stdout), "\n"), `(?m)^a unit's run \(--affected with no base\): no passing run of this change is on record, so what changed since the commit the slice in progress started from \(`+slice2[:12]+`, the last that closed a slice of the change\) is selected; a slice's close takes `+"`--affected <base>`"+`$`)
	noMatch(t, strings.Join(notesOf(t, r.Stdout), "\n"), `depend on`)

	human := jhonGit(t, root, "executions", "run", "c1", "--affected", "--dry-run", "--force")
	equal(t, human.Code, 0, human.Out())
	match(t, human.Stdout, `(?m)^scope  affected \(affected since the change's last passing run\), `)
}

// T253: the change's base stays the fallback: with no slice closed yet, and
// for a change on no slice at all.
func TestExecutionsBaselessAffectedFallsBackToTheChangesBase(t *testing.T) {
	t.Parallel()
	root := nodeWorkspace(t, map[string]string{"openspec/changes/c1/tasks.md": threeSlices(0)})
	support.Git(t, root, "checkout", "-q", "-b", "feat")
	support.Write(t, root, map[string]string{"lib/index.js": "module.exports = 3;\n"})
	support.Git(t, root, "commit", "-qam", "slice 1 in progress")
	r := jhonGit(t, root, "executions", "run", "c1", "--affected", "--json")
	equal(t, r.Code, 0, r.Out())
	equal(t, ran(t, root), []string{"app", "lib", "tool"}, "no slice closed: the whole change")
	equal(t, readRecord(t, root)["since"], "main")
	match(t, strings.Join(notesOf(t, r.Stdout), "\n"), `(?m)^a unit's run \(--affected with no base\): no passing run of this change is on record, so what changed since the change's base \(main\) is selected; `)

	// A slice closed on the default branch itself (no branch of its own):
	// HEAD is the change's base, and nothing is measured from further back.
	plain := nodeWorkspace(t, map[string]string{"openspec/changes/c1/tasks.md": threeSlices(1)})
	support.Write(t, plain, map[string]string{"other/index.js": "module.exports = 4;\n"})
	r = jhonGit(t, plain, "executions", "run", "c1", "--affected", "--json")
	equal(t, r.Code, 0, r.Out())
	equal(t, ran(t, plain), []string{"other"})
	equal(t, readRecord(t, plain)["since"], "HEAD")
}

// T253: a selection a changed shared project fills with its dependents
// says so, counting the projects it brought in, even from the right base; a
// run that measured from further back than the slice's start (the default
// branch) names the slice's base, and only then.
func TestExecutionsSaysWhenASharedProjectDragsItsDependentsIn(t *testing.T) {
	t.Parallel()
	root, slice2 := slicedWorkspace(t)
	r := jhonGit(t, root, "executions", "run", "c1", "--affected", "main", "--dry-run", "--json")
	equal(t, r.Code, 0, r.Out())
	notes := strings.Join(notesOf(t, r.Stdout), "\n")
	match(t, notes, `(?m)^`+regexp.QuoteMeta("lib, which changed since main, brings 2 projects into the selection only as its dependents (app, tool); a slice's run measures from the commit the slice started from: `--affected "+slice2[:12]+"`")+`$`)

	// From the slice's own base, lib changed in this slice: said, with no
	// other base to name.
	support.Write(t, root, map[string]string{"lib/index.js": "module.exports = 6;\n"})
	r = jhonGit(t, root, "executions", "run", "c1", "--affected", slice2, "--dry-run", "--json")
	equal(t, r.Code, 0, r.Out())
	notes = strings.Join(notesOf(t, r.Stdout), "\n")
	match(t, notes, `(?m)^`+regexp.QuoteMeta("lib, which changed since "+slice2+", brings 2 projects into the selection only as its dependents (app, tool)")+`$`)

	// A base past the slice's start is never told to measure from the
	// slice's start, which would select more (L1728 B2).
	support.Git(t, root, "commit", "-qam", "slice 3, part 1")
	support.Write(t, root, map[string]string{"lib/index.js": "module.exports = 7;\n"})
	r = jhonGit(t, root, "executions", "run", "c1", "--affected", "HEAD", "--dry-run", "--json")
	equal(t, r.Code, 0, r.Out())
	notes = strings.Join(notesOf(t, r.Stdout), "\n")
	match(t, notes, `(?m)^`+regexp.QuoteMeta("lib, which changed since HEAD, brings 2 projects into the selection only as its dependents (app, tool)")+`$`)
	noMatch(t, notes, `measures from the commit the slice started from`)

	// One dependent is no span.
	one := nodeWorkspace(t, map[string]string{"tool/package.json": `{"name":"tool"}`})
	support.Write(t, one, map[string]string{"lib/index.js": "module.exports = 3;\n"})
	r = jhonGit(t, one, "executions", "run", "c1", "--affected", "base", "--dry-run", "--json")
	equal(t, r.Code, 0, r.Out())
	noMatch(t, strings.Join(notesOf(t, r.Stdout), "\n"), `brings`)
}

// T253 (L1728): a unit's run whose last passing run is older than the
// slice's start selects more than the slice changed: said, with the slice's
// base as the narrower run.
func TestExecutionsBaselessAffectedSaysAPassOlderThanTheSliceIsWider(t *testing.T) {
	t.Parallel()
	root := nodeWorkspace(t, map[string]string{"openspec/changes/c1/tasks.md": threeSlices(0)})
	support.Git(t, root, "checkout", "-q", "-b", "feat")
	support.Write(t, root, map[string]string{"app/index.js": "require('lib'); // 1\n"})
	equal(t, jhonGit(t, root, "executions", "run", "c1", "--affected", "--json").Code, 0, "a pass before slice 1 closed")
	support.Write(t, root, map[string]string{"lib/index.js": "module.exports = 3;\n", "openspec/changes/c1/tasks.md": threeSlices(1)})
	support.Git(t, root, "commit", "-qam", "slice 1")
	support.Write(t, root, map[string]string{"other/index.js": "module.exports = 4;\n", "openspec/changes/c1/tasks.md": threeSlices(2)})
	support.Git(t, root, "commit", "-qam", "slice 2")
	slice2 := strings.TrimSpace(support.Git(t, root, "rev-parse", "HEAD"))
	support.Write(t, root, map[string]string{"other/index.js": "module.exports = 5;\n"})
	r := jhonGit(t, root, "executions", "run", "c1", "--affected", "--dry-run", "--json")
	equal(t, r.Code, 0, r.Out())
	match(t, strings.Join(notesOf(t, r.Stdout), "\n"), `(?m)^a unit's run \(--affected with no base\): what changed since the change's last passing run \([^)]+\) is selected, which reaches back past the commit the slice in progress started from \(`+slice2[:12]+`\): `+"`--affected "+slice2[:12]+"`"+` selects only what this slice changed; a slice's close takes `+"`--affected <base>`"+`$`)

	// A pass inside the slice is narrower: nothing to say.
	equal(t, jhonGit(t, root, "executions", "run", "c1", "--affected", "--json").Code, 0)
	support.Write(t, root, map[string]string{"other/index.js": "module.exports = 6;\n"})
	r = jhonGit(t, root, "executions", "run", "c1", "--affected", "--dry-run", "--json")
	equal(t, r.Code, 0, r.Out())
	noMatch(t, strings.Join(notesOf(t, r.Stdout), "\n"), `reaches back`)
}
