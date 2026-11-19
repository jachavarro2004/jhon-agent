// The normal session's permissions (S33): with no mode set, only what truly
// matters asks. A push to a branch that is not the repository's default
// passes, and so do reading GitHub and the pull request's collaboration
// (create, view, checks, comment, ready, edit, run watch/view). A push to the
// default branch, a push of every branch or of tags, a merge, a release,
// repository and secret writes, a workflow run, deploy, destroy, publish,
// remote and outside-write still ask, in one question per command; where
// nobody can be asked they are refused, as before.

package guards

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// normalRepo is a repository with an origin (a bare repository) whose HEAD
// names def, main pushed to it, a branch topic, a tag v1, and HEAD on head.
// def "" leaves origin/HEAD unset.
func normalRepo(t *testing.T, s sgEnv, name, def, head string) string {
	t.Helper()
	origin := filepath.Join(s.scratch, name+"-origin.git")
	support.Git(t, s.scratch, "init", "-q", "--bare", origin)
	repo := support.GitRepo(t, mkdir(t, filepath.Join(s.scratch, name)))
	support.Git(t, repo, "remote", "add", "origin", origin)
	support.Git(t, repo, "push", "-q", "origin", "main")
	support.Git(t, repo, "branch", "develop")
	support.Git(t, repo, "push", "-q", "origin", "develop")
	support.Git(t, repo, "fetch", "-q", "origin")
	// A fetch may set origin/HEAD itself (git 2.48 and later): set or drop it.
	if def != "" {
		support.Git(t, repo, "remote", "set-head", "origin", def)
	} else {
		support.Git(t, repo, "remote", "set-head", "origin", "-d")
	}
	support.Git(t, repo, "branch", "topic")
	support.Git(t, repo, "tag", "v1")
	if head == "detached" {
		support.Git(t, repo, "switch", "-q", "--detach")
	} else if head != "main" {
		support.Git(t, repo, "switch", "-q", "-c", head)
	}
	return repo
}

// callers are the callers each rule is checked for: a sub-agent under
// bypassPermissions and in default mode, where nobody can be asked, and the
// top level in permission_mode default.
var normalSubAgents = []sgOpts{{}, {mode: ptr("default")}}

// passes checks that command passes silently for every caller.
func (s sgEnv) passes(t *testing.T, command, cwd, label string) {
	t.Helper()
	for _, o := range append(normalSubAgents, top()) {
		o.cwd = cwd
		r := s.bash(t, command, o)
		code(t, r, 0, label+": "+command)
		if r.Stdout != "" || r.Stderr != "" {
			t.Errorf("%s: %q printed %q %q, want a silent pass", label, command, r.Stdout, r.Stderr)
		}
	}
}

// asks checks that command asks the top level as cls, and is refused for a
// sub-agent with cls's grant hint.
func (s sgEnv) asks(t *testing.T, command, cwd, cls, label string) {
	t.Helper()
	for _, o := range normalSubAgents {
		o.cwd = cwd
		r := s.bash(t, command, o)
		code(t, r, 2, label+", sub-agent: "+command)
		matches(t, r.Stderr, regexp.QuoteMeta(cls)+` needs the owner's authorization`, label+", sub-agent: "+command)
	}
	o := top()
	o.cwd = cwd
	matches(t, askedWhy(t, s.bash(t, command, o).Stdout, label+": "+command), `^`+regexp.QuoteMeta(cls)+`: `, label+", top-level: "+command)
}

// Rule 1: a push to a branch that is not the default passes; the current
// branch counts when the push names none.
func TestNormalPushToAFeatureBranchPasses(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	feat := normalRepo(t, s, "work", "main", "feat")
	for _, c := range []string{"git push", "git push -u origin feat", "git push --set-upstream origin feat", "git push origin feat",
		"git push origin HEAD", "git push -u origin HEAD", "git push origin HEAD:feat", "git push origin feat:refs/heads/feat",
		"git push origin refs/heads/feat", "git push origin topic", "git push origin feat topic", "git push -q origin feat",
		"git push origin HEAD~1:feat", "git push -o ci.skip origin feat"} {
		s.passes(t, c, feat, "feature branch")
	}
	s.passes(t, "git -C "+feat+" push -u origin feat", s.p, "named by -C")
	s.passes(t, "cd "+feat+" && git push origin feat && gh pr create --fill", s.p, "push then a pull request")
	// main and master always ask, whatever origin/HEAD names (L130).
	dev := normalRepo(t, s, "dev", "develop", "main")
	s.asks(t, "git push origin main", dev, "push", "main asks even when origin/HEAD names develop")
}

