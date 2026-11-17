// Conformance tests for `jhon init [dir] [--json]`. The discovery rules
// themselves are unit-tested in internal/discover.
//
// `jhon init` is a read-only diagnostic: it reports what discovery finds.
// The init step is the only writer of openspec/config.yaml, so every case here also
// proves the tree is byte-for-byte unchanged afterwards.
//
// Fixture trees are built in temporary directories rather than committed: a
// nested repository needs its own `.git`, which git refuses to track, and
// node_modules/ is ignored by this repository.
package ldd

import (
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// ── the bounded scan (no declared membership) ───────────────────────────────

var scanned = map[string]string{
	"package.json":                  `{"name":"root","scripts":{"test":"node --test"}}`,
	"A/pyproject.toml":              "[project]\nname = \"a\"\n\n[tool.pytest.ini_options]\naddopts = \"-q\"\n",
	"B/Cargo.toml":                  "[package]\nname = \"b\"\n",
	"x/y/go.mod":                    "module example.com/y\n",
	"x/y/z/go.mod":                  "module example.com/z\n",    // three levels down: beyond the bound
	"C/requirements.txt":            "requests\n",                // not a supported marker
	"D/pyproject.toml":              "[project]\nname = \"d\"\n", // Python with no explicit pytest config
	"node_modules/dep/package.json": "{}",
	"vendor/lib/go.mod":             "module v\n",
	"dist/package.json":             "{}",
	"build/package.json":            "{}",
	"out/package.json":              "{}",
	"target/Cargo.toml":             "[package]\n",
	".cache/package.json":           "{}",
	"__pycache__/pyproject.toml":    "",
	".venv/pyproject.toml":          "",
	"venv/pyproject.toml":           "",
	"nested/.git":                   "gitdir: /elsewhere\n", // a nested repository boundary
	"nested/package.json":           "{}",
	"E/sub/.git/HEAD":               "ref: refs/heads/main\n",
	"E/sub/package.json":            "{}",
}

func jhonPlain(t *testing.T, dir string, args ...string) support.Result {
	t.Helper()
	return support.Jhon(t, dir, support.Env(), args...)
}

// ── the CLI ─────────────────────────────────────────────────────────────────

func TestInitJSONWritesNothing(t *testing.T) {
	t.Parallel()
	root := tree(t, scanned)
	before := snapshot(t, root, true)
	r := jhonPlain(t, root, "init", root, "--json")
	equal(t, r.Code, 0, r.Stderr)
	out := object(t, r.Stdout)
	equal(t, out["root"], root)
	var paths []any
	for _, p := range out["projects"].([]any) {
		paths = append(paths, p.(map[string]any)["path"])
	}
	equal(t, paths, []any{".", "A", "B", "D", "x/y"})
	equal(t, out["openspec_config"].(map[string]any)["exists"], false)
	equal(t, snapshot(t, root, true), before)
}

func TestInitHumanReportWritesNothing(t *testing.T) {
	t.Parallel()
	root := tree(t, scanned)
	before := snapshot(t, root, true)
	r := jhonPlain(t, root, "init")
	equal(t, r.Code, 0, r.Stderr)
	match(t, r.Stdout, `workspace\s+\S+`)
	match(t, r.Stdout, `(?m)^\s+x/y\s+Go\s+go test \./\.\.\.`)
	match(t, r.Stdout, `openspec/config\.yaml\s+absent`)
	equal(t, snapshot(t, root, true), before)
}

func TestInitRefusesDryRun(t *testing.T) {
	t.Parallel()
	root := tree(t, map[string]string{"package.json": "{}"})
	before := snapshot(t, root, true)
	r := jhonPlain(t, root, "init", "--dry-run")
	equal(t, r.Code, 2, r.Stdout)
	match(t, r.Stderr, `usage: jhon init`)
	equal(t, snapshot(t, root, true), before)
}

func TestInitCoverage(t *testing.T) {
	t.Parallel()
	root := tree(t, makefileWorkspace())
	r := jhonPlain(t, root, "init", root, "--json")
	equal(t, r.Code, 0, r.Stderr)
	equal(t, keys(t, field(t, []byte(r.Stdout), "coverage")), []string{"covered", "command", "reason"})
	equal(t, object(t, r.Stdout)["coverage"].(map[string]any)["covered"], "unknown")
}

// ── testability ─────────────────────────────────────────────────────────────

func pytestMember(name string) string {
	return "[project]\nname = \"" + name + "\"\ndependencies = [\"pytest\"]\n\n[tool.pytest.ini_options]\ntestpaths = [\"tests\"]\n"
}

var uvExemptRoot = map[string]string{
	"pyproject.toml":           "[project]\nname = \"root\"\n\n[tool.uv.workspace]\nmembers = [\"libs/*\"]\n",
	"uv.lock":                  "version = 1\n",
	"README.md":                "workspace\n",
	"scripts/deploy.sh":        "#!/bin/sh\necho deploy\n",
	"libs/a/pyproject.toml":    pytestMember("a"),
	"libs/a/src/a/__init__.py": "",
	"libs/a/tests/test_a.py":   "def test_a():\n    assert True\n",
	"libs/b/pyproject.toml":    pytestMember("b"),
	"libs/b/b.py":              "",
	"libs/b/tests/test_b.py":   "def test_b():\n    assert True\n",
	".venv/lib/site.py":        "",
	"node_modules/x/index.js":  "",
}

func TestTestabilityVocabulary(t *testing.T) {
	t.Parallel()
	match(t, support.Read(t, support.Path("prose", "contracts", "vocabulary.yaml")),
		`(?m)^testability_state: \[testable, structurally_exempt, unknown\]$`)
}

func TestInitTestability(t *testing.T) {
	t.Parallel()
	root := tree(t, uvExemptRoot)
	out := object(t, jhonPlain(t, root, "init", root, "--json").Stdout)
	var got [][]any
	for _, p := range out["projects"].([]any) {
		m := p.(map[string]any)
		got = append(got, []any{m["path"], m["testability"]})
	}
	equal(t, got, [][]any{{".", "structurally_exempt"}, {"libs/a", "testable"}, {"libs/b", "testable"}})
	match(t, jhonPlain(t, root, "init", root).Stdout, `testability\s+structurally_exempt`)
}
