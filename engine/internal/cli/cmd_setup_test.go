package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// TestSetupNeedsATerminal: `jhon setup` with stdin and stdout not a terminal
// exits 2, says why, and writes nothing.
func TestSetupNeedsATerminal(t *testing.T) {
	home := t.TempDir()
	env := support.Env("HOME="+home, "XDG_CONFIG_HOME=", "JHON_SETTINGS=", "JHON_PROFILES=")
	r := support.Jhon(t, home, env, "setup")
	if r.Code != 2 || r.Stderr != "jhon setup needs a terminal\n" || r.Stdout != "" {
		t.Errorf("jhon setup = %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Errorf("jhon setup wrote %v into the home", entries)
	}
}

// TestSetupRefusesAnArgument: `jhon setup x` exits 2 naming the argument,
// before it looks for a terminal; stdin from a pipe is refused with the
// exact line (the verifier's probes, L17).
func TestSetupRefusesAnArgument(t *testing.T) {
	home := t.TempDir()
	env := support.Env("HOME="+home, "XDG_CONFIG_HOME=", "JHON_SETTINGS=", "JHON_PROFILES=")
	r := support.Jhon(t, home, env, "setup", "x")
	if r.Code != 2 || !strings.HasPrefix(r.Stderr, "jhon setup: unknown argument: x\n") || r.Stdout != "" {
		t.Errorf("jhon setup x = %d, stdout %q, stderr %q", r.Code, r.Stdout, r.Stderr)
	}
	r = support.Run(t, support.Cmd{Args: []string{support.Bin(t), "setup"}, Dir: home, Env: env, Stdin: "q"})
	if r.Code != 2 || r.Stderr != "jhon setup needs a terminal\n" {
		t.Errorf("jhon setup <pipe = %d, stderr %q", r.Code, r.Stderr)
	}
}
