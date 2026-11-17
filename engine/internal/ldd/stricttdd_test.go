package ldd

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/discover"
)

// jhonRoot points the contract readers at this checkout.
func jhonRoot(t *testing.T) {
	t.Helper()
	root, _ := filepath.Abs("../../..")
	t.Setenv("JHON_ROOT", root)
}

// tree writes files under a fresh temporary root.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		write(t, filepath.Join(root, rel), content)
	}
	return root
}

// resolveTree resolves a tree whose openspec/config.yaml is config (absent when nil).
func resolveTree(t *testing.T, files map[string]string, config *string, mode bool) StrictTdd {
	t.Helper()
	files = maps.Clone(files)
	if config != nil {
		files["openspec/config.yaml"] = *config
	}
	return ResolveStrictTdd(config, mode, discover.Discover(tree(t, files)))
}

func cfg(s string) *string { return &s }

func with(base map[string]string, extra map[string]string) map[string]string {
	out := maps.Clone(base)
	maps.Copy(out, extra)
	return out
}

// triple is [value, source, evidence], the shape most assertions compare.
func triple(r StrictTdd) [3]any { return [3]any{r.Value, r.Source, r.Evidence} }

func expect(t *testing.T, label string, r StrictTdd, value bool, source, evidence string) {
	t.Helper()
	if want := [3]any{value, source, evidence}; triple(r) != want {
		t.Errorf("%s: %v, want %v (%s)", label, triple(r), want, r.Reason)
	}
}

func matches(t *testing.T, label, s, pattern string) {
	t.Helper()
	if !regexp.MustCompile(pattern).MatchString(s) {
		t.Errorf("%s: %q does not match %s", label, s, pattern)
	}
}

var (
	single            = map[string]string{"package.json": `{"scripts":{"test":"vitest run"}}`}
	evidenceMakefile  = "test-all:\n\tcd api && pytest\n\tcd frontend && npm test\n\tcd worker && go test ./...\n"
	makefileWorkspace = map[string]string{
		"Makefile":              "test-all:\n\tcd api && pytest\n",
		"api/pyproject.toml":    "[project]\nname = \"api\"\n\n[tool.pytest.ini_options]\naddopts = \"-q\"\n",
		"frontend/package.json": `{"scripts":{"test":"vitest run"}}`,
		"worker/go.mod":         "module example.com/worker\n",
	}
	twoUncovered = map[string]string{
		"A/pyproject.toml": "[tool.pytest.ini_options]\n",
		"B/Cargo.toml":     "[package]\nname = \"b\"\n",
		"C/pyproject.toml": "[project]\nname = \"c\"\n",
		"C/app.py":         "print('c')\n",
	}
	twoWithEvidence = with(twoUncovered, map[string]string{"Makefile": "test-all:\n\tcd A && pytest\n\tcd B && cargo test\n\tcd C && pytest\n"})
)

const (
	evidenceAB         = "strict_tdd_evidence:\n  A: Makefile:2\n  B: Makefile:3\n  C: Makefile:4\n"
	overrideC          = "test_command_override:\n  C: make -C .. test-c\n"
	legacyNote         = `legacy(?s:.)*strict_tdd(?s:.)*/jhon-ldd init`
	legacyEvidenceNote = "legacy strict_tdd_evidence; reconcile into test_command_override with /jhon-ldd init"
)

func TestTddContractMode(t *testing.T) {
	jhonRoot(t)
	for src, want := range map[string]bool{"## Strict TDD\n\n**Mode: enabled.**\n": true, "## Strict TDD\r\n\r\n**Mode: disabled.**  \r\n": false} {
		if got, err := TddContractMode(src); got != want || err != nil {
			t.Errorf("TddContractMode(%q) = %v, %v", src, got, err)
		}
	}
	if _, err := TddContractMode("## Strict TDD\n\nno mode here\n"); err == nil || !strings.Contains(err.Error(), "contracts/tdd.md") {
		t.Errorf("no mode line: %v", err)
	}
	if _, err := TddContractMode("**Mode: enabled.** now\n"); err == nil {
		t.Error("a mode line with trailing text was accepted")
	}
	if _, err := TddContractMode(readContract(t, "tdd.md")); err != nil {
		t.Errorf("the repository's own contract declares no mode: %v", err)
	}
}

