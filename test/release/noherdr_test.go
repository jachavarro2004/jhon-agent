package release

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T24b (L291, L304): herdr is no longer part of Jhon. `release pack` has no
// --herdr, and a sync leaves whatever herdr install a home holds as it is,
// even with the old settings and variables saying herdr is on.
func TestNoHerdr(t *testing.T) {
	t.Run("release pack --herdr is an unknown argument", func(t *testing.T) {
		env := scratchHome(t)
		r := run(t, env, support.Bin(t), []string{"release", "pack", "--herdr", filepath.Join(env.home, "herdr")})
		wantCode(t, r, 2)
		match(t, r.Stderr, `(?m)^jhon release: unexpected argument: --herdr$`)
		noMatch(t, r.Stderr, `herdr <bin>`)
	})

	t.Run("sync leaves a home's herdr install byte-identical and says nothing of herdr", func(t *testing.T) {
		parallel(t)
		env := scratchHome(t)
		owned := map[string]string{
			".local/bin/herdr-jhon": "#!/bin/sh\necho herdr-jhon\n",
			".config/herdr/plugins/local/jhon-sidebar/herdr-plugin.toml": "id = \"jhon.sidebar\"\n",
			".config/herdr/plugins/local/jhon-sidebar/run.sh":            "echo mine\n",
			".pi/jhon/extensions/herdr-agent-state.ts":                   "export default () => {};\n",
			".local/share/jhon/herdr.json":                               "{\"commit\":\"mine\"}\n",
			".config/jhon/settings.toml":                                 "schema = 1\n\n[integrations]\nherdr = true\n",
		}
		support.Write(t, env.home, owned)
		src := scratchSource(t)
		r := run(t, env, cliOf(src), []string{"sync"}, "JHON_HERDR=1", "HERDR_JHON_BIN="+filepath.Join(env.home, ".local", "bin", "herdr-jhon"),
			"XDG_CONFIG_HOME="+filepath.Join(env.home, ".config"))
		wantCode(t, r, 0)
		for rel, want := range owned {
			if rel == ".config/jhon/settings.toml" {
				continue
			}
			equal(t, support.Read(t, filepath.Join(env.home, rel)), want, rel)
		}
		equal(t, names(t, filepath.Join(env.home, ".config", "herdr", "plugins", "local", "jhon-sidebar")), []string{"herdr-plugin.toml", "run.sh"})
		equal(t, exists(filepath.Join(env.home, ".config", "herdr", "plugins.json")), false)
		if strings.Contains(strings.ReplaceAll(r.Out(), "integrations.herdr = true is no longer read", ""), "herdr") {
			t.Errorf("sync speaks of herdr:\n%s", r.Out())
		}
	})
}
