// inspect-guard, the PreToolUse hook that confines the review lenses' Bash
// to the read-only prefixes declared in agents.yaml.
//
// The guard is spawned exactly as Claude Code runs it: the hook payload on
// stdin, the allowed prefixes as arguments. Exit 0 allows; exit 2 blocks.

package guards

import (
	"path/filepath"
	"reflect"
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
		"jhon review freeze --base main", // each lens freezes for itself (contracts/review-4r.md)
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
		"git diff && echo",
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
		"git diff ~/x",
		"git diff 'a b'",
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

func TestInspectGuardNoPrefixesDeniesAll(t *testing.T) {
	t.Parallel()
	code(t, hook(t, nil, "inspect-guard", nil, map[string]any{"tool_name": "Bash", "tool_input": cmd("git diff")}), 2, "no prefixes")
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
		{"review-risk", "uv add pytest", ptr("general"), 0},
		{"review-risk", "uv run pytest", ptr("explore"), 0},
		{"review-risk", "uv add pytest", ptr("review-risk"), 2},
		{"review-risk", "uv add pytest", nil, 2},
		{"review-risk", "uv add pytest", ptr(""), 2},
		{"review-risk", "git push", ptr("review-risk"), 2},
		{"review-risk", "git log --oneline -5", ptr("review-risk"), 0},
		{"review-risk", "git status --short", ptr("review-risk"), 0},
		{"review-risk", "git log --output=f", ptr("review-risk"), 2},
		{"review-risk", "git diff a b", ptr("review-risk"), 0},
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

func TestInspectGuardOwnerNameIsNotAPrefix(t *testing.T) {
	t.Parallel()
	code(t, inspectScoped(t, "git", []string{"git diff"}, "git log", ptr("git")), 2, "git log")
}

func TestInspectGuardWithoutAgentJudgesEveryCaller(t *testing.T) {
	t.Parallel()
	code(t, inspectRaw(t, map[string]any{"tool_name": "Bash", "agent_type": "general", "tool_input": cmd("uv add pytest")}), 2, "uv add pytest")
}

func TestInspectGuardAgentWithoutNameFailsClosed(t *testing.T) {
	t.Parallel()
	code(t, hook(t, nil, "inspect-guard", []string{"--agent"}, map[string]any{"tool_name": "Bash", "agent_type": "general", "tool_input": cmd("git diff")}), 2, "--agent")
}

// ── declared allowed forms (docket AA5) ─────────────────────────────────────
// A refusal that concerns only the form of a command declares the forms this
// guard allows, on its own line, so an agent and `jhon audit` can tell a form
// the same authority permits from a way around it. A refusal over an option
// that writes a file or runs a program, and a refusal of the payload itself,
// declare nothing: the form was never the issue.

func declared(err string) []string {
	out := []string{}
	for _, l := range strings.Split(err, "\n") {
		if strings.HasPrefix(l, "allowed forms: ") {
			out = append(out, l)
		}
	}
	return out
}

func declares(t *testing.T, r support.Result, want []string) {
	t.Helper()
	if got := declared(r.Stderr); !reflect.DeepEqual(got, want) {
		t.Errorf("declared %q; want %q: %s", got, want, r.Stderr)
	}
}

func TestInspectGuardFormRefusalDeclaresForms(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"git -C /work/p diff a b", "git checkout .", "git diff; rm -rf x", "git diff ~/x", "GIT_PAGER=cat git diff"} {
		t.Run(strconv.Quote(command), func(t *testing.T) {
			t.Parallel()
			r := inspectBash(t, command)
			code(t, r, 2, command)
			declares(t, r, []string{"allowed forms: " + strings.Join(inspectAllow, ", ")})
		})
	}
}

func TestInspectGuardReasonOnFirstLine(t *testing.T) {
	t.Parallel()
	r := inspectBash(t, "git checkout .")
	matches(t, strings.Split(r.Stderr, "\n")[0], `^inspect-guard: command is outside this agent's Bash allowlist \(git diff, git show, `, "first line")
}

func TestInspectGuardOptionRefusalDeclaresNoForms(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"git diff --output=f", "git show --textconv HEAD"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			r := inspectBash(t, command)
			code(t, r, 2, command)
			declares(t, r, []string{})
		})
	}
}

func TestInspectGuardPayloadRefusalDeclaresNoForms(t *testing.T) {
	t.Parallel()
	declares(t, inspectRaw(t, "{not json"), []string{})
	declares(t, inspectRaw(t, map[string]any{"tool_name": "Bash", "tool_input": map[string]any{}}), []string{})
}