func TestPrecedence(t *testing.T) {
	jhonRoot(t)
	for _, mode := range []bool{true, false} {
		r := resolveTree(t, single, nil, mode)
		expect(t, "no config", r, mode, "tdd.md", "n/a")
		if r.Notes == nil || len(r.Notes) != 0 {
			t.Errorf("no config: notes %#v", r.Notes)
		}
	}
	expect(t, "legacy over the contract", resolveTree(t, single, cfg("strict_tdd: false\n"), true), false, "legacy", "n/a")
	r := resolveTree(t, single, cfg("schema: spec-driven\n"), false)
	expect(t, "neither policy nor legacy derives", r, true, "derived", "ok")
	if !reflect.DeepEqual(r.Notes, []string{}) {
		t.Errorf("derived notes %#v", r.Notes)
	}
	// An empty config is a config: it derives, unlike no config.
	expect(t, "empty config", resolveTree(t, single, cfg(""), false), true, "derived", "ok")
}

func TestCapabilityFromResolvedProjects(t *testing.T) {
	jhonRoot(t)
	expect(t, "fixture 1", resolveTree(t, single, cfg("strict_tdd: true\n"), true), true, "legacy", "ok")
	r := resolveTree(t, map[string]string{
		"Cargo.toml": "[workspace]\nmembers = [\"crates/*\"]\n", "crates/a/Cargo.toml": "[package]\n", "crates/b/Cargo.toml": "[package]\n",
	}, cfg("strict_tdd: true\n"), true)
	expect(t, "fixture 2", r, true, "legacy", "ok")
	r = resolveTree(t, map[string]string{"A/pyproject.toml": "[tool.pytest.ini_options]\n", "B/Cargo.toml": "[package]\n"}, cfg("strict_tdd: true\n"), true)
	expect(t, "fixture 6", r, true, "legacy", "ok")
	matches(t, "fixture 6 names A", r.Reason, `A`)
	matches(t, "fixture 6 names B", r.Reason, `B`)

	r = resolveTree(t, makefileWorkspace, cfg("strict_tdd: true\nstrict_tdd_evidence:\n  api: Makefile:2\n  frontend: Makefile:2\n  worker: Makefile:2\n"), true)
	expect(t, "fixture 4", r, true, "legacy", "ok")
	matches(t, "fixture 4 note", strings.Join(r.Notes, "\n"), `legacy strict_tdd_evidence; reconcile into test_command_override`)
	r = resolveTree(t, with(makefileWorkspace, map[string]string{"Makefile": evidenceMakefile}),
		cfg("strict_tdd: true\nstrict_tdd_evidence:\n  api: Makefile:2\n  \"frontend\": Makefile:3\n  worker: 'Makefile:4'\n"), true)
	expect(t, "fixture 5", r, true, "legacy", "ok")

	base := with(makefileWorkspace, map[string]string{"Makefile": evidenceMakefile})
	for _, block := range []string{
		"  api: Makefile:2\n  worker: Makefile:3\n",
		"  api: Makefile:2\n  frontend: scripts/test.sh:1\n  worker: Makefile:4\n",
		"  api: Makefile:2\n  frontend: Makefile:3\n  worker: Makefile:40\n",
		"  api: ../Makefile:2\n  frontend: Makefile\n  worker: Makefile:4\n",
		"  \".\": Makefile:2\n",
	} {
		r := resolveTree(t, base, cfg("strict_tdd: true\nstrict_tdd_evidence:\n"+block), true)
		expect(t, block, r, true, "legacy", "ok")
		matches(t, block, strings.Join(r.Notes, "\n"), `legacy strict_tdd_evidence`)
	}
	r = resolveTree(t, map[string]string{"Makefile": "t:\n\tcd api && pytest\n", "api/pyproject.toml": "[project]\n", "api/app.py": ""},
		cfg("strict_tdd: true\nstrict_tdd_evidence:\n  api: Makefile:2\n"), true)
	expect(t, "unknown stays unknown", r, true, "legacy", "invalid")
	matches(t, "unknown reason", r.Reason, `unknown: api`)

	expect(t, "no coverage", resolveTree(t, map[string]string{"README.md": "x\n"}, cfg("strict_tdd: true\n"), true), true, "legacy", "invalid")
	expect(t, "false is never judged", resolveTree(t, makefileWorkspace, cfg("strict_tdd: false\n"), true), false, "legacy", "n/a")
}

