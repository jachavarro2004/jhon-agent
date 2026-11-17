package release

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// entries is every entry under dir: a file's bytes, a link's target, a
// directory's name; nil when dir is absent.
func entries(t *testing.T, dir string) map[string]string {
	t.Helper()
	if !exists(dir) {
		return nil
	}
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(p)
			out[p] = "-> " + target
		case d.IsDir():
			out[p] = "dir"
		default:
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			out[p] = string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The runtimes are adapters (S37, T56): sync, check and doctor work only on
// the runtimes settings.runtimes turns on.
func TestRuntimesSettings(t *testing.T) {
	t.Run("jhon sync and jhon check with runtimes.claude_code = false touch no ~/.claude path and say Claude Code is skipped", func(t *testing.T) {
		parallel(t)
		env := scratchHome(t)
		support.Write(t, env.home, map[string]string{
			".config/jhon/settings.toml": "schema = 1\n\n[runtimes]\nclaude_code = false\n",
			".pi/agent/auth.json":        "{}\n",
		})
		src := scratchSource(t)
		before := entries(t, filepath.Join(env.home, ".claude"))
		r := syncOf(t, env, src)
		wantCode(t, r, 0)
		match(t, r.Out(), `(?m)^skip: claude-code \(runtimes\.claude_code = false\); ~/\.claude is left as is$`)
		equal(t, entries(t, filepath.Join(env.home, ".claude")), before, "sync touched ~/.claude")
		if !exists(filepath.Join(env.home, ".pi", "jhon", "settings.json")) {
			t.Fatal("pi was not installed\n" + r.Out())
		}
		c := run(t, env, cliOf(src), []string{"check"})
		wantCode(t, c, 0)
		noMatch(t, c.Out(), `DRIFT`)
		equal(t, entries(t, filepath.Join(env.home, ".claude")), before, "check touched ~/.claude")
	})

	t.Run("jhon doctor with runtimes.claude_code = false does not ask for Claude Code's agents and says it is off", func(t *testing.T) {
		parallel(t)
		env := scratchHome(t)
		support.Write(t, env.home, map[string]string{".config/jhon/settings.toml": "[runtimes]\nclaude_code = false\n"})
		src := scratchSource(t)
		d := run(t, env, cliOf(src), []string{"doctor"})
		noMatch(t, d.Out(), `no agents rendered for Claude Code`)
		match(t, d.Out(), `claude-code is off \(runtimes\.claude_code = false\); ~/\.claude is not checked`)
	})

	t.Run("jhon doctor with both runtimes on still asks for Claude Code's agents", func(t *testing.T) {
		parallel(t)
		env := scratchHome(t)
		src := scratchSource(t)
		match(t, run(t, env, cliOf(src), []string{"doctor"}).Out(), `no agents rendered for Claude Code`)
	})
}
