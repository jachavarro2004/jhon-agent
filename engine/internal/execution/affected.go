// Package execution runs and records a change's test runs
// (contracts/ldd.md, "Test runs"): which projects a
// run selects, the run itself, and the record that status and delivery read.
package execution

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/toml"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// This file is which projects a test run selects with `--affected <base>`.
//
// A project is selected when a file under it changed since the base (the
// merge base of the base and HEAD, compared with the working state: commits,
// staged and unstaged edits, and untracked files), or when it depends,
// directly or through other projects, on a selected project. A file belongs to
// the deepest discovered project whose directory holds it; `openspec/` is the
// change's record, never project input, and is ignored, as is jhon-studio's
// working output under `.jhon/studio/` at any depth (layout.StudioOutput, T8g).
//
// Every project is selected, with the reason recorded, when the selection
// cannot be trusted: a changed file outside every project (root
// configuration, a lockfile, CI, a shared directory no manifest claims), or
// dependency information that is incomplete (a manifest that does not parse).
// Dependencies are read from the manifests discovery already recognises:
// package.json (file:, link:, portal:, and workspace: specs, or a dependency
// named like another package), go.mod (require of another module, replace to
// a path), Cargo.toml (a path dependency, or a dependency named like another
// crate), and pyproject.toml (a path source, or a dependency named like
// another project).
//
// A changed file the project declares inert (`executions_inert` in
// openspec/config.yaml: a file, or a directory ending in `/`) is no test
// input either: a docker-compose.yml that only runs the services locally
// selects nothing.
//
// Read-only: git diff, merge-base, and ls-files only read.

// The scopes of a run.
const (
	Full     = "full"
	Affected = "affected"
)

// Selected is a project a run selects, and why.
type Selected struct {
	Project string `json:"project"`
	Reason  string `json:"reason"`
}

// Selection is the projects a run selects.
type Selection struct {
	// Scope is Full when every project was selected, Affected otherwise.
	Scope string `json:"scope"`
	// Selected is the projects to run, each with why it was selected.
	Selected []Selected `json:"selected"`
	// Reasons say why the scope is what it is: the fallback to every project, or what was ignored.
	Reasons []string `json:"reasons"`
}

// ignoredOf is the prefix of root's layout's record.
func ignoredOf(root string) string { return layout.For(root).Dir() + "/" }

// ChangedSince is the paths changed since base, relative to root, and those
// outside root (relative to the git top level); errMsg says why they cannot
// be read.
func ChangedSince(root, base string) (files, outside []string, errMsg string) {
	top := vcs.GitTopLevel(root)
	if top == "" {
		return nil, nil, root + " is not inside a git work tree; --affected needs one"
	}
	commit, err := project.ReadGitCmd(root, "rev-parse", "--verify", "--quiet", base+"^{commit}").Output()
	if err != nil {
		return nil, nil, fmt.Sprintf("the base '%s' does not resolve to a commit", base)
	}
	mb, err := project.ReadGitCmd(root, "merge-base", js.Trim(string(commit)), "HEAD").Output()
	if err != nil {
		return nil, nil, fmt.Sprintf("the base '%s' has no merge base with HEAD", base)
	}
	diff, ok := vcs.WorkingDiff(root, js.Trim(string(mb)))
	if !ok {
		return nil, nil, fmt.Sprintf("git could not diff the working state against '%s'", base)
	}
	files, outside = underRoot(top, root, diff)
	return files, outside, ""
}

// ChangedBetween is ChangedSince for two trees of the repository holding
// root (vcs.WorkingTree): the paths that differ from one to the other. ok is
// false outside a git work tree, or when a tree does not resolve: git's gc
// prunes a tree no commit holds.
func ChangedBetween(root, from, to string) (files, outside []string, ok bool) {
	top := vcs.GitTopLevel(root)
	if top == "" || from == "" || to == "" {
		return nil, nil, false
	}
	diff, ok := vcs.TreeDiff(top, from, to)
	if !ok {
		return nil, nil, false
	}
	files, outside = underRoot(top, root, diff)
	return files, outside, true
}

// underRoot splits diff, paths relative to the git top level top, into those
// under root (relative to it) and those outside it.
func underRoot(top, root string, diff []string) (files, outside []string) {
	prefix := filepath.ToSlash(relReal(top, root))
	files, outside = []string{}, []string{}
	for _, p := range diff {
		switch {
		case prefix == "":
			files = append(files, p)
		case strings.HasPrefix(p, prefix+"/"):
			files = append(files, p[len(prefix)+1:])
		default:
			outside = append(outside, p)
		}
	}
	return files, outside
}

