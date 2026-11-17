package ldd

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// tasks.md is asked for only on what Jhon can measure (T31, L311): over the
// advisory lines of code, a high-risk item, or more than one component.
func TestNeedsTasks(t *testing.T) {
	small := Route{Tier: Medium, Lines: 120, VerifiedLines: 400, Projects: []string{"engine"}}
	for _, c := range []struct {
		what string
		r    func(Route) Route
		want []string
	}{
		{"a small one-component change", func(r Route) Route { return r }, nil},
		{"at the advisory size", func(r Route) Route { r.Lines = 400; return r }, nil},
		{"over the advisory size", func(r Route) Route { r.Lines = 401; return r },
			[]string{"401 lines of code, over the advisory 400"}},
		{"a high-risk item", func(r Route) Route { r.Tier = High; return r }, []string{"risk high"}},
		{"two components", func(r Route) Route { r.Projects = []string{"engine", "shell"}; return r },
			[]string{"2 components: engine, shell"}},
		{"no component", func(r Route) Route { r.Projects = []string{}; return r }, nil},
	} {
		if got := NeedsTasks(c.r(small)); !slices.Equal(got, c.want) {
			t.Errorf("%s: %q, want %q", c.what, got, c.want)
		}
	}
}

// A spec delta is asked for only when a main spec changes or is added.
func TestNeedsSpec(t *testing.T) {
	for _, c := range []struct {
		what    string
		changed map[string]string
		want    []string
	}{
		{"no main spec", map[string]string{"src/app.py": "M", "openspec/changes/c/specs/a/spec.md": "A"}, nil},
		{"a main spec changes", map[string]string{"openspec/specs/a/spec.md": "M"}, []string{"main spec openspec/specs/a/spec.md changes"}},
		{"a main spec is added", map[string]string{"openspec/specs/b/spec.md": "A"}, []string{"main spec openspec/specs/b/spec.md is added"}},
	} {
		if got := NeedsSpec(c.changed); !slices.Equal(got, c.want) {
			t.Errorf("%s: %q, want %q", c.what, got, c.want)
		}
	}
}

// From the repository: the change's LDD record files never count toward
// the lines, and a main spec in the diff (or untracked) asks for a spec.
func TestNeedsOf(t *testing.T) {
	jhonRoot(t)
	root := routeRepo(t, 30, "src/app.py", "tests/test_app.py")
	write(t, filepath.Join(root, "openspec/changes/c/tasks.md"), strings.Repeat("- [ ] a task\n", 900))
	r, err := RouteOf(root)
	if err != nil {
		t.Fatal(err)
	}
	n := NeedsOf(root, r)
	if n.Tasks || n.Spec || n.Proposal || n.Design {
		t.Errorf("a small change with a long record: %+v", n)
	}
	write(t, filepath.Join(root, "openspec/specs/a/spec.md"), "# a\n")
	if n := NeedsOf(root, r); !n.Spec || !slices.Equal(n.Why.Spec, []string{"main spec openspec/specs/a/spec.md is added"}) {
		t.Errorf("an untracked main spec: %+v", n)
	}
	gitT(t, root, "add", "-A")
	gitT(t, root, "commit", "-q", "-m", "spec")
	gitT(t, root, "checkout", "-q", "main")
	gitT(t, root, "merge", "-q", "feature")
	gitT(t, root, "checkout", "-q", "-b", "next")
	if n := NeedsOf(root, r); n.Spec {
		t.Errorf("no main spec touched: %+v", n)
	}
	write(t, filepath.Join(root, "openspec/specs/a/spec.md"), "# a\nmore\n")
	if n := NeedsOf(root, r); !slices.Equal(n.Why.Spec, []string{"main spec openspec/specs/a/spec.md changes"}) {
		t.Errorf("a changed main spec: %+v", n)
	}
}

// `needs` follows a selected change's status JSON fields, with every key
// present, before `folder`, the last.
func TestNeedsInStatusJSON(t *testing.T) {
	jhonRoot(t)
	root := routeRepo(t, 30, "src/app.py", "tests/test_app.py")
	write(t, filepath.Join(root, "openspec/changes/c/tasks.md"), "- [ ] a task\n")
	st := statusOf(t, root, s("c"))
	o := st.object()
	if o[len(o)-2].key != "needs" || o[len(o)-1].key != "folder" {
		t.Fatalf("last keys %q, %q", o[len(o)-2].key, o[len(o)-1].key)
	}
	b, _ := json.Marshal(o[len(o)-2].value)
	if got := string(b); got != `{"tasks":false,"spec":false,"proposal":false,"design":false,"why":{"tasks":[],"spec":[],"proposal":[],"design":[]}}` {
		t.Errorf("needs: %s", got)
	}
}
