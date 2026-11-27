// inspect-guard, the PreToolUse hook that keeps the review facets' Bash
// read-only by construction (T271): a line passes when every program it runs
// is on the read-only list and keeps its rule, or is one of Jhon's own
// readers declared in agents.yaml (inspect_allow).
//
// The guard is spawned exactly as Claude Code runs it: the hook payload on
// stdin, Jhon's readers as arguments. Exit 0 allows; exit 2 blocks.

package guards

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

var inspectAllow = []string{"git diff", "git show", "git log", "git status", "codegraph callers", "codegraph impact", "codegraph affected", "jhon review freeze"}

func inspectRaw(t *testing.T, stdin any) support.Result {
	t.Helper()
	return hook(t, nil, "inspect-guard", inspectAllow, stdin)
}

func inspectBash(t *testing.T, command string) support.Result {
	t.Helper()
	return inspectRaw(t, map[string]any{"tool_name": "Bash", "tool_input": cmd(command)})
}

func blocked(t *testing.T, r support.Result, label string) {
	t.Helper()
	if r.Code != 2 {
		t.Errorf("%s: expected a block, got exit %d", label, r.Code)
	}
	if strings.TrimSpace(r.Stderr) == "" {
		t.Errorf("%s: a block must state its reason on stderr", label)
	}
}

func TestInspectGuardAllows(t *testing.T) {
	t.Parallel()
	for _, command := range []string{
		"git diff BASE CANDIDATE",
		"git diff --name-only a b",
		"git show CANDIDATE:src/x.ts",
		"git diff HEAD~1 HEAD",
		"codegraph callers foo",
		"jhon review freeze --base main", // each facet freezes for itself (contracts/review-prism.md)
		// T271: what was refused only for its spelling.
		"git diff && echo",
		"git diff ~/x",
		"git diff 'a b'",
	} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			code(t, inspectBash(t, command), 0, command)
		})
	}
}

func TestInspectGuardDenies(t *testing.T) {
	t.Parallel()
	for _, command := range []string{
		"git checkout .",
		"git diff; rm -rf x",
		"git diff | sh",
		"git diff $(id)",
		"git diff `id`",
		"git diff > f",
		"git diff --output=f",
		"git diff --outp=f",
		"git diff --ext-diff",
		"git show --textconv HEAD",
		"git -c core.pager=sh diff",
		"GIT_EXTERNAL_DIFF=x git diff",
		"rm -rf /",
		"codegraph init .",
		"git push",
		"git diff *",
		"git diff\nrm -rf x",
	} {
		t.Run(strconv.Quote(command), func(t *testing.T) {
			t.Parallel()
			blocked(t, inspectBash(t, command), command)
		})
	}
}

func TestInspectGuardDeniesMalformedJSON(t *testing.T) {
	t.Parallel()
	code(t, inspectRaw(t, "{not json"), 2, "malformed JSON")
}

func TestInspectGuardDeniesBashWithoutCommand(t *testing.T) {
	t.Parallel()
	code(t, inspectRaw(t, map[string]any{"tool_name": "Bash", "tool_input": map[string]any{}}), 2, "no command")
}

func TestInspectGuardAllowsNonBash(t *testing.T) {
	t.Parallel()
	code(t, inspectRaw(t, map[string]any{"tool_name": "Read", "tool_input": map[string]any{"file_path": "/etc/passwd"}}), 0, "Read")
}

// ── owner scoping ───────────────────────────────────────────────────────────
// The renderer passes the owning agent as a leading `--agent <name>`. When
// that agent runs as the main session, Claude Code also fires its frontmatter
// hooks for the sub-agents it launches, and the payload's `agent_type` names
// the agent actually calling. A call from another agent is not this guard's
// to judge; an absent `agent_type` is judged (fail closed).

// inspectScoped runs the guard owned by owner; agentType nil omits it.
func inspectScoped(t *testing.T, owner string, allow []string, command string, agentType *string) support.Result {
	t.Helper()
	payload := map[string]any{"tool_name": "Bash", "tool_input": cmd(command)}
	if agentType != nil {
		payload["agent_type"] = *agentType
	}
	return hook(t, nil, "inspect-guard", append([]string{"--agent", owner}, allow...), payload)
}

