package discover

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	root, _ := filepath.Abs("../../..")
	os.Setenv("JHON_ROOT", root)
	os.Exit(m.Run())
}

// tree is a temporary directory holding files, by relative path.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func with(base map[string]string, extra map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func pkg(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func paths(d *Discovery) []string {
	var out []string
	for _, p := range d.Projects {
		out = append(out, p.Path)
	}
	return out
}

func byPath(d *Discovery) map[string]*Project {
	out := map[string]*Project{}
	for _, p := range d.Projects {
		out[p.Path] = p
	}
	return out
}

func deref(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func expectMatch(t *testing.T, what, s, pattern string) {
	t.Helper()
	if !regexp.MustCompile(pattern).MatchString(s) {
		t.Errorf("%s = %q, want a match for %s", what, s, pattern)
	}
}

// ── the bounded scan (no declared membership) ───────────────────────────────

var scanned = map[string]string{
	"package.json":                  pkg(map[string]any{"name": "root", "scripts": map[string]string{"test": "node --test"}}),
	"A/pyproject.toml":              "[project]\nname = \"a\"\n\n[tool.pytest.ini_options]\naddopts = \"-q\"\n",
	"B/Cargo.toml":                  "[package]\nname = \"b\"\n",
	"x/y/go.mod":                    "module example.com/y\n",
	"x/y/z/go.mod":                  "module example.com/z\n",
	"C/requirements.txt":            "requests\n",
	"D/pyproject.toml":              "[project]\nname = \"d\"\n",
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
	"nested/.git":                   "gitdir: /elsewhere\n",
	"nested/package.json":           "{}",
	"E/sub/.git/HEAD":               "ref: refs/heads/main\n",
	"E/sub/package.json":            "{}",
}

func TestScan(t *testing.T) {
	d := Discover(tree(t, scanned))
	if d.Membership != "scanned" || !reflect.DeepEqual(paths(d), []string{".", "A", "B", "D", "x/y"}) {
		t.Fatalf("scanned %s %q: the root and two levels below, never an excluded, generated, dependency, or nested-repository directory", d.Membership, paths(d))
	}
	p := byPath(d)
	for path, want := range map[string][2]any{
		".": {"Node", "npm test"}, "A": {"Python", "python -m pytest"}, "B": {"Rust", "cargo test"},
		"x/y": {"Go", "go test ./..."}, "D": {"Python", nil},
	} {
		if got := [2]any{strings.Join(p[path].Stacks, "+"), deref(p[path].TestCommand)}; got != want {
			t.Errorf("%s: stack and command %v, want %v (read from explicit files only)", path, got, want)
		}
	}
}

// An Nx workspace (nx.json, nx a dependency) with no test script runs its
// projects' test targets through Nx.
func TestNxWorkspaceTestCommand(t *testing.T) {
	nx := pkg(map[string]any{"devDependencies": map[string]string{"nx": "22.6.5"}})
	r := Discover(tree(t, map[string]string{"package.json": nx, "nx.json": "{}\n"}))
	if got := deref(r.Projects[0].TestCommand); got != "npx --no-install nx run-many -t test" {
		t.Errorf("an Nx workspace: %v", got)
	}
	if r := Discover(tree(t, map[string]string{"package.json": nx})); r.Projects[0].TestCommand != nil {
		t.Error("nx in the dependencies without an nx.json is no Nx workspace")
	}
	if r := Discover(tree(t, map[string]string{"package.json": "{}", "nx.json": "{}\n"})); r.Projects[0].TestCommand != nil {
		t.Error("an nx.json without nx installed runs nothing")
	}
	scripted := pkg(map[string]any{"scripts": map[string]string{"test": "vitest run"}, "devDependencies": map[string]string{"nx": "22.6.5"}})
	if r := Discover(tree(t, map[string]string{"package.json": scripted, "nx.json": "{}\n"})); deref(r.Projects[0].TestCommand) != "npm test" {
		t.Error("a test script the workspace states wins")
	}
}

func TestProjectShape(t *testing.T) {
	r := Discover(tree(t, map[string]string{"package.json": pkg(map[string]any{"scripts": map[string]string{"test": `echo "Error: no test specified" && exit 1`}})}))
	if r.Projects[0].TestCommand != nil {
		t.Error("npm's placeholder test script is not a test command")
	}
	r = Discover(tree(t, map[string]string{"package.json": "{}", "pyproject.toml": "[project]\n"}))
	if len(r.Projects) != 1 || !reflect.DeepEqual(r.Projects[0].Stacks, []string{"Node", "Python"}) {
		t.Errorf("several markers are one project with several stacks: %+v", r.Projects)
	}
	if r := Discover(tree(t, map[string]string{"README.md": "hello\n"})); len(r.Projects) != 0 || r.Projects == nil {
		t.Errorf("zero projects is an empty list: %#v", r.Projects)
	}
}

// ── Python: the command must run under the declaring manager ────────────────

const pytestConfig = "\n[tool.pytest.ini_options]\ntestpaths = [\"tests\"]\n"

func py(t *testing.T, files map[string]string) *Project { return byPath(Discover(tree(t, files)))["."] }

func uvGroup(group string) map[string]string {
	return map[string]string{
		"pyproject.toml": "[project]\nname = \"a\"\n\n[tool.uv]\nmanaged = true\n\n[dependency-groups]\n" + group + " = [\n  # pinned for CI\n  \"pytest ; python_version >= '3.10'\",\n  { include-group = \"lint\" },\n]\nlint = [\"ruff\"]\n" + pytestConfig,
	}
}

func TestPythonCommand(t *testing.T) {
	cases := []struct {
		name    string
		files   map[string]string
		command any
		source  string
	}{
		{"uv: pytest in [project].dependencies", map[string]string{
			"pyproject.toml": "[project]\nname = \"a\"\ndependencies = [\"pytest>=8\", \"httpx\"]\n" + pytestConfig, "uv.lock": "version = 1\n",
		}, "uv run pytest", `\[project\]\.dependencies.*uv`},
		{"uv: pytest in exactly one extra", map[string]string{
			"pyproject.toml": "[project]\nname = \"a\"\n\n[project.optional-dependencies]\ndev = [\"pytest>=8.3.0\", \"pytest-cov>=6\", \"ruff\"]\nkafka = [\"aiokafka\"]\n" + pytestConfig,
			"uv.lock":        "version = 1\n",
		}, "uv run --extra dev pytest", `\[project\.optional-dependencies\]\.dev`},
		{"uv: pytest in exactly one group", uvGroup("test"), "uv run --group test pytest", `\[dependency-groups\]\.test`},
		{"uv: pytest in exactly one group, named even when dev", uvGroup("dev"), "uv run --group dev pytest", `\[dependency-groups\]\.dev`},
		{"uv: pytest in more than one extra or group", map[string]string{
			"pyproject.toml": "[project]\nname = \"a\"\n\n[project.optional-dependencies]\ndev = [\"pytest\"]\ntest = [\"pytest[testing]>=8\"]\n" + pytestConfig,
			"uv.lock":        "version = 1\n",
		}, nil, `more than one.*\.dev.*\.test`},
		{"uv: pytest mentioned only in text or comments", map[string]string{
			"pyproject.toml": "[project]\nname = \"a\"\ndescription = \"tested with pytest\"\ndependencies = [\n  # \"pytest\" comes from the CI image\n  \"httpx\",\n]\n\n[tool.notes]\ntext = \"\"\"\ndev = [\"pytest\"]\n\"\"\"\n" + pytestConfig,
			"uv.lock":        "version = 1\n",
		}, nil, `not declared`},
		{"uv: pytest plugins are not pytest", map[string]string{
			"pyproject.toml": "[project]\nname = \"a\"\n\n[project.optional-dependencies]\ndev = [\"pytest-cov>=6\", \"pytest_asyncio\"]\n" + pytestConfig,
			"uv.lock":        "version = 1\n",
		}, nil, `not declared`},
		{"poetry: pytest in a non-optional group", map[string]string{
			"pyproject.toml": "[tool.poetry]\nname = \"a\"\n\n[tool.poetry.dependencies]\npython = \"^3.12\"\n\n[tool.poetry.group.dev.dependencies]\npytest = \"^8.0\"\npytest-cov = { version = \"^6\" }\n" + pytestConfig,
			"poetry.lock":    "# lock\n",
		}, "poetry run pytest", `\[tool\.poetry\.group\.dev\.dependencies\].*poetry`},
		{"poetry: pytest only in an optional group", map[string]string{
			"pyproject.toml": "[tool.poetry]\nname = \"a\"\n\n[tool.poetry.group.test]\noptional = true\n\n[tool.poetry.group.test.dependencies]\npytest = \"^8.0\"\n" + pytestConfig,
		}, nil, `optional`},
		{"neither uv nor poetry", map[string]string{
			"pyproject.toml": "[project]\nname = \"a\"\n\n[project.optional-dependencies]\ndev = [\"pytest\"]\n" + pytestConfig,
		}, "python -m pytest", `^pyproject\.toml pytest configuration$`},
		{"both managers", map[string]string{
			"pyproject.toml": "[tool.poetry]\nname = \"a\"\n" + pytestConfig, "uv.lock": "",
		}, nil, `both uv \(uv\.lock\) and poetry \(\[tool\.poetry\]\)`},
		{"invalid TOML", map[string]string{
			"pyproject.toml": "a = 1\na = 2\n" + pytestConfig, "uv.lock": "",
		}, nil, `not valid TOML \(line 2: duplicate key 'a'\)`},
	}
	for _, c := range cases {
		p := py(t, c.files)
		if deref(p.TestCommand) != c.command {
			t.Errorf("%s: command %v, want %v", c.name, deref(p.TestCommand), c.command)
		}
		expectMatch(t, c.name+": source", deref(p.TestCommandSource).(string), c.source)
	}
}

func TestUvWorkspaceMember(t *testing.T) {
	r := byPath(Discover(tree(t, map[string]string{
		"pyproject.toml":        "[project]\nname = \"root\"\n\n[tool.uv.workspace]\nmembers = [\"libs/*\"]\n",
		"uv.lock":               "version = 1\n",
		"libs/a/pyproject.toml": "[project]\nname = \"a\"\n\n[project.optional-dependencies]\ndev = [\n  \"pytest>=8.3.0\",\n  \"pytest-asyncio\",\n]\n" + pytestConfig,
		"libs/b/pyproject.toml": "[project]\nname = \"b\"\n",
	})))
	if deref(r["libs/a"].TestCommand) != "uv run --extra dev pytest" || r["."].TestCommand != nil || r["libs/b"].TestCommand != nil {
		t.Errorf("a member takes the manager from the root: %v %v %v", deref(r["libs/a"].TestCommand), deref(r["."].TestCommand), deref(r["libs/b"].TestCommand))
	}
	expectMatch(t, "libs/a source", *r["libs/a"].TestCommandSource, `workspace root`)
}

// ── declared membership ─────────────────────────────────────────────────────

var uvWorkspace = map[string]string{
	"pyproject.toml": "[project]\nname = \"root\"\n\n[tool.uv]\nmanaged = true\n\n" +
		"[tool.uv.workspace]\nmembers = [\n  \"libs/*\",\n  \"apps/deep/nested/svc\",\n]\nexclude = [\"libs/skip\"]\n\n[tool.ruff]\nline-length = 88\n",
	"libs/a/pyproject.toml":               "[project]\nname = \"a\"\n",
	"libs/b/pyproject.toml":               "[project]\nname = \"b\"\n",
	"libs/skip/pyproject.toml":            "[project]\nname = \"skip\"\n",
	"apps/deep/nested/svc/pyproject.toml": "[project]\nname = \"svc\"\n",
	"stray/pyproject.toml":                "[project]\nname = \"stray\"\n",
}

var jsWorkspaces = map[string]string{
	"package.json":            pkg(map[string]any{"private": true, "workspaces": []string{"packages/*", "tools/cli"}, "scripts": map[string]string{"test": "npm -ws test"}}),
	"packages/a/package.json": pkg(map[string]any{"scripts": map[string]string{"test": "vitest run"}}),
	"packages/b/package.json": pkg(map[string]any{"scripts": map[string]string{"test": `echo "Error: no test specified" && exit 1`}}),
	"tools/cli/package.json":  pkg(map[string]string{"name": "cli"}),
	"other/pyproject.toml":    "[tool.pytest.ini_options]\n",
}

func TestDeclaredMembership(t *testing.T) {
	cases := []struct {
		name     string
		files    map[string]string
		declared []string
		paths    []string
	}{
		{"package.json workspaces", jsWorkspaces, []string{"package.json workspaces"}, []string{".", "packages/a", "packages/b", "tools/cli"}},
		{"the object form of workspaces", map[string]string{
			"package.json": pkg(map[string]any{"workspaces": map[string]any{"packages": []string{"apps/*"}}}), "apps/web/package.json": "{}",
		}, []string{"package.json workspaces"}, []string{".", "apps/web"}},
		{"pnpm, with negations", map[string]string{
			"package.json": "{}", "pnpm-workspace.yaml": "packages:\n  - 'packages/*'\n  - \"!packages/skip\"\n",
			"packages/keep/package.json": "{}", "packages/skip/package.json": "{}", "stray/package.json": "{}",
		}, []string{"pnpm-workspace.yaml packages"}, []string{".", "packages/keep"}},
		{"Cargo, minus exclude", map[string]string{
			"Cargo.toml":             "[workspace]\nmembers = [\n  \"crates/*\",\n]\nexclude = [\"crates/old\"]\n",
			"crates/core/Cargo.toml": "[package]\nname = \"core\"\n", "crates/old/Cargo.toml": "[package]\nname = \"old\"\n", "scratch/Cargo.toml": "[package]\n",
		}, []string{"Cargo.toml [workspace] members"}, []string{".", "crates/core"}},
		{"uv, minus exclude, at any depth", uvWorkspace,
			[]string{"pyproject.toml [tool.uv.workspace] members"}, []string{".", "apps/deep/nested/svc", "libs/a", "libs/b"}},
		{"uv, single-quoted, commented quotes ignored", with(uvWorkspace, map[string]string{
			"pyproject.toml": "[project]\nname = \"root\"\n\n[tool.uv.workspace]\nmembers = [\n  'libs/*',  # all \"libs\" packages\n  \"apps/deep/nested/svc\",\n  # 'stray',\n]\nexclude = ['libs/skip']\n",
		}), []string{"pyproject.toml [tool.uv.workspace] members"}, []string{".", "apps/deep/nested/svc", "libs/a", "libs/b"}},
		{"no [tool.uv.workspace]: the bounded scan", map[string]string{
			"pyproject.toml": "[project]\nname = \"root\"\n\n[tool.uv]\nmanaged = true\n", "libs/a/pyproject.toml": "[project]\n", "apps/deep/nested/svc/pyproject.toml": "[project]\n",
		}, []string{}, []string{".", "libs/a"}},
		{"inside a nested repository or an excluded directory", map[string]string{
			"package.json":              pkg(map[string]any{"workspaces": []string{"packages/*", "node_modules/x"}}),
			"packages/own/package.json": "{}", "packages/sub/.git": "gitdir: /elsewhere\n", "packages/sub/package.json": "{}", "node_modules/x/package.json": "{}",
		}, []string{"package.json workspaces"}, []string{".", "packages/own"}},
	}
	for _, c := range cases {
		d := Discover(tree(t, c.files))
		membership := "declared"
		if len(c.declared) == 0 {
			membership = "scanned"
		}
		if d.Membership != membership || !reflect.DeepEqual(d.DeclaredBy, c.declared) || !reflect.DeepEqual(paths(d), c.paths) {
			t.Errorf("%s: %s, declared by %q, projects %q; want %s, %q, %q", c.name, d.Membership, d.DeclaredBy, paths(d), membership, c.declared, c.paths)
		}
	}
	p := byPath(Discover(tree(t, jsWorkspaces)))
	if got := [3]any{deref(p["packages/a"].TestCommand), deref(p["packages/b"].TestCommand), deref(p["tools/cli"].TestCommand)}; got != [3]any{"npm test", nil, nil} {
		t.Errorf("workspace member commands %v", got)
	}
	if d := Discover(tree(t, uvWorkspace)); d.Membership != "declared" || !reflect.DeepEqual(d.Projects[1].Stacks, []string{"Python"}) {
		t.Errorf("uv membership %s %v", d.Membership, d.Projects[1].Stacks)
	}
}

func TestDeclaredMemberNotes(t *testing.T) {
	d := Discover(tree(t, map[string]string{
		"package.json":   pkg(map[string]any{"workspaces": []string{"../elsewhere", "docs", "a/**", "missing"}}),
		"docs/README.md": "no marker\n",
	}))
	if !reflect.DeepEqual(paths(d), []string{"."}) {
		t.Errorf("projects %q", paths(d))
	}
	want := []string{
		"declared member '../elsewhere' resolves outside the workspace root; not inspected",
		"declared member 'docs' has no supported project marker",
		"declared member 'a/**' uses '**', an unbounded pattern; not expanded",
		"declared member 'missing' does not exist",
	}
	if !reflect.DeepEqual(d.Notes, want) {
		t.Errorf("notes %q", d.Notes)
	}
	d = Discover(tree(t, map[string]string{
		"pyproject.toml":        "[tool.uv.workspace]\nmembers = [\"../elsewhere\", \"libs/a\"]\n",
		"libs/a/pyproject.toml": "[project]\n",
	}))
	if !reflect.DeepEqual(paths(d), []string{".", "libs/a"}) {
		t.Errorf("uv projects %q", paths(d))
	}
	expectMatch(t, "uv notes", strings.Join(d.Notes, "\n"), `\.\./elsewhere.*outside the workspace root`)
}

func TestOpenspecConfig(t *testing.T) {
	if Discover(tree(t, map[string]string{"package.json": "{}"})).OpenspecConfig.Exists {
		t.Error("absent config reported as existing")
	}
	d := Discover(tree(t, map[string]string{"package.json": "{}", "openspec/config.yaml": "schema: spec-driven\n"}))
	if d.OpenspecConfig != (OpenspecConfig{"openspec/config.yaml", true}) {
		t.Errorf("openspec_config = %+v", d.OpenspecConfig)
	}
}

// ── coverage ─────────────────────────────────────────────────────────────────

func cov(t *testing.T, files map[string]string) Coverage { return Discover(tree(t, files)).Coverage }

func TestCoverage(t *testing.T) {
	c := cov(t, map[string]string{"package.json": pkg(map[string]any{"scripts": map[string]string{"test": "vitest run"}})})
	if c.Covered != "yes" || deref(c.Command) != "npm test" {
		t.Errorf("a single project with a test command: %+v", c)
	}
	c = cov(t, map[string]string{"svc/go.mod": "module example.com/svc\n"})
	if c.Covered != "yes" || deref(c.Command) != "go test ./..." || !strings.Contains(c.Reason, "svc/") {
		t.Errorf("exactly one go.mod: %+v", c)
	}
	c = cov(t, map[string]string{
		"package.json":        pkg(map[string]any{"private": true, "scripts": map[string]string{"test": "pnpm -r test"}}),
		"pnpm-workspace.yaml": "packages:\n  - 'packages/*'\n", "packages/a/package.json": pkg(map[string]any{"scripts": map[string]string{"test": "vitest run"}}), "packages/b/package.json": "{}",
	})
	if c.Covered != "yes" || deref(c.Command) != "npm test" || !strings.Contains(c.Reason, "pnpm -r test") {
		t.Errorf("a root script over every pnpm member: %+v", c)
	}
	for _, script := range []string{"npm test --workspaces", "npm -ws test", "turbo run test", "nx run-many -t test", "pnpm --recursive test"} {
		c := cov(t, map[string]string{"package.json": pkg(map[string]any{"workspaces": []string{"packages/*"}, "scripts": map[string]string{"test": script}}), "packages/a/package.json": "{}"})
		if c.Covered != "yes" {
			t.Errorf("%s: %+v", script, c)
		}
	}
	c = cov(t, map[string]string{"Cargo.toml": "[workspace]\nmembers = [\"crates/*\"]\n", "crates/core/Cargo.toml": "[package]\n", "crates/cli/Cargo.toml": "[package]\n"})
	if c.Covered != "yes" || deref(c.Command) != "cargo test" {
		t.Errorf("a Cargo virtual workspace: %+v", c)
	}
	for name, files := range map[string]map[string]string{
		"a Cargo root that is also a package": {"Cargo.toml": "[package]\nname = \"root\"\n\n[workspace]\nmembers = [\"crates/*\"]\n", "crates/core/Cargo.toml": "[package]\n"},
		"a project outside the JS workspace": {
			"package.json": pkg(map[string]any{"workspaces": []string{"packages/*"}, "scripts": map[string]string{"test": "npm -ws test"}}), "packages/a/package.json": "{}",
			"Cargo.toml": "[workspace]\nmembers = [\"crates/*\"]\n", "crates/core/Cargo.toml": "[package]\n",
		},
		"a Makefile target": {
			"Makefile": "test-all:\n\tcd api && pytest\n", "api/pyproject.toml": "[project]\nname = \"api\"\n\n[tool.pytest.ini_options]\naddopts = \"-q\"\n",
			"frontend/package.json": pkg(map[string]any{"scripts": map[string]string{"test": "vitest run"}}), "worker/go.mod": "module example.com/worker\n",
		},
		"an undetermined command": {"pyproject.toml": "[project]\nname = \"a\"\n\n[project.optional-dependencies]\ndev = [\"pytest\"]\ntest = [\"pytest\"]\n\n[tool.pytest.ini_options]\n", "uv.lock": ""},
		"a uv workspace":          with(uvWorkspace, map[string]string{"libs/a/pyproject.toml": "[project]\nname = \"a\"\ndependencies = [\"pytest\"]\n\n[tool.pytest.ini_options]\n"}),
	} {
		if c := cov(t, files); c.Covered != "unknown" || c.Command != nil {
			t.Errorf("%s: %+v", name, c)
		}
	}
	c = cov(t, map[string]string{"A/pyproject.toml": "[tool.pytest.ini_options]\n", "B/Cargo.toml": "[package]\n"})
	if c.Covered != "unknown" || c.Command != nil {
		t.Errorf("two unrelated projects: %+v", c)
	}
	expectMatch(t, "two unrelated projects", c.Reason, `^2 projects`)
	if c := cov(t, map[string]string{"README.md": "hello\n"}); c.Covered != "no" || !strings.Contains(c.Reason, "no project") {
		t.Errorf("zero projects: %+v", c)
	}
	if c := cov(t, map[string]string{"A/pyproject.toml": "[project]\n", "B/package.json": "{}"}); c.Covered != "no" || c.Command != nil {
		t.Errorf("no project has any test command: %+v", c)
	}
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

func TestTestability(t *testing.T) {
	p := byPath(Discover(tree(t, uvExemptRoot)))
	if p["."].Testability != Exempt() || p["libs/a"].Testability != Testable() || p["libs/b"].Testability != Testable() {
		t.Errorf("a uv workspace root with nothing outside its members: %s (%s)", p["."].Testability, p["."].TestabilityReason)
	}
	expectMatch(t, "exempt reason", p["."].TestabilityReason, `, outside libs/a, libs/b$`)
	p = byPath(Discover(tree(t, with(uvExemptRoot, map[string]string{"tool.py": "print('x')\n"}))))
	if p["."].Testability != Unknown() || p["."].TestabilityReason != "no test command, but tool.py is Python source (*.py)" {
		t.Errorf("a stray source file: %s (%s)", p["."].Testability, p["."].TestabilityReason)
	}
	p = byPath(Discover(tree(t, with(uvExemptRoot, map[string]string{"tests/data.json": "{}\n"}))))
	if p["."].Testability != Unknown() || p["."].TestabilityReason != "no test command, but tests/ is a test directory by convention (tests/)" {
		t.Errorf("a tests/ directory: %s (%s)", p["."].Testability, p["."].TestabilityReason)
	}
	if p := py(t, map[string]string{"pyproject.toml": "[project]\nname = \"a\"\n\n[project.optional-dependencies]\ndev = [\"pytest\"]\ntest = [\"pytest\"]\n\n[tool.pytest.ini_options]\n", "uv.lock": ""}); p.Testability != Unknown() {
		t.Errorf("a null command with a reason is unknown: %s", p.Testability)
	}
	if p := py(t, map[string]string{"package.json": "{}", "src/__tests__/fixture.json": "{}\n"}); p.Testability != Unknown() {
		t.Errorf("a Node test directory by convention: %s", p.Testability)
	}
	if p := py(t, map[string]string{"package.json": "{}", "docs/guide.md": "x\n"}); p.Testability != Exempt() {
		t.Errorf("a bare package.json: %s (%s)", p.Testability, p.TestabilityReason)
	}
	p = byPath(Discover(tree(t, map[string]string{"pyproject.toml": "[project]\nname = \"root\"\n", "pkg/pyproject.toml": "[project]\n", "pkg/mod.py": ""})))
	if p["."].Testability != Exempt() || p["pkg"].Testability != Unknown() {
		t.Errorf("source inside a nested project is that project's: %s, %s", p["."].Testability, p["pkg"].Testability)
	}
}

func TestEnclosingProject(t *testing.T) {
	if got := EnclosingProject(tree(t, map[string]string{"notes.txt": "x"})); got != "" {
		t.Errorf("a scratch folder is no project: %q", got)
	}
	root := tree(t, map[string]string{"pyproject.toml": ""})
	if got := EnclosingProject(root); got != root {
		t.Errorf("a manifest directory: %q", got)
	}
	root = tree(t, map[string]string{"openspec/config.yaml": "", "a/b/x": ""})
	if got := EnclosingProject(filepath.Join(root, "a", "b")); got != root {
		t.Errorf("an ancestor holding openspec/: %q", got)
	}
	if got := FindRoot(filepath.Join(root, "a", "b")); got != root {
		t.Errorf("FindRoot = %q", got)
	}
	if plain := tree(t, nil); FindRoot(plain) != plain {
		t.Error("FindRoot without openspec/ is the directory itself")
	}
}
