package hook

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/grant"
)

// The ask sites and the classes they ask (S31, L154), each covered by
// test/guards/mode_ask_sites_test.go under every mode. A new caller of
// askJSON, or a new class a guard asks, fails here until it is listed there
// and here, so that no ask skips the owner's mode again.
var (
	askSites   = []string{"RouteGuard", "SafetyGuard", "VCSGuard"}
	askClasses = []string{"deploy", "destroy", "gh-write", "git-config", "outside-write", "publish", "push", "remote", "review", "settings", "unknown", "unreadable"}
)

func parseDir(t *testing.T, dir string) []*ast.File {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var out []*ast.File
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, file)
	}
	return out
}

// stringConsts is every package-level string constant in files, by name.
func stringConsts(files []*ast.File) map[string]string {
	out := map[string]string{}
	for _, f := range files {
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				continue
			}
			for _, s := range g.Specs {
				v := s.(*ast.ValueSpec)
				for i, n := range v.Names {
					if i < len(v.Values) {
						if lit, ok := v.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							out[n.Name], _ = strconv.Unquote(lit.Value)
						}
					}
				}
			}
		}
	}
	return out
}

// classesAsked is the class of every call to one of makers in files whose
// first argument is a string literal or constant; a parameter passed on
// (the makers themselves) names none.
func classesAsked(t *testing.T, files []*ast.File, makers ...string) []string {
	t.Helper()
	consts := stringConsts(files)
	var out []string
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok || len(c.Args) == 0 {
				return true
			}
			id, ok := c.Fun.(*ast.Ident)
			if !ok || !slices.Contains(makers, id.Name) {
				return true
			}
			cls := ""
			switch a := c.Args[0].(type) {
			case *ast.BasicLit:
				cls, _ = strconv.Unquote(a.Value)
			case *ast.Ident:
				cls = consts[a.Name]
			}
			if cls != "" && !slices.Contains(out, cls) {
				out = append(out, cls)
			}
			return true
		})
	}
	return out
}

func TestAskSitesAreListed(t *testing.T) {
	hookFiles := parseDir(t, ".")
	var sites []string
	for _, f := range hookFiles {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Name.Name == "askJSON" {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "askJSON" && !slices.Contains(sites, fn.Name.Name) {
						sites = append(sites, fn.Name.Name)
					}
				}
				return true
			})
		}
	}
	slices.Sort(sites)
	if !slices.Equal(sites, askSites) {
		t.Errorf("askJSON is called by %v, listed %v: a new ask site honours the owner's mode (passByMode) and is listed here and in test/guards/mode_ask_sites_test.go", sites, askSites)
	}

	// The classes: the guard package's findings, route-guard's ask, and
	// vcs-guard's settings and outside-write questions.
	classes := classesAsked(t, parseDir(t, filepath.Join("..", "guard")), "ask", "askAt", "writeAsk", "A")
	classes = append(classes, "destroy") // guard.destroy
	classes = append(classes, classesAsked(t, hookFiles, "ask")...)
	classes = append(classes, "settings", "outside-write") // vcsJudge
	slices.Sort(classes)
	classes = slices.Compact(classes)
	if !slices.Equal(classes, askClasses) {
		t.Errorf("the guards ask %v, listed %v: a new class is in grant.Classes or grant.Questions, and listed here and in test/guards/mode_ask_sites_test.go", classes, askClasses)
	}
	for _, cls := range classes {
		if _, ok := grant.Questions[cls]; !ok && !slices.Contains(grant.Classes, cls) {
			t.Errorf("the mode does not know class %s: it is neither a grant class nor in grant.Questions", cls)
		}
	}
}
