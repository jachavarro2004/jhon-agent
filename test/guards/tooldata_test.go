// T268 (L1762): a tool's own data in a workspace member (CodeGraph's index,
// 2.4 GB in the owner's) is never project input: route-guard's PR check and
// `jhon status` leave it out of the member's tree, read it without writing
// into the member's repository, and a tree recorded with it in, by an
// earlier jhon, is the tested tree once it is ignored. An untracked tree
// over the budget a check hashes is named, with what to ignore. Ignoring a
// folder goes to .gitignore, which the guards allow; a write to
// .git/info/exclude stays refused, and its refusal says so.

package guards

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// memberPR is newWorkspace with lib joined and change c, which has a slice
// in lib, on branch feat/c in both, and the primary's PR naming lib's.
func memberPR(t *testing.T) (w wsFixture, pr func() support.Result) {
	t.Helper()
	w = newWorkspace(t, "openspec/config.yaml")
	support.Write(t, w.app, map[string]string{"openspec/config.yaml": "workspace:\n  lib: ../lib\n"})
	support.Git(t, w.app, "add", "-A")
	support.Git(t, w.app, "commit", "-q", "-m", "ldd")
	w.join(t, w.lib)
	support.Git(t, w.app, "checkout", "-q", "-b", "feat/c")
	support.Write(t, w.app, map[string]string{"openspec/changes/c/tasks.md": "## Slice 1: lib side\n\nRepo: lib\n\n- [x] lib task\n"})
	support.Git(t, w.app, "add", "-A")
	support.Git(t, w.app, "commit", "-q", "-m", "c")
	support.Git(t, w.lib, "checkout", "-q", "-b", "feat/c")
	support.Git(t, w.lib, "remote", "add", "origin", "git@github.com:acme/lib.git")
	return w, func() support.Result {
		t.Helper()
		return hook(t, w.env, "route-guard", nil, map[string]any{"tool_name": "Bash", "cwd": w.app, "permission_mode": "default",
			"tool_input": cmd("gh pr create --title x --body 'see https://github.com/acme/lib/pull/7'")})
	}
}

// recordMemberRun writes the primary's record of a passing run of lib that
// tested tree.
func recordMemberRun(t *testing.T, w wsFixture, tree string) {
	t.Helper()
	support.Write(t, w.app, map[string]string{"openspec/changes/c/execution-record.json": `{"repos":{"lib":{"working_tree":"` + tree + `","after_working_tree":"` + tree +
		`","executions":[{"project":"src","command":"true","cwd":"src","exit_code":0,"status":"pass","duration_ms":1}],"passed":true,"complete":true}}}` + "\n"})
}

// objectFiles are the files of dir's repository's object directory.
func objectFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(filepath.Join(dir, ".git", "objects"), func(p string, e os.DirEntry, err error) error {
		// git's own lock files come and go: a commit starts `git maintenance
		// run --auto` detached, which holds objects/maintenance.lock a moment.
		if err == nil && !e.IsDir() && !strings.HasSuffix(p, ".lock") {
			out = append(out, p)
		}
		return nil
	})
	slices.Sort(out)
	return out
}

func TestRouteGuardPrimaryPRLeavesAMembersToolDataOut(t *testing.T) {
	t.Parallel()
	w, pr := memberPR(t)
	support.Write(t, w.lib, map[string]string{".codegraph/codegraph.db": "index\n", ".codegraph/.gitignore": "*.tmp\n"})
	// The tree an earlier jhon recorded for lib: its HEAD with the index in.
	index := filepath.Join(t.TempDir(), "index")
	env := support.Env("GIT_INDEX_FILE=" + index)
	for _, args := range [][]string{{"read-tree", "HEAD"}, {"add", "-A"}} {
		if r := support.Run(t, support.Cmd{Args: append([]string{"git", "-C", w.lib}, args...), Env: env}); r.Code != 0 {
			t.Fatalf("git %v: %s", args, r.Stderr)
		}
	}
	old := support.Run(t, support.Cmd{Args: []string{"git", "-C", w.lib, "write-tree"}, Env: env})
	recordMemberRun(t, w, strings.TrimSpace(old.Stdout))
	before := objectFiles(t, w.lib)
	// CodeGraph writes on: bytes no git object holds yet.
	support.Write(t, w.lib, map[string]string{".codegraph/later.db": "more index\n"})

	// The index untracked and not ignored: it is no input of lib's, so lib
	// is clean and its HEAD the tested tree.
	r := pr()
	code(t, r, 0, "a primary PR whose member holds CodeGraph's index, untracked")
	s := support.Jhon(t, w.app, w.env, "status", "c", "--json")
	if !strings.Contains(s.Stdout, `"members": [`+"\n"+`    {`+"\n"+`      "name": "lib",`+"\n"+`      "ready": true,`) {
		t.Errorf("status with the member's index untracked: exit %d\n%s%s", s.Code, s.Stdout, s.Stderr)
	}
	// The owner ignores it (L1762: then the PR was refused, "3378 path(s)
	// differ (.codegraph/.gitignore, ...").
	support.Write(t, w.lib, map[string]string{".git/info/exclude": ".codegraph/\n"})
	r = pr()
	code(t, r, 0, "a primary PR once the member's index is ignored")
	if strings.Contains(r.Stderr, "path(s) differ") {
		t.Errorf("the recorded tree's index counts as a difference: %s", r.Stderr)
	}
	// Neither the guard nor status wrote into lib's repository.
	if got := objectFiles(t, w.lib); !slices.Equal(got, before) {
		t.Errorf("lib's objects: %d files before, %d after the PR checks and status", len(before), len(got))
	}
}

