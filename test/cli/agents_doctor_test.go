// `jhon doctor` says when a Claude Code session has no guard to run: the
// record of the rendered agents, or the main agent's definition, is gone.
// The doctor's row names the file and the fix; no call is refused for it.
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestDoctorNamesAMissingAgentsRecordOrMainAgent(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	agents := filepath.Join(m.home, ".claude", "agents")
	support.Write(t, agents, map[string]string{"jhon-orchestrator.md": "---\nname: jhon-orchestrator\n---\nbody\n", "worker.md": "---\nname: worker\n---\nbody\n", ".jhon-owned": "jhon-orchestrator\nworker\n"})
	env := append(append([]string{}, m.env...), "JHON_SETTINGS="+filepath.Join(m.home, "settings.toml"))
	support.Write(t, m.home, map[string]string{"settings.toml": "schema = 1\n\n[runtimes]\nclaude_code = true\n"})
	record := "  FAIL  the record of the rendered agents (~/.claude/agents/.jhon-owned) is missing — run 'jhon sync'\n"
	main := "  FAIL  the main agent's definition (~/.claude/agents/jhon-orchestrator.md) is missing: no guard judges the main agent's tool calls in a Claude Code session — run 'jhon sync'\n"
	rows := func() string {
		t.Helper()
		r := support.Jhon(t, m.proj, env, "doctor")
		if !strings.Contains(r.Stdout, "discovery") {
			t.Fatalf("doctor: exit %d\n%s", r.Code, r.Out())
		}
		return r.Stdout
	}
	// Preserve: with both there, neither row.
	if out := rows(); strings.Contains(out, "the record of the rendered agents") || strings.Contains(out, "the main agent's definition") || !strings.Contains(out, "2 agents in ~/.claude/agents") {
		t.Errorf("with the record and the main agent there, doctor says:\n%s", out)
	}
	if err := os.Remove(filepath.Join(agents, ".jhon-owned")); err != nil {
		t.Fatal(err)
	}
	if out := rows(); !strings.Contains(out, record) || strings.Contains(out, "the main agent's definition") {
		t.Errorf("with no record, doctor says:\n%s\nwant the row %q alone", out, record)
	}
	support.Write(t, agents, map[string]string{".jhon-owned": "jhon-orchestrator\nworker\n"})
	if err := os.Remove(filepath.Join(agents, "jhon-orchestrator.md")); err != nil {
		t.Fatal(err)
	}
	out := rows()
	if !strings.Contains(out, main) || strings.Contains(out, "the record of the rendered agents") {
		t.Errorf("with no main agent, doctor says:\n%s\nwant the row %q alone", out, main)
	}
	if r := support.Jhon(t, m.proj, env, "doctor"); r.Code != 1 {
		t.Errorf("doctor with no main agent: exit %d; want 1", r.Code)
	}
}

// A main agent's definition that is there and cannot be read, or holds
// nothing, leaves the session as guardless as a missing one: the doctor's
// row says which, with the fix, and the doctor fails.
func TestDoctorNamesAMainAgentDefinitionThatCannotBeReadOrIsEmpty(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("skipped: root reads a file of mode 000, so an unreadable definition cannot be made here")
	}
	const file = "~/.claude/agents/jhon-orchestrator.md"
	for name, c := range map[string]struct {
		mode os.FileMode
		text string
		row  string
	}{
		"unreadable": {0o000, "---\nname: jhon-orchestrator\n---\nbody\n", "  FAIL  the main agent's definition (" + file + ") cannot be read: no guard judges the main agent's tool calls in a Claude Code session — make it a file the owner can read, or remove it, then run 'jhon sync'\n"},
		"empty":      {0o644, "", "  FAIL  the main agent's definition (" + file + ") is empty: no guard judges the main agent's tool calls in a Claude Code session — run 'jhon sync'\n"},
		"blank":      {0o644, "\n  \n", "  FAIL  the main agent's definition (" + file + ") is empty: no guard judges the main agent's tool calls in a Claude Code session — run 'jhon sync'\n"},
	} {
		m := newMachine(t)
		agents := filepath.Join(m.home, ".claude", "agents")
		support.Write(t, agents, map[string]string{"jhon-orchestrator.md": c.text, "worker.md": "---\nname: worker\n---\nbody\n", ".jhon-owned": "jhon-orchestrator\nworker\n"})
		definition := filepath.Join(agents, "jhon-orchestrator.md")
		if err := os.Chmod(definition, c.mode); err != nil {
			t.Fatal(err)
		}
		// The scratch folder is removed whatever the file's mode.
		t.Cleanup(func() { _ = os.Chmod(definition, 0o644) })
		env := append(append([]string{}, m.env...), "JHON_SETTINGS="+filepath.Join(m.home, "settings.toml"))
		support.Write(t, m.home, map[string]string{"settings.toml": "schema = 1\n\n[runtimes]\nclaude_code = true\n"})
		r := support.Jhon(t, m.proj, env, "doctor")
		if r.Code != 1 || !strings.Contains(r.Stdout, c.row) || strings.Count(r.Stdout, "the main agent's definition") != 1 || strings.Contains(r.Stdout, "the record of the rendered agents") {
			t.Errorf("%s: doctor exits %d and says:\n%s\nwant exit 1 and the row %q alone", name, r.Code, r.Stdout, c.row)
		}
	}
}

