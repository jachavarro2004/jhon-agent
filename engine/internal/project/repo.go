package project

import (
	"os"
	"path/filepath"
	"strings"
)

// A Repo is where a directory's project is for the OS sandbox (T128a): its
// top folder, the folder that holds the `.git` entry, and the git common dir
// when the entry proves which one it is; both real paths. Common is "" where
// nothing proves it: no folder outside the top is the project's then.
type Repo struct{ Top, Common string }

// ReadRepo is the Repo of dir read from the disk, with no git process: the
// OS sandbox asks on every command, where a git process costs as much as the
// sandbox itself, and a program found on PATH is one a sandboxed command may
// have planted. It walks up from dir to the first folder that holds a `.git`
// entry; a git folder itself (a bare repository, inside `.git`) has no work
// tree; the walk stops at a file system boundary. The zero Repo is no
// project, as Root's "": no repository, or one whose top is / or the home.
//
// A sandboxed command may write everything under the top, `.git` included,
// so nothing written there is trusted to name a folder outside it (L888):
// core.worktree, config includes and GIT_* variables are not read, and a
// `.git` file gives a common dir only where that side points back at it
// (linkedCommon). A `.git` file that is not pointed back at (a submodule's,
// a separate git dir's) and a `.git` symlink still make their folder the top,
// with no common dir.
func ReadRepo(dir string) Repo {
	d := Realish(dir)
	for !Exists(d) && filepath.Dir(d) != d {
		d = filepath.Dir(d)
	}
	start, ok := device(d)
	if !ok {
		return Repo{}
	}
	for {
		if common, found := entryOf(d); found {
			if d == string(filepath.Separator) || d == Realish(Home()) {
				return Repo{}
			}
			return Repo{Top: d, Common: common}
		}
		if isGitDir(d) {
			return Repo{}
		}
		up := filepath.Dir(d)
		if up == d {
			return Repo{}
		}
		if at, ok := device(up); !ok || at != start {
			return Repo{}
		}
		d = up
	}
}

// RepoAt is the Repo whose top is dir, whatever is above it and whether or
// not dir still holds a `.git` entry: the caller pinned the project when the
// session started, and a sandboxed command may have removed the entry since.
// The common dir is what dir's own `.git` proves, as for ReadRepo. The zero
// Repo for / and the home.
func RepoAt(dir string) Repo {
	d := Realish(dir)
	if d == string(filepath.Separator) || d == Realish(Home()) {
		return Repo{}
	}
	common, _ := entryOf(d)
	return Repo{Top: d, Common: common}
}

// Roots are the repository's project roots, as Roots gives them for a
// directory in it; none for no project.
func (r Repo) Roots() []string {
	if r.Top == "" {
		return nil
	}
	return rootsOf(r.Top, mainOf(r.Common))
}

// Worktrees are the repository's linked worktrees: each folder a
// <common>/worktrees/<name>/gitdir file names whose own `.git` file points
// back at that admin folder, but / and the home. The gitdir files are inside
// the project, where a sandboxed command may write: one that names a folder
// whose `.git` does not point back lists nothing (L888).
func (r Repo) Worktrees() []string {
	if r.Common == "" {
		return nil
	}
	entries, _ := os.ReadDir(filepath.Join(r.Common, "worktrees"))
	var out []string
	for _, e := range entries {
		// The admin folder as the common dir holds it, never where a link
		// in its place leads: another repository's would be pointed back at.
		admin := filepath.Join(r.Common, "worktrees", e.Name())
		if fi, err := os.Lstat(admin); err != nil || !fi.IsDir() {
			continue
		}
		dot, ok := target(filepath.Join(admin, "gitdir"), "")
		if !ok {
			continue
		}
		if fi, err := os.Lstat(dot); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if back, ok := target(dot, "gitdir:"); !ok || back != admin {
			continue
		}
		if w := filepath.Dir(dot); w != string(filepath.Separator) && w != Realish(Home()) {
			out = append(out, w)
		}
	}
	return out
}

// entryOf is whether d holds a `.git` entry, and the common dir it proves: a
// git folder is its own; a `.git` file's is linkedCommon's.
func entryOf(d string) (common string, found bool) {
	dot := filepath.Join(d, ".git")
	fi, err := os.Lstat(dot)
	if err != nil {
		return "", false
	}
	switch {
	case fi.IsDir():
		return dot, isGitDir(dot)
	case fi.Mode().IsRegular():
		if _, ok := target(dot, "gitdir:"); !ok {
			return "", false
		}
		return linkedCommon(dot), true
	}
	// A link: the folder is the top, and where the link leads proves nothing.
	return "", Exists(dot)
}

// linkedCommon is the common dir of the linked worktree whose `.git` file is
// dot: the file names an admin folder <common>/worktrees/<name>, and that
// folder's gitdir file points back at dot. "" for any other `.git` file.
func linkedCommon(dot string) string {
	admin, _ := target(dot, "gitdir:")
	worktrees := filepath.Dir(admin)
	if filepath.Base(worktrees) != "worktrees" {
		return ""
	}
	if back, ok := target(filepath.Join(admin, "gitdir"), ""); !ok || back != dot {
		return ""
	}
	return filepath.Dir(worktrees)
}

// target is the real path the first line of file names after prefix,
// relative to the file's folder; ok is false when the file cannot be read or
// its line lacks the prefix or the path.
func target(file, prefix string) (string, bool) {
	b, err := os.ReadFile(file)
	if err != nil {
		return "", false
	}
	line, _, _ := strings.Cut(string(b), "\n")
	p, ok := strings.CutPrefix(strings.TrimSpace(line), prefix)
	if p = strings.TrimSpace(p); !ok || p == "" {
		return "", false
	}
	return Realish(Resolve(filepath.Dir(file), p)), true
}

// commonOf is a git folder's common dir: where its commondir file leads (a
// linked worktree's), or itself.
//
// The path is the file's as git reads it: only its trailing newline and
// carriage returns are dropped, never other white space (L41), so the
// folder judged is the one git uses, not a decoy beside it whose name lacks
// the white space.
func commonOf(gitdir string) string {
	b, err := os.ReadFile(filepath.Join(gitdir, "commondir"))
	if err != nil {
		return gitdir
	}
	return Realish(Resolve(gitdir, strings.TrimRight(string(b), "\r\n")))
}

// isGitDir is git's is_git_directory: a HEAD, and the objects and refs
// folders of its common dir. It only tells a git folder from another one; no
// root comes from what it reads.
func isGitDir(d string) bool {
	if _, err := os.Lstat(filepath.Join(d, "HEAD")); err != nil {
		return false
	}
	common := commonOf(d)
	for _, name := range []string{"objects", "refs"} {
		if fi, err := os.Stat(filepath.Join(common, name)); err != nil || !fi.IsDir() {
			return false
		}
	}
	return true
}
