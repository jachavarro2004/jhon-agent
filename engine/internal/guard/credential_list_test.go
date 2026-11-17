package guard

import (
	"slices"
	"testing"
)

func TestListElementsSplitsAtTheTopLevel(t *testing.T) {
	got, ok := listElements(`pwd; echo 'a;b' && cat x 2>&1 || ls | head`)
	want := []string{"pwd", "echo 'a;b'", "cat x 2>&1", "ls | head"}
	if !ok || !slices.Equal(got, want) {
		t.Errorf("listElements = %q, %v; want %q", got, ok, want)
	}
	for _, l := range []string{"a & b", "a;", "a\nb", "; a", "a 'b"} {
		if _, ok := listElements(l); ok {
			t.Errorf("listElements(%q) is read", l)
		}
	}
	for _, e := range []string{"pwd", "git status --short", "git diff --stat", "ls -R | grep -v '^\\.git' | head -50", "ls tests 2>/dev/null"} {
		if !readOnlyElement(e) {
			t.Errorf("readOnlyElement(%q) = false", e)
		}
	}
}
