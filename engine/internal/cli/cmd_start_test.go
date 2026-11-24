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

// T58c J1 (L1193): connectorsEnv fails closed. A set with no when_off:
// launch manifest still keeps the claude.ai connectors out unless
// claude_code.connectors is on, and claude_code.connectors stays a key.
func TestConnectorsStayOutWithNoLaunchOffManifest(t *testing.T) {
	t.Cleanup(integrations.Use(slices.DeleteFunc(slices.Clone(integrations.All()), func(m integrations.Manifest) bool { return m.WhenOff == "launch" })))
	if n := len(integrations.OffAt("launch")); n != 0 {
		t.Fatalf("the set still has %d launch-off manifest(s)", n)
	}
	if got := connectorsSwitch(t, "schema = 1\n"); got != "ENABLE_CLAUDEAI_MCP_SERVERS=false" {
		t.Errorf("default: %q", got)
	}
	if got := connectorsSwitch(t, "schema = 1\n\n[claude_code]\nconnectors = false\n"); got != "ENABLE_CLAUDEAI_MCP_SERVERS=false" {
		t.Errorf("connectors off: %q", got)
	}
	if got := connectorsSwitch(t, "schema = 1\n\n[claude_code]\nconnectors = true\n"); got != "" {
		t.Errorf("connectors on: %q", got)
	}
}

// studioAt sets the settings to text and puts a fake jhon-studio on PATH
// (with /usr/bin and /bin) whose `integration --json` prints answer, or
// none at all when answer is ""; it logs each call to the returned file.
func studioAt(t *testing.T, text, answer string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	must(t, os.WriteFile(path, []byte(text), 0o600))
	t.Setenv("JHON_SETTINGS", path)
	bin := filepath.Join(dir, "bin")
	must(t, os.MkdirAll(bin, 0o755))
	log := filepath.Join(dir, "calls")
	if answer != "" {
		must(t, os.WriteFile(filepath.Join(bin, "jhon-studio"), []byte("#!/bin/sh\necho \"$*\" >> '"+log+"'\ncat <<'EOF'\n"+answer+"\nEOF\n"), 0o755))
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	return log
}

// odd/tasks/jhon-studio.md S3: with integrations.studio on and jhon-studio
// answering, jhon start loads its plugin as a second --plugin-dir; off,
// jhon-studio is never asked and nothing is added.
func TestStartLoadsAnOnCLIIntegrationsPlugin(t *testing.T) {
	plugin := t.TempDir()
	answer := `{"version":"0.1.0","plugin_dir":"` + plugin + `","skill":"` + plugin + `/skills/video/SKILL.md"}`
	log := studioAt(t, "schema = 1\n\n[integrations]\nstudio = true\n", answer)
	args, notes := launchPlugins()
	if !slices.Equal(args, []string{"--plugin-dir", plugin}) || len(notes) != 0 {
		t.Errorf("on: args %q, notes %q", args, notes)
	}
	if b, _ := os.ReadFile(log); string(b) != "integration --json\n" {
		t.Errorf("on: calls %q", b)
	}
	log = studioAt(t, "schema = 1\n", answer)
	if args, notes := launchPlugins(); len(args) != 0 || len(notes) != 0 {
		t.Errorf("off: args %q, notes %q", args, notes)
	}
	if _, err := os.Stat(log); err == nil {
		t.Error("off: jhon-studio was asked")
	}
}

// On but not answering, start still starts, with one note saying why.
func TestStartWithAnOnCLIIntegrationThatDoesNotAnswerNotesItOnce(t *testing.T) {
	const on = "schema = 1\n\n[integrations]\nstudio = true\n"
	const tail = "; this session starts without its plugin"
	for _, c := range []struct{ what, answer, want string }{
		{"not on PATH", "", "jhon: integrations.studio is on, but jhon-studio is not on PATH" + tail},
		{"not its JSON", "nope", "jhon: integrations.studio is on, but jhon-studio integration --json: not the JSON {version, plugin_dir, skill}" + tail},
		{"no plugin folder", `{"plugin_dir":"/nonexistent/jhon-studio-plugin"}`, "jhon: integrations.studio is on, but its plugin_dir, /nonexistent/jhon-studio-plugin, is not a folder" + tail},
	} {
		studioAt(t, on, c.answer)
		args, notes := launchPlugins()
		if len(args) != 0 || !slices.Equal(notes, []string{c.want}) {
			t.Errorf("%s: args %q, notes %q", c.what, args, notes)
		}
	}
}

// A cli integration off at launch is no connector: it never keeps the
// claude.ai connectors out (PRESERVE, T42a).
func TestAnOffCLIIntegrationLeavesTheConnectorsAlone(t *testing.T) {
	if got := connectorsSwitch(t, "schema = 1\n\n[claude_code]\nconnectors = true\n\n[integrations]\nstudio = false\n"); got != "" {
		t.Errorf("studio off, connectors on: %q", got)
	}
}

// L11 (the verifier's probe): the claude.ai connectors follow
// claude_code.connectors (m365's setting) alone, whatever integrations.studio
// is: unset, off or on.
func TestConnectorsFollowM365AloneWhateverStudioIs(t *testing.T) {
	for _, studio := range []string{"", "\n[integrations]\nstudio = false\n", "\n[integrations]\nstudio = true\n"} {
		for _, connectors := range []bool{false, true} {
			text := "schema = 1\n\n[claude_code]\nconnectors = " + map[bool]string{false: "false", true: "true"}[connectors] + "\n" + studio
			want := "ENABLE_CLAUDEAI_MCP_SERVERS=false"
			if connectors {
				want = ""
			}
			if got := connectorsSwitch(t, text); got != want {
				t.Errorf("connectors %v, studio %q: %q, want %q", connectors, studio, got, want)
			}
		}
	}
}
