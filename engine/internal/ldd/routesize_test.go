package ldd

import (
	"path/filepath"
	"strings"
	"testing"
)

// routeRepo is a repository on a feature branch off main whose change adds
// files, each with lines lines.
func routeRepo(t *testing.T, lines int, files ...string) string {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	gitT(t, root, "init", "-q")
	write(t, filepath.Join(root, "src/base.py"), "X = 1\n")
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "base")
	gitT(t, root, "checkout", "-q", "-b", "feature")
	for _, f := range files {
		write(t, filepath.Join(root, f), strings.Repeat("x = 1\n", lines))
	}
	return root
}

// Only code counts toward the advisory size (L12): lines added to tests
// and documents, what `jhon risk` reads as passive, do not. Size alone never
// puts a change on the verified route (S25): a change over the advisory
// size stays standard, and its route says so as a note.
func TestRouteSizeCountsCodeOnly(t *testing.T) {
	jhonRoot(t)
	for _, c := range []struct {
		what  string
		files []string
		level string
		lines int
	}{
		{"docs only", []string{"docs/guide.md"}, RouteStandard, 0},
		{"tests only", []string{"tests/test_a.py"}, RouteStandard, 0},
		{"code with its test", []string{"src/app.py", "tests/test_app.py"}, RouteStandard, 500},
	} {
		root := routeRepo(t, 500, c.files...)
		r, err := RouteOf(root)
		if err != nil {
			t.Fatalf("%s: %v", c.what, err)
		}
		if r.Level != c.level || r.Lines != c.lines {
			t.Errorf("%s: level %s, lines %d, want %s, %d: %v", c.what, r.Level, r.Lines, c.level, c.lines, r.Reasons)
		}
	}
	r, _ := RouteOf(routeRepo(t, 500, "src/app.py", "tests/test_app.py"))
	if got := strings.Join(r.Reasons, "; "); got != "risk medium is under the verified tier (high); 500 added lines of code are over the advisory size (400): size alone asks for no verify" {
		t.Errorf("the size's note: %q", got)
	}
	r, _ = RouteOf(routeRepo(t, 500, "docs/guide.md"))
	if got := strings.Join(r.Reasons, "; "); got != "risk passive is under the verified tier (high)" {
		t.Errorf("the standard reason: %q", got)
	}
}

// route_verified_lines is still read, as the advisory size, so a config
// that sets it keeps working; it no longer forces verify (S25).
func TestRouteVerifiedLinesIsAdvisory(t *testing.T) {
	jhonRoot(t)
	root := routeRepo(t, 150, "src/app.py", "tests/test_app.py")
	write(t, filepath.Join(root, "openspec/config.yaml"), "route_verified_lines: 100\n")
	r, err := RouteOf(root)
	if err != nil {
		t.Fatal(err)
	}
	if r.Level != RouteStandard || r.VerifiedLines != 100 || r.Lines != 150 {
		t.Errorf("level %s, advisory %d, lines %d, want standard, 100, 150: %v", r.Level, r.VerifiedLines, r.Lines, r.Reasons)
	}
	if got := strings.Join(r.Reasons, "; "); got != "risk medium is under the verified tier (high); 150 added lines of code are over the advisory size (100): size alone asks for no verify" {
		t.Errorf("the size's note: %q", got)
	}
}
