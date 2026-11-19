// T57 slice 5 (L1314): in a multi-repository workspace the primary and the
// members the owner joined are one project for the guards that judge
// "outside the project" (S1), and route-guard judges a member's PR with the
// primary's record, the change its branch names (S2). A listed sibling that
// has not joined, and any other repository, stay outside as before: a
// write there runs with a note (T153).

package guards

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// wsFixture is a scratch workspace: app, the primary, lists lib and web;
// other is listed nowhere. Nobody joined yet.
type wsFixture struct {
	app, lib, web, other string
	env                  []string
}

func newWorkspace(t *testing.T, config string) wsFixture {
	t.Helper()
	// Outside /tmp and $TMPDIR: the guards take any write there for scratch
	// (project.TmpRoots), so a sibling there would never be outside.
	scratch := outsideTemp(t)
	w := wsFixture{app: filepath.Join(scratch, "app"), lib: filepath.Join(scratch, "lib"), web: filepath.Join(scratch, "web"), other: filepath.Join(scratch, "other")}
	w.env = support.Env("HOME="+sgHome, "XDG_STATE_HOME="+filepath.Join(scratch, "state"), "TMPDIR="+mkdir(t, filepath.Join(scratch, "tmp")), "JHON_ROOT="+support.Root())
	for _, d := range []string{w.app, w.lib, w.web, w.other} {
		support.Write(t, d, map[string]string{"src/a.py": "X = 1\n"})
		support.GitRepo(t, d)
		support.Git(t, d, "add", "-A")
		support.Git(t, d, "commit", "-q", "-m", "base")
	}
	support.Write(t, w.app, map[string]string{config: "workspace:\n  lib: ../lib\n  web: ../web\n"})
	return w
}

// join is the owner's `jhon workspace join` of dir into app's workspace.
func (w wsFixture) join(t *testing.T, dir string) {
	t.Helper()
	code(t, support.Jhon(t, dir, w.env, "workspace", "join", w.app), 0, "jhon workspace join in "+dir)
}

// guard is one sub-agent call through the plugin's pre-tool, made in cwd.
func (w wsFixture) guard(t *testing.T, cwd, tool string, input map[string]any) support.Result {
	t.Helper()
	payload := map[string]any{"hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": input, "cwd": cwd, "session_id": "s",
		"permission_mode": "bypassPermissions", "agent_id": "a1", "agent_type": "worker"}
	return hook(t, w.env, "pre-tool", []string{"--agent", "*", "safety-guard", "vcs-guard", ".git"}, payload)
}

