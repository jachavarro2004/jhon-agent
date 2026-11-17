package integrations

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// exceptions are the integration literals engine code may still hold
// outside this reader, file (relative to engine/) and literal, each with
// the one-line reason it stays (T58b O4, L1167). A new literal is refused;
// moving one of these out of the code removes its line here.
var exceptions = []struct{ lit, reason string }{
	{"internal/cli/cmd_context_state.go: integrations.engram", "outside this slice's surfaces"},
	{"internal/ldd/state.go: engram", "outside this slice's surfaces"},
	{"internal/ldd/state.go: .codegraph", "outside this slice's surfaces"},
	{"internal/ldd/state.go: codegraph", "outside this slice's surfaces"},
	{"internal/ldd/context.go: engram", "outside this slice's surfaces"},
	{"internal/hook/launchcontext.go: .codegraph", "CodeGraph's own index folder; v1 has no field for it, and a new field is v2 (integrations/README.md)"},
	{"internal/hook/launchcontext.go: .codegraph-wsl", "CodeGraph's own index folder under WSL; v1 has no field for it, and a new field is v2"},
	{"internal/hook/launchcontext.go: codegraph.db", "CodeGraph's own database file; v1 has no field for it, and a new field is v2"},
}

// TestNoIntegrationLiteralsOutsideTheReader (M2, L1150): engine code names
// no integration in a string literal; it reads names, servers, tools,
// settings and checks from here. Allowed: tests (_test.go), this package,
// testdata, comments, and a literal with a space in it — a sentence that
// mentions a name, as an error or a help text does. Refused: a literal with
// no space that is or holds, as a whole word, an integration's name,
// display name, own setting (integrations.<name>), binary or server, or an mcp__<server> tool name.
func TestNoIntegrationLiteralsOutsideTheReader(t *testing.T) {
	var idents []string
	for _, m := range All() {
		idents = append(idents, m.Name, m.Display, m.Doctor.Binary)
		if m.Setting == "integrations."+m.Name {
			idents = append(idents, m.Setting) // its own; claude_code.connectors is the runtime's
		}
		idents = append(idents, m.ClaudeServers...)
		if m.Pi != nil {
			idents = append(idents, m.Pi.Server)
		}
	}
	idents = slices.DeleteFunc(slices.Compact(idents), func(s string) bool { return s == "" || strings.Contains(s, " ") })
	var alts []string
	for _, s := range idents {
		alts = append(alts, regexp.QuoteMeta(s))
	}
	word := regexp.MustCompile(`(?i)(?:^|[^a-z0-9_])(?:` + strings.Join(alts, "|") + `)(?:[^a-z0-9_]|$)`)
	mcp := regexp.MustCompile(`(?i)mcp__(?:` + strings.Join(alts, "|") + `)`)

	engine, _ := filepath.Abs("../..")
	var found []string
	err := filepath.WalkDir(engine, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(engine, p)
		if d.IsDir() && (d.Name() == "testdata" || rel == filepath.Join("internal", "integrations")) {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, err := strconv.Unquote(lit.Value)
			if err != nil || strings.ContainsAny(v, " \n") {
				return true
			}
			if word.MatchString(v) || mcp.MatchString(v) {
				found = append(found, filepath.ToSlash(rel)+": "+v)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, e := range exceptions {
		if e.reason == "" {
			t.Errorf("%s: an exception gives its reason", e.lit)
		}
		listed = append(listed, e.lit)
		if !slices.Contains(found, e.lit) {
			t.Errorf("%s is no longer in the code: remove it from exceptions", e.lit)
		}
	}
	for _, f := range found {
		if !slices.Contains(listed, f) {
			t.Errorf("%s: an integration literal; read it from engine/internal/integrations", f)
		}
	}
}

// TestStrayCatchesALiteral: the rule's own check, on a literal it must refuse
// and a sentence it must allow.
func TestStrayCatchesALiteral(t *testing.T) {
	re := regexp.MustCompile(`(?i)(?:^|[^a-z0-9_])(?:engram)(?:[^a-z0-9_]|$)`)
	for v, want := range map[string]bool{"engram": true, "integrations.engram": true, "engram_bytes": false, "mcp__engram__x": false} {
		if re.MatchString(v) != want {
			t.Errorf("%q: %v, want %v", v, !want, want)
		}
	}
}
