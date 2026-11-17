// LDD, "Slices": with `delivery: change`, a slice is delivered as a commit on
// the change's branch, so a commit that closes a slice commits the tree the
// change's last test run tested; the change's record is left out, since its
// tasks are checked off after the tests pass.

package ldd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestASliceCommitCommitsTheTestedTree(t *testing.T) {
	t.Parallel()
	root := workspace(t, "a", "true", "b", "true", "c", "true", "d", "true")
	config := filepath.Join(root, "openspec", "config.yaml")
	b, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, append(b, "delivery: change\n"...), 0o666); err != nil {
		t.Fatal(err)
	}
	open := "## Slice 1: a\n\nDelivers: CA-01\n\n- [ ] 1.1 a\n"
	support.Write(t, root, map[string]string{"openspec/changes/c1/tasks.md": open})
	support.GitRepo(t, root)
	support.Git(t, root, "checkout", "-q", "-B", "main")
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "base")
	support.Git(t, root, "checkout", "-q", "-b", "feat")
	support.Write(t, root, map[string]string{"openspec/changes/c1/proposal.md": "# c1\n"})
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "the change starts")

	commit := func() support.Result {
		payload, _ := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "git add -A && git commit -m 'slice 1'"},
			"cwd": root, "permission_mode": "default"})
		return support.Run(t, support.Cmd{Args: []string{support.Bin(t), "hook", "route-guard"}, Env: support.Env(), Stdin: string(payload)})
	}
	closed := "## Slice 1: a\n\nDelivers: CA-01\n\n- [x] 1.1 a\n"

	support.Write(t, root, map[string]string{"a/feature.go": "package a\n"})
	support.Write(t, root, map[string]string{"openspec/changes/c1/tasks.md": closed})
	r := commit()
	equal(t, r.Code, 2, "a slice closed with no test run")
	match(t, r.Stderr, `closes Slice 1: a of c1, and the change has no recorded test run`, r.Stderr)

	equal(t, jhonGit(t, root, "executions", "run", "c1").Code, 0, "the slice's tests pass")
	equal(t, commit().Code, 0, "the tested tree, its tasks checked off after the run")

	support.Write(t, root, map[string]string{"a/feature.go": "package a\n\nvar X = 1\n"})
	r = commit()
	equal(t, r.Code, 2, "a file changed after the run")
	if !regexp.MustCompile(`1 file\(s\) changed after its last test run \(a/feature\.go\)`).MatchString(r.Stderr) {
		t.Errorf("the refusal names the file: %s", r.Stderr)
	}

	// A commit that closes no slice passes, as does any commit without
	// delivery: change.
	support.Write(t, root, map[string]string{"openspec/changes/c1/tasks.md": open})
	equal(t, commit().Code, 0, "a commit inside a slice")
	support.Write(t, root, map[string]string{"openspec/changes/c1/tasks.md": closed})
	if err := os.WriteFile(config, b, 0o666); err != nil {
		t.Fatal(err)
	}
	equal(t, commit().Code, 0, "delivery by slice: the delivery check holds each one")
}

