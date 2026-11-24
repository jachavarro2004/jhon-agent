// T57 slice 7 (L1324, D3): one PR per repository, the primary's last. The
// primary's `gh pr create` for a change with workspace members opens only
// when every member's delivery is ready and the PR's body names, for each
// one, a pull request of that member's origin; a change with no member,
// and a member's own PR, are judged as before.

package guards

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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

// D3 (L1334): an archived change's members hold the primary's PR as an
// active one's do; a member's run archived with the change counts.
func TestRouteGuardPrimaryPRWaitsForMembersOfAnArchivedChange(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t, "openspec/config.yaml")
	support.Write(t, w.app, map[string]string{"openspec/config.yaml": "workspace:\n  lib: ../lib\n"})
	support.Git(t, w.app, "add", "-A")
	support.Git(t, w.app, "commit", "-q", "-m", "ldd")
	w.join(t, w.lib)
	support.Git(t, w.app, "checkout", "-q", "-b", "feat/c")
	archived := "openspec/changes/archive/2026-11-18-c/"
	support.Write(t, w.app, map[string]string{archived + "tasks.md": "## Slice 1: lib side\n\nRepo: lib\n\n- [x] lib task\n"})
	support.Git(t, w.app, "add", "-A")
	support.Git(t, w.app, "commit", "-q", "-m", "c")
	support.Git(t, w.lib, "remote", "add", "origin", "git@github.com:acme/lib.git")
	call := func(command string) support.Result {
		t.Helper()
		return hook(t, w.env, "route-guard", nil, map[string]any{"tool_name": "Bash", "tool_input": cmd(command), "cwd": w.app, "permission_mode": "default"})
	}
	r := call("gh pr create --title c --body 'core work'")
	code(t, r, 2, "an archived change whose member never ran")
	matches(t, r.Stderr, "^route-guard: the change c \\(archived, "+archived+"\\) delivers in members of the workspace too, one PR per repository, the primary's last: member lib is not ready \\(no recorded test run: run jhon executions run c --repo lib \\(.*\\)\\): a member's run is recorded while the change is active: move "+archived+" back to openspec/changes/c/, run `jhon executions run c --repo lib`, open the member's PR, then the primary's\\.", "names the archive and the way back")
	if strings.Contains(r.Stderr, "jhon executions run archive/") {
		t.Errorf("the inner reason names the archived folder for the run: %q", r.Stderr)
	}

	tree := strings.TrimSpace(support.Git(t, w.lib, "rev-parse", "HEAD^{tree}"))
	support.Write(t, w.app, map[string]string{archived + "execution-record.json": `{"repos":{"lib":{"working_tree":"` + tree + `","after_working_tree":"` + tree +
		`","executions":[{"project":"src","command":"true","cwd":"src","exit_code":0,"status":"pass","duration_ms":1}],"passed":true,"complete":true}}}` + "\n"})
	code(t, call("gh pr create --title c --body 'core work'"), 2, "an archived change naming no member PR")
	code(t, call("gh pr create --title c --body 'see https://github.com/acme/lib/pull/7'"), 0, "the archived run is ready and its PR named")
}

