package discover

// The resolved projects (contracts/ldd.md, "Resolved
// command"): live discovery plus the human's test command overrides. This is
// how every consumer reads projects: `jhon status` (for `projects` and the
// Strict TDD capability) and `jhon executions run` (for what to run).
// Discovery stays `jhon init`'s; this only layers `test_command_override`
// from openspec/config.yaml on top of it, so a command is never chosen twice.
//
//   - A project's resolved command is its override when present, otherwise
//     its discovered `test_command`. It is used as written: nothing adds flags.
//   - An `unknown` project with an override becomes `testable`, because the
//     override is exactly the certain command discovery could not find.
//   - A `structurally_exempt` project stays exempt: an override does not make
//     a project with nothing to test into one that must run. The entry is
//     reported, so the human sees it has no effect.
//   - An override for a path discovery did not find is reported and ignored:
//     running a command for a directory discovery does not recognise would
//     make the override, not discovery, decide what exists.
//
// Every key, value, and note template comes from contracts/vocabulary.yaml.

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// configVocabulary is what resolution reads from contracts/vocabulary.yaml,
// on first use.
type configVocabulary struct {
	discovered, override string
	keys, notes          map[string]string
}

var configVocab = sync.OnceValue(func() configVocabulary {
	src := contract.MustContract("vocabulary.yaml")
	_, keys := contract.Map(src, "strict_tdd_config")
	_, notes := contract.Map(src, "strict_tdd_notes")
	for _, k := range []string{"file", "override", "evidence"} {
		if keys[k] == "" {
			panic(fmt.Errorf("contracts/vocabulary.yaml: strict_tdd_config must name %s", k))
		}
	}
	for _, k := range []string{"override_undiscovered", "override_exempt"} {
		if notes[k] == "" {
			panic(fmt.Errorf("contracts/vocabulary.yaml: strict_tdd_notes must declare %s", k))
		}
	}
	s := contract.MustVocabulary("command_source", 2)
	return configVocabulary{s[0], s[1], keys, notes}
})

// Discovered is the command_source value of a command discovery found.
func Discovered() string { return configVocab().discovered }

// Override is the command_source value of a command the config overrides.
func Override() string { return configVocab().override }

// A ResolvedProject is a discovered project after overrides. Command is nil
// when the project is not testable; CommandSource, one of `command_source`,
// is nil when there is no command.
type ResolvedProject struct {
	Path string `json:"path"`
	// Testability is one of `testability_state`, after overrides.
	Testability       string  `json:"testability"`
	TestabilityReason string  `json:"testability_reason"`
	Command           *string `json:"command"`
	CommandSource     *string `json:"command_source"`
	// Checks are the project's lint and format checks after overrides.
	Checks []Check `json:"checks"`
}

// ResolvedProjects are a root's resolved projects and the notes on ignored overrides.
type ResolvedProjects struct {
	Projects []ResolvedProject `json:"projects"`
	Notes    []string          `json:"notes"`
}

// A StringMap is a JavaScript Map of strings: values by key, keys in
// first-insertion order.
type StringMap struct {
	Keys   []string
	Values map[string]string
}

// Get is the value at k, and whether the map holds it.
func (m *StringMap) Get(k string) (string, bool) {
	if m == nil {
		return "", false
	}
	v, ok := m.Values[k]
	return v, ok
}

func (m *StringMap) set(k, v string) {
	if _, ok := m.Values[k]; !ok {
		m.Keys = append(m.Keys, k)
	}
	m.Values[k] = v
}

var (
	commentLine = lazyre.New(`^` + js.Space + `*#`)
	indented    = lazyre.New(`^[ \t]`)
	configEntry = lazyre.New(`^("[^"]*"|'[^']*'|[^:` + js.SpaceChars + `][^:]*?):` + js.Space + `+(` + js.Dot + `+?)` + js.Space + `*(?:` + js.Space + `#` + js.Dot + `*)?$`)
)

// unquote is a trimmed value without one pair of surrounding quotes.
func unquote(s string) string {
	s = js.Trim(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] && !strings.ContainsAny(s[1:len(s)-1], "\r\u2028\u2029") {
		return s[1 : len(s)-1]
	}
	return s
}

