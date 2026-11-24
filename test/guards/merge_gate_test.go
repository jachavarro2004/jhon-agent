// route-guard holds a merge into the default branch to the delivery check
// (T219): a command that moves the default branch (main, master, or the
// one origin/HEAD names) to another commit runs only when a ready
// `jhon deliver check <change> --action merge` on record covers that commit,
// the exact one or one whose tree differs only in the change's record. The
// line is judged before it runs, so a check on the same line never counts.
// Updating the default branch from its own remote, and moving any other
// branch, stay allowed.

package guards

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

const (
	mgBranch = "fix/verify-advisories"
	mgChange = "fix-verify-advisories"
)

// mgEnv is a project under LDD on branch mgBranch, whose change mgChange
// edits code and its test; main stays at the base commit and is checked
// out nowhere, as in the owner's session.
type mgEnv struct {
	p, scratch string
	env        []string
}

func newMG(t *testing.T) mgEnv {
	t.Helper()
	scratch := realTemp(t)
	m := mgEnv{p: filepath.Join(scratch, "proj"), scratch: scratch}
	support.Write(t, m.p, map[string]string{
		"openspec/config.yaml": "test_command_override:\n  a: \"true\"\n",
		"a/go.mod":             "module example.com/a\n",
		"a/a.go":               "package a\n",
	})
	support.GitRepo(t, m.p)
	m.commit(t, "base")
	support.Git(t, m.p, "checkout", "-q", "-b", mgBranch)
	support.Write(t, m.p, map[string]string{
		"openspec/changes/" + mgChange + "/tasks.md": "- [x] fix\n",
		"a/a.go":      "package a\n\nconst X = 1\n",
		"a/a_test.go": "package a\n",
	})
	m.commit(t, "change")
	m.env = support.Env("HOME=/home/jhon-merge-test", "XDG_STATE_HOME="+filepath.Join(scratch, "state"), "JHON_ROOT="+support.Root(), "GOCACHE="+goCache(t))
	return m
}

func (m mgEnv) commit(t *testing.T, message string) {
	t.Helper()
	support.Git(t, m.p, "add", "-A")
	support.Git(t, m.p, "commit", "-q", "--allow-empty", "-m", message)
}

func (m mgEnv) rev(t *testing.T, rev string) string {
	t.Helper()
	return strings.TrimSpace(support.Git(t, m.p, "rev-parse", rev))
}

// tested records a passing run of the change and commits it, as a change
// does before its check.
func (m mgEnv) tested(t *testing.T) mgEnv {
	t.Helper()
	r := support.Jhon(t, m.p, m.env, "executions", "run", mgChange, "--json")
	code(t, r, 0, "jhon executions run")
	m.commit(t, "record")
	return m
}

// check runs the merge check on its own line and wants its exit.
func (m mgEnv) check(t *testing.T, want int) mgEnv {
	t.Helper()
	code(t, support.Jhon(t, m.p, m.env, "deliver", "check", mgChange, "--action", "merge"), want, "jhon deliver check --action merge")
	return m
}

// record is where a ready check of the change records itself: the git
// common dir's jhon/changes/<change>/delivery.json (T225).
func (m mgEnv) record() string {
	return filepath.Join(m.p, ".git", "jhon", "changes", mgChange, "delivery.json")
}

// ready is a change whose branch tip a ready merge check covers.
func readyMG(t *testing.T) mgEnv { return newMG(t).tested(t).check(t, 0) }

// notReadyMG is a change whose last check, after a new commit, is not
// ready: the ready record it left names the commit before.
func notReadyMG(t *testing.T) mgEnv {
	m := readyMG(t)
	support.Write(t, m.p, map[string]string{"a/a.go": "package a\n\nconst X = 2\n"})
	m.commit(t, "after the check")
	return m.check(t, 1)
}

// mainWorktree is a linked worktree checked out on main.
func (m mgEnv) mainWorktree(t *testing.T) string {
	t.Helper()
	wt := filepath.Join(m.scratch, "wtm")
	support.Git(t, m.p, "worktree", "add", "-q", wt, "main")
	return wt
}

