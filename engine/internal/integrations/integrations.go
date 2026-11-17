// Package integrations is the one reader of the integration manifests,
// integrations/<name>/manifest.yaml, schema jhon.integration/v1 (T58a, L460,
// L1150): what names each integration Jhon works with (Engram, CodeGraph,
// Context7, the Microsoft 365 connector) — its MCP servers per runtime, the
// capabilities of prose/agents.yaml it grants, the setting that turns it
// off, its doctor checks and its setup text. The engine reads these values
// from here and names no integration itself (stray_test.go keeps it so).
//
// The manifests are compiled in (package integrations at the repository
// root embeds them), so every command reads the same ones wherever it runs;
// `jhon validate` reads the tree's own with Load and Assumed (the compiled-in
// ones, Embedded, in a tree without the folder) and refuses a malformed one,
// or a set the engine cannot use, naming the file and the field.
//
// The format is a small YAML subset, read by this package alone: `key:
// value`, a flow list `[a, b]`, a nested block indented two spaces, `#`
// comments, and a value in double quotes when it holds `: ` or `#`.
package integrations

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	embedded "github.com/jachavarro2004/jhon-agent/integrations"
)

// Schema is the one schema version this reader knows. A manifest names its
// major version only, jhon.integration/v<N>; one of another major is
// refused, naming the file, the field and this version (the policy:
// integrations/README.md).
const Schema = "jhon.integration/v1"

// schemaForm is a schema of any major version.
var schemaForm = regexp.MustCompile(`^jhon\.integration/v[1-9][0-9]*$`)

// Kinds are how an integration is reached: an MCP server over stdio or
// http, a connector the runtime's account mounts, or a CLI.
var Kinds = []string{"mcp-stdio", "mcp-http", "mcp-connector", "cli"}

// WhenOff is what its setting set to false does: refused (the setting
// cannot be false: required for now), render (the render drops its
// capabilities, servers and the body lines naming it) or launch (the
// runtime's launch keeps it out; the render is unchanged).
var WhenOff = []string{"refused", "render", "launch"}

// InstallVias are the ways setup can install a binary, in the order a
// manifest may list them; manual is advice, never run.
var InstallVias = []string{"go", "npm", "brew", "manual"}

// Manifest is one integrations/<name>/manifest.yaml.
type Manifest struct {
	// File is integrations/<name>/manifest.yaml.
	File    string
	Schema  string
	Name    string
	Display string
	Kind    string
	// Order places it among the others: settings, setup rows, servers.
	Order int
	// Setting is the settings key that turns it off.
	Setting  string
	Default  bool
	Required bool
	WhenOff  string
	// Capabilities are the agents.yaml capabilities whose tools are its own.
	Capabilities []string
	// ClaudeServers are its MCP server ids in Claude Code: a tool is
	// mcp__<server>__<tool>, or the server name alone for every tool of it.
	ClaudeServers []string
	// Pi is its server in pi's mcp.json; nil when pi mounts none.
	Pi     *PiServer
	Doctor Doctor
	Setup  Setup
}

// PiServer is an mcp.json server.
type PiServer struct {
	Server  string
	Command string
	Args    []string
	URL     string
	// Tools are a capability's tools in pi, where pi's names differ from
	// Claude Code's; a capability absent here takes Claude Code's, renamed
	// to the pi server's prefix.
	Tools map[string][]string
}

// Doctor is what `jhon doctor` checks: Binary at Version (`<binary>
// <probe>` prints it), or URL when Probe is remote.
type Doctor struct{ Binary, Version, Source, Probe, URL string }

// Setup is the setup UI's text: the row's description (wide and narrow),
// the line under it, the note when a locked row is toggled, and the
// install commands by way, {version} standing for Doctor.Version.
type Setup struct {
	Summary, SummaryNarrow, Detail, LockedNote string
	Install                                    []Install
}

// Install is one way to install the binary.
type Install struct{ Via, Command string }

// Problem is one refusal: the file, the field (dotted for a nested one; ""
// when it names none) and why.
type Problem struct{ File, Field, Msg string }

func (p Problem) String() string {
	if p.Field == "" {
		return p.File + ": " + p.Msg
	}
	return p.File + ": " + p.Field + ": " + p.Msg
}

// ClaudePrefixes are the prefixes its tools carry in Claude Code, one per server.
func (m Manifest) ClaudePrefixes() []string {
	var out []string
	for _, s := range m.ClaudeServers {
		out = append(out, "mcp__"+s+"__")
	}
	return out
}

