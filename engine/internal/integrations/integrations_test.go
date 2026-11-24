package integrations

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

const good = `schema: jhon.integration/v1
name: alpha
display: Alpha
kind: mcp-stdio
order: 1
setting: integrations.alpha
default: true
required: false
when_off: render
capabilities: [alpha]
claude-code:
  servers: [alpha]
pi:
  server: alpha
  command: alpha
  args: [mcp]
  tools:
    alpha: [mcp__alpha__look]
doctor:
  binary: alpha
  version: 1.0.0
  probe: --version
setup:
  summary: a thing
  install:
    npm: npm install -g alpha@{version}
    manual: "install alpha {version}: by hand"
`

func fsOf(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for k, v := range files {
		m[k] = &fstest.MapFile{Data: []byte(v)}
	}
	return m
}

func TestEmbeddedManifests(t *testing.T) {
	all := All()
	var names []string
	for _, m := range all {
		names = append(names, m.Name)
	}
	if want := []string{"engram", "codegraph", "context7", "m365", "studio"}; !slices.Equal(names, want) {
		t.Fatalf("names %v, want %v (by order)", names, want)
	}
	e, ok := Get("engram")
	if !ok || e.Display != "Engram" || !e.Required || e.Pi == nil || e.Pi.Command != "engram" ||
		!slices.Equal(e.Pi.Args, []string{"mcp", "--tools=agent"}) || e.Doctor.Version == "" ||
		!slices.Equal(e.ClaudePrefixes(), []string{"mcp__engram__", "mcp__plugin_engram_engram__"}) {
		t.Fatalf("engram: %+v", e)
	}
	if m, ok := ByCapability("ms365"); !ok || m.Name != "m365" || m.Pi != nil {
		t.Fatalf("ms365: %+v %v", m, ok)
	}
	c, _ := Get("context7")
	if c.Pi == nil || c.Pi.URL != "https://mcp.context7.com/mcp" || !slices.Equal(c.Pi.Tools["context7"], []string{"mcp__context7__resolve_library_id", "mcp__context7__query_docs"}) {
		t.Fatalf("context7: %+v", c.Pi)
	}
	if got := Required(); len(got) != 2 || got[0].Name != "engram" || got[1].Name != "codegraph" {
		t.Fatalf("required: %v", got)
	}
}

// odd/tasks/jhon-studio.md S3: jhon-studio is an opt-in cli integration, off
// by default, which the launch keeps out while it is off; it grants no
// capability (a cli mounts no tools) and setup offers only its manual install,
// which is its signed release, never a git clone (T242); its doctor.source
// names the repository jhon installs its releases from.
func TestStudioIsAnOptInCLI(t *testing.T) {
	s, ok := Get("studio")
	if !ok || s.Kind != "cli" || s.Display != "jhon-studio" || s.Setting != "integrations.studio" || s.Default || s.Required ||
		s.WhenOff != "launch" || len(s.Capabilities) != 0 || len(s.ClaudeServers) != 0 || s.Pi != nil {
		t.Fatalf("studio: %+v", s)
	}
	if s.Doctor.Binary != "jhon-studio" || s.Doctor.Probe != "--version" || s.Doctor.Version == "" {
		t.Errorf("studio's doctor: %+v", s.Doctor)
	}
	if s.Setup.Summary != "presentation videos for any project" || s.Setup.SummaryNarrow == "" || s.Setup.Detail == "" {
		t.Errorf("studio's setup text: %+v", s.Setup)
	}
	const manual = "jhon setup and jhon update install its newest signed release that supports this jhon; by hand, see https://github.com/jachavarro2004/jhon-studio#install"
	if vias := s.InstallVias(); !slices.Equal(vias, []string{"manual"}) || s.InstallCommand("manual", s.Doctor.Version) != manual {
		t.Errorf("studio's install: %v %q", vias, s.InstallCommand("manual", s.Doctor.Version))
	}
	if owner, repo, ok := s.ReleaseRepo(); !ok || owner != "jachavarro2004" || repo != "jhon-studio" {
		t.Errorf("studio's ReleaseRepo = %q, %q, %v", owner, repo, ok)
	}
	// No other manifest is released this way: Engram's source is a module
	// path, CodeGraph's an npm package, and neither is a cli.
	for _, m := range All() {
		if _, _, ok := m.ReleaseRepo(); ok && m.Name != "studio" {
			t.Errorf("%s is released from GitHub as jhon reads it", m.Name)
		}
	}
}

