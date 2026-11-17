// Package profiles is the owner's model profiles.
//
// agents.yaml assigns each agent a tier and each runtime a model per tier.
// A profile overrides that without touching the framework: a runtime's tier
// (`tiers.<runtime>.<tier>`), or one agent's model in one runtime
// (`agents.<agent>.<runtime>`), each written `model` or `model@effort` as in
// agents.yaml. The profiles and the active one live in the owner's
// ~/.config/jhon/profiles.json (JHON_PROFILES overrides the path); `default`
// is agents.yaml itself, and an absent file means `default`. The renderer reads
// the active profile when it projects a runtime, so an agent's model is, in
// order: the profile's agent override, the profile's tier, agents.yaml.
//
// The file is held as js.ParseJSON reads it: a profile is whatever the
// owner wrote, judged by Problems, and written back in its own key order by
// WriteActive. The command is internal/cli, cmd_profile.go.
package profiles

import (
	"errors"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// Default is the profile that is agents.yaml itself.
const Default = "default"

// The delegation modes a profile gives the orchestrator (T117): Inline, the
// default and never written to the file, lets it write small work itself;
// Writer has it read and plan while workers write.
const (
	Inline = "inline"
	Writer = "writer"
)

// Delegations are the delegation values, the default first.
var Delegations = []string{Inline, Writer}

// DelegationProblem is why value cannot be a delegation mode; "" when it can.
func DelegationProblem(value any) string {
	if s, ok := value.(string); ok && slices.Contains(Delegations, s) {
		return ""
	}
	return fmt.Sprintf("delegation '%s' is not one of %s", js.String(value), strings.Join(Delegations, ", "))
}

// Delegation is profile's delegation mode: its `delegation`, else Inline.
func Delegation(profile any) string {
	if s, ok := js.Prop(profile, "delegation").(string); ok && slices.Contains(Delegations, s) {
		return s
	}
	return Inline
}

// SetDelegation sets profile's delegation mode; Inline, the default, deletes
// the key.
func SetDelegation(root *js.Object, profile, mode string) error {
	p, ok := js.Prop(profilesOf(root), profile).(*js.Object)
	if !ok {
		return fmt.Errorf("no profile '%s'", profile)
	}
	if why := DelegationProblem(mode); why != "" {
		return errors.New(why)
	}
	if mode == Inline {
		p.Delete("delegation")
	} else {
		p.Set("delegation", mode)
	}
	return nil
}

// Effort is the word a runtime gives a model's effort, and its values.
type Effort struct {
	Word   string
	Values []string
}

// Efforts are the effort values each runtime documents; a runtime absent
// here takes any effort.
var Efforts = map[string]Effort{
	"claude-code": {"effort", []string{"low", "medium", "high", "xhigh", "max"}},
	"pi":          {"thinking", []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}},
}

// Tier is a tier value split: `model`, or `model@effort` (docket AX2).
// Claude Code receives its effort as `effort:`
// frontmatter, and pi as `thinking:`.
type Tier struct {
	Model, Effort string // Effort is "" for a bare model
}

// TierValue splits a tier value at its first `@`; false when either side is
// empty.
func TierValue(v string) (Tier, bool) {
	model, effort, at := strings.Cut(v, "@")
	if !at {
		return Tier{Model: v}, v != ""
	}
	return Tier{model, effort}, model != "" && effort != ""
}

// HomeDir is os.homedir(): HOME when set, else the user's entry.
func HomeDir() string {
	if h, ok := os.LookupEnv("HOME"); ok {
		return h
	}
	if u, err := user.Current(); err == nil {
		return u.HomeDir
	}
	return ""
}

// Path is the profiles file: JHON_PROFILES, else ~/.config/jhon/profiles.json.
func Path() string {
	if p := os.Getenv("JHON_PROFILES"); p != "" {
		return p
	}
	return filepath.Join(HomeDir(), ".config", "jhon", "profiles.json")
}

// File is a profiles file: the active profile's name ("" with HasActive
// false when it names none) and the profiles, by name.
type File struct {
	Active    string
	HasActive bool
	Profiles  *js.Object
}

