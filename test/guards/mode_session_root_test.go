// The mode that answers an ask is the session's own project's whenever the
// finding names no other real project (S31, T51): a finding with no
// repository (an aws call in a script that cd's into a computed directory),
// a directory that is no repository (a worktree already removed), or a
// worktree of the session's project. A finding in another real project needs
// that project's mode and the session's alike, so a cd into a super-yolo
// project lends a normal session nothing (L157).

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

// bootScript loses the guard its directory, as .github/scripts/bootstrap-registry.sh
// does: the aws calls after it name no repository.
const bootScript = "#!/usr/bin/env bash\nset -euo pipefail\nREPO_ROOT=\"$(cd \"$(dirname \"${BASH_SOURCE[0]}\")/..\" && pwd)\"\n" +
	"aws iam put-role-policy --role-name r --policy-name p --policy-document x\naws ecr create-repository --repository-name r\n"

// modeLog is the modes' log of the state dir: "<mode> <class> <root>" per line.
func modeLog(t *testing.T, state string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(state, "jhon", "modes", "allowed.jsonl"))
	if err != nil {
		return ""
	}
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var a map[string]any
		if err := json.Unmarshal([]byte(l), &a); err != nil {
			t.Fatal(err)
		}
		out = append(out, a["mode"].(string)+" "+a["class"].(string)+" "+a["root"].(string))
	}
	return strings.Join(out, "\n")
}

// askedOrRefused is an ask at the top level and a refusal from a sub-agent.
func (s sgEnv) askedOrRefused(t *testing.T, command, cls string, o sgOpts, label string) {
	t.Helper()
	o.top = true
	r := s.bash(t, command, o)
	code(t, r, 0, label+" (top level)")
	matches(t, askedWhy(t, r.Stdout, label), `^`+regexp.QuoteMeta(cls)+`: `, label+" (top level)")
	o.top = false
	r = s.bash(t, command, o)
	code(t, r, 2, label+" (sub-agent)")
	noDecision(t, r.Stdout, label+" (sub-agent)")
}

// A deploy in a script that names no repository is the session's project's
// (01a1add1:153, `bash .github/scripts/bootstrap-registry.sh`).
func TestModeNoRootFindingTakesTheSessionMode(t *testing.T) {
	t.Parallel()
	const run = "bash -n scripts/boot.sh"
	const note = `^note \(not a refusal; the call ran\): allowed by the owner's super-yolo mode: deploy: bash scripts/boot.sh: aws iam; deploy: bash scripts/boot.sh: aws ecr$`
	s := newSG(t)
	support.Write(t, s.p, map[string]string{"scripts/boot.sh": bootScript})
	// Normal asks, as before.
	s.askedOrRefused(t, run, "deploy", sgOpts{}, "normal")
	s.setMode(t, s.p, "super-yolo")
	for _, o := range []sgOpts{top(), {}} {
		r := s.bash(t, run, o)
		code(t, r, 0, "super-yolo")
		matches(t, modeNote(t, r.Stdout), note, "super-yolo")
	}
	// One line per finding, two findings per call.
	line := "super-yolo deploy " + s.p
	equalStr(t, modeLog(t, s.state), strings.Repeat(line+"\n", 3)+line, "the modes' log names the session's project")
	s.setMode(t, s.p, "yolo")
	s.allowedBy(t, run, "yolo", sgOpts{}, "yolo passes deploy")
}

// A normal session running the super-yolo project's script still asks: a
// finding that names no repository is the session's, never the script's.
func TestModeNoRootFindingInANormalSessionStillAsks(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	support.Write(t, s.p, map[string]string{"scripts/boot.sh": bootScript})
	s.setMode(t, s.p, "super-yolo")
	s.askedOrRefused(t, "bash -n "+s.p+"/scripts/boot.sh", "deploy", sgOpts{cwd: s.other}, "a normal session")
	equalStr(t, modeLog(t, s.state), "", "the modes' log")
}

