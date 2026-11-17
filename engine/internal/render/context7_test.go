package render

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/manifest"
)

// writeFile is a settings file holding text.
func writeFile(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.toml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// T133e (L1016, G3): context7 is its hosted endpoint, so the versions table
// gains no pin and doctor compares no tool for it.
func TestContext7IsAHostedEndpointWithNoPin(t *testing.T) {
	if m, _ := integrations.Get("context7"); m.Doctor.URL != "https://mcp.context7.com/mcp" || m.Pi == nil || m.Pi.URL != m.Doctor.URL {
		t.Errorf("context7's url = %+v", m)
	}
	for _, tool := range Tools {
		if strings.Contains(tool.Command, "context7") {
			t.Errorf("a pinned tool for a hosted endpoint: %+v", tool)
		}
	}
	t.Setenv("JHON_SETTINGS", filepath.Join(t.TempDir(), "none.toml"))
	e := engineAt(t, repo, "--validate")
	entry := js.Prop(js.Prop(pi{e}.piMcp(), "mcpServers"), "context7")
	if got := js.StringifyValueCompact(entry); got != `{"url":"https://mcp.context7.com/mcp","exposure":"direct"}` {
		t.Errorf("mcp.json's context7 = %s", got)
	}
	for _, pkg := range (pi{e}).piPackages() {
		if s, ok := pkg.(string); ok && strings.Contains(s, "context7") {
			t.Errorf("a pi package for a hosted endpoint: %s", s)
		}
	}
}

// pi names a server's tool with every character but letters, digits and `_`
// written `_` (pi-coding-agent's createMcpToolName); the capability's Claude
// Code names are the server's own.
// context7Cap is the capability integrations/context7/manifest.yaml grants.
const context7Cap = "context7"

func TestContext7ToolsInEachRuntime(t *testing.T) {
	t.Setenv("JHON_SETTINGS", filepath.Join(t.TempDir(), "none.toml"))
	e := engineAt(t, repo, "--validate")
	claude, ok := e.m.Capabilities.Get(context7Cap)
	if !ok || !slices.Equal(claude, []string{"mcp__context7__resolve-library-id", "mcp__context7__query-docs"}) {
		t.Fatalf("agents.yaml's context7 capability = %v", claude)
	}
	other := regexp.MustCompile(`[^A-Za-z0-9_]`)
	var want []string
	for _, name := range claude {
		want = append(want, other.ReplaceAllString(name, "_"))
	}
	if !slices.Equal(e.piTools(manifest.Agent{Caps: []string{context7Cap}}), want) {
		t.Errorf("pi's context7 tools = %v, want %v", e.piTools(manifest.Agent{Caps: []string{context7Cap}}), want)
	}
	var holders []string
	for _, a := range e.m.Agents {
		if slices.Contains(a.Caps, context7Cap) {
			holders = append(holders, a.Name)
		}
	}
	if !slices.Equal(holders, []string{"explore", "worker"}) {
		t.Errorf("the agents holding context7 = %v, want explore and worker (L1000)", holders)
	}
}

// integrations.context7 = false, or settings that cannot be read: no agent
// holds the capability, mcp.json mounts no context7, and a body's sentence
// naming it is not rendered.
func TestContext7OffLeavesNoTrace(t *testing.T) {
	for name, text := range map[string]string{"off": "schema = 1\n\n[integrations]\ncontext7 = false\n", "unreadable": "[broken\n"} {
		t.Setenv("JHON_SETTINGS", writeFile(t, text))
		e := engineAt(t, repo, "--validate")
		if js.Prop(pi{e}.piMcp(), "mcpServers").(*js.Object).Has("context7") {
			t.Errorf("%s: mcp.json mounts context7", name)
		}
		for _, a := range e.m.Agents {
			if slices.Contains(a.Caps, context7Cap) {
				t.Errorf("%s: %s holds the capability", name, a.Name)
			}
			for _, rt := range []string{"claude-code", "pi"} {
				// The install's path (atInstall) holds the test's name.
				if strings.Contains(strings.ToLower(strings.ReplaceAll(e.bodyFor(rt, a), e.current(), "")), "context7") {
					t.Errorf("%s: %s's %s body names context7", name, a.Name, rt)
				}
			}
		}
	}
}

// With it on, each holder's body carries the sentence as whole lines, so
// taking it out leaves the rest of the body as written.
func TestContext7SentenceIsWholeLines(t *testing.T) {
	t.Setenv("JHON_SETTINGS", writeFile(t, "schema = 1\n"))
	e := engineAt(t, repo, "--validate")
	for _, a := range e.m.Agents {
		if !slices.Contains(a.Caps, context7Cap) {
			continue
		}
		var named []string
		for _, line := range strings.Split(e.bodyOf(a), "\n") {
			if strings.Contains(strings.ToLower(line), "context7") {
				named = append(named, line)
			}
		}
		if len(named) != 1 || !regexp.MustCompile(`^(- )?[A-Z].*\.$`).MatchString(named[0]) {
			t.Errorf("%s: the lines naming context7 are not one whole sentence: %q", a.Name, named)
		}
	}
}