// relReal is path.relative(realpath(from), realpath(to)), "" for the same directory.
func relReal(from, to string) string {
	real := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return project.Resolve("", p)
	}
	rel, err := filepath.Rel(real(from), real(to))
	if err != nil || rel == "." {
		return ""
	}
	return rel
}

// owner is the deepest project path (never `.`) that holds file, or "".
func owner(file string, projects []string) string {
	best := ""
	for _, p := range projects {
		if p == "." || !strings.HasPrefix(file, p+"/") {
			continue
		}
		if best == "" || js.Len(p) > js.Len(best) {
			best = p
		}
	}
	return best
}

// ── dependencies ────────────────────────────────────────────────────────────

// A ref is one dependency a manifest declares: by name (when named), by
// path (when path is not ""), or both.
type ref struct {
	name, path string
	named      bool
}

type manifest struct {
	names []string
	refs  []ref
}

func pyName(s string) string { return pyNameSep.ReplaceAllString(js.Lower(s), "-") }

var (
	pyNameSep  = lazyre.New(`[-_.]+`)
	nodePath   = lazyre.New(`^(?:file|link|portal):(` + js.Dot + `+)$`)
	goModule   = lazyre.New(`^module` + js.Space + `+([^` + js.SpaceChars + `]+)`)
	goOpen     = lazyre.New(`^(require|replace)` + js.Space + `*\($`)
	goKind     = lazyre.New(`^(require|replace)` + js.Space)
	goKindBody = lazyre.New(`^(require|replace)` + js.Space + `+`)
	spaces     = lazyre.New(js.Space + `+`)
	relPath    = lazyre.New(`^\.{1,2}/`)
	pepName    = lazyre.New(`^` + js.Space + `*([A-Za-z0-9][A-Za-z0-9._-]*)`)
	pepPath    = lazyre.New(`@` + js.Space + `*file://([^` + js.SpaceChars + `]+)`)
)

// ── package.json, read as JavaScript's JSON.parse reads it ──

// parseJSON parses JSON with objects as *js.Object and arrays as []any.
func parseJSON(src string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(src))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("Unexpected non-whitespace character after JSON")
	}
	return v, nil
}

// endOfInput names a truncated document as JavaScript's JSON.parse does; other syntax
// errors keep encoding/json's wording.
func endOfInput(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return errors.New("Unexpected end of JSON input")
	}
	return err
}

func decodeValue(dec *json.Decoder) (any, error) {
	t, err := dec.Token()
	if err != nil {
		return nil, endOfInput(err)
	}
	switch t {
	case json.Delim('{'):
		o := js.NewObject()
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return nil, err
			}
			v, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			o.Set(k.(string), v)
		}
		_, err := dec.Token()
		return o, endOfInput(err)
	case json.Delim('['):
		a := []any{}
		for dec.More() {
			v, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
		_, err := dec.Token()
		return a, endOfInput(err)
	}
	return t, nil
}

// asEntries is a value as Object.entries reads an object, or nil when it is
// not one (an array is an object keyed by its indices).
func asEntries(v any) (keys []string, vals map[string]any, ok bool) {
	switch x := v.(type) {
	case *js.Object:
		keys, vals = x.Keys(), map[string]any{}
		for _, k := range keys {
			vals[k] = js.Prop(x, k)
		}
		return keys, vals, true
	case []any:
		vals = map[string]any{}
		for i, e := range x {
			k := strconv.Itoa(i)
			keys, vals[k] = append(keys, k), e
		}
		return keys, vals, true
	}
	return nil, nil, false
}

func nodeManifest(src string) (manifest, error) {
	doc, err := parseJSON(src)
	if err != nil {
		return manifest{}, err
	}
	if doc == nil {
		return manifest{}, errors.New("Cannot read properties of null (reading 'name')")
	}
	pkg, _ := doc.(*js.Object)
	if pkg == nil {
		// A number, a string, or an array has no name and no dependencies.
		return manifest{}, nil
	}
	var m manifest
	if name, ok := js.Prop(pkg, "name").(string); ok {
		m.names = append(m.names, name)
	}
	for _, field := range []string{"dependencies", "devDependencies", "peerDependencies", "optionalDependencies"} {
		keys, vals, ok := asEntries(js.Prop(pkg, field))
		if !ok {
			continue
		}
		for _, name := range keys {
			spec, _ := vals[name].(string)
			if p := nodePath.FindStringSubmatch(spec); p != nil {
				m.refs = append(m.refs, ref{name, p[1], true})
			} else {
				m.refs = append(m.refs, ref{name, "", true})
			}
		}
	}
	return m, nil
}

