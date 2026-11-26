package sandbox

import (
	"errors"
	"io/fs"

	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// maxGitFolderEntries bounds the walk of a repository's git folders
// (GitFolderProblem): past it the line runs in the sandbox.
const maxGitFolderEntries = 200000

// GitFolderProblem is why an outside git line may not run with the git
// folders git reports for a folder it works in, gitDir and commonDir
// (absolute: the same for a plain repository, a worktree's admin folder and
// the main .git for a linked one), "" when it may (T249, L40). git writes
// there outside the sandbox (FETCH_HEAD, the reflogs, the refs, the index,
// new objects), and a sandboxed command may have written the folders
// before: a link in them, or a file that shares its data with another (a
// hard link), would let that write land outside the project. So:
//
//   - each folder lies in one of the project's own folders (ownFolders,
//     real paths): a .git file or a commondir that names another
//     repository's is not the project's;
//   - no entry in either, walked without following a link and at most
//     maxGitFolderEntries, is a link, or anything but a folder or a regular
//     file (a FIFO git would wait on, a socket, a device);
//   - no regular file outside objects/ has more than one link: git rewrites
//     those in place or beside them. Objects are written as new files and
//     never in place, and `git clone --local` hard-links them, so they are
//     not judged so.
//
// The check is not racy: while the line runs, no sandboxed command that can
// write its project runs (Registry), and none outlives its own run.
func (in Input) GitFolderProblem(gitDir, commonDir string) string {
	var own []string
	for _, p := range in.ownFolders() {
		own = append(own, realPath(p, in.Stat))
	}
	why, _ := gitFolderWalk(gitDir, commonDir, own, "the project")
	return why
}

// The kinds of problem gitFolderWalk finds, which decide the fix a kept
// line's note gives (L1731 N1: by the problem's identity, never by its
// words).
const (
	walkFine      = iota
	walkElsewhere // a git folder outside the project or the repository
	walkPlanted   // a link, a hard link, a FIFO, a socket or a device
	walkTooBig    // past maxGitFolderEntries
	walkUnread    // a folder that cannot be read, a name that is not absolute
)

// gitFolderWalk is GitFolderProblem's check of gitDir and commonDir, which
// must lie in one of own (real paths), whose name whose says ("the
// project", "the repository <top>", T251), with the problem's kind.
func gitFolderWalk(gitDir, commonDir string, own []string, whose string) (string, int) {
	walked := 0
	for _, dir := range []string{gitDir, commonDir} {
		if !filepath.IsAbs(dir) {
			return "git names no absolute git folder (" + dir + ")", walkUnread
		}
		dir = filepath.Clean(dir)
		if real, err := filepath.EvalSymlinks(dir); err != nil || !within(real, own) {
			return "the repository's git folder " + dir + " is not in " + whose, walkElsewhere
		}
		problem, kind := "", walkPlanted
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if walked++; walked > maxGitFolderEntries {
				problem, kind = "the repository's git folders hold more than "+strconv.Itoa(maxGitFolderEntries)+" entries", walkTooBig
				return fs.SkipAll
			}
			if d.Type()&fs.ModeSymlink != 0 {
				problem = p + " in the repository's git folder is a link, which git would write through"
				return fs.SkipAll
			}
			if !d.IsDir() && !d.Type().IsRegular() {
				// A FIFO, a socket or a device: git would wait on one or
				// write to it (L41).
				problem = p + " in the repository's git folder is not a folder or a regular file"
				return fs.SkipAll
			}
			if d.IsDir() {
				return nil
			}
			if rel, _ := filepath.Rel(dir, p); rel == "objects" || strings.HasPrefix(rel, "objects"+string(filepath.Separator)) {
				return nil
			}
			fi, err := d.Info()
			if err != nil {
				return err
			}
			if st, ok := fi.Sys().(*syscall.Stat_t); !ok || uint64(st.Nlink) > 1 {
				problem = p + " in the repository's git folder shares its data with another file (a hard link), which git would write through"
				return fs.SkipAll
			}
			return nil
		})
		if problem != "" {
			return problem, kind
		}
		if err != nil && !errors.Is(err, fs.SkipAll) {
			return "the repository's git folder " + dir + " cannot be read (" + err.Error() + ")", walkUnread
		}
		if gitDir == commonDir {
			break
		}
	}
	return "", walkFine
}
