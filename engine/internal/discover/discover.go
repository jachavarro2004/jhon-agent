// Package discover is `jhon init`'s read-only project discovery and the
// resolved projects every consumer reads through it.
//
// Discovery reports the workspace root, the projects found (relative path,
// stack, and a test command read from explicit files), whether
// openspec/config.yaml exists, whether one command covers every project
// (informational), each project's testability, and the root's VCS state. It
// never creates or modifies a file: the LDD init phase is the one writer of
// openspec/config.yaml.
package discover

import (
	"encoding/json"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// A Marker is a project manifest and the stack it names.
type Marker struct{ File, Stack string }

// Markers are the supported project markers, in order (SKILL.md step 2).
var Markers = []Marker{
	{"package.json", "Node"},
	{"go.mod", "Go"},
	{"pyproject.toml", "Python"},
	{"Cargo.toml", "Rust"},
}

// excluded are the directories discovery never enters (rule 3). Nested
// repositories are checked separately.
var excluded = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true, "out": true,
	"target": true, ".cache": true, "__pycache__": true, ".venv": true, "venv": true,
}

// MaxDepth bounds the fallback scan: the root and at most two directory levels below it (rule 2).
const MaxDepth = 2

// A Project is one discovered project. TestCommand and TestCommandSource are
// nil when no file states them; when a pytest configuration exists but no
// command is certain, TestCommand is nil and TestCommandSource says why.
type Project struct {
	Path              string   `json:"path"`
	Stacks            []string `json:"stacks"`
	Markers           []string `json:"markers"`
	TestCommand       *string  `json:"test_command"`
	TestCommandSource *string  `json:"test_command_source"`
	// Testability is one of `testability_state`; computed, never configured.
	Testability string `json:"testability"`
	// TestabilityReason names the file or directory that ruled out the exemption when one did.
	TestabilityReason string `json:"testability_reason"`
	// Checks are the project's lint and format commands, as files state them.
	Checks []Check `json:"checks"`
}

// Coverage is whether one command, run from the workspace root unless the
// reason says otherwise, covers every discovered project; Command is nil
// unless covered.
type Coverage struct {
	Covered string  `json:"covered"`
	Command *string `json:"command"`
	Reason  string  `json:"reason"`
}

// OpenspecConfig is where the LDD config lives and whether it exists.
type OpenspecConfig struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

// Discovery is what `jhon init --json` prints, keys in their printed order.
type Discovery struct {
	Root string `json:"root"`
	// Membership is "declared" or "scanned".
	Membership     string         `json:"membership"`
	DeclaredBy     []string       `json:"declared_by"`
	Projects       []*Project     `json:"projects"`
	OpenspecConfig OpenspecConfig `json:"openspec_config"`
	Coverage       Coverage       `json:"coverage"`
	// Vcs is diagnostic only: LDD needs `ready`, `jhon status` enforces it,
	// and `jhon init` configures nothing.
	Vcs   vcs.Vcs  `json:"vcs"`
	Notes []string `json:"notes"`
}

func str(s string) *string { return &s }

// read is a file's text; ok is false when it cannot be read.
func read(p string) (string, bool) {
	b, err := os.ReadFile(p)
	return string(b), err == nil
}

func isDir(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.IsDir()
}

// isRepo is whether p holds its own `.git` (file or directory): another repository.
func isRepo(p string) bool {
	_, err := os.Lstat(filepath.Join(p, ".git"))
	return err == nil
}

// parseJSON parses src: ok is false when src is not JSON.
func parseJSON(src string) (v any, ok bool) {
	return v, json.Unmarshal([]byte(src), &v) == nil
}

// field is v.k for a parsed JSON value: nil unless v is an object holding k.
func field(v any, k string) any {
	if m, ok := v.(map[string]any); ok {
		return m[k]
	}
	return nil
}

// crossesBoundary is whether rel (relative to root) crosses a boundary
// discovery must not enter: an excluded name, or a nested repository at any
// level below root.
func crossesBoundary(root, rel string) bool {
	at := root
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" {
			continue
		}
		if excluded[seg] {
			return true
		}
		at = filepath.Join(at, seg)
		if isRepo(at) {
			return true
		}
	}
	return false
}

