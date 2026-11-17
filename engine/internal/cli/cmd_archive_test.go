package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// S35 (L145): an archived change ends its session. `jhon archive` tells the
// agent and the owner, once, to start a fresh session with a handoff; an
// archive that moved nothing says nothing of it.
func TestArchiveSuggestsAFreshSessionOnce(t *testing.T) {
	t.Setenv("JHON_TELEMETRY", "off")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "openspec", "changes", "c1"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "openspec", "changes", "c1", "tasks.md"), []byte("- [x] 1.1 done\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	code, out := capture(t, []string{"archive", "c1", "2026-11-05"})
	if code != 0 || !strings.Contains(out, "readback: empty") {
		t.Fatalf("archive: %d %q", code, out)
	}
	if n := strings.Count(out, "One session per change"); n != 1 {
		t.Errorf("the notice is printed %d times, want once: %q", n, out)
	}
	for _, want := range []string{"c1 is archived", "`jhon context --phase none --handoff`", "mem_session_summary", "/new in pi", "/clear or a restart in Claude Code"} {
		if !strings.Contains(out, want) {
			t.Errorf("the notice lacks %q: %q", want, out)
		}
	}
	code, out = capture(t, []string{"archive", "c1", "2026-11-05"})
	if code == 0 || strings.Contains(out, "One session per change") {
		t.Errorf("a second archive of c1: %d %q; want a failure and no notice", code, out)
	}
}