// redGreenOnRecord puts the primary's RED before GREEN in the change's
// execution record file, keeping what it holds (a member's repos.<name>):
// `jhon archive` refuses a change without it (T223).
func redGreenOnRecord(t *testing.T, file string) {
	t.Helper()
	record := map[string]any{}
	if b, err := os.ReadFile(file); err == nil {
		if err := json.Unmarshal(b, &record); err != nil {
			t.Fatal(err)
		}
	}
	record["timestamp"] = "2026-11-18T10:01:00.000Z"
	record["complete"] = true
	record["scope"] = "full"
	record["executions"] = []any{map[string]any{"project": ".", "command": "true", "status": "pass", "exit_code": 0}}
	record["history"] = []any{map[string]any{"timestamp": "2026-11-18T10:00:00.000Z", "passed": false}}
	b, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The same through `jhon archive` in the ldd layout.
func TestRouteGuardPrimaryPRWaitsForMembersOfAChangeJhonArchived(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t, "ldd/config.yaml")
	support.Write(t, w.app, map[string]string{"ldd/config.yaml": "workspace:\n  lib: ../lib\n", "ldd/changes/.keep": ""})
	support.Git(t, w.app, "add", "-A")
	support.Git(t, w.app, "commit", "-q", "-m", "ldd")
	w.join(t, w.lib)
	support.Git(t, w.app, "checkout", "-q", "-b", "feat/c")
	support.Write(t, w.app, map[string]string{"ldd/changes/c/tasks.md": "## Slice 1: lib side\n\nRepo: lib\n\n- [x] lib task\n"})
	support.Git(t, w.app, "add", "-A")
	support.Git(t, w.app, "commit", "-q", "-m", "c")
	redGreenOnRecord(t, filepath.Join(w.app, "ldd/changes/c/execution-record.json"))
	r := support.Jhon(t, w.app, w.env, "archive", "c", "2026-11-18")
	code(t, r, 0, "jhon archive c")
	support.Git(t, w.app, "add", "-A")
	support.Git(t, w.app, "commit", "-q", "-m", "archive c")
	support.Git(t, w.lib, "remote", "add", "origin", "git@github.com:acme/lib.git")
	res := hook(t, w.env, "route-guard", nil, map[string]any{"tool_name": "Bash", "tool_input": cmd("gh pr create --title c --body 'core work'"), "cwd": w.app, "permission_mode": "default"})
	code(t, res, 2, "a change jhon archived whose member never ran")
	matches(t, res.Stderr, "^route-guard: the change c \\(archived, ldd/changes/archive/2026-11-18-c/\\) delivers in members of the workspace too, .*member lib is not ready", "names the member")
}

