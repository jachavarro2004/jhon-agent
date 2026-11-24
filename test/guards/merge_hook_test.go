// The merge gate in git itself (T219 S5-S8): in a repository under LDD git
// runs `jhon hook merge-gate` as a reference-transaction hook from the
// repository's local configuration, which route-guard writes on the first
// guarded command. Whatever a line does at run time, git refuses to move
// the default branch to a commit no ready merge check covers, with the
// instruction route-guard gives; after a ready check the same moves run,
// and the everyday flow (pull, fetch, merges and rebases on feature
// branches, reset to the upstream) is unchanged.

package guards

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// gateInstalled installs the merge gate as route-guard does on a guarded
// Bash call, and wants it in the repository's configuration.
func (m mgEnv) gateInstalled(t *testing.T) {
	t.Helper()
	code(t, m.bash(t, m.p, "git status"), 0, "a guarded call")
	if got := strings.TrimSpace(support.Git(t, m.p, "config", "--local", "--get", "hook.jhon-merge-gate.event")); got != "reference-transaction" {
		t.Fatalf("the merge gate's event: %q, want reference-transaction", got)
	}
}

// sh runs line for real, as the agent's shell would, in dir: its git
// carries the agent's marker (JHON_AGENT=1, S9).
func (m mgEnv) sh(t *testing.T, dir, line string, extra ...string) support.Result {
	t.Helper()
	return m.run(t, dir, line, append([]string{"JHON_AGENT=1"}, extra...)...)
}

// owner runs line for real as the owner's terminal would: no marker.
func (m mgEnv) owner(t *testing.T, dir, line string) support.Result {
	t.Helper()
	return m.run(t, dir, line)
}