// A destroy in a directory that is no repository, here a worktree already
// removed, is the session's project's (01a1aa6d:919, `git branch -D` after a
// cd into acme_app-worktrees/root-test-override).
func TestModeNonRepoRootTakesTheSessionMode(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	base := varTemp(t, "jhon-mode-root-")
	repo := support.GitRepo(t, mkdir(t, filepath.Join(base, "repo")))
	plain := mkdir(t, filepath.Join(base, "plain"))
	gone := "cd " + repo + "-worktrees/gone && git branch -D gone-branch"
	inPlain := "cd " + plain + " && git branch -D x"
	o := sgOpts{cwd: repo}
	s.askedOrRefused(t, gone, "destroy", o, "normal, a removed worktree")
	s.setMode(t, repo, "yolo")
	s.askedOrRefused(t, gone, "destroy", o, "yolo still asks destroy")
	s.setMode(t, repo, "super-yolo")
	for _, c := range []string{gone, inPlain} {
		for _, top := range []bool{true, false} {
			o.top = top
			r := s.bash(t, c, o)
			code(t, r, 0, c)
			matches(t, modeNote(t, r.Stdout), `^note \(not a refusal; the call ran\): allowed by the owner's super-yolo mode: destroy: git branch -D`, c)
		}
	}
	for _, l := range strings.Split(modeLog(t, s.state), "\n") {
		equalStr(t, l, "super-yolo destroy "+repo, "the modes' log names the session's project")
	}
}

// A destroy in a worktree of the session's project is the project's.
func TestModeSessionWorktreeRootTakesTheSessionMode(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	base := varTemp(t, "jhon-mode-wtroot-")
	repo := support.GitRepo(t, mkdir(t, filepath.Join(base, "repo")))
	wt := filepath.Join(base, "repo-worktrees", "feature")
	support.Git(t, repo, "worktree", "add", "-q", wt, "-b", "feature")
	c := "cd " + wt + " && git reset --hard"
	s.askedOrRefused(t, c, "destroy", sgOpts{cwd: repo}, "normal")
	s.setMode(t, repo, "super-yolo")
	s.allowedBy(t, c, "super-yolo", sgOpts{cwd: repo}, "the main checkout's session, its worktree")
	s.allowedBy(t, "git -C "+repo+" reset --hard", "super-yolo", sgOpts{cwd: wt}, "the worktree's session, the main checkout")
}

// A finding in another real project needs both modes: a cd into a
// super-yolo project lends a normal session nothing, for every class
// (L157), and a super-yolo session lends a normal project nothing either.
func TestModeAnotherProjectNeedsBothModes(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	s.setMode(t, s.other, "super-yolo")
	for _, c := range []struct{ command, cls string }{
		{"cd <OTHER> && git reset --hard", "destroy"},
		{"git -C <OTHER> push", "push"},
		{"cd <OTHER> && git push", "push"},
	} {
		s.askedOrRefused(t, s.expand(c.command), c.cls, sgOpts{}, "a normal session: "+c.command)
	}
	equalStr(t, modeLog(t, s.state), "", "the modes' log")
	s.setMode(t, s.other, "normal")
	s.setMode(t, s.p, "super-yolo")
	s.askedOrRefused(t, s.expand("cd <OTHER> && git push"), "push", sgOpts{}, "a super-yolo session in a normal project")
	s.setMode(t, s.other, "super-yolo")
	s.allowedBy(t, s.expand("cd <OTHER> && git reset --hard"), "super-yolo", sgOpts{}, "both super-yolo")
}

// varSG is newSG with both projects under /var/tmp, which the guard does
// not count as scratch (/tmp is), and the session's project in super-yolo.
func varSG(t *testing.T, prefix string) sgEnv {
	t.Helper()
	s := newSG(t)
	vb := varTemp(t, prefix)
	s.p = support.GitInit(t, mkdir(t, filepath.Join(vb, "proj")))
	s.other = support.GitInit(t, mkdir(t, filepath.Join(vb, "other")))
	s.setMode(t, s.p, "super-yolo")
	return s
}

