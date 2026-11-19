package cli

// T128f (L1352 F1): `jhon sandbox status --json` gives the effective
// sandbox.mode and where it comes from: on by default on Linux where the
// sandbox can run, off by default elsewhere (D3) and on a Linux host where
// it cannot, with the reason (slice 8, L1375; sandbox_default_test.go), and
// the settings file's value on every system.

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestStatusGivesTheEffectiveModeAndItsSource(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	file := filepath.Join(m.home, "settings.toml")
	env := append(append([]string{}, m.env...), "JHON_SETTINGS="+file)
	byDefault := "off"
	if runtime.GOOS == "linux" {
		byDefault = "on"
	}
	for _, c := range []struct{ file, mode, source string }{
		{"", byDefault, "default"},
		{"schema = 1\n", byDefault, "default"},
		{"[sandbox]\nmode = \"on\"\n", "on", "settings"},
		{"[sandbox]\nmode = \"off\"\n", "off", "settings"},
	} {
		support.Write(t, m.home, map[string]string{"settings.toml": c.file})
		r := support.Jhon(t, m.proj, env, "sandbox", "status", "--json")
		var s struct {
			Mode   string `json:"mode"`
			Source string `json:"mode_source"`
			Why    string `json:"mode_reason"`
		}
		want := c.mode
		if err := json.Unmarshal([]byte(r.Stdout), &s); err == nil && c.source == "default" && s.Why != "" {
			want = "off" // this Linux host cannot run the sandbox
		}
		if r.Code != 0 || r.Stderr != "" || json.Unmarshal([]byte(r.Stdout), &s) != nil || s.Mode != want || s.Source != c.source || (s.Why != "" && (c.source != "default" || runtime.GOOS != "linux")) {
			t.Errorf("%q: exit %d, %q, stderr %q; want mode %s from %s", c.file, r.Code, r.Stdout, r.Stderr, want, c.source)
		}
	}
}
