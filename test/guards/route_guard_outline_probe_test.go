package guards

// T127 verifier probes (L842-L848): route-guard under delivery: change
// counts outlined slices before a PR opens, in every PR-opening form, and
// names the change that still has them.

import (
	"regexp"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

const probeOutline = "## Intent\nDelivers: X is 2\n\n## Slice 1: a\nDelivers: X is 2\n- [x] 1.1 a\n\n## Slice 2: b\nDelivers: X is shown\n"
const probeDone = "## Intent\nDelivers: X is 2\n\n## Slice 1: a\nDelivers: X is 2\n- [x] 1.1 a\n\n## Slice 2: b\nDelivers: X is shown\n- [x] 2.1 b\n"

func probeRG(t *testing.T, config string, tasks map[string]string) rgEnv {
	t.Helper()
	r := newRG(t, config)
	files := map[string]string{"src/app.py": "X = 2\n", "tests/test_app.py": "def test_x(): pass\n"}
	for c, src := range tasks {
		files["openspec/changes/"+c+"/tasks.md"] = src
	}
	support.Write(t, r.p, files)
	for c := range tasks {
		redGreen(t, r.p, "openspec/changes/"+c+"/execution-record.json")
	}
	return r
}

var probePRForms = []string{
	"gh pr create --title x --body y",
	"gh pr new --fill",
	"gh -R o/r pr create --fill",
	"gh --repo o/r pr create --fill",
	"gh pr create --repo o/r --fill",
	"true && gh pr create --fill",
	"X=gh; $X pr create --fill",
	"bash -c 'gh pr create --fill'",
}

func TestProbeT127RouteGuardPRForms(t *testing.T) {
	t.Parallel()
	want := "route-guard: delivery: change opens one PR per change, after its last slice: c1 has 1 outlined slice(s) with no tasks yet (Slice 2: b). Task and commit each slice on the branch and open the PR when every slice is done."
	out := probeRG(t, "delivery: change\n", map[string]string{"c1": probeOutline})
	done := probeRG(t, "delivery: change\n", map[string]string{"c1": probeDone})
	perSlice := probeRG(t, "", map[string]string{"c1": probeOutline})
	for _, c := range probePRForms {
		for _, top := range []bool{true, false} {
			res := out.call(t, "Bash", cmd(c), top)
			code(t, res, 2, c)
			matches(t, res.Stderr, "^"+regexp.QuoteMeta(want)+`\n?$`, c)
			code(t, done.call(t, "Bash", cmd(c), top), 0, c+" (every slice done)")
			code(t, perSlice.call(t, "Bash", cmd(c), top), 0, c+" (no delivery: change)")
		}
	}
	for _, c := range []string{"gh pr create --help", "gh pr view 1", "gh pr list", "gh pr merge 1 --rebase --delete-branch", "git status"} {
		code(t, out.call(t, "Bash", cmd(c), true), 0, c+" opens no PR")
	}
}

func TestProbeT127RouteGuardNamesTheChange(t *testing.T) {
	t.Parallel()
	four := "## Slice 1: a\nDelivers: A\n- [x] 1.1 a\n\n## Slice 2: b\nDelivers: B\n\n## Slice 3: c\nDelivers: C\n\n## Slice 4: d\nDelivers: D\n\n## Slice 5: e\nDelivers: E\n"
	r := probeRG(t, "delivery: change\n", map[string]string{"c1": probeDone, "c2": four})
	res := r.call(t, "Bash", cmd("gh pr create --fill"), true)
	code(t, res, 2, "c2 outlined")
	matches(t, res.Stderr, `^route-guard: delivery: change opens one PR per change, after its last slice: c2 has 4 outlined slice\(s\) with no tasks yet \(Slice 2: b, Slice 3: c, Slice 4: d\)\. `, "names c2, 4, three slices")
}
