package guard

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// A multiplexer's pane-run command runs its argument in another pane; the
// guard judges that argument (T50b, F4). The parsing is generic: only the
// paneMultiplexers table names a program.
func TestPaneRunParsingIsGeneric(t *testing.T) {
	if !paneMultiplexers["herdr"] {
		t.Errorf("paneMultiplexers lacks a known multiplexer: %v", paneMultiplexers)
	}
	if script, _ := paneRuns([]string{"pane", "run", "w1:p2", "git", "push"}); script != "git push" {
		t.Errorf("pane run script: %q", script)
	}
	if _, words := paneRuns([]string{"agent", "start", "c", "--kind", "claude", "--", "--agent", "x"}); !slices.Equal(words, []string{"claude", "--agent", "x"}) {
		t.Errorf("agent start words: %q", words)
	}
	for _, f := range []string{"safety.go", "safety_programs.go"} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for n, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "herdr") && !strings.Contains(line, "paneMultiplexers =") {
				t.Errorf("%s:%d names a multiplexer outside paneMultiplexers: %s", f, n+1, strings.TrimSpace(line))
			}
		}
	}
}