func (m mgEnv) run(t *testing.T, dir, line string, extra ...string) support.Result {
	t.Helper()
	env := append(append([]string{}, m.env...), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid", "GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true")
	return support.Run(t, support.Cmd{Args: []string{"bash", "-c", line}, Dir: dir, Env: append(env, extra...)})
}

// refOr is rev's commit, or "" when it names none.
func (m mgEnv) refOr(t *testing.T, rev string) string {
	t.Helper()
	r := support.Run(t, support.Cmd{Args: []string{"git", "rev-parse", "--verify", "--quiet", rev}, Dir: m.p, Env: support.Env()})
	return strings.TrimSpace(r.Stdout)
}

const gateRefusal = "run `jhon deliver check "

// mgAway are moves of main made from the project, where main is checked
// out nowhere.
func mgAway(m mgEnv) []string {
	b := mgBranch
	inc := filepath.Join(m.scratch, "alias.inc")
	return []string{
		"git fetch -q . " + b + ":main",
		"git push -q . " + b + ":main",
		"git update-ref refs/heads/main " + b,
		"git branch -f main " + b,
		"git -c core.hooksPath=/dev/null fetch -q . " + b + ":main",
		"git -c alias.ff='fetch -q .' ff " + b + ":main",
		"git -c include.path=" + inc + " ff " + b + ":main",
		"GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=alias.ff GIT_CONFIG_VALUE_0='fetch -q .' git ff " + b + ":main",
		"git config remote.self.url . && git config remote.self.fetch '+refs/heads/" + b + ":refs/heads/main' && git fetch -q self",
		"git fetch -q . " + b + ":ma{in,x}",
		"X=mainx; git fetch -q . " + b + ":${X:0:4}",
		`printf 'update refs/heads/m\141in %s\n' "$(git rev-parse ` + b + `)" | git update-ref --stdin`,
		"git symbolic-ref refs/heads/main refs/heads/" + b,
		"git worktree add -q -B main ../wt2 " + b,
		"bash m.sh",
	}
}

// mgOnMain are moves of main made from a worktree on it.
func mgOnMain() []string {
	b := mgBranch
	return []string{
		"git merge -q " + b,
		"git checkout -q nosuchbranch; git merge -q " + b,
		"false || git merge -q " + b,
		"if true; then git merge -q --no-ff -m merged " + b + "; fi",
		"git fetch -q . " + b + " & wait; git merge -q FETCH_HEAD",
		"git -c alias.m=merge m " + b,
		"git pull -q . " + b,
		"git reset -q --" + "hard " + b,
		"git reset -q --soft " + b,
		"git rebase -q " + b,
		"git rebase -q -x true " + b,
		"git commit -q --allow-empty -m direct",
		"git branch -u " + b + " && git pull -q",
	}
}

func (m mgEnv) scripts(t *testing.T) {
	t.Helper()
	support.Write(t, m.scratch, map[string]string{"alias.inc": "[alias]\n\tff = fetch -q .\n"})
	support.Write(t, m.p, map[string]string{"m.sh": "git fetch -q . " + mgBranch + ":main\n"})
}

// S5, S8: with no ready check, every move of main run for real leaves it
// where it was: git refuses, in route-guard's words.
func TestMergeHookRefusesEveryMove(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	m.gateInstalled(t)
	m.scripts(t)
	before := m.rev(t, "main")
	for _, c := range mgAway(m) {
		r := m.sh(t, m.p, c)
		equalStr(t, m.refOr(t, "main"), before, c+": main stays")
		if !strings.Contains(r.Stderr, "jhon merge gate: ") || !strings.Contains(r.Stderr, gateRefusal) {
			t.Errorf("%s: git's refusal names the check: exit %d %q", c, r.Code, r.Stderr)
		}
	}
	wtm := m.mainWorktree(t)
	for _, c := range mgOnMain() {
		r := m.sh(t, wtm, c)
		equalStr(t, m.refOr(t, "main"), before, c+": main stays")
		if !strings.Contains(r.Stderr, gateRefusal) {
			t.Errorf("%s: git's refusal names the check: exit %d %q", c, r.Code, r.Stderr)
		}
		m.sh(t, wtm, "git merge --abort; git rebase --abort; git branch --unset-upstream; git reset -q --"+"hard main; true")
	}
	// Deleting main is the safety guard's; creating it again is a move.
	d := newMG(t).tested(t)
	d.gateInstalled(t)
	r := d.sh(t, d.p, "git branch -"+"D main && git branch main "+mgBranch)
	if got := d.refOr(t, "main"); got == d.rev(t, mgBranch) || !strings.Contains(r.Stderr, gateRefusal) {
		t.Errorf("delete then create: main %q, stderr %q", got, r.Stderr)
	}
}

// S5: after a ready check of the branch's tip, the same moves run.
func TestMergeHookAllowsAfterAReadyCheck(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		onMain bool
		line   string
	}{
		{false, "git fetch -q . " + mgBranch + ":main"},
		{false, "git update-ref refs/heads/main " + mgBranch},
		{false, "bash m.sh"},
		{true, "git merge -q --ff-only " + mgBranch},
		{true, "git merge -q --no-ff -m merged " + mgBranch},
		{true, "git reset -q --" + "hard " + mgBranch},
	} {
		m := readyMG(t)
		m.gateInstalled(t)
		m.scripts(t)
		dir := m.p
		if c.onMain {
			dir = m.mainWorktree(t)
		}
		r := m.sh(t, dir, c.line)
		code(t, r, 0, c.line)
		if tip, got := m.rev(t, mgBranch), m.rev(t, "main"); got != tip && m.rev(t, "main^2") != tip {
			t.Errorf("%s: main %s, want the branch's tip %s merged", c.line, got, tip)
		}
	}
}

// S3: the everyday flow runs as before with the hook installed: commits
// and merges on a feature branch, fetches, and main updated from its own
// upstream.
func TestMergeHookEverydayFlow(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	m.gateInstalled(t)
	origin := filepath.Join(m.scratch, "origin.git")
	support.Git(t, m.scratch, "init", "-q", "--bare", origin)
	support.Git(t, m.p, "remote", "add", "origin", origin)
	support.Git(t, m.p, "push", "-q", "origin", "main")
	support.Git(t, m.p, "branch", "-q", "--set-upstream-to=origin/main", "main")
	other := filepath.Join(m.scratch, "other")
	support.Git(t, m.scratch, "clone", "-q", origin, other)
	ahead := func() {
		support.Git(t, other, "commit", "-q", "--allow-empty", "-m", "upstream")
		support.Git(t, other, "push", "-q", "origin", "main")
	}
	wtm := m.mainWorktree(t)
	ahead()
	code(t, m.sh(t, wtm, "git pull -q --ff-only"), 0, "pull --ff-only")
	ahead()
	code(t, m.sh(t, wtm, "git fetch -q origin && git merge -q --ff-only origin/main"), 0, "fetch then merge --ff-only origin/main")
	ahead()
	code(t, m.sh(t, wtm, "git fetch -q origin && git reset -q --"+"hard origin/main"), 0, "reset to origin/main")
	equalStr(t, m.rev(t, "main"), m.rev(t, "origin/main"), "main is at its upstream")
	for _, c := range []string{
		"git commit -q --allow-empty -m feature",
		"git fetch -q origin",
		"git merge -q --no-edit main",
		"git checkout -q -b feat2 && git commit -q --allow-empty -m two && git rebase -q main",
		"git checkout -q " + mgBranch + " && git merge -q --no-edit feat2",
		"git push -q origin " + mgBranch,
	} {
		code(t, m.sh(t, m.p, c), 0, c)
	}
}

