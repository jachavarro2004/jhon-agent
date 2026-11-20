package ldd

import (
	"path/filepath"
	"slices"
	"testing"
)

// A Prism record is review-prism.md naming at least one facet, only the four, and
// `verdict: pass` (L9, L15); a Judgment Day record is none (T172).
func TestPrismRecord(t *testing.T) {
	for src, want := range map[string]bool{
		"# Prism review\n\nfacets: safety\nverdict: pass\n":  true,
		"facets: safety, behavior, clarity\nverdict: PASS\n": true,
		"facets: [resilience, risk]\n**Verdict**: pass\n":    true,
		"facets: safety\nverdict: fail\n":                    false,
		"verdict: pass\n":                                    false,
		"facets:\nverdict: pass\n":                           false,
		"facets: safety, speed\nverdict: pass\n":             false,
		"facets: safety\n":                                   false,
		"facets: safety behavior\nverdict: pass\n":           true,
		"- facets: [risk]\n- verdict: pass\n":                true,
	} {
		root, _ := filepath.EvalSymlinks(t.TempDir())
		write(t, filepath.Join(root, "openspec/changes/c1/review-prism.md"), src)
		if got := PrismDone(root, "c1") != ""; got != want {
			t.Errorf("PrismDone(%q) = %t, want %t", src, got, want)
		}
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	if got := PrismDone(root, "c1"); got != "" {
		t.Errorf("no record: %q", got)
	}
	write(t, filepath.Join(root, "openspec/changes/c1/review-prism.md"), "facets: safety\nverdict: pass\n")
	if got := PrismDone(root, "c1"); got != "review-prism.md (facets: safety; verdict: pass)" {
		t.Errorf("the record: %q", got)
	}
	jd, _ := filepath.EvalSymlinks(t.TempDir())
	write(t, filepath.Join(jd, "openspec/changes/c1/judgments/2026-11-03.md"), closed)
	if got := PrismDone(jd, "c1"); got != "" {
		t.Errorf("a Judgment Day record is no Prism review: %q", got)
	}
}

// review_required in openspec/config.yaml names a change or an area (a
// path glob) and the reviews it requires.
func TestRequiredReviews(t *testing.T) {
	config := "schema: spec-driven\nreview_required:\n  - change: c1\n    review: verify\n  - area: \"src/auth/**\"\n    review: [verify, 4r]\n  - area: src/pay\n    review: 4r\n  - change: c2\n    review: nonsense\nother: x\n"
	got := RequiredReviews(config)
	want := []Required{{Change: "c1", Reviews: []string{"checker"}}, {Area: "src/auth/**", Reviews: []string{"checker", "prism"}}, {Area: "src/pay", Reviews: []string{"prism"}}}
	if len(got) != len(want) {
		t.Fatalf("RequiredReviews = %#v", got)
	}
	for i := range want {
		if got[i].Change != want[i].Change || got[i].Area != want[i].Area || !slices.Equal(got[i].Reviews, want[i].Reviews) {
			t.Errorf("entry %d = %#v, want %#v", i, got[i], want[i])
		}
	}
	for _, c := range []struct {
		area, path string
		want       bool
	}{
		{"src/auth/**", "src/auth/login.py", true},
		{"src/auth/**", "src/auth/deep/x.py", true},
		{"src/auth/**", "src/authz/x.py", false},
		{"src/*.py", "src/a.py", true},
		{"src/*.py", "src/a/b.py", false},
		{"src/pay", "src/pay/charge.go", true},
		{"src/pay", "src/payment.go", false},
		{"**/migrations/*.sql", "svc/db/migrations/1.sql", true},
	} {
		if got := AreaMatches(c.area, c.path); got != c.want {
			t.Errorf("AreaMatches(%q, %q) = %t, want %t", c.area, c.path, got, c.want)
		}
	}
}

// The reviews a change owes: verify on the verified route, Prism review when its risk
// reaches the verified tier (not by size alone), and what review_required
// names for it by its name or its diff's paths; each done or due.
func TestReviewsOf(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	kinds := func(rs []Review) (out []string) {
		for _, r := range rs {
			out = append(out, r.Review)
		}
		return out
	}
	high := Route{Level: RouteVerified, Tier: High, VerifiedTier: High}
	got := ReviewsOf(root, []string{"c1"}, high)
	if !slices.Equal(kinds(got), []string{"checker", "prism"}) || got[1].Done || !slices.Equal(got[1].By, []string{"risk high"}) {
		t.Fatalf("risk high: %#v", got)
	}
	if line := got[1].Line(); line != "Prism review due (risk high): pick the pertinent facets (safety, failure, behavior, clarity) by what the change touches and record them in review-prism.md with `facets:` and `verdict: pass`" {
		t.Errorf("the Prism line: %q", line)
	}
	write(t, filepath.Join(root, "openspec/changes/c1/review-prism.md"), "facets: safety\nverdict: pass\n")
	if got := ReviewsOf(root, []string{"c1"}, high); !got[1].Done || got[1].Line() != "Prism review done (risk high): review-prism.md (facets: safety; verdict: pass)" {
		t.Errorf("Prism review on record: %#v", got[1])
	}
	bySize := Route{Level: RouteVerified, Tier: Medium, VerifiedTier: High}
	if got := ReviewsOf(root, []string{"c1"}, bySize); !slices.Equal(kinds(got), []string{"checker"}) {
		t.Errorf("verified by size asks checker alone: %#v", got)
	}
	standard := Route{Level: RouteStandard, Tier: Medium, VerifiedTier: High, files: []string{"src/auth/login.py"}}
	if got := ReviewsOf(root, []string{"c1"}, standard); len(got) != 0 {
		t.Errorf("a standard route without review_required owes none: %#v", got)
	}

	write(t, filepath.Join(root, "openspec/config.yaml"), "review_required:\n  - change: c1\n    review: verify\n  - area: \"src/auth/**\"\n    review: 4r\n  - area: \"src/pay/**\"\n    review: verify\n")
	got = ReviewsOf(root, []string{"c1"}, standard)
	if !slices.Equal(kinds(got), []string{"checker", "prism"}) || got[0].Done || !got[1].Done ||
		!slices.Equal(got[0].By, []string{"review_required: change c1"}) || !slices.Equal(got[1].By, []string{"review_required: area src/auth/**"}) {
		t.Fatalf("review_required on a standard route: %#v", got)
	}
	if line := got[0].Line(); line != "checker due (review_required: change c1): launch the jhon-checker agent and record verify-report.md with `verdict: pass`" {
		t.Errorf("the verify line: %q", line)
	}
	if got := ReviewsOf(root, []string{"c2"}, Route{Level: RouteStandard, Tier: Medium, VerifiedTier: High, files: []string{"src/other.py"}}); len(got) != 0 {
		t.Errorf("another change, outside every area: %#v", got)
	}
	write(t, filepath.Join(root, "openspec/changes/c1/judgments/2026-11-03.md"), closed)
	if got := ReviewsOf(root, []string{"c1"}, standard); got[0].Done {
		t.Errorf("a Judgment Day record is no verification: %#v", got)
	}
	if got := ReviewsOf(root, []string{"archive/c1"}, standard); !slices.Equal(kinds(got), []string{"checker", "prism"}) {
		t.Errorf("an archived change goes by its name: %#v", got)
	}
}

// review_required is read in the YAML forms a person writes, and an entry
// jhon cannot read is reported with its line and why, never dropped (L17).
func TestReadRequired(t *testing.T) {
	for config, want := range map[string][]Required{
		"review_required:\n- change: c1\n  review: 4r\n":                                                                 {{Change: "c1", Reviews: []string{"prism"}}},
		"review_required:\n  - change: c1 # billing\n    review: 4r  # owner\n":                                          {{Change: "c1", Reviews: []string{"prism"}}},
		"review_required:\n  - {change: c1, review: 4r}\n":                                                               {{Change: "c1", Reviews: []string{"prism"}}},
		"review_required:\n  - change: c1\n    review: VERIFY\n":                                                         {{Change: "c1", Reviews: []string{"checker"}}},
		"review_required: # owner\n    - change: 'c1'\n      review: [Verify, \"4R\"]\n":                                 {{Change: "c1", Reviews: []string{"checker", "prism"}}},
		"review_required:\n  - area: \"src/#a/**\"\n    review:\n      - verify\n      - 4r\n":                           {{Area: "src/#a/**", Reviews: []string{"checker", "prism"}}},
		"review_required:\n-\n  change: c1\n  review:\n  - 4r\n# done\nother: x\n":                                       {{Change: "c1", Reviews: []string{"prism"}}},
		"review_required:\n  - {area: /src/**, review: [verify, 4r]}\n\n  # a comment\n  - change: c2\n    review: 4r\n": {{Area: "/src/**", Reviews: []string{"checker", "prism"}}, {Change: "c2", Reviews: []string{"prism"}}},
		"review_required: [{change: c1, review: 4r}]\n":                                                                  {{Change: "c1", Reviews: []string{"prism"}}},
		"review_required: []\nother: x\n":                                                                                {},
		"schema: spec-driven\n":                                                                                          {},
	} {
		got, bad := ReadRequired(config)
		if len(bad) != 0 || len(got) != len(want) {
			t.Errorf("ReadRequired(%q) = %#v, %#v", config, got, bad)
			continue
		}
		for i := range want {
			if got[i].Change != want[i].Change || got[i].Area != want[i].Area || !slices.Equal(got[i].Reviews, want[i].Reviews) {
				t.Errorf("ReadRequired(%q) entry %d = %#v, want %#v", config, i, got[i], want[i])
			}
		}
	}
	for config, want := range map[string]Unreadable{
		"review_required:\n  - change: c1\n    review: 5r\n":            {Line: 3, Text: "review: 5r", Why: "`5r` is not a review: checker or prism"},
		"review_required:\n  - change: c1\n    review: jd\n":            {Line: 3, Text: "review: jd", Why: "`jd` is not a review: checker or prism"},
		"review_required:\n  - change: c1\n    this is not yaml\n":      {Line: 3, Text: "this is not yaml", Why: "it is not `change:`, `area:`, or `review:` of an entry"},
		"review_required:\n  - review: 4r\n":                            {Line: 2, Text: "- review: 4r", Why: "the entry names neither `change` nor `area`"},
		"review_required:\n  - change: c1\n":                            {Line: 2, Text: "- change: c1", Why: "the entry names no review"},
		"review_required:\n  - change: c2\n    review: 4r\n  garbage\n": {Line: 4, Text: "garbage", Why: "it is not an entry of the list (`- change: ...` or `- area: ...`)"},
		"review_required:\n  - {change: c1, review: 4r\n":               {Line: 2, Text: "- {change: c1, review: 4r", Why: "the flow mapping does not close"},
		"review_required: c1\n":                                         {Line: 1, Text: "review_required: c1", Why: "it is not a list of entries"},
		"review_required:\n  - change: c1\n    reviewer: 4r\n":          {Line: 3, Text: "reviewer: 4r", Why: "it is not `change:`, `area:`, or `review:` of an entry"},
	} {
		_, bad := ReadRequired(config)
		if len(bad) != 1 || bad[0] != want {
			t.Errorf("ReadRequired(%q) unreadable = %#v, want %#v", config, bad, want)
		}
	}
	if !AreaMatches("/src/**", "src/a.py") || AreaMatches("/src/**", "lib/src/a.py") {
		t.Errorf("a leading / matches from the repository root")
	}
}

// An unreadable entry is a review_required item in route.reviews, never
// done, whose line names the line and why (L17).
func TestReviewsOfUnreadable(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	write(t, filepath.Join(root, "openspec/config.yaml"), "review_required:\n  - change: c1\n    review: 5r\n")
	got := ReviewsOf(root, []string{"c1"}, Route{Level: RouteStandard, Tier: Medium, VerifiedTier: High})
	if len(got) != 1 || got[0].Review != ReviewUnreadable || got[0].Done {
		t.Fatalf("an unreadable entry: %#v", got)
	}
	if line := got[0].Line(); line != "review_required in openspec/config.yaml has an entry jhon cannot read (line 3: review: 5r): `5r` is not a review: checker or prism; fix it before the PR" {
		t.Errorf("the line: %q", line)
	}
}

// A flow mapping or list reads only between its own brackets: one missing
// its opening bracket is no mapping or list (it fails closed).
func TestFlowFormsNeedBothBrackets(t *testing.T) {
	if _, ok := flowMapping("change: c1, review: 4r}", 1, "x"); ok {
		t.Error("a mapping without its opening brace was read")
	}
	if _, ok := flowList("verify, 4r]", 1, "x"); ok {
		t.Error("a list without its opening bracket was read")
	}
	if out, bad := flowEntries("{change: c1, review: 4r}]", 1, "x", nil); out != nil || len(bad) != 1 {
		t.Errorf("entries without their opening bracket: %+v, %+v", out, bad)
	}
	if _, ok := flowMapping("{change: c1, review: 4r}", 1, "x"); !ok {
		t.Error("a whole flow mapping is read")
	}
}
