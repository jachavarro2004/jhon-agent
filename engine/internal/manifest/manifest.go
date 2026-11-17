// Package manifest is agents.yaml: the agents, their tiers and
// capabilities, and the runtime capability model. One reader serves the renderer, the profiles, and the
// reviews; it reports a line it cannot place, and the renderer's validation
// refuses it.
package manifest

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// Map is a JavaScript object's entries in insertion order: a key set again
// keeps its place. agents.yaml's keys are names, never array indices, so
// insertion order is Object.keys order.
type Map[V any] struct {
	keys []string
	vals map[string]V
}

// Set is m[k] = v.
func (m *Map[V]) Set(k string, v V) {
	if m.vals == nil {
		m.vals = map[string]V{}
	}
	if _, ok := m.vals[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.vals[k] = v
}

// Get is m[k], and whether m holds k.
func (m Map[V]) Get(k string) (V, bool) { v, ok := m.vals[k]; return v, ok }

// Has is whether m holds k.
func (m Map[V]) Has(k string) bool { _, ok := m.vals[k]; return ok }

// Keys is Object.keys(m).
func (m Map[V]) Keys() []string { return m.keys }

// Len is the number of entries.
func (m Map[V]) Len() int { return len(m.keys) }

// Agent is one entry of `agents:`. An absent key is "" (a key given no
// value is reported and left unset, so "" never stands for a value), except
// Description and Vars, whose absence is distinct.
type Agent struct {
	Name string
	// Tier is "" when the agent declares none.
	Tier string
	// Effort overrides the Claude Code tier's effort for this role (docket AX).
	Effort string
	// Caps is nil when the agent declares no caps.
	Caps []string
	Note string
	// Description is the JSON value of `description:`, js.Undefined{} when absent.
	Description any
	// Body is the prompt file relative to the repository root; "" is agents/<name>.md.
	Body string
	// Mode "primary" is user-selectable and not hidden.
	Mode string
	// Template is a body template under agents/templates/, without `.md`.
	Template string
	// Vars are the template's `{{name}}` values; nil when the agent has no `vars:`.
	Vars *Map[string]
	// MaxTurns is Claude Code `maxTurns`.
	MaxTurns string
	// OmitClaudeMd is Claude Code `omitClaudeMd`.
	OmitClaudeMd string
	// Bounded `true` keeps a writer to its handoff's edit surfaces (the scope guard, internal/hook).
	Bounded string
}

// TierName is the tier as a JavaScript property key: "undefined" when absent.
func (a Agent) TierName() string {
	if a.Tier == "" {
		return "undefined"
	}
	return a.Tier
}

// Issue is a manifest line the reader cannot place.
type Issue struct {
	Line int
	Msg  string
}

// Manifest is agents.yaml as Parse reads it, with the
// runtime capability model (Matrix) of the same text.
type Manifest struct {
	// Tiers is each runtime's model value per tier.
	Tiers        Map[Map[string]]
	Capabilities Map[[]string]
	Agents       []Agent
	InspectAllow []string
	Issues       []Issue
	Matrix       Matrix
}

// Load reads <root>/agents.yaml.
func Load(root string) (Manifest, error) {
	b, err := os.ReadFile(filepath.Join(root, "prose", "agents.yaml"))
	if err != nil {
		return Manifest{}, js.FSError(err)
	}
	return Parse(js.DecodeUTF8(b)), nil
}

// MustLoad is Load, or a panic: nothing that needs the manifest runs
// without it.
func MustLoad(root string) Manifest {
	m, err := Load(root)
	if err != nil {
		panic(err.Error())
	}
	return m
}

var (
	// TopLevelKeys are the keys this reader understands; anything else is
	// reported, never skipped.
	TopLevelKeys = append([]string{"version", "tiers", "capabilities", "inspect_allow", "agents"}, MatrixKeys...)
	listKeys     = []string{"inspect_allow"}
	agentKeys    = []string{"name", "tier", "effort", "caps", "note", "description", "body", "mode", "template", "vars", "maxTurns", "omitClaudeMd", "bounded"}
)

var (
	manifestComment = lazyre.New(js.Space + `+#` + js.Dot + `*$`)
	topLevel        = lazyre.New(`^\w+:`)
	topList         = lazyre.New(`^([\w-]+):` + js.Space + `*\[(` + js.Dot + `*)\]`)
	tierLine        = lazyre.New(`^` + js.Space + `+([\w-]+):` + js.Space + `*(\{` + js.Dot + `*\})`)
	capLine         = lazyre.New(`^` + js.Space + `+([\w-]+):` + js.Space + `*\[(` + js.Dot + `*)\]`)
	agentStart      = lazyre.New(`^` + js.Space + `+-` + js.Space + `+name:` + js.Space + `*(` + js.Dot + `+)$`)
	varsEntry       = lazyre.New(`^` + js.Space + `+([\w-]+):` + js.Space + `*(` + js.Dot + `+)$`)
	agentKey        = lazyre.New(`^` + js.Space + `+(\w+):` + js.Space + `*(` + js.Dot + `*)$`)
	inlineBraces    = lazyre.New(`^\{|\}$`)
	listBrackets    = lazyre.New(`^\[|\]$`)
)

// Parse reads agents.yaml's text line by line, and its matrix sections
// into the runtime capability model.
func Parse(src string) Manifest {
	m := Manifest{InspectAllow: []string{}}
	section := ""
	var cur *Agent
	varsIndent := -1
	flush := func() {
		if cur != nil && cur.Name != "" {
			m.Agents = append(m.Agents, *cur)
		}
		cur = nil
	}
	for i, raw := range strings.Split(src, "\n") {
		line := manifestComment.ReplaceAllString(raw, "")
		if js.Trim(line) == "" || strings.HasPrefix(trimStart(line), "#") {
			continue
		}
		bad := func(msg string) { m.Issues = append(m.Issues, Issue{i + 1, msg}) }
		if topLevel.MatchString(line) {
			flush()
			varsIndent = -1
			section, _, _ = strings.Cut(line, ":")
			if !slices.Contains(TopLevelKeys, section) {
				bad(fmt.Sprintf("unknown top-level key '%s'", section))
			}
			if l := topList.FindStringSubmatch(line); l != nil && slices.Contains(listKeys, l[1]) {
				m.InspectAllow = splitList(l[2])
			}
			continue
		}
		switch {
		case section == "tiers":
			if t := tierLine.FindStringSubmatch(line); t != nil {
				m.Tiers.Set(t[1], inline(t[2]))
			} else {
				bad("cannot parse tiers entry: " + js.Trim(line))
			}
		case section == "capabilities":
			if c := capLine.FindStringSubmatch(line); c != nil {
				m.Capabilities.Set(c[1], splitList(c[2]))
			} else {
				bad("cannot parse capabilities entry: " + js.Trim(line))
			}
		case section == "agents":
			if s := agentStart.FindStringSubmatch(line); s != nil {
				flush()
				cur = &Agent{Name: js.Trim(s[1]), Description: js.Undefined{}}
				varsIndent = -1
				continue
			}
			indent := js.Len(line) - js.Len(trimStart(line))
			if varsIndent >= 0 && indent > varsIndent && cur != nil {
				kv := varsEntry.FindStringSubmatch(line)
				if kv == nil {
					bad("cannot parse vars entry: " + js.Trim(line))
					continue
				}
				if v, ok := scalar(kv[2]); ok {
					cur.Vars.Set(kv[1], v)
				} else {
					bad("cannot parse vars value: " + js.Trim(line))
				}
				continue
			}
			varsIndent = -1
			kv := agentKey.FindStringSubmatch(line)
			if cur == nil || kv == nil {
				bad("cannot parse: " + js.Trim(line))
				continue
			}
			k, v := kv[1], kv[2]
			if !slices.Contains(agentKeys, k) {
				bad(fmt.Sprintf("%s: unknown key '%s'", cur.Name, k))
				continue
			}
			switch {
			case k == "vars":
				if js.Trim(v) != "" {
					bad(cur.Name + ": vars must be a block of 'name: value' lines")
					continue
				}
				cur.Vars = &Map[string]{}
				varsIndent = indent
			case js.Trim(v) == "":
				bad(fmt.Sprintf("%s: '%s' has no value", cur.Name, k))
			case k == "caps":
				cur.Caps = splitList(listBrackets.ReplaceAllString(v, ""))
			case k == "description":
				d, err := js.ParseJSON(v)
				if err != nil {
					bad(cur.Name + ": description is not a JSON string")
				} else {
					cur.Description = d
				}
			default:
				cur.set(k, js.Trim(v))
			}
		case slices.Contains(MatrixKeys, section):
			// The runtime capability model: ParseMatrix reads it.
		case section != "version":
			bad("cannot parse: " + js.Trim(line))
		}
	}
	flush()
	m.Matrix = ParseMatrix(src)
	return m
}

// set assigns one of the agent's plain string keys.
func (a *Agent) set(k, v string) {
	switch k {
	case "name":
		a.Name = v
	case "tier":
		a.Tier = v
	case "effort":
		a.Effort = v
	case "note":
		a.Note = v
	case "body":
		a.Body = v
	case "mode":
		a.Mode = v
	case "template":
		a.Template = v
	case "maxTurns":
		a.MaxTurns = v
	case "omitClaudeMd":
		a.OmitClaudeMd = v
	case "bounded":
		a.Bounded = v
	}
}

// scalar is a vars value: JSON when it starts with a double quote (so it
// may hold commas and escapes), else trimmed; false when the JSON is not a
// string.
func scalar(v string) (string, bool) {
	if !strings.HasPrefix(v, `"`) {
		return js.Trim(v), true
	}
	x, err := js.ParseJSON(v)
	s, ok := x.(string)
	return s, err == nil && ok
}

// splitList is `a, b, c` as its trimmed, non-empty items.
func splitList(s string) []string {
	out := []string{}
	for _, x := range strings.Split(s, ",") {
		if x = js.Trim(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// trimStart is String.prototype.trimStart.
func trimStart(s string) string { return strings.TrimLeftFunc(s, js.IsSpace) }

// inline is a `{ k: v, k: v }` map, split at every comma and first colon.
func inline(s string) Map[string] {
	var out Map[string]
	for _, kv := range strings.Split(inlineBraces.ReplaceAllString(s, ""), ",") {
		k, v, ok := strings.Cut(kv, ":")
		if !ok { // kv.slice(0, -1) and kv.slice(0)
			u := utf16.Encode([]rune(kv))
			if len(u) > 0 {
				u = u[:len(u)-1]
			}
			k, v = string(utf16.Decode(u)), kv
		}
		out.Set(js.Trim(k), js.Trim(v))
	}
	return out
}

// TierTable is the tiers as plain maps, runtime → tier → value, for
// lookups that need no order (profiles.ModelFor).
func (m Manifest) TierTable() map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, rt := range m.Tiers.Keys() {
		row, _ := m.Tiers.Get(rt)
		out[rt] = map[string]string{}
		for _, t := range row.Keys() {
			out[rt][t], _ = row.Get(t)
		}
	}
	return out
}

// TierNames is the union of the tier names the runtimes declare, in first
// appearance order.
func (m Manifest) TierNames() []string {
	var out []string
	for _, rt := range m.Tiers.Keys() {
		row, _ := m.Tiers.Get(rt)
		for _, t := range row.Keys() {
			if !slices.Contains(out, t) {
				out = append(out, t)
			}
		}
	}
	return out
}

// AgentNames are the agents' names, in order.
func (m Manifest) AgentNames() []string {
	out := make([]string, len(m.Agents))
	for i, a := range m.Agents {
		out[i] = a.Name
	}
	return out
}

// Find is the first agent named name.
func (m Manifest) Find(name string) (Agent, bool) {
	for _, a := range m.Agents {
		if a.Name == name {
			return a, true
		}
	}
	return Agent{}, false
}

// Tier is the value of tier in runtime's row; "" when either is absent.
func (m Manifest) Tier(runtime, tier string) string {
	row, _ := m.Tiers.Get(runtime)
	v, _ := row.Get(tier)
	return v
}

// MustLoadRelease is the manifest of the release the binary belongs to
// (contract.Root), or a panic.
func MustLoadRelease() Manifest {
	root, err := contract.Root()
	if err != nil {
		panic(err)
	}
	return MustLoad(root)
}