// S6: the hook is installed only in a repository under LDD.
func TestMergeHookOnlyUnderLDD(t *testing.T) {
	t.Parallel()
	m := newMG(t)
	plain := mkdir(t, filepath.Join(m.scratch, "plain"))
	support.GitRepo(t, plain)
	code(t, m.bash(t, plain, "git status"), 0, "a call in a repository without LDD")
	r := support.Run(t, support.Cmd{Args: []string{"git", "config", "--get-regexp", `^hook\.`}, Dir: plain, Env: support.Env()})
	equalStr(t, r.Stdout, "", "no hook outside LDD")
	r = support.Jhon(t, plain, m.env, "hook", "merge-gate", "--install")
	if r.Code == 0 {
		t.Errorf("--install outside LDD: exit 0, want a refusal")
	}
	support.Git(t, m.p, "config", "hook.lint.event", "pre-commit")
	support.Git(t, m.p, "config", "hook.lint.command", "true")
	m.gateInstalled(t)
	equalStr(t, strings.TrimSpace(support.Git(t, m.p, "config", "--get", "hook.lint.command")), "true", "another hook is untouched")
}

// S8: git prints route-guard's instruction.
func TestMergeHookRefusalText(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	m.gateInstalled(t)
	tip := m.rev(t, mgBranch)
	r := m.sh(t, m.p, "git fetch -q . "+mgBranch+":main")
	want := "jhon merge gate: this moves the default branch main to " + tip[:12] +
		", and no ready merge check of that commit is on record: run `jhon deliver check " + mgChange + " --action merge` on its own line, fix what it reports, then merge.\n"
	if !strings.HasPrefix(r.Stderr, want) || r.Code == 0 {
		t.Errorf("git's refusal: exit %d %q, want it to open with %q", r.Code, r.Stderr, want)
	}
}

// S6: route-guard refuses a command that removes the merge gate or turns
// it off; reading the configuration, other keys and core.hooksPath (which
// does not turn a configured hook off) stay allowed.
func TestMergeGateCannotBeTurnedOff(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	m.gateInstalled(t)
	inc := filepath.Join(m.scratch, "x.inc")
	m.cases(t, "turns the gate off", 2, [][2]string{
		{m.p, "git config --unset hook.jhon-merge-gate.command"},
		{m.p, "git config --remove-section hook.jhon-merge-gate"},
		{m.p, "git config hook.jhon-merge-gate.enabled false"},
		{m.p, "git config unset hook.jhon-merge-gate.event"},
		{m.p, "git config --global hook.reference-transaction.enabled false"},
		{m.p, "git -c hook.jhon-merge-gate.enabled=false status"},
		{m.p, "git -c hook.reference-transaction.enabled=false fetch -q . " + mgBranch + ":other"},
		{m.p, "git -c include.path=" + inc + " status"},
		{m.p, "git config include.path " + inc},
		{m.p, "GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=hook.reference-transaction.enabled GIT_CONFIG_VALUE_0=false git status"},
		{m.p, "export GIT_CONFIG_PARAMETERS=\"'hook.jhon-merge-gate.enabled'='false'\"; git status"},
		{m.p, "GIT_CONFIG_GLOBAL=" + inc + " git status"},
	})
	m.cases(t, "allowed", 0, [][2]string{
		{m.p, "git config --get hook.jhon-merge-gate.command"},
		{m.p, "git config --list"},
		{m.p, "git config user.name someone"},
		{m.p, "git -c core.hooksPath=/dev/null status"},
	})
}

