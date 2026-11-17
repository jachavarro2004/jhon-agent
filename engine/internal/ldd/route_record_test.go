package ldd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
)

// The record a PR waits for (S23): at risk passive or medium a tasks.md with
// an `## Intent` line and a `Delivers:` line is complete, and a spec delta
// is due only when a scenario changes; `No spec change:` still stands in
// for one. At risk high the record is today's: tasks.md and a spec delta,
// or `No spec change:`.
func TestDocumentedByTier(t *testing.T) {
	oneLine := "## Intent\nDelivers: the report totals by month\n"
	for _, c := range []struct {
		what  string
		tasks string
		specs bool
		tier  Level
		want  bool
	}{
		{"one line at passive", oneLine, false, Passive, true},
		{"one line at medium", oneLine, false, Medium, true},
		{"one line at high", oneLine, false, High, false},
		{"Intent without Delivers at medium", "## Intent\nWhy: totals\n", false, Medium, false},
		{"Delivers without Intent at medium", "## Slice 1: a\nDelivers: totals\n- [x] 1.1 a\n", false, Medium, false},
		{"tasks and a spec delta at high", "## Slice 1: a\n- [x] 1.1 a\n", true, High, true},
		{"No spec change at high", "No spec change: a rename.\n", false, High, true},
		{"No spec change at medium", "No spec change: a rename.\n", false, Medium, true},
		{"tasks alone at high", "## Slice 1: a\n- [x] 1.1 a\n", false, High, false},
		{"no tasks.md", "", true, Medium, false},
	} {
		dir := t.TempDir()
		if c.tasks != "" {
			write(t, filepath.Join(dir, "tasks.md"), c.tasks)
		}
		if c.specs {
			if err := os.MkdirAll(filepath.Join(dir, "specs", "a"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if got := Documented(layout.OpenSpec, dir, c.tier); got != c.want {
			t.Errorf("%s: Documented = %v, want %v", c.what, got, c.want)
		}
	}
}
