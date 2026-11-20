// Package settings is the owner's settings: one versioned file by domain,
// ~/.config/jhon/settings.toml (schema 1, S38), read with each value's
// source.
//
// A value is, in order of precedence, its environment variable (today's
// names, JHON_SHELL_* and the rest, keep working), the file, or the
// schema's default; with no file every key takes its default. Each key has
// a type and its accepted values, and the file is judged whole: an unknown
// key, a value of the wrong type or out of range, both runtimes off, or
// engram/codegraph off (required for now) is an error naming the key and
// what it accepts. An unusable environment value is ignored, as the shell
// ignores one today. A key T15.3 removed is no error: Retired lists it with
// why, Load reads its renamed value, and Cleanup drops it (retired.go).
//
// Set edits the file line by line, so the owner's comments and every
// unrelated byte stay; Migrate is the one-time move from the shell's JSON
// files. Project reads a project's LDD rules (openspec/config.yaml),
// read-only. Nothing here imports an optional integration.
package settings

import (
	"errors"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// Schema is the version of the file this package reads and writes.
const Schema = 1

// Where a value comes from.
const (
	SourceDefault = "default"
	SourceFile    = "file"
	SourceEnv     = "env"
	SourceProject = "project"
)

// A Value is one key's effective value: a bool, an int, a string, or (for
// a project's block keys) a []string.
type Value struct {
	Key    string `json:"key"`
	Value  any    `json:"value"`
	Source string `json:"source"`
	Env    string `json:"env,omitempty"` // the variable, when Source is env
	// The schema's description of the key (T15.15, L179): the engine is the
	// one source of each key's kind, accepted values and default.
	Kind    string   `json:"kind,omitempty"`   // bool, int, enum, name, key, path, paths
	Values  []string `json:"values,omitempty"` // an enum's values, in order
	Default any      `json:"default,omitempty"`
	Min     *int     `json:"min,omitempty"` // an int's bounds; no max is unbounded
	Max     *int     `json:"max,omitempty"`
}

// InvalidError is a value the schema does not accept, or a key it does not
// know: what `set` refuses with exit 2.
type InvalidError struct{ Key, Msg string }

func (e *InvalidError) Error() string { return e.Key + ": " + e.Msg }

// FileError is a settings file that cannot be used: unreadable, malformed,
// or holding values the schema refuses.
type FileError struct {
	Path     string
	Problems []string
}

func (e *FileError) Error() string {
	return e.Path + ": " + strings.Join(e.Problems, "; ")
}

// requiredMsg is the refusal of a required integration off (engram, codegraph).
const requiredMsg = "required for now: Jhon's own memory/indexer will make it optional"

type kind int

const (
	kBool kind = iota
	kInt
	kEnum
	kName  // a profile name
	kKey   // a key binding, or off
	kPath  // an absolute path, or one under ~/
	kPaths // a list of folders, each absolute or under ~/
)

// kindNames are the kinds as `settings list --json` names them.
var kindNames = map[kind]string{kBool: "bool", kInt: "int", kEnum: "enum", kName: "name", kKey: "key", kPath: "path", kPaths: "paths"}

// spec is one key of the schema.
type spec struct {
	key      string
	kind     kind
	def      any
	enum     []string          // kEnum
	aliases  map[string]string // kEnum: an older name, read and set as its value
	min, max int               // kInt; max < 0 is unbounded
	required bool              // only true is accepted, for now
	env      string            // the variable that overrides it
	fromEnv  func(string) (any, bool)
}

var (
	nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	keyRe  = regexp.MustCompile(`^[A-Za-z0-9+\-]+$`)
)

// Environment readers, each as the shell reads its variable today.
func envBoolIs1(v string) (any, bool) { return v == "1", v != "" }
func envOnOff(v string) (any, bool)   { return v == "on", v == "on" || v == "off" }
func envKey(v string) (any, bool) {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "off") {
		return "off", true
	}
	return v, keyRe.MatchString(v)
}

