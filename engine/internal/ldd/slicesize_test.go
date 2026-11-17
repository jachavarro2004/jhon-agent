package ldd

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// sliceNote is the advisory note on a slice over the advisory size (S10).
func sliceNote(n string, lines, advisory int) string {
	return fmt.Sprintf("slice %s adds %d lines of code, over the advisory %d: say so to the owner and offer to split it into slices that each build, test, and deliver", n, lines, advisory)
}

// sliceRepo is a repository on a feature branch off main whose change c1
// has tasks (uncommitted) and adds files, each with lines lines.
func sliceRepo(t *testing.T, tasks string, lines int, files ...string) string {
	t.Helper()
	root := routeRepo(t, lines, files...)
	write(t, filepath.Join(root, "openspec/changes/c1/tasks.md"), tasks)
	return root
}

func sliceStatus(t *testing.T, root string) Status {
	t.Helper()
	st, err := StatusOf(root, nil)
	if err != nil || st.Route == nil || st.Route.Slice == nil {
		t.Fatalf("status: %v, route %+v", err, st.Route)
	}
	return st
}

const twoSlices = "## Slice 1: a\n- [ ] 1.1 a\n\n## Slice 2: b\n- [ ] 2.1 b\n"

// Status reports the lines of code the slice in progress adds, and notes
// one over the advisory size with its real count; never a refusal, and
// `next` stays the same (S10).
func TestSliceSizeInStatus(t *testing.T) {
	jhonRoot(t)
	under := sliceStatus(t, sliceRepo(t, twoSlices, 300, "src/app.py"))
	if s := under.Route.Slice; s.Slice != "1" || s.Lines != 300 || s.Advisory != 400 {
		t.Errorf("300 lines of code: %+v", s)
	}
	if slices.ContainsFunc(under.Notes, func(n string) bool { return strings.Contains(n, "over the advisory") }) {
		t.Errorf("300 lines of code get no note: %q", under.Notes)
	}

	over := sliceStatus(t, sliceRepo(t, twoSlices, 450, "src/app.py"))
	if s := over.Route.Slice; s.Slice != "1" || s.Lines != 450 || s.Advisory != 400 {
		t.Errorf("450 lines of code: %+v", s)
	}
	if !slices.Contains(over.Notes, sliceNote("1", 450, 400)) {
		t.Errorf("450 lines of code: no note with the count: %q", over.Notes)
	}
	if over.Next != under.Next || !over.Ok {
		t.Errorf("the note changes next: %s, want %s", over.Next, under.Next)
	}
	if h := Human(over); !strings.Contains(h, "slice       slice 1 adds 450 lines of code (advisory 400)") {
		t.Errorf("the human report has no slice size:\n%s", h)
	}

	docs := sliceStatus(t, sliceRepo(t, twoSlices, 450, "docs/guide.md"))
	if s := docs.Route.Slice; s.Lines != 0 {
		t.Errorf("450 lines of docs count: %+v", s)
	}
	if slices.ContainsFunc(docs.Notes, func(n string) bool { return strings.Contains(n, "over the advisory") }) {
		t.Errorf("450 lines of docs get no note: %q", docs.Notes)
	}
}

// A second slice is measured from the commit that closed the first, not
// from the merge base.
func TestSliceSizeFromTheLastSliceCommit(t *testing.T) {
	jhonRoot(t)
	root := sliceRepo(t, twoSlices, 450, "src/app.py")
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "plan")
	write(t, filepath.Join(root, "openspec/changes/c1/tasks.md"), strings.Replace(twoSlices, "- [ ] 1.1", "- [x] 1.1", 1))
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "slice 1")
	write(t, filepath.Join(root, "src/b.py"), strings.Repeat("y = 1\n", 300))
	st := sliceStatus(t, root)
	if s := st.Route.Slice; s.Slice != "2" || s.Lines != 300 {
		t.Errorf("slice 2 measured from slice 1's commit: %+v", s)
	}
	if slices.ContainsFunc(st.Notes, func(n string) bool { return strings.Contains(n, "over the advisory") }) {
		t.Errorf("slice 2 with 300 lines gets no note: %q", st.Notes)
	}
	write(t, filepath.Join(root, "src/b.py"), strings.Repeat("y = 1\n", 450))
	if st := sliceStatus(t, root); !slices.Contains(st.Notes, sliceNote("2", 450, 400)) {
		t.Errorf("slice 2 with 450 lines: %q", st.Notes)
	}
}