// entries are a directory's entries in JavaScript's name order; nil when unreadable.
func entries(dir string) []os.DirEntry {
	es, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	slices.SortFunc(es, func(a, b os.DirEntry) int { return js.Compare(a.Name(), b.Name()) })
	return es
}

// childDirs are the child directory names, never following a symlink out of the tree.
func childDirs(dir string) []string {
	var out []string
	for _, e := range entries(dir) {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

func project_(root, rel string) *Project {
	dir := filepath.Join(root, rel)
	p := &Project{Path: rel, Stacks: []string{}, Markers: []string{}}
	if rel == "" {
		p.Path = "."
	}
	for _, m := range Markers {
		if project.Exists(filepath.Join(dir, m.File)) {
			p.Markers = append(p.Markers, m.File)
			p.Stacks = append(p.Stacks, m.Stack)
		}
	}
	if len(p.Markers) == 0 {
		return nil
	}
	if cmd := testCommand(dir, root, p.Markers); cmd != nil {
		p.TestCommand, p.TestCommandSource = cmd.command, str(cmd.source)
	}
	p.Checks = checks(dir, root, p.Markers)
	// Testability is set in Discover, once every project is known: the scan skips nested projects.
	return p
}

// ── declared membership (rule 1) ────────────────────────────────────────────

type declaration struct {
	source           string
	include, exclude []string
}

const (
	cargoSource = "Cargo.toml [workspace] members"
	// uvSource is declared membership only: no coverage rule reads it
	// (whether `uv run pytest` covers every member is undecided).
	uvSource = "pyproject.toml [tool.uv.workspace] members"
)

var (
	pnpmBlock = lazyre.New(`(?m)` + js.LineStart + `packages:` + js.Space + `*\n((?:[ \t]+-` + js.Dot + `*\n?)+)`)
	pnpmItem  = lazyre.New(`(?m)^[ \t]+-` + js.Space + `*(` + js.Dot + `+?)` + js.Space + `*$`)
)

// declaredPatterns are the declared member patterns from package.json,
// pnpm-workspace.yaml, Cargo.toml, and a uv pyproject.toml.
func declaredPatterns(root string) []declaration {
	var out []declaration
	if src, _ := read(filepath.Join(root, "package.json")); src != "" {
		if v, ok := parseJSON(src); ok && v != nil {
			ws := field(v, "workspaces")
			list, isList := ws.([]any)
			if !isList {
				list, isList = field(ws, "packages").([]any)
			}
			if isList {
				d := declaration{source: "package.json workspaces", include: []string{}}
				for _, s := range list {
					if s, ok := s.(string); ok {
						d.include = append(d.include, s)
					}
				}
				out = append(out, d)
			}
		}
	}

	if pnpm, _ := read(filepath.Join(root, "pnpm-workspace.yaml")); pnpm != "" {
		block := ""
		if m := pnpmBlock.FindStringSubmatch(pnpm); m != nil {
			block = m[1]
		}
		d := declaration{source: "pnpm-workspace.yaml packages", include: []string{}, exclude: []string{}}
		for _, m := range pnpmItem.FindAllStringSubmatch(block, -1) {
			item := m[1]
			if item != "" && (item[0] == '\'' || item[0] == '"') {
				item = item[1:]
			}
			if item != "" && (item[len(item)-1] == '\'' || item[len(item)-1] == '"') {
				item = item[:len(item)-1]
			}
			if rest, neg := strings.CutPrefix(item, "!"); neg {
				d.exclude = append(d.exclude, rest)
			} else {
				d.include = append(d.include, item)
			}
		}
		out = append(out, d)
	}

	if d, ok := tomlMembers(filepath.Join(root, "Cargo.toml"), "workspace"); ok {
		d.source = cargoSource
		out = append(out, d)
	}
	// uv declares its workspace in the root pyproject.toml, with the same
	// members/exclude glob arrays as Cargo, so it is read the same way.
	if d, ok := tomlMembers(filepath.Join(root, "pyproject.toml"), "tool.uv.workspace"); ok {
		d.source = uvSource
		out = append(out, d)
	}
	return out
}

var memberToken = lazyre.New(`"([^"]*)"|'([^']*)'|#[^\n]*`)

// tomlMembers are the `members` and `exclude` string arrays of one TOML
// table of the file; ok is false when the file or the table is absent.
func tomlMembers(file, table string) (d declaration, ok bool) {
	src, _ := read(file)
	header := regexp.MustCompile(`(?m)` + js.LineStart + `\[` + regexp.QuoteMeta(table) + `\]` + js.Space + `*\n`).FindStringIndex(src)
	if header == nil {
		return d, false
	}
	body := src[header[1]:tableEnd(src, header[1])]
	array := func(key string) []string {
		out := []string{}
		m := regexp.MustCompile(`(?m)` + js.LineStart + key + js.Space + `*=` + js.Space + `*\[([\s\S]*?)\]`).FindStringSubmatch(body)
		if m == nil {
			return out
		}
		// TOML allows basic ("…") and literal ('…') strings, and comments inside a
		// multi-line array. Scan token by token so a quote inside a comment is
		// never read as a member.
		for _, t := range memberToken.FindAllStringSubmatchIndex(m[1], -1) {
			switch {
			case t[2] >= 0:
				out = append(out, m[1][t[2]:t[3]])
			case t[4] >= 0:
				out = append(out, m[1][t[4]:t[5]])
			}
		}
		return out
	}
	return declaration{include: array("members"), exclude: array("exclude")}, true
}

// tableEnd is where a table's body starting at from ends: the first line
// that opens with `[`, or the end of src (JavaScript's ^ under the m flag
// also follows \r, U+2028, and U+2029).
func tableEnd(src string, from int) int {
	for p := from; p < len(src); p++ {
		if src[p] != '[' {
			continue
		}
		before := src[:p]
		if p == 0 || strings.HasSuffix(before, "\n") || strings.HasSuffix(before, "\r") ||
			strings.HasSuffix(before, "\u2028") || strings.HasSuffix(before, "\u2029") {
			return p
		}
	}
	return len(src)
}

// glob compiles a name pattern whose `*` is any run of characters but `/`.
func glob(name string) *regexp.Regexp {
	parts := strings.Split(name, "*")
	for i, s := range parts {
		parts[i] = regexp.QuoteMeta(s)
	}
	return regexp.MustCompile(`^` + strings.Join(parts, `[^/]*`) + `$`)
}

var trailingSlashes = lazyre.New(`/+$`)

// expand is one member pattern as workspace-relative directories. A segment
// may be a literal or contain `*` (any name within one level, dot entries
// excluded, as the package managers do). `**` would be an unbounded crawl,
// so it is noted rather than expanded.
func expand(root, pattern string, notes *[]string) []string {
	clean := trailingSlashes.ReplaceAllString(strings.TrimPrefix(pattern, "./"), "")
	if filepath.IsAbs(clean) || slices.Contains(strings.Split(clean, "/"), "..") {
		rel, err := filepath.Rel(root, project.Resolve(root, clean))
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			*notes = append(*notes, "declared member '"+pattern+"' resolves outside the workspace root; not inspected")
			return nil
		}
		if rel == "." {
			rel = ""
		}
		return []string{filepath.ToSlash(rel)}
	}
	if strings.Contains(clean, "**") {
		*notes = append(*notes, "declared member '"+pattern+"' uses '**', an unbounded pattern; not expanded")
		return nil
	}
	paths := []string{""}
	join := func(p, n string) string {
		if p == "" {
			return n
		}
		return p + "/" + n
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == "" {
			continue
		}
		var next []string
		if !strings.Contains(seg, "*") {
			for _, p := range paths {
				next = append(next, join(p, seg))
			}
		} else {
			re := glob(seg)
			for _, p := range paths {
				for _, n := range childDirs(filepath.Join(root, p)) {
					if !strings.HasPrefix(n, ".") && re.MatchString(n) {
						next = append(next, join(p, n))
					}
				}
			}
		}
		paths = next
	}
	return paths
}

// ── discovery ───────────────────────────────────────────────────────────────

// Discover is the discovery of the workspace at root. It panics when the
// vocabulary it reads is broken: discovery cannot run without it.
func Discover(root string) *Discovery {
	root = project.Resolve("", root)
	notes := []string{}
	declared := declaredPatterns(root)
	found := map[string]*Project{}
	var order []string
	// members are the declared member paths that became projects, per
	// declaring source; coverage needs to know who declared whom.
	members := map[string]map[string]bool{}
	add := func(rel string) *Project {
		p := project_(root, rel)
		if p != nil {
			if _, seen := found[p.Path]; !seen {
				order = append(order, p.Path)
			}
			found[p.Path] = p
		}
		return p
	}

	// The root is inspected in both modes: in a declared workspace it is where
	// the workspace-level scripts and manifests live.
	add("")

	if len(declared) > 0 {
		for _, d := range declared {
			members[d.source] = map[string]bool{}
			skip := map[string]bool{}
			for _, e := range d.exclude {
				for _, rel := range expand(root, e, &[]string{}) {
					skip[rel] = true
				}
			}
			for _, pattern := range d.include {
				for _, rel := range expand(root, pattern, &notes) {
					_, known := found[rel]
					if rel != "" && !skip[rel] && known {
						members[d.source][rel] = true
					}
					if rel == "" || skip[rel] || known {
						continue
					}
					if !isDir(filepath.Join(root, rel)) {
						notes = append(notes, "declared member '"+rel+"' does not exist")
						continue
					}
					if crossesBoundary(root, rel) {
						continue
					}
					if add(rel) != nil {
						members[d.source][rel] = true
					} else {
						notes = append(notes, "declared member '"+rel+"' has no supported project marker")
					}
				}
			}
		}
	} else {
		var walk func(rel string, depth int)
		walk = func(rel string, depth int) {
			if depth > MaxDepth {
				return
			}
			if rel != "" {
				add(rel)
			}
			for _, name := range childDirs(filepath.Join(root, rel)) {
				child := name
				if rel != "" {
					child = rel + "/" + name
				}
				if excluded[name] || isRepo(filepath.Join(root, child)) {
					continue
				}
				walk(child, depth+1)
			}
		}
		walk("", 0)
	}

	// T64: a root with Python test files and no marker, where nothing else
	// is found, is a Python project with no command: the owner's or
	// init's test_command_override names it, as for an unknown one.
	if len(order) == 0 && markerlessPython(root) {
		found["."] = &Project{Path: ".", Stacks: []string{"Python"}, Markers: []string{}, Checks: checks(root, root, nil)}
		order = append(order, ".")
	}

	projects := []*Project{}
	for _, path := range order {
		projects = append(projects, found[path])
	}
	slices.SortStableFunc(projects, func(a, b *Project) int {
		switch {
		case a.Path == b.Path:
			return 0
		case a.Path == ".":
			return -1
		case b.Path == ".":
			return 1
		}
		return js.Compare(a.Path, b.Path)
	})
	for _, p := range projects {
		p.Testability, p.TestabilityReason = testability(root, p, projects)
	}
	d := &Discovery{
		Root:           root,
		Membership:     "scanned",
		DeclaredBy:     []string{},
		Projects:       projects,
		OpenspecConfig: OpenspecConfig{layout.For(root).Config(), project.UsesLdd(root)},
		Coverage:       coverage(root, projects, members),
		Vcs:            vcs.State(root),
		Notes:          notes,
	}
	if len(declared) > 0 {
		d.Membership = "declared"
	}
	for _, x := range declared {
		d.DeclaredBy = append(d.DeclaredBy, x.source)
	}
	return d
}

// markerlessPython is whether root holds a Python test file by convention
// (test_*.py, *_test.py) beside it, not below.
func markerlessPython(root string) bool {
	for _, e := range entries(root) {
		if _, ok := find(vocab().conventions["Python"], false, e.Name()); ok && !e.IsDir() && strings.HasSuffix(e.Name(), ".py") {
			return true
		}
	}
	return false
}

// EnclosingProject is the project enclosing from, or "" (here beside the
// markers it reads). A project is a git
// work tree, an ancestor holding openspec/, or a directory holding a
// project manifest; the home directory or a scratch folder is none of
// these, and doctor then says nothing about version control.
func EnclosingProject(from string) string {
	start := project.Resolve("", from)
	if top := vcs.GitTopLevel(start); top != "" {
		return top
	}
	for d := start; ; d = filepath.Dir(d) {
		if _, ok := layout.Detected(d); ok {
			return d
		}
		if d == filepath.Dir(d) {
			break
		}
	}
	for _, m := range Markers {
		if project.Exists(filepath.Join(start, m.File)) {
			return start
		}
	}
	return ""
}
