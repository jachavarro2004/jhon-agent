// Package vcs is the VCS state of a project root
// (contracts/vocabulary.yaml, "Version control").
//
// `jhon init --json` reports the state, `jhon status` notes it below
// `ready`, and `jhon doctor` warns about it; nothing gates on it. Nothing
// here ever initialises a repository or commits; that happens only with the
// user's consent.
//
// Detection is deterministic and read-only: git commands against the root
// that read the repository and never write it, run without the caller's
// GIT_* variables (project.ReadGitCmd). The vocabulary values are read on
// first use, so a broken vocabulary fails the command that needs them, not
// every command of the binary.
package vcs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

type vocabulary struct{ none, noHead, ready, candidateUnknown, note string }

var vocab = sync.OnceValue(func() vocabulary {
	states := contract.MustVocabulary("vcs_state", 3)
	_, notes := contract.Map(contract.MustContract("vocabulary.yaml"), "vcs_notes")
	if notes["note"] == "" {
		panic(errors.New("contracts/vocabulary.yaml: vcs_notes must declare note"))
	}
	return vocabulary{states[0], states[1], states[2], contract.MustVocabulary("candidate_unknown", 1)[0], notes["note"]}
})

// None is the vcs_state value for a root outside any git work tree.
func None() string { return vocab().none }

// NoHead is the vcs_state value for a repository without a commit.
func NoHead() string { return vocab().noHead }

// Ready is the vcs_state value for a repository whose HEAD resolves.
func Ready() string { return vocab().ready }

// CandidateUnknown is the candidate_unknown value: what git could not answer.
func CandidateUnknown() string { return vocab().candidateUnknown }

// Vcs is a root's state, one of the vcs_state values, and why.
type Vcs struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
}

// result is one git run: its stdout, whether it exited 0, and the error
// that kept it from running at all.
type result struct {
	out   string
	ok    bool
	spawn error
}

func git(root string, args ...string) result { return gitEnv(nil, root, args...) }

// runner runs git in root with args and env added to its environment:
// gitEnv, or a Reading's, which keeps its objects apart and stops with its
// context.
type runner func(env []string, root string, args ...string) result

func gitEnv(env []string, root string, args ...string) result {
	c := project.ReadGitCmd(root, args...)
	c.Env = append(c.Env, env...)
	return outcome(c.Output())
}

// outcome is a git run's result from its output and error.
func outcome(out []byte, err error) result {
	var exit *exec.ExitError
	switch {
	case err == nil:
		return result{out: string(out), ok: true}
	case errors.As(err, &exit):
		return result{out: string(out)}
	case errors.Is(err, exec.ErrNotFound):
		// node's spawnSync error for a missing program.
		return result{spawn: errors.New("spawnSync git ENOENT")}
	default:
		return result{spawn: err}
	}
}