// A directory that does not exist yet is judged by its nearest existing
// ancestor, and a cd follows a link the command itself created: a
// super-yolo session lends a normal project nothing either way (L183).
func TestModeNotYetExistingDirInAnotherProjectNeedsBothModes(t *testing.T) {
	t.Parallel()
	s := varSG(t, "jhon-mode-nd-")
	for _, c := range []struct{ command, cls string }{
		{"mkdir -p <OTHER>/nd && cd <OTHER>/nd && aws ecs run-task --cluster c", "deploy"},
		{"ln -s <OTHER> <P>/l2 && cd <P>/l2 && aws ecs run-task --cluster c", "deploy"},
		{`cd <OTHER>/nd3 && eval "$X"`, "unreadable"},
		{"mkdir -p <OTHER>/nd && cd <OTHER>/nd && git reset --hard", "destroy"},
	} {
		s.askedOrRefused(t, s.expand(c.command), c.cls, sgOpts{}, "a super-yolo session, a normal project: "+c.command)
	}
	equalStr(t, modeLog(t, s.state), "", "the modes' log")
	// A directory not there yet in the session's own project, or outside
	// any repository, is still the session's.
	for _, c := range []string{
		"mkdir -p <P>/nd && cd <P>/nd && aws ecs run-task --cluster c",
		"mkdir -p <P>/nd && cd <P>/nd && git reset --hard",
		"cd " + filepath.Dir(s.p) + "/proj-worktrees/gone && git reset --hard",
	} {
		s.allowedBy(t, s.expand(c), "super-yolo", sgOpts{}, "the session's own: "+c)
	}
}

// GIT_DIR and GIT_WORK_TREE name the repository a git command acts on; when
// they name another project, or disagree, the session's mode alone passes
// nothing (L183).
func TestModeGitDirInAnotherProjectNeedsBothModes(t *testing.T) {
	t.Parallel()
	s := varSG(t, "jhon-mode-gd-")
	for _, c := range []struct{ command, cls string }{
		{"GIT_DIR=<OTHER>/.git GIT_WORK_TREE=<P> git push", "push"},
		{"GIT_DIR=<OTHER>/.git GIT_WORK_TREE=<P> git reset --hard", "destroy"},
		{"GIT_DIR=<OTHER>/.git git reset --hard", "destroy"},
		{"git --git-dir=<OTHER>/.git --work-tree=<P> reset --hard", "destroy"},
	} {
		s.askedOrRefused(t, s.expand(c.command), c.cls, sgOpts{}, "a super-yolo session, another repository: "+c.command)
	}
	equalStr(t, modeLog(t, s.state), "", "the modes' log")
	s.allowedBy(t, s.expand("GIT_DIR=<P>/.git GIT_WORK_TREE=<P> git reset --hard"), "super-yolo", sgOpts{}, "the session's own repository")
}

// A cd inside $(…) or ( … ) runs in a subshell: it moves that subshell's
// directory only, never the outer command's. A cd that loses the directory
// (an unknown value, cd -) leaves a directory the guard cannot tell, which
// is normal; so is git -C to a value it cannot read; and a script of another
// repository is that repository's. A super-yolo session lends a normal
// project nothing either way (L185).
func TestModeLostDirInAnotherProjectNeedsBothModes(t *testing.T) {
	t.Parallel()
	s := varSG(t, "jhon-mode-lost-")
	support.Write(t, s.other, map[string]string{"scripts/boot.sh": bootScript})
	for _, c := range []struct{ command, cls string }{
		{`cd <OTHER> && R="$(cd "$(dirname x)/.." && pwd)" && aws ecs run-task --cluster c`, "deploy"},
		{`cd "$(printf %s <OTHER>)" && git reset --hard`, "destroy"},
		{"cd $UNKNOWNVAR && aws ecs run-task --cluster c", "deploy"},
		{"cd - && git reset --hard", "destroy"},
		{"git -C <OTHER> reset --hard", "destroy"},
		{"git -C <OTHER> branch -D x", "destroy"},
		{`git -C "$X" reset --hard`, "destroy"},
		{"(cd <OTHER> && git reset --hard)", "destroy"},
		{`R="$(cd <OTHER> && git reset --hard)"`, "destroy"},
		{"bash -n <OTHER>/scripts/boot.sh", "deploy"},
	} {
		s.askedOrRefused(t, s.expand(c.command), c.cls, sgOpts{}, "a super-yolo session, a normal project: "+c.command)
	}
	equalStr(t, modeLog(t, s.state), "", "the modes' log")
	mkdir(t, filepath.Join(s.p, "src"))
	for _, c := range []string{
		`R="$(cd "$(dirname x)/.." && pwd)" && aws ecs run-task --cluster c`,
		"(cd <OTHER>) && git reset --hard",
		`R="$(cd <OTHER> && pwd)" && git reset --hard`,
		"git -C <P> reset --hard",
		"cd src && git push origin main",
	} {
		s.allowedBy(t, s.expand(c), "super-yolo", sgOpts{}, "the session's own: "+c)
	}
}

