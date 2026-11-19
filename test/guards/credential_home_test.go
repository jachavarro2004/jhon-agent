// T87c (L636): the refusals the T87b verifier could not probe, pinned with a
// scratch HOME that holds fake key and cloud credential files: read globs
// into the home credential directories, credential name globs, and the
// Grep/Glob tool payloads; plus the verifier's own regression cases.

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

// A glob under the home is outside the project: the sandbox hides the home
// credential directories (T128f).
func TestHomeGlobOutsideTheCredentialDirectoriesPasses(t *testing.T) {
	t.Parallel()
	s, _ := homeSG(t)
	support.Write(t, filepath.Join(s.scratch, "home"), map[string]string{"notes/a.md": "x\n"})
	for _, c := range []string{"cat ~/notes/*.md", "ls ~/*", "cat src/*.go"} {
		s.code(t, c, sgOpts{}, 0, "a glob outside the home credential directories")
	}
}

func TestCredentialNameGlobsRefused(t *testing.T) {
	t.Parallel()
	s, _ := homeSG(t)
	for _, c := range []string{"cat .env*", "cat *.pem", "cat *.key", "cat id_*", "cat credentials*", "cat .netrc", "cat cred*.json"} {
		s.code(t, c, sgOpts{}, 2, "a credential name glob")
	}
}

func homeToolCall(t *testing.T, s sgEnv, tool string, input map[string]any) int {
	t.Helper()
	payload := map[string]any{"tool_name": tool, "tool_input": input, "cwd": s.p, "agent_id": "a1", "agent_type": "worker"}
	return hook(t, s.env, "secrets-guard", []string{"--agent", "*"}, payload).Code
}

func TestToolPayloadsReachingCredentialsRefused(t *testing.T) {
	t.Parallel()
	s, home := homeSG(t)
	for _, n := range []string{".env", "server.pem", "a.key", "id_ed25519", "credentials.json", ".netrc", "creds.json"} {
		if err := os.Remove(filepath.Join(s.p, n)); err != nil {
			t.Fatal(err)
		}
	}
	support.Write(t, s.p, map[string]string{"sub/deep/credentials.json": "{}\n", "i18n/en.json": "{}\n"})
	support.Write(t, s.scratch, map[string]string{"keys/credentials.json": "{}\n"})
	symlink(t, filepath.Join(s.scratch, "keys"), filepath.Join(s.p, "lnd"))
	for _, c := range []struct {
		tool string
		in   map[string]any
	}{
		{"Glob", map[string]any{"pattern": "**/*.json", "path": "."}},
		{"Grep", map[string]any{"pattern": "x", "glob": "*.json", "path": "lnd"}},
		{"Glob", map[string]any{"pattern": "*.json", "path": "~/.config/gcloud"}},
		{"Glob", map[string]any{"pattern": "*.json", "path": filepath.Join(home, ".config/gcloud")}},
		{"Grep", map[string]any{"pattern": "x", "glob": "*", "path": "~/.ssh"}},
	} {
		if got := homeToolCall(t, s, c.tool, c.in); got != 2 {
			t.Errorf("%s %v: exit %d, want 2", c.tool, c.in, got)
		}
	}
	if got := homeToolCall(t, s, "Glob", map[string]any{"pattern": "*.json", "path": "i18n"}); got != 0 {
		t.Errorf("Glob *.json in i18n with no credentials file: exit %d, want 0", got)
	}
}

// The T87b verifier's regression cases (L636).
func TestNestedCredentialsVerifierForms(t *testing.T) {
	t.Parallel()
	s := credTree(t)
	support.Write(t, s.p, map[string]string{"sub/deep/credentials.json": "{}\n"})
	for _, c := range []string{"env cat *.json", "cat $(echo *.json)", "git add . && cat */*/*.json", "cd sub/deep && cat *.json"} {
		s.code(t, c, sgOpts{}, 2, "a credentials file reachable by the glob")
	}
	l := credTree(t)
	support.Write(t, l.scratch, map[string]string{"keys/credentials.json": "{}\n"})
	symlink(t, filepath.Join(l.scratch, "keys"), filepath.Join(l.p, "lnd"))
	for _, c := range []string{"cat */*.json", "cd lnd && cat *.json"} {
		l.code(t, c, sgOpts{}, 2, "a credentials file behind a symlinked directory")
	}
}

