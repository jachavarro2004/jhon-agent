package ldd

import "testing"

// T268: each glob is compiled once, and matches as before.
func TestGlobRegexIsCompiledOnce(t *testing.T) {
	a, b := GlobRegex("src/**/*.go"), GlobRegex("src/**/*.go")
	if a != b {
		t.Error("GlobRegex compiled the same glob twice")
	}
	if !a.MatchString("src/x/y.go") || !a.MatchString("src/y.go") || a.MatchString("src/y.py") || GlobRegex("*.md").MatchString("a/b.md") {
		t.Errorf("GlobRegex matches differently: %s", a)
	}
}
