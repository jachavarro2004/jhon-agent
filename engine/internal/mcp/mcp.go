// Package mcp is `jhon mcp`: a read-only MCP server over stdio
// (JSON-RPC 2.0, one message per line). Its tools are `jhon audit --json`
// and `jhon status --json`, run as they are on the command line; it writes
// nothing and runs nothing else.
//
// It runs each tool as the jhon binary it is part of, so it never needs
// node.
package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// args is a tool call's arguments.
type args map[string]any

type tool struct {
	name        string
	description string
	inputSchema json.RawMessage
	argv        func(a args) ([]string, error)
}

// word is a string argument that cannot be read as a flag; "" when absent or empty.
func word(a args, key string) (string, error) {
	v := a[key]
	if v == nil || v == "" {
		return "", nil
	}
	s, ok := v.(string)
	if !ok || strings.HasPrefix(s, "-") {
		return "", errors.New(key + " must be a string that does not start with '-'")
	}
	return s, nil
}

// truthy is JavaScript's truthiness for a value decoded from JSON.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && !math.IsNaN(x)
	case string:
		return x != ""
	}
	return true
}

var tools = []tool{
	{
		name:        "audit",
		description: "Audit one Claude Code session (jhon audit --json): findings, outcomes, and with context the token use per agent, phase, and change.",
		inputSchema: json.RawMessage(`{"type":"object","properties":{"session":{"type":"string","description":"session id or transcript path"},"context":{"type":"boolean"}},"required":["session"]}`),
		argv: func(a args) ([]string, error) {
			session, err := word(a, "session")
			if err != nil {
				return nil, err
			}
			if session == "" {
				return nil, errors.New("session is required")
			}
			out := []string{"audit", session, "--json"}
			if truthy(a["context"]) {
				out = append(out, "--context")
			}
			return out, nil
		},
	},
	{
		name:        "status",
		description: "Informational LDD state of a change (jhon status --json).",
		inputSchema: json.RawMessage(`{"type":"object","properties":{"change":{"type":"string"},"root":{"type":"string","description":"project root"}}}`),
		argv: func(a args) ([]string, error) {
			change, err := word(a, "change")
			if err != nil {
				return nil, err
			}
			root, err := word(a, "root")
			if err != nil {
				return nil, err
			}
			var out []string
			if change != "" {
				out = append(out, change)
			}
			out = append([]string{"status"}, append(out, "--json")...)
			if root != "" {
				out = append(out, "--root", root)
			}
			return out, nil
		},
	},
}

// Run runs `jhon <args>` and reports its output and exit code; exited is
// false when it did not exit normally (a signal, or no start at all).
type Run func(args []string) (stdout, stderr string, code int, exited bool)

type content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolResult struct {
	Content []content `json:"content"`
	IsError bool      `json:"isError"`
}

func textResult(text string, isError bool) toolResult {
	return toolResult{[]content{{"text", text}}, isError}
}

// rpcError is a JSON-RPC error a request ends in.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return e.Message }

func callTool(run Run, name string, a args) (toolResult, error) {
	i := -1
	for j, t := range tools {
		if t.name == name {
			i = j
		}
	}
	if i < 0 {
		return toolResult{}, &rpcError{-32602, "unknown tool: " + name}
	}
	argv, err := tools[i].argv(a)
	if err != nil {
		return textResult(err.Error(), true), nil
	}
	stdout, stderr, code, exited := run(argv)
	// jhon audit exits 1 on a violation, which is a result; 2 is a usage or audit error.
	if !exited || code >= 2 {
		return textResult(js.Trim(stderr+stdout), true), nil
	}
	return textResult(stdout, false), nil
}

type reply struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result"`
}

type errorReply struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Error   *rpcError `json:"error"`
}

type initializeResult struct {
	ProtocolVersion any `json:"protocolVersion"`
	Capabilities    struct {
		Tools struct{} `json:"tools"`
	} `json:"capabilities"`
	ServerInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

type listedTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations struct {
		ReadOnlyHint bool `json:"readOnlyHint"`
	} `json:"annotations"`
}

// field is v[key] when v is an object; present is false otherwise, or when
// it lacks the key (JavaScript's undefined).
func field(v any, key string) (value any, present bool) {
	if m, ok := v.(map[string]any); ok {
		value, present = m[key]
	}
	return value, present
}

// handle is the reply to one message, nil for a notification (or anything
// without an id). A message that is the JSON null is a parse error: it has
// no id to read.
func handle(run Run, msg any) any {
	if msg == nil {
		return errorReply{"2.0", nil, &rpcError{-32700, "parse error"}}
	}
	id, ok := field(msg, "id")
	if !ok {
		return nil
	}
	id = numbers(id)
	params, _ := field(msg, "params")
	method, hasMethod := field(msg, "method")
	fail := func(e *rpcError) any { return errorReply{"2.0", id, e} }
	switch method {
	case "initialize":
		r := initializeResult{ProtocolVersion: "2025-06-18"}
		if v, _ := field(params, "protocolVersion"); v != nil {
			r.ProtocolVersion = numbers(v)
		}
		r.ServerInfo.Name, r.ServerInfo.Version = "jhon", "1"
		return reply{"2.0", id, r}
	case "ping":
		return reply{"2.0", id, struct{}{}}
	case "tools/list":
		list := make([]listedTool, len(tools))
		for i, t := range tools {
			list[i] = listedTool{Name: t.name, Description: t.description, InputSchema: t.inputSchema}
			list[i].Annotations.ReadOnlyHint = true
		}
		return reply{"2.0", id, struct {
			Tools []listedTool `json:"tools"`
		}{list}}
	case "tools/call":
		name := "undefined"
		if v, ok := field(params, "name"); ok {
			name = js.String(v)
		}
		a, _ := field(params, "arguments")
		m, _ := a.(map[string]any)
		r, err := callTool(run, name, m)
		if err != nil {
			return fail(err.(*rpcError))
		}
		return reply{"2.0", id, r}
	}
	shown := "undefined"
	if hasMethod {
		shown = js.String(method)
	}
	return fail(&rpcError{-32601, "method not found: " + shown})
}

// numbers is v with every number in its shortest form (js.NumberString),
// for a value a reply echoes.
func numbers(v any) any {
	switch x := v.(type) {
	case float64:
		return json.Number(js.NumberString(x))
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = numbers(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = numbers(e)
		}
		return out
	}
	return v
}

// line answers one line: "" when it takes no reply.
func line(run Run, l string) string {
	if js.Trim(l) == "" {
		return ""
	}
	var msg any
	var out any
	if err := json.Unmarshal([]byte(l), &msg); err != nil {
		out = errorReply{"2.0", nil, &rpcError{-32700, "parse error"}}
	} else {
		out = handle(run, msg)
	}
	if out == nil {
		return ""
	}
	s, err := js.StringifyCompact(out)
	if err != nil {
		return ""
	}
	return s + "\n"
}

// Serve answers messages from in on out, one per line, until in ends. A
// line ends at \n, \r\n, or \r, as node's readline reads it.
func Serve(in io.Reader, out io.Writer, run Run) error {
	r := bufio.NewReader(in)
	w := bufio.NewWriter(out)
	for {
		chunk, err := r.ReadString('\n')
		for _, l := range strings.Split(strings.TrimSuffix(chunk, "\n"), "\r") {
			if reply := line(run, l); reply != "" {
				w.WriteString(reply)
				if ferr := w.Flush(); ferr != nil {
					return ferr
				}
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
