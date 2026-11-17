// `jhon mcp`: a read-only MCP server over stdio (JSON-RPC 2.0, one message
// per line) exposing `jhon audit` and `jhon status` as tools.
package audit

import (
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// mcpSession sends msgs to one server process and returns its replies by id.
func mcpSession(t *testing.T, msgs ...map[string]any) map[float64]map[string]any {
	t.Helper()
	var in strings.Builder
	for _, m := range msgs {
		in.WriteString(js(t, m) + "\n")
	}
	r := support.Run(t, support.Cmd{Args: []string{support.Bin(t), "mcp"}, Env: jhonEnv(), Stdin: in.String()})
	equal(t, r.Code, 0, r.Stderr)
	out := map[float64]map[string]any{}
	for _, l := range strings.Split(r.Stdout, "\n") {
		if l != "" {
			m := parse(t, l)
			id, _ := m["id"].(float64)
			out[id] = m
		}
	}
	return out
}

var mcpInit = map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize",
	"params": map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "0"}}}

func toolCall(id int, name string, args map[string]any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}
}

func TestMCP(t *testing.T) {
	sessionFile := func(t *testing.T) string { return mainOf(t, "clean") }

	t.Run("initialize names the server and offers tools", func(t *testing.T) {
		t.Parallel()
		r := mcpSession(t, mcpInit)[1]
		equal(t, get(r, "result", "serverInfo", "name"), any("jhon"))
		if get(r, "result", "capabilities", "tools") == nil {
			t.Fatal(js(t, r))
		}
	})

	t.Run("tools/list offers exactly the read-only audit and status tools", func(t *testing.T) {
		t.Parallel()
		r := mcpSession(t, mcpInit, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})[2]
		var names []string
		for _, tool := range get(r, "result", "tools").([]any) {
			name := get(tool, "name").(string)
			names = append(names, name)
			equal(t, get(tool, "annotations", "readOnlyHint"), any(true), name)
		}
		slices.Sort(names)
		equal(t, names, []string{"audit", "status"})
	})

	t.Run("audit returns the --json report of a session", func(t *testing.T) {
		t.Parallel()
		r := mcpSession(t, mcpInit, toolCall(3, "audit", map[string]any{"session": sessionFile(t), "context": true}))[3]
		equal(t, get(r, "result", "isError"), any(false), js(t, r))
		direct := audit(t, []string{sessionFile(t), "--json", "--context"})
		equal(t, parse(t, get(r, "result", "content", 0, "text").(string)), parse(t, direct.Stdout))
	})

	t.Run("an argument that looks like a flag is refused, not passed on", func(t *testing.T) {
		t.Parallel()
		r := mcpSession(t, mcpInit, toolCall(4, "audit", map[string]any{"session": "--help"}))[4]
		equal(t, get(r, "result", "isError"), any(true))
		match(t, get(r, "result", "content", 0, "text").(string), `session must be a string that does not start with '-'`)
	})

	t.Run("an unknown tool or method is a JSON-RPC error", func(t *testing.T) {
		t.Parallel()
		m := mcpSession(t, mcpInit, toolCall(5, "deploy", map[string]any{}), map[string]any{"jsonrpc": "2.0", "id": 6, "method": "nope"})
		if m[5]["error"] == nil {
			t.Fatal(js(t, m[5]))
		}
		equal(t, get(m[6], "error", "code"), any(float64(-32601)))
	})
}