// A commit or a PR is judged at the repository it runs in (T140, L1085): a
// `cd` to another worktree, or git -C, moves it there from the session's
// root, and a target the guard cannot tell keeps the session root's answer.
// The case: the main checkout on a change's branch with a closed, untested,
// uncommitted slice, and the edit made in a linked worktree of its own.
func TestASliceCommitIsJudgedWhereItRuns(t *testing.T) {
	t.Parallel()
	root := workspace(t, "a", "true", "b", "true", "c", "true", "d", "true")
	config := filepath.Join(root, "openspec", "config.yaml")
	b, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, append(b, "delivery: change\n"...), 0o666); err != nil {
		t.Fatal(err)
	}
	open := "## Slice 1: a\n\nDelivers: CA-01\n\n- [ ] 1.1 a\n\n## Slice 2: b\n\nDelivers: CA-02\n\n- [ ] 2.1 b\n"
	support.Write(t, root, map[string]string{"openspec/changes/c1/tasks.md": open})
	support.GitRepo(t, root)
	support.Git(t, root, "checkout", "-q", "-B", "main")
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "base")
	support.Git(t, root, "checkout", "-q", "-b", "feat")
	support.Write(t, root, map[string]string{"openspec/changes/c1/proposal.md": "# c1\n"})
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "the change starts")
	// A sibling of the checkout, as a worktrees folder beside it.
	other := filepath.Join(filepath.Dir(root), "other")
	support.Git(t, root, "worktree", "add", "-q", "-b", "chore", other, "main")

	// The session's own slice closes, untested and uncommitted.
	support.Write(t, root, map[string]string{"a/feature.go": "package a\n",
		"openspec/changes/c1/tasks.md": "## Slice 1: a\n\nDelivers: CA-01\n\n- [x] 1.1 a\n\n## Slice 2: b\n\nDelivers: CA-02\n\n- [ ] 2.1 b\n"})
	// The edit in the other worktree, on its own branch.
	support.Write(t, other, map[string]string{"b/app.py": "print('other')\n"})

	run := func(command string) support.Result {
		payload, _ := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": command},
			"cwd": root, "permission_mode": "default"})
		return support.Run(t, support.Cmd{Args: []string{support.Bin(t), "hook", "route-guard"}, Env: support.Env(), Stdin: string(payload)})
	}
	refusedForC1 := func(r support.Result, why string) {
		t.Helper()
		equal(t, r.Code, 2, why, r.Stderr)
		match(t, r.Stderr, `closes Slice 1: a of c1`, why)
	}
	passes := func(r support.Result, why string) {
		t.Helper()
		equal(t, r.Code, 0, why, r.Stderr)
		noMatch(t, r.Stderr, `c1`, why)
	}

	// At the session root: as before (K4).
	refusedForC1(run("git add -A && git commit -m 'slice 1'"), "a commit at the session root")
	// K1: a cd to the other worktree commits there.
	passes(run("cd "+other+" && git add -A && git commit -m x"), "a commit in the other worktree")
	passes(run("cd ../other && git commit -m x; cd -"), "")
	// K2: git -C names the other worktree.
	passes(run("git -C "+other+" commit -am x"), "git -C the other worktree")
	// K3: a target the guard cannot tell keeps the session root's answer.
	refusedForC1(run(`cd "$X" && git commit -m x`), "a cd it cannot follow")
	refusedForC1(run(`git -C "$X" commit -m x`), "a git -C it cannot follow")
	refusedForC1(run("cd "+other+" && git commit -m x && cd "+root+" && git commit -m y"), "a commit at each: the session root's is judged too")
	refusedForC1(run("(cd "+other+") && git commit -m x"), "a subshell's cd does not move the commit")
	refusedForC1(run("bash -c 'cd "+other+"' && git commit -m x"), "nor does a shell's")
	passes(run("env -C "+other+" git commit -m x"), "env -C the other worktree")

	// A repository without LDD's layout is judged as a session rooted there
	// would be: no refusal from the session's change.
	plain := filepath.Join(filepath.Dir(root), "plain")
	support.Write(t, plain, map[string]string{"main.go": "package main\n"})
	support.GitRepo(t, plain)
	there := func(command string) support.Result {
		payload, _ := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": command},
			"cwd": plain, "permission_mode": "default"})
		return support.Run(t, support.Cmd{Args: []string{support.Bin(t), "hook", "route-guard"}, Env: support.Env(), Stdin: string(payload)})
	}
	for _, c := range [][2]string{{"cd " + plain + " && git commit -am x", "git commit -am x"},
		{"git -C " + plain + " commit -am x", "git commit -am x"},
		{"cd " + plain + " && gh pr create --title x", "gh pr create --title x"}} {
		r, want := run(c[0]), there(c[1])
		noMatch(t, r.Stderr, `c1`, c[0])
		equal(t, r.Code, want.Code, c[0], r.Stderr)
		equal(t, r.Stderr, want.Stderr, c[0])
		equal(t, r.Stdout, want.Stdout, c[0])
	}

	// K2: a PR from the other worktree waits for no task of the session's change.
	r := run("gh pr create --title x")
	equal(t, r.Code, 2, "the session's change has an unchecked task", r.Stderr)
	match(t, r.Stderr, `c1 has 1 unchecked task`, r.Stderr)
	// It is judged by the other worktree's branch, which carries an
	// unrecorded edit and no change of its own.
	r = run("cd " + other + " && gh pr create --title x")
	noMatch(t, r.Stderr, `c1|unchecked task`, "a PR from the other worktree")
	match(t, r.Stderr, `the branch carries no change with its record`, r.Stderr)
	r = run("cd " + other + " && gh pr create -R o/r --title x")
	equal(t, r.Code, 2, "a PR gh is given its repository for", r.Stderr)
	match(t, r.Stderr, `c1 has 1 unchecked task`, r.Stderr)
	r = run(`cd "$X" && gh pr create --title x`)
	equal(t, r.Code, 2, "a PR from a directory it cannot tell", r.Stderr)
	match(t, r.Stderr, `c1 has 1 unchecked task`, r.Stderr)

	// K1: the other worktree's own closed, untested slice refuses its commit.
	support.Write(t, other, map[string]string{"openspec/changes/c2/tasks.md": "## Slice 1: c\n\nDelivers: CA-03\n\n- [ ] 1.1 c\n"})
	support.Git(t, other, "add", "-A")
	support.Git(t, other, "commit", "-q", "-m", "c2 starts")
	support.Write(t, other, map[string]string{"openspec/changes/c2/tasks.md": "## Slice 1: c\n\nDelivers: CA-03\n\n- [x] 1.1 c\n"})
	r = run("cd " + other + " && git commit -am x")
	equal(t, r.Code, 2, "the other worktree's own slice", r.Stderr)
	match(t, r.Stderr, `closes Slice 1: c of c2`, r.Stderr)
	noMatch(t, r.Stderr, `of c1`, r.Stderr)
	r = run("git -C " + other + " commit -am x")
	equal(t, r.Code, 2, "the other worktree's own slice, by git -C", r.Stderr)
	match(t, r.Stderr, `closes Slice 1: c of c2`, r.Stderr)
}