// vcs-guard: a write outside the project from a worktree of a super-yolo
// project passes with the project's mode; from a normal one it asks.
func TestModeVcsGuardFromASessionWorktree(t *testing.T) {
	t.Parallel()
	w := newWriteEnv(t)
	support.Git(t, w.project, "commit", "-q", "--allow-empty", "-m", "base")
	wt := filepath.Join(w.w, "proj-worktrees", "feature")
	support.Git(t, w.project, "worktree", "add", "-q", wt, "-b", "feature")
	call := func() support.Result {
		return hook(t, w.env, "vcs-guard", vcsDirs(t), map[string]any{"tool_name": "Write", "cwd": wt, "session_id": "s",
			"permission_mode": "default", "tool_input": map[string]any{"file_path": "/etc/jhon.conf"}})
	}
	r := call()
	code(t, r, 0, "normal")
	matches(t, askedWhy(t, r.Stdout, "normal"), `^outside-write: `, "normal")
	code(t, support.Jhon(t, w.project, w.env, "mode", "super-yolo"), 0, "jhon mode super-yolo")
	r = call()
	code(t, r, 0, "super-yolo")
	matches(t, modeNote(t, r.Stdout), `^note \(not a refusal; the call ran\): allowed by the owner's super-yolo mode: outside-write: `, "super-yolo")
}

// route-guard: a reviewer nobody asked for, from a worktree of a yolo
// project, passes with the project's mode.
func TestModeRouteGuardFromASessionWorktree(t *testing.T) {
	t.Parallel()
	r := newRG(t, "")
	wt := filepath.Join(filepath.Dir(r.p), "proj-worktrees", "feature")
	support.Git(t, r.p, "worktree", "add", "-q", wt, "-b", "feature")
	call := func() support.Result {
		return hook(t, r.env, "route-guard", nil, map[string]any{"tool_name": "Agent", "cwd": wt, "permission_mode": "default",
			"tool_input": map[string]any{"subagent_type": "review-risk", "prompt": "x"}})
	}
	res := call()
	code(t, res, 0, "normal")
	matches(t, askedWhy(t, res.Stdout, "normal"), `^review: `, "normal")
	code(t, support.Jhon(t, r.p, r.env, "mode", "yolo"), 0, "jhon mode yolo")
	res = call()
	code(t, res, 0, "yolo")
	matches(t, modeNote(t, res.Stdout), `^note \(not a refusal; the call ran\): allowed by the owner's yolo mode: review: `, "yolo")
}