// goComment is the start of a trailing `//` comment that /\/\/.*$/ removes:
// the first `//` followed by no line terminator, -1 when there is none.
func goComment(l string) int {
	for i := 0; i+1 < len(l); i++ {
		if l[i] == '/' && l[i+1] == '/' && !strings.ContainsAny(l[i+2:], "\r  ") {
			return i
		}
	}
	return -1
}

// firstWord is s.split(/\s+/)[0].
func firstWord(s string) string {
	if loc := spaces.FindStringIndex(s); loc != nil {
		return s[:loc[0]]
	}
	return s
}

func goManifest(src string) (manifest, error) {
	var m manifest
	block := ""
	for _, l := range strings.Split(src, "\n") {
		if i := goComment(l); i >= 0 {
			l = l[:i]
		}
		if l = js.Trim(l); l == "" {
			continue
		}
		if block != "" && l == ")" {
			block = ""
			continue
		}
		if mod := goModule.FindStringSubmatch(l); mod != nil {
			m.names = append(m.names, mod[1])
			continue
		}
		if open := goOpen.FindStringSubmatch(l); open != nil {
			block = open[1]
			continue
		}
		kind, body := block, l
		if block == "" {
			if k := goKind.FindStringSubmatch(l); k != nil {
				kind = k[1]
			}
			body = goKindBody.ReplaceAllString(l, "")
		}
		switch kind {
		case "require":
			m.refs = append(m.refs, ref{firstWord(body), "", true})
		case "replace":
			if parts := strings.Split(body, "=>"); len(parts) > 1 {
				if target := firstWord(js.Trim(parts[1])); target != "" && relPath.MatchString(target) {
					m.refs = append(m.refs, ref{path: target})
				}
			}
		}
	}
	return m, nil
}

// tomlEntry is one key of a dependency table and its value.
type tomlEntry struct {
	name string
	v    toml.Value
}

func tomlDeps(t *toml.Table) []tomlEntry {
	if t == nil {
		return nil
	}
	var out []tomlEntry
	for _, k := range t.Keys() {
		v, _ := t.Get(k)
		out = append(out, tomlEntry{k, v})
	}
	return out
}

// tableString is v.<key> when v is a table holding a string there.
func tableString(v toml.Value, key string) (string, bool) {
	t, ok := toml.AsTable(v)
	if !ok {
		return "", false
	}
	s, ok := mustGet(t, key).(string)
	return s, ok
}

func mustGet(t *toml.Table, k string) toml.Value {
	if t == nil {
		return nil
	}
	v, _ := t.Get(k)
	return v
}

// tableValues is Object.values of the table at a path, or nothing.
func tableValues(t *toml.Table) []toml.Value {
	var out []toml.Value
	for _, e := range tomlDeps(t) {
		out = append(out, e.v)
	}
	return out
}

func cargoManifest(src string) (manifest, error) {
	doc, err := toml.Parse(src)
	if err != nil {
		return manifest{}, err
	}
	var m manifest
	if name, ok := mustGet(toml.TableAt(doc, "package"), "name").(string); ok {
		m.names = append(m.names, name)
	}
	tables := []*toml.Table{toml.TableAt(doc, "dependencies"), toml.TableAt(doc, "dev-dependencies"), toml.TableAt(doc, "build-dependencies")}
	for _, target := range tableValues(toml.TableAt(doc, "target")) {
		if t, ok := toml.AsTable(target); ok {
			tables = append(tables, toml.TableAt(t, "dependencies"), toml.TableAt(t, "dev-dependencies"), toml.TableAt(t, "build-dependencies"))
		}
	}
	for _, t := range tables {
		for _, e := range tomlDeps(t) {
			pkg, ok := tableString(e.v, "package")
			if !ok {
				pkg = e.name
			}
			path, _ := tableString(e.v, "path")
			m.refs = append(m.refs, ref{pkg, path, true})
		}
	}
	return m, nil
}

