package discover

import (
	"errors"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"path/filepath"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/toml"
)

// inferred is a test command, nil when none is certain, and where it came
// from or why there is none.
type inferred struct {
	command *string
	source  string
}

// pytestConfigured are the files whose section configures pytest.
var pytestConfigured = []struct {
	file string
	re   *lazyre.Regexp
}{
	{"pyproject.toml", lazyre.New(`(?m)` + js.LineStart + `\[tool\.pytest(?:\.ini_options)?\]`)},
	{"pytest.ini", lazyre.New(`(?m)` + js.LineStart + `\[pytest\]`)},
	{"tox.ini", lazyre.New(`(?m)` + js.LineStart + `\[pytest\]`)},
	{"setup.cfg", lazyre.New(`(?m)` + js.LineStart + `\[tool:pytest\]`)},
}

// testCommand is the project's test command and where it came from, read
// from explicit files only; nil when no file states one.
func testCommand(dir, root string, markers []string) *inferred {
	for _, m := range markers {
		switch m {
		case "package.json":
			src, ok := read(filepath.Join(dir, m))
			if !ok {
				src = "{}"
			}
			pkg, ok := parseJSON(src)
			if !ok {
				continue
			}
			if pkg == nil {
				// pkg.scripts on a null document: a TypeError, as JavaScript reports it.
				panic(errors.New("TypeError: Cannot read properties of null (reading 'scripts')"))
			}
			// The script `npm init` writes when a package has no tests is not a test command.
			if s, ok := field(field(pkg, "scripts"), "test").(string); ok && js.Trim(s) != "" && !strings.Contains(s, "no test specified") {
				return &inferred{str("npm test"), "package.json scripts.test"}
			}
			// An Nx workspace keeps its tests in each project's test target.
			if nxInstalled(pkg) && project.Exists(filepath.Join(dir, "nx.json")) {
				return &inferred{str(NxTests), "nx.json, nx in package.json; every project's test target"}
			}
		case "go.mod":
			return &inferred{str("go test ./..."), "go.mod; the Go toolchain's own test command"}
		case "Cargo.toml":
			return &inferred{str("cargo test"), "Cargo.toml; Cargo's own test command"}
		case "pyproject.toml":
			// Python has no toolchain test command; pytest counts only when a file configures it.
			for _, c := range pytestConfigured {
				if src, _ := read(filepath.Join(dir, c.file)); c.re.MatchString(src) {
					r := pythonCommand(dir, root, c.file+" pytest configuration")
					return &r
				}
			}
		}
	}
	return nil
}

// NxTests is an Nx workspace's test command: every project's test target,
// through the workspace's own nx (npx installs nothing).
const NxTests = "npx --no-install nx run-many -t test"

// nxInstalled is whether package.json lists nx among its dependencies.
func nxInstalled(pkg any) bool {
	for _, k := range []string{"dependencies", "devDependencies"} {
		if _, ok := field(field(pkg, k), "nx").(string); ok {
			return true
		}
	}
	return false
}

// ── Python: the command must run where pytest is installed ──────────────────

// parsePyproject is the TOML of a directory's pyproject.toml; an unreadable file reads as empty.
func parsePyproject(dir string) (*toml.Table, error) {
	src, _ := read(filepath.Join(dir, "pyproject.toml"))
	return toml.Parse(src)
}