// S9: the owner is never refused: with no agent marker, git moves main as
// the owner asks; the same move with the marker is refused.
func TestMergeHookNeverRefusesTheOwner(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	m.gateInstalled(t)
	wtm := m.mainWorktree(t)
	before := m.rev(t, "main")
	r := m.sh(t, wtm, "git commit -q --allow-empty -m agent")
	if m.rev(t, "main") != before || !strings.Contains(r.Stderr, gateRefusal) {
		t.Errorf("the agent's commit on main: exit %d %q; want git's refusal", r.Code, r.Stderr)
	}
	code(t, m.owner(t, wtm, "git commit -q --allow-empty -m owner"), 0, "the owner's commit on main")
	if m.rev(t, "main") == before {
		t.Errorf("the owner's commit did not move main")
	}
	code(t, m.owner(t, wtm, "git merge -q --no-edit "+mgBranch), 0, "the owner's merge")
	code(t, m.owner(t, wtm, "git commit -q --amend --allow-empty -m amended"), 0, "the owner's amend")
}

// L12 B2, B3, B5, B7: the gate is not turned off by what the line changes
// around git: the work tree's LDD files moved away, HOME at the project,
// every branch deleted before main is created, a git of its own on PATH.
func TestMergeHookHoldsWhateverTheLineChanges(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	m.gateInstalled(t)
	before := m.rev(t, "main")
	fake := mkdir(t, filepath.Join(m.scratch, "fakebin"))
	support.Write(t, fake, map[string]string{"git": "#!/bin/sh\nexit 0\n"})
	if err := os.Chmod(filepath.Join(fake, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	git := m.realGit(t)
	b := mgBranch
	for _, c := range []struct {
		line  string
		extra []string
	}{
		{"mv openspec/config.yaml config.away && git fetch -q . " + b + ":main; mv config.away openspec/config.yaml", nil},
		{"git fetch -q . " + b + ":main", []string{"HOME=" + m.p}},
		{git + " fetch -q . " + b + ":main", []string{"PATH=" + fake + ":" + os.Getenv("PATH"), "GIT_EXEC_PATH=" + fake}},
		{"t=$(git rev-parse " + b + ") && git checkout -q --detach && git branch -" + "D main " + b + " && git branch main $t", nil},
	} {
		r := m.sh(t, m.p, c.line, c.extra...)
		if got := m.refOr(t, "main"); got != "" && got != before || !strings.Contains(r.Stderr, gateRefusal) {
			t.Errorf("%s %v: main %q (was %s), exit %d %q; want git's refusal", c.line, c.extra, got, before, r.Code, r.Stderr)
		}
	}
}

// realGit is the git on PATH, by its absolute path.
func (m mgEnv) realGit(t *testing.T) string {
	t.Helper()
	r := support.Run(t, support.Cmd{Args: []string{"bash", "-c", "command -v git"}, Env: support.Env()})
	return strings.TrimSpace(r.Stdout)
}

// S9, L12 B4: route-guard refuses an agent line that drops or fakes the
// marker, or turns the gate off where it can read it: an alias value, a
// GIT_CONFIG_* variable set by printf -v, read or declare.
func TestMergeGateMarkerAndAliasesAreKept(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	m.gateInstalled(t)
	b := mgBranch
	m.cases(t, "refused", 2, [][2]string{
		{m.p, "unset JHON_AGENT; git fetch -q . " + b + ":main"},
		{m.p, "env -u JHON_AGENT git status"},
		{m.p, "JHON_AGENT= git commit -q --allow-empty -m x"},
		{m.p, "export JHON_AGENT=0; git status"},
		{m.p, "env -i git status"},
		{m.p, "git config alias.m '-c hook.reference-transaction.enabled=false merge'"},
		{m.p, "git -c alias.m='-c core.hooksPath=/x merge' m " + b},
		{m.p, "printf -v GIT_CONFIG_COUNT 1; git status"},
		{m.p, "read GIT_CONFIG_COUNT <<< 1; git status"},
		{m.p, "declare -x GIT_CONFIG_PARAMETERS=x; git status"},
	})
	m.cases(t, "allowed", 0, [][2]string{
		{m.p, "git status"},
		{m.p, "git config alias.st status"},
		{m.p, "env FOO=1 git status"},
	})
}

// L14: a line that strips the marker or gives git configuration by a form
// a plain reading misses is either refused by route-guard or, run for
// real, leaves main where it was.
func TestMergeGateHoldsAgainstIndirectMarkerAndConfig(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	m.gateInstalled(t)
	before := m.rev(t, "main")
	b := mgBranch
	move := "git fetch -q . " + b + ":main"
	for _, line := range []string{
		"/usr/bin/env -i PATH=\"$PATH\" HOME=\"$HOME\" " + move,
		"command env -i PATH=\"$PATH\" " + move,
		"(exec -c " + move + ")",
		"env --ignore-env PATH=\"$PATH\" " + move,
		"env --i PATH=\"$PATH\" " + move,
		"env -iv PATH=\"$PATH\" " + move,
		"unset \"JHON_AGENT\"; " + move,
		"unset JHON\\_AGENT; " + move,
		"unset JHON_'AGENT'; " + move,
		"n=JHON_AGENT; unset $n; " + move,
		"unset ${!JHON_@}; " + move,
		"export -n JHON_AGENT; " + move,
		"env --unset=JHON_AGENT " + move,
		"env -uJHON_AGENT " + move,
		"[() { return 0; }; export -f [; " + move,
		"read() { return 1; }; export -f read; " + move,
		"SHELLOPTS=noexec " + move,
		"declare -x GIT_CONFIG_{COUNT=1,KEY_0=hook.reference-transaction.enabled,VALUE_0=false}; " + move,
		"git config alias.m \"-c 'hook'.reference-transaction.enabled=false fetch\"",
		"git -c alias.m=\"-c \\\"hook\\\".jhon-merge-gate.enabled=false fetch\" m -q . " + b + ":main",
	} {
		if r := m.bash(t, m.p, line); r.Code == 2 {
			continue
		}
		m.sh(t, m.p, line)
		m.sh(t, m.p, "git m -q . "+b+":main")
		if got := m.refOr(t, "main"); got != before {
			t.Errorf("%s: route-guard let it run and main moved to %s (was %s)", line, got, before)
			support.Git(t, m.p, "update-ref", "refs/heads/main", before)
		}
		m.sh(t, m.p, "git config --unset-all alias.m; true")
	}
}

// L14: a line that only reads or searches for the names runs: no guard
// refuses printing the marker or grepping the docs for GIT_CONFIG_*.
func TestMergeGateAllowsSearchingForTheNames(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	m.gateInstalled(t)
	m.cases(t, "a reader", 0, [][2]string{
		{m.p, "printenv JHON_AGENT"},
		{m.p, "echo $JHON_AGENT"},
		{m.p, "env | grep JHON"},
		{m.p, "grep -rn GIT_CONFIG_COUNT docs"},
		{m.p, "rg GIT_CONFIG_PARAMETERS"},
		{m.p, "rg -n 'GIT_CONFIG_PARAMETERS' ."},
		{m.p, "cat notes.md | grep -n JHON_AGENT"},
		{m.p, "env"},
		{m.p, "env FOO=1 git status"},
	})
}

// L14 c8: the hook's shell line holds against functions the agent's shell
// exports in place of the commands it would run (`[`, read, printf,
// command): git still refuses.
func TestMergeHookLineIgnoresExportedFunctions(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	m.gateInstalled(t)
	before := m.rev(t, "main")
	r := m.sh(t, m.p, `[() { return 0; }; test() { return 0; }; read() { return 1; }; printf() { :; }; command() { :; }; export -f [ test read printf command; git fetch -q . `+mgBranch+`:main`)
	if got := m.rev(t, "main"); got != before || !strings.Contains(r.Stderr, gateRefusal) {
		t.Errorf("exported functions: main %s (was %s), exit %d %q; want git's refusal", got, before, r.Code, r.Stderr)
	}
}

// S9, L17: ENV and BASH_ENV are no input of the hook's sh (non-interactive,
// POSIX mode), so an app's everyday ENV lines run, and setting both does not
// turn the hook off.
func TestMergeGateAllowsTheAppsENV(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	m.gateInstalled(t)
	m.cases(t, "an app's ENV", 0, [][2]string{
		{m.p, "ENV=production npm run build"}, {m.p, "ENV=test go test ./..."}, {m.p, "export ENV=staging"},
		{m.p, "BASH_ENV=x bash -c true"},
	})
	before := m.rev(t, "main")
	m.sh(t, m.p, `printf 'exit 0\n' > evil; export ENV=$PWD/evil BASH_ENV=$PWD/evil; git fetch -q . `+mgBranch+`:main`)
	equalStr(t, m.refOr(t, "main"), before, "ENV and BASH_ENV do not turn the hook off")
}

// S9 owner, L17: exported functions in the owner's shell never refuse the
// owner.
func TestMergeHookOwnerWithExportedFunctions(t *testing.T) {
	t.Parallel()
	m := newMG(t).tested(t)
	m.gateInstalled(t)
	code(t, m.owner(t, m.p, `read() { return 1; }; printf() { :; }; command() { :; }; [() { return 1; }; export -f read printf command [; git fetch -q . `+mgBranch+`:main`), 0, "owner with exported functions")
}
