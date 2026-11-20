package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// Judgment Day is removed (T172, L1439, L1440): jhon has no judgment
// command, and no runtime renders its agents. The 4R stays the one outside
// review.
func TestJudgmentDayRemoved(t *testing.T) {
	t.Parallel()

	t.Run("jhon judgment is an unknown command", func(t *testing.T) {
		t.Parallel()
		r := support.Run(t, support.Cmd{Args: []string{support.Bin(t), "judgment", "status", "x.md"}, Env: support.Env()})
		wantCode(t, r, 1)
		equal(t, r.Stdout, "")
		if !strings.HasPrefix(r.Stderr, "jhon: unknown command: judgment\n\n") {
			t.Errorf("stderr = %q", r.Stderr)
		}
	})

	t.Run("no runtime renders a jd-* agent", func(t *testing.T) {
		t.Parallel()
		home := renderedHome(t)
		noJD(t, filepath.Join(home, ".claude", "agents"))
		for _, a := range allManifestAgents(t) {
			if strings.HasPrefix(a, "jd-") {
				t.Errorf("agents.yaml declares %s", a)
			}
		}
	})

	// An owner whose profile still names a removed agent is warned, and the
	// install runs: the entry is ignored (L48).
	t.Run("a profile naming a jd-* agent is a warning, not an error", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, ".pi"), 0o777); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(home, ".config", "jhon", "profiles.json"), `{"active":"x","profiles":{"x":{"agents":{"jd-fix-agent":{"claude-code":"claude-opus-5-5@max"},"jd-judge-a":{"pi":"claude-bridge/claude-opus-5-5@high"}}}}}`)
		r := install(t, home, repoRoot)
		wantCode(t, r, 0)
		match(t, r.Stderr, `(?m)^profile: warning: x\.agents: jd-fix-agent is not an agent; its entry is ignored `)
		match(t, r.Stderr, `(?m)^profile: warning: x\.agents: jd-judge-a is not an agent; its entry is ignored `)
		noJD(t, filepath.Join(home, ".claude", "agents"))
		noJD(t, filepath.Join(home, ".pi", "jhon", "agents"))
	})
}

// noJD fails when dir holds a Judgment Day agent, or holds no agent at all.
func noJD(t *testing.T, dir string) {
	t.Helper()
	files := entries(t, dir)
	if !contains(files, "worker.md") {
		t.Errorf("%s renders no worker: %v", dir, files)
	}
	for _, f := range files {
		if strings.HasPrefix(f, "jd-") {
			t.Errorf("a Judgment Day agent is rendered: %s", filepath.Join(dir, f))
		}
	}
}
