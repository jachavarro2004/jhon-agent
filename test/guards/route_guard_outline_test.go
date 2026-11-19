package guards

import (
	"regexp"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// Slices outlined first (T127, L837, L845): under `delivery: change` the PR
// opens after the change's last slice, and a slice still outlined (its
// heading and Delivers: line, no task yet) is a slice not done, as
// `jhon deliver check --action merge` says, in its words (T154: one verdict,
// ldd.SlicesLeft); the refusal names it.
func TestRouteGuardDeliveryChangeWaitsForOutlinedSlices(t *testing.T) {
	t.Parallel()
	pr := cmd("gh pr create --title x --body y")
	r := newRG(t, "delivery: change\n")
	support.Write(t, r.p, map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n",
		"openspec/changes/c1/tasks.md": "## Intent\nDelivers: X is 2\n\n## Slice 1: a\nDelivers: X is 2\n- [x] 1.1 a\n\n## Slice 2: b\nDelivers: X is shown\n\n## Slice 3: c\nDelivers: X is exported\n"})
	redGreen(t, r.p, "openspec/changes/c1/execution-record.json")

	res := r.call(t, "Bash", pr, true)
	code(t, res, 2, "every task checked, two slices still outlined")
	want := "route-guard: c1: delivery: change in openspec/config.yaml merges the change once, after its last slice; 2 slice(s) of tasks.md are outlined with no tasks yet (Slice 2: b, Slice 3: c): commit each slice on the change's branch and merge when every slice is done."
	matches(t, res.Stderr, "^"+regexp.QuoteMeta(want)+`\n?$`, "the refusal names the outlined slices")

	support.Write(t, r.p, map[string]string{"openspec/changes/c1/tasks.md": "## Intent\nDelivers: X is 2\n\n## Slice 1: a\nDelivers: X is 2\n- [x] 1.1 a\n\n## Slice 2: b\nDelivers: X is shown\n- [ ] 2.1 b\n\n## Slice 3: c\nDelivers: X is exported\n"})
	res = r.call(t, "Bash", pr, true)
	code(t, res, 2, "an unchecked task beside an outlined slice")
	matches(t, res.Stderr, `^route-guard: c1: delivery: change in openspec/config\.yaml merges the change once, after its last slice; 1 task\(s\) of tasks\.md are unchecked \(2\.1 b\): .*; delivery: change .* 1 slice\(s\) of tasks\.md are outlined with no tasks yet \(Slice 3: c\)`, "the unchecked task first, then the outlined slice, as deliver check lists them")

	support.Write(t, r.p, map[string]string{"openspec/changes/c1/tasks.md": "## Intent\nDelivers: X is 2\n\n## Slice 1: a\nDelivers: X is 2\n- [x] 1.1 a\n\n## Slice 2: b\nDelivers: X is shown\n- [x] 2.1 b\n\n## Slice 3: c\nDelivers: X is exported\n- [x] 3.1 c\n"})
	res = r.call(t, "Bash", pr, true)
	code(t, res, 0, "every slice tasked and done")
	if res.Stderr != "" {
		t.Errorf("the PR opens with nothing to say: %q", res.Stderr)
	}

	// Without delivery: change, a PR per slice: an outlined slice left is
	// the next PR's work, never a refusal of this one.
	slice := newRG(t, "")
	support.Write(t, slice.p, map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n",
		"openspec/changes/c1/tasks.md": "## Intent\nDelivers: X is 2\n\n## Slice 1: a\nDelivers: X is 2\n- [x] 1.1 a\n\n## Slice 2: b\nDelivers: X is shown\n"})
	redGreen(t, slice.p, "openspec/changes/c1/execution-record.json")
	code(t, slice.call(t, "Bash", pr, true), 0, "delivery per slice ignores the outline")
}
