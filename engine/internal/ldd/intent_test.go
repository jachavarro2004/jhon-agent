package ldd

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The intent of a change without a design lives in tasks.md `## Intent`, and
// status suggests a proposal or design only when the route calls for one.

const intentNote = "tasks.md has no ## Intent: state why and the acceptance criteria verify checks against"

// routedRepo is a git repository that is its own top level, with projects A
// and B. With design, both get enough untracked code that the route is
// verified and spans both, so it asks for a design; without, the diff is
// small and the route is standard.
func routedRepo(t *testing.T, design bool) string {
	t.Helper()
	root := tree(t, map[string]string{
		"A/pyproject.toml": "[project]\nname = \"a\"\n",
		"B/Cargo.toml":     "[package]\nname = \"b\"\n",
	})
	gitT(t, root, "init", "-q")
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "init")
	if design {
		write(t, filepath.Join(root, "A/app.py"), strings.Repeat("x = 1\n", 250))
		write(t, filepath.Join(root, "B/src/lib.rs"), strings.Repeat("// b\n", 250))
	} else {
		write(t, filepath.Join(root, "A/app.py"), "x = 1\n")
	}
	return root
}

func routeOfStatus(t *testing.T, st Status, design bool) {
	t.Helper()
	if st.Route == nil || st.Route.Design != design {
		t.Fatalf("route: %+v, want design %t", st.Route, design)
	}
}

func TestStatusSkipsAProposalTheRouteDoesNotAskFor(t *testing.T) {
	root := routedRepo(t, false)
	write(t, filepath.Join(root, "openspec/changes/small/.gitkeep"), "")
	st := statusOf(t, root, s("small"))
	routeOfStatus(t, st, false)
	// Its code has no test, so risk is high: it needs tasks.md, and no spec
	// delta, as no main spec changes (T31).
	if st.Next != "tasks" {
		t.Errorf("an empty change on a standard route: next %s, want tasks", st.Next)
	}

	write(t, filepath.Join(root, "openspec/changes/specced/specs/auth/spec.md"),
		"# Delta for Auth\n\n## ADDED Requirements\n\n### Requirement: Login\n\nThe system MUST log in.\n\n#### Scenario: ok\n\n- WHEN in\n- THEN ok\n")
	st = statusOf(t, root, s("specced"))
	// It needs no tasks.md (T31): apply, its RED before GREEN, comes next.
	if stateOf(st, "specs/") != "done" || st.Next != "apply" {
		t.Errorf("a specced change on a standard route skips design and tasks: specs %s, next %s, want apply", stateOf(st, "specs/"), st.Next)
	}
}

func TestStatusKeepsTheSuggestionWithAProposal(t *testing.T) {
	root := routedRepo(t, false)
	copyChange(t, root, "proposal-placeholder", "proposed")
	st := statusOf(t, root, s("proposed"))
	routeOfStatus(t, st, false)
	// A partial proposal the change does not need is not suggested (T31b).
	if st.Next != "tasks" {
		t.Errorf("an unneeded partial proposal is not suggested: next %s, want tasks", st.Next)
	}
	copyChange(t, root, "spec-no-scenario", "specced")
	if st := statusOf(t, root, s("specced")); st.Next != "spec" {
		t.Errorf("with a proposal, the order is today's: next %s", st.Next)
	}
}

func TestStatusSuggestsAProposalTheRouteAsksFor(t *testing.T) {
	root := routedRepo(t, true)
	write(t, filepath.Join(root, "openspec/changes/wide/.gitkeep"), "")
	st := statusOf(t, root, s("wide"))
	routeOfStatus(t, st, true)
	if st.Next != "propose" {
		t.Errorf("a route that asks for a design: next %s, want propose", st.Next)
	}
}

func TestStatusNotesATasksFileWithoutIntent(t *testing.T) {
	root := routedRepo(t, false)
	tasks := "## Slice 1: login\n\n- [ ] 1.1 Log in\n"
	write(t, filepath.Join(root, "openspec/changes/bare/tasks.md"), "# Tasks\n\n"+tasks)
	write(t, filepath.Join(root, "openspec/changes/meant/tasks.md"),
		"# Tasks\n\n## Intent\n\nUsers log in. Accepted when a valid login opens a session.\n\n"+tasks)
	st := statusOf(t, root, s("bare"))
	routeOfStatus(t, st, false)
	if !slices.Contains(st.Notes, intentNote) {
		t.Errorf("tasks.md without ## Intent is noted: %q", st.Notes)
	}
	if st := statusOf(t, root, s("meant")); slices.Contains(st.Notes, intentNote) {
		t.Errorf("tasks.md with ## Intent is not noted: %q", st.Notes)
	}
	if st := of(t, "tasks-partial"); slices.Contains(st.Notes, intentNote) {
		t.Errorf("a change without a route is not noted: %q", st.Notes)
	}

	wide := routedRepo(t, true)
	write(t, filepath.Join(wide, "openspec/changes/bare/tasks.md"), "# Tasks\n\n"+tasks)
	if st := statusOf(t, wide, s("bare")); slices.Contains(st.Notes, intentNote) {
		t.Errorf("a route with a design keeps its intent in proposal.md: %q", st.Notes)
	}
}
