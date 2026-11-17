package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The owner's shell rides in part 1, the part every session gets: it adds
// no part of its own, so a one-part definition still has no part 2.
func TestAgentHooksOwnerShellRidesInPartOne(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/usr/bin/fish")
	dir := filepath.Join(home, ".claude", "agents")
	os.MkdirAll(dir, 0o755)
	if err := os.WriteFile(filepath.Join(dir, "jhon-orchestrator.md"), []byte(agentFile), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	start := `{"hook_event_name":"SessionStart","agent_type":"jhon-orchestrator","cwd":"` + cwd + `"}`
	_, out, _ := runAgentHooks(t, start, "--part", "1")
	if !strings.Contains(out, "Part 1 of 1") || !strings.Contains(out, "owner's shell is fish") || !strings.Contains(out, "# Orchestrator") {
		t.Errorf("part 1 carries the definition and the owner's shell: %q", out)
	}
	if _, out, _ = runAgentHooks(t, start, "--part", "2"); out != "" {
		t.Errorf("the owner's shell added a part: %q", out)
	}
	t.Setenv("SHELL", "/bin/bash")
	if _, out, _ = runAgentHooks(t, start, "--part", "1"); strings.Contains(out, "owner's shell") {
		t.Errorf("a POSIX shell is not noted: %q", out)
	}
}