func pythonManifest(src string) (manifest, error) {
	doc, err := toml.Parse(src)
	if err != nil {
		return manifest{}, err
	}
	var m manifest
	for _, n := range []toml.Value{mustGet(toml.TableAt(doc, "project"), "name"), mustGet(toml.TableAt(doc, "tool", "poetry"), "name")} {
		if s, ok := n.(string); ok {
			m.names = append(m.names, pyName(s))
		}
	}
	pep508 := func(d toml.Value) {
		s, ok := d.(string)
		if !ok {
			return
		}
		name := pepName.FindStringSubmatch(s)
		if name == nil {
			return
		}
		path := ""
		if p := pepPath.FindStringSubmatch(s); p != nil {
			path = p[1]
		}
		m.refs = append(m.refs, ref{pyName(name[1]), path, true})
	}
	project := toml.TableAt(doc, "project")
	if deps, ok := mustGet(project, "dependencies").([]toml.Value); ok {
		for _, d := range deps {
			pep508(d)
		}
	}
	for _, t := range []*toml.Table{toml.TableAt(doc, "project", "optional-dependencies"), toml.TableAt(doc, "dependency-groups")} {
		for _, group := range tableValues(t) {
			if g, ok := group.([]toml.Value); ok {
				for _, d := range g {
					pep508(d)
				}
			}
		}
	}
	poetry := []*toml.Table{toml.TableAt(doc, "tool", "poetry", "dependencies"), toml.TableAt(doc, "tool", "poetry", "dev-dependencies")}
	for _, g := range tableValues(toml.TableAt(doc, "tool", "poetry", "group")) {
		t, _ := toml.AsTable(g)
		poetry = append(poetry, toml.TableAt(t, "dependencies"))
	}
	for _, t := range poetry {
		for _, e := range tomlDeps(t) {
			path, _ := tableString(e.v, "path")
			m.refs = append(m.refs, ref{pyName(e.name), path, true})
		}
	}
	for _, e := range tomlDeps(toml.TableAt(doc, "tool", "uv", "sources")) {
		if path, _ := tableString(e.v, "path"); path != "" {
			m.refs = append(m.refs, ref{pyName(e.name), path, true})
		}
	}
	return m, nil
}

var readers = []struct {
	file string
	read func(string) (manifest, error)
}{{"package.json", nodeManifest}, {"go.mod", goManifest}, {"Cargo.toml", cargoManifest}, {"pyproject.toml", pythonManifest}}

// Dependencies is, for each project, the other projects it depends on (in
// the order the manifests name them), and the reasons the information is
// incomplete (empty when it is complete). The error is a manifest that
// exists and cannot be read at all.
func Dependencies(root string, projects []string) (deps map[string][]string, incomplete []string, err error) {
	manifests := map[string][]manifest{}
	var order []string
	incomplete = []string{}
	for _, p := range projects {
		var list []manifest
		for _, r := range readers {
			file := filepath.Join(root, p, r.file)
			if !project.Exists(file) {
				continue
			}
			b, err := os.ReadFile(file)
			if err != nil {
				return nil, nil, err
			}
			m, err := r.read(string(b))
			if err != nil {
				first, _, _ := strings.Cut(err.Error(), "\n")
				incomplete = append(incomplete, fmt.Sprintf("%s cannot be read for dependencies (%s)", path.Join(p, r.file), first))
				continue
			}
			list = append(list, m)
		}
		if _, seen := manifests[p]; !seen {
			order = append(order, p)
		}
		manifests[p] = list
	}
	byName := map[string]string{}
	for _, p := range order {
		for _, m := range manifests[p] {
			for _, n := range m.names {
				byName[n] = p
			}
		}
	}
	byPath := map[string]string{}
	for _, p := range projects {
		byPath[project.Resolve(root, p)] = p
	}
	deps = map[string][]string{}
	for _, p := range order {
		out := []string{}
		for _, m := range manifests[p] {
			for _, r := range m.refs {
				var via []string
				if r.path != "" {
					if d, ok := byPath[project.Resolve(filepath.Join(root, p), r.path)]; ok {
						via = append(via, d)
					}
				}
				if r.named {
					if d, ok := byName[r.name]; ok {
						via = append(via, d)
					}
				}
				for _, d := range via {
					if d != p && !slices.Contains(out, d) {
						out = append(out, d)
					}
				}
			}
		}
		deps[p] = out
	}
	return deps, incomplete, nil
}

// ── selection ───────────────────────────────────────────────────────────────

// all is every candidate selected, for reasons.
func all(candidates, reasons []string) Selection {
	s := Selection{Full, []Selected{}, reasons}
	for _, c := range candidates {
		s.Selected = append(s.Selected, Selected{c, "all projects selected"})
	}
	return s
}

// Inert is whether f, relative to the root, is one of inert: the same file,
// or under an entry ending in `/`.
func Inert(f string, inert []string) bool {
	return slices.ContainsFunc(inert, func(i string) bool {
		return f == i || (strings.HasSuffix(i, "/") && strings.HasPrefix(f, i))
	})
}