// envStall is JHON_SHELL_STALL_SECONDS and JHON_SHELL_THINKING_STALL_SECONDS:
// a whole number of seconds, 0 or more.
func envStall(v string) (any, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	return n, err == nil && n >= 0 && !strings.HasPrefix(strings.TrimSpace(v), "+")
}

// envCompactAt is JHON_SHELL_COMPACT_AT: off or 0 leaves compaction to pi,
// which 100 says; a whole number from 1 to 99 is the share.
func envCompactAt(v string) (any, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "off" || v == "0" {
		return 100, true
	}
	n, err := strconv.Atoi(v)
	return n, err == nil && n > 0 && n < 100
}

// schema is every key, in the order list shows them, read each time from
// the integrations' manifests (a test's set too: integrations.Use).
func schema() []spec {
	s := []spec{
		{key: "runtimes.claude_code", kind: kBool, def: true},
		{key: "runtimes.pi", kind: kBool, def: true},
	}
	// The integrations' own keys, integrations.<name>, by their manifests'
	// order (integrations/<name>/manifest.yaml, T58a): a required one takes
	// only true; an optional one false turns it off as its manifest's
	// when_off says (T58b, L1167). A manifest setting in a runtime's
	// namespace comes later (manifestKeys).
	s = append(s, manifestKeys(func(m integrations.Manifest) bool { return m.Setting == "integrations."+m.Name })...)
	s = append(s, []spec{
		{key: "integrations.telemetry", kind: kBool, def: false, env: "JHON_TELEMETRY", fromEnv: envOnOff},
		// T15.7 (L97): one palette, Jhon's own Rose Ink or the desktop palette
		// file (T50f: "ryoku" is its older name).
		{key: "integrations.theme.palette", kind: kEnum, def: "rose-ink", enum: []string{"rose-ink", "desktop"}, aliases: map[string]string{"ryoku": "desktop"}},
	}...)
	// The integrations' settings in a runtime's namespace, by their
	// manifests' order: m365's claude_code.connectors (T42a, L403), the
	// claude.ai account connectors in a Claude Code Jhon session, off by
	// default (jhon start keeps them out). It stays a key, first and off by
	// default, in a set where no manifest names it (T58c, L1193).
	runtimeKeys := manifestKeys(func(m integrations.Manifest) bool { return m.Setting != "integrations."+m.Name })
	if !slices.ContainsFunc(runtimeKeys, func(k spec) bool { return k.key == Connectors }) {
		runtimeKeys = append([]spec{{key: Connectors, kind: kBool, def: false}}, runtimeKeys...)
	}
	s = append(s, runtimeKeys...)
	s = append(s, []spec{
		// T135 (L1055): in both runtimes, a main session's turn that ends with a
		// reply announcing an action of its own, undone, is sent back once to
		// do it or say why not (hook/announcestop.go); false lets it end.
		{key: "agents.act_on_announce", kind: kBool, def: true},
		// T128a (L872, odd/proposals/t128-sandbox.md): whether the runtimes run
		// each model-chosen command in the OS sandbox, on by default where
		// the sandbox can run since T128f (sandbox.Mode reads the default as
		// off where the probe starts no sandbox, and off Linux, D3), and the
		// folders a sandboxed command may write besides the project's.
		{key: "sandbox.mode", kind: kEnum, def: "on", enum: []string{"on", "off"}},
		{key: "sandbox.allow_write", kind: kPaths, def: []string{}},
		{key: "pi.profile", kind: kName, def: "default"},
		{key: "shell.appearance.animations", kind: kEnum, def: "full", enum: []string{"full", "off"}},
		{key: "shell.appearance.ascii", kind: kBool, def: false, env: "JHON_SHELL_ASCII", fromEnv: envBoolIs1},
		{key: "shell.appearance.icons", kind: kEnum, def: "geometric", enum: []string{"geometric", "emoji"}},
		{key: "shell.appearance.status", kind: kEnum, def: "auto", enum: []string{"auto", "hidden"}},
		// T15.12d (L153 answers 1-2): the Gentle Shell's card style and chrome surface, its defaults first.
		{key: "shell.appearance.cards", kind: kEnum, def: "float", enum: []string{"float", "neon"}},
		{key: "shell.appearance.surface", kind: kEnum, def: "transparent", enum: []string{"transparent", "tinted"}},
		// L195: the pane's density; compact by default, the owner's choice (L1498).
		{key: "shell.appearance.density", kind: kEnum, def: "compact", enum: []string{"comfortable", "compact", "minimal"}},
		// T155 (L1377, L1394): the context ladder is these two keys. The main
		// session compacts at compact_at of its window or past compact_tokens,
		// whichever is first (T115a, T121); the fresh-session notice comes ten
		// points of the window before compact_at (at least 1%) or at three
		// quarters of compact_tokens; the delegation notice (T115e) at half of
		// compact_tokens or 15% of the window. compact_tokens 0 turns every
		// token rung off. fresh_session_at, fresh_session_tokens and
		// backstop_tokens are retired (retired.go).
		{key: "shell.behaviour.compact_at", kind: kInt, def: 60, min: 1, max: 100, env: "JHON_SHELL_COMPACT_AT", fromEnv: envCompactAt},
		{key: "shell.behaviour.compact_tokens", kind: kInt, def: 200000, min: 0, max: -1},
		// T122 (L762 F2): past this many tokens or 40% of the window, whichever
		// is first, a sub-agent child hears once to wrap up and return
		// path:line evidence; 0 turns it off. T155 dropped the cap that refused
		// its tool calls (child_cap_tokens, retired): a context is the owner's.
		{key: "shell.behaviour.child_backstop_tokens", kind: kInt, def: 80000, min: 0, max: -1},
		// The seconds of API silence before the shell aborts a turn and sends one
		// continue, on a direct API provider with thinking off or minimal; 0 turns
		// all stall recovery off. On claude-bridge or while the model thinks the
		// threshold is thinking_stall_seconds instead, 0 there never recovering (T38, L382).
		{key: "shell.behaviour.stall_seconds", kind: kInt, def: 75, min: 0, max: -1, env: "JHON_SHELL_STALL_SECONDS", fromEnv: envStall},
		{key: "shell.behaviour.thinking_stall_seconds", kind: kInt, def: 300, min: 0, max: -1, env: "JHON_SHELL_THINKING_STALL_SECONDS", fromEnv: envStall},
		{key: "shell.behaviour.double_esc_cancel", kind: kBool, def: false, env: "JHON_SHELL_DOUBLE_ESC_CANCEL", fromEnv: envOnOff},
		{key: "shell.keys.panel", kind: kKey, def: "alt+j", env: "JHON_SHELL_JHON_KEY", fromEnv: envKey},
		// T167 (L1438): the key that opens /jhon:agents, shell.keys.tasks until
		// v0.4 (retired.go moves an old value); its variable keeps its name.
		{key: "shell.keys.agents", kind: kKey, def: "alt+s", env: "JHON_SHELL_TASKS_KEY", fromEnv: envKey},
		// T174-S5b (L1495): the key that opens /jhon:jobs; alt+o, which pi
		// binds nowhere and the shell's other keys do not take.
		{key: "shell.keys.jobs", kind: kKey, def: "alt+o", env: "JHON_SHELL_JOBS_KEY", fromEnv: envKey},
		// T174-S5b (L1493-L1495): the usage view (/jhon:usage) and the todo
		// list's fold, back after T174 S1 had retired them.
		{key: "shell.keys.usage", kind: kKey, def: "alt+u", env: "JHON_SHELL_USAGE_KEY", fromEnv: envKey},
		{key: "shell.keys.todos", kind: kKey, def: "alt+t", env: "JHON_SHELL_TODO_KEY", fromEnv: envKey},
		// T169 (L1438): shell.keys.rail and shell.keys.commands went with the
		// panel sheet and the command palette; T174 (L1462): shell.keys.changes
		// with the changes view (retired.go).
	}...)
	return s
}