func ptr(s string) *string { return &s }

func TestInspectGuardOwnerScoping(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		owner, command string
		agentType      *string
		want           int
	}{
		{"prism-safety", "uv add pytest", ptr("general"), 0},
		{"prism-safety", "uv run pytest", ptr("explore"), 0},
		{"prism-safety", "uv add pytest", ptr("prism-safety"), 2},
		{"prism-safety", "uv add pytest", nil, 2},
		{"prism-safety", "uv add pytest", ptr(""), 2},
		{"prism-safety", "git push", ptr("prism-safety"), 2},
		{"prism-safety", "git log --oneline -5", ptr("prism-safety"), 0},
		{"prism-safety", "git status --short", ptr("prism-safety"), 0},
		{"prism-safety", "git log --output=f", ptr("prism-safety"), 2},
		{"prism-safety", "git diff a b", ptr("prism-safety"), 0},
	} {
		at := "undefined"
		if c.agentType != nil {
			at = strconv.Quote(*c.agentType)
		}
		t.Run("owner "+c.owner+", agent_type "+at+": "+c.command+" exits "+strconv.Itoa(c.want), func(t *testing.T) {
			t.Parallel()
			r := inspectScoped(t, c.owner, inspectAllow, c.command, c.agentType)
			code(t, r, c.want, c.command)
			if c.want == 2 && strings.TrimSpace(r.Stderr) == "" {
				t.Error("a block must state its reason on stderr")
			}
		})
	}
}

// The owner's name is never read as one of Jhon's readers.
func TestInspectGuardOwnerNameIsNotAPrefix(t *testing.T) {
	t.Parallel()
	code(t, inspectScoped(t, "codegraph", []string{"codegraph callers"}, "codegraph init", ptr("codegraph")), 2, "codegraph init")
}

func TestInspectGuardWithoutAgentJudgesEveryCaller(t *testing.T) {
	t.Parallel()
	code(t, inspectRaw(t, map[string]any{"tool_name": "Bash", "agent_type": "general", "tool_input": cmd("uv add pytest")}), 2, "uv add pytest")
}

func TestInspectGuardAgentWithoutNameFailsClosed(t *testing.T) {
	t.Parallel()
	code(t, hook(t, nil, "inspect-guard", []string{"--agent"}, map[string]any{"tool_name": "Bash", "agent_type": "general", "tool_input": cmd("git diff")}), 2, "--agent")
}

// ── the refusal (S11) ───────────────────────────────────────────────────────
// A refusal names, on its first line, what it refused, why, and what to do
// instead. No refusal declares `allowed forms` any more: nothing is judged by
// its form (T271), so `jhon audit` reads no form a later call could match.

func TestInspectGuardRefusalNamesWhatWhyAndAFix(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"git checkout .", "git diff; rm -rf x", "git diff > f", "git diff --output=f", "GIT_PAGER=cat git diff", "git diff *"} {
		t.Run(strconv.Quote(command), func(t *testing.T) {
			t.Parallel()
			r := inspectBash(t, command)
			code(t, r, 2, command)
			matches(t, strings.Split(r.Stderr, "\n")[0], `^inspect-guard: \S.*: \S.*\. Instead: \S`, "first line")
			if strings.Contains(r.Stderr, "allowed forms:") {
				t.Errorf("%s declares forms: %s", command, r.Stderr)
			}
		})
	}
}

func TestInspectGuardPayloadRefusalDeclaresNoForms(t *testing.T) {
	t.Parallel()
	for _, r := range []support.Result{inspectRaw(t, "{not json"), inspectRaw(t, map[string]any{"tool_name": "Bash", "tool_input": map[string]any{}})} {
		if strings.Contains(r.Stderr, "allowed forms:") {
			t.Errorf("declares forms: %s", r.Stderr)
		}
	}
}

