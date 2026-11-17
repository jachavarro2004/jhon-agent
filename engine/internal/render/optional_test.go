package render

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// fixtureOptional is an optional integration no shipped manifest names:
// documentation over http that grants the `web` capability, turned off at
// render by its own setting.
var fixtureOptional = integrations.Manifest{
	File: "integrations/docsfixture/manifest.yaml", Schema: integrations.Schema, Name: "docsfixture", Display: "Docs Fixture",
	Kind: "mcp-http", Order: 9, Setting: "integrations.docsfixture", Default: true, WhenOff: "render",
	Capabilities: []string{"web"}, ClaudeServers: []string{"docsfixture"},
	Pi: &integrations.PiServer{Server: "docsfixture", URL: "https://docs.example.invalid/mcp"},
}

// T58b O1 (L1167): an extra optional integration in a fixture set turns off
// through the same path as context7: its setting, read by settings from its
// manifest, takes its capability from every agent and its server out of
// mcp.json; on, both stay.
func TestAnExtraOptionalIntegrationTurnsOffByItsManifest(t *testing.T) {
	restore := integrations.Use(append(integrations.All(), fixtureOptional))
	t.Cleanup(restore)
	holders := func(e *engine) []string {
		var out []string
		for _, a := range e.m.Agents {
			if slices.Contains(a.Caps, "web") {
				out = append(out, a.Name)
			}
		}
		return out
	}
	for _, c := range []struct {
		name, settings string
		on             bool
	}{
		{"default", "", true},
		{"on", "schema = 1\n\n[integrations]\ndocsfixture = true\n", true},
		{"off", "schema = 1\n\n[integrations]\ndocsfixture = false\n", false},
	} {
		if c.settings == "" {
			t.Setenv("JHON_SETTINGS", filepath.Join(t.TempDir(), "none.toml"))
		} else {
			t.Setenv("JHON_SETTINGS", writeFile(t, c.settings))
		}
		e := engineAt(t, repo, "--validate")
		mounted := js.Prop(pi{e}.piMcp(), "mcpServers").(*js.Object).Has("docsfixture")
		if mounted != c.on {
			t.Errorf("%s: mcp.json mounts docsfixture = %v, want %v", c.name, mounted, c.on)
		}
		if got := holders(e); (len(got) > 0) != c.on {
			t.Errorf("%s: agents holding its capability = %v, want any = %v", c.name, got, c.on)
		}
	}
}