// Names are the profile names a user may choose: default, then the file's.
func (f File) Names() []string { return append([]string{Default}, f.Profiles.Keys()...) }

func exists(file string) bool { _, err := os.Stat(file); return err == nil }

// Read is the profiles at file, and why it cannot be read; an absent file
// is no profiles and no problem.
func Read(file string) (File, []string) {
	empty := File{Profiles: js.NewObject()}
	if !exists(file) {
		return empty, nil
	}
	b, err := os.ReadFile(file)
	var raw any
	if err == nil {
		raw, err = js.ParseJSON(js.DecodeUTF8(b))
	}
	if err != nil {
		return empty, []string{file + " is not valid JSON"}
	}
	o, ok := raw.(*js.Object)
	if !ok {
		return empty, []string{file + " must hold a JSON object"}
	}
	profiles, active := js.Prop(o, "profiles"), js.Prop(o, "active")
	if _, undef := profiles.(js.Undefined); !undef && !js.IsObject(profiles) {
		return empty, []string{file + ": profiles must be an object"}
	}
	out := File{Profiles: js.NewObject()}
	if p, ok := profiles.(*js.Object); ok {
		out.Profiles = p
	}
	switch a := active.(type) {
	case js.Undefined:
	case string:
		out.Active, out.HasActive = a, true
	default:
		return empty, []string{file + ": active must be a profile name"}
	}
	return out, nil
}

// Active is the active profile's name and value: the file's `active`, or
// default (agents.yaml, no overrides, an empty object). A name the file
// lacks is no overrides too.
func Active(f File) (string, any) {
	name := Default
	if f.HasActive {
		name = f.Active
	}
	if name == Default {
		return name, js.NewObject()
	}
	if p := js.Prop(f.Profiles, name); !js.Nullish(p) {
		return name, p
	}
	return name, js.NewObject()
}

// Context is what a profile is judged against: the manifest's runtimes,
// tier names, and agent names.
type Context struct {
	Runtimes, Tiers, Agents []string
}

// valueProblem is why value cannot stand for runtime; "" when it can.
func valueProblem(value any, runtime string) string {
	s, isString := value.(string)
	t, ok := TierValue(s)
	if !isString || !ok {
		return "write the value as model or model@effort"
	}
	if e, has := Efforts[runtime]; has && t.Effort != "" && !slices.Contains(e.Values, t.Effort) {
		return fmt.Sprintf("%s '%s' is not one of %s", e.Word, t.Effort, strings.Join(e.Values, ", "))
	}
	return ""
}

// Warnings are what a profile names that the manifest no longer has: an
// agent since retired. Its entry is ignored and nothing stops (L48).
func Warnings(f File, ctx Context) []string {
	var out []string
	for _, name := range f.Profiles.Keys() {
		p, ok := js.Prop(f.Profiles, name).(*js.Object)
		if !ok {
			continue
		}
		agents, ok := js.Prop(p, "agents").(*js.Object)
		if !ok {
			continue
		}
		for _, a := range agents.Keys() {
			if !slices.Contains(ctx.Agents, a) {
				out = append(out, fmt.Sprintf("%s.agents: %s is not an agent; its entry is ignored", name, a))
			}
		}
	}
	return out
}

// Problems are every problem of the profiles against the manifest's
// runtimes, tiers, and agents; a retired agent is a warning instead.
func Problems(f File, ctx Context) []string {
	var out []string
	if f.HasActive && f.Active != Default && !f.Profiles.Has(f.Active) && !objectPrototype[f.Active] {
		out = append(out, fmt.Sprintf("active profile '%s' is not defined", f.Active))
	}
	if f.Profiles.Has(Default) {
		out = append(out, fmt.Sprintf("'%s' is agents.yaml itself; define no profile by that name", Default))
	}
	for _, name := range f.Profiles.Keys() {
		p, ok := js.Prop(f.Profiles, name).(*js.Object)
		if !ok {
			out = append(out, name+": a profile must be an object")
			continue
		}
		for _, k := range p.Keys() {
			if k == "delegation" {
				if v := js.Prop(p, k); DelegationProblem(v) != "" {
					out = append(out, fmt.Sprintf("%s.delegation: '%s' is not one of %s", name, js.String(v), strings.Join(Delegations, ", ")))
				}
			} else if k != "tiers" && k != "agents" {
				out = append(out, fmt.Sprintf("%s: unknown key '%s'", name, k))
			}
		}
		out = append(out, sectionProblems(name, "tiers", js.Prop(p, "tiers"), ctx)...)
		out = append(out, sectionProblems(name, "agents", js.Prop(p, "agents"), ctx)...)
	}
	return out
}