func TestStrictTddFor(t *testing.T) {
	jhonRoot(t)
	root := tree(t, with(makefileWorkspace, map[string]string{"openspec/config.yaml": "strict_tdd: true\n"}))
	got, err := StrictTddFor(root)
	mode, _ := TddContractMode(readContract(t, "tdd.md"))
	if want := ResolveStrictTdd(cfg("strict_tdd: true\n"), mode, discover.Discover(root)); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("StrictTddFor = %+v, %v; want %+v", got, err, want)
	}
	got, err = StrictTddFor(tree(t, single))
	if want := ResolveStrictTdd(nil, mode, discover.Discover(root)); err != nil || got.Source != want.Source {
		t.Errorf("StrictTddFor without a config = %+v, %v", got, err)
	}
}

func readContract(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "prose", "contracts", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestNestedLegacyForm(t *testing.T) {
	jhonRoot(t)
	expect(t, "nested true", resolveTree(t, single, cfg("schema: spec-driven\ntesting:\n  strict_tdd: true\n  test_command: \"npm test\"\n"), true), true, "legacy", "ok")
	r := resolveTree(t, single, cfg("testing:\n    test_command: \"npm test\"\n    strict_tdd: false  # off\n"), true)
	expect(t, "nested false", r, false, "legacy", "n/a")
	if strings.Contains(r.Reason, "does not set") {
		t.Errorf("nested false reason %q", r.Reason)
	}
	expect(t, "top-level", resolveTree(t, single, cfg("schema: spec-driven\nstrict_tdd: true\ntesting:\n  test_command: \"npm test\"\n"), true), true, "legacy", "ok")
	expect(t, "agree true", resolveTree(t, single, cfg("strict_tdd: true\ntesting:\n  strict_tdd: true\n"), true), true, "legacy", "ok")
	expect(t, "agree false", resolveTree(t, single, cfg("strict_tdd: false\ntesting:\n  strict_tdd: false\n"), true), false, "legacy", "n/a")
	for _, config := range []string{"strict_tdd: false\ntesting:\n  strict_tdd: true\n", "strict_tdd: true\ntesting:\n  strict_tdd: false\n"} {
		r := resolveTree(t, single, cfg(config), true)
		expect(t, config, r, true, "legacy", "invalid")
		matches(t, config, r.Reason, `top-level`)
		matches(t, config, r.Reason, `testing`)
		matches(t, config, strings.Join(r.Notes, "\n"), legacyNote)
	}
	for _, config := range []string{
		"schema: spec-driven\ncontext: |\n  Projects here use strict_tdd: true\n  strict_tdd: true\n",
		"schema: spec-driven\ncontext: >-\n  testing:\n    strict_tdd: true\n",
		"rules:\n  testing:\n    strict_tdd: true\n",
		"other:\n  strict_tdd: true\n",
		"# strict_tdd: true\ntesting:\n  # strict_tdd: true\n  test_command: pytest\n",
		"testing: # a comment, not a value\n  strict_tdd: maybe\n",
	} {
		r := resolveTree(t, single, cfg(config), true)
		expect(t, config, r, true, "derived", "ok")
		if !reflect.DeepEqual(r.Notes, []string{}) {
			t.Errorf("%q: notes %#v", config, r.Notes)
		}
	}
	legacy := "schema: spec-driven\n\ncontext: |\n  Tech stack: Python services with pytest.\n  Team rule: strict_tdd: true is expected for every change.\n  Architecture: hexagonal.\n\ntesting:\n  strict_tdd: true\n  test_command: \"pytest\"\n  linter: \"none\"\n  type_checker: \"none\"\n"
	twoPython := map[string]string{
		"pyproject.toml":         "[project]\nname = \"root\"\n\n[tool.pytest.ini_options]\ntestpaths = [\"tests\"]\n",
		"service/pyproject.toml": "[project]\nname = \"service\"\n\n[tool.pytest.ini_options]\ntestpaths = [\"tests\"]\n",
	}
	expect(t, "regression", resolveTree(t, twoPython, cfg(legacy), true), true, "legacy", "ok")
}

func TestPolicyDerivedLegacy(t *testing.T) {
	jhonRoot(t)
	r := resolveTree(t, single, cfg("strict_tdd_policy: require\n"), true)
	expect(t, "require covered", r, true, "policy", "ok")
	if !reflect.DeepEqual(r.Notes, []string{}) {
		t.Errorf("require notes %#v", r.Notes)
	}
	r = resolveTree(t, twoUncovered, cfg("strict_tdd_policy: require\n"), true)
	expect(t, "require unknown", r, true, "policy", "invalid")
	matches(t, "require unknown", r.Reason, `unknown: C`)
	expect(t, "evidence alone", resolveTree(t, twoWithEvidence, cfg("strict_tdd_policy: require\n"+evidenceAB), true), true, "policy", "invalid")
	expect(t, "override", resolveTree(t, twoUncovered, cfg("strict_tdd_policy: require\n"+overrideC), true), true, "policy", "ok")
	for _, files := range []map[string]string{single, twoUncovered, twoWithEvidence} {
		expect(t, "forbid", resolveTree(t, files, cfg("strict_tdd_policy: forbid  # team decision\n"+evidenceAB), true), false, "policy", "n/a")
	}
	expect(t, "derive true", resolveTree(t, single, cfg("schema: spec-driven\n"), true), true, "derived", "ok")
	expect(t, "derive false", resolveTree(t, twoUncovered, cfg("schema: spec-driven\n"), true), false, "derived", "n/a")
	if r := resolveTree(t, twoWithEvidence, cfg("schema: spec-driven\n"+evidenceAB), true); r.Value || r.Source != "derived" {
		t.Errorf("evidence derives: %+v", r)
	}
	expect(t, "override derives", resolveTree(t, twoUncovered, cfg("schema: spec-driven\n"+overrideC), true), true, "derived", "ok")

	for _, c := range []struct {
		config   string
		value    bool
		evidence string
	}{
		{"strict_tdd: true\n", true, "ok"},
		{"testing:\n  strict_tdd: true\n", true, "ok"},
		{"strict_tdd: false\n", false, "n/a"},
		{"testing:\n  strict_tdd: false\n", false, "n/a"},
	} {
		r := resolveTree(t, single, cfg(c.config), true)
		expect(t, c.config, r, c.value, "legacy", c.evidence)
		if len(r.Notes) != 1 {
			t.Fatalf("%q: notes %#v", c.config, r.Notes)
		}
		matches(t, c.config, r.Notes[0], legacyNote)
	}
	r = resolveTree(t, twoUncovered, cfg("testing:\n  strict_tdd: true\n"), true)
	expect(t, "legacy true blocks", r, true, "legacy", "invalid")
	matches(t, "legacy true blocks", strings.Join(r.Notes, "\n"), legacyNote)

	for _, config := range []string{"strict_tdd_policy: forbid\nstrict_tdd: true\n", "strict_tdd_policy: forbid\ntesting:\n  strict_tdd: true\n"} {
		r := resolveTree(t, single, cfg(config), true)
		expect(t, config, r, false, "policy", "n/a")
		if len(r.Notes) != 1 {
			t.Fatalf("%q: notes %#v", config, r.Notes)
		}
		matches(t, config, r.Notes[0], `ignored`)
		matches(t, config, r.Notes[0], `remove`)
	}
	r = resolveTree(t, twoUncovered, cfg("strict_tdd_policy: require\nstrict_tdd: false\n"), true)
	expect(t, "require beside legacy", r, true, "policy", "invalid")
	matches(t, "require beside legacy", strings.Join(r.Notes, "\n"), `ignored`)

	r = resolveTree(t, single, cfg("strict_tdd_policy: yes\n"), true)
	expect(t, "unreadable policy", r, true, "policy", "invalid")
	matches(t, "unreadable policy", r.Reason, `strict_tdd_policy: 'yes', which is neither require nor forbid`)
	expect(t, "quoted policy", resolveTree(t, single, cfg("strict_tdd_policy: \"forbid\" # q\n"), true), false, "policy", "n/a")
}

func TestDerivedIsDynamic(t *testing.T) {
	jhonRoot(t)
	config := "schema: spec-driven\n"
	root := tree(t, map[string]string{"pyproject.toml": "[tool.pytest.ini_options]\n", "openspec/config.yaml": config})
	listing := func() []string {
		entries, _ := os.ReadDir(filepath.Join(root, "openspec"))
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return names
	}
	before := listing()
	first, err := StrictTddFor(root)
	if err != nil || !first.Value || first.Source != "derived" {
		t.Fatalf("first: %+v, %v", first, err)
	}
	os.Remove(filepath.Join(root, "pyproject.toml"))
	for rel, text := range twoUncovered {
		write(t, filepath.Join(root, rel), text)
	}
	second, err := StrictTddFor(root)
	if err != nil || second.Value || second.Source != "derived" {
		t.Errorf("second: %+v, %v", second, err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "openspec", "config.yaml")); string(b) != config {
		t.Error("the config was rewritten")
	}
	if !slices.Equal(listing(), before) {
		t.Error("something derived was persisted under openspec/")
	}

	// A legacy false written before any code stays false once pytest arrives.
	root = tree(t, map[string]string{"openspec/config.yaml": "schema: spec-driven\n\nstrict_tdd: false\n"})
	write(t, filepath.Join(root, "pyproject.toml"), "[project]\nname = \"app\"\n\n[tool.pytest.ini_options]\ntestpaths = [\"tests\"]\n")
	if c := discover.Discover(root).Coverage.Covered; c != "yes" {
		t.Fatalf("precondition: coverage %s, want yes", c)
	}
	r, err := StrictTddFor(root)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, "regression J", r, false, "legacy", "n/a")
	matches(t, "regression J", strings.Join(r.Notes, "\n"), legacyNote)
}

