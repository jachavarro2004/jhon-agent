package hook

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/workspace"
)

// T268 (L1762): route-guard judges a primary's PR members within its own
// time, memberCheckTime, well under the shell's 10 s; when the check runs
// out of it, the owner is asked at the top level, and a sub-agent's PR is
// refused naming `jhon deliver check` in the member, never a silent pass
// nor a timeout.

// lateWorkspace is a primary, app, on branch feat/c whose change c has a
// slice in its joined member lib, which has no run yet.
func lateWorkspace(t *testing.T) (app, lib string) {
	t.Helper()
	base := launchTemp(t)
	app, lib = filepath.Join(base, "app"), filepath.Join(base, "lib")
	for _, d := range []string{app, lib} {
		launchWrite(t, filepath.Join(d, "src", "a.py"), "X = 1\n")
		launchGit(t, base, "init", "-q", d)
		launchGit(t, d, "add", "-A")
		launchGit(t, d, "commit", "-q", "-m", "base")
		launchGit(t, d, "checkout", "-q", "-b", "feat/c")
	}
	launchWrite(t, filepath.Join(app, "openspec", "config.yaml"), "workspace:\n  lib: ../lib\n")
	launchWrite(t, filepath.Join(app, "openspec", "changes", "c", "tasks.md"), "## Slice 1: lib side\n\nRepo: lib\n\n- [x] lib task\n")
	launchGit(t, app, "add", "-A")
	launchGit(t, app, "commit", "-q", "-m", "c")
	launchGit(t, lib, "remote", "add", "origin", "git@github.com:acme/lib.git")
	if err := workspace.Join(lib, app); err != nil {
		t.Fatal(err)
	}
	return app, lib
}

func routeGuardPR(t *testing.T, app string, extra map[string]any) (code int, stdout, stderr string) {
	t.Helper()
	payload := map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash", "cwd": app,
		"tool_input": map[string]any{"command": "gh pr create --title x --body 'see https://github.com/acme/lib/pull/7'"}}
	for k, v := range extra {
		payload[k] = v
	}
	data, _ := json.Marshal(payload)
	var out, errw bytes.Buffer
	code = RouteGuard([]string{"--agent", "*"}, IO{bytes.NewReader(data), &out, &errw})
	return code, out.String(), errw.String()
}

func TestRouteGuardMembersCheckOutOfTimeAsksOrRefusesWithTheCheck(t *testing.T) {
	app, lib := lateWorkspace(t)
	defer func(d time.Duration) { memberCheckTime = d }(memberCheckTime)
	lead := "the change c delivers in members of the workspace too, one PR per repository, the primary's last: "

	// Within its time the member is judged, as before: it has no run.
	code, stdout, stderr := routeGuardPR(t, app, map[string]any{"agent_id": "a1"})
	if code != 2 || stdout != "" || !strings.HasPrefix(stderr, "route-guard: "+lead+"member lib is not ready (no recorded test run: run jhon executions run c --repo lib ") {
		t.Fatalf("judged in time: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}

	memberCheckTime = 0
	late := lead + "the delivery check of member lib did not finish in 0s, so this PR is not judged: run `jhon deliver check c` in " + lib +
		", which says what holds it (most often a large untracked folder git does not ignore: build output, or a tool's data, which `jhon doctor` names); open the PR once it is ready. Do not retry the PR before that: it only waits again."
	// A sub-agent, which nobody can ask: refused, with the check to run.
	code, stdout, stderr = routeGuardPR(t, app, map[string]any{"agent_id": "a1"})
	if code != 2 || stdout != "" || stderr != "route-guard: "+late+"\n" {
		t.Errorf("out of time in a sub-agent: exit %d, stdout %q, stderr:\n%s\nwant:\nroute-guard: %s", code, stdout, stderr, late)
	}
	// The top level: the owner is asked, with the same words.
	code, stdout, stderr = routeGuardPR(t, app, map[string]any{"permission_mode": "default"})
	var answer struct {
		Out struct {
			Decision string `json:"permissionDecision"`
			Reason   string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(stdout), &answer); code != 0 || stderr != "" || err != nil || answer.Out.Decision != "ask" || answer.Out.Reason != late {
		t.Errorf("out of time at the top level: exit %d, stderr %q, stdout %q", code, stderr, stdout)
	}
}
