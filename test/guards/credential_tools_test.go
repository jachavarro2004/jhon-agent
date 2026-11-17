// The secrets guard reads a tool's glob as the safety guard reads a
// command's (T28): by its name, unless it is relative, searched inside the
// project, and expands there to no real credentials file.

package guards

import (
	"path/filepath"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// toolCall runs secrets-guard on one call of tool in p.
func toolCall(t *testing.T, p, tool string, input map[string]any) int {
	t.Helper()
	payload := map[string]any{"tool_name": tool, "tool_input": input, "cwd": p, "agent_id": "a1", "agent_type": "worker"}
	return hook(t, nil, "secrets-guard", []string{"--agent", "*"}, payload).Code
}

// T87b (L630-L631): an extension glob searched outside the project is
// walked, as a Grep glob or path or a Glob pattern; refused once the walk
// meets a credentials file.
func TestToolExtensionGlobOutsideTheProjectIsWalked(t *testing.T) {
	t.Parallel()
	parent := realTemp(t)
	p := filepath.Join(parent, "proj")
	support.Write(t, p, map[string]string{"i18n/en.json": "{}\n"})
	support.GitInit(t, p)
	support.Write(t, parent, map[string]string{"srv/a.json": "{}\n"})
	srv := filepath.Join(parent, "srv")
	grep := []map[string]any{{"glob": "*.json", "path": parent}, {"glob": srv + "/*.json"}, {"glob": "../*.json"}}
	glob := []map[string]any{{"pattern": "../*.json"}, {"pattern": "*.json", "path": srv}}
	run := func(want int, why string) {
		for _, in := range grep {
			in["pattern"] = "x"
			if got := toolCall(t, p, "Grep", in); got != want {
				t.Errorf("Grep %v %s: exit %d, want %d", in, why, got, want)
			}
		}
		for _, in := range glob {
			if got := toolCall(t, p, "Glob", in); got != want {
				t.Errorf("Glob %v %s: exit %d, want %d", in, why, got, want)
			}
		}
	}
	run(0, "with no credentials file")
	support.Write(t, parent, map[string]string{"srv/credentials.json": "{}\n"})
	run(2, "beside a credentials file")
}

func TestSecretsGuardGlobExpands(t *testing.T) {
	t.Parallel()
	p := realTemp(t)
	support.GitInit(t, p)
	support.Write(t, p, map[string]string{"i18n/en.json": "{}\n", ".env.example": "X=\n"})
	for _, g := range []string{"**.json", "*.json", "i18n/*.json", "*.pem", ".env*"} {
		if got := toolCall(t, p, "Grep", map[string]any{"pattern": "x", "glob": g}); got != 0 {
			t.Errorf("Grep glob %s with no credentials file: exit %d, want 0", g, got)
		}
	}
	for _, in := range []map[string]any{{"glob": "*.json", "path": "~/keys"}, {"glob": "id_*", "path": "~"}, {"glob": "{a,b}/*.json"}} {
		in["pattern"] = "x"
		if got := toolCall(t, p, "Grep", in); got != 2 {
			t.Errorf("Grep %v searched outside the project: exit %d, want 2", in, got)
		}
	}
	support.Write(t, p, map[string]string{"sub/credentials.json": "{}\n", "sub/.env": "X=1\n"})
	for _, g := range []string{"*.json", ".env*"} {
		if got := toolCall(t, p, "Grep", map[string]any{"pattern": "x", "glob": g}); got != 2 {
			t.Errorf("Grep glob %s beside sub/: exit %d, want 2", g, got)
		}
	}
	if got := toolCall(t, p, "Grep", map[string]any{"pattern": "x", "path": filepath.Join(p, "i18n"), "glob": "*.json"}); got != 0 {
		t.Errorf("Grep glob *.json under i18n: exit %d, want 0", got)
	}
	if got := toolCall(t, p, "Grep", map[string]any{"pattern": "x", "path": "sub/.env"}); got != 2 {
		t.Errorf("Grep of a literal credentials file: exit %d, want 2", got)
	}
}
