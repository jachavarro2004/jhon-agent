// T281, the owner's decision on modes (one mode for the workspace): a
// member the primary's config lists, that joined and that has no mode of
// its own takes the primary's mode, from a session in the primary and in the
// member, and says so; a member with its own mode keeps it (both must pass,
// as before); a repository only sandbox.allow_write names, a sibling the
// primary does not list and a listed one that has not joined never take
// another's. No agent sets a mode (S10): the tests set them as the owner.

package guards

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// push is a top-level `git push` run in repo from a session in cwd.
func (w wsSet) push(t *testing.T, cwd, repo string) support.Result {
	t.Helper()
	return w.call(t, true, cwd, "Bash", cmd("cd "+repo+" && git push"))
}

// passedFrom is whether r passed with the owner's super-yolo note, naming
// from where the mode was taken ("" for the project's own).
func passedFrom(t *testing.T, r support.Result, from, label string) {
	t.Helper()
	code(t, r, 0, label)
	mode := "super-yolo mode"
	if from != "" {
		mode += ", from " + from
	}
	matches(t, modeNote(t, r.Stdout), "^"+regexp.QuoteMeta("note (not a refusal; the call ran): allowed by the owner's "+mode+": push: "), label)
}

// pushAsked is whether r asks the owner about the push.
func pushAsked(t *testing.T, r support.Result, label string) {
	t.Helper()
	code(t, r, 0, label)
	matches(t, askedWhy(t, r.Stdout, label), `^push: `, label)
}

func (w wsSet) mode(t *testing.T, dir, mode string) {
	t.Helper()
	code(t, support.Jhon(t, dir, w.env, "mode", mode), 0, "jhon mode "+mode+" in "+dir)
}

// 1: a listed, joined member with no mode takes the primary's, in a session
// of the primary and in its own, and `jhon mode` there says from where.
func TestT281AListedJoinedMemberTakesThePrimarysMode(t *testing.T) {
	t.Parallel()
	w := newWSSet(t)
	pushAsked(t, w.push(t, w.app, w.lib), "before any mode")
	w.mode(t, w.app, "super-yolo")
	passedFrom(t, w.push(t, w.app, w.lib), w.app, "the member from the primary")
	passedFrom(t, w.push(t, w.lib, w.lib), w.app, "the member in its own session")
	passedFrom(t, w.push(t, w.app, w.app), "", "the primary itself")
	r := support.Jhon(t, w.lib, w.env, "mode")
	code(t, r, 0, "jhon mode in the member")
	matches(t, r.Stdout, "^"+regexp.QuoteMeta("super-yolo for "+w.lib+", from "+w.app+" since ")+`\S+`+regexp.QuoteMeta(": a member of its workspace with no mode of its own takes the primary's (`jhon mode <mode>` here gives it one of its own)")+"\n$", "jhon mode")
	r = support.Jhon(t, w.lib, w.env, "mode", "--json")
	matches(t, r.Stdout, `^\{"mode":"super-yolo","root":"`+regexp.QuoteMeta(w.lib)+`","since":"[^"]+","from":"`+regexp.QuoteMeta(w.app)+`"\}`, "jhon mode --json")
	// The modes' log names the project the mode is the record of.
	if log := modeLog(t, w.state); !strings.Contains(log, "super-yolo push "+w.app) {
		t.Errorf("the modes' log: %q", log)
	}
}

// 2: a member with its own mode keeps it: both must pass, as before.
func TestT281AMemberWithItsOwnModeKeepsIt(t *testing.T) {
	t.Parallel()
	w := newWSSet(t)
	w.mode(t, w.app, "super-yolo")
	w.mode(t, w.lib, "normal")
	pushAsked(t, w.push(t, w.app, w.lib), "a member set normal, from the primary")
	pushAsked(t, w.push(t, w.lib, w.lib), "a member set normal, in its own session")
	r := support.Jhon(t, w.lib, w.env, "mode")
	matches(t, r.Stdout, "^"+regexp.QuoteMeta("normal for "+w.lib+" since "), "jhon mode in a member with its own")
	w.mode(t, w.lib, "super-yolo")
	passedFrom(t, w.push(t, w.app, w.lib), "", "a member set super-yolo")
}

// 3, 4, 5: a repository only sandbox.allow_write names, a sibling the
// primary does not list, and a listed member that has not joined take no
// other mode.
func TestT281OnlyListedJoinedMembersTakeThePrimarysMode(t *testing.T) {
	t.Parallel()
	w := newWSSet(t)
	w.mode(t, w.app, "super-yolo")
	for _, c := range []struct{ repo, label string }{
		{w.shared, "an allow_write repository"}, {w.site, "a joined sibling the primary does not list"}, {w.web, "a listed member that has not joined"},
	} {
		pushAsked(t, w.push(t, w.app, c.repo), c.label)
		r := support.Jhon(t, c.repo, w.env, "mode")
		matches(t, r.Stdout, "^"+regexp.QuoteMeta("normal for "+c.repo)+"\n$", c.label+": jhon mode")
	}
}

// 6: the session's own worktree takes the session's mode, as before, with
// no "from".
func TestT281TheSessionsWorktreeTakesItsModeAsBefore(t *testing.T) {
	t.Parallel()
	w := newWSSet(t)
	support.Git(t, w.app, "commit", "-q", "--allow-empty", "-m", "base")
	wt := filepath.Join(w.app+"-worktrees", "wt")
	support.Git(t, w.app, "worktree", "add", "-q", "-b", "wt", wt)
	w.mode(t, w.app, "super-yolo")
	passedFrom(t, w.push(t, w.app, wt), "", "the primary's worktree")
}

// A project with no workspace: its own mode, and another repository needs
// both, as before.
func TestT281AProjectWithNoWorkspaceKeepsItsModesAsBefore(t *testing.T) {
	t.Parallel()
	w := newWSSet(t)
	if err := os.WriteFile(filepath.Join(w.scratch, "settings.toml"), []byte("[sandbox]\nmode = \"off\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.mode(t, w.other, "super-yolo")
	passedFrom(t, w.push(t, w.other, w.other), "", "its own push")
	pushAsked(t, w.push(t, w.other, w.shared), "a normal repository from a super-yolo session")
	w.mode(t, w.app, "super-yolo")
	// A session outside the workspace lends the member nothing, nor does
	// the primary through it: the member's own normal asks, as before.
	pushAsked(t, w.push(t, w.other, w.lib), "a member from a super-yolo session outside its workspace, its primary super-yolo")
}
