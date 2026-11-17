// T101 (L671): a proposal and a design the route comes to ask for mid-change
// are said on the next commit that closes a slice, as `jhon status` says them:
// a note, never a refusal.

package ldd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestASliceCommitNamesTheProposalAndDesignTheRouteNowAsks(t *testing.T) {
	t.Parallel()
	lines := func(n int) string { return "package a\n" + strings.Repeat("var _ = 1\n", n) }
	py := func(n int) string { return strings.Repeat("x = 1\n", n) }
	setup := func(t *testing.T, add int, record map[string]string) string {
		root := tree(t, map[string]string{
			"a/go.mod":                     "module example.com/a\n",
			"b/pyproject.toml":             "[project]\nname = \"b\"\n",
			"openspec/config.yaml":         "route_verified_tier: passive\nroute_design_lines: 5\n",
			"openspec/changes/c1/tasks.md": "## Slice 1: a\n\n- [ ] 1.1 a\n\n## Slice 2: b\n\n- [ ] 2.1 b\n",
		})
		support.GitRepo(t, root)
		support.Git(t, root, "checkout", "-q", "-B", "main")
		support.Git(t, root, "add", "-A")
		support.Git(t, root, "commit", "-q", "-m", "base")
		support.Git(t, root, "checkout", "-q", "-b", "feat")
		support.Write(t, root, record)
		support.Write(t, root, map[string]string{"a/a.go": lines(add), "b/b.py": py(add),
			"openspec/changes/c1/tasks.md": "## Slice 1: a\n\n- [x] 1.1 a\n\n## Slice 2: b\n\n- [ ] 2.1 b\n"})
		return root
	}
	commit := func(t *testing.T, root string) support.Result {
		payload, _ := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "git add -A && git commit -m 'slice 1'"},
			"cwd": root, "permission_mode": "default"})
		return support.Run(t, support.Cmd{Args: []string{support.Bin(t), "hook", "route-guard"}, Env: support.Env(), Stdin: string(payload)})
	}
	note := func(t *testing.T, out string) string {
		if strings.TrimSpace(out) == "" {
			return ""
		}
		var j struct {
			H map[string]string `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal([]byte(out), &j); err != nil {
			t.Fatalf("%v: %q", err, out)
		}
		if j.H["permissionDecision"] != "" || j.H["hookEventName"] != "PreToolUse" {
			t.Errorf("a note decides nothing: %s", out)
		}
		return j.H["additionalContext"]
	}
	// The verified route says verify is due on every slice commit; the
	// design note follows it.
	const verify = "route verified (risk high reaches the verified tier (passive)): verify is due before delivery; launch the verify agent and record verify-report.md before the PR"
	const want = verify + "; the route asks for a proposal and a design now (a, b): write openspec/changes/c1/proposal.md (missing) and openspec/changes/c1/design.md (missing) before the next slice; `jhon status c1` lists what each needs"

	t.Run("over the threshold", func(t *testing.T) {
		t.Parallel()
		root := setup(t, 6, nil)
		r := commit(t, root)
		equal(t, r.Code, 0, "a note, not a refusal")
		equal(t, r.Stderr, "", "stderr")
		equal(t, note(t, r.Stdout), want, "the note")
	})
	t.Run("only the design missing", func(t *testing.T) {
		t.Parallel()
		root := setup(t, 6, map[string]string{"openspec/changes/c1/proposal.md": "# c1\n\n## Why\n\nBecause.\n\n## What Changes\n\nThings.\n",
			"openspec/changes/c1/design.md": "# Design\n"})
		r := commit(t, root)
		equal(t, r.Code, 0, "exit")
		equal(t, note(t, r.Stdout), verify+"; the route asks for a proposal and a design now (a, b): write openspec/changes/c1/design.md (incomplete) before the next slice; `jhon status c1` lists what each needs", "the note")
	})
	t.Run("under the threshold", func(t *testing.T) {
		t.Parallel()
		root := setup(t, 2, nil)
		r := commit(t, root)
		equal(t, r.Code, 0, "exit")
		equal(t, note(t, r.Stdout), verify, "no design note")
	})
	t.Run("record already written", func(t *testing.T) {
		t.Parallel()
		root := setup(t, 6, map[string]string{"openspec/changes/c1/proposal.md": "# c1\n\n## Why\n\nBecause.\n\n## What Changes\n\nThings.\n",
			"openspec/changes/c1/design.md": "# Design\n\nThe a and b parts talk over a file.\n"})
		r := commit(t, root)
		equal(t, r.Code, 0, "exit")
		equal(t, note(t, r.Stdout), verify, "no design note")
	})
}
