// With sandbox.mode on, Claude Code matches the owner's own Bash deny and
// ask rules against the wrapped command, so they stop applying (L899 b).
// `jhon sandbox status` and `jhon doctor` say how many such rules Claude
// Code's settings hold: the user's, the project's and the project's local
// file. They never print a rule, and never fail on a file they cannot read.
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// rules is `jhon sandbox status --json`'s claude_bash_rules.
type rules struct {
	Deny       int      `json:"deny"`
	Ask        int      `json:"ask"`
	Unreadable []string `json:"unreadable"`
}

// rules is the status's claude_bash_rules from the project; there is false
// when the status has no such field.
func (m machine) rules(t *testing.T, env []string) (r rules, there bool) {
	t.Helper()
	res := support.Jhon(t, m.proj, env, "sandbox", "status", "--json")
	if res.Code != 0 || res.Stderr != "" {
		t.Fatalf("status --json: exit %d, stderr %q", res.Code, res.Stderr)
	}
	var s map[string]json.RawMessage
	if err := json.Unmarshal([]byte(res.Stdout), &s); err != nil {
		t.Fatalf("status --json printed %q: %v", res.Stdout, err)
	}
	raw, there := s["claude_bash_rules"]
	if there {
		if err := json.Unmarshal(raw, &r); err != nil {
			t.Fatalf("claude_bash_rules is %s: %v", raw, err)
		}
	}
	return r, there
}

// on is the machine's environment with sandbox.mode on.
func (m machine) on(t *testing.T) []string {
	t.Helper()
	support.Write(t, m.home, map[string]string{".config/jhon/settings.toml": "schema = 1\n\n[sandbox]\nmode = \"on\"\n"})
	return m.env
}

// claudeSettings writes the three files: three Bash deny rules and one Bash
// ask rule among rules of other tools and allow rules, which do not count.
func (m machine) claudeSettings(t *testing.T) {
	t.Helper()
	support.Write(t, m.home, map[string]string{".claude/settings.json": `{"permissions":{"deny":["Bash(example-deny-one *)","Bash","Read(./example-private/**)","WebFetch"],"allow":["Bash(example-allowed *)"]},"model":"x"}`})
	support.Write(t, m.proj, map[string]string{
		".claude/settings.json":       `{"permissions":{"ask":["Bash(example-ask-one:*)","Edit(/docs/**)"],"deny":[]}}`,
		".claude/settings.local.json": `{"permissions":{"deny":["Bash(example-deny-two)", 7, "BashOutput", "Bashful(x)"]}}`,
	})
}

const rulesWarning = "Claude Code's settings hold 3 Bash deny rules and 1 Bash ask rule, which do not apply to sandboxed commands (Claude Code matches them against the wrapped command)"

func TestStatusCountsClaudeCodesBashRulesWithTheModeOn(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	m.claudeSettings(t)
	env := m.on(t)
	if got, there := m.rules(t, env); !there || got.Deny != 3 || got.Ask != 1 || len(got.Unreadable) != 0 {
		t.Errorf("claude_bash_rules is %+v (there %v); want 3 deny, 1 ask, none unreadable", got, there)
	}
	for _, args := range [][]string{{"sandbox", "status"}, {"sandbox", "status", "--json"}, {"doctor"}} {
		r := support.Jhon(t, filepath.Join(m.proj, "src"), env, args...)
		if strings.Contains(r.Out(), "example-") {
			t.Errorf("jhon %s prints a rule's text:\n%s", strings.Join(args, " "), r.Out())
		}
	}
	r := support.Jhon(t, filepath.Join(m.proj, "src"), env, "sandbox", "status")
	if r.Code != 0 || strings.Count(r.Stdout, "\n") != 1 || !strings.HasPrefix(r.Stdout, "sandbox on; ") || !strings.HasSuffix(r.Stdout, "; project "+m.proj+"; warning: "+rulesWarning+"\n") {
		t.Errorf("status: exit %d, %q; want one line that ends with the project and the warning", r.Code, r.Stdout)
	}
	if r := support.Jhon(t, m.proj, env, "doctor"); !strings.Contains(r.Stdout, "  warn  "+rulesWarning+"\n") {
		t.Errorf("doctor has no warning row for the rules:\n%s", r.Out())
	}
	// The user's file where CLAUDE_CONFIG_DIR puts it.
	moved := filepath.Join(m.home, "claude-config")
	support.Write(t, moved, map[string]string{"settings.json": `{"permissions":{"ask":["Bash(example-moved *)"]}}`})
	if got, _ := m.rules(t, append(slices.Clone(env), "CLAUDE_CONFIG_DIR="+moved)); got.Deny != 1 || got.Ask != 2 {
		t.Errorf("with CLAUDE_CONFIG_DIR: %+v; want the project's 1 deny and 1 ask, and the moved file's 1 ask", got)
	}
}

