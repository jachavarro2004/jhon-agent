package render

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T133e (L1016): context7, library and framework documentation as an MCP
// server, for explore and worker alone. It is the hosted endpoint: nothing is
// installed or pinned, and the render writes a URL and never reaches the
// network. integrations.context7 = false takes every trace of it out.

// context7 is pi's mcp.json entry: a URL and the exposure a child's --tools
// needs; no command, no header, no key.
var context7Entry = map[string]any{"url": "https://mcp.context7.com/mcp", "exposure": "direct"}

// The tools as each runtime names them: Claude Code keeps the server's
// names, pi writes every character but letters, digits and `_` as `_`.
var (
	context7Claude = []string{"mcp__context7__resolve-library-id", "mcp__context7__query-docs"}
	context7Pi     = []string{"mcp__context7__resolve_library_id", "mcp__context7__query_docs"}
)

// context7Home is a HOME with both runtimes rendered, the owner's settings
// file holding settings ("" for none); built once per value, read-only.
func context7Home(t *testing.T, shared *lazyHome, settings string) string {
	t.Helper()
	return shared.get(t, func(home string) string {
		write(t, filepath.Join(home, ".pi", "agent", "auth.json"), "{}\n")
		write(t, filepath.Join(home, ".claude", ".keep"), "")
		if settings != "" {
			write(t, settingsFile(home), settings)
		}
		if r := install(t, home, repoRoot); r.Code != 0 {
			return "the render failed:\n" + r.Out()
		}
		return ""
	})
}

var context7On, context7Off lazyHome

const context7OffSettings = "schema = 1\n\n[integrations]\ncontext7 = false\n"

// rendered is every file the render wrote for both runtimes, the home's own
// path taken out of names and contents.
func context7Rendered(t *testing.T, home string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, dir := range []string{filepath.Join(home, ".claude"), filepath.Join(home, ".pi", "jhon")} {
		for p, body := range snapshot(t, dir) {
			out[strings.ReplaceAll(p, home, "~")] = strings.ReplaceAll(body, home, "~")
		}
	}
	return out
}

