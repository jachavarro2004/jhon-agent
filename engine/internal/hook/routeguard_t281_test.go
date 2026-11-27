package hook

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// T281 (S12): a gh line given its repository (-R, --repo) is judged in the
// repository it runs in, with that repository's change, when that one is of
// the session's working set and one of its remotes is the repository named;
// otherwise at the session's root, as before. Measured: six hook timeouts
// were such a line in another worktree running the primary's members check,
// and five refusals judged it by the primary's change.

// worktreeOfLate is lateWorkspace with a linked worktree of app, on branch
// fix/w from main with its own change w (no member), app's remote acme/app, and a
// repository beside them, declared nowhere, with the same remote.
func worktreeOfLate(t *testing.T) (app, wt, stray string) {
	t.Helper()
	app, _ = lateWorkspace(t)
	launchGit(t, app, "remote", "add", "origin", "git@github.com:acme/app.git")
	wt = filepath.Join(app+"-worktrees", "w")
	launchGit(t, app, "worktree", "add", "-q", "-b", "fix/w", wt, "main")
	launchWrite(t, filepath.Join(wt, "openspec", "changes", "w", "tasks.md"), "## Slice 1: w\n\n- [x] w task\n")
	launchGit(t, wt, "add", "-A")
	launchGit(t, wt, "commit", "-q", "-m", "w")
	stray = filepath.Join(filepath.Dir(app), "stray")
	launchGit(t, filepath.Dir(app), "init", "-q", stray)
	launchGit(t, stray, "remote", "add", "origin", "https://github.com/acme/app.git")
	return app, wt, stray
}

func TestRouteGuardPlacesAGhLineGivenItsRepositoryWhereItRuns(t *testing.T) {
	app, wt, stray := worktreeOfLate(t)
	// The slow dependency: the members check gets no time at all, so a line
	// that still runs it says so.
	defer func(d time.Duration) { memberCheckTime = d }(memberCheckTime)
	memberCheckTime = 0
	const late = "did not finish in 0s"
	pr := func(dir, repo string) string {
		return "cd " + dir + " && gh pr create " + repo + " --head fix/w --title x --body y"
	}
	for _, repo := range []string{"--repo acme/app", "-R acme/app", "--repo=github.com/ACME/app", "-Rgit@github.com:acme/app.git"} {
		_, _, stderr := routeGuardPR(t, app, map[string]any{"agent_id": "a1", "tool_input": map[string]any{"command": pr(wt, repo)}})
		if strings.Contains(stderr, late) || strings.Contains(stderr, "the change c ") || strings.Contains(stderr, "member lib") {
			t.Errorf("%s: judged by the primary's change and members:\n%s", repo, stderr)
		}
	}
	// Judged at the session's root, as before: the repository named is none
	// of the worktree's remotes, the line runs in a repository outside the
	// working set, or gh's repository comes from GH_REPO.
	for _, line := range []string{pr(wt, "--repo acme/other"), pr(stray, "--repo acme/app"), "cd " + wt + " && GH_REPO=acme/app gh pr create --head fix/w --title x --body y"} {
		code, _, stderr := routeGuardPR(t, app, map[string]any{"agent_id": "a1", "tool_input": map[string]any{"command": line}})
		if code != 2 || !strings.Contains(stderr, late) {
			t.Errorf("%s: exit %d, want the primary's members check as before:\n%s", line, code, stderr)
		}
	}
}

func TestGhRepoAndOwnerRepo(t *testing.T) {
	for in, want := range map[string]string{
		"acme/app": "acme/app", "github.com/Acme/App": "acme/app", "git@github.com:acme/app.git": "acme/app",
		"https://github.com/acme/app.git/": "acme/app", "ssh://git@github.com/acme/app": "acme/app", "app": "", "": "",
	} {
		if got := ownerRepo(in); got != want {
			t.Errorf("ownerRepo(%q) = %q, want %q", in, got, want)
		}
	}
}