// ClaudeTool is whether t is a tool of its Claude Code servers.
func (m Manifest) ClaudeTool(t string) bool {
	for _, s := range m.ClaudeServers {
		if t == "mcp__"+s || strings.HasPrefix(t, "mcp__"+s+"__") {
			return true
		}
	}
	return false
}

// PiPrefix is the prefix its tools carry in pi; "" when pi mounts none.
func (m Manifest) PiPrefix() string {
	if m.Pi == nil {
		return ""
	}
	return "mcp__" + m.Pi.Server + "__"
}

// InstallVias are the ways its manifest lists, in order.
func (m Manifest) InstallVias() []string {
	var out []string
	for _, i := range m.Setup.Install {
		out = append(out, i.Via)
	}
	return out
}

// InstallCommand is the command (or, for manual, the advice) of a way, at
// version; "" when the manifest lists no such way.
func (m Manifest) InstallCommand(via, version string) string {
	for _, i := range m.Setup.Install {
		if i.Via == via {
			return strings.ReplaceAll(i.Command, "{version}", version)
		}
	}
	return ""
}

// The integrations the engine still reads by name (L1159, F4): setup's
// locked rows and the pins it installs (Core, in row order). Assumed refuses
// a set that breaks this, so a lookup by one of them never finds a shape it
// cannot use. No optional integration is read by name (T58b, L1167): its
// manifest's setting, when_off and doctor block decide its on and off.
const (
	Engram    = "engram"
	CodeGraph = "codegraph"
)

// Core are setup's locked rows and the pins it installs, in row order:
// required, and kept so by Assumed (owner, L1166).
var Core = []string{Engram, CodeGraph}

// embeddedSet is the compiled-in manifests and their problems, read once.
var embeddedSet = sync.OnceValues(func() ([]Manifest, []Problem) {
	ms, probs := Load(embedded.FS)
	return ms, append(probs, Assumed(ms)...)
})

// used is a set a test made the one every lookup reads (Use); nil reads
// the compiled-in one.
var used atomic.Pointer[[]Manifest]

// All are the compiled-in manifests, by Order (a malformed one as far as it
// reads). They never stop the binary: a lookup that finds nothing gives a
// zero Manifest, `jhon validate` reports their problems (Embedded), and
// integrations_test.go keeps a malformed set from shipping.
func All() []Manifest {
	if ms := used.Load(); ms != nil {
		return *ms
	}
	ms, _ := embeddedSet()
	return ms
}

// Use makes ms, by Order, the set All and every lookup read until restore
// is called: a test turns a fixture set's integrations on and off through
// the same path as the compiled-in ones (T58b, O1). Never called outside a
// test.
func Use(ms []Manifest) (restore func()) {
	ms = slices.Clone(ms)
	sort.SliceStable(ms, func(i, j int) bool { return ms[i].Order < ms[j].Order })
	prev := used.Swap(&ms)
	return func() { used.Store(prev) }
}

// Embedded are the compiled-in manifests and every problem of them: of
// form (Load) and of shape (Assumed).
func Embedded() ([]Manifest, []Problem) {
	ms, probs := embeddedSet()
	return ms, slices.Clone(probs)
}

// Assumed are the problems of a set that breaks what the engine still
// assumes of it, each naming the file and the field: every Core integration
// is there, required, with a pinned binary, and no other is required (setup
// shows a locked row for each Core one and no other). An optional
// integration may be absent: nothing reads one by name.
func Assumed(ms []Manifest) []Problem {
	var probs []Problem
	file := func(name string) string { return "integrations/" + name + "/manifest.yaml" }
	find := func(name string) (Manifest, bool) {
		i := slices.IndexFunc(ms, func(m Manifest) bool { return m.Name == name })
		if i < 0 {
			return Manifest{}, false
		}
		return ms[i], true
	}
	for _, name := range Core {
		m, ok := find(name)
		switch {
		case !ok:
			probs = append(probs, Problem{file(name), "name", "no manifest is named " + name + "; setup's locked rows and pins read it by name"})
			continue
		case !m.Required:
			probs = append(probs, Problem{m.File, "required", "false, but setup shows " + name + " as a locked row and pins its binary"})
		}
		if m.Doctor.Version == "" {
			probs = append(probs, Problem{m.File, "doctor.version", "missing; setup installs " + name + "'s binary at it"})
		}
	}
	for _, m := range ms {
		if m.Required && !slices.Contains(Core, m.Name) {
			probs = append(probs, Problem{m.File, "required", "setup shows a locked row only for " + strings.Join(Core, " and ")})
		}
	}
	return probs
}

