package settings

import (
	"slices"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
)

// T58b O1 (L1167): the integrations' keys are their manifests' settings,
// read when a value is: a fixture set's extra optional integration has its
// key, default and on/off like the shipped ones, and a manifest setting in
// another namespace (m365's claude_code.connectors) keeps its place.
func TestEveryManifestSettingIsAKey(t *testing.T) {
	keys := Keys()
	at := func(k string) int { return slices.Index(keys, k) }
	if i, j := at("claude_code.connectors"), at("integrations.theme.palette"); i != j+1 {
		t.Errorf("claude_code.connectors at %d, want right after integrations.theme.palette (%d)", i, j)
	}
	extra := integrations.Manifest{Name: "docsfixture", Order: 9, Setting: "integrations.docsfixture", Default: true, WhenOff: "render"}
	conn := integrations.Manifest{Name: "connfixture", Order: 10, Setting: "claude_code.connfixture", Default: false, WhenOff: "launch"}
	restore := integrations.Use(append(integrations.All(), extra, conn))
	defer restore()
	keys = Keys()
	if i, j := at("integrations.docsfixture"), at("integrations.context7"); i != j+1 {
		t.Errorf("integrations.docsfixture at %d, want right after integrations.context7 (%d)", i, j)
	}
	if i, j := at("claude_code.connfixture"), at("claude_code.connectors"); i != j+1 {
		t.Errorf("claude_code.connfixture at %d, want right after claude_code.connectors (%d)", i, j)
	}
	vals, err := Load(write(t, "schema = 1\n\n[integrations]\ndocsfixture = false\n\n[claude_code]\nconnfixture = true\n"), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if v := valueOf(t, vals, "integrations.docsfixture"); v.Value != false || v.Default != true || v.Source != SourceFile {
		t.Errorf("integrations.docsfixture = %+v", v)
	}
	if v := valueOf(t, vals, "claude_code.connfixture"); v.Value != true || v.Default != false {
		t.Errorf("claude_code.connfixture = %+v", v)
	}
}
