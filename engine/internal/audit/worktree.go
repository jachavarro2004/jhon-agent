package audit

import (
	"os"
	"path/filepath"
	"strings"
)

// A session's repository may be checked out more than once: `git worktree
// add` puts a lane's tree beside the main one (<repo>-worktrees/<lane>), and
// the workers write there while the session's directory stays the main tree.
// Read from git's own files, never by running git.

// pathIn is the path a git file holds, relative to dir when not absolute.
func pathIn(dir string, b []byte) string {
	p := strings.TrimSpace(string(b))
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	return filepath.Clean(p)
}

// commonDir is the git directory every worktree of top's repository shares:
// `.git` itself in the main tree, and a worktree's gitdir's `commondir`; ""
// when top holds no `.git`.
func commonDir(top string) string {
	dot := filepath.Join(top, ".git")
	st, err := os.Stat(dot)
	switch {
	case err != nil:
		return ""
	case st.IsDir():
		return dot
	}
	b, err := os.ReadFile(dot)
	d, ok := strings.CutPrefix(string(b), "gitdir:")
	if err != nil || !ok {
		return ""
	}
	gitdir := pathIn(top, []byte(d))
	if c, err := os.ReadFile(filepath.Join(gitdir, "commondir")); err == nil {
		return pathIn(gitdir, c)
	}
	return gitdir
}

// worktreeTops are the tops of every worktree of the repository whose
// common git directory is common: the main tree first, then each linked
// worktree that still exists, by name; none for no directory.
func worktreeTops(common string) []string {
	var out []string
	if common == "" {
		return out
	}
	if filepath.Base(common) == ".git" {
		out = append(out, filepath.Dir(common))
	}
	readStat(filepath.Join(common, "worktrees"))
	entries, _ := os.ReadDir(filepath.Join(common, "worktrees"))
	for _, e := range entries {
		// A linked worktree's `gitdir` names its `.git` file.
		dir := filepath.Join(common, "worktrees", e.Name())
		readStat(filepath.Join(dir, "gitdir"))
		if b, err := os.ReadFile(filepath.Join(dir, "gitdir")); err == nil {
			top := filepath.Dir(pathIn(dir, b))
			readExists(top)
			if exists(top) {
				addOnce(&out, top)
			}
		}
	}
	return out
}

// worktreesOf is dir and the same directory in every other worktree of its
// repository; dir alone outside a repository.
func worktreesOf(dir string) []string {
	out := []string{dir}
	top := repoOf(filepath.Join(dir, "x"))
	if top == "" {
		return out
	}
	rel, err := filepath.Rel(top, dir)
	if err != nil {
		return out
	}
	for _, t := range worktreeTops(commonDir(top)) {
		addOnce(&out, filepath.Join(t, rel))
	}
	return out
}