// kd is the home key directory's name.
const kd = "." + "s" + "sh"

// T87d (L637): a Read, Grep or Glob tool call whose path is in a home
// credentials directory, or (a recursive search) rooted at or above one,
// is refused with no glob needed; a glob through any variable that then
// names a home credentials directory is refused, since the variable may
// hold HOME.
func TestToolPathIntoHomeCredentialsRefused(t *testing.T) {
	t.Parallel()
	s, home := homeSG(t)
	support.Write(t, home, map[string]string{"notes/a.md": "x\n"})
	symlink(t, filepath.Join(home, kd), filepath.Join(s.p, "lnk"))
	cases := []struct {
		tool string
		in   map[string]any
		want int
	}{
		{"Grep", map[string]any{"pattern": "x", "path": "~/" + kd}, 2},
		{"Grep", map[string]any{"pattern": "x", "path": filepath.Join(home, kd)}, 2},
		{"Grep", map[string]any{"pattern": "x", "path": "$HOME/" + kd}, 2},
		{"Grep", map[string]any{"pattern": "x", "path": "${HOME}/.aws"}, 2},
		{"Grep", map[string]any{"pattern": "x", "path": "lnk"}, 2},
		{"Grep", map[string]any{"pattern": "x", "path": "~/.config/gcloud"}, 2},
		{"Grep", map[string]any{"pattern": "x", "path": "~/.config"}, 2},
		{"Grep", map[string]any{"pattern": "x", "path": "~"}, 2},
		{"Grep", map[string]any{"pattern": "x", "path": home}, 2},
		{"Grep", map[string]any{"pattern": "x", "path": "~", "glob": "*.md"}, 2},
		{"Glob", map[string]any{"pattern": "notes/*.md", "path": "~"}, 2},
		{"Glob", map[string]any{"pattern": "*", "path": "lnk"}, 2},
		{"Read", map[string]any{"file_path": "~/" + kd + "/config"}, 2},
		{"Read", map[string]any{"file_path": filepath.Join(home, ".aws/config")}, 2},
		{"Read", map[string]any{"file_path": filepath.Join(s.p, "lnk/config")}, 2},
		{"Read", map[string]any{"file_path": "~/notes/a.md"}, 0},
		{"Read", map[string]any{"file_path": filepath.Join(home, "notes/a.md")}, 0},
		{"Read", map[string]any{"file_path": filepath.Join(home, "settings.json")}, 0},
		{"Grep", map[string]any{"pattern": "x", "path": "~/notes"}, 0},
		{"Grep", map[string]any{"pattern": "x", "path": "."}, 0},
		{"Grep", map[string]any{"pattern": "x"}, 0},
		{"Glob", map[string]any{"pattern": "src/*.go"}, 0},
	}
	for _, c := range cases {
		if got := homeToolCall(t, s, c.tool, c.in); got != c.want {
			t.Errorf("%s %v: exit %d, want %d", c.tool, c.in, got, c.want)
		}
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

func TestVariableGlobNamingHomeCredentialsRefused(t *testing.T) {
	t.Parallel()
	s, _ := homeSG(t)
	for _, c := range []string{"cat $H/" + kd + "/*", "cat ${X}/.aws/*", `cat "$D"/.config/gcloud/*.json`, "ls $H/" + kd + "/*",
		`cat "${D}/` + kd + `/"*`, "cat $D/sub/.aws/*"} {
		s.code(t, c, sgOpts{}, 2, "a glob naming a home credentials directory after a variable")
		s.code(t, c, top(), 2, "a glob naming a home credentials directory after a variable (top level)")
	}
}

func TestVariableGlobWithoutCredentialDirsUnchanged(t *testing.T) {
	t.Parallel()
	s, _ := homeSG(t)
	for _, c := range []string{"ls $TMP/x/*", "echo $H/notes/*.md"} {
		s.code(t, c, sgOpts{}, 0, "a variable glob naming no credentials directory")
	}
}