// T140 K1/K3, reverse direction: a session in the linked worktree commits in
// the main checkout, and a cd outside any repository keeps the session root's
// answer.
func TestASliceCommitFromAWorktreeIsJudgedAtTheMainCheckout(t *testing.T) {
	t.Parallel()
	root := workspace(t, "a", "true")
	config := filepath.Join(root, "openspec", "config.yaml")
	b, _ := os.ReadFile(config)
	_ = os.WriteFile(config, append(b, "delivery: change\n"...), 0o666)
	support.Write(t, root, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n\nDelivers: CA-01\n\n- [ ] 1.1 a\n\n## Slice 2: b\n\nDelivers: CA-02\n\n- [ ] 2.1 b\n"})
	support.GitRepo(t, root)
	support.Git(t, root, "checkout", "-q", "-B", "main")
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "base")
	support.Git(t, root, "checkout", "-q", "-b", "feat")
	other := filepath.Join(filepath.Dir(root), "other")
	support.Git(t, root, "worktree", "add", "-q", "-b", "chore", other, "main")
	support.Write(t, root, map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n\nDelivers: CA-01\n\n- [x] 1.1 a\n\n## Slice 2: b\n\nDelivers: CA-02\n\n- [ ] 2.1 b\n"})
	run := func(cwd, command string) support.Result {
		payload, _ := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": command}, "cwd": cwd, "permission_mode": "default"})
		return support.Run(t, support.Cmd{Args: []string{support.Bin(t), "hook", "route-guard"}, Env: support.Env(), Stdin: string(payload)})
	}
	for _, c := range []string{"cd " + root + " && git commit -am x", "git -C " + root + " commit -am x",
		"cd " + other + " && git --git-dir=" + root + "/.git --work-tree=" + root + " commit -m x"} {
		r := run(other, c)
		equal(t, r.Code, 2, c, r.Stderr)
		match(t, r.Stderr, `closes Slice 1: a of c1`, c)
	}
	r := run(root, "cd "+os.TempDir()+" && git commit -m x")
	equal(t, r.Code, 2, "outside any repository", r.Stderr)
	match(t, r.Stderr, `closes Slice 1: a of c1`, r.Stderr)
}