// L11 (the verifier's probe): every MCP kind still needs a capability; only
// a cli may grant none.
func TestEveryMCPKindStillNeedsCapabilities(t *testing.T) {
	for _, kind := range []string{"mcp-stdio", "mcp-http", "mcp-connector"} {
		text := strings.NewReplacer("kind: mcp-stdio", "kind: "+kind, "capabilities: [alpha]\n", "", "  command: alpha\n", "  command: alpha\n  url: https://alpha.example.invalid/mcp\n").Replace(good)
		_, probs := Load(fsOf(map[string]string{"alpha/manifest.yaml": text}))
		var got []string
		for _, p := range probs {
			got = append(got, p.String())
		}
		if !slices.Contains(got, "integrations/alpha/manifest.yaml: capabilities: missing") {
			t.Errorf("%s with no capabilities: %q", kind, got)
		}
	}
}

// A cli integration may grant no capability; an MCP one still must.
func TestOnlyACLIMayGrantNoCapability(t *testing.T) {
	cli := strings.NewReplacer("kind: mcp-stdio", "kind: cli", "capabilities: [alpha]\n", "").Replace(good)
	cli = cli[:strings.Index(cli, "claude-code:")] + cli[strings.Index(cli, "doctor:"):]
	if _, probs := Load(fsOf(map[string]string{"alpha/manifest.yaml": cli})); len(probs) != 0 {
		t.Errorf("a cli with no capabilities: %v", probs)
	}
	_, probs := Load(fsOf(map[string]string{"alpha/manifest.yaml": strings.Replace(good, "capabilities: [alpha]\n", "", 1)}))
	if len(probs) == 0 || probs[0].String() != "integrations/alpha/manifest.yaml: capabilities: missing" {
		t.Errorf("an mcp-stdio with no capabilities: %v", probs)
	}
}

func TestLoadParsesEveryField(t *testing.T) {
	ms, probs := Load(fsOf(map[string]string{"alpha/manifest.yaml": good}))
	if len(probs) != 0 {
		t.Fatal(probs)
	}
	m := ms[0]
	if m.File != "integrations/alpha/manifest.yaml" || m.Order != 1 || m.WhenOff != "render" || m.Default != true ||
		m.Doctor.Probe != "--version" || m.Setup.Summary != "a thing" {
		t.Fatalf("%+v", m)
	}
	if got := m.InstallCommand("manual", "2.0"); got != "install alpha 2.0: by hand" {
		t.Fatalf("manual: %q", got)
	}
	if vias := m.InstallVias(); !slices.Equal(vias, []string{"npm", "manual"}) {
		t.Fatalf("vias: %v", vias)
	}
}