// S1: a joined sibling's files are the project's, from the primary and
// from the member; a listed sibling that has not joined, and another
// repository, are outside: a sub-agent's Write or Edit there runs with the
// outside note (a refusal until T153).
func TestWorkspaceJoinedMembersAreTheProject(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t, "ldd/config.yaml")
	write := func(p string) map[string]any { return map[string]any{"file_path": p, "content": "X = 2\n"} }
	writes := func(cwd, target string) []struct {
		tool  string
		input map[string]any
	} {
		return []struct {
			tool  string
			input map[string]any
		}{
			{"Write", write(filepath.Join(target, "src", "b.py"))},
			{"Edit", map[string]any{"file_path": filepath.Join(target, "src", "a.py"), "old_string": "1", "new_string": "2"}},
			{"Bash", cmd("echo 'X = 2' > " + filepath.Join(target, "src", "a.py"))},
			{"Bash", cmd("sed -i s/1/2/ " + filepath.Join(target, "src", "a.py"))},
		}
	}
	// Outside: Write and Edit run with the outside note; a shell write there
	// is the sandbox's (T128f slice 5): the guards let it through and the
	// sandbox keeps the folder read-only (test/sandbox/workspace_test.go).
	outside := func(cwd, target, label string) {
		t.Helper()
		for _, c := range writes(cwd, target) {
			r := w.guard(t, cwd, c.tool, c.input)
			code(t, r, 0, label+": "+c.tool+" "+js(t, c.input))
			if c.tool != "Bash" {
				matches(t, adviceOf(t, r.Stdout), outsideNote, label+": "+c.tool)
			}
		}
	}
	inside := func(cwd, target, label string) {
		t.Helper()
		for _, c := range writes(cwd, target) {
			r := w.guard(t, cwd, c.tool, c.input)
			code(t, r, 0, label+": "+c.tool+" "+js(t, c.input))
			noDecision(t, r.Stdout, label)
			if note := adviceOf(t, r.Stdout); note != "" {
				t.Errorf("%s: %s inside the project has a note: %q", label, c.tool, note)
			}
		}
	}
	// Nobody joined: every sibling is outside, as outside a workspace.
	outside(w.app, w.lib, "lib before it joined")
	outside(w.lib, w.app, "app from lib before it joined")

	w.join(t, w.lib)
	inside(w.app, w.lib, "lib, joined, from the primary")
	inside(w.lib, w.app, "the primary from lib")
	inside(w.lib, w.lib, "lib itself")
	// Fail closed: web is listed and has not joined; other is listed nowhere.
	outside(w.app, w.web, "web, listed, not joined")
	outside(w.lib, w.web, "web from lib")
	outside(w.app, w.other, "a repository outside the workspace")
	outside(w.lib, w.other, "another repository from lib")
	// The member's root stays the project's own: removing it is refused.
	code(t, w.guard(t, w.app, "Bash", cmd("rm -rf "+w.lib)), 2, "rm -rf of the member's root")

	// other joins, but app does not list it: no member, still outside.
	w.join(t, w.other)
	outside(w.app, w.other, "a joined repository the primary does not list")
	outside(w.other, w.app, "the primary from a repository it does not list")

	// lib leaves: outside again.
	code(t, support.Jhon(t, w.lib, w.env, "workspace", "leave"), 0, "jhon workspace leave")
	outside(w.app, w.lib, "lib after it left")
}

