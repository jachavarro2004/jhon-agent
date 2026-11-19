package audit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// bashCall is a main-thread Bash call and its result, at second at.
func bashCall(id string, at int, command string) []step {
	common := map[string]any{"userType": "external", "cwd": root, "sessionId": session, "version": "2.1.280", "parentUuid": nil, "isSidechain": false}
	return []step{
		rawStep(with(common, map[string]any{"type": "assistant", "uuid": "b-" + id, "timestamp": iso(at),
			"message": map[string]any{"role": "assistant", "id": "msg_" + id, "content": []any{
				map[string]any{"type": "tool_use", "id": "toolu_" + id, "name": "Bash", "input": map[string]any{"command": command}}}}})),
		rawStep(with(common, map[string]any{"type": "user", "uuid": "r-" + id, "timestamp": iso(at + 1),
			"message": map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "toolu_" + id, "content": "ok"}}}})),
	}
}

// T160 (L1405): a main-thread Bash command that assigns an empty quoted
// string (`out=""`) after a change is named panicked `jhon audit` with
// "index out of range [0] with length 0" in audit.parallel, so `jhon audit
// --all --since 14d` died on the first such session in its window while
// `--since 7d`, whose window held none, ran. Both audit every session.
func TestAuditEmptyQuotedAssignment(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	steps := []step{userStep("go")}
	steps = append(steps, bashCall("n", 100, "jhon deliver check "+x)...)
	steps = append(steps, bashCall("e", 102, `out=""`)...)
	steps = append(steps, bashCall("s", 104, `msg=''`)...)
	empty := sessionWith(t, steps...)
	clean := sessionWith(t, userStep("go"))
	for name, main := range map[string]string{"-work-empty": empty, "-work-clean": clean} {
		if err := os.CopyFS(filepath.Join(home, ".claude", "projects", name), os.DirFS(filepath.Dir(main))); err != nil {
			t.Fatal(err)
		}
	}
	// The session with the empty assignment last ran ten days ago: outside 7d, inside 14d.
	old := time.Now().Add(-10 * 24 * time.Hour)
	if err := os.Chtimes(filepath.Join(home, ".claude", "projects", "-work-empty", session+".jsonl"), old, old); err != nil {
		t.Fatal(err)
	}

	t.Run("jhon audit of the session exits with its own status, no panic", func(t *testing.T) {
		r := audit(t, []string{empty, "--json"})
		equal(t, r.Code, 0, r.Stderr)
		noMatch(t, r.Stderr, `panic|index out of range`)
		equal(t, get(parse(t, r.Stdout), "status"), any("nothing-to-audit"))
	})

	for _, c := range []struct {
		since    string
		sessions float64
	}{{"7d", 1}, {"14d", 2}, {"30d", 2}, {"60d", 2}} {
		t.Run("jhon audit --all --since "+c.since+" audits every session in the window", func(t *testing.T) {
			r := audit(t, []string{"--all", "--since", c.since, "--json"}, "HOME="+home)
			equal(t, r.Code, 0, r.Stderr)
			noMatch(t, r.Stderr, `panic|index out of range`)
			j := parse(t, r.Stdout)
			equal(t, get(j, "sessions"), any(c.sessions))
			equal(t, get(j, "audit_errors"), any(0.0))
			human := audit(t, []string{"--all", "--since", c.since}, "HOME="+home)
			equal(t, human.Code, 0, human.Stderr)
			match(t, human.Stdout, `\b`+map[float64]string{1: "1", 2: "2"}[c.sessions]+` session\(s\), 0 Jhon, 0 audit-error\(s\)`)
		})
	}
}
