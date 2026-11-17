package cli

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
)

// connectorsSwitch is the ENABLE_CLAUDEAI_MCP_SERVERS entry connectorsEnv
// leaves in an empty environment, "" for none, under the settings text.
func connectorsSwitch(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.toml")
	must(t, os.WriteFile(path, []byte(text), 0o600))
	t.Setenv("JHON_SETTINGS", path)
	t.Setenv("ENABLE_CLAUDEAI_MCP_SERVERS", "")
	os.Unsetenv("ENABLE_CLAUDEAI_MCP_SERVERS")
	env := connectorsEnv(nil)
	i := slices.IndexFunc(env, func(kv string) bool { return len(kv) > 27 && kv[:27] == "ENABLE_CLAUDEAI_MCP_SERVERS" })
	if i < 0 {
		return ""
	}
	return env[i]
}

// T42a (L403), kept by T58b: the claude.ai connectors stay out of a Claude
// Code launch unless claude_code.connectors (m365's manifest setting) is on.
func TestConnectorsFollowTheirManifestSetting(t *testing.T) {
	if got := connectorsSwitch(t, "schema = 1\n"); got != "ENABLE_CLAUDEAI_MCP_SERVERS=false" {
		t.Errorf("default: %q", got)
	}
	if got := connectorsSwitch(t, "schema = 1\n\n[claude_code]\nconnectors = true\n"); got != "" {
		t.Errorf("connectors on: %q", got)
	}
}

// T58b O1 (L1167): a fixture set's extra connector integration, turned off
// at launch by its own manifest setting, keeps the connectors out through
// the same path; with every launch-off integration on they load.
func TestAnExtraLaunchOffIntegrationTurnsOffByItsManifest(t *testing.T) {
	extra := integrations.Manifest{Name: "connfixture", Kind: "mcp-connector", Order: 9, Setting: "integrations.connfixture",
		Default: true, WhenOff: "launch", Capabilities: []string{"connfixture"}, ClaudeServers: []string{"claude_ai_Fixture"}}
	t.Cleanup(integrations.Use(append(integrations.All(), extra)))
	if got := connectorsSwitch(t, "schema = 1\n\n[claude_code]\nconnectors = true\n\n[integrations]\nconnfixture = false\n"); got != "ENABLE_CLAUDEAI_MCP_SERVERS=false" {
		t.Errorf("connfixture off: %q", got)
	}
	if got := connectorsSwitch(t, "schema = 1\n\n[claude_code]\nconnectors = true\n\n[integrations]\nconnfixture = true\n"); got != "" {
		t.Errorf("every launch-off integration on: %q", got)
	}
}