// Rule 2: a push to the default branch, or with no branch while on it, of
// every branch, or of tags still asks as push.
func TestNormalPushToTheDefaultBranchAsks(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	feat := normalRepo(t, s, "work", "main", "feat")
	for _, c := range []string{"git push origin main", "git push origin HEAD:main", "git push origin feat:main", "git push origin refs/heads/main",
		"git push origin HEAD:refs/heads/main", "git push origin feat main", "git push --all origin", "git push --branches origin",
		"git push --tags", "git push origin --tags", "git push --follow-tags origin feat", "git push origin v1", "git push origin refs/tags/v1",
		"git push origin tag v1", "git push origin 'refs/heads/*:refs/heads/*'", "git push https://example.invalid/r.git feat",
		"git push origin --delete topic", "git push origin :topic", "git push --repo=origin feat"} {
		s.asks(t, c, feat, "push", "feature branch")
	}
	main := normalRepo(t, s, "onmain", "main", "main")
	for _, c := range []string{"git push", "git push origin", "git push -u origin HEAD", "git push origin HEAD"} {
		s.asks(t, c, main, "push", "on the default branch")
	}
	// origin/HEAD unset: main and master are both the default.
	none := normalRepo(t, s, "nohead", "", "feat")
	for _, c := range []string{"git push origin main", "git push origin feat:master"} {
		s.asks(t, c, none, "push", "no origin/HEAD")
	}
	s.passes(t, "git push origin feat", none, "no origin/HEAD, a feature branch")
	dev := normalRepo(t, s, "dev", "develop", "feat")
	s.asks(t, "git push origin develop", dev, "push", "origin/HEAD names develop")
	// A feature branch whose push goes to the default branch's upstream.
	up := normalRepo(t, s, "upstream", "main", "feat")
	support.Git(t, up, "config", "push.default", "upstream")
	support.Git(t, up, "branch", "-q", "--set-upstream-to", "origin/main")
	s.asks(t, "git push", up, "push", "an upstream on the default branch")
	matching := normalRepo(t, s, "matching", "main", "feat")
	support.Git(t, matching, "config", "push.default", "matching")
	s.asks(t, "git push", matching, "push", "push.default matching")
	detached := normalRepo(t, s, "detached", "main", "detached")
	s.asks(t, "git push", detached, "push", "a detached HEAD")
	s.asks(t, "git push origin HEAD", detached, "push", "a detached HEAD")
	// The repository the push targets cannot be told: as before, it asks.
	s.asks(t, "GIT_DIR="+feat+"/.git git push origin feat", feat, "push", "GIT_DIR")
}

// Rule 3: reading GitHub and the pull request's collaboration pass.
func TestNormalGhCollaborationPasses(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	feat := normalRepo(t, s, "work", "main", "feat")
	for _, c := range []string{"gh pr create --fill", "gh pr create --title x --body y --base main", "gh pr view 1", "gh pr checks 1",
		"gh pr comment 1 -b x", "gh issue comment 1 -b x", "gh run watch 1", "gh run view 1", "gh pr ready 1", "gh pr edit 1 --title x",
		"gh pr list", "gh api repos/o/r/pulls/1", "gh api -X POST repos/o/r/issues/1/comments -f body=x",
		"gh api repos/o/r/pulls/1/comments -f body=x", "gh api --method PATCH /repos/o/r/pulls/1 -f title=x", "gh api repos/{owner}/{repo}/pulls -f title=x -f head=feat -f base=main"} {
		s.passes(t, c, feat, "gh")
	}
}

