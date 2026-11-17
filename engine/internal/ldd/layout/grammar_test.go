package layout

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

func TestMain(m *testing.M) {
	root, _ := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	os.Setenv("JHON_ROOT", root)
	os.Exit(m.Run())
}

// Both shaped layouts read a change's record with today's grammar (T57
// slice 1, G1): tasks.md, specs/<capability>/spec.md, the main spec under
// SpecsDir, and the `## Slice <n>: <name>` heading.
func TestGrammarOfBothLayouts(t *testing.T) {
	for _, l := range []Layout{OpenSpec, Native} {
		if l.Tasks() != "tasks.md" || l.DeltaSpecs() != "specs" {
			t.Errorf("%s: Tasks %q, DeltaSpecs %q", l.Name(), l.Tasks(), l.DeltaSpecs())
		}
		if got, want := l.MainSpec("auth/spec.md"), l.Dir()+"/specs/auth/spec.md"; got != want {
			t.Errorf("%s: MainSpec = %q, want %q", l.Name(), got, want)
		}
		for line, want := range map[string]string{
			"## Slice 1: login":         "Slice 1: login",
			"### Slice 3 — audit trail": "Slice 3 — audit trail",
			"#### SLICE 12  ":           "SLICE 12",
			"## slice 2":                "slice 2",
			"##### Slice 2":             "",
			"## ſlice 1":                "",
			"# Slice 1: top":            "",
			"## Slices 1":               "",
			"Slice 1: plain":            "",
		} {
			got := ""
			if m := l.SliceHeading().FindStringSubmatch(line); m != nil {
				got = m[1]
			}
			if got != want {
				t.Errorf("%s: SliceHeading(%q) = %q, want %q", l.Name(), line, got, want)
			}
		}
	}
}

// The slice heading spells JavaScript's \s and . as package js does (G1).
func TestSliceGrammarIsJavaScripts(t *testing.T) {
	if jsSpace != js.Space || jsDot != js.Dot {
		t.Fatalf("jsSpace/jsDot drifted from js.Space/js.Dot")
	}
}

// A program that links no spec reader is told so, not handed an empty spec.
func TestScenariosWithoutReader(t *testing.T) {
	saved := specReader
	t.Cleanup(func() { specReader = saved })
	specReader = nil
	if _, err := Native.Scenarios("# x\n", 0); err != errNoSpecReader {
		t.Fatalf("Scenarios without a reader: %v", err)
	}
}

// The record's grammar is the layout's (T57 slice 1, G5): no non-test
// engine file outside ldd/layout names one layout (layout.OpenSpec,
// layout.Native) or the grammar itself (the tasks.md and specs artifact
// names, the slice heading), except where listed, each with its reason.
func TestGrammarNamedOnlyHere(t *testing.T) {
	exceptions := map[string]string{
		// Detection: openspec/ is found above the start, ldd/ only at it (T60, L477).
		"discover/projects.go layout.OpenSpec": "detection climbs to an ancestor's openspec/, never an ancestor's ldd/",
		"discover/projects.go layout.Native":   "detection climbs to an ancestor's openspec/, never an ancestor's ldd/",
		// Status's artifacts are an output contract named the same in every layout today.
		`ldd/status.go "tasks.md"`: "jhon status names its artifacts (artifacts[].artifact, owed) with these labels",
		`ldd/status.go "specs/"`:   "jhon status names its artifacts (artifacts[].artifact, owed) with these labels",
		// A delta spec's file name inside DeltaSpecs.
		`ldd/status.go "spec.md"`:         "the delta specs' file name, shared by the shaped layouts",
		`ldd/slicescenarios.go "spec.md"`: "the delta specs' file name, shared by the shaped layouts",
		// The renderer checks generated prompt text, not a project's record.
		`render/validate.go "tasks.md"`: "file names a rendered prompt may cite",
		`render/validate.go "spec.md"`:  "file names a rendered prompt may cite",
	}
	engine := filepath.Join("..", "..", "..")
	fset := token.NewFileSet()
	var found []string
	used := map[string]bool{}
	err := filepath.WalkDir(engine, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(engine, p)
		rel = strings.TrimPrefix(filepath.ToSlash(rel), "internal/")
		if strings.HasPrefix(rel, "ldd/layout/") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return err
		}
		report := func(pos token.Pos, what string) {
			key := rel + " " + what
			if _, ok := exceptions[key]; ok {
				used[key] = true
				return
			}
			found = append(found, fset.Position(pos).String()+" "+what)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && strings.Contains(strings.ToLower(id.Name), "layout") && (x.Sel.Name == "OpenSpec" || x.Sel.Name == "Native") {
					report(x.Pos(), id.Name+"."+x.Sel.Name)
				}
			case *ast.BasicLit:
				if x.Kind != token.STRING {
					return true
				}
				v, _ := strconv.Unquote(x.Value)
				if slices.Contains([]string{"tasks.md", "specs", "specs/", "spec.md"}, v) || strings.Contains(v, "[sS][lL][iI][cC][eE]") {
					report(x.Pos(), x.Value)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Errorf("the record's grammar named outside ldd/layout (ask the layout: Tasks, DeltaSpecs, MainSpec, SliceHeading, Scenarios): %s", strings.Join(found, ", "))
	}
	for k := range exceptions {
		if !used[k] {
			t.Errorf("exception %q matches nothing: drop it", k)
		}
	}
}