// ── other repositories (docket F8, AT1; T271) ───────────────────────────────
// Session 6506479f's orchestrator was refused two harmless reads: `git -C
// <its change's worktree> status --short` and `git rev-parse <rev>^{tree}`;
// a reviewer was refused `cd <dir> &&`, `2>/dev/null` and `| head`. Since
// T271 a read is a read wherever it runs, as the Read tool's is: `cd` and
// `git -C` reach any folder, and what writes, runs or fetches stays refused.

var inspectOrch = append(append([]string{}, inspectAllow...), "git rev-parse")

type inspectRepos struct{ project, worktree, other string }

// newInspectRepos is a repository with a linked worktree beside it, and an
// unrelated repository.
func newInspectRepos(t *testing.T) inspectRepos {
	t.Helper()
	root := realTemp(t)
	r := inspectRepos{project: filepath.Join(root, "acme_app"), worktree: filepath.Join(root, "acme_app-worktrees", "verification-email-ordering"), other: filepath.Join(root, "other")}
	mkdir(t, filepath.Join(r.project, "sub"))
	support.GitRepo(t, r.project)
	support.Git(t, r.project, "worktree", "add", "-q", "-b", "wt", r.worktree)
	mkdir(t, r.other)
	support.GitRepo(t, r.other)
	return r
}

func inCwd(t *testing.T, cwd, command string) support.Result {
	t.Helper()
	return hook(t, nil, "inspect-guard", append([]string{"--agent", "jhon-orchestrator"}, inspectOrch...),
		map[string]any{"tool_name": "Bash", "cwd": cwd, "tool_input": cmd(command), "agent_type": "jhon-orchestrator"})
}

func TestInspectGuardReadsAnyRepository(t *testing.T) {
	t.Parallel()
	r := newInspectRepos(t)
	for _, command := range []string{
		"git -C " + r.worktree + " status --short",
		"git -C " + filepath.Join(r.project, "sub") + " status --short",
		"git -C sub diff HEAD",
		"git -C " + r.worktree + " rev-parse HEAD",
		"git -C " + r.other + " status",
		"git -C /tmp status",
		"git -C " + r.project + " -C " + r.other + " status",
		"git -C ~/x status",
		"git rev-parse 9b1bde21^{tree}", "git diff HEAD~1^{tree} HEAD^{tree}", "git -C " + r.worktree + " rev-parse HEAD^{tree}",
		"git rev-parse HEAD^{commit}", "git rev-parse {tree}", "codegraph callers x^{tree}",
		"cd " + r.worktree + " && git diff HEAD~1 HEAD", "cd sub && git status --short", "cd " + r.other + " && git diff", "cd /tmp && git status",
		"cd ~ && git diff", "cd sub && cd .. && git diff", "cd sub; git diff",
		"git diff a b 2>/dev/null", "git diff >/dev/null", "git diff a b | head -n 40", "git show HEAD:a.go | tail -20",
		"git diff --name-only a b | wc -l", "git diff | head -c 4", "git diff | tail -f", "git diff | wc -L", "git diff | head $(id)",
		"cd " + r.worktree + " && git diff a b 2>/dev/null | head -50", "git rev-parse HEAD^{tree} 2>/dev/null",
	} {
		code(t, inCwd(t, r.project, command), 0, command)
	}
	for _, command := range []string{
		"git -C " + r.project + " push",
		"git -C " + r.project + " -c core.pager=sh diff",
		"git -C " + r.project + " diff --output=f",
		"git -C " + r.project + " diff --ext-diff",
		"git rev-parse HEAD^{tree}; rm x",
		"git rev-parse HEAD^{tree} > f",
		"cd sub && git push",
		"git diff 2>/dev/null > f",
		"git diff 2>/tmp/x",
		"git diff | head | sh",
		"git diff | sh",
		"git diff | head -n 4; rm -rf x",
		"git push 2>/dev/null",
		"git checkout . | head",
	} {
		blocked(t, inCwd(t, r.project, command), command)
	}
}
