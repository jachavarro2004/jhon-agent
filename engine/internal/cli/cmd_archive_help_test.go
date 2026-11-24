package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T223 S3: `jhon archive --help` and `-h` print the archive command's own
// help, exit 0, and write nothing: the change stays where it is, no archive
// directory appears, and nothing goes to stderr.
func TestArchiveHelpIsItsOwn(t *testing.T) {
	t.Setenv("JHON_TELEMETRY", "off")
	for _, args := range [][]string{{"archive", "--help"}, {"archive", "-h"}, {"archive", "c1", "2026-11-05", "--help"}} {
		dir := t.TempDir()
		change := filepath.Join(dir, "openspec", "changes", "c1")
		if err := os.MkdirAll(change, 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(change, "tasks.md"), []byte("- [x] 1.1 done\n"), 0o666); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)
		code, out, errOut := captureAll(t, args)
		if code != 0 {
			t.Errorf("jhon %v = %d", args, code)
		}
		if errOut != "" {
			t.Errorf("jhon %v wrote to stderr: %q", args, errOut)
		}
		if !strings.HasPrefix(out, "jhon archive — ") || !strings.Contains(out, "\nusage\n  jhon archive <change-name> <YYYY-MM-DD>\n") {
			t.Errorf("jhon %v is not the archive's own help:\n%s", args, out)
		}
		if strings.Contains(out, "agent framework") || strings.Contains(out, "jhon sync ") {
			t.Errorf("jhon %v printed the general help:\n%s", args, out)
		}
		flat := strings.Join(strings.Fields(out), " ")
		if !strings.Contains(flat, "refuses a change with no RED before GREEN on record (tests.red_before_green in jhon status <change> --json)") {
			t.Errorf("jhon %v does not say it refuses without RED before GREEN:\n%s", args, out)
		}
		if b, err := os.ReadFile(filepath.Join(change, "tasks.md")); err != nil || string(b) != "- [x] 1.1 done\n" {
			t.Errorf("jhon %v moved or changed the change: %q %v", args, b, err)
		}
		if _, err := os.Lstat(filepath.Join(dir, "openspec", "changes", "archive")); err == nil {
			t.Errorf("jhon %v wrote an archive directory", args)
		}
	}
}