// pythonCommand is the pytest command for a project whose tests a file
// configures. The configuration says tests exist, not that pytest is
// installed: under uv or poetry an extra or an optional group is absent from
// the default environment, and `python -m pytest` would fail there. So when
// either manager is in evidence, the command is derived from where
// pyproject.toml declares pytest, read as TOML structure; when that does not
// determine one command, the command is nil and the source states why. With
// neither manager the earlier behaviour stands.
func pythonCommand(dir, root, config string) inferred {
	doc, err := parsePyproject(dir)
	if err != nil {
		// Unreadable structure means the manager and the declaration are unknown.
		return inferred{nil, fmt.Sprintf("%s, but pyproject.toml is not valid TOML (%s); no command is inferred", config, err.Error())}
	}
	managers := pythonManagers(dir, root, doc)
	if len(managers) == 0 {
		return inferred{str("python -m pytest"), config}
	}
	if len(managers) > 1 {
		return inferred{nil, fmt.Sprintf("%s, but both uv (%s) and poetry (%s) are in evidence; no command is inferred", config, managers[0].evidence, managers[1].evidence)}
	}
	m := managers[0]
	r := poetryRun(doc)
	if m.name == "uv" {
		r = uvRun(doc)
	}
	via := fmt.Sprintf("%s (%s)", m.name, m.evidence)
	if r.command != "" {
		return inferred{str(r.command), fmt.Sprintf("%s; pytest in pyproject.toml %s; %s", config, r.where, via)}
	}
	return inferred{nil, fmt.Sprintf("%s, but %s under %s; no command is inferred", config, r.why, via)}
}

type manager struct{ name, evidence string }

// pythonManagers are the package managers declarative files name for this
// project. A member takes uv from the discovery root only when that root
// declares `[tool.uv.workspace]`: a uv.lock alone does not make a nested
// directory part of its environment.
func pythonManagers(dir, root string, doc *toml.Table) []manager {
	var out []manager
	uv := ""
	if project.Exists(filepath.Join(dir, "uv.lock")) {
		uv = "uv.lock"
	} else if toml.TableAt(doc, "tool", "uv") != nil {
		uv = "[tool.uv]"
	}
	if uv == "" && project.Resolve("", dir) != project.Resolve("", root) {
		rootDoc, _ := parsePyproject(root) // a malformed root is no workspace evidence
		if toml.TableAt(rootDoc, "tool", "uv", "workspace") != nil {
			uv = "[tool.uv.workspace] at the workspace root"
			if project.Exists(filepath.Join(root, "uv.lock")) {
				uv = "uv.lock at the workspace root"
			}
		}
	}
	if uv != "" {
		out = append(out, manager{"uv", uv})
	}
	poetry := ""
	if project.Exists(filepath.Join(dir, "poetry.lock")) {
		poetry = "poetry.lock"
	} else if toml.TableAt(doc, "tool", "poetry") != nil {
		poetry = "[tool.poetry]"
	}
	if poetry != "" {
		out = append(out, manager{"poetry", poetry})
	}
	return out
}

var (
	separators = lazyre.New(`[-_.]+`)
	reqName    = lazyre.New(`^` + js.Space + `*([A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)`)
)

// normalise is PEP 503 normalisation, so `PyTest` and `pytest` compare equal and `pytest-cov` never does.
func normalise(name string) string { return separators.ReplaceAllString(strings.ToLower(name), "-") }

// isPytest is whether a PEP 508 requirement string names the package `pytest` itself (any version, extras, or markers).
func isPytest(req toml.Value) bool {
	s, ok := req.(string)
	if !ok {
		return false
	}
	m := reqName.FindStringSubmatch(s)
	return m != nil && normalise(m[1]) == "pytest"
}

func listsPytest(v toml.Value) bool {
	list, _ := v.([]toml.Value)
	for _, x := range list {
		if isPytest(x) {
			return true
		}
	}
	return false
}

// get is t[k], nil for a nil table.
func get(t *toml.Table, k string) toml.Value {
	if t == nil {
		return nil
	}
	v, _ := t.Get(k)
	return v
}

// namesListing are the names of the entries of a table of requirement lists (extras, dependency groups) that list pytest.
func namesListing(t *toml.Table) []string {
	var out []string
	if t == nil {
		return out
	}
	for _, k := range t.Keys() {
		if listsPytest(get(t, k)) {
			out = append(out, k)
		}
	}
	return out
}

// run is a run command and where pytest is declared, or, with no command, why there is none.
type run struct{ command, where, why string }

