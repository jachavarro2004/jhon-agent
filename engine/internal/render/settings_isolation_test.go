package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// T264 (L1735): a test failure reached main with a green check because
// TestFixedContextBytes read the caller's settings: the owner's jhon-studio
// on locally, the defaults in CI. TestMain pins JHON_SETTINGS and
// XDG_CONFIG_HOME to a file of the run's own; this test shows it, by running
// this test binary again with a hostile settings file where the caller's
// would be found, in XDG_CONFIG_HOME and in JHON_SETTINGS, and finding the
// render the same as with neither.

// settingsProbeEnv names the file a probe run writes its render digest
// to; TestMain runs the probe instead of the tests when it is set.
const settingsProbeEnv = "JHON_TEST_SETTINGS_PROBE"

// settingsProbe writes to out the digest of the render a test at engineAt
// gets, under the environment TestMain left; 0 when it could.
func settingsProbe(out string) int {
	home, err := os.MkdirTemp("", "jhon-render-probe-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(home)
	// engineAt's own environment.
	os.Setenv("HOME", home)
	os.Setenv("JHON_PROFILES", "")
	os.Setenv("JHON_DESKTOP_COLORS", "")
	a, perr := parseArgs([]string{"--validate"})
	if perr != "" {
		fmt.Fprintln(os.Stderr, perr)
		return 1
	}
	e, err := newEngine(resolveRoot(repo), a, &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(out, []byte(renderDigest(e)), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// renderDigest is a digest of everything settings can change in a render:
// the runtimes off, the optional integrations on, the tools off, each
// agent's capabilities and body, the lean and full main text and the
// always-on bundle, in both runtimes.
func renderDigest(e *engine) string {
	// At a representative home, as TestFixedContextBytes measures: each
	// test's home is a temporary folder of its own.
	e.home = "/home/owner"
	h := sha256.New()
	fmt.Fprintf(h, "off %v\noptional %v\ntools off %v\n", e.off, e.optionalOn, e.toolsOff)
	for _, rt := range []string{"claude-code", "pi"} {
		fmt.Fprintf(h, "%s lean\n%s\nfull\n%s\nbundle\n%s\n", rt, e.leanText(rt), e.fullText(rt), e.alwaysOnBundle(rt))
		for _, a := range e.m.Agents {
			fmt.Fprintf(h, "%s %v\n%s\n", a.Name, a.Caps, e.bodyFor(rt, a))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestARenderTestNeverReadsTheCallersSettings(t *testing.T) {
	pinned := renderDigest(engineAt(t, repo, "--validate"))

	// The hostile file turns every optional integration on (jhon-studio is
	// off by default), and is no no-op: read on purpose, it changes the render.
	caller := t.TempDir()
	hostile := filepath.Join(caller, "jhon", "settings.toml")
	if err := os.MkdirAll(filepath.Dir(hostile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostile, []byte(allOptionalOn()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JHON_SETTINGS", hostile)
	if got := renderDigest(engineAt(t, repo, "--validate")); got == pinned {
		t.Fatalf("the hostile settings change nothing even when read on purpose:\n%s", allOptionalOn())
	}

	// This binary again, as a caller with that file would run it.
	var base []string
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); k != "JHON_SETTINGS" && k != "XDG_CONFIG_HOME" && k != settingsProbeEnv {
			base = append(base, kv)
		}
	}
	for name, env := range map[string][]string{
		"neither":                        nil,
		"the caller's XDG_CONFIG_HOME":   {"XDG_CONFIG_HOME=" + caller},
		"the caller's JHON_SETTINGS":     {"JHON_SETTINGS=" + hostile},
		"both, as an owner may set them": {"XDG_CONFIG_HOME=" + caller, "JHON_SETTINGS=" + hostile},
	} {
		out := filepath.Join(t.TempDir(), "digest")
		cmd := exec.Command(os.Args[0], "-test.run=^$")
		cmd.Env = append(append(slices.Clone(base), env...), settingsProbeEnv+"="+out)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: the probe run failed: %v\n%s", name, err, b)
		}
		got, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("%s: the probe wrote no digest: %v", name, err)
		}
		if string(got) != pinned {
			t.Errorf("%s: a render test read the caller's settings (digest %s, want %s with no settings file)", name, got, pinned)
		}
	}
}
