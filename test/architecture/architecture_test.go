// Package architecture holds the engine's package structure to the rules
// the Go engine was built on: packages by feature, each owning the ports it
// consumes (no domain/application/adapters/infrastructure layer folders,
// which would add labels, not dependency enforcement or isolation); cli the
// only composition root; and leaf packages with no internal dependency.
package architecture

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

const module = "github.com/jachavarro2004/jhon-agent/engine/"

// imports is every internal import of the engine's non-test Go files as
// "<package> -> <package>", module prefix dropped. It parses the files
// itself, rather than asking `go list`, so the test cache sees every file
// the answer depends on and never keeps a stale pass.
func imports(t *testing.T) []string {
	t.Helper()
	var edges []string
	engine := filepath.Join(support.Root(), "engine")
	err := filepath.WalkDir(engine, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.Contains(p, string(filepath.Separator)+"testdata"+string(filepath.Separator)) {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(engine, filepath.Dir(p))
		for _, imp := range f.Imports {
			if path, _ := strconv.Unquote(imp.Path.Value); strings.HasPrefix(path, module) {
				edges = append(edges, filepath.ToSlash(rel)+" -> "+strings.TrimPrefix(path, module))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(edges)
	return slices.Compact(edges)
}

func TestTheEngineImportsExactlyTheRecordedDependencies(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(support.Root(), "test", "architecture", "imports.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var recorded []string
	for _, line := range strings.Split(string(b), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			recorded = append(recorded, line)
		}
	}
	actual := imports(t)
	for _, e := range actual {
		if !slices.Contains(recorded, e) {
			t.Errorf("new dependency %s: add it to test/architecture/imports.txt if it is meant", e)
		}
	}
	for _, e := range recorded {
		if !slices.Contains(actual, e) {
			t.Errorf("%s is gone: remove it from test/architecture/imports.txt", e)
		}
	}
}

func TestOnlyTheBinaryComposesAndTheLeavesDependOnNothing(t *testing.T) {
	leaves := map[string][]string{
		"internal/js":           {},
		"internal/isodate":      {},
		"internal/contract":     {"internal/js"},
		"internal/toml":         {"internal/js"},
		"internal/requirements": {"internal/contract", "internal/js"},
	}
	for _, e := range imports(t) {
		from, to, _ := strings.Cut(e, " -> ")
		if to == "internal/cli" && from != "cmd/jhon" {
			t.Errorf("%s: only cmd/jhon composes the commands", e)
		}
		if allowed, leaf := leaves[from]; leaf && !slices.Contains(allowed, to) {
			t.Errorf("%s: %s is a leaf package", e, from)
		}
	}
}

// TestTheCoreNeverImportsAnIntegration holds S37 (L193/L194): telemetry, the
// desktop palette (rose ink) and the dashboard are integrations, so
// no core package imports them; only cli, the composition root, wires them
// in, and an integration may use another (the dashboard paints with rose
// ink and reads telemetry).
func TestTheCoreNeverImportsAnIntegration(t *testing.T) {
	integrations := []string{"internal/telemetry", "internal/palette", "internal/dashboard"}
	for _, e := range imports(t) {
		from, to, _ := strings.Cut(e, " -> ")
		if slices.Contains(integrations, to) && from != "internal/cli" && !slices.Contains(integrations, from) {
			t.Errorf("%s: %s is an opt-in integration; only internal/cli wires it in", e, to)
		}
	}
}

// tuiImporters are the packages under engine that import a Bubble Tea or
// Lip Gloss module (github.com/charmbracelet/*) in non-test files, each as
// "<package> -> <import>".
func tuiImporters(t *testing.T, engine string) []string {
	return importersOf(t, engine, "github.com/charmbracelet/")
}

// importersOf are the packages under engine that import a module under
// prefix in non-test files, each as "<package> -> <import>".
func importersOf(t *testing.T, engine, prefix string) []string {
	t.Helper()
	var edges []string
	err := filepath.WalkDir(engine, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.Contains(p, string(filepath.Separator)+"testdata"+string(filepath.Separator)) {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(engine, filepath.Dir(p))
		for _, imp := range f.Imports {
			if path, _ := strconv.Unquote(imp.Path.Value); strings.HasPrefix(path, prefix) {
				edges = append(edges, filepath.ToSlash(rel)+" -> "+path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return edges
}

// tuiOutsideSetupUI are the importers that are not internal/setupui.
func tuiOutsideSetupUI(edges []string) []string { return outside(edges, "internal/setupui") }

// outside are the importers that are not pkg.
func outside(edges []string, pkg string) []string {
	var out []string
	for _, e := range edges {
		if from, _, _ := strings.Cut(e, " -> "); from != pkg {
			out = append(out, e)
		}
	}
	return out
}

// TestOnlySetupUIImportsTheTUILibraries holds S7: Bubble Tea and Lip Gloss
// stay in internal/setupui, the one TUI package.
func TestOnlySetupUIImportsTheTUILibraries(t *testing.T) {
	for _, e := range tuiOutsideSetupUI(tuiImporters(t, filepath.Join(support.Root(), "engine"))) {
		t.Errorf("%s: only internal/setupui imports github.com/charmbracelet/*", e)
	}
}

// TestTheTUILockCatchesAnotherPackage: the lock sees an import outside
// setupui (a fake tree), and lets setupui's through.
func TestTheTUILockCatchesAnotherPackage(t *testing.T) {
	engine := t.TempDir()
	for dir, src := range map[string]string{
		"internal/setupui": "package setupui\n\nimport _ \"github.com/charmbracelet/bubbletea\"\n",
		"internal/other":   "package other\n\nimport _ \"github.com/charmbracelet/lipgloss\"\n",
	} {
		if err := os.MkdirAll(filepath.Join(engine, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(engine, dir, "x.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := tuiOutsideSetupUI(tuiImporters(t, engine))
	if len(got) != 1 || got[0] != "internal/other -> github.com/charmbracelet/lipgloss" {
		t.Errorf("outside setupui = %v", got)
	}
}

// TestOnlyUpdateImportsSigstore holds S8: sigstore-go and the sigstore
// modules it brings verify a release's signature in internal/update alone.
func TestOnlyUpdateImportsSigstore(t *testing.T) {
	for _, e := range outside(importersOf(t, filepath.Join(support.Root(), "engine"), "github.com/sigstore/"), "internal/update") {
		t.Errorf("%s: only internal/update imports github.com/sigstore/*", e)
	}
}

// TestTheSigstoreLockCatchesAnotherPackage: the lock sees an import outside
// update (a fake tree), and lets update's through.
func TestTheSigstoreLockCatchesAnotherPackage(t *testing.T) {
	engine := t.TempDir()
	for dir, src := range map[string]string{
		"internal/update": "package update\n\nimport _ \"github.com/sigstore/sigstore-go/pkg/verify\"\n",
		"internal/other":  "package other\n\nimport _ \"github.com/sigstore/sigstore-go/pkg/bundle\"\n",
	} {
		if err := os.MkdirAll(filepath.Join(engine, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(engine, dir, "x.go"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := outside(importersOf(t, engine, "github.com/sigstore/"), "internal/update")
	if len(got) != 1 || got[0] != "internal/other -> github.com/sigstore/sigstore-go/pkg/bundle" {
		t.Errorf("outside update = %v", got)
	}
}
