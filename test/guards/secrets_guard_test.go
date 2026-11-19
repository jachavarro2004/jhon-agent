// secrets-guard, the PreToolUse hook every agent carries: no agent edits or
// writes a file that holds credentials (.env and its stages, private keys,
// credentials.json); the owner handles them. A template of one
// (.env.example, .env.sample) passes like any file. Reading one is not
// judged: the always-on prose says never to (T151, L1366, L1369). A shell
// command's write or delete of one is safety-guard's (safety_guard_test.go).

package guards

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
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
		{"Edit", map[string]any{"file_path": at(".env.local"), "old_string": "a", "new_string": "b"}},
		{"Write", map[string]any{"file_path": at(".env"), "content": "X=1\n"}},
		{"Write", map[string]any{"file_path": at("deploy/server.key"), "content": "k\n"}},
		{"MultiEdit", map[string]any{"file_path": at("credentials.json"), "edits": []any{}}},
		{"NotebookEdit", map[string]any{"notebook_path": at(".envrc"), "new_source": "x"}},
	} {
		if got := call(c.tool, c.input); got != 2 {
			t.Errorf("%s %v: exit %d, want 2 (refused)", c.tool, c.input, got)
		}
	}
	for _, c := range []struct {
		tool  string
		input map[string]any
	}{
		{"Read", map[string]any{"file_path": at(".env")}},
		{"Read", map[string]any{"file_path": at("backend/.env.production")}},
		{"Read", map[string]any{"file_path": at("deploy/server.key")}},
		{"Read", map[string]any{"file_path": at("credentials.json")}},
		{"Grep", map[string]any{"pattern": "KEY", "path": at(".env")}},
		{"Grep", map[string]any{"pattern": "KEY", "glob": ".env*"}},
		{"Read", map[string]any{"file_path": at(".env.example")}},
		{"Read", map[string]any{"file_path": at("backend/.env.local.sample")}},
		{"Edit", map[string]any{"file_path": at(".env.example"), "old_string": "a", "new_string": "b"}},
		{"Write", map[string]any{"file_path": at(".env.template"), "content": "X=\n"}},
		{"Read", map[string]any{"file_path": at("src/env.py")}},
		{"Grep", map[string]any{"pattern": "KEY", "glob": "*.ts"}},
	} {
		if got := call(c.tool, c.input); got != 0 {
			t.Errorf("%s %v: exit %d, want 0 (allowed)", c.tool, c.input, got)
		}
	}
}

// T151 (L1366, L1394): through pre-tool, as Claude Code runs the guards, a
// read of a credentials file or a home credentials directory is allowed; an
// edit-tool write of one and a shell delete of one stay refused (L1368).
func TestCredentialReadsAllowedWritesRefusedThroughPreTool(t *testing.T) {
	t.Parallel()
	s, home := homeSG(t)
	support.Write(t, home, map[string]string{"keys/cert.pem": "fake\n"})
	pre := func(tool string, in map[string]any, cwd string) support.Result {
		t.Helper()
		payload := map[string]any{"tool_name": tool, "tool_input": in, "cwd": cwd, "agent_id": "a1", "agent_type": "worker", "session_id": "s"}
		return hook(t, s.env, "pre-tool", []string{"--agent", "worker", "safety-guard", "secrets-guard"}, payload)
	}
	for _, c := range []struct {
		tool string
		in   map[string]any
		cwd  string
	}{
		{"Read", map[string]any{"file_path": filepath.Join(s.p, ".env")}, s.p},
		{"Grep", map[string]any{"pattern": "x", "path": "~/.aws"}, s.p},
		{"Grep", map[string]any{"pattern": "x", "path": filepath.Join(home, ".aws")}, s.p},
		{"Glob", map[string]any{"pattern": "**/*.pem"}, home},
		{"Glob", map[string]any{"pattern": "**/*.pem", "path": "~"}, s.p},
	} {
		if r := pre(c.tool, c.in, c.cwd); r.Code != 0 {
			t.Errorf("%s %v: exit %d, want 0 (a read is not judged): %s", c.tool, c.in, r.Code, r.Stderr)
		}
	}
	env := filepath.Join(s.p, ".env")
	want := "secrets-guard: %s of " + env + " holds credentials, which the owner handles: no agent reads, edits, or deletes it. " +
		"A template such as .env.example is read and edited like any file; ask the owner for a value the work needs.\n"
	for _, c := range []struct {
		tool string
		in   map[string]any
	}{
		{"Edit", map[string]any{"file_path": env, "old_string": "X", "new_string": "Y"}},
		{"Write", map[string]any{"file_path": env, "content": "X=1\n"}},
	} {
		r := pre(c.tool, c.in, s.p)
		if r.Code != 2 || r.Stderr != strings.Replace(want, "%s", c.tool, 1) {
			t.Errorf("%s .env: exit %d stderr %q, want 2 and %q", c.tool, r.Code, r.Stderr, strings.Replace(want, "%s", c.tool, 1))
		}
	}
	r := pre("Bash", cmd("rm .env"), s.p)
	if r.Code != 2 || !strings.Contains(r.Stderr, ".env holds credentials, which the owner handles") {
		t.Errorf("Bash rm .env: exit %d stderr %q, want 2 and the credentials refusal", r.Code, r.Stderr)
	}
	if !exists(env) {
		t.Errorf(".env was removed")
	}
}