// sectionProblems judges a profile's `tiers` (runtime → tier → value) or
// `agents` (agent → runtime → value).
func sectionProblems(name, key string, section any, ctx Context) []string {
	if _, undef := section.(js.Undefined); undef {
		return nil
	}
	s, ok := section.(*js.Object)
	if !ok {
		return []string{fmt.Sprintf("%s.%s: must be an object", name, key)}
	}
	var out []string
	for _, outer := range s.Keys() {
		if key == "tiers" && !slices.Contains(ctx.Runtimes, outer) {
			out = append(out, fmt.Sprintf("%s.tiers: %s is not a runtime", name, outer))
			continue
		}
		if key == "agents" && !slices.Contains(ctx.Agents, outer) {
			// An agent the manifest no longer has is a warning (Warnings).
			continue
		}
		inner, ok := js.Prop(s, outer).(*js.Object)
		if !ok {
			out = append(out, fmt.Sprintf("%s.%s.%s: must be an object", name, key, outer))
			continue
		}
		for _, k := range inner.Keys() {
			runtime := k
			if key == "tiers" {
				runtime = outer
				if !slices.Contains(ctx.Tiers, k) {
					out = append(out, fmt.Sprintf("%s.tiers.%s: %s is not a tier", name, outer, k))
					continue
				}
			} else if !slices.Contains(ctx.Runtimes, k) {
				out = append(out, fmt.Sprintf("%s.agents.%s: %s is not a runtime", name, outer, k))
				continue
			}
			if why := valueProblem(js.Prop(inner, k), runtime); why != "" {
				out = append(out, fmt.Sprintf("%s.%s.%s.%s: %s", name, key, outer, k, why))
			}
		}
	}
	return out
}

// objectPrototype are the names `name in {}` finds on every JavaScript
// object, which the check for an undefined active profile passes over.
var objectPrototype = map[string]bool{
	"constructor": true, "__proto__": true, "hasOwnProperty": true, "isPrototypeOf": true,
	"propertyIsEnumerable": true, "toString": true, "valueOf": true, "toLocaleString": true,
	"__defineGetter__": true, "__defineSetter__": true, "__lookupGetter__": true, "__lookupSetter__": true,
}

// Agent is what ModelFor needs of an agent: its name, and its tier ("" when
// it declares none).
type Agent struct {
	Name, Tier string
}

// Model is an agent's model value in a runtime and where it comes from.
// Value is a string unless the profile holds something else there.
type Model struct {
	Value  any
	Source string
}

// ModelFor is an agent's model value in runtime under profile: the
// profile's own value for the agent, else the profile's tier, else
// agents.yaml's; false when nothing declares one.
func ModelFor(tiers map[string]map[string]string, profile any, runtime string, agent Agent) (Model, bool) {
	tier := agent.Tier
	if tier == "" {
		tier = "undefined"
	}
	if own := js.Prop(js.Prop(js.Prop(profile, "agents"), agent.Name), runtime); js.Truthy(own) {
		return Model{own, fmt.Sprintf("profile agent %s.%s", agent.Name, runtime)}, true
	}
	if t := js.Prop(js.Prop(js.Prop(profile, "tiers"), runtime), tier); js.Truthy(t) {
		return Model{t, fmt.Sprintf("profile tier %s.%s", runtime, tier)}, true
	}
	if base := tiers[runtime][tier]; base != "" {
		return Model{base, fmt.Sprintf("agents.yaml tiers.%s.%s", runtime, tier)}, true
	}
	return Model{}, false
}

