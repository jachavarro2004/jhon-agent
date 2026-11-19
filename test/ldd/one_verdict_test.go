// T154 (L1394): one deliver check. A commit that closes a slice, `jhon
// deliver check` and a PR judge the same tree with one verdict (ldd's
// TestedTree and SlicesLeft): the same answer, in the same words.

package ldd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestCommitDeliverCheckAndPRAgree(t *testing.T) {
	t.Parallel()
	root := workspace(t, "a", "true", "b", "true", "c", "true", "d", "true")
	config := filepath.Join(root, "openspec", "config.yaml")
	b, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, append(b, "delivery: change\n"...), 0o666); err != nil {
		t.Fatal(err)
	}
	outlined := "\n## Slice 2: b\n\nDelivers: CA-02\n"
	support.Write(t, root, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n\nDelivers: CA-01\n\n- [ ] 1.1 a\n" + outlined})
	support.GitRepo(t, root)
	support.Git(t, root, "checkout", "-q", "-B", "main")
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "base")
	support.Git(t, root, "checkout", "-q", "-b", "feat")
	support.Write(t, root, map[string]string{"openspec/changes/c1/proposal.md": "# c1\n"})
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "the change starts")

	guard := func(command string) support.Result {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": command},
			"cwd": root, "permission_mode": "default"})
		return support.Run(t, support.Cmd{Args: []string{support.Bin(t), "hook", "route-guard"}, Env: support.Env(), Stdin: string(payload)})
	}
	check := func() (int, []string) {
		t.Helper()
		r := jhonGit(t, root, "deliver", "check", "c1", "--json")
		var c struct{ Reasons []string }
		if err := json.Unmarshal([]byte(r.Stdout), &c); err != nil {
			t.Fatalf("deliver check: %v: %q %q", err, r.Stdout, r.Stderr)
		}
		return r.Code, c.Reasons
	}
	has := func(reasons []string, s string) bool {
		for _, r := range reasons {
			if strings.Contains(r, s) {
				return true
			}
		}
		return false
	}

	// Slice 1: its code, its passing run, then its task checked off.
	support.Write(t, root, map[string]string{"a/feature.go": "package a\n", "a/feature_test.go": "package a\n"})
	equal(t, jhonGit(t, root, "executions", "run", "c1").Code, 0, "the slice's tests pass")
	support.Write(t, root, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n\nDelivers: CA-01\n\n- [x] 1.1 a\n" + outlined})

	// The tested tree: the commit passes, and deliver check says the run
	// tested it; the task checked off after the run is the change's record.
	equal(t, guard("git add -A && git commit -m 'slice 1'").Code, 0, "the commit of the tested tree")
	_, reasons := check()
	if !has(reasons, "the last test run passed on it as it is: commit it") {
		t.Errorf("deliver check of the tested tree: %q", reasons)
	}

	// A file changed after the run: both name it, in the same words.
	support.Write(t, root, map[string]string{"a/feature.go": "package a\n\nvar X = 1\n"})
	untested := "1 file(s) changed after its last test run (a/feature.go)"
	r := guard("git add -A && git commit -m 'slice 1'")
	equal(t, r.Code, 2, "the commit of an untested tree", r.Stderr)
	if !strings.Contains(r.Stderr, untested) {
		t.Errorf("the commit's refusal: %q", r.Stderr)
	}
	code, reasons := check()
	equal(t, code, 1, "deliver check of an untested tree", reasons)
	if !has(reasons, untested) {
		t.Errorf("deliver check of an untested tree: %q", reasons)
	}

	// Committed as tested: deliver check and the PR wait for the outlined
	// slice with one reason, word for word.
	support.Write(t, root, map[string]string{"a/feature.go": "package a\n"})
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "slice 1")
	code, reasons = check()
	left := "delivery: change in openspec/config.yaml merges the change once, after its last slice; 1 slice(s) of tasks.md are outlined with no tasks yet (Slice 2: b): commit each slice on the change's branch and merge when every slice is done"
	equal(t, code, 1, "deliver check with a slice left", reasons)
	equal(t, strings.Join(reasons, "\n"), left, "deliver check's only reason")
	r = guard("gh pr create --title x --body y")
	equal(t, r.Code, 2, "the PR with a slice left", r.Stderr)
	equal(t, r.Stderr, "route-guard: c1: "+left+".\n", "the PR's refusal is deliver check's reason")

	// Every slice done and committed: deliver check is ready, and the PR
	// waits for no slice.
	support.Write(t, root, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n\nDelivers: CA-01\n\n- [x] 1.1 a\n\n## Slice 2: b\n\nDelivers: CA-02\n\n- [x] 2.1 b\n"})
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "slice 2")
	code, reasons = check()
	equal(t, code, 0, "deliver check with every slice done", reasons)
	if r := guard("gh pr create --title x --body y"); strings.Contains(r.Stderr, "delivery: change") {
		t.Errorf("the PR with every slice done: %q", r.Stderr)
	}
}
