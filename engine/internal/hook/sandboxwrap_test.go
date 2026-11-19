package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/sandbox"
)

// T128f (L1352 F1, D3): off Linux the wrap reads the default as off and
// takes no call; an explicit on refuses with the probe's reason and fix.
func TestSandboxWrapOffLinux(t *testing.T) {
	was := sandboxHost
	t.Cleanup(func() { sandboxHost = was })
	sandboxHost = func() sandbox.Host { return sandbox.Host{OS: "darwin", ReadFile: os.ReadFile, Stat: os.Stat} }
	dir := t.TempDir()
	t.Setenv(sandbox.ProjectEnv, dir)
	payload := []byte(`{"tool_name":"Bash","tool_input":{"command":"ls"},"cwd":"` + dir + `"}`)
	file := filepath.Join(dir, "settings.toml")
	for _, c := range []struct{ file, refusal string }{
		{"", ""},
		{"[sandbox]\nmode = \"off\"\n", ""},
		{"[sandbox]\nmode = \"on\"\n", "sandbox: sandbox.mode is on and the sandbox cannot run here: the sandbox does not run on this system (darwin); the call is refused; fix: none on this system: it needs Linux and bubblewrap"},
	} {
		if err := os.WriteFile(file, []byte(c.file), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("JHON_SETTINGS", file)
		updated, refusal := sandboxWrap("worker", payload)
		if updated != nil || refusal != c.refusal {
			t.Errorf("%q: updated %v, refusal %q; want none and %q", c.file, updated, refusal, c.refusal)
		}
	}
	// On Linux the default is on: the same call with no settings is the wrap's.
	sandboxHost = func() sandbox.Host { return sandbox.Host{OS: "linux", ReadFile: os.ReadFile, Stat: os.Stat} }
	t.Setenv("PATH", dir)
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if updated, refusal := sandboxWrap("worker", payload); updated != nil || !strings.Contains(refusal, "bubblewrap (bwrap) is not on PATH") {
		t.Errorf("linux, no settings, no bwrap: updated %v, refusal %q; want the probe's refusal", updated, refusal)
	}
}