// L1346: a member that has not joined is named with the owner's join once,
// as the first step of the way to its PR, an archived change's way back
// included.
func TestRouteGuardPrimaryPRNamesAnUnjoinedMembersJoinOnce(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t, "openspec/config.yaml")
	support.Write(t, w.app, map[string]string{"openspec/config.yaml": "workspace:\n  lib: ../lib\n"})
	support.Git(t, w.app, "add", "-A")
	support.Git(t, w.app, "commit", "-q", "-m", "ldd")
	support.Git(t, w.app, "checkout", "-q", "-b", "feat/c")
	support.Write(t, w.app, map[string]string{"openspec/changes/c/tasks.md": "## Slice 1: lib side\n\nRepo: lib\n\n- [x] lib task\n"})
	support.Git(t, w.app, "add", "-A")
	support.Git(t, w.app, "commit", "-q", "-m", "c")
	support.Git(t, w.lib, "remote", "add", "origin", "git@github.com:acme/lib.git")
	call := func() support.Result {
		t.Helper()
		return hook(t, w.env, "route-guard", nil, map[string]any{"tool_name": "Bash", "tool_input": cmd("gh pr create --title c --body 'see https://github.com/acme/lib/pull/7'"), "cwd": w.app, "permission_mode": "default"})
	}
	unjoined := "member lib is not ready \\(" + regexp.QuoteMeta(w.lib) + " has not joined the workspace\\): the owner runs `jhon workspace join " + regexp.QuoteMeta(w.app) + "` in " + regexp.QuoteMeta(w.lib) + ", "
	once := func(r support.Result) {
		t.Helper()
		if n := strings.Count(r.Stderr, "has not joined"); n != 1 {
			t.Errorf("the join is named %d times, want once: %q", n, r.Stderr)
		}
		if n := strings.Count(r.Stderr, "jhon workspace join"); n != 1 {
			t.Errorf("the join command is named %d times, want once: %q", n, r.Stderr)
		}
	}

	r := call()
	code(t, r, 2, "an active change whose member has not joined")
	matches(t, r.Stderr, "^route-guard: the change c delivers in members of the workspace too, one PR per repository, the primary's last: "+unjoined+"open its PR, then the primary's\\.", "names the join as the step")
	once(r)

	archived := "openspec/changes/archive/2026-11-18-c/"
	if err := os.MkdirAll(filepath.Join(w.app, "openspec/changes/archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	support.Git(t, w.app, "mv", "openspec/changes/c", strings.TrimSuffix(archived, "/"))
	support.Git(t, w.app, "commit", "-q", "-m", "archive c")
	r = call()
	code(t, r, 2, "an archived change whose member has not joined")
	matches(t, r.Stderr, "^route-guard: the change c \\(archived, "+archived+"\\) delivers in members of the workspace too, one PR per repository, the primary's last: "+
		strings.Replace(unjoined, "\\): ", "\\): a member's run is recorded while the change is active: ", 1)+
		"move "+archived+" back to openspec/changes/c/, run `jhon executions run c --repo lib`, open the member's PR, then the primary's\\.", "the way back starts with the join")
	once(r)
}

// L1346 (verifier's regression): a member's run recorded while the change
// is active counts after `jhon archive`; the primary's PR opens only when
// it names the member's PR.
func TestRouteGuardMemberRunRecordedBeforeJhonArchiveCounts(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t, "ldd/config.yaml")
	support.Write(t, w.app, map[string]string{"ldd/config.yaml": "workspace:\n  lib: ../lib\n", "ldd/changes/.keep": ""})
	support.Git(t, w.app, "add", "-A")
	support.Git(t, w.app, "commit", "-q", "-m", "ldd")
	support.Write(t, w.lib, map[string]string{"a/go.mod": "module example.com/a\n", "a/a.go": "package a\n", "ldd/config.yaml": "test_command_override:\n  a: \"true\"\n"})
	support.Git(t, w.lib, "add", "-A")
	support.Git(t, w.lib, "commit", "-q", "-m", "lib")
	support.Git(t, w.lib, "remote", "add", "origin", "git@github.com:acme/lib.git")
	w.join(t, w.lib)
	support.Git(t, w.app, "checkout", "-q", "-b", "feat/c")
	support.Write(t, w.app, map[string]string{"ldd/changes/c/tasks.md": "## Slice 1: lib side\n\nRepo: lib\n\n- [x] lib task\n"})
	support.Git(t, w.app, "add", "-A")
	support.Git(t, w.app, "commit", "-q", "-m", "c")

	code(t, support.Jhon(t, w.app, w.env, "executions", "run", "c", "--repo", "lib"), 0, "the member's run while the change is active")
	redGreenOnRecord(t, filepath.Join(w.app, "ldd/changes/c/execution-record.json"))
	code(t, support.Jhon(t, w.app, w.env, "archive", "c", "2026-11-18"), 0, "jhon archive c")
	support.Git(t, w.app, "add", "-A")
	support.Git(t, w.app, "commit", "-q", "-m", "archive c")
	call := func(command string) support.Result {
		t.Helper()
		return hook(t, w.env, "route-guard", nil, map[string]any{"tool_name": "Bash", "tool_input": cmd(command), "cwd": w.app, "permission_mode": "default"})
	}
	r := call("gh pr create --title c --body 'core work'")
	code(t, r, 2, "the archived run counts, but the body names no member PR")
	matches(t, r.Stderr, "^route-guard: the change c \\(archived, ldd/changes/archive/2026-11-18-c/\\) delivers in members of the workspace too, one PR per repository, the primary's last: this PR's body names no pull request of member lib: open its PR first and name it, https://github\\.com/acme/lib/pull/<n>, in this PR's body\\.", "the member is ready, its PR unnamed")
	code(t, call("gh pr create --title c --body 'see https://github.com/acme/other/pull/7'"), 2, "another repository's PR")
	code(t, call("gh pr create --title c --body 'see https://github.com/acme/lib/pull/7'"), 0, "the body names the member's PR")
}