// Rule 4: a merge, a release, repository and secret writes, a workflow run,
// and an API write to anything but a pull request's collaboration still ask.
func TestNormalGhWritesAsk(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	feat := normalRepo(t, s, "work", "main", "feat")
	for _, c := range []string{"gh pr merge 1 --squash", "gh release create v1", "gh repo edit --description x", "gh repo create x --private",
		"gh repo fork", "gh secret set X", "gh workflow run deploy.yml", "gh pr close 1", "gh pr review 1 --approve", "gh issue create -t x",
		"gh api -X POST repos/o/r/releases", "gh api -X DELETE repos/o/r/git/refs/heads/x", "gh api -X PUT repos/o/r/pulls/1/merge",
		"gh api -X PATCH repos/o/r/pulls/1 -f state=closed", "gh api -X DELETE repos/o/r/pulls/1/comments", "gh api graphql -f query=x",
		"gh api -X POST repos/o/r/pulls/1/reviews -f event=APPROVE", "gh api -X PATCH repos/o/r/pulls/1 --input body.json",
		"gh api --hostname example.invalid -X POST repos/o/r/actions/workflows/1/dispatches"} {
		s.asks(t, c, feat, "gh-write", "gh")
	}
	for _, o := range append(normalSubAgents, top()) {
		o.cwd = feat
		code(t, s.bash(t, "gh pr merge 1 --admin", o), 2, "gh pr merge --admin stays refused")
	}
}

// Rule 5: deploy, destroy, publish and remote are unchanged. A shell write
// outside the project is the sandbox's (T128f).
func TestNormalOtherClassesStillAsk(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	feat := normalRepo(t, s, "work", "main", "feat")
	for _, c := range []struct{ command, cls string }{
		{"kubectl apply -f k.yaml", "deploy"}, {"git reset --hard", "destroy"}, {"npm publish", "publish"}, {"ssh host", "remote"},
	} {
		s.asks(t, c.command, feat, c.cls, "unchanged")
	}
}

// Rule 6: a command that needs several classes asks one question that
// names them all; where nobody can be asked, one refusal names them all.
func TestNormalOneQuestionPerCommand(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	feat := normalRepo(t, s, "work", "main", "feat")
	o := top()
	o.cwd = feat
	r := s.bash(t, "git push origin main && gh pr merge 1 --squash", o)
	code(t, r, 0, "push and merge")
	equalStr(t, askedWhy(t, r.Stdout, "push and merge"), "push: git push; gh-write: gh pr merge", "one question names both classes")
	for _, sub := range normalSubAgents {
		sub.cwd = feat
		r := s.bash(t, "git push origin main && gh pr merge 1 --squash", sub)
		code(t, r, 2, "push and merge, sub-agent")
		matches(t, r.Stderr, `^safety-guard: git push: push needs the owner's authorization\..* gh pr merge: gh-write needs the owner's authorization\.`, "push and merge, sub-agent")
	}
	// The feature push passes; the merge alone is asked.
	equalStr(t, askedWhy(t, s.bash(t, "git push -u origin feat && gh pr merge 1 --squash", o).Stdout, "feature push and merge"),
		"gh-write: gh pr merge", "only the merge is asked")
}

// Rule 7: yolo and super-yolo pass what still asks; what passes in normal
// passes with no mode note, as nothing was asked.
func TestNormalModesUnchanged(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"yolo", "super-yolo"} {
		s := newSG(t)
		feat := normalRepo(t, s, "work", "main", "feat")
		s.setMode(t, feat, mode)
		for _, c := range []string{"git push origin main", "gh pr merge 1 --squash", "git push origin main && gh pr merge 1 --squash"} {
			s.allowedBy(t, c, mode, sgOpts{cwd: feat}, mode+" sub-agent "+c)
			s.allowedBy(t, c, mode, sgOpts{top: true, cwd: feat}, mode+" top-level "+c)
		}
		s.passes(t, "git push -u origin feat && gh pr create --fill", feat, mode)
	}
}