func TestStatusSaysNothingOfTheRulesWithTheModeOff(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	before := map[string]support.Result{}
	commands := [][]string{{"sandbox", "status"}, {"sandbox", "status", "--json"}}
	for _, args := range commands {
		before[strings.Join(args, " ")] = support.Jhon(t, m.proj, m.env, args...)
	}
	m.claudeSettings(t)
	for _, args := range commands {
		if got := support.Jhon(t, m.proj, m.env, args...); got != before[strings.Join(args, " ")] || strings.Contains(got.Stdout, "claude") || strings.Contains(got.Stdout, "warning") {
			t.Errorf("jhon %s with the mode off: %+v; before the rules %+v", strings.Join(args, " "), got, before[strings.Join(args, " ")])
		}
	}
	if r := support.Jhon(t, m.proj, m.env, "doctor"); strings.Contains(r.Stdout, "Bash deny") {
		t.Errorf("doctor with the mode off names the rules:\n%s", r.Stdout)
	}
}

func TestStatusNeverFailsOnClaudeCodesSettings(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	env := m.on(t)
	// No file at all: no rule, no warning.
	if got, there := m.rules(t, env); !there || got.Deny != 0 || got.Ask != 0 || len(got.Unreadable) != 0 {
		t.Errorf("with no settings file: %+v (there %v); want the field with no rule", got, there)
	}
	if r := support.Jhon(t, m.proj, env, "sandbox", "status"); r.Code != 0 || !strings.HasSuffix(r.Stdout, "; project "+m.proj+"\n") {
		t.Errorf("status with no settings file: exit %d, %q; want no warning", r.Code, r.Stdout)
	}
	// One file whose rules are no list, one that is no JSON, one that is a folder.
	support.Write(t, m.home, map[string]string{".claude/settings.json": `{"permissions":{"deny":"Bash"}}`})
	support.Write(t, m.proj, map[string]string{".claude/settings.json": "{not json"})
	local := filepath.Join(m.proj, ".claude", "settings.local.json")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	got, _ := m.rules(t, env)
	want := []string{filepath.Join(m.home, ".claude", "settings.json"), filepath.Join(m.proj, ".claude", "settings.json"), local}
	if got.Deny != 0 || got.Ask != 0 || !slices.Equal(got.Unreadable, want) {
		t.Errorf("with three broken files: %+v; want no rule and unreadable %q", got, want)
	}
	r := support.Jhon(t, m.proj, env, "sandbox", "status")
	warning := "; warning: Claude Code's settings could not be read for Bash deny and ask rules, which do not apply to sandboxed commands (unreadable: " + strings.Join(want, ", ") + ")\n"
	if r.Code != 0 || r.Stderr != "" || strings.Count(r.Stdout, "\n") != 1 || !strings.HasSuffix(r.Stdout, warning) {
		t.Errorf("status with three broken files: exit %d, stderr %q, %q", r.Code, r.Stderr, r.Stdout)
	}
}
