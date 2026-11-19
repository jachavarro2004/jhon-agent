package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sandbox"
)

// T128f (L1352 F1, F2, D3): off Linux the default reads as off and doctor
// warns that the model's commands run unsandboxed; an explicit on keeps
// refusing every command, which doctor counts as a problem.
func TestDoctorOffLinuxWarnsOrFailsByTheMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude"))
	was := sandboxHost
	t.Cleanup(func() { sandboxHost = was })
	sandboxHost = func() sandbox.Host { return sandbox.Host{OS: "darwin", ReadFile: os.ReadFile, Stat: os.Stat} }
	const cannot = "it cannot run here: the sandbox does not run on this system (darwin); fix: none on this system: it needs Linux and bubblewrap"
	for _, c := range []struct {
		file, mode, source, row string
		failed                  int
	}{
		{"", "off", "default", "  warn  sandbox off; " + cannot + "; the model's commands run unsandboxed\n", 0},
		{"[sandbox]\nmode = \"off\"\n", "off", "settings", "  warn  sandbox off; " + cannot + "; the model's commands run unsandboxed\n", 0},
		{"[sandbox]\nmode = \"on\"\n", "on", "settings", "  FAIL  sandbox on; " + cannot + "; until then every command of the model is refused\n", 1},
	} {
		file := filepath.Join(home, "settings.toml")
		must(t, os.WriteFile(file, []byte(c.file), 0o644))
		t.Setenv("JHON_SETTINGS", file)
		if s := sandboxNow(""); s.Mode != c.mode || s.ModeSource != c.source || s.CanRun {
			t.Errorf("%q: status %+v; want %s (%s), unable to run", c.file, s, c.mode, c.source)
		}
		var out bytes.Buffer
		o := &release.Out{W: &out}
		doctor{o: o}.sandbox()
		if want := "\nsandbox\n" + c.row; out.String() != want || o.Failed != c.failed {
			t.Errorf("%q: doctor printed %q (%d failed); want %q", c.file, out.String(), o.Failed, want)
		}
	}
}