func TestExemptAndUnknownProjects(t *testing.T) {
	jhonRoot(t)
	member := func(name string) string {
		return "[project]\nname = \"" + name + "\"\ndependencies = [\"pytest\"]\n\n[tool.pytest.ini_options]\n"
	}
	uv := map[string]string{
		"pyproject.toml":         "[project]\nname = \"root\"\n\n[tool.uv.workspace]\nmembers = [\"libs/*\"]\n",
		"uv.lock":                "version = 1\n",
		"Makefile":               "test:\n\tcd libs/a && uv run pytest\n\tcd libs/b && uv run pytest\n",
		"libs/a/pyproject.toml":  member("a"),
		"libs/a/tests/test_a.py": "def test_a():\n    assert True\n",
		"libs/b/pyproject.toml":  member("b"),
		"libs/b/tests/test_b.py": "def test_b():\n    assert True\n",
	}
	traces := "strict_tdd_evidence:\n  libs/a: Makefile:2\n  libs/b: Makefile:3\n"
	r := resolveTree(t, uv, cfg("strict_tdd_policy: require\n"+traces), true)
	expect(t, "L1", r, true, "policy", "ok")
	matches(t, "L1", r.Reason, `structurally_exempt`)
	expect(t, "L1 missing trace", resolveTree(t, uv, cfg("strict_tdd_policy: require\nstrict_tdd_evidence:\n  libs/a: Makefile:2\n"), true), true, "policy", "ok")
	expect(t, "L1 extra trace", resolveTree(t, uv, cfg("strict_tdd_policy: require\n"+traces+"  \".\": Makefile:1\n"), true), true, "policy", "ok")
	r = resolveTree(t, with(uv, map[string]string{"tool.py": "print('x')\n"}), cfg("strict_tdd_policy: require\n"+traces), true)
	expect(t, "L1 unknown root", r, true, "policy", "invalid")
	matches(t, "L1 unknown root", r.Reason, `unknown: \.`)

	acmeMember := func(name string) string {
		return "[project]\nname = \"" + name + "\"\n\n[project.optional-dependencies]\ndev = [\"pytest\"]\n\n[tool.pytest.ini_options]\n"
	}
	acme := map[string]string{
		"pyproject.toml": "[project]\nname = \"acme\"\n\n[tool.uv.workspace]\nmembers = [\"svc/*\"]\n", "uv.lock": "version = 1\n",
		"svc/a/pyproject.toml": acmeMember("a"), "svc/a/tests/test_a.py": "def test_a():\n    assert True\n",
		"svc/b/pyproject.toml": acmeMember("b"), "svc/b/tests/test_b.py": "def test_b():\n    assert True\n",
		"svc/c/pyproject.toml": acmeMember("c"), "svc/c/tests/test_c.py": "def test_c():\n    assert True\n",
	}
	oneUnknown := with(acme, map[string]string{"svc/c/pyproject.toml": "[project]\nname = \"c\"\n", "svc/c/tests/test_c.py": "", "svc/c/c.py": "print('c')\n"})
	d := discover.Discover(tree(t, acme))
	var states [][2]string
	for _, p := range d.Projects {
		states = append(states, [2]string{p.Path, p.Testability})
	}
	if want := [][2]string{{".", "structurally_exempt"}, {"svc/a", "testable"}, {"svc/b", "testable"}, {"svc/c", "testable"}}; !reflect.DeepEqual(states, want) || d.Coverage.Covered != "unknown" {
		t.Errorf("acme precondition: %q, coverage %s", states, d.Coverage.Covered)
	}
	r = resolveTree(t, acme, cfg("strict_tdd_policy: require\n"), true)
	expect(t, "acme", r, true, "policy", "ok")
	if regexp.MustCompile(`strict_tdd_evidence|coverage`).MatchString(r.Reason) {
		t.Errorf("acme reason %q", r.Reason)
	}
	expect(t, "acme derived", resolveTree(t, acme, cfg("schema: spec-driven\n"), true), true, "derived", "ok")
	r = resolveTree(t, oneUnknown, cfg("strict_tdd_policy: require\n"), true)
	expect(t, "one unknown", r, true, "policy", "invalid")
	matches(t, "one unknown", r.Reason, `unknown: svc/c`)
	expect(t, "override", resolveTree(t, oneUnknown, cfg("strict_tdd_policy: require\ntest_command_override:\n  svc/c: uv run --with pytest pytest\n"), true), true, "policy", "ok")
	makefile := "t:\n\tcd svc/a && pytest\n\tcd svc/b && pytest\n\tcd svc/c && pytest\n"
	r = resolveTree(t, with(oneUnknown, map[string]string{"Makefile": makefile}),
		cfg("strict_tdd_policy: require\nstrict_tdd_evidence:\n  svc/a: Makefile:2\n  svc/b: Makefile:3\n  svc/c: Makefile:4\n"), true)
	expect(t, "evidence ignored", r, true, "policy", "invalid")
	if !reflect.DeepEqual(r.Notes, []string{legacyEvidenceNote}) {
		t.Errorf("notes %#v", r.Notes)
	}
	r = resolveTree(t, with(acme, map[string]string{"Makefile": makefile}), cfg("strict_tdd_policy: require\nstrict_tdd_evidence:\n  svc/a: Makefile:2\n"), true)
	expect(t, "incomplete block", r, true, "policy", "ok")
	if !reflect.DeepEqual(r.Notes, []string{legacyEvidenceNote}) {
		t.Errorf("notes %#v", r.Notes)
	}
	expect(t, "zero testable", resolveTree(t, map[string]string{"pyproject.toml": "[project]\nname = \"docs\"\n"}, cfg("strict_tdd_policy: require\n"), true), true, "policy", "invalid")
}
