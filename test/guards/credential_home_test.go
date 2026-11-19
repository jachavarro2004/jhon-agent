// T87c (L636), T151 (L1366): secrets-guard with a scratch HOME that holds
// fake key and cloud credential files. A file tool's read or search of a
// home credentials directory is not judged; an edit tool's write into one is
// refused (L638, guard_gaps_t94_test.go). A shell command is not read for
// credentials (T128f, L1366).

package guards

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// homeSG is newSG with HOME a scratch directory holding fake credentials
// (never the real home) and a project .env and .pem.
func homeSG(t *testing.T) (sgEnv, string) {
	t.Helper()
	s := newSG(t)
	home := mkdir(t, filepath.Join(s.scratch, "home"))
	support.Write(t, home, map[string]string{
		".ssh/id_rsa": "fake\n", ".ssh/config.json": "{}\n", ".aws/credentials": "[default]\nfake\n",
		".config/gcloud/application_default_credentials.json": "{}\n", "settings.json": "{}\n",
	})
	support.Write(t, s.p, map[string]string{".env": "X=fake\n", "server.pem": "fake\n", "a.key": "fake\n", "id_ed25519": "fake\n",
		"credentials.json": "{}\n", ".netrc": "fake\n", "creds.json": "{}\n"})
	s.env = support.Env("HOME="+home, "XDG_STATE_HOME="+s.state, "TMPDIR="+s.tmp)
	return s, home
}

func homeToolCall(t *testing.T, s sgEnv, tool string, input map[string]any) int {
	t.Helper()
	payload := map[string]any{"tool_name": tool, "tool_input": input, "cwd": s.p, "agent_id": "a1", "agent_type": "worker"}
	return hook(t, s.env, "secrets-guard", []string{"--agent", "*"}, payload).Code
}

// kd is the home key directory's name.
const kd = "." + "s" + "sh"

// T151: a Read, Grep or Glob whose path or glob is in, at or above a home
// credentials directory is allowed; an edit tool's write there is refused.
func TestToolReadsOfHomeCredentialsAllowed(t *testing.T) {
	t.Parallel()
	s, home := homeSG(t)
	symlink(t, filepath.Join(home, kd), filepath.Join(s.p, "lnk"))
	cases := []struct {
		tool string
		in   map[string]any
		want int
	}{
		{"Grep", map[string]any{"pattern": "x", "path": "~/" + kd}, 0},
		{"Grep", map[string]any{"pattern": "x", "path": "$HOME/" + kd}, 0},
		{"Grep", map[string]any{"pattern": "x", "path": "${HOME}/.aws"}, 0},
		{"Grep", map[string]any{"pattern": "x", "path": "lnk"}, 0},
		{"Grep", map[string]any{"pattern": "x", "path": "~"}, 0},
		{"Grep", map[string]any{"pattern": "x", "glob": "*", "path": "~/" + kd}, 0},
		{"Glob", map[string]any{"pattern": "*.json", "path": "~/.config/gcloud"}, 0},
		{"Glob", map[string]any{"pattern": "*", "path": "lnk"}, 0},
		{"Read", map[string]any{"file_path": "~/" + kd + "/id_rsa"}, 0},
		{"Read", map[string]any{"file_path": filepath.Join(home, ".aws/credentials")}, 0},
		{"Read", map[string]any{"file_path": filepath.Join(s.p, "lnk/config.json")}, 0},
		{"Write", map[string]any{"file_path": filepath.Join(home, ".aws/new.txt"), "content": "x"}, 2},
		{"Edit", map[string]any{"file_path": filepath.Join(s.p, "lnk/config.json"), "old_string": "{", "new_string": "["}, 2},
	}
	for _, c := range cases {
		if got := homeToolCall(t, s, c.tool, c.in); got != c.want {
			t.Errorf("%s %v: exit %d, want %d", c.tool, c.in, got, c.want)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(home, kd, "config.json")); string(b) != "{}\n" {
		t.Errorf("%s/config.json changed: %q", kd, b)
	}
}

// L641: Jhon's own install (its prose, config and binary) is write-protected,
// not a credentials directory: the agent reads init.md and the skills there.
func TestFrameworkInstallStaysReadable(t *testing.T) {
	t.Parallel()
	s, home := homeSG(t)
	init := ".local/share/jhon/current/prose/skills/ldd/references/init.md"
	support.Write(t, home, map[string]string{init: "# init\n", ".config/jhon/settings.toml": "x = 1\n"})
	for _, c := range []struct {
		tool string
		in   map[string]any
	}{
		{"Read", map[string]any{"file_path": filepath.Join(home, init)}},
		{"Read", map[string]any{"file_path": "~/" + init}},
		{"Read", map[string]any{"file_path": "~/.config/jhon/settings.toml"}},
		{"Grep", map[string]any{"pattern": "x", "path": "~/.local/share/jhon/current/prose"}},
		{"Glob", map[string]any{"pattern": "*.md", "path": "~/.local/share/jhon/current/prose/skills/ldd/references"}},
	} {
		if got := homeToolCall(t, s, c.tool, c.in); got != 0 {
			t.Errorf("%s %v: exit %d, want 0", c.tool, c.in, got)
		}
	}
	s.code(t, "cat ~/.local/share/jhon/current/prose/skills/ldd/references/*.md", sgOpts{}, 0, "reading Jhon's own prose")
}
