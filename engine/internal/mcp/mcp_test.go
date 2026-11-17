package mcp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// fake is a Run that records each call and answers with a fixed result.
type fake struct {
	calls          [][]string
	stdout, stderr string
	code           int
	exited         bool
}

func (f *fake) run(args []string) (string, string, int, bool) {
	f.calls = append(f.calls, args)
	return f.stdout, f.stderr, f.code, f.exited
}

// serve sends lines to one server and returns its output lines.
func serve(t *testing.T, f *fake, input string) []string {
	t.Helper()
	var out strings.Builder
	if err := Serve(strings.NewReader(input), &out, f.run); err != nil {
		t.Fatal(err)
	}
	if out.Len() == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
}

func TestInitializeAndList(t *testing.T) {
	out := serve(t, &fake{}, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}
{"jsonrpc":"2.0","id":"two","method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"ping"}
{"jsonrpc":"2.0","id":4.0,"method":"initialize"}
`)
	want := []string{
		`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"jhon","version":"1"}}}`,
		`{"jsonrpc":"2.0","id":"two","result":{"tools":[` +
			`{"name":"audit","description":"Audit one Claude Code session (jhon audit --json): findings, outcomes, and with context the token use per agent, phase, and change.","inputSchema":{"type":"object","properties":{"session":{"type":"string","description":"session id or transcript path"},"context":{"type":"boolean"}},"required":["session"]},"annotations":{"readOnlyHint":true}},` +
			`{"name":"status","description":"Informational LDD state of a change (jhon status --json).","inputSchema":{"type":"object","properties":{"change":{"type":"string"},"root":{"type":"string","description":"project root"}}},"annotations":{"readOnlyHint":true}}]}}`,
		`{"jsonrpc":"2.0","id":3,"result":{}}`,
		`{"jsonrpc":"2.0","id":4,"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"jhon","version":"1"}}}`,
	}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(out, "\n"), strings.Join(want, "\n"))
	}
}

func TestToolCallsRunTheSubcommand(t *testing.T) {
	f := &fake{stdout: "{\"ok\": true}\n", code: 1, exited: true}
	out := serve(t, f, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"audit","arguments":{"session":"abc","context":1}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"audit","arguments":{"session":"abc","context":""}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"status","arguments":{"change":"c","root":"/r"}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"status"}}
`)
	want := [][]string{
		{"audit", "abc", "--json", "--context"},
		{"audit", "abc", "--json"},
		{"status", "c", "--json", "--root", "/r"},
		{"status", "--json"},
	}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls %v, want %v", f.calls, want)
	}
	// Exit 1 is a result, not an error.
	if out[0] != `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"{\"ok\": true}\n"}],"isError":false}}` {
		t.Errorf("%s", out[0])
	}
}

func TestFailedRunIsAnErrorResult(t *testing.T) {
	f := &fake{stdout: "out\n", stderr: "  usage\n", code: 2, exited: true}
	out := serve(t, f, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"status","arguments":{}}}`+"\n")
	if out[0] != `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"usage\nout"}],"isError":true}}` {
		t.Errorf("%s", out[0])
	}
	f = &fake{stdout: "partial", exited: false}
	out = serve(t, f, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"status","arguments":{}}}`+"\n")
	if !strings.Contains(out[0], `"isError":true`) {
		t.Errorf("a killed run: %s", out[0])
	}
}

func TestBadArgumentsAreRefusedNotPassedOn(t *testing.T) {
	f := &fake{}
	out := serve(t, f, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"audit","arguments":{"session":"--help"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"audit","arguments":{}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"status","arguments":{"root":7}}}
`)
	want := []string{
		`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"session must be a string that does not start with '-'"}],"isError":true}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"session is required"}],"isError":true}}`,
		`{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"root must be a string that does not start with '-'"}],"isError":true}}`,
	}
	if !reflect.DeepEqual(out, want) || len(f.calls) != 0 {
		t.Errorf("got %v, calls %v", out, f.calls)
	}
}

func TestErrorsNotificationsAndLines(t *testing.T) {
	out := serve(t, &fake{}, "{\"jsonrpc\":\"2.0\",\"id\":5,\"method\":\"tools/call\",\"params\":{\"name\":\"deploy\"}}\r\n"+
		"{\"jsonrpc\":\"2.0\",\"id\":6,\"method\":\"nope\"}\r"+
		"{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n"+
		"   \n"+
		"not json\n"+
		"null\n"+
		"[1]\n"+
		"{\"id\":null}\n"+
		"{\"id\":7,\"method\":\"tools/call\"}")
	want := []string{
		`{"jsonrpc":"2.0","id":5,"error":{"code":-32602,"message":"unknown tool: deploy"}}`,
		`{"jsonrpc":"2.0","id":6,"error":{"code":-32601,"message":"method not found: nope"}}`,
		`{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"parse error"}}`,
		`{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"parse error"}}`,
		`{"jsonrpc":"2.0","id":null,"error":{"code":-32601,"message":"method not found: undefined"}}`,
		`{"jsonrpc":"2.0","id":7,"error":{"code":-32602,"message":"unknown tool: undefined"}}`,
	}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(out, "\n"), strings.Join(want, "\n"))
	}
	for _, l := range out {
		if !json.Valid([]byte(l)) {
			t.Errorf("invalid JSON: %s", l)
		}
	}
}
