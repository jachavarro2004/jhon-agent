package discover

import (
	"reflect"
	"strings"
	"testing"
)

// kinds is a project's checks as "kind: command".
func kinds(cs []Check) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.Kind+": "+c.Command)
	}
	return out
}

func TestChecksAreDiscoveredOnlyWhereAFileStatesThem(t *testing.T) {
	d := Discover(tree(t, map[string]string{
		"go/go.mod":            "module example.com/go\n",
		"rs/Cargo.toml":        "[package]\nname = \"rs\"\n",
		"web/package.json":     `{"scripts": {"test": "vitest", "lint": "eslint .", "format": "prettier --write .", "format:check": "prettier --check ."}}`,
		"bare/package.json":    `{"scripts": {"format": "prettier --write ."}}`,
		"py/pyproject.toml":    "[project]\nname = \"py\"\n\n[tool.ruff]\nline-length = 100\n",
		"uvpy/pyproject.toml":  "[project]\nname = \"uvpy\"\n",
		"uvpy/ruff.toml":       "line-length = 100\n",
		"uvpy/uv.lock":         "",
		"plain/pyproject.toml": "[project]\nname = \"plain\"\n",
	}))
	p := byPath(d)
	for path, want := range map[string][]string{
		"go":    {"lint: go vet ./...", "format: ! gofmt -l . | grep ."},
		"rs":    {"lint: cargo clippy -- -D warnings", "format: cargo fmt --check"},
		"web":   {"lint: npm run lint", "format: npm run format:check"},
		"bare":  {},
		"py":    {"lint: ruff check .", "format: ruff format --check ."},
		"uvpy":  {"lint: uv run ruff check .", "format: uv run ruff format --check ."},
		"plain": {},
	} {
		if got := kinds(p[path].Checks); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %q, want %q", path, got, want)
		}
	}
	if s := p["web"].Checks[1].Source; s != "package.json scripts.format:check" {
		t.Errorf("a check names the file that states it: %q", s)
	}
}

func TestCheckOverridesReplaceTheDiscoveredCommandOfTheirKind(t *testing.T) {
	r := resolved(t, "lint_command_override:\n  go: golangci-lint run\n  py: ruff check .\n  gone: x\nformat_command_override:\n  \".\": treefmt --fail-on-change\n")
	if got := kinds(by(r, "go").Checks); !reflect.DeepEqual(got, []string{"lint: golangci-lint run", "format: ! gofmt -l . | grep ."}) {
		t.Errorf("go: %q", got)
	}
	if c := by(r, "go").Checks[0]; c.Source != Override() {
		t.Errorf("an override's source is %q: %+v", Override(), c)
	}
	if got := kinds(by(r, "py").Checks); !reflect.DeepEqual(got, []string{"lint: ruff check ."}) {
		t.Errorf("py, which discovery gave none: %q", got)
	}
	if got := kinds(by(r, ".").Checks); !reflect.DeepEqual(got, []string{"format: treefmt --fail-on-change"}) {
		t.Errorf("the exempt root still takes a check: %q", got)
	}
	if !strings.Contains(strings.Join(r.Notes, "\n"), "lint_command_override for 'gone', which is not a discovered project") {
		t.Errorf("an override for no project is noted: %q", r.Notes)
	}
}
