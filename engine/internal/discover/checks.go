package discover

// A project's code checks (contracts/ldd.md, "Code checks"): the
// lint and format commands a file states, as the test command is found.
// Each only reads; a format check fails on unformatted code and changes
// nothing. A toolchain's own checker counts (go vet, gofmt, clippy, cargo
// fmt); otherwise a file must name the tool: a package.json script, or a
// ruff configuration.

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"path/filepath"
	"slices"
	"sync"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// A Check is one code check: Kind is one of `code_check_kind`.
type Check struct {
	Kind    string `json:"kind"`
	Command string `json:"command"`
	Source  string `json:"source"`
}

// checkVocab is `code_check_kind` and the config key that overrides each kind.
var checkVocab = sync.OnceValues(func() ([]string, map[string]string) {
	kinds := contract.MustVocabulary("code_check_kind", 2)
	_, keys := contract.Map(contract.MustContract("vocabulary.yaml"), "code_check_config")
	for _, k := range kinds {
		if keys[k] == "" {
			panic(fmt.Errorf("contracts/vocabulary.yaml: code_check_config must name %s", k))
		}
	}
	return kinds, keys
})

var ruffSection = lazyre.New(`(?m)^\[tool\.ruff[.\]]`)

// checks are the checks the project at dir states, lint before format; the
// first marker that states a kind gives it.
func checks(dir, root string, markers []string) []Check {
	kinds, _ := checkVocab()
	lint, format := kinds[0], kinds[1]
	out := []Check{}
	add := func(kind, command, source string) {
		if !slices.ContainsFunc(out, func(c Check) bool { return c.Kind == kind }) {
			out = append(out, Check{kind, command, source})
		}
	}
	for _, m := range markers {
		switch m {
		case "package.json":
			src, _ := read(filepath.Join(dir, m))
			pkg, _ := parseJSON(src)
			scripts := field(pkg, "scripts")
			if s, ok := field(scripts, "lint").(string); ok && js.Trim(s) != "" {
				add(lint, "npm run lint", "package.json scripts.lint")
			}
			if s, ok := field(scripts, "format:check").(string); ok && js.Trim(s) != "" {
				add(format, "npm run format:check", "package.json scripts.format:check")
			}
		case "go.mod":
			add(lint, "go vet ./...", "go.mod; the Go toolchain's own go vet")
			add(format, "! gofmt -l . | grep .", "go.mod; the Go toolchain's own gofmt, failing on the files it lists")
		case "Cargo.toml":
			add(lint, "cargo clippy -- -D warnings", "Cargo.toml; Cargo's own clippy")
			add(format, "cargo fmt --check", "Cargo.toml; Cargo's own rustfmt")
		case "pyproject.toml":
			src, _ := read(filepath.Join(dir, m))
			config := ""
			switch {
			case ruffSection.MatchString(src):
				config = "pyproject.toml [tool.ruff]"
			case project.Exists(filepath.Join(dir, "ruff.toml")):
				config = "ruff.toml"
			case project.Exists(filepath.Join(dir, ".ruff.toml")):
				config = ".ruff.toml"
			default:
				continue
			}
			// ruff runs where the project's manager installs it, as pytest does.
			doc, err := parsePyproject(dir)
			if err != nil {
				continue
			}
			managers := pythonManagers(dir, root, doc)
			if len(managers) > 1 {
				continue
			}
			prefix, via := "", ""
			if len(managers) == 1 {
				prefix, via = managers[0].name+" run ", fmt.Sprintf("; %s (%s)", managers[0].name, managers[0].evidence)
			}
			add(lint, prefix+"ruff check .", config+via)
			add(format, prefix+"ruff format --check .", config+via)
		}
	}
	return out
}

// resolveChecks is a project's checks after the config's overrides, each
// override replacing the discovered check of its kind.
func resolveChecks(p *Project, overrides map[string]*StringMap) []Check {
	kinds, _ := checkVocab()
	out := []Check{}
	for _, kind := range kinds {
		if command, ok := overrides[kind].Get(p.Path); ok {
			out = append(out, Check{kind, command, Override()})
		} else if i := slices.IndexFunc(p.Checks, func(c Check) bool { return c.Kind == kind }); i >= 0 {
			out = append(out, p.Checks[i])
		}
	}
	return out
}

// checkOverrides are the config's check overrides by kind, and the notes
// on entries for paths discovery did not find.
func checkOverrides(config string, known map[string]bool) (map[string]*StringMap, []string) {
	kinds, keys := checkVocab()
	out := map[string]*StringMap{}
	var notes []string
	for _, kind := range kinds {
		m := ConfigMap(config, keys[kind])
		out[kind] = m
		if m == nil {
			continue
		}
		for _, path := range m.Keys {
			if !known[path] {
				notes = append(notes, overrideNote(configVocab().notes["override_undiscovered"], keys[kind], path))
			}
		}
	}
	return out, notes
}