// Connectors is the switch of the claude.ai account connectors in a Claude
// Code Jhon session (T42a, L403): m365's manifest setting, and a key
// whether or not a manifest names it, since jhon start keeps the
// connectors out unless it is on (T58c, L1193).
const Connectors = "claude_code.connectors"

// manifestKeys are the boolean keys of the manifests which, by Order, a
// required one taking only true.
func manifestKeys(which func(integrations.Manifest) bool) []spec {
	var s []spec
	for _, m := range integrations.All() {
		if m.Setting != "" && which(m) {
			s = append(s, spec{key: m.Setting, kind: kBool, def: m.Default, required: m.Required})
		}
	}
	return s
}

// extraPrefix is the passthrough section T15.3 removed (retired.go reads what it held).
const extraPrefix = "shell.extra."

// Keys are the schema's keys, in order.
func Keys() []string {
	keys := schema()
	out := make([]string, len(keys))
	for i, s := range keys {
		out[i] = s.key
	}
	return out
}

func lookup(key string) (spec, bool) {
	if path, err := splitKey(key); err == nil {
		key = joinKey(path)
	}
	for _, s := range schema() {
		if s.key == key {
			return s, true
		}
	}
	return spec{}, false
}

// Accepted is what a key accepts, in words.
func (s spec) accepted() string {
	if s.required {
		return "true (required for now)"
	}
	switch s.kind {
	case kBool:
		return "true, false"
	case kInt:
		if s.max < 0 {
			return fmt.Sprintf("a whole number, %d or more", s.min)
		}
		return fmt.Sprintf("a whole number from %d to %d", s.min, s.max)
	case kEnum:
		var out []string
		for _, e := range s.enum {
			if e == "" {
				e = `"" (none)`
			}
			for old, now := range s.aliases {
				if now == e {
					e += " (" + old + ", its older name)"
				}
			}
			out = append(out, e)
		}
		return strings.Join(out, ", ")
	case kName:
		return "a profile name (letters, digits, '.', '_', '-')"
	case kKey:
		return "a key such as alt+j, or off"
	case kPath:
		return "an absolute path, or one under ~/"
	case kPaths:
		return "a list of folders, each an absolute path or one under ~/, never /, the home folder or one above it; jhon settings set takes them separated by commas"
	}
	return ""
}

