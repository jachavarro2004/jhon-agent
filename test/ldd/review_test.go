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

// On a risk-high route `jhon status` reports 4R due, by risk, until a 4R
// record exists (S17, L9); size alone puts no change on the verified route
// (S25), so a large change owes no review.
func TestStatusFourRDueOnRiskHigh(t *testing.T) {
	t.Parallel()
	root := reviewRepo(t, "schema: spec-driven\n", map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [ ] 1.1 a\n",
		"src/migrations/0001_add.sql": "alter table a drop column b;\n", "tests/test_app.py": "def test_a(): pass\n"})
	r := reviews(t, root)
	equal(t, r["4r"], map[string]any{"review": "4r", "by": []any{"risk high"}, "done": false}, "4R due")
	equal(t, r["verify"]["done"], false, "verify due")
	h := jhonGit(t, root, "status")
	equal(t, h.Code, 0, h.Stderr)
	match(t, h.Stdout, `(?m)^review      4R due \(risk high\): pick the pertinent lenses`)
	support.Write(t, root, map[string]string{"openspec/changes/c1/review-4r.md": "lenses: risk, reliability\nverdict: pass\n"})
	equal(t, reviews(t, root)["4r"], map[string]any{"review": "4r", "by": []any{"risk high"}, "done": true, "record": "review-4r.md (lenses: risk, reliability; verdict: pass)"}, "4R on record")
	match(t, jhonGit(t, root, "status").Stdout, `(?m)^review      4R done \(risk high\): review-4r\.md`)

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
	config := "schema: spec-driven\nreview_required:\n  - change: c1\n    review: jd\n  - area: \"src/billing/**\"\n    review: 4r\n  - area: \"src/pay/**\"\n    review: verify\n"
	root := reviewRepo(t, config, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [ ] 1.1 a\n",
		"src/billing/charge.py": "X = 2\n", "tests/test_login.py": "def test_a(): pass\n"})
	r := reviews(t, root)
	equal(t, r["jd"], map[string]any{"review": "jd", "by": []any{"review_required: change c1"}, "done": false}, "jd by name")
	equal(t, r["4r"], map[string]any{"review": "4r", "by": []any{"review_required: area src/billing/**"}, "done": false}, "4R by area")
	if _, ok := r["verify"]; ok {
		t.Errorf("an area the diff does not touch applies nothing: %v", r)
	}
	h := jhonGit(t, root, "status").Stdout
	match(t, h, `(?m)^review      Judgment Day due \(review_required: change c1\): run it and close it APPROVED under judgments/$`)
	match(t, h, `(?m)^review      4R due \(review_required: area src/billing/\*\*\)`)
	support.Write(t, root, map[string]string{"openspec/changes/c1/judgments/2026-11-03.md": "## CLOSE\n\n```json\n{\"verdict\":\"APPROVED\"}\n```\n"})
	equal(t, reviews(t, root)["jd"]["done"], true, "a Judgment Day closed APPROVED")

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
		"why": "`5r` is not a review: verify, 4r, or jd"}, "the unreadable entry")
	match(t, jhonGit(t, root, "status").Stdout, "(?m)^review      review_required in openspec/config.yaml has an entry jhon cannot read \\(line 4: review: 5r\\): `5r` is not a review: verify, 4r, or jd; fix it before the PR$")
}
