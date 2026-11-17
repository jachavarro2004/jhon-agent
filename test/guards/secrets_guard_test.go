// secrets-guard, the PreToolUse hook every agent carries: no agent reads,
// greps, edits, or writes a file that holds credentials (.env and its
// stages, private keys, credentials.json); the owner handles them. A
// template of one (.env.example, .env.sample) passes like any file. A shell
// command that names one is safety-guard's (safety_guard_test.go).

package guards

import (
	"path/filepath"
	"testing"
)

func TestSecretsGuard(t *testing.T) {
	t.Parallel()
	p := realTemp(t)
	call := func(tool string, input map[string]any) int {
		t.Helper()
		payload := map[string]any{"tool_name": tool, "tool_input": input, "cwd": p, "agent_id": "a1", "agent_type": "worker"}
		return hook(t, nil, "secrets-guard", []string{"--agent", "*"}, payload).Code
	}
	at := func(name string) string { return filepath.Join(p, name) }
	for _, c := range []struct {
		tool  string
		input map[string]any
	}{
		{"Read", map[string]any{"file_path": at(".env")}},
		{"Read", map[string]any{"file_path": at("backend/.env.production")}},
		{"Read", map[string]any{"file_path": at(".envrc")}},
		{"Read", map[string]any{"file_path": at("deploy/server.key")}},
		{"Read", map[string]any{"file_path": "/home/x/.ssh/id_ed25519"}},
		{"Read", map[string]any{"file_path": at("credentials.json")}},
		{"Edit", map[string]any{"file_path": at(".env.local"), "old_string": "a", "new_string": "b"}},
		{"Write", map[string]any{"file_path": at(".env"), "content": "X=1\n"}},
		{"Grep", map[string]any{"pattern": "KEY", "path": at(".env")}},
		{"Grep", map[string]any{"pattern": "KEY", "glob": ".env*"}},
	} {
		if got := call(c.tool, c.input); got != 2 {
			t.Errorf("%s %v: exit %d, want 2 (refused)", c.tool, c.input, got)
		}
	}
	for _, c := range []struct {
		tool  string
		input map[string]any
	}{
		{"Read", map[string]any{"file_path": at(".env.example")}},
		{"Read", map[string]any{"file_path": at("backend/.env.local.sample")}},
		{"Edit", map[string]any{"file_path": at(".env.example"), "old_string": "a", "new_string": "b"}},
		{"Write", map[string]any{"file_path": at(".env.template"), "content": "X=\n"}},
		{"Read", map[string]any{"file_path": at("src/env.py")}},
		{"Read", map[string]any{"file_path": at("deploy/server.pub")}},
		{"Grep", map[string]any{"pattern": "KEY", "path": at("src")}},
		{"Grep", map[string]any{"pattern": "KEY", "glob": "*.ts"}},
	} {
		if got := call(c.tool, c.input); got != 0 {
			t.Errorf("%s %v: exit %d, want 0 (allowed)", c.tool, c.input, got)
		}
	}
}