func TestInspectGuardNoPrefixesDeclaresNoForms(t *testing.T) {
	t.Parallel()
	r := hook(t, nil, "inspect-guard", nil, map[string]any{"tool_name": "Bash", "tool_input": cmd("git diff")})
	code(t, r, 2, "no prefixes")
	declares(t, r, []string{})
}

// ── narrow read forms (docket F8 / AT1) ─────────────────────────────────────
// Session 6506479f's orchestrator was refused two harmless reads and
// delegated them to a worker: `git -C <its change's worktree> status --short`
// (outside the allowlist) and `git rev-parse <rev>^{tree}` (shell syntax). The
// guard now reads exactly these two forms: one leading `-C <path>` whose path
// is inside the session's cwd or a linked worktree of the same repository,
// and a `<rev>^{tree}` argument of a git command. Everything else stays
// refused.

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

func TestInspectGuardF8DashCInProject(t *testing.T) {
	t.Parallel()
	r := newInspectRepos(t)
	for _, command := range []string{
		"git -C " + r.worktree + " status --short",
		"git -C " + filepath.Join(r.project, "sub") + " status --short",
		"git -C sub diff HEAD",
		"git -C " + r.worktree + " rev-parse HEAD",
	} {
		code(t, inCwd(t, r.project, command), 0, command)
	}
}

func TestInspectGuardF8TreeArgument(t *testing.T) {
	t.Parallel()
	r := newInspectRepos(t)
	for _, command := range []string{"git rev-parse 9b1bde21^{tree}", "git diff HEAD~1^{tree} HEAD^{tree}", "git -C " + r.worktree + " rev-parse HEAD^{tree}"} {
		code(t, inCwd(t, r.project, command), 0, command)
	}
}

func TestInspectGuardF8OpensNothingElse(t *testing.T) {
	t.Parallel()
	r := newInspectRepos(t)
	for _, command := range []string{
		"git -C " + r.other + " status",
		"git -C /tmp status",
		"git -C " + r.project + " push",
		"git -C " + r.project + " -c core.pager=sh diff",
		"git -C " + r.project + " -C " + r.other + " status",
		"git -C " + r.project + " diff --output=f",
		"git -C " + r.project + " diff --ext-diff",
		"git -C ~/x status",
		"git -C",
		"git rev-parse HEAD^{commit}",
		"git rev-parse {tree}",
		"git rev-parse HEAD^{tree}; rm x",
		"git rev-parse HEAD^{tree} > f",
		"codegraph callers x^{tree}",
	} {
		code(t, inCwd(t, r.project, command), 2, command)
	}
}

// Docket AT1 (closed in AY B5): a reviewer was refused three harmless
// wrappers around an allowed read. The guard now strips exactly these, then
// judges what is left as before: one leading `cd <dir> &&` into the session's
// project or its worktrees, a literal `2>/dev/null`, and one trailing
// `| head`, `| tail` (with `-n N` or `-N`), or `| wc` (with `-l`, `-w`, or
// `-c`). Any other shell syntax, and any wrapper around a refused command,
// stays refused.
func TestInspectGuardAT1Wrappers(t *testing.T) {
	t.Parallel()
	r := newInspectRepos(t)
	for _, command := range []string{
		"cd " + r.worktree + " && git diff HEAD~1 HEAD",
		"cd sub && git status --short",
		"git diff a b 2>/dev/null",
		"git diff a b | head -n 40",
		"git show HEAD:a.go | tail -20",
		"git diff --name-only a b | wc -l",
		"cd " + r.worktree + " && git diff a b 2>/dev/null | head -50",
		"git rev-parse HEAD^{tree} 2>/dev/null",
	} {
		code(t, inCwd(t, r.project, command), 0, command)
	}
	for _, command := range []string{
		"cd " + r.other + " && git diff",
		"cd /tmp && git status",
		"cd ~ && git diff",
		"cd sub && git push",
		"cd sub && cd .. && git diff",
		"git diff 2>/dev/null > f",
		"git diff 2>/tmp/x",
		"git diff >/dev/null",
		"git diff | head | sh",
		"git diff | sh",
		"git diff | head -n 4; rm -rf x",
		"git diff | head -c 4",
		"git diff | tail -f",
		"git diff | wc -L",
		"git diff | head $(id)",
		"cd sub; git diff",
		"git push 2>/dev/null",
		"git checkout . | head",
	} {
		blocked(t, inCwd(t, r.project, command), command)
	}
}
