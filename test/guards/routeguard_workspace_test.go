// T57 slice 7 (L1324, D3): one PR per repository, the primary's last. The
// primary's `gh pr create` for a change with workspace members opens only
// when every member's delivery is ready and the PR's body names, for each
// one, a pull request of that member's origin; a change with no member,
// and a member's own PR, are judged as before.

package guards

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestRouteGuardPrimaryPRWaitsForItsMembers(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t, "openspec/config.yaml")
	support.Write(t, w.app, map[string]string{"openspec/config.yaml": "workspace:\n  lib: ../lib\n"})
	support.Git(t, w.app, "add", "-A")
	support.Git(t, w.app, "commit", "-q", "-m", "ldd")
	w.join(t, w.lib)
	call := func(command string) support.Result {
		t.Helper()
		payload := map[string]any{"tool_name": "Bash", "tool_input": cmd(command), "cwd": w.app, "permission_mode": "default"}
		return hook(t, w.env, "route-guard", nil, payload)
	}
	// A change of the primary's with no member is judged as before.
	support.Git(t, w.app, "checkout", "-q", "-b", "feat/plain")
	support.Write(t, w.app, map[string]string{"openspec/changes/plain/tasks.md": "## Slice 1: core\n\n- [x] core task\n"})
	support.Git(t, w.app, "add", "-A")
	support.Git(t, w.app, "commit", "-q", "-m", "plain")
	code(t, call("gh pr create --title x --body 'nothing'"), 0, "a change with no member")

	support.Git(t, w.app, "checkout", "-q", "main")
	support.Git(t, w.app, "checkout", "-q", "-b", "feat/c")
	support.Write(t, w.app, map[string]string{"openspec/changes/c/tasks.md": "## Slice 1: lib side\n\nRepo: lib\n\n- [x] lib task\n"})
	support.Git(t, w.app, "add", "-A")
	support.Git(t, w.app, "commit", "-q", "-m", "c")
	support.Git(t, w.lib, "checkout", "-q", "-b", "feat/c")
	support.Git(t, w.lib, "remote", "add", "origin", "git@github.com:acme/lib.git")
	lead := "^route-guard: the change c delivers in members of the workspace too, one PR per repository, the primary's last: "

	// No run of the member's: not ready.
	r := call("gh pr create --title x --body 'see https://github.com/acme/lib/pull/7'")
	code(t, r, 2, "a primary PR whose member has no run")
	matches(t, r.Stderr, lead+"member lib is not ready \\(no recorded test run: run jhon executions run c --repo lib .*\\): run `jhon deliver check c` in "+w.lib+", open its PR, then the primary's\\.", "names the member and the command")

	// The member's passing run of its HEAD: ready; the body names its PR.
	tree := strings.TrimSpace(support.Git(t, w.lib, "rev-parse", "HEAD^{tree}"))
	support.Write(t, w.app, map[string]string{"openspec/changes/c/execution-record.json": `{"repos":{"lib":{"working_tree":"` + tree + `","after_working_tree":"` + tree +
		`","executions":[{"project":"src","command":"true","cwd":"src","exit_code":0,"status":"pass","duration_ms":1}],"passed":true,"complete":true}}}` + "\n"})
	r = call("gh pr create --title x --body 'nothing'")
	code(t, r, 2, "a primary PR naming no member PR")
	matches(t, r.Stderr, lead+"this PR's body names no pull request of member lib: open its PR first and name it, https://github\\.com/acme/lib/pull/<n>, in this PR's body\\.", "names the URL form")
	code(t, call("gh pr create --title x --body 'see https://github.com/acme/other/pull/7'"), 2, "another repository's PR")
	code(t, call("gh pr create --title x --body 'see https://github.com/acme/lib/pull/7'"), 0, "the body names the member's PR")
	code(t, call("gh pr create --title x --body \"$(cat <<'EOF'\nPart of github.com/acme/lib/pull/12\nEOF\n)\""), 0, "a heredoc body")
	body := filepath.Join(filepath.Dir(w.app), "tmp", "body.md")
	support.Write(t, filepath.Dir(body), map[string]string{"body.md": "Part of https://github.com/acme/lib/pull/3\n"})
	code(t, call("gh pr create --title x --body-file "+body), 0, "a body file")
	code(t, call("gh pr merge 3 --squash"), 0, "a merge is not judged by members")

	// An origin that names no hosted repository: its PR cannot be named.
	support.Git(t, w.lib, "remote", "set-url", "origin", filepath.Join(w.other, ".git"))
	r = call("gh pr create --title x --body 'see https://github.com/acme/lib/pull/7'")
	code(t, r, 2, "a member with no hosted origin")
	matches(t, r.Stderr, lead+"member lib has no origin remote naming its repository on a host \\(`git -C "+w.lib+" remote get-url origin`\\)", "names the remote")
}