// paths is v as a kPaths key holds it: every entry as written, each one a
// folder the key accepts. v is a TOML array, or the list a command line gave.
func (s spec) paths(v any) (any, error) {
	var items []any
	switch x := v.(type) {
	case []any:
		items = x
	case []string:
		for _, p := range x {
			items = append(items, p)
		}
	default:
		return nil, &InvalidError{s.key, fmt.Sprintf("%s is not accepted (accepted: %s)", show(v), s.accepted())}
	}
	out := []string{}
	for _, item := range items {
		p, ok := item.(string)
		if !ok || !openable(p) {
			return nil, &InvalidError{s.key, fmt.Sprintf("%s is not accepted (accepted: %s)", show(item), s.accepted())}
		}
		out = append(out, p)
	}
	return out, nil
}

// openable is whether p names a folder a kPaths key may hold: absolute or
// under ~/, and not /, the home folder, or a folder above it.
func openable(p string) bool {
	if !filepath.IsAbs(p) && !strings.HasPrefix(p, "~/") {
		return false
	}
	abs, home := Expand(p), filepath.Clean(project.Home())
	return abs != string(filepath.Separator) && !project.Within(home, abs)
}

// Expand is a settings path made absolute: one under ~/ is under the home
// folder; cleaned.
func Expand(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return filepath.Join(project.Home(), rest)
	}
	return filepath.Clean(p)
}

// Paths is the kPaths key's folders now, each absolute (Expand); none when
// the settings cannot be read.
func Paths(key string) []string {
	vals, err := Load(Path(), os.LookupEnv)
	if err != nil {
		return nil
	}
	v, _ := Find(vals, key)
	list, _ := v.Value.([]string)
	var out []string
	for _, p := range list {
		out = append(out, Expand(p))
	}
	return out
}

// same is whether two values of one key are equal; a list is compared entry
// by entry.
func same(a, b any) bool { return reflect.DeepEqual(a, b) }