// Get is the integration called name.
func Get(name string) (Manifest, bool) {
	i := slices.IndexFunc(All(), func(m Manifest) bool { return m.Name == name })
	if i < 0 {
		return Manifest{}, false
	}
	return All()[i], true
}

// ByCapability is the integration granting capability c.
func ByCapability(c string) (Manifest, bool) {
	i := slices.IndexFunc(All(), func(m Manifest) bool { return slices.Contains(m.Capabilities, c) })
	if i < 0 {
		return Manifest{}, false
	}
	return All()[i], true
}

// Required are the integrations that cannot be turned off, by Order.
func Required() []Manifest {
	return slices.DeleteFunc(slices.Clone(All()), func(m Manifest) bool { return !m.Required })
}

// Optional are the integrations their setting can turn off, by Order:
// every one not required, whatever its when_off.
func Optional() []Manifest {
	return slices.DeleteFunc(slices.Clone(All()), func(m Manifest) bool { return m.Required })
}

// OffAt are the optional integrations whose setting, false, acts at stage
// (a when_off: render or launch), by Order.
func OffAt(stage string) []Manifest {
	return slices.DeleteFunc(Optional(), func(m Manifest) bool { return m.WhenOff != stage })
}

// RemoteProbed are the optional integrations doctor asks over the network
// (doctor.probe: remote, at doctor.url) only when told to, by Order.
func RemoteProbed() []Manifest {
	return slices.DeleteFunc(Optional(), func(m Manifest) bool { return m.Doctor.Probe != "remote" || m.Doctor.URL == "" })
}

// Load reads every <name>/manifest.yaml of fsys (the integrations folder)
// and validates each and the set: the manifests by Order (a malformed one
// as far as it reads), and every problem.
func Load(fsys fs.FS) ([]Manifest, []Problem) {
	paths, err := fs.Glob(fsys, "*/manifest.yaml")
	if err != nil || len(paths) == 0 {
		return nil, []Problem{{"integrations", "", "no integrations/<name>/manifest.yaml"}}
	}
	sort.Strings(paths)
	var out []Manifest
	var probs []Problem
	for _, p := range paths {
		b, err := fs.ReadFile(fsys, p)
		file := "integrations/" + p
		if err != nil {
			probs = append(probs, Problem{file, "", err.Error()})
			continue
		}
		m, ps := parse(file, path.Dir(p), string(b))
		probs = append(probs, ps...)
		out = append(out, m)
	}
	probs = append(probs, across(out)...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out, probs
}

// across are the problems between manifests: a name, order, setting,
// capability or server two claim.
func across(ms []Manifest) []Problem {
	var probs []Problem
	seen := map[string]string{}
	claim := func(m Manifest, field, kind, v, what string) {
		if v == "" || v == "0" {
			return
		}
		k := kind + "\x00" + v
		if first, ok := seen[k]; ok {
			probs = append(probs, Problem{m.File, field, v + " is also " + what + " " + first})
			return
		}
		seen[k] = m.File
	}
	for _, m := range ms {
		claim(m, "name", "name", m.Name, "the name of")
		claim(m, "order", "order", strconv.Itoa(m.Order), "the order of")
		claim(m, "setting", "setting", m.Setting, "the setting of")
		for _, c := range m.Capabilities {
			claim(m, "capabilities", "cap", c, "granted by")
		}
		for _, s := range m.ClaudeServers {
			claim(m, "claude-code.servers", "cc", s, "a server of")
		}
		if m.Pi != nil {
			claim(m, "pi.server", "pi", m.Pi.Server, "a server of")
		}
	}
	return probs
}

// fields are v1's, dotted for a nested one; a block's own key ends in ".".
var fields = []string{
	"schema", "name", "display", "kind", "order", "setting", "default", "required", "when_off", "capabilities",
	"claude-code.", "claude-code.servers",
	"pi.", "pi.server", "pi.command", "pi.args", "pi.url", "pi.tools.",
	"doctor.", "doctor.binary", "doctor.version", "doctor.source", "doctor.probe", "doctor.url",
	"setup.", "setup.summary", "setup.summary_narrow", "setup.detail", "setup.locked_note", "setup.install.",
}

// node is a read value: a scalar, a flow list, or a block.
type node struct {
	scalar string
	list   []string
	isList bool
	block  *block
}

type block struct {
	keys []string
	vals map[string]node
}

func (b *block) set(k string, n node) bool {
	if _, dup := b.vals[k]; dup {
		return false
	}
	b.keys = append(b.keys, k)
	b.vals[k] = n
	return true
}

// read is the YAML subset: the root block and the problems placing a line.
func read(file, text string) (*block, []Problem) {
	var probs []Problem
	root := &block{vals: map[string]node{}}
	type frame struct {
		indent int
		b      *block
	}
	stack := []frame{{0, root}}
	var pending *block // a key with no value opens a block at the next deeper indent
	pendingIndent := -1
	for i, raw := range strings.Split(text, "\n") {
		line := stripComment(raw)
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		bad := func(msg string) { probs = append(probs, Problem{file, "", fmt.Sprintf("line %d: %s", i+1, msg)}) }
		if pending != nil && indent > pendingIndent {
			stack = append(stack, frame{indent, pending})
			pending = nil
		} else if pending != nil {
			pending = nil
		}
		for len(stack) > 1 && indent < stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}
		top := stack[len(stack)-1]
		if indent != top.indent {
			bad("cannot be read as key: value")
			continue
		}
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok || k == "" || strings.ContainsAny(k, " \"[") {
			bad("cannot be read as key: value")
			continue
		}
		v = strings.TrimSpace(v)
		var n node
		switch {
		case v == "":
			n.block = &block{vals: map[string]node{}}
			pending, pendingIndent = n.block, indent
		case strings.HasPrefix(v, "["):
			if !strings.HasSuffix(v, "]") {
				bad("a list ends with ]")
				continue
			}
			n.isList = true
			for _, x := range strings.Split(v[1:len(v)-1], ",") {
				if x = unquote(strings.TrimSpace(x)); x != "" {
					n.list = append(n.list, x)
				}
			}
		default:
			n.scalar = unquote(v)
		}
		if !top.b.set(k, n) {
			bad(k + " is given twice")
		}
	}
	return root, probs
}

