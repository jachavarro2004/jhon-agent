// `jhon context [<change>] --phase <phase|none> [--json]` (docket Y). The
// resolution itself is unit-tested in internal/ldd (context_test.go).
//
// The launch context the orchestrator pastes into a delegation is this
// command's output, verbatim (orchestrator.md, "Resolve context"). Its derived
// fields are `jhon status`'s, never re-derived, and the phase-specific fields
// come from `phase_context_fields` in contracts/vocabulary.yaml. The expected
// values are read off the docket Y decision and that declaration, not off the
// implementation.
package ldd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

var contextKeys = []string{"context_id", "resolved_at", "head", "project_root", "change", "phase", "vcs"}

// ── the CLI ─────────────────────────────────────────────────────────────────

func TestContextJSON(t *testing.T) {
	t.Parallel()
	root := support.GitRepo(t, temp(t))
	r := jhonGit(t, root, "context", "c", "--phase", "explore", "--json")
	equal(t, r.Code, 0, r.Stderr)
	equal(t, keys(t, []byte(r.Stdout)), append(slices.Clone(contextKeys), "projects", "strict_tdd"))
	c := object(t, r.Stdout)
	equal(t, c["project_root"], root)
	equal(t, c["change"], "c")
	equal(t, c["phase"], "explore")
}

func TestContextCompactJSON(t *testing.T) {
	t.Parallel()
	root := support.GitRepo(t, temp(t))
	r := jhonGit(t, root, "context", "c", "--phase", "explore", "--json")
	equal(t, r.Code, 0, r.Stderr)
	out := strings.TrimRight(r.Stdout, " \t\r\n")
	equal(t, len(strings.Split(out, "\n")), 1, r.Stdout)
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(out)); err != nil {
		t.Fatal(err)
	}
	equal(t, out, compact.String())
}

func TestContextProjectRootFromSubdirectory(t *testing.T) {
	t.Parallel()
	root := support.GitRepo(t, temp(t))
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o777); err != nil {
		t.Fatal(err)
	}
	r := jhonGit(t, sub, "context", "--phase", "none")
	equal(t, r.Code, 0, r.Stderr)
	equal(t, object(t, r.Stdout)["project_root"], root)
}

func TestContextOutsideGit(t *testing.T) {
	t.Parallel()
	plain := temp(t)
	r := jhonGit(t, plain, "context", "--phase", "none", "--json")
	equal(t, r.Code, 0, r.Stderr)
	c := object(t, r.Stdout)
	equal(t, c["project_root"], plain)
	equal(t, c["vcs"].(map[string]any)["state"], "none")
	equal(t, c["head"], nil)
}

func TestContextRoot(t *testing.T) {
	t.Parallel()
	root := support.GitRepo(t, temp(t))
	r := jhonGit(t, temp(t), "context", "--phase", "none", "--root", root)
	equal(t, r.Code, 0, r.Stderr)
	equal(t, object(t, r.Stdout)["project_root"], root)
}

func TestContextRefusals(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		args []string
	}{
		{"an unknown phase", []string{"--phase", "deploy"}},
		{"no --phase", []string{"c"}},
		{"--phase without a value", []string{"--phase"}},
		{"two changes", []string{"a", "b", "--phase", "none"}},
		{"an unknown option", []string{"--phase", "none", "--bogus"}},
	} {
		t.Run("jhon context refuses "+c.name+" with exit 2 and prints nothing on stdout", func(t *testing.T) {
			t.Parallel()
			r := jhonGit(t, temp(t), append([]string{"context"}, c.args...)...)
			equal(t, r.Code, 2, r.Out())
			equal(t, r.Stdout, "")
			match(t, r.Stderr, `usage: jhon context`)
		})
	}
}

func TestContextHelp(t *testing.T) {
	t.Parallel()
	match(t, jhonGit(t, support.Path("test", "ldd"), "help").Stdout, `jhon context \[<change>\] --phase`)
}

// ── the handoff packet (plan item 7) ────────────────────────────────────────
// `--handoff` adds the change's risk tier (jhon risk) and the ids of the
// decisions Engram holds for the change and the areas it touches, so a child
// session or a session after /clear starts from what was already decided.
// JHON_ENGRAM names the engram binary; a fake one makes these deterministic.

func fakeEngram(t *testing.T, dir string) (bin, args string) {
	t.Helper()
	bin, args = filepath.Join(dir, "engram"), filepath.Join(dir, "args.txt")
	script := `#!/bin/sh
printf '%s\n' "$*" >> '` + args + `'
case "$*" in
  *"--type decision"*) printf 'Found 2 memories:\n\n[1] #101 (decision) — Tenant isolation by search_path\n    body\n\n[2] #102 (decision) — Notification service owns email\n    body\n' ;;
  *) printf 'Found 1 memories:\n\n[1] #103 (architecture) — Event bus per service\n    body\n' ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, args
}

func handoff(t *testing.T, dir, engram string, args ...string) support.Result {
	t.Helper()
	argv := append(append([]string{"context"}, args...), "--phase", "none", "--handoff")
	return support.Jhon(t, dir, support.Env("JHON_ENGRAM="+engram), argv...)
}

func TestContextHandoff(t *testing.T) {
	t.Parallel()
	root := support.GitRepo(t, temp(t))
	support.Write(t, root, map[string]string{"services/notifications/mailer.ts": "export const send = () => 1;\n"})
	bin, argsFile := fakeEngram(t, temp(t))
	r := handoff(t, root, bin, "hu-12-items")
	equal(t, r.Code, 0, r.Stderr)
	equal(t, keys(t, []byte(r.Stdout)), append(slices.Clone(contextKeys), "risk", "decisions"))
	c := object(t, r.Stdout)
	equal(t, c["risk"].(map[string]any)["tier"], "high", "code with no test touched")
	equal(t, c["decisions"], []any{
		map[string]any{"id": 101.0, "type": "decision", "title": "Tenant isolation by search_path"},
		map[string]any{"id": 102.0, "type": "decision", "title": "Notification service owns email"},
		map[string]any{"id": 103.0, "type": "architecture", "title": "Event bus per service"},
	})
	asked := support.Read(t, argsFile)
	match(t, asked, `(?m)^search hu-12-items notifications .*--type decision`)
	match(t, asked, `--type architecture`)
}

func TestContextHandoffWithoutEngram(t *testing.T) {
	t.Parallel()
	root := support.GitRepo(t, temp(t))
	r := handoff(t, root, filepath.Join(root, "no-such-engram"), "c1")
	equal(t, r.Code, 0, r.Stderr)
	c := object(t, r.Stdout)
	v, ok := c["decisions"]
	equal(t, ok && v == nil, true, "decisions is null")
	match(t, c["decisions_note"].(string), `engram unavailable`)
	equal(t, c["risk"].(map[string]any)["tier"], "passive")
}
