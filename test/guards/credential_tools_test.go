// A file tool's glob (Grep's glob, Glob's pattern) is not read for
// credentials: a search names files and reads them, and reading is not
// limited by guard code (T151, L1366). The glob walk that served it is gone.

package guards

import (
	"path/filepath"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// toolCall runs secrets-guard on one call of tool in p.
func toolCall(t *testing.T, p, tool string, input map[string]any) int {
	t.Helper()
	payload := map[string]any{"tool_name": tool, "tool_input": input, "cwd": p, "agent_id": "a1", "agent_type": "worker"}
	return hook(t, nil, "secrets-guard", []string{"--agent", "*"}, payload).Code
}

// T151: globs that once walked into a credentials file, inside the project,
// outside it, or through a link, are allowed.
func TestToolGlobsReachingCredentialsAllowed(t *testing.T) {
	t.Parallel()
	parent := realTemp(t)
	p := filepath.Join(parent, "proj")
	support.Write(t, p, map[string]string{"i18n/en.json": "{}\n", "sub/credentials.json": "{}\n", "sub/.env": "X=fake\n"})
	support.GitInit(t, p)
	support.Write(t, parent, map[string]string{"srv/credentials.json": "{}\n"})
	symlink(t, filepath.Join(parent, "srv"), filepath.Join(p, "lnd"))
	for _, c := range []struct {
		tool string
		in   map[string]any
	}{
		{"Grep", map[string]any{"pattern": "x", "glob": "*.json"}},
		{"Grep", map[string]any{"pattern": "x", "glob": ".env*"}},
		{"Grep", map[string]any{"pattern": "x", "glob": "*.json", "path": parent}},
		{"Grep", map[string]any{"pattern": "x", "glob": "*.json", "path": "lnd"}},
		{"Grep", map[string]any{"pattern": "x", "glob": "{a,b}/*.json"}},
		{"Glob", map[string]any{"pattern": "../*.json"}},
		{"Glob", map[string]any{"pattern": "**/credentials.json"}},
		{"Glob", map[string]any{"pattern": "/srv/**/id_*"}},
	} {
		if got := toolCall(t, p, c.tool, c.in); got != 0 {
			t.Errorf("%s %v: exit %d, want 0", c.tool, c.in, got)
		}
	}
}