// TiersOnly is {tiers: profile.tiers}: the profile without its agent
// overrides, for a review whose tier passes over judge B's own model.
func TiersOnly(profile any) any { return js.Obj("tiers", js.Prop(profile, "tiers")) }

// WriteActive writes `active` into the file, keeping the rest of it, in its
// order; atomic.
func WriteActive(file, name string) error {
	return Edit(file, func(root *js.Object) error {
		root.Set("active", name)
		return nil
	})
}

// Edit reads the profiles file (an absent one is no profiles), lets edit
// change its object, and writes it back through a temporary file renamed
// into place; nothing is written when edit fails.
func Edit(file string, edit func(root *js.Object) error) error {
	prev := js.Obj("profiles", js.NewObject())
	if exists(file) {
		b, err := os.ReadFile(file)
		if err != nil {
			return js.FSError(err)
		}
		v, err := js.ParseJSON(js.DecodeUTF8(b))
		if err != nil {
			return err
		}
		o, ok := v.(*js.Object)
		if !ok {
			return fmt.Errorf("%s must hold a JSON object", file)
		}
		prev = o.Clone()
	}
	if err := edit(prev); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o777); err != nil {
		return js.FSError(err)
	}
	tmp := file + "." + strconv.Itoa(os.Getpid()) + ".tmp"
	if err := os.WriteFile(tmp, []byte(js.StringifyValue(prev)+"\n"), 0o666); err != nil {
		return js.FSError(err)
	}
	return js.FSError(os.Rename(tmp, file))
}

// profileName is a name a new profile may take.
var profileName = lazyre.New(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// NameProblem is why name cannot be a new profile's; "" when it can.
func NameProblem(f File, name string) string {
	switch {
	case name == Default:
		return fmt.Sprintf("'%s' is agents.yaml itself; define no profile by that name", Default)
	case !profileName.MatchString(name) || objectPrototype[name]:
		return fmt.Sprintf("'%s' is not a profile name: letters, digits, '.', '_', or '-', up to 64", name)
	case f.Profiles.Has(name):
		return fmt.Sprintf("profile '%s' already exists", name)
	}
	return ""
}

// profilesOf is root's `profiles` object, created when absent.
func profilesOf(root *js.Object) *js.Object {
	p, ok := js.Prop(root, "profiles").(*js.Object)
	if !ok {
		p = js.NewObject()
		root.Set("profiles", p)
	}
	return p
}

// New adds the profile name to root: a copy of from's overrides, or none
// when from is default.
func New(root *js.Object, name, from string) {
	var p *js.Object
	if src, ok := js.Prop(profilesOf(root), from).(*js.Object); ok && from != Default {
		// A deep copy: the new profile shares no object with its source.
		if v, err := js.ParseJSON(js.StringifyValue(src)); err == nil {
			p, _ = v.(*js.Object)
		}
	}
	if p == nil {
		p = js.NewObject()
	}
	profilesOf(root).Set(name, p)
}

// SetAgent sets one agent's model in one runtime under profile, or removes
// it (value ""), leaving no empty object behind.
func SetAgent(root *js.Object, profile, agent, runtime, value string) error {
	p, ok := js.Prop(profilesOf(root), profile).(*js.Object)
	if !ok {
		return fmt.Errorf("no profile '%s'", profile)
	}
	agents, ok := js.Prop(p, "agents").(*js.Object)
	if !ok {
		if value == "" {
			return nil
		}
		agents = js.NewObject()
		p.Set("agents", agents)
	}
	a, ok := js.Prop(agents, agent).(*js.Object)
	if !ok {
		if value == "" {
			return nil
		}
		a = js.NewObject()
		agents.Set(agent, a)
	}
	if value == "" {
		a.Delete(runtime)
	} else {
		a.Set(runtime, value)
	}
	if len(a.Keys()) == 0 {
		agents.Delete(agent)
	}
	if len(agents.Keys()) == 0 {
		p.Delete("agents")
	}
	return nil
}

// Remove deletes the profile name from root.
func Remove(root *js.Object, name string) {
	profilesOf(root).Delete(name)
}