func TestRenderContext7(t *testing.T) {
	t.Parallel()

	t.Run("pi: mcp.json mounts context7's hosted endpoint beside engram, codegraph and jhon, and only explore and worker hold its tools", func(t *testing.T) {
		t.Parallel()
		home := context7Home(t, &context7On, "")
		pi := filepath.Join(home, ".pi", "jhon")
		servers := readJSON(t, filepath.Join(pi, "mcp.json")).(map[string]any)["mcpServers"].(map[string]any)
		var names []string
		for name := range servers {
			names = append(names, name)
		}
		equal(t, sorted(names), []string{"codegraph", "context7", "engram", "jhon"})
		equal(t, servers["context7"], jsonOf(context7Entry), "a URL and its exposure: no command, no package, no header, no key")
		_, delegable := manifestAgents(t)
		for _, name := range delegable {
			file := support.Read(t, filepath.Join(pi, "agents", name+".md"))
			tools := listItems(frontmatter(file))
			if name == "jhon-scout" || name == "jhon-builder" {
				if n := len(tools); n < 2 {
					t.Fatal(name, tools)
				}
				equal(t, tools[len(tools)-2:], context7Pi, name+": context7's tools, after the ones it had")
				match(t, flat(file), `(?i)library or framework documentation: context7 before guessing`, name)
				continue
			}
			noMatch(t, file, `(?i)context7`, name)
		}
		for _, f := range []string{"jhon-orchestrator.md", "jhon-orchestrator-writer.md", "settings.json", "jhon.json"} {
			noMatch(t, support.Read(t, filepath.Join(pi, f)), `(?i)context7`, f)
		}
	})

	t.Run("Claude Code: explore and worker name the tools of a server called context7, which the owner's Claude Code has or not; no other agent does", func(t *testing.T) {
		t.Parallel()
		home := context7Home(t, &context7On, "")
		primary, delegable := manifestAgents(t)
		for _, name := range append(primary, delegable...) {
			file := claudeAgent(t, home, name)
			tools := claudeTools(frontmatter(file))
			if name == "jhon-scout" || name == "jhon-builder" {
				equal(t, tools[len(tools)-2:], context7Claude, name)
				match(t, flat(file), `(?i)library or framework documentation: context7 before guessing`, name)
				continue
			}
			noMatch(t, file, `(?i)context7`, name)
		}
	})

	t.Run("integrations.context7 = false: no server, no tool and no sentence, in either runtime", func(t *testing.T) {
		t.Parallel()
		home := context7Home(t, &context7Off, context7OffSettings)
		for p, body := range context7Rendered(t, home) {
			noMatch(t, body, `(?i)context7`, p)
		}
		pi := filepath.Join(home, ".pi", "jhon")
		current := filepath.Join(home, ".local", "share", "jhon", "current")
		equal(t, readJSON(t, filepath.Join(pi, "mcp.json")), jsonOf(map[string]any{"mcpServers": map[string]any{
			"engram":    map[string]any{"command": "engram", "args": []any{"mcp", "--tools=agent"}, "exposure": "direct"},
			"codegraph": map[string]any{"command": "codegraph", "args": []any{"serve", "--mcp"}, "exposure": "direct"},
			"jhon":      map[string]any{"command": filepath.Join(current, "build", "jhon"), "args": []any{"mcp"}, "exposure": "direct"}}}))
		equal(t, listItems(frontmatter(support.Read(t, filepath.Join(pi, "agents", "jhon-builder.md")))), []string{"read", "grep", "find", "ls", "edit", "write", "bash", "mcp__engram__mem_search", "mcp__engram__mem_get_observation",
			"mcp__engram__mem_save", "mcp__engram__mem_update", "mcp__engram__mem_judge", "mcp__codegraph__codegraph_explore", "todo"}, "the worker's tools as they were")
		explore := listItems(frontmatter(support.Read(t, filepath.Join(pi, "agents", "jhon-scout.md"))))
		equal(t, explore[len(explore)-1], "todo", "explore's tools end as they did")
	})

	t.Run("on against off: mcp.json and the two agents differ, by the server, the tools and one sentence; every other rendered file is byte-identical", func(t *testing.T) {
		t.Parallel()
		on, off := context7Rendered(t, context7Home(t, &context7On, "")), context7Rendered(t, context7Home(t, &context7Off, context7OffSettings))
		var differ []string
		for p, body := range on {
			if other, ok := off[p]; !ok || other != body {
				differ = append(differ, p)
			}
		}
		for p := range off {
			if _, ok := on[p]; !ok {
				differ = append(differ, p)
			}
		}
		sort.Strings(differ)
		equal(t, differ, []string{"~/.claude/agents/jhon-builder.md", "~/.claude/agents/jhon-scout.md", "~/.pi/jhon/agents/jhon-builder.md", "~/.pi/jhon/agents/jhon-scout.md", "~/.pi/jhon/mcp.json"})
		// What an agent file gains is its tools and the one sentence, whole lines of it.
		without := func(text string, drop func(line string) bool) string {
			var kept []string
			for _, line := range strings.Split(text, "\n") {
				if !drop(line) {
					kept = append(kept, line)
				}
			}
			return strings.Join(kept, "\n")
		}
		sentence := func(line string) bool { return strings.Contains(line, "context7 before guessing") }
		for _, name := range []string{"jhon-scout", "jhon-builder"} {
			pi := "~/.pi/jhon/agents/" + name + ".md"
			equal(t, without(on[pi], func(line string) bool {
				return sentence(line) || line == "  - "+context7Pi[0] || line == "  - "+context7Pi[1]
			}), off[pi], pi)
			claude := "~/.claude/agents/" + name + ".md"
			equal(t, without(strings.Replace(on[claude], ", "+strings.Join(context7Claude, ", "), "", 1), sentence), off[claude], claude)
		}
	})
}