// uvRun: uv installs `[project].dependencies` always; an extra or a group
// only when named. Every group is named explicitly, `dev` included, because
// the default groups are configurable (`[tool.uv] default-groups`). pytest in
// several extras or groups leaves the choice open, so none is made.
func uvRun(doc *toml.Table) run {
	if listsPytest(get(toml.TableAt(doc, "project"), "dependencies")) {
		return run{command: "uv run pytest", where: "[project].dependencies"}
	}
	type hit struct{ flag, where string }
	var found []hit
	for _, e := range namesListing(toml.TableAt(doc, "project", "optional-dependencies")) {
		found = append(found, hit{"--extra " + e, "[project.optional-dependencies]." + e})
	}
	for _, g := range namesListing(toml.TableAt(doc, "dependency-groups")) {
		found = append(found, hit{"--group " + g, "[dependency-groups]." + g})
	}
	switch {
	case len(found) == 1:
		return run{command: "uv run " + found[0].flag + " pytest", where: found[0].where}
	case len(found) > 1:
		var where []string
		for _, f := range found {
			where = append(where, f.where)
		}
		return run{why: "pytest is declared in more than one extra or group (" + strings.Join(where, ", ") + ")"}
	}
	return run{why: "pytest is not declared in [project].dependencies, [project.optional-dependencies], or [dependency-groups]"}
}

// poetryEntry is how a poetry dependency table declares pytest: "required",
// "optional", or "" when it does not. A dependency is a key; its value is a
// version string, a table, or a list of tables (multiple constraints).
// `optional = true` makes it extra-only.
func poetryEntry(deps *toml.Table) string {
	if deps == nil {
		return ""
	}
	for _, k := range deps.Keys() {
		if normalise(k) != "pytest" {
			continue
		}
		specs, isList := get(deps, k).([]toml.Value)
		if !isList {
			specs = []toml.Value{get(deps, k)}
		}
		for _, s := range specs {
			if t, ok := toml.AsTable(s); !ok || get(t, "optional") != true {
				return "required"
			}
		}
		return "optional"
	}
	return ""
}

// poetryRun: poetry installs its main dependencies and every non-optional
// group by default, so `poetry run pytest` holds for any of them. An optional
// group or an extra needs `poetry install --with`/`--extras` first, which is
// not a run command, so that case has no command rather than a guess.
func poetryRun(doc *toml.Table) run {
	main := poetryEntry(toml.TableAt(doc, "tool", "poetry", "dependencies"))
	if main == "required" {
		return run{command: "poetry run pytest", where: "[tool.poetry.dependencies]"}
	}
	if listsPytest(get(toml.TableAt(doc, "project"), "dependencies")) {
		return run{command: "poetry run pytest", where: "[project].dependencies"}
	}

	groups := toml.TableAt(doc, "tool", "poetry", "group")
	if groups == nil {
		groups = toml.NewTable()
	}
	var inGroups []string
	for _, g := range groups.Keys() {
		if poetryEntry(toml.TableAt(groups, g, "dependencies")) == "required" {
			inGroups = append(inGroups, g)
		}
	}
	for _, g := range inGroups {
		if get(toml.TableAt(groups, g), "optional") != true {
			return run{command: "poetry run pytest", where: "[tool.poetry.group." + g + ".dependencies]"}
		}
	}

	var optional []string
	for _, g := range inGroups {
		optional = append(optional, "optional group [tool.poetry.group."+g+"]")
	}
	if main == "optional" {
		optional = append(optional, "[tool.poetry.dependencies] with optional = true")
	}
	for _, e := range namesListing(toml.TableAt(doc, "project", "optional-dependencies")) {
		optional = append(optional, "extra [project.optional-dependencies]."+e)
	}
	if len(optional) > 0 {
		return run{why: "pytest is declared only as optional (" + strings.Join(optional, ", ") + "); installing it needs poetry install --with or --extras, which is not a run command"}
	}
	return run{why: "pytest is not declared in [tool.poetry.dependencies], a [tool.poetry.group.<name>.dependencies] table, or [project].dependencies"}
}