// stripComment is line without a `#` comment outside double quotes.
func stripComment(line string) string {
	quoted := false
	for i, r := range line {
		switch {
		case r == '"':
			quoted = !quoted
		case r == '#' && !quoted && (i == 0 || line[i-1] == ' '):
			return strings.TrimRight(line[:i], " ")
		}
	}
	return strings.TrimRight(line, " \r")
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// parse reads and validates one manifest; dir is its folder's name.
func parse(file, dir, text string) (Manifest, []Problem) {
	root, probs := read(file, text)
	m := Manifest{File: file}
	bad := func(field, msg string) { probs = append(probs, Problem{file, field, msg}) }
	// Unknown fields, at every depth but the free-keyed blocks (pi.tools, setup.install).
	var walk func(prefix string, b *block)
	walk = func(prefix string, b *block) {
		for _, k := range b.keys {
			f, n := prefix+k, b.vals[k]
			switch {
			case n.block != nil && slices.Contains(fields, f+"."):
				if f != "pi.tools" && f != "setup.install" {
					walk(f+".", n.block)
				}
			case n.block == nil && slices.Contains(fields, f):
			default:
				bad(f, "not a field of "+Schema)
			}
		}
	}
	walk("", root)
	get := func(f string) (node, bool) {
		b := root
		parts := strings.Split(f, ".")
		for _, p := range parts[:len(parts)-1] {
			n, ok := b.vals[p]
			if !ok || n.block == nil {
				return node{}, false
			}
			b = n.block
		}
		n, ok := b.vals[parts[len(parts)-1]]
		return n, ok
	}
	has := func(f string) bool { _, ok := get(f); return ok }
	str := func(f string, required bool) string {
		n, ok := get(f)
		switch {
		case !ok && required:
			bad(f, "missing")
		case ok && (n.isList || n.block != nil):
			bad(f, "is not a single value")
		case ok && n.scalar == "" && required:
			bad(f, "missing")
		}
		return n.scalar
	}
	list := func(f string, required bool) []string {
		n, ok := get(f)
		switch {
		case !ok && required:
			bad(f, "missing")
		case ok && !n.isList:
			bad(f, "is not a list [a, b]")
		case ok && len(n.list) == 0 && required:
			bad(f, "missing")
		}
		return n.list
	}
	boolean := func(f string) bool {
		v := str(f, true)
		if v != "" && v != "true" && v != "false" {
			bad(f, v+" is not true or false")
		}
		return v == "true"
	}
	oneOf := func(f, v string, set []string) {
		if v != "" && !slices.Contains(set, v) {
			bad(f, v+" is not one of "+strings.Join(set, ", "))
		}
	}

	m.Schema = str("schema", true)
	if m.Schema != "" && m.Schema != Schema {
		if schemaForm.MatchString(m.Schema) {
			bad("schema", m.Schema+" is another major version; this binary reads "+Schema+" (integrations/README.md)")
		} else {
			bad("schema", m.Schema+" is not jhon.integration/v<N>; this binary reads "+Schema)
		}
		return m, probs // another version's fields are not v1's to judge
	}
	m.Name = str("name", true)
	if m.Name != "" && m.Name != dir {
		bad("name", m.Name+" is not its folder's name, "+dir)
	}
	m.Display = str("display", true)
	m.Kind = str("kind", true)
	oneOf("kind", m.Kind, Kinds)
	if o := str("order", true); o != "" {
		n, err := strconv.Atoi(o)
		if err != nil || n < 1 {
			bad("order", o+" is not a whole number from 1")
		}
		m.Order = n
	}
	m.Setting = str("setting", true)
	m.Default = boolean("default")
	m.Required = boolean("required")
	m.WhenOff = str("when_off", true)
	oneOf("when_off", m.WhenOff, WhenOff)
	if m.Required && m.WhenOff != "" && m.WhenOff != "refused" {
		bad("when_off", "a required integration's is refused, not "+m.WhenOff)
	}
	if !m.Required && m.WhenOff == "refused" {
		bad("when_off", "refused is a required integration's")
	}
	m.Capabilities = list("capabilities", true)
	mcp := strings.HasPrefix(m.Kind, "mcp-")
	m.ClaudeServers = list("claude-code.servers", mcp)

	if has("pi") {
		p := &PiServer{Server: str("pi.server", true), Command: str("pi.command", false), Args: list("pi.args", false), URL: str("pi.url", false)}
		switch {
		case m.Kind == "mcp-stdio" && p.Command == "":
			bad("pi.command", "missing (an mcp-stdio server needs a command)")
		case m.Kind == "mcp-http" && p.URL == "":
			bad("pi.url", "missing (an mcp-http server needs a url)")
		case !mcp:
			bad("pi", "a "+m.Kind+" mounts no MCP server")
		}
		if n, ok := get("pi.tools"); ok && n.block != nil {
			p.Tools = map[string][]string{}
			for _, c := range n.block.keys {
				t := n.block.vals[c]
				if !t.isList || len(t.list) == 0 {
					bad("pi.tools."+c, "is not a list [a, b]")
				} else if !slices.Contains(m.Capabilities, c) {
					bad("pi.tools."+c, c+" is not one of its capabilities")
				}
				p.Tools[c] = t.list
			}
		}
		m.Pi = p
	}
	m.Doctor = Doctor{Binary: str("doctor.binary", false), Version: str("doctor.version", false), Source: str("doctor.source", false),
		Probe: str("doctor.probe", has("doctor")), URL: str("doctor.url", false)}
	if m.Doctor.Binary != "" && m.Doctor.Version == "" {
		bad("doctor.version", "missing")
	}
	if m.Doctor.Probe == "remote" && m.Doctor.URL == "" {
		bad("doctor.url", "missing (a remote probe needs a url)")
	}
	m.Setup = Setup{Summary: str("setup.summary", has("setup")), SummaryNarrow: str("setup.summary_narrow", false),
		Detail: str("setup.detail", false), LockedNote: str("setup.locked_note", false)}
	if m.Required && has("setup") && m.Setup.LockedNote == "" {
		bad("setup.locked_note", "missing (a required integration's row is locked on)")
	}
	if n, ok := get("setup.install"); ok && n.block != nil {
		for _, via := range n.block.keys {
			v := n.block.vals[via]
			switch {
			case !slices.Contains(InstallVias, via):
				bad("setup.install."+via, "not one of "+strings.Join(InstallVias, ", "))
			case v.isList || v.block != nil || v.scalar == "":
				bad("setup.install."+via, "is not a single value")
			}
			m.Setup.Install = append(m.Setup.Install, Install{via, v.scalar})
		}
		if m.Doctor.Binary == "" {
			bad("setup.install", "installs no doctor.binary")
		}
	}
	return m, probs
}

// ProbeRemote asks endpoint, an integration's doctor.url, one MCP
// initialize, with no credential, bounded to 5 seconds: nil when it answers
// 200. Only doctor --remote and the dashboard's ?probe=1 call it.
func ProbeRemote(endpoint string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const initialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"jhon-doctor","version":"0"}}}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(initialize))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// The caller names the endpoint itself: only why it did not answer.
		var sent *url.Error
		if errors.As(err, &sent) {
			return sent.Err
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
