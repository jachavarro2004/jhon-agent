// `jhon executions run` ends with its own status line (T108, L671): piped
// through `tail`, the pipeline's exit is tail's, so the run's last stdout line
// names the recorded result and its exit code; and a job runner that sets
// JHON_EXECUTIONS_REPORT learns which record the run wrote, to read its result.
//
// Every command in these fixtures is harmless shell in a temporary directory.
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

const record = "openspec/changes/c1/execution-record.json"

// TestMain points the binary at this checkout (test/ldd/helpers_test.go).
func TestMain(m *testing.M) {
	os.Setenv("JHON_ROOT", support.Root())
	os.Exit(m.Run())
}

func workspace(t *testing.T, command string) string {
	t.Helper()
	root := t.TempDir()
	support.Write(t, root, map[string]string{
		"a/go.mod":                     "module example.com/a\n",
		"openspec/changes/c1/tasks.md": "- [x] done\n",
		"openspec/config.yaml":         "test_command_override:\n  a: " + command + "\nlint_command_override:\n  a: \"true\"\nformat_command_override:\n  a: \"true\"\n",
	})
	return root
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

func TestExecutionsRunEndsWithItsStatusLine(t *testing.T) {
	for _, c := range []struct {
		name, command string
		code          int
		want          string
	}{
		{"pass", "echo ok", 0, "jhon executions: pass, exit 0 (3 of 3 passed; record " + record + ")"},
		{"fail", "echo bad; exit 3", 1, "jhon executions: fail, exit 1 (2 of 3 passed; record " + record + ")"},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := workspace(t, c.command)
			r := support.Jhon(t, root, support.Env("JHON_EXECUTIONS_REPORT="), "executions", "run", "c1", "--force")
			if r.Code != c.code {
				t.Fatalf("exit %d, want %d\n%s", r.Code, c.code, r.Out())
			}
			if got := lastLine(r.Stdout); got != c.want {
				t.Fatalf("last stdout line %q, want %q\n%s", got, c.want, r.Stdout)
			}
			// The summary before it is unchanged.
			if !strings.Contains(r.Stdout, "\nrecord     "+record+"\n") {
				t.Fatalf("summary record line missing:\n%s", r.Stdout)
			}
		})
	}
}

func TestExecutionsRunJSONKeepsStdoutOneDocument(t *testing.T) {
	root := workspace(t, "exit 3")
	r := support.Jhon(t, root, support.Env("JHON_EXECUTIONS_REPORT="), "executions", "run", "c1", "--json")
	if r.Code != 1 || !strings.HasPrefix(r.Stdout, "{") || !strings.HasSuffix(strings.TrimSpace(r.Stdout), "}") {
		t.Fatalf("exit %d, stdout not one JSON document:\n%s", r.Code, r.Stdout)
	}
	if got, want := lastLine(r.Stderr), "jhon executions: fail, exit 1 (2 of 3 passed; record "+record+")"; got != want {
		t.Fatalf("last stderr line %q, want %q", got, want)
	}
}

func TestExecutionsRunNamesItsRecordToTheJobRunner(t *testing.T) {
	root := workspace(t, "exit 3")
	report := filepath.Join(t.TempDir(), "report")
	r := support.Jhon(t, root, support.Env("JHON_EXECUTIONS_REPORT="+report), "executions", "run", "c1")
	if r.Code != 1 {
		t.Fatalf("exit %d\n%s", r.Code, r.Out())
	}
	b, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.EvalSymlinks(filepath.Join(root, record))
	got := strings.TrimSpace(string(b))
	if resolved, err := filepath.EvalSymlinks(got); err != nil || resolved != abs {
		t.Fatalf("report names %q, want the record %q", got, abs)
	}
	// A dry run writes no record, so it names none.
	dry := filepath.Join(t.TempDir(), "dry")
	support.Jhon(t, root, support.Env("JHON_EXECUTIONS_REPORT="+dry), "executions", "run", "c1", "--dry-run")
	if _, err := os.Stat(dry); !os.IsNotExist(err) {
		t.Fatalf("a dry run wrote the report file: %v", err)
	}
}