// State is the VCS state of root, per the vocabulary's three values.
func State(root string) Vcs {
	root = project.Resolve("", root)
	inside := git(root, "rev-parse", "--is-inside-work-tree")
	// git missing, or not runnable, is reported as none: LDD cannot rely on a
	// history nobody can read.
	if inside.spawn != nil {
		return Vcs{None(), "git could not be run: " + inside.spawn.Error()}
	}
	if !inside.ok || strings.TrimSpace(inside.out) != "true" {
		return Vcs{None(), root + " is not inside a git work tree"}
	}
	head := git(root, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if !head.ok {
		return Vcs{NoHead(), "the repository has no commit: HEAD does not resolve"}
	}
	sha := strings.TrimSpace(head.out)
	return Vcs{Ready(), "HEAD resolves to " + sha[:min(12, len(sha))]}
}

var shaRe = lazyre.New(`^[0-9a-f]{40,64}$`)

// HeadSha is the full sha HEAD resolves to in root, or "" below `ready`.
// `jhon context` reports it as `head` so a launch context names the commit
// it was resolved at.
func HeadSha(root string) string {
	head := git(project.Resolve("", root), "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	sha := ""
	if head.ok {
		sha = strings.TrimSpace(head.out)
	}
	if !shaRe.MatchString(sha) {
		return ""
	}
	return sha
}

// Candidate is the revision a test run or a delivery names. Clean is a bool,
// or CandidateUnknown() when git cannot answer.
type Candidate struct {
	Commit string `json:"commit"`
	Tree   string `json:"tree"`
	Clean  any    `json:"clean"`
}

// toolData is the pathspec that leaves a tool's own data out of what git
// reads, from the git top level down (layout.ToolData, T268): CodeGraph's
// index, jhon-studio's output and Engram's data are never project input,
// whatever the project's .gitignore says, and git never reads their bytes.
func toolData() []string { return append([]string{":/"}, layout.ToolDataExcludes()...) }

// CandidateOf is the candidate of root (contracts/ldd.md, "Tested
// revision"): HEAD's commit, HEAD's tree, and whether the working tree is
// clean. `git status --porcelain --untracked-files=all` excludes no tracked
// or untracked path but a tool's own data (toolData) and those in exempt
// (paths relative to the git top level: a change's own test and delivery
// records, written after the candidate is fixed), so any other uncommitted
// byte makes it unclean. Outside a git repository, or below `ready`, each
// value is CandidateUnknown(). Read-only.
func CandidateOf(root string, exempt map[string]bool) Candidate {
	return candidateOf(gitEnv, root, exempt)
}

func candidateOf(run runner, root string, exempt map[string]bool) Candidate {
	root = project.Resolve("", root)
	u := CandidateUnknown()
	unknown := Candidate{u, u, u}
	commit := HeadSha(root)
	if commit == "" {
		return unknown
	}
	tree := git(root, "rev-parse", "--verify", "--quiet", "HEAD^{tree}")
	top := GitTopLevel(root)
	if !tree.ok || top == "" {
		return unknown
	}
	st := run(nil, top, append([]string{"status", "--porcelain=v1", "-z", "--untracked-files=all", "--"}, toolData()...)...)
	if !st.ok {
		return unknown
	}
	clean := true
	for _, p := range StatusPaths(st.out) {
		clean = clean && exempt[p]
	}
	return Candidate{commit, strings.TrimSpace(tree.out), clean}
}

// WorkingTree is the git tree the working state of root would commit to:
// HEAD's tree with every uncommitted edit and every untracked file git does
// not ignore, but not a tool's own data (toolData) nor the paths in exempt
// (relative to the git top level: a change's own records). The same bytes
// give the same tree, so it names what a test run tested even before a
// commit. It is built in a temporary index, a copy of the real one for its
// file stat cache, so the real index, HEAD, and every ref are untouched;
// like `git add`, it stores the working files' blobs, which nothing
// references and git's gc prunes, so the tree a test run records resolves
// later. A reader that only compares (status, a guard, a delivery check)
// reads it with a Reading, which writes nothing into the repository. ""
// outside a git repository, or when git cannot build it.
func WorkingTree(root string, exempt map[string]bool) string {
	top := GitTopLevel(project.Resolve("", root))
	if top == "" {
		return ""
	}
	tmp, err := os.CreateTemp("", "jhon-index-")
	if err != nil {
		return ""
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	tree, _ := workingTree(gitEnv, top, tmp.Name(), exempt)
	return tree
}

// workingTree is WorkingTree in top with run, its temporary index at index.
func workingTree(run runner, top, index string, exempt map[string]bool) (string, bool) {
	copyIndex(top, index)
	add := append([]string{"add", "-A", "--"}, toolData()...)
	for _, p := range slices.Sorted(maps.Keys(exempt)) {
		add = append(add, ":(top,exclude,literal)"+p)
	}
	in := func(args ...string) result { return run([]string{"GIT_INDEX_FILE=" + index}, top, args...) }
	if r := in(add...); !r.ok {
		return "", false
	}
	tree := in("write-tree")
	return strings.TrimSpace(tree.out), tree.ok && strings.TrimSpace(tree.out) != ""
}

// copyIndex copies top's real index to index, for its file stat cache, or
// leaves none there when the repository has none yet (git starts an empty
// one).
func copyIndex(top, index string) {
	os.Remove(index)
	idx := git(top, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if !idx.ok {
		return
	}
	real := strings.TrimSpace(idx.out)
	b, err := os.ReadFile(real)
	if err != nil {
		return
	}
	_ = os.WriteFile(index, b, 0o600)
	// The copy keeps the real index's mtime: git trusts an entry's
	// stat only when the file is older than the index, so a copy
	// dated now would take a same-size edit made in the second the
	// entry was written ("racy git") for the old content.
	if st, err := os.Stat(real); err == nil {
		_ = os.Chtimes(index, st.ModTime(), st.ModTime())
	}
}

// Frozen is a review's candidate (contracts/review-prism.md, "Freeze"): the
// base commit it is reviewed against, the working tree's git tree, and the
// subject, the SHA-256 of the binary diff from one to the other. Diff is
// that diff and Paths the paths it changes, for a review that carries them
// inline.
type Frozen struct {
	Base, Candidate, Subject string
	Diff                     string
	Paths                    []string
}

// Freeze is root's working state frozen for a review against the merge base
// of HEAD and baseRef. It is a function of the bytes: two facets freezing
// the same working state get the same Frozen, so each freezes for itself.
func Freeze(root, baseRef string) (Frozen, error) { return FreezeExempt(root, baseRef, nil) }

// FreezeExempt is Freeze leaving out the paths in exempt (relative to the
// git top level: a change's own records, written after the review), from
// the candidate and from the diff, so writing them leaves the subject as it
// was.
func FreezeExempt(root, baseRef string, exempt map[string]bool) (Frozen, error) {
	top := GitTopLevel(project.Resolve("", root))
	if top == "" {
		return Frozen{}, errors.New(root + " is not inside a git work tree")
	}
	base := git(top, "merge-base", "HEAD", baseRef)
	if !base.ok {
		return Frozen{}, fmt.Errorf("no merge base of HEAD and %s", baseRef)
	}
	f := Frozen{Base: strings.TrimSpace(base.out), Candidate: WorkingTree(top, exempt)}
	if f.Candidate == "" {
		return Frozen{}, errors.New("the working tree could not be written as a git tree")
	}
	spec := exemptSpec(exempt)
	diff, err := project.ReadGitCmd(top, append([]string{"diff", "--binary", f.Base, f.Candidate}, spec...)...).Output()
	if err != nil {
		return Frozen{}, fmt.Errorf("git diff %s %s: %v", f.Base, f.Candidate, err)
	}
	if len(diff) == 0 {
		return Frozen{}, &EmptyError{Dir: top, Base: baseRef, Worktrees: worktreesWithChanges(top, baseRef)}
	}
	sum := sha256.Sum256(diff)
	f.Subject = hex.EncodeToString(sum[:])
	f.Diff = string(diff)
	if names := git(top, append([]string{"diff", "--name-only", "--no-renames", f.Base, f.Candidate}, spec...)...); names.ok {
		f.Paths = lines(names.out)
	}
	return f, nil
}

// exemptSpec is the pathspec that leaves exempt out of a diff: none when
// exempt is empty, so the diff is the whole one.
func exemptSpec(exempt map[string]bool) []string {
	if len(exempt) == 0 {
		return nil
	}
	spec := []string{"--", "."}
	for _, p := range slices.Sorted(maps.Keys(exempt)) {
		spec = append(spec, ":(exclude,literal)"+p)
	}
	return spec
}

// Show is the content of path in tree (a frozen candidate), and whether the
// tree holds it.
func Show(root, tree, path string) ([]byte, bool) {
	out, err := project.ReadGitCmd(project.Resolve("", root), "cat-file", "blob", tree+":"+path).Output()
	return out, err == nil
}

// EmptyError is a freeze whose diff is empty: there is nothing to review in
// Dir against Base. Worktrees are the repository's other worktrees that
// hold changes (commits past the merge base, or uncommitted bytes), where
// the change most likely lives.
type EmptyError struct {
	Dir, Base string
	Worktrees []string
}

func (e *EmptyError) Error() string {
	msg := "nothing to freeze in " + e.Dir + ": the diff against " + e.Base + " is empty; run it from the change's worktree (`cd <worktree>` or `git -C <worktree>`)"
	if len(e.Worktrees) > 0 {
		msg += "\nworktrees with changes:\n  " + strings.Join(e.Worktrees, "\n  ")
	}
	return msg
}

// worktreesWithChanges are top's repository's other worktrees whose HEAD is
// past its merge base with baseRef or whose working tree is not clean, in
// `git worktree list` order. Read-only.
func worktreesWithChanges(top, baseRef string) []string {
	list := git(top, "worktree", "list", "--porcelain")
	if !list.ok {
		return nil
	}
	var out []string
	for _, l := range lines(list.out) {
		dir, ok := strings.CutPrefix(l, "worktree ")
		if !ok || dir == top {
			continue
		}
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			continue
		}
		head, base := git(dir, "rev-parse", "HEAD"), git(dir, "merge-base", "HEAD", baseRef)
		status := git(dir, append([]string{"status", "--porcelain", "--untracked-files=all", "--"}, toolData()...)...)
		if (head.ok && base.ok && strings.TrimSpace(head.out) != strings.TrimSpace(base.out)) || (status.ok && strings.TrimSpace(status.out) != "") {
			out = append(out, dir)
		}
	}
	return out
}

// A Worktree is a linked worktree of a repository: its path, and the
// branch checked out there ("" when its HEAD is detached).
type Worktree struct{ Path, Branch string }

// LinkedWorktrees are the linked worktrees of root's repository whose
// directory exists, in `git worktree list` order: never the main one.
// Read-only; nil when git cannot list them.
func LinkedWorktrees(root string) []Worktree {
	list := git(project.Resolve("", root), "worktree", "list", "--porcelain")
	if !list.ok {
		return nil
	}
	var out []Worktree
	for i, entry := range strings.Split(strings.TrimSpace(list.out), "\n\n") {
		var w Worktree
		for _, l := range lines(entry) {
			if p, ok := strings.CutPrefix(l, "worktree "); ok {
				w.Path = p
			} else if b, ok := strings.CutPrefix(l, "branch refs/heads/"); ok {
				w.Branch = b
			}
		}
		// The first entry is the main worktree.
		if i == 0 || w.Path == "" {
			continue
		}
		if st, err := os.Stat(w.Path); err == nil && st.IsDir() {
			out = append(out, w)
		}
	}
	return out
}

// StatusPaths are the paths of `git status --porcelain=v1 -z` output; a rename or copy names both sides.
func StatusPaths(out string) []string {
	fields := strings.Split(out, "\x00")
	var paths []string
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		paths = append(paths, f[3:])
		if f[0] == 'R' || f[0] == 'C' {
			i++
			if i < len(fields) {
				paths = append(paths, fields[i])
			}
		}
	}
	return slices.DeleteFunc(paths, func(p string) bool { return p == "" })
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// TreeDiff is the paths whose content differs between trees a and b in
// root; ok is false when either does not resolve. It reads nothing from the
// working tree, and names a tool's own data too: a reader leaves it out by
// layout.ToolData (a tree an earlier jhon recorded holds it).
func TreeDiff(root, a, b string) (paths []string, ok bool) { return treeDiff(gitEnv, root, a, b) }

func treeDiff(run runner, root, a, b string) (paths []string, ok bool) {
	r := run(nil, project.Resolve("", root), "diff", "--name-only", "--no-renames", a, b, "--")
	if !r.ok {
		return nil, false
	}
	return lines(r.out), true
}

// WorkingDiff is the paths, relative to the git top level, whose content
// differs between tree and the working state: HEAD plus every uncommitted
// change, tracked or untracked (what git ignores is never in a tree),
// sorted; ok is false when the tree does not resolve. It lists names and
// hashes nothing; a reader leaves a tool's own data out by layout.ToolData.
func WorkingDiff(root, tree string) (paths []string, ok bool) {
	top := GitTopLevel(root)
	if top == "" {
		return nil, false
	}
	d := git(top, "diff", "--name-only", "--no-renames", tree, "--")
	u := git(top, "ls-files", "--others", "--exclude-standard")
	if !d.ok || !u.ok {
		return nil, false
	}
	seen := map[string]bool{}
	for _, p := range append(lines(d.out), lines(u.out)...) {
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	slices.SortFunc(paths, js.Compare)
	return paths, true
}

// GitTopLevel is the top level of the git work tree enclosing from, or "" outside one.
func GitTopLevel(from string) string {
	top := git(project.Resolve("", from), "rev-parse", "--show-toplevel")
	if !top.ok {
		return ""
	}
	return strings.TrimSpace(top.out)
}

// Note is the note for a state below ready, filled from the vocabulary template.
func Note(v Vcs) string {
	return strings.ReplaceAll(strings.ReplaceAll(vocab().note, "{state}", v.State), "{reason}", v.Reason)
}