// TestLoadRefusesMalformed: a missing field, an unknown schema, a duplicate
// name, and the rest of what v1 refuses, each naming the file and the field.
func TestLoadRefusesMalformed(t *testing.T) {
	other := strings.NewReplacer("name: alpha", "name: beta", "order: 1", "order: 2",
		"integrations.alpha", "integrations.beta", "capabilities: [alpha]", "capabilities: [beta]",
		"servers: [alpha]", "servers: [beta]", "server: alpha", "server: beta", "    alpha: [mcp__alpha__look]", "    beta: [mcp__beta__look]").Replace(good)
	for _, c := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"a missing field", map[string]string{"alpha/manifest.yaml": strings.Replace(good, "display: Alpha\n", "", 1)},
			"integrations/alpha/manifest.yaml: display: missing"},
		{"an unknown schema", map[string]string{"alpha/manifest.yaml": strings.Replace(good, "jhon.integration/v1", "jhon.integration/v9", 1)},
			"integrations/alpha/manifest.yaml: schema: jhon.integration/v9 is another major version; this binary reads jhon.integration/v1 (integrations/README.md)"},
		{"a schema not of the form", map[string]string{"alpha/manifest.yaml": strings.Replace(good, "jhon.integration/v1", "jhon.integration/1.1", 1)},
			"integrations/alpha/manifest.yaml: schema: jhon.integration/1.1 is not jhon.integration/v<N>; this binary reads jhon.integration/v1"},
		{"a missing schema", map[string]string{"alpha/manifest.yaml": strings.Replace(good, "schema: jhon.integration/v1\n", "", 1)},
			"integrations/alpha/manifest.yaml: schema: missing"},
		{"a duplicate name", map[string]string{"alpha/manifest.yaml": good, "beta/manifest.yaml": strings.Replace(other, "name: beta", "name: alpha", 1)},
			"integrations/beta/manifest.yaml: name: alpha is also the name of integrations/alpha/manifest.yaml"},
		{"a name that is not its folder", map[string]string{"gamma/manifest.yaml": good},
			"integrations/gamma/manifest.yaml: name: alpha is not its folder's name, gamma"},
		{"an unknown field", map[string]string{"alpha/manifest.yaml": good + "colour: red\n"},
			"integrations/alpha/manifest.yaml: colour: not a field of jhon.integration/v1"},
		{"an unknown nested field", map[string]string{"alpha/manifest.yaml": strings.Replace(good, "  probe: --version\n", "  probe: --version\n  timeout: 5\n", 1)},
			"integrations/alpha/manifest.yaml: doctor.timeout: not a field of jhon.integration/v1"},
		{"an unknown kind", map[string]string{"alpha/manifest.yaml": strings.Replace(good, "kind: mcp-stdio", "kind: grpc", 1)},
			"integrations/alpha/manifest.yaml: kind: grpc is not one of mcp-stdio, mcp-http, mcp-connector, cli"},
		{"a non-boolean", map[string]string{"alpha/manifest.yaml": strings.Replace(good, "default: true", "default: yes", 1)},
			"integrations/alpha/manifest.yaml: default: yes is not true or false"},
		{"a required integration that can be turned off", map[string]string{"alpha/manifest.yaml": strings.Replace(good, "required: false", "required: true", 1)},
			"integrations/alpha/manifest.yaml: when_off: a required integration's is refused, not render"},
		{"a doctor binary without a version", map[string]string{"alpha/manifest.yaml": strings.Replace(good, "  version: 1.0.0\n", "", 1)},
			"integrations/alpha/manifest.yaml: doctor.version: missing"},
		{"a duplicate order", map[string]string{"alpha/manifest.yaml": good, "beta/manifest.yaml": strings.Replace(other, "order: 2", "order: 1", 1)},
			"integrations/beta/manifest.yaml: order: 1 is also the order of integrations/alpha/manifest.yaml"},
		{"a capability two grant", map[string]string{"alpha/manifest.yaml": good, "beta/manifest.yaml": strings.Replace(other, "capabilities: [beta]", "capabilities: [alpha]", 1)},
			"integrations/beta/manifest.yaml: capabilities: alpha is also granted by integrations/alpha/manifest.yaml"},
		{"an unknown install method", map[string]string{"alpha/manifest.yaml": strings.Replace(good, "    npm:", "    pip:", 1)},
			"integrations/alpha/manifest.yaml: setup.install.pip: not one of go, npm, brew, manual"},
		{"a pi server with neither command nor url", map[string]string{"alpha/manifest.yaml": strings.Replace(good, "  command: alpha\n", "", 1)},
			"integrations/alpha/manifest.yaml: pi.command: missing (an mcp-stdio server needs a command)"},
		{"a line the reader cannot place", map[string]string{"alpha/manifest.yaml": good + "  stray\n"},
			"integrations/alpha/manifest.yaml: line 28: cannot be read as key: value"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, probs := Load(fsOf(c.files))
			var got []string
			for _, p := range probs {
				got = append(got, p.String())
			}
			if !slices.Contains(got, c.want) {
				t.Fatalf("want %q among\n%s", c.want, strings.Join(got, "\n"))
			}
		})
	}
}

func TestLoadRefusesNoManifests(t *testing.T) {
	if _, probs := Load(fstest.MapFS{}); len(probs) != 1 || probs[0].String() != "integrations: no integrations/<name>/manifest.yaml" {
		t.Fatalf("%v", probs)
	}
}

// T58b O1 (L1167): an optional integration is one its setting can turn
// off, whatever its when_off; OffAt splits them by the stage that acts, and
// RemoteProbed are those doctor asks over the network. A test's set (Use)
// is read by every lookup until restored.
func TestOptionalByTheirManifests(t *testing.T) {
	names := func(ms []Manifest) []string {
		var out []string
		for _, m := range ms {
			out = append(out, m.Name)
		}
		return out
	}
	for _, c := range []struct {
		what string
		got  []Manifest
		want []string
	}{
		{"Optional", Optional(), []string{"context7", "m365", "studio"}},
		{"OffAt(render)", OffAt("render"), []string{"context7"}},
		{"OffAt(launch)", OffAt("launch"), []string{"m365", "studio"}},
		{"RemoteProbed", RemoteProbed(), []string{"context7"}},
	} {
		if !slices.Equal(names(c.got), c.want) {
			t.Errorf("%s = %v, want %v", c.what, names(c.got), c.want)
		}
	}
	extra := Manifest{Name: "zeta", Order: 9, Setting: "integrations.zeta", WhenOff: "render", Capabilities: []string{"zeta"}}
	restore := Use(append(All(), extra))
	if got := names(OffAt("render")); !slices.Equal(got, []string{"context7", "zeta"}) {
		t.Errorf("with a fixture set, OffAt(render) = %v", got)
	}
	if m, ok := ByCapability("zeta"); !ok || m.Name != "zeta" {
		t.Errorf("with a fixture set, ByCapability(zeta) = %+v %v", m, ok)
	}
	restore()
	if _, ok := Get("zeta"); ok {
		t.Error("restore left the fixture set")
	}
}
