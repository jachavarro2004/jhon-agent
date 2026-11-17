package discover

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestFormatterIsTheOwningProjectsOwnFormatter(t *testing.T) {
	root := tree(t, map[string]string{
		"svc/go.mod":                     "module example.com/svc\n",
		"svc/a.go":                       "",
		"svc/README.md":                  "",
		"rs/Cargo.toml":                  "[package]\nname = \"rs\"\nedition = \"2021\"\n",
		"rs/src/lib.rs":                  "",
		"old/Cargo.toml":                 "[package]\nname = \"old\"\n",
		"old/src/lib.rs":                 "",
		"py/pyproject.toml":              "[project]\nname = \"py\"\n\n[tool.ruff]\n",
		"py/uv.lock":                     "",
		"py/app.py":                      "",
		"bare/pyproject.toml":            "[project]\nname = \"bare\"\n",
		"bare/app.py":                    "",
		"web/package.json":               `{"name": "web"}`,
		"web/node_modules/.bin/prettier": "",
		"web/src/a.ts":                   "",
		"plain/package.json":             `{"name": "plain"}`,
		"plain/a.js":                     "",
		"loose.go":                       "",
	})
	for file, want := range map[string]*Formatter{
		"svc/a.go":       {Dir: "svc", Argv: []string{"gofmt", "-w", "a.go"}, Tool: "gofmt"},
		"svc/README.md":  nil,
		"rs/src/lib.rs":  {Dir: "rs", Argv: []string{"rustfmt", "--edition", "2021", "src/lib.rs"}, Tool: "rustfmt"},
		"old/src/lib.rs": {Dir: "old", Argv: []string{"rustfmt", "--edition", "2015", "src/lib.rs"}, Tool: "rustfmt"},
		"py/app.py":      {Dir: "py", Argv: []string{"uv", "run", "ruff", "format", "app.py"}, Tool: "ruff"},
		"bare/app.py":    nil,
		"web/src/a.ts":   {Dir: "web", Argv: []string{filepath.Join(root, "web", "node_modules", ".bin", "prettier"), "--write", "src/a.ts"}, Tool: "prettier"},
		"plain/a.js":     nil,
		"loose.go":       nil,
	} {
		got := FormatterFor(filepath.Join(root, file))
		if got != nil {
			rel, _ := filepath.Rel(root, got.Dir)
			got.Dir = rel
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %+v, want %+v", file, got, want)
		}
	}
}