// bash is the owner's Bash call of command from cwd, through route-guard.
func (m mgEnv) bash(t *testing.T, cwd, command string) support.Result {
	t.Helper()
	return hook(t, m.env, "route-guard", nil, map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": command},
		"cwd": cwd, "permission_mode": "default"})
}

// mgForms are the commands that move main to the branch's tip, each with
// the directory it runs from (p, the project; wtm, a worktree on main).
func mgForms(m mgEnv, wtm string) [][2]string {
	b := mgBranch
	return [][2]string{
		{m.p, "git fetch . " + b + ":main"},
		{m.p, "git fetch -q . " + b + ":main"},
		{m.p, "git fetch . +" + b + ":main"},
		{m.p, "git fetch . " + b + ":refs/heads/main"},
		{m.p, "git fetch -q -- . " + b + ":main"},
		{m.p, "git fetch . " + b + ":other " + b + ":main"},
		{m.p, "git fetch " + m.p + " " + b + ":main"},
		{m.p, "git fetch ../proj " + b + ":main"},
		{m.p, "git push . " + b + ":main"},
		{m.p, "git push . " + b + ":refs/heads/main"},
		{m.p, "git update-ref refs/heads/main " + b},
		{m.p, "git branch -f main " + b},
		{m.p, "git branch --force main " + b},
		{m.p, "git branch -M " + b + " main"},
		{m.p, "git branch -C " + b + " main"},
		{m.p, "git checkout -B main " + b},
		{m.p, "git switch -C main " + b},
		{m.p, "git checkout -q main && git merge " + b},
		{m.p, "git switch main; git merge --no-ff -m merged " + b},
		{m.p, "git checkout main && git pull . " + b},
		{wtm, "git merge " + b},
		{wtm, "git merge --ff-only " + b},
		{wtm, "git -C " + m.p + " fetch . " + b + ":main"},
		{wtm, "cd " + m.p + " && git fetch . " + b + ":main"},
		{m.p, "sh -c 'git fetch . " + b + ":main'"},
		{m.p, "env GIT_TRACE=0 git fetch . " + b + ":main"},
		{m.p, "B=" + b + "; git fetch . $B:main"},
		{m.p, "jhon sandbox run --project " + m.p + " --profile work -- sh -c 'git fetch -q . " + b + ":main'"},
		{m.p, "git -c alias.ff='fetch -q .' ff " + b + ":main"},
	}
}

// S1: every command that moves the default branch to another commit is a
// merge: refused with no check, refused when the newest check was not
// ready, and allowed after a ready check of the exact commit.
func TestMergeGateMovesOfTheDefaultBranch(t *testing.T) {
	t.Parallel()
	none := newMG(t).tested(t)
	notReady := notReadyMG(t)
	ready := readyMG(t)
	for _, c := range []struct {
		name string
		m    mgEnv
		want int
	}{{"no check", none, 2}, {"a check that was not ready", notReady, 2}, {"a ready check of the commit", ready, 0}} {
		wtm := c.m.mainWorktree(t)
		before := c.m.rev(t, "main")
		for _, f := range mgForms(c.m, wtm) {
			r := c.m.bash(t, f[0], f[1])
			code(t, r, c.want, c.name+": "+f[1])
			if c.want == 2 && !strings.Contains(r.Stderr, "`jhon deliver check ") {
				t.Errorf("%s: %s: the refusal names the check: %q", c.name, f[1], r.Stderr)
			}
		}
		equalStr(t, c.m.rev(t, "main"), before, c.name+": the guard moves nothing")
	}
}

// S1: identity is the tree: committing the change's record after a ready
// check leaves the commit it checked covered.
func TestMergeGateRecordCommittedAfterTheCheck(t *testing.T) {
	t.Parallel()
	m := readyMG(t)
	// The record an earlier jhon wrote in the change's folder, committed (T225 S3).
	support.Write(t, m.p, map[string]string{"openspec/changes/" + mgChange + "/delivery.json": support.Read(t, m.record())})
	m.commit(t, "the check's record")
	code(t, m.bash(t, m.p, "git fetch -q . "+mgBranch+":main"), 0, "the tip differs from the checked commit only in the record")
	support.Write(t, m.p, map[string]string{"a/a.go": "package a\n\nconst X = 3\n"})
	m.commit(t, "code after the check")
	code(t, m.bash(t, m.p, "git fetch -q . "+mgBranch+":main"), 2, "code changed after the check")
}