// ConfigMap is the top-level mapping of `key: value` lines in the config, or
// nil when the key is absent. Keys and values may be quoted, and a trailing
// comment is dropped; `.` names the root project. A line scan over the one
// shape the LDD init phase writes, not a YAML parser.
func ConfigMap(config, key string) *StringMap {
	lines := strings.Split(config, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	header := regexp.MustCompile(`^` + regexp.QuoteMeta(key) + `:` + js.Space + `*(?:#` + js.Dot + `*)?$`)
	at := -1
	for i, l := range lines {
		if header.MatchString(l) {
			at = i
			break
		}
	}
	if at < 0 {
		return nil
	}
	out := &StringMap{Values: map[string]string{}}
	for _, line := range lines[at+1:] {
		if js.Trim(line) == "" || commentLine.MatchString(line) {
			continue
		}
		if !indented.MatchString(line) {
			break
		}
		if kv := configEntry.FindStringSubmatch(js.Trim(line)); kv != nil {
			out.set(unquote(kv[1]), unquote(kv[2]))
		}
	}
	return out
}

func note(template, path string) string {
	return overrideNote(template, configVocab().keys["override"], path)
}

// overrideNote is a note template for the override key key.
func overrideNote(template, key, path string) string {
	s := strings.ReplaceAll(template, "{file}", configVocab().keys["file"])
	s = strings.ReplaceAll(s, "{override}", key)
	return strings.ReplaceAll(s, "{path}", path)
}

// ResolveProjects is the resolution of a discovery under config, the text of
// openspec/config.yaml ("" when there is none).
func ResolveProjects(d *Discovery, config string) ResolvedProjects {
	v := configVocab()
	overrides := ConfigMap(config, v.keys["override"])
	out := ResolvedProjects{Projects: []ResolvedProject{}, Notes: []string{}}
	known := map[string]bool{}
	for _, p := range d.Projects {
		known[p.Path] = true
	}
	checkOverrides, checkNotes := checkOverrides(config, known)
	for _, p := range d.Projects {
		r := ResolvedProject{Path: p.Path, Testability: p.Testability, TestabilityReason: p.TestabilityReason, Checks: resolveChecks(p, checkOverrides)}
		override, has := overrides.Get(p.Path)
		switch {
		case has && p.Testability == Exempt():
			out.Notes = append(out.Notes, note(v.notes["override_exempt"], p.Path))
		case has:
			r.Testability, r.TestabilityReason = Testable(), v.keys["override"]+" "+override
			r.Command, r.CommandSource = str(override), str(v.override)
		case p.Testability == Testable():
			r.Command, r.CommandSource = p.TestCommand, str(v.discovered)
		}
		out.Projects = append(out.Projects, r)
	}
	if overrides != nil {
		for _, path := range overrides.Keys {
			if !known[path] {
				out.Notes = append(out.Notes, note(v.notes["override_undiscovered"], path))
			}
		}
	}
	out.Notes = append(out.Notes, checkNotes...)
	return out
}

// ConfigFor is the config text of a root, "" when it has none; an error
// when it exists but cannot be read. It is the root's layout's config
// (openspec/ or ldd/, T60); the vocabulary's file names it in messages only.
func ConfigFor(root string) (string, error) {
	file := project.LddConfigFile(root)
	if !project.Exists(file) {
		return "", nil
	}
	b, err := os.ReadFile(file)
	return string(b), err
}

// ProjectsFor is the resolved projects of a root: its live discovery and its config.
func ProjectsFor(root string) (ResolvedProjects, error) {
	d := Discover(root)
	config, err := ConfigFor(root)
	if err != nil {
		return ResolvedProjects{}, err
	}
	return ResolveProjects(d, config), nil
}

// FindRoot is the project root: the nearest ancestor of from holding a
// layout, or from itself. `jhon status` and `jhon executions` resolve it
// the same way, so both read the same config and the same change directory.
// Inside a git repository the search stops at its toplevel: a layout
// above it, openspec/ or ldd/, is never the repository's (L477, L786,
// L853), and a nested repository or linked worktree is bounded by its
// own. When git cannot name a toplevel (from inside .git, or a
// safe.directory refusal) the search climbs as for no repository. In a folder in no repository openspec/ is found above it, as it
// always was (L464), and a native ldd/ only at from itself.
func FindRoot(from string) string {
	start := project.Resolve("", from)
	top := ""
	if t, ok := project.ReadGit(start, "rev-parse", "--show-toplevel"); ok {
		top = project.Realish(t)
	}
	for d := start; ; d = filepath.Dir(d) {
		if top != "" {
			if !project.Within(project.Realish(d), top) {
				return start
			}
			if _, ok := layout.Detected(d); ok {
				return d
			}
		} else if layout.OpenSpec.Detect(d) || (d == start && layout.Native.Detect(d)) {
			return d
		}
		if d == filepath.Dir(d) {
			return start
		}
	}
}
