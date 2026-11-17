package ldd

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// normalized matches a scenario's name in a test's text: lowered, every run
// of characters that are neither letters nor digits one space, padded. The
// regular expression is the rule as first written; normalized keeps it.
func TestNormalizedLowersAndCollapsesEveryNonWordRun(t *testing.T) {
	rule := regexp.MustCompile(`[^\p{L}\p{N}]+`)
	want := func(s string) string {
		return " " + strings.TrimSpace(rule.ReplaceAllString(strings.ToLower(s), " ")) + " "
	}
	for _, s := range []string{
		"", " ", "---", "Scenario: Create a City",
		"def test_create_city__rejects_DUPLICATE_code():\n\tassert x == 1",
		"  leading and trailing  ", "Ñandú ÉLAN über İstanbul ΣΑΣ", "日本語のテスト 123", "a\u00a0b\u2003c", "x\xffy\xfe", "tab\tnew\nline\r\n", "émoji 🚀 rocket", "٣ arabic-indic digits",
	} {
		if got := normalized(s); got != want(s) {
			t.Errorf("normalized(%q) = %q, want %q", s, got, want(s))
		}
	}
}

// A scenario counts as named when its normalized name is in a test file's
// normalized text, however the files are scanned: the result is the one the
// plain scan of every file gives.
func TestScenarioCoverageMatchesThePlainScan(t *testing.T) {
	deliverEnv(t)
	spec := "## ADDED Requirements\n### Requirement: Cities\nThe system SHALL.\n" +
		"#### Scenario: Create a City\n- WHEN x\n#### Scenario: Rejects DUPLICATE code (409)\n- WHEN y\n" +
		"#### Scenario: Ñandú über ÉLAN\n- WHEN z\n#### Scenario: never named\n- WHEN w\n#### Scenario: a b\n- WHEN v\n"
	files := []string{
		"def test_create_a_city():\n    pass",
		"it('rejects duplicate-code 409', () => {})",
		"// scenario: ñandú   ÜBER élan!",
		"no match here: create city, rejects duplicate",
		"x\xffa\xfeb",
		"",
	}
	scan := func(visit func(path, src string)) {
		for i, s := range files {
			visit(string(rune('a'+i)), s)
		}
	}
	got, err := ScenarioCoverage([]string{spec}, scan)
	if err != nil {
		t.Fatal(err)
	}
	// The plain scan: every file normalized, every name looked for.
	var want []string
	for _, n := range []string{"Create a City", "Rejects DUPLICATE code (409)", "Ñandú über ÉLAN", "never named", "a b"} {
		for _, s := range files {
			if strings.Contains(normalized(s), normalized(n)) {
				want = append(want, n)
				break
			}
		}
	}
	if got.Total != 5 || got.Covered != len(want) {
		t.Fatalf("coverage %+v, the plain scan covers %q", got, want)
	}
	for _, n := range got.Uncovered {
		if slices.Contains(want, n) {
			t.Errorf("%q is named by a test, yet uncovered", n)
		}
	}
}