// A script of the session's own project run from another project's
// directory runs its commands there: it needs that project's mode too, and
// a bash 5.3 funsub (`${ …; }`, `${| …; }`) runs in the current shell, so
// its cd moves the outer directory. A super-yolo session lends a normal
// project nothing either way (L187).
func TestModeOwnScriptElsewhereAndFunsubNeedBothModes(t *testing.T) {
	t.Parallel()
	s := varSG(t, "jhon-mode-v3-")
	support.Write(t, s.p, map[string]string{"scripts/aws.sh": "#!/bin/sh\naws ecs run-task --cluster c\n"})
	for _, c := range []struct{ command, cls string }{
		{"cd <OTHER> && bash <P>/scripts/aws.sh", "deploy"},
		{"cd <OTHER> && <P>/scripts/aws.sh", "deploy"},
		{"env -C <OTHER> bash <P>/scripts/aws.sh", "deploy"},
		{"(cd <OTHER> && bash <P>/scripts/aws.sh)", "deploy"},
		{"x=${ cd <OTHER>; } && aws ecs run-task --cluster c", "deploy"},
		{"x=${| cd <OTHER>; } && aws ecs run-task --cluster c", "deploy"},
		{`x=${ cd <OTHER>; } && eval "$X"`, "unreadable"},
	} {
		s.askedOrRefused(t, s.expand(c.command), c.cls, sgOpts{}, "a super-yolo session, a normal project: "+c.command)
	}
	equalStr(t, modeLog(t, s.state), "", "the modes' log")
	s.allowedBy(t, s.expand("bash <P>/scripts/aws.sh"), "super-yolo", sgOpts{}, "the session's own script at home")
	s.allowedBy(t, s.expand("x=${ cd <P>/src; } && aws ecs run-task --cluster c"), "super-yolo", sgOpts{}, "funsub into its own project")
}

// The same for a deletion: the session's own script that deletes a branch,
// run from another project's directory, asks there (L187).
func TestModeOwnDestroyScriptElsewhereNeedsBothModes(t *testing.T) {
	t.Parallel()
	s := varSG(t, "jhon-mode-v3d-")
	support.Write(t, s.p, map[string]string{"scripts/prune.sh": "#!/bin/sh\ngit branch -D x\n"})
	for _, command := range []string{
		"cd <OTHER> && bash <P>/scripts/prune.sh",
		"cd <OTHER> && <P>/scripts/prune.sh",
		"(cd <OTHER> && bash <P>/scripts/prune.sh)",
		"env -C <OTHER> bash <P>/scripts/prune.sh",
		"x=${ cd <OTHER>; } && git branch -D x",
	} {
		s.askedOrRefused(t, s.expand(command), "destroy", sgOpts{}, "a super-yolo session, a normal project: "+command)
	}
	equalStr(t, modeLog(t, s.state), "", "the modes' log")
	s.allowedBy(t, s.expand("bash <P>/scripts/prune.sh"), "super-yolo", sgOpts{}, "the session's own script at home")
}

// A git command the guard cannot read (an unknown alias, a word it cannot
// tell) is the repository's git -C names: it lends another project nothing.
func TestModeGitChdirUnknownNeedsBothModes(t *testing.T) {
	t.Parallel()
	s := varSG(t, "jhon-mode-v3g-")
	s.askedOrRefused(t, s.expand(`git -C <OTHER> "$X"`), "unknown", sgOpts{}, "a super-yolo session, a normal project")
	equalStr(t, modeLog(t, s.state), "", "the modes' log")
	s.allowedBy(t, s.expand(`git -C <P> "$X"`), "super-yolo", sgOpts{}, "the session's own project")
}

// S31/L187: more B1/B2 variants (T51 verify #4, L189).
func TestModeOwnScriptAndFunsubVariantsNeedBothModes(t *testing.T) {
	t.Parallel()
	s := varSG(t, "jhon-mode-v4-")
	support.Write(t, s.p, map[string]string{"scripts/aws.sh": "#!/bin/sh\naws ecs run-task --cluster c\n"})
	for _, c := range []struct{ command, cls string }{
		{"cd <OTHER> && timeout 5 bash <P>/scripts/aws.sh", "deploy"},
		{"env --chdir=<OTHER> bash <P>/scripts/aws.sh", "deploy"},
		{"x=${ y=${| cd <OTHER>; }; } && aws ecs run-task --cluster c", "deploy"},
		{"y=$(x=${ cd <OTHER>; }; aws ecs run-task --cluster c)", "deploy"},
		{"declare x=${ cd <OTHER>; }; aws ecs run-task --cluster c", "deploy"},
		{"cd <OTHER>; x=${ ( cd <P> ); }; aws ecs run-task --cluster c", "deploy"},
		{`x=${ cd <OTHER>; } && git -C . "$X"`, "unknown"},
	} {
		s.askedOrRefused(t, s.expand(c.command), c.cls, sgOpts{}, c.command)
	}
	equalStr(t, modeLog(t, s.state), "", "the modes' log")
}