// S2: route-guard on a joined member takes the primary's record: the change
// the member's branch names, its RED before GREEN and its light verify; the
// member needs no LDD layout of its own. A listed sibling that has not
// joined still delivers nothing until LDD is set up, and its code edit runs
// with the set-up note (T153).
func TestRouteGuardMemberPRUsesThePrimarysRecord(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t, "openspec/config.yaml")
	mkdir(t, filepath.Join(w.app, "openspec", "changes"))
	support.Git(t, w.app, "add", "-A")
	support.Git(t, w.app, "commit", "-q", "-m", "ldd")
	for _, d := range []string{w.app, w.lib, w.web} {
		support.Git(t, d, "checkout", "-q", "-b", "feat/c1")
	}
	call := func(cwd, tool string, input map[string]any) support.Result {
		t.Helper()
		payload := map[string]any{"tool_name": tool, "tool_input": input, "cwd": cwd, "permission_mode": "default"}
		return hook(t, w.env, "route-guard", nil, payload)
	}
	pr := cmd("gh pr create --title x --body y")
	edit := map[string]any{"file_path": filepath.Join(w.lib, "src", "a.py"), "content": "X = 2\n"}
	setUp := "^route-guard: .*LDD"
	// Not joined: lib is a repository with no layout, as today.
	noted := call(w.lib, "Write", edit)
	code(t, noted, 0, "a code edit in lib before it joined")
	if got := adviceOf(t, noted.Stdout); got != setUpNote() {
		t.Errorf("a code edit in lib before it joined: note %q", got)
	}
	matches(t, call(w.lib, "Bash", cmd("git commit -qam x")).Stderr, setUp, "a commit in lib before it joined")
	matches(t, call(w.lib, "Bash", pr).Stderr, setUp, "a PR in lib before it joined")

	w.join(t, w.lib)
	w.join(t, w.web)
	support.Write(t, w.app, map[string]string{"openspec/config.yaml": "workspace:\n  lib: ../lib\n"})
	code(t, call(w.lib, "Write", edit), 0, "a code edit in a joined member")
	code(t, call(w.lib, "Bash", cmd("git commit -qam x")), 0, "a commit in a joined member")
	support.Write(t, w.lib, map[string]string{"src/a.py": "X = 2\n", "tests/test_a.py": "def test_x(): pass\n"})
	res := call(w.lib, "Bash", pr)
	code(t, res, 2, "a member's PR with no record in the primary")
	lead := "^route-guard: this change alters behaviour in " + w.lib + ", a member of the workspace of " + w.app + ", and the primary's change feat-c1, which the member's branch names, "
	matches(t, res.Stderr, lead+"has no RED before GREEN on record \\(LDD, contracts/ldd.md, TDD always\\): run `jhon executions run feat-c1 --affected <base>`", "the refusal names the primary's change")
	rec := ".git/jhon/changes/feat-c1/execution-record.json"
	support.Write(t, w.app, map[string]string{rec: redGreenUnverified("")})
	res = call(w.lib, "Bash", pr)
	code(t, res, 0, "a member's PR with no light verify in the primary runs, with a note (T154)")
	if got := adviceOf(t, res.Stdout); got != lightVerifyNote("feat-c1") {
		t.Errorf("a member's PR with no light verify: note %q; want %q", got, lightVerifyNote("feat-c1"))
	}
	support.Write(t, w.app, map[string]string{rec: redGreenRecord})
	code(t, call(w.lib, "Bash", pr), 0, "the primary's record judges the member's PR")
	code(t, call(w.app, "Bash", cmd("cd "+w.lib+" && gh pr create --title x --body y")), 0, "the member's PR from the primary's session")
	// The record is the change the branch names: another branch has none.
	support.Git(t, w.lib, "checkout", "-q", "-b", "feat/c2")
	matches(t, call(w.lib, "Bash", pr).Stderr, "the primary's change feat-c2, which the member's branch names, has no RED before GREEN", "another branch names another change")

	// web joined, but the primary no longer lists it: no member, fail closed.
	support.Write(t, w.web, map[string]string{"src/a.py": "X = 2\n"})
	matches(t, call(w.web, "Bash", pr).Stderr, setUp, "a PR in an unlisted sibling")
	noted = call(w.web, "Write", map[string]any{"file_path": filepath.Join(w.web, "src", "b.py"), "content": "x"})
	code(t, noted, 0, "a code edit in an unlisted sibling")
	if got := adviceOf(t, noted.Stdout); got != setUpNote() {
		t.Errorf("a code edit in an unlisted sibling: note %q", got)
	}
	if strings.Contains(call(w.web, "Bash", pr).Stderr, "member") {
		t.Error("an unlisted sibling is judged as a member")
	}
}

// S1 (L1323): a config entry that reaches a folder through a symlink, a
// `..` or the parent folder is no member: writes there stay outside, from
// the primary and from a joined member, and run with the outside note.
func TestWorkspaceConfigPathsThroughLinksStayOutside(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t, "ldd/config.yaml")
	parent := filepath.Dir(w.app)
	symlink(t, w.other, filepath.Join(parent, "lnk"))
	support.Write(t, w.app, map[string]string{"ldd/config.yaml": "workspace:\n  lib: ../lib\n  sl: ../lnk\n  dd: ../lib/../other\n  up: ..\n"})
	w.join(t, w.lib)
	for _, cwd := range []string{w.app, w.lib} {
		for _, target := range []string{filepath.Join(w.other, "src", "b.py"), filepath.Join(parent, "lnk", "src", "b.py"), filepath.Join(parent, "top.txt")} {
			r := w.guard(t, cwd, "Write", map[string]any{"file_path": target, "content": "X = 2\n"})
			code(t, r, 0, "from "+cwd+": Write "+target)
			matches(t, adviceOf(t, r.Stdout), outsideNote, "from "+cwd+": Write "+target)
		}
	}
}