// T133e (L1016, G6): a plain doctor says how context7 is mounted, or that it
// is off, and asks its endpoint nothing; `jhon settings` turns it off and on.
func TestDoctorSaysHowContext7IsMountedAndSettingsTurnItOff(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	file := filepath.Join(m.home, "settings.toml")
	// A proxy nobody listens on: a network call by a plain doctor would fail, not pass unseen.
	env := append(append([]string{}, m.env...), "JHON_SETTINGS="+file, "HTTPS_PROXY=http://127.0.0.1:1", "HTTP_PROXY=http://127.0.0.1:1")
	support.Write(t, m.home, map[string]string{"settings.toml": "schema = 1\n"})
	mounted := "context7 is the hosted https://mcp.context7.com/mcp, for explore and worker: nothing installed or pinned; run 'jhon doctor --remote' to ask whether it answers\n"
	off := "context7 is off (integrations.context7 = false)\n"
	row := func() string {
		t.Helper()
		out := support.Jhon(t, m.proj, env, "doctor").Stdout
		_, versions, ok := strings.Cut(out, "\nversions\n")
		if !ok {
			t.Fatalf("doctor has no versions section:\n%s", out)
		}
		return versions
	}
	if got := row(); !strings.Contains(got, mounted) || strings.Contains(got, "context7 answers") || strings.Contains(got, "context7 unreachable") {
		t.Errorf("on, its default, doctor says:\n%s\nwant %q and no answer asked", got, mounted)
	}
	if r := support.Jhon(t, m.proj, env, "settings", "get", "integrations.context7"); r.Code != 0 || strings.TrimSpace(r.Stdout) != "true" {
		t.Errorf("settings get: exit %d, %q; want true, its default", r.Code, r.Stdout)
	}
	if r := support.Jhon(t, m.proj, env, "settings", "set", "integrations.context7", "false"); r.Code != 0 {
		t.Fatalf("settings set false: exit %d\n%s", r.Code, r.Out())
	}
	if got := support.Read(t, file); got != "schema = 1\n\n[integrations]\ncontext7 = false\n" {
		t.Errorf("the settings file holds %q", got)
	}
	if got := row(); !strings.Contains(got, off) || strings.Contains(got, "the hosted") {
		t.Errorf("off, doctor says:\n%s\nwant %q", got, off)
	}
	r := support.Jhon(t, m.proj, env, "settings", "set", "integrations.context7", "hosted")
	if want := "integrations.context7: \"hosted\" is not accepted (accepted: true, false)"; r.Code != 2 || !strings.Contains(r.Stderr, want) {
		t.Errorf("a value that is no bool: exit %d, stderr %q; want 2 and %q", r.Code, r.Stderr, want)
	}
	if got := support.Read(t, file); got != "schema = 1\n\n[integrations]\ncontext7 = false\n" {
		t.Errorf("a refused value changed the file: %q", got)
	}
}