// Accepted is what key accepts, in words; "" for an unknown key.
func Accepted(key string) string {
	s, ok := lookup(key)
	if !ok {
		return ""
	}
	return s.accepted()
}

// check is v as the key holds it, or the error naming what it accepts.
// v is what TOML decoding gives: bool, int64, string.
func (s spec) check(v any) (any, error) {
	bad := func() error {
		return &InvalidError{s.key, fmt.Sprintf("%s is not accepted (accepted: %s)", show(v), s.accepted())}
	}
	switch s.kind {
	case kBool:
		b, ok := v.(bool)
		if !ok {
			return nil, bad()
		}
		if s.required && !b {
			return nil, &InvalidError{s.key, requiredMsg}
		}
		return b, nil
	case kInt:
		n, ok := v.(int64)
		if !ok || n < int64(s.min) || s.max >= 0 && n > int64(s.max) {
			return nil, bad()
		}
		return int(n), nil
	case kPaths:
		return s.paths(v)
	}
	str, ok := v.(string)
	if !ok {
		return nil, bad()
	}
	switch s.kind {
	case kEnum:
		if now, alias := s.aliases[str]; alias {
			str = now
		}
		ok = slices.Contains(s.enum, str)
	case kName:
		ok = nameRe.MatchString(str)
	case kKey:
		ok = str == "off" || keyRe.MatchString(str)
	case kPath:
		ok = filepath.IsAbs(str) || strings.HasPrefix(str, "~/") && len(str) > len("~/")
	}
	if !ok {
		return nil, bad()
	}
	return str, nil
}

// show is a value as the owner wrote it: a string quoted.
func show(v any) string {
	if s, ok := v.(string); ok {
		return strconv.Quote(s)
	}
	return fmt.Sprint(v)
}

// Path is the settings file: JHON_SETTINGS, else
// $XDG_CONFIG_HOME/jhon/settings.toml, else ~/.config/jhon/settings.toml.
func Path() string {
	if p := os.Getenv("JHON_SETTINGS"); p != "" {
		return p
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "jhon", "settings.toml")
	}
	return filepath.Join(project.Home(), ".config", "jhon", "settings.toml")
}

// fileValues reads and judges a settings file: its values by key, the keys
// sorted, and the retired keys it holds. An absent file is no values.
func fileValues(path string) (map[string]any, []string, []Note, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil, nil, nil
	}
	if err != nil {
		return nil, nil, nil, &FileError{path, []string{err.Error()}}
	}
	return parse(path, string(b))
}

// parse judges a settings document: each key against the schema, a retired
// key as a note (its renamed value read, retired.go), then the rules across
// keys.
func parse(path, text string) (map[string]any, []string, []Note, error) {
	var doc map[string]any
	if _, err := toml.Decode(text, &doc); err != nil {
		return nil, nil, nil, &FileError{path, []string{err.Error()}}
	}
	vals := map[string]any{}
	var order, problems []string
	var notes []Note
	migrated := map[string]any{}
	for _, leaf := range leaves(doc, nil) {
		key, v := joinKey(leaf.path), leaf.value
		if key == "schema" {
			if n, ok := v.(int64); !ok || n != Schema {
				problems = append(problems, fmt.Sprintf("schema: %s is not accepted (accepted: %d)", show(v), Schema))
			}
			continue
		}
		if note, ok := retire(key, v); ok {
			notes = append(notes, note)
			for k, mv := range note.values {
				migrated[k] = mv
			}
			continue
		}
		s, known := lookup(key)
		if !known {
			problems = append(problems, key+": unknown key")
			continue
		}
		got, err := s.check(v)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		vals[key] = got
		order = append(order, key)
	}
	// A retired key whose new key the file sets itself is ignored: the
	// explicit key wins, and migrating only drops the retired one (T15.9).
	for i, n := range notes {
		for k := range n.values {
			if _, set := vals[k]; set {
				notes[i].Ignored, notes[i].Now, notes[i].values = k, "", nil
				delete(migrated, k)
			}
		}
	}
	// A renamed value reads under its key, unless the file sets that key itself.
	for k, mv := range migrated {
		if _, set := vals[k]; !set {
			vals[k] = mv
			order = append(order, k)
		}
	}
	if len(problems) == 0 {
		if err := runtimesRule(func(k string) any {
			if v, ok := vals[k]; ok {
				return v
			}
			s, _ := lookup(k)
			return s.def
		}); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) > 0 {
		return nil, nil, nil, &FileError{path, problems}
	}
	return vals, order, notes, nil
}

