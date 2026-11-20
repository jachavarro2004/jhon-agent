package ldd

import (
	"slices"
	"testing"
)

// The Prism facets a diff calls for come from its risk reasons (L46).
func TestFacetsOf(t *testing.T) {
	for _, c := range []struct {
		name    string
		changed []Changed
		want    []string
	}{
		{"a migration calls for risk", []Changed{{"db/migrations/001_a.sql", "alter table a drop column b;"}}, []string{"safety"}},
		{"plain code calls for reliability", []Changed{{"src/app.py", "X = 2"}}, []string{"behavior"}},
		{"code that spawns or retries adds resilience", []Changed{{"src/run.go", "cmd := exec.Command(\"git\")"}}, []string{"failure", "behavior"}},
		{"a security path calls for risk", []Changed{{"src/auth/login.py", "x = 1"}}, []string{"safety"}},
		{"delivery calls for resilience", []Changed{{".github/workflows/ci.yml", "on: push"}}, []string{"failure"}},
		{"documents alone fall back to reliability", []Changed{{"README.md", "x"}}, []string{"behavior"}},
	} {
		got := facetsOf(c.changed, Classify(c.changed, nil))
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v (reasons %v)", c.name, got, c.want, Classify(c.changed, nil))
		}
	}
}
