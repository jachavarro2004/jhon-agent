package render

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/integrations"
)

// piBuiltinTools are pi's own tools.
var piBuiltinTools = []string{"read", "bash", "edit", "write", "grep", "find", "ls"}

// claudeCodeTools are Claude Code's tool names, which pi's render never names.
var claudeCodeTools = []string{"Bash", "Read", "Write", "Edit", "Grep", "Glob", "Monitor", "Agent", "SendMessage",
	"AskUserQuestion", "ExitPlanMode", "TaskCreate", "TaskUpdate", "TaskList", "WebSearch", "WebFetch", "ToolSearch"}

var (
	piRegistered = regexp.MustCompile(`registerTool\(\{\s*name:\s*("([a-z_]+)"|([A-Z_]+))`)
	toolConst    = regexp.MustCompile(`(?m)^export const ([A-Z_]+) = "([a-z_]+)";`)
	backticked   = regexp.MustCompile("`([A-Za-z][A-Za-z0-9_-]*)`")
)

// shellTools are the tools the jhon shell registers in pi (shell/extensions,
// shell/lib), each name resolved from its constant.
func shellTools(t *testing.T) []string {
	t.Helper()
	var src strings.Builder
	for _, dir := range []string{"shell/extensions", "shell/lib", "shell/lib/ui"} {
		files, _ := filepath.Glob(filepath.Join(repo, dir, "*.ts"))
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			src.Write(b)
			src.WriteByte('\n')
		}
	}
	consts := map[string]string{}
	for _, m := range toolConst.FindAllStringSubmatch(src.String(), -1) {
		consts[m[1]] = m[2]
	}
	var out []string
	for _, m := range piRegistered.FindAllStringSubmatch(src.String(), -1) {
		name := m[2]
		if name == "" {
			name = consts[m[3]]
		}
		if name == "" {
			t.Fatalf("tool constant %s does not resolve", m[3])
		}
		out = append(out, name)
	}
	return out
}

// T146 (L1211, FA1): every tool name pi's render mentions is a tool pi
// has: its own, the shell's, or an integration server's under its mcp__
// prefix. A Claude Code tool name, or an MCP tool named without its server
// prefix, is a call pi cannot make.
func TestPiRenderNamesOnlyPiTools(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	p := pi{e}
	real := append(slices.Clone(piBuiltinTools), shellTools(t)...)
	var mcp []string
	for _, c := range e.m.Capabilities.Keys() {
		m, ok := integrations.ByCapability(c)
		if !ok {
			continue
		}
		list, _ := e.m.Capabilities.Get(c)
		pt := integrationPiTools(m, c, list)
		real = append(real, pt...)
		mcp = append(mcp, list...)
	}
	// Mentions: a tool name of either runtime, or an MCP tool's bare name.
	tool := map[string]bool{}
	for _, n := range append(append(slices.Clone(real), claudeCodeTools...), mcp...) {
		tool[n] = true
		if i := strings.LastIndex(n, "__"); strings.HasPrefix(n, "mcp__") && i > 0 {
			tool[n[i+2:]] = true
		}
	}
	for _, f := range p.Files() {
		if !strings.HasSuffix(f.Path, ".md") {
			continue
		}
		for _, m := range backticked.FindAllStringSubmatch(f.Content, -1) {
			if name := m[1]; tool[name] && !slices.Contains(real, name) {
				t.Errorf("%s names `%s`, which is no pi tool", f.Path, name)
			}
		}
	}
	if !slices.Contains(real, "bash_background") || !slices.Contains(real, "subagent_continue") || !slices.Contains(real, "mcp__engram__mem_save") {
		t.Fatalf("pi's tools are not read: %v", real)
	}
}
