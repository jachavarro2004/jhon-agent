package ldd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Status suggests verify only when it is due (S28): on the verified route
// (risk high) or when review_required names it. A standard-route change
// whose tasks are done, its RED before GREEN and a passing light verify on
// record (T31, T31b), is suggested archive, never verify.
func TestStatusSuggestsVerifyOnlyWhenDue(t *testing.T) {
	jhonRoot(t)
	done := "## Intent\nDelivers: the totals\n\n- [x] 1.1 totals\n"
	for _, c := range []struct {
		what, file, config, next string
	}{
		{"standard route", "src/app.py", "delivery: change\n", "archive"},
		{"verified route", "src/auth/login.py", "delivery: change\n", "verify"},
		{"review_required names verify", "src/app.py", "review_required:\n  - change: c\n    review: [verify]\n", "verify"},
	} {
		root := configuredRepo(t, c.config, c.file, "tests/test_app.py")
		write(t, filepath.Join(root, "openspec/changes/c/tasks.md"), done)
		lightVerified(t, filepath.Join(root, "openspec/changes/c/execution-record.json"))
		st := statusOf(t, root, s("c"))
		if st.Route == nil {
			t.Fatalf("%s: no route", c.what)
		}
		if st.Next != c.next {
			t.Errorf("%s (%s): next %s, want %s", c.what, st.Route.Level, st.Next, c.next)
		}
		suggestion := "next        verify  (suggestion)"
		if got := strings.Contains(Human(st), suggestion); got != (c.next == "verify") {
			t.Errorf("%s: %q in the view is %v:\n%s", c.what, suggestion, got, Human(st))
		}
		// L46: a route that owes no verify does not list its report.
		listed := slices.ContainsFunc(shownArtifacts(st), func(a ArtifactStatus) bool { return a.Artifact == "verify-report.md" })
		if listed != (c.next == "verify") {
			t.Errorf("%s: verify-report.md listed is %v: %v", c.what, listed, st.Artifacts)
		}
		if strings.Contains(Human(st), "verify-report.md") != (c.next == "verify") {
			t.Errorf("%s: the view names verify-report.md wrongly:\n%s", c.what, Human(st))
		}
	}
	// A report on record is listed even where verify is not due.
	root := configuredRepo(t, "delivery: change\n", "src/app.py", "tests/test_app.py")
	write(t, filepath.Join(root, "openspec/changes/c/tasks.md"), done)
	write(t, filepath.Join(root, "openspec/changes/c/verify-report.md"), "verdict: pass\n")
	if st := statusOf(t, root, s("c")); !slices.ContainsFunc(shownArtifacts(st), func(a ArtifactStatus) bool { return a.Artifact == "verify-report.md" }) {
		t.Errorf("a written verify-report.md is listed: %v", st.Artifacts)
	}
}

// `## Skipped phases` is no longer read (S28): a proposal with or without
// it gives the same status, and design stays optional, as status only
// suggests it when the route asks for one.
func TestSkippedPhasesIsNotRead(t *testing.T) {
	jhonRoot(t)
	strip := func(t *testing.T, root, change string) {
		t.Helper()
		p := filepath.Join(root, "openspec/changes", change, "proposal.md")
		b, _ := os.ReadFile(p)
		before, _, _ := strings.Cut(string(b), "## Skipped phases")
		write(t, p, before)
	}
	// The RED before GREEN note names its change: the two differ only there.
	tdd := func(n string) bool { return strings.HasPrefix(n, "no RED before GREEN") }
	same := func(t *testing.T, what, want string, with, without Status) {
		t.Helper()
		if with.Next != want || without.Next != want {
			t.Errorf("%s: next %s with the section, %s without; want %s", what, with.Next, without.Next, want)
		}
		if !slices.Equal(slices.DeleteFunc(with.Notes, tdd), slices.DeleteFunc(without.Notes, tdd)) {
			t.Errorf("%s: notes differ:\n%q\n%q", what, with.Notes, without.Notes)
		}
		if strings.Contains(strings.Join(with.Notes, "\n"), "Skipped phases") {
			t.Errorf("%s: the section is read: %q", what, with.Notes)
		}
	}
	// No route: a tree outside git.
	plain := tree(t, nil)
	copyChange(t, plain, "skipped-design", "with")
	copyChange(t, plain, "skipped-design", "without")
	strip(t, plain, "without")
	same(t, "no route", "tasks", statusOf(t, plain, s("with")), statusOf(t, plain, s("without")))
	// The standard route, which asks for no design.
	routed := configuredRepo(t, "delivery: change\n", "src/app.py", "tests/test_app.py")
	copyChange(t, routed, "skipped-design", "with")
	copyChange(t, routed, "skipped-design", "without")
	strip(t, routed, "without")
	with, without := statusOf(t, routed, s("with")), statusOf(t, routed, s("without"))
	if with.Route == nil || with.Route.Design {
		t.Fatalf("standard route: %+v", with.Route)
	}
	// The standard route needs no tasks.md here (T31): apply comes next.
	same(t, "standard route", "apply", with, without)
}

// The status view lists an artifact only when it is written or the change owes it
// (L48): a standard-route change with only tasks.md lists tasks.md alone;
// a written exploration is listed; a high-risk change owes tasks.md and its
// verify, and a spec delta only when a main spec changes (T31).
func TestStatusListsOnlyWrittenOrOwedArtifacts(t *testing.T) {
	jhonRoot(t)
	names := func(st Status) []string {
		var out []string
		for _, a := range shownArtifacts(st) {
			out = append(out, a.Artifact)
		}
		return out
	}
	done := "## Intent\nDelivers: the totals\n\n- [ ] 1.1 totals\n"
	root := configuredRepo(t, "delivery: change\n", "src/app.py", "tests/test_app.py")
	write(t, filepath.Join(root, "openspec/changes/c/tasks.md"), done)
	if got := names(statusOf(t, root, s("c"))); !slices.Equal(got, []string{"tasks.md"}) {
		t.Errorf("standard route, tasks only: %v, want [tasks.md]", got)
	}
	if v := Human(statusOf(t, root, s("c"))); strings.Contains(v, "missing") {
		t.Errorf("the view names nothing missing:\n%s", v)
	}
	write(t, filepath.Join(root, "openspec/changes/c/exploration.md"), "# x\n")
	if got := names(statusOf(t, root, s("c"))); !slices.Equal(got, []string{"exploration.md", "tasks.md"}) {
		t.Errorf("a written exploration is listed: %v", got)
	}
	high := configuredRepo(t, "delivery: change\n", "src/auth/login.py", "tests/test_app.py")
	write(t, filepath.Join(high, "openspec/changes/c/tasks.md"), done)
	if got := names(statusOf(t, high, s("c"))); !slices.Equal(got, []string{"tasks.md", "verify-report.md"}) {
		t.Errorf("high risk owes tasks.md and verify: %v", got)
	}
}
