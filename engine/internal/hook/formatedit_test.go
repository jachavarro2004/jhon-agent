package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func formatEdit(t *testing.T, payload map[string]any) (int, string) {
	t.Helper()
	in, _ := json.Marshal(payload)
	var out, errs bytes.Buffer
	code := FormatEdit([]string{"--agent", "worker"}, IO{bytes.NewReader(in), &out, &errs})
	return code, out.String()
}

func TestFormatEditRewritesTheEditedFileWithItsProjectsFormatter(t *testing.T) {
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Skip("gofmt is not installed")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n"), 0o644)
	file := filepath.Join(dir, "a.go")
	os.WriteFile(file, []byte("package x\nfunc  A( ) int { return 1 }\n"), 0o644)

	code, out := formatEdit(t, map[string]any{"cwd": dir, "tool_name": "Write", "tool_input": map[string]any{"file_path": "a.go"}})
	b, _ := os.ReadFile(file)
	if code != 0 || string(b) != "package x\n\nfunc A() int { return 1 }\n" {
		t.Fatalf("exit %d, file:\n%s", code, b)
	}
	if !strings.Contains(out, `"additionalContext":"jhon: gofmt reformatted a.go; read it again before the next edit"`) || !strings.Contains(out, `"hookEventName":"PostToolUse"`) {
		t.Errorf("the agent is told its copy is stale: %s", out)
	}
	// Already formatted: nothing to say.
	if code, out := formatEdit(t, map[string]any{"cwd": dir, "tool_input": map[string]any{"file_path": file}}); code != 0 || out != "" {
		t.Errorf("unchanged: exit %d, %q", code, out)
	}
}

func TestFormatEditNeverFailsTheEdit(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "bad.go"), []byte("package x\nfunc {\n"), 0o644)
	for name, payload := range map[string]map[string]any{
		"a syntax error the formatter refuses": {"cwd": dir, "tool_input": map[string]any{"file_path": "bad.go"}},
		"a file no project formats":            {"cwd": dir, "tool_input": map[string]any{"file_path": "notes.txt"}},
		"no file":                              {"cwd": dir, "tool_input": map[string]any{}},
	} {
		if code, out := formatEdit(t, payload); code != 0 || out != "" {
			t.Errorf("%s: exit %d, %q", name, code, out)
		}
	}
	var out bytes.Buffer
	if code := FormatEdit(nil, IO{strings.NewReader("not json"), &out, &out}); code != 0 {
		t.Errorf("a payload that is not JSON: exit %d", code)
	}
}