// SelectAffected is the selection among candidates (the projects a run may
// execute, in run order) of the projects affected by the change since base.
// projects is every discovered project path, testable or not: a file in a
// project that runs no tests still selects the projects that depend on it.
// errMsg says why the change cannot be read; err is a manifest that cannot be.
//
// inert is the changed files that are no test input (Inert).
func SelectAffected(root, base string, projects, candidates, inert []string) (sel Selection, errMsg string, err error) {
	files, outside, errMsg := ChangedSince(root, base)
	if errMsg != "" {
		return Selection{}, errMsg, nil
	}
	sel, err = SelectChanged(root, base, files, outside, projects, candidates, inert)
	return sel, "", err
}

// SelectChanged is SelectAffected for a change already read: files (relative
// to root) and outside (ChangedSince, ChangedBetween), measured from what
// since names.
func SelectChanged(root, since string, files, outside, projects, candidates, inert []string) (Selection, error) {
	reasons := []string{}
	var global []string
	for _, f := range outside {
		global = append(global, f+" is outside the workspace root")
	}
	direct := map[string][]string{}
	source := map[string]string{}
	skipped, studio, inertN, unread := 0, 0, 0, 0
	for _, f := range files {
		if strings.HasPrefix(f, ignoredOf(root)) {
			skipped++
			continue
		}
		// jhon-studio's working output, at any depth, is no test input (T8g).
		if layout.StudioOutput(f) {
			studio++
			continue
		}
		if Inert(f, inert) {
			inertN++
			continue
		}
		// Documentation is test input only to the tests that read it: it
		// selects the candidates whose test modules name it, wherever it is.
		if Prose(f) {
			read := false
			for _, c := range candidates {
				if len(ProseReaders(root, c, []string{f}, projects)) > 0 {
					direct[c] = append(direct[c], f)
					read = true
				}
			}
			if !read {
				unread++
			}
			continue
		}
		p := owner(f, projects)
		if p == "" {
			global = append(global, f+" is outside every project (root configuration, a lockfile, CI, or a shared file)")
			continue
		}
		direct[p] = append(direct[p], f)
		source[f] = p
	}
	if skipped > 0 {
		reasons = append(reasons, fmt.Sprintf("%d changed file(s) under %s ignored: the change's record is not project input", skipped, ignoredOf(root)))
	}
	if studio > 0 {
		reasons = append(reasons, fmt.Sprintf("%d changed file(s) under %s/ ignored: jhon-studio's working output is not project input", studio, layout.StudioDir))
	}
	if unread > 0 {
		reasons = append(reasons, fmt.Sprintf("%d changed documentation file(s) ignored: no test module names them", unread))
	}
	if inertN > 0 {
		reasons = append(reasons, fmt.Sprintf("%d changed file(s) ignored: executions_inert declares them no test input", inertN))
	}
	if len(global) > 0 {
		for _, g := range global {
			reasons = append(reasons, "every project selected: "+g)
		}
		return all(candidates, reasons), nil
	}
	// A source file is also test input to another project's tests that name
	// its path (SourceReaders).
	for c, fs := range SourceReaders(root, candidates, source) {
		direct[c] = append(direct[c], fs...)
	}
	deps, incomplete, err := Dependencies(root, projects)
	if err != nil {
		return Selection{}, err
	}
	if len(incomplete) > 0 {
		for _, i := range incomplete {
			reasons = append(reasons, "every project selected: dependency information is incomplete: "+i)
		}
		return all(candidates, reasons), nil
	}

	why := map[string]string{}
	listed := func(fs []string) string {
		s := strings.Join(fs[:min(3, len(fs))], ", ")
		if len(fs) > 3 {
			s += fmt.Sprintf(", and %d more", len(fs)-3)
		}
		return s
	}
	for p, fs := range direct {
		why[p] = "changed: " + listed(fs)
	}
	// Dependents, to a fixed point: a project that depends on a selected one is
	// selected. deps is walked in project order, so the selection is stable.
	var order []string
	for _, p := range projects {
		if _, ok := deps[p]; ok && !slices.Contains(order, p) {
			order = append(order, p)
		}
	}
	for grew := true; grew; {
		grew = false
		for _, p := range order {
			if _, ok := why[p]; ok {
				continue
			}
			if i := slices.IndexFunc(deps[p], func(d string) bool { _, ok := why[d]; return ok }); i >= 0 {
				why[p] = "depends on " + deps[p][i]
				grew = true
			}
		}
	}
	selected := []Selected{}
	for _, c := range candidates {
		if r, ok := why[c]; ok {
			selected = append(selected, Selected{c, r})
		}
	}
	if len(selected) == len(candidates) && len(candidates) > 0 {
		return Selection{Full, selected, append(reasons, "every project is affected")}, nil
	}
	if len(selected) == 0 {
		reasons = append(reasons, "no project is affected since "+since)
	}
	return Selection{Affected, selected, reasons}, nil
}