func TestRouteGuardPrimaryPRNamesAMembersUntrackedOutputOverTheBudget(t *testing.T) {
	t.Parallel()
	w, pr := memberPR(t)
	recordMemberRun(t, w, strings.TrimSpace(support.Git(t, w.lib, "rev-parse", "HEAD^{tree}")))
	code(t, pr(), 0, "lib clean at its tested HEAD")
	// Build output nobody ignores, over the 10000 files a check hashes.
	out := mkdir(t, filepath.Join(w.lib, "out-1"))
	for i := range 10001 {
		if err := os.WriteFile(filepath.Join(out, fmt.Sprintf("f%05d", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := pr()
	code(t, r, 2, "a primary PR whose member holds 10001 untracked files")
	want := "member lib is not ready (the working tree is not clean (commit or remove the uncommitted changes), and the working tree holds 10001 untracked files (0 B) git does not ignore, over the 10000 files a check hashes in its time: out-1/; when they are build or test output, add them to .gitignore (out-1/), else commit them; order: a passing run (`jhon executions run`), commit the tree it tested, then `jhon deliver check`)"
	if !strings.Contains(r.Stderr, want) {
		t.Errorf("over the budget:\n%s\nwant it to hold:\n%s", r.Stderr, want)
	}
}

func TestIgnoringAFolderGoesToGitignore(t *testing.T) {
	t.Parallel()
	w := newWorkspace(t, "openspec/config.yaml")
	guards := []string{"--agent", "*", "safety-guard", "vcs-guard", ".git", "route-guard"}
	call := func(tool string, input map[string]any) support.Result {
		t.Helper()
		return hook(t, w.env, "pre-tool", guards, map[string]any{"hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": input,
			"cwd": w.lib, "permission_mode": "default"})
	}
	gitignore := filepath.Join(w.lib, ".gitignore")
	fix := "; to leave untracked files out of git, write the same lines to " + gitignore + " instead (`>> " + gitignore + "`, or an Edit of it), which is allowed"
	// The owner's agent's line (L1762), refused as every write into .git is.
	r := call("Bash", cmd(`printf '.codegraph/\nout-*/\n' >> `+filepath.Join(w.lib, ".git", "info", "exclude")))
	code(t, r, 2, "a write to .git/info/exclude")
	if !strings.Contains(r.Stderr, "it writes into the VCS directory .git"+fix+".") {
		t.Errorf("the exclude refusal names no fix that runs:\n%s", r.Stderr)
	}
	r = call("Write", map[string]any{"file_path": filepath.Join(w.lib, ".git", "info", "exclude"), "content": ".codegraph/\n"})
	code(t, r, 2, "a Write of .git/info/exclude")
	if !strings.Contains(r.Stderr, "no one writes there through Write"+fix) {
		t.Errorf("the Write refusal names no fix that runs:\n%s", r.Stderr)
	}
	// The fix it names runs: a line added to the project's .gitignore.
	code(t, call("Bash", cmd(`printf '.codegraph/\n' >> .gitignore`)), 0, "printf >> .gitignore")
	code(t, call("Bash", cmd(`printf '.codegraph/\nout-*/\n' >> `+gitignore)), 0, "printf >> <member>/.gitignore")
	code(t, call("Edit", map[string]any{"file_path": gitignore, "old_string": "", "new_string": ".codegraph/\n"}), 0, "an Edit of .gitignore")
}