// runtimesRule is S38's: at least one runtime is on.
func runtimesRule(get func(string) any) error {
	if get("runtimes.claude_code") == false && get("runtimes.pi") == false {
		return &InvalidError{"runtimes", "at least one of claude_code and pi must be true"}
	}
	return nil
}

// leaf is one decoded value that is not a table, and its key path.
type leaf struct {
	path  []string
	value any
}

// leaves are the decoded document's values, keys sorted at each level. (The
// TOML library's MetaData.Keys at this version aliases the paths it returns,
// so the document is walked instead.)
func leaves(doc map[string]any, prefix []string) []leaf {
	keys := make([]string, 0, len(doc))
	for k := range doc {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var out []leaf
	for _, k := range keys {
		path := append(slices.Clone(prefix), k)
		if sub, ok := doc[k].(map[string]any); ok {
			out = append(out, leaves(sub, path)...)
			continue
		}
		out = append(out, leaf{path, doc[k]})
	}
	return out
}

// Load is every key's effective value, in the schema's order; env looks a
// variable up (os.LookupEnv).
func Load(path string, env func(string) (string, bool)) ([]Value, error) {
	vals, _, _, err := fileValues(path)
	if err != nil {
		return nil, err
	}
	var out []Value
	for _, s := range schema() {
		v := Value{Key: s.key, Value: s.def, Source: SourceDefault, Kind: kindNames[s.kind], Values: s.enum}
		if rt, ok := runtimeOfKey[s.key]; ok {
			v.Value = Detected(rt)
		}
		v.Default = v.Value
		if s.kind == kInt {
			lo, hi := s.min, s.max
			v.Min = &lo
			if hi >= 0 {
				v.Max = &hi
			}
		}
		if fv, ok := vals[s.key]; ok {
			v.Value, v.Source = fv, SourceFile
		}
		if s.env != "" {
			if raw, set := env(s.env); set {
				if ev, ok := s.fromEnv(raw); ok {
					v.Value, v.Source, v.Env = ev, SourceEnv, s.env
				}
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// On is whether the bool key is true now: the file at Path() and this
// process's environment. Settings that cannot be read leave it false, so an
// opt-in integration stays off.
func On(key string) bool {
	vals, err := Load(Path(), os.LookupEnv)
	if err != nil {
		return false
	}
	v, _ := Find(vals, key)
	return v.Value == true
}

// runtimeKeys are each runtime's key under [runtimes], by the manifest's
// runtime name.
var runtimeKeys = map[string]string{"claude-code": "runtimes.claude_code", "pi": "runtimes.pi"}

// runtimeOfKey is runtimeKeys the other way.
var runtimeOfKey = map[string]string{"runtimes.claude_code": "claude-code", "runtimes.pi": "pi"}

// runtimeHome and runtimeCommand are where a runtime is detected: its
// directory under the home (the one its renderer writes into) or its
// command on PATH.
var (
	runtimeHome    = map[string]string{"claude-code": ".claude", "pi": ".pi"}
	runtimeCommand = map[string]string{"claude-code": "claude", "pi": "pi"}
)

// Detected is a runtime key's default (T10g, S4): on only where the runtime
// is present, its directory under the home or its command on PATH, so a
// fresh home gets only the core. A value in the file or the environment
// always wins.
func Detected(runtime string) bool {
	if project.Exists(filepath.Join(project.Home(), runtimeHome[runtime])) {
		return true
	}
	_, err := exec.LookPath(runtimeCommand[runtime])
	return err == nil
}

// RuntimeOn is whether runtime (a manifest runtime name: claude-code or pi)
// is on now, and the key that says so. Settings that cannot be read leave
// each runtime at its default, detection (T10j): a broken file never drops
// a runtime the owner works in, nor adds one that is not there. A runtime
// with no key is on.
func RuntimeOn(runtime string) (on bool, key string) {
	on, key, _ = RuntimeSetting(runtime)
	return on, key
}

// RuntimeSetting is RuntimeOn and where its value comes from: SourceDefault
// is the detection (Detected).
func RuntimeSetting(runtime string) (on bool, key, source string) {
	key, ok := runtimeKeys[runtime]
	if !ok {
		return true, "", SourceDefault
	}
	vals, err := Load(Path(), os.LookupEnv)
	if err != nil {
		// A file that cannot be read sets nothing: the default, detection
		// (T10j), never a runtime that is not there.
		return Detected(runtime), key, SourceDefault
	}
	v, _ := Find(vals, key)
	return v.Value != false, key, v.Source
}

// Str is the string key's value now; its default when the settings cannot
// be read.
func Str(key string) string {
	if vals, err := Load(Path(), os.LookupEnv); err == nil {
		if v, ok := Find(vals, key); ok {
			s, _ := v.Value.(string)
			return s
		}
	}
	s, _ := lookup(key)
	str, _ := s.def.(string)
	return str
}

// Find is the value of key among vals.
func Find(vals []Value, key string) (Value, bool) {
	if path, err := splitKey(key); err == nil {
		key = joinKey(path)
	}
	for _, v := range vals {
		if v.Key == key {
			return v, true
		}
	}
	return Value{}, false
}

// Known is whether key is in the schema.
func Known(key string) bool {
	_, ok := lookup(key)
	return ok
}

// splitKey reads a dotted key: bare segments, "basic" or 'literal' quoted
// ones, spaces around the dots.
func splitKey(s string) ([]string, error) {
	path, rest, err := readKey(s)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(rest) != "" {
		return nil, fmt.Errorf("'%s' is not a key", s)
	}
	return path, nil
}

// readKey reads a key path from the start of s, returning the rest.
func readKey(s string) ([]string, string, error) {
	var path []string
	i := 0
	for {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			return nil, "", fmt.Errorf("'%s' is not a key", s)
		}
		switch s[i] {
		case '"':
			j := i + 1
			var b strings.Builder
			for j < len(s) && s[j] != '"' {
				if s[j] == '\\' && j+1 < len(s) {
					j++
				}
				b.WriteByte(s[j])
				j++
			}
			if j >= len(s) {
				return nil, "", fmt.Errorf("'%s' is not a key", s)
			}
			path, i = append(path, b.String()), j+1
		case '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return nil, "", fmt.Errorf("'%s' is not a key", s)
			}
			path, i = append(path, s[i+1:i+1+j]), i+2+j
		default:
			j := i
			for j < len(s) && isBare(s[j]) {
				j++
			}
			if j == i {
				return nil, "", fmt.Errorf("'%s' is not a key", s)
			}
			path, i = append(path, s[i:j]), j
		}
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i < len(s) && s[i] == '.' {
			i++
			continue
		}
		return path, s[i:], nil
	}
}

func isBare(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

// quoteSegment is a key segment as TOML writes it: bare when it can be.
func quoteSegment(s string) string {
	for i := 0; i < len(s); i++ {
		if !isBare(s[i]) {
			return tomlString(s)
		}
	}
	if s == "" {
		return `""`
	}
	return s
}

func joinKey(path []string) string {
	out := make([]string, len(path))
	for i, p := range path {
		out[i] = quoteSegment(p)
	}
	return strings.Join(out, ".")
}

// tomlString is s as a TOML basic string.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// tomlValue is v as TOML writes it.
func tomlValue(v any) string {
	switch x := v.(type) {
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case string:
		return tomlString(x)
	case []string:
		items := make([]string, len(x))
		for i, p := range x {
			items[i] = tomlString(p)
		}
		return "[" + strings.Join(items, ", ") + "]"
	}
	return tomlString(fmt.Sprint(v))
}