// S1: the repository's own default branch is the one origin/HEAD names.
func TestMergeGateOriginHeadNamesTheDefault(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	support.Git(t, m.p, "update-ref", "refs/remotes/origin/trunk", "main")
	support.Git(t, m.p, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
	code(t, m.bash(t, m.p, "git fetch . "+mgBranch+":trunk"), 2, "trunk is the default")
	code(t, m.bash(t, m.p, "git fetch . "+mgBranch+":main"), 2, "main always counts")
	code(t, m.bash(t, m.p, "git fetch . "+mgBranch+":other"), 0, "another branch")
}

// S1: a target the guard cannot read is refused, even after a ready check.
func TestMergeGateUnreadableTargetFailsClosed(t *testing.T) {
	t.Parallel()
	m := readyMG(t)
	support.Git(t, m.p, "remote", "add", "origin", filepath.Join(m.scratch, "origin.git"))
	for _, c := range []string{
		`git fetch . "` + mgBranch + `:$DEST"`,
		`git fetch . 'refs/heads/*:refs/heads/*'`,
		"git fetch origin " + mgBranch + ":main",
		"git fetch upstream " + mgBranch + ":main",
		`git update-ref refs/heads/main "$NEW"`,
		"git fetch . nosuchbranch:main",
		"git fetch --refmap='refs/heads/*:refs/heads/*' . " + mgBranch,
		`python3 -c "import os; os.system('git fetch -q . ` + mgBranch + `:main')"`,
		"git fetch -q --stdin . < specs.txt",
	} {
		r := m.bash(t, m.p, c)
		code(t, r, 2, c)
		matches(t, r.Stderr, "run `jhon deliver check [^`]+ --action merge` on its own line, fix what it reports, then merge", c)
	}
}

// S1: the owner's line, verbatim but for the change's name: the check is
// not ready, and main stays where it was.
func TestMergeGateOwnersLine(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	support.Write(t, m.p, map[string]string{"notes.txt": "uncommitted\n"})
	m.check(t, 1)
	before := m.rev(t, "main")
	line := "jhon deliver check " + mgChange + " --action merge 2>&1 | tail -6; git fetch -q . " + mgBranch + ":main"
	r := m.bash(t, m.p, line)
	code(t, r, 2, "the owner's line")
	if r.Code == 0 {
		// The harness runs what the guard allows.
		support.Run(t, support.Cmd{Args: []string{"bash", "-c", line}, Dir: m.p, Env: m.env})
	}
	equalStr(t, m.rev(t, "main"), before, "main stays where it was")
}

// S2: a check and a merge on the same line never count as checked.
func TestMergeGateCheckOnTheSameLine(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	record := m.record()
	check := "jhon deliver check " + mgChange + " --action merge"
	for _, c := range []string{
		check + "; git fetch -q . " + mgBranch + ":main",
		check + " && git fetch -q . " + mgBranch + ":main",
		check + " 2>&1 | tail -6; git fetch -q . " + mgBranch + ":main",
		check + " && git checkout main && git merge " + mgBranch,
	} {
		code(t, m.bash(t, m.p, c), 2, c)
	}
	if _, err := os.Stat(record); err == nil {
		t.Errorf("the guard ran no check: %s exists", record)
	}
	// The same check on its own line, then the merge, passes.
	m.check(t, 0)
	code(t, m.bash(t, m.p, "git fetch -q . "+mgBranch+":main"), 0, "the merge after a ready check on its own line")
}

// S3: what was allowed stays allowed: fetching a remote, moving or
// creating another branch, updating main from its own remote, merging into
// a branch that is not the default, `gh pr merge`, and a repository
// without LDD.
func TestMergeGateAllowedStaysAllowed(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	origin := filepath.Join(m.scratch, "origin.git")
	support.Git(t, m.scratch, "init", "-q", "--bare", origin)
	support.Git(t, m.p, "remote", "add", "origin", origin)
	// origin/main is one commit ahead of main, on the branch's base.
	up := strings.TrimSpace(support.Git(t, m.p, "commit-tree", "main^{tree}", "-p", "main", "-m", "upstream"))
	support.Git(t, m.p, "update-ref", "refs/remotes/origin/main", up)
	support.Git(t, m.p, "branch", "-q", "--set-upstream-to=origin/main", "main")
	wtm := m.mainWorktree(t)
	b := mgBranch
	for _, c := range [][2]string{
		{m.p, "git fetch origin"},
		{m.p, "git fetch origin main"},
		{m.p, "git fetch -q --prune origin"},
		{m.p, "git fetch origin main:main"},
		{m.p, "git fetch . " + b + ":other"},
		{m.p, "git branch other"},
		{m.p, "git branch -f other " + b},
		{m.p, "git branch -M " + b + " renamed"},
		{m.p, "git checkout -B other " + b},
		{m.p, "git switch -C other"},
		{m.p, "git checkout -b another"},
		{m.p, "git update-ref refs/heads/other " + b},
		{m.p, "git push . " + b + ":other"},
		{m.p, "git merge main"},
		{m.p, "git merge origin/main"},
		{m.p, "git pull --rebase origin main"},
		{wtm, "git pull"},
		{wtm, "git pull origin main"},
		{wtm, "git merge origin/main"},
		{wtm, "git merge @{u}"},
		{wtm, "git fetch origin main && git merge FETCH_HEAD"},
		{wtm, "git merge --abort"},
		{wtm, "git checkout " + b + " && git merge main"},
		{m.p, "gh pr merge 1 --squash"},
		{m.p, "git status && git log --oneline main.." + b},
	} {
		code(t, m.bash(t, c[0], c[1]), 0, c[1])
	}
	// A repository without LDD's layout: no change, no check, as before.
	plain := mkdir(t, filepath.Join(m.scratch, "plain"))
	support.GitRepo(t, plain)
	support.Git(t, plain, "checkout", "-q", "-b", "feat")
	code(t, m.bash(t, plain, "git fetch . feat:main"), 0, "a repository without LDD")
}

// S4: the refusal says what to do: the check on its own line, fix what it
// reports, then merge.
func TestMergeGateRefusalSaysWhatToDo(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	tip := m.rev(t, mgBranch)
	r := m.bash(t, m.p, "git fetch -q . "+mgBranch+":main")
	code(t, r, 2, "no check")
	equalStr(t, r.Stderr, "route-guard: `git fetch -q . "+mgBranch+":main` moves the default branch main to "+tip[:12]+
		", and no ready merge check of that commit is on record (a check on the same line never counts: the guard reads the line before it runs): run `jhon deliver check "+mgChange+
		" --action merge` on its own line, fix what it reports, then merge.\n", "the refusal")
	r = m.bash(t, m.p, "git fetch . 'refs/heads/*:refs/heads/*'")
	code(t, r, 2, "an unreadable target")
	equalStr(t, r.Stderr, "route-guard: `git fetch . refs/heads/*:refs/heads/*` may move the default branch main, and the guard cannot read to which commit: "+
		"its destination is a pattern. Name the branch and the commit plainly: run `jhon deliver check <change> --action merge` on its own line, fix what it reports, then merge.\n", "the unreadable refusal")
}

// withUpstream gives m an origin whose main is one commit ahead of main,
// main's upstream, and another remote whose main is the branch's tip; it
// returns a worktree on main.
func (m mgEnv) withUpstream(t *testing.T) string {
	t.Helper()
	origin := filepath.Join(m.scratch, "origin.git")
	support.Git(t, m.scratch, "init", "-q", "--bare", origin)
	support.Git(t, m.p, "remote", "add", "origin", origin)
	support.Git(t, m.p, "remote", "add", "other", filepath.Join(m.scratch, "other.git"))
	up := strings.TrimSpace(support.Git(t, m.p, "commit-tree", "main^{tree}", "-p", "main", "-m", "upstream"))
	support.Git(t, m.p, "update-ref", "refs/remotes/origin/main", up)
	support.Git(t, m.p, "update-ref", "refs/remotes/other/main", m.rev(t, mgBranch))
	support.Git(t, m.p, "branch", "-q", "--set-upstream-to=origin/main", "main")
	return m.mainWorktree(t)
}

// cases runs each [cwd, command] through route-guard and wants code.
func (m mgEnv) cases(t *testing.T, label string, want int, cases [][2]string) {
	t.Helper()
	for _, c := range cases {
		code(t, m.bash(t, c[0], c[1]), want, label+": "+c[1])
	}
}

// S1 (L5 B1): `update-ref --stdin` is judged by the lines it reads: a
// literal printf, echo, here-string or heredoc is read; input the guard
// cannot see is refused.
func TestMergeGateUpdateRefStdin(t *testing.T) {
	t.Parallel()
	b := mgBranch
	forms := func(m mgEnv) [][2]string {
		return [][2]string{
			{m.p, "printf 'update refs/heads/main " + b + "\\n' | git update-ref --stdin"},
			{m.p, "echo 'create refs/heads/main " + b + "' | git update-ref --stdin"},
			{m.p, "git update-ref --stdin <<< 'update refs/heads/main " + b + "'"},
			{m.p, "git update-ref --stdin <<'EOF'\nupdate refs/heads/main " + b + "\nEOF"},
		}
	}
	none := newMG(t).tested(t)
	none.cases(t, "no check", 2, forms(none))
	ready := readyMG(t)
	ready.cases(t, "a ready check", 0, forms(ready))
	ready.cases(t, "input the guard cannot read", 2, [][2]string{
		{ready.p, "git update-ref --stdin < specs.txt"},
		{ready.p, "cat specs.txt | git update-ref --stdin"},
		{ready.p, "echo \"update refs/heads/main $(git rev-parse " + b + ")\" | git update-ref --stdin"},
		{ready.p, "git update-ref --stdin"},
	})
	none.cases(t, "another branch", 0, [][2]string{
		{none.p, "printf 'update refs/heads/other " + b + "\\n' | git update-ref --stdin"},
	})
}

// S1 (L5 B2): FETCH_HEAD holds what the line's last fetch wrote: a fetch
// after the upstream's replaces it.
func TestMergeGateFetchHeadFollowsTheLastFetch(t *testing.T) {
	t.Parallel()
	b := mgBranch
	none := newMG(t).tested(t)
	wtm := none.withUpstream(t)
	none.cases(t, "no check", 2, [][2]string{
		{wtm, "git fetch origin main && git fetch -q . " + b + " && git merge FETCH_HEAD"},
		{wtm, "git fetch origin main; git fetch origin feature; git merge FETCH_HEAD"},
		{wtm, "git fetch other main && git merge FETCH_HEAD"},
	})
	ready := readyMG(t)
	wtr := ready.withUpstream(t)
	ready.cases(t, "a ready check", 0, [][2]string{
		{wtr, "git fetch origin main && git fetch -q . " + b + " && git merge FETCH_HEAD"},
	})
}

// S3 (L5 F1): a bare fetch on main fetches its upstream.
func TestMergeGateBareFetchThenFetchHead(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	wtm := m.withUpstream(t)
	m.cases(t, "the upstream", 0, [][2]string{
		{wtm, "git fetch && git merge FETCH_HEAD"},
		{wtm, "git fetch -q origin && git merge FETCH_HEAD"},
		{wtm, "git fetch origin main && git merge FETCH_HEAD"},
	})
}

// S1 (L5 B3): a refspec, revision or destination the guard cannot read is
// refused, even after a ready check.
func TestMergeGateSubstitutedRefspecFailsClosed(t *testing.T) {
	t.Parallel()
	m := readyMG(t)
	wtm := m.mainWorktree(t)
	b := mgBranch
	m.cases(t, "unreadable", 2, [][2]string{
		{m.p, "git fetch . $(echo " + b + ":main)"},
		{m.p, "git fetch . `echo " + b + ":main`"},
		{m.p, "git fetch . $SPEC"},
		{m.p, "git fetch $REPO " + b + ":main"},
		{m.p, "git push . $(echo " + b + ":main)"},
		{wtm, "git merge $(git rev-parse " + b + ")"},
		{wtm, "git pull . $(echo " + b + ")"},
		{wtm, "git pull $REMOTE"},
	})
}

// S1 (L5): every other command that moves main is judged the same way:
// reset, rebase, worktree add -B, symbolic-ref.
func TestMergeGateOtherMovesOfMain(t *testing.T) {
	t.Parallel()
	b := mgBranch
	forms := func(m mgEnv, wtm string) [][2]string {
		return [][2]string{
			{wtm, "git reset --" + "hard " + b},
			{wtm, "git reset --keep " + b},
			{wtm, "git reset --soft " + b},
			{wtm, "git reset " + b},
			{wtm, "git rebase " + b},
			{m.p, "git rebase " + b + " main"},
			{m.p, "git worktree add -B main ../wt2 " + b},
			{m.p, "git symbolic-ref refs/heads/main refs/heads/" + b},
		}
	}
	none := newMG(t).tested(t)
	none.cases(t, "no check", 2, forms(none, none.mainWorktree(t)))
	ready := readyMG(t)
	ready.cases(t, "a ready check", 0, forms(ready, ready.mainWorktree(t)))
}

// S3: a reset or a rebase that moves main nowhere new, a reset of paths,
// and the same commands on another branch stay allowed.
func TestMergeGateResetAndRebaseAllowed(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	wtm := m.withUpstream(t)
	hard := "--" + "hard"
	m.cases(t, "allowed", 0, [][2]string{
		{wtm, "git reset"},
		{wtm, "git reset " + hard},
		{wtm, "git reset " + hard + " HEAD"},
		{wtm, "git reset HEAD a/a.go"},
		{wtm, "git reset -- a/a.go"},
		{wtm, "git reset --keep HEAD~1"},
		{wtm, "git reset " + hard + " origin/main"},
		{wtm, "git rebase origin/main"},
		{wtm, "git rebase"},
		{wtm, "git rebase --continue"},
		{m.p, "git reset " + hard + " main"},
		{m.p, "git rebase main"},
		{m.p, "git worktree add ../w3 -b feat/w"},
		{m.p, "git symbolic-ref --short HEAD"},
	})
}

// S1 (L5): `git pull <remote> main` and `git fetch <remote> main:main`
// update main from its own upstream only when <remote> is main's.
func TestMergeGatePullOwnRemoteOnly(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	wtm := m.withUpstream(t)
	m.cases(t, "another remote", 2, [][2]string{
		{wtm, "git pull other main"},
		{m.p, "git fetch other main:main"},
	})
	m.cases(t, "main's remote", 0, [][2]string{
		{wtm, "git pull origin main"},
		{m.p, "git fetch origin main:main"},
	})
}

// S1 (L5): a record a commit carries is not a check: only the records in
// the worktrees and the git common dir count.
func TestMergeGateRecordInTheCommitIsNotTrusted(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	tip := m.rev(t, mgBranch)
	support.Git(t, m.p, "checkout", "-q", "-b", "forged")
	support.Write(t, m.p, map[string]string{"openspec/changes/" + mgChange + "/delivery.json": `{"action":"merge","artifact":null,"head_commit":"` + tip + `"}` + "\n"})
	m.commit(t, "a forged record")
	support.Git(t, m.p, "checkout", "-q", mgBranch)
	code(t, m.bash(t, m.p, "git fetch . forged:main"), 2, "a record only the commit carries")
}

// S3 (L7): with no branch.main.remote, origin is main's remote for a pull
// and a fetch too, as it is for `merge origin/main`.
func TestMergeGateOriginIsMainsRemoteWhenNoneIsSet(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	support.Git(t, m.p, "remote", "add", "origin", filepath.Join(m.scratch, "origin.git"))
	wtm := m.mainWorktree(t)
	m.cases(t, "origin", 0, [][2]string{
		{wtm, "git pull origin main"},
		{wtm, "git fetch origin main && git merge FETCH_HEAD"},
		{m.p, "git fetch origin main:main"},
	})
}
