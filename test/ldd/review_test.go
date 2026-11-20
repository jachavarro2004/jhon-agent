package ldd

import (
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// reviewRepo is a repository on a feature branch off main with config as
// its openspec/config.yaml, and files changed on the branch.
func reviewRepo(t *testing.T, config string, files map[string]string) string {
	t.Helper()
	root := temp(t)
	support.Write(t, root, map[string]string{"openspec/config.yaml": config, "src/base.py": "X = 1\n"})
	support.GitRepo(t, root)
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "base")
	support.Git(t, root, "checkout", "-q", "-b", "feat")
	support.Write(t, root, files)
	return root
}

// reviews is status's route.reviews, by review.
func reviews(t *testing.T, root string) map[string]map[string]any {
	t.Helper()
	route, _ := statusJSON(t, root)["route"].(map[string]any)
	list, _ := route["reviews"].([]any)
	out := map[string]map[string]any{}
	for _, r := range list {
		m, _ := r.(map[string]any)
		out[m["review"].(string)] = m
	}
	return out
}

// On a risk-high route `jhon status` reports Prism review due, by risk, until a Prism review
// record exists (S17, L9); size alone puts no change on the verified route
// (S25), so a large change owes no review.
func TestStatusPrismDueOnRiskHigh(t *testing.T) {
	t.Parallel()
	root := reviewRepo(t, "schema: spec-driven\n", map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [ ] 1.1 a\n",
		"src/migrations/0001_add.sql": "alter table a drop column b;\n", "tests/test_app.py": "def test_a(): pass\n"})
	r := reviews(t, root)
	equal(t, r["prism"], map[string]any{"review": "prism", "by": []any{"risk high"}, "done": false}, "Prism review due")
	equal(t, r["checker"]["done"], false, "verify due")
	h := jhonGit(t, root, "status")
	equal(t, h.Code, 0, h.Stderr)
	match(t, h.Stdout, `(?m)^review      Prism review due \(risk high\): pick the pertinent facets`)
	support.Write(t, root, map[string]string{"openspec/changes/c1/review-prism.md": "facets: safety, behavior\nverdict: pass\n"})
	equal(t, reviews(t, root)["prism"], map[string]any{"review": "prism", "by": []any{"risk high"}, "done": true, "record": "review-prism.md (facets: safety, behavior; verdict: pass)"}, "Prism review on record")
	match(t, jhonGit(t, root, "status").Stdout, `(?m)^review      Prism review done \(risk high\): review-prism\.md`)

	size := reviewRepo(t, "schema: spec-driven\n", map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [ ] 1.1 a\n",
		"src/big.py": strings.Repeat("x = 1\n", 450), "tests/test_big.py": "def test_a(): pass\n"})
	r = reviews(t, size)
	if len(r) != 0 {
		t.Errorf("a large change owes no review: %v", r)
	}
}

// review_required names a change or an area and the review it asks for;
// status reports the ones that apply to the selected change, by its name or
// its diff's paths, and which are missing (S11).
func TestStatusReviewRequired(t *testing.T) {
	t.Parallel()
	config := "schema: spec-driven\nreview_required:\n  - change: c1\n    review: checker\n  - area: \"src/billing/**\"\n    review: prism\n  - area: \"src/pay/**\"\n    review: checker\n"
	root := reviewRepo(t, config, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [ ] 1.1 a\n",
		"src/billing/charge.py": "X = 2\n", "tests/test_login.py": "def test_a(): pass\n"})
	r := reviews(t, root)
	equal(t, r["checker"], map[string]any{"review": "checker", "by": []any{"review_required: change c1"}, "done": false}, "verify by name; the src/pay/** area the diff does not touch applies nothing")
	equal(t, r["prism"], map[string]any{"review": "prism", "by": []any{"review_required: area src/billing/**"}, "done": false}, "Prism review by area")
	h := jhonGit(t, root, "status").Stdout
	match(t, h, "(?m)^review      checker due \\(review_required: change c1\\): launch the jhon-checker agent and record verify-report.md with `verdict: pass`$")
	match(t, h, `(?m)^review      Prism review due \(review_required: area src/billing/\*\*\)`)
	support.Write(t, root, map[string]string{"openspec/changes/c1/verify-report.md": "verdict: pass\n"})
	equal(t, reviews(t, root)["checker"]["done"], true, "a passing verify report")

	other := reviewRepo(t, config, map[string]string{"openspec/changes/c2/tasks.md": "## Slice 1: a\n- [ ] 1.1 a\n",
		"src/other.py": "X = 2\n", "tests/test_other.py": "def test_a(): pass\n"})
	if r := reviews(t, other); len(r) != 0 {
		t.Errorf("another change outside every area owes none: %v", r)
	}
	if regexp.MustCompile(`(?m)^review `).MatchString(jhonGit(t, other, "status").Stdout) {
		t.Errorf("no review line for a change that owes none")
	}
}

// An entry of review_required jhon cannot read is reported in status's
// route.reviews with its line and why, never dropped (L17).
func TestStatusReviewRequiredUnreadable(t *testing.T) {
	t.Parallel()
	root := reviewRepo(t, "schema: spec-driven\nreview_required:\n  - change: c1\n    review: 5r\n", map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [ ] 1.1 a\n",
		"src/other.py": "X = 2\n", "tests/test_other.py": "def test_a(): pass\n"})
	equal(t, reviews(t, root)["review_required"], map[string]any{"review": "review_required", "by": []any{"line 4: review: 5r"}, "done": false,
		"why": "`5r` is not a review: checker or prism"}, "the unreadable entry")
	match(t, jhonGit(t, root, "status").Stdout, "(?m)^review      review_required in openspec/config.yaml has an entry jhon cannot read \\(line 4: review: 5r\\): `5r` is not a review: checker or prism; fix it before the PR$")
}
