package ldd

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
)

// Archive is `jhon archive <change-name> <YYYY-MM-DD>`, run from the project
// root: it moves openspec/changes/<change-name> to
// openspec/changes/archive/<YYYY-MM-DD>-<change-name> mechanically, so file
// content never passes through a model (the LDD archive phase, step 4).
//
// The method is decided before anything moves, from the index, never from a
// failed attempt: a change directory holding a tracked file moves with
// `git mv` (which carries its untracked files and keeps the tracked ones
// tracked), one holding none, or outside a repository, with a rename. A
// failed `git mv` is final: git's own message passes through and nothing
// falls back. An existing destination is refused before any move. The
// source is snapshotted first, must be gone after the move, and the
// destination must equal the snapshot (the readback).
//
// A change with no RED before GREEN on record is refused before anything
// moves (T223): the fact `jhon status --json` reports as
// tests.red_before_green (execution.RedBeforeGreenOnRecord, a workspace
// member's included). Only a change that alters no behaviour is archived
// without it, while its diff against the default branch is not empty
// (noBehaviourApplied, as status reads it): its route records no tests by
// design; an empty diff, after its merge, earns no exemption.
//
// Exit status: 0 moved and verified; 1 refused or readback failed; 2 usage;
// otherwise the status of the failed git command.
func Archive(args []string, stdout, stderr io.Writer) int {
	usage := func() int {
		fmt.Fprintln(stderr, "usage: jhon archive <change-name> <YYYY-MM-DD>  (run from the project root)")
		return 2
	}
	if len(args) != 2 || !changeName.MatchString(args[0]) || args[0] == "archive" || !isoDay.MatchString(args[1]) {
		return usage()
	}
	change, date := args[0], args[1]
	l := layout.For(".")
	source := filepath.FromSlash(l.ChangeDir(change))
	destination := filepath.FromSlash(l.Archive(date, change))

	if fi, err := os.Lstat(source); err != nil || !fi.IsDir() {
		fmt.Fprintf(stderr, "archive source %s is not a directory; nothing was moved.\n", source)
		return 1
	}
	if _, err := os.Lstat(destination); err == nil {
		fmt.Fprintf(stderr, "archive destination collision: source %s and destination %s remain unchanged. Resolve the destination collision, then rerun this archive step.\n", source, destination)
		return 1
	}
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !execution.RedBeforeGreenOnRecord(root, change) {
		g := gitReads{}
		if !g.noBehaviourApplied(root, g.routeAt(root)) {
			fmt.Fprintln(stderr, NoRedGreenRefusal(root, change))
			return 1
		}
		fmt.Fprintf(stdout, "no RED before GREEN needed: %s alters no behaviour (every file outside %s/ that differs from the default branch is a test or a document)\n", change, layout.For(root).Dir())
	}

	// Outside a work tree nothing can be tracked. Inside one, a failing
	// `git ls-files` is an error to report, not evidence of "untracked".
	method := "mv"
	// jhon's own git runs no program the repository's configuration names
	// (project.ReadGitCmd, T250): a sandboxed command may have written it.
	inside := project.ReadGitCmd(".", "rev-parse", "--is-inside-work-tree")
	if inside.Run() == nil {
		ls := project.ReadGitCmd(".", "ls-files", "--", source)
		ls.Stderr = stderr
		out, err := ls.Output()
		if err != nil {
			code := exitCode(err)
			fmt.Fprintf(stderr, "git ls-files failed with status %d; nothing was moved.\n", code)
			return code
		}
		if len(bytes.TrimSpace(out)) > 0 {
			method = "git-mv"
		}
	}

	snapshot, err := os.MkdirTemp("", "ldd-archive.")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer os.RemoveAll(snapshot)
	copyRoot := filepath.Join(snapshot, "source")
	if err := copyTree(source, copyRoot); err != nil {
		fmt.Fprintf(stderr, "snapshot of %s failed: %v; nothing was moved.\n", source, err)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o777); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	fmt.Fprintf(stdout, "method: %s\n", method)
	if method == "git-mv" {
		// git mv cleans the moved path through the index: no filter the
		// configuration names runs (T250 B4).
		mv := project.ReadGitCmd(".", "mv", source, destination)
		mv.Stdout, mv.Stderr = stdout, stderr
		if err := mv.Run(); err != nil {
			code := exitCode(err)
			fmt.Fprintf(stderr, "git mv exited with status %d; no fallback was attempted.\n", code)
			return code
		}
	} else if err := os.Rename(source, destination); err != nil {
		fmt.Fprintf(stderr, "mv failed: %v\n", err)
		return 1
	}
	if _, err := os.Lstat(source); err == nil {
		fmt.Fprintln(stderr, "archive move left the source directory in place")
		return 1
	}

	fmt.Fprintf(stdout, "readback: diff -r <snapshot> %s\n", destination)
	if diffs := diffTrees(copyRoot, destination); len(diffs) > 0 {
		for _, d := range diffs {
			fmt.Fprintln(stdout, d)
		}
		fmt.Fprintln(stderr, "readback: the destination differs from the snapshot")
		return 1
	}
	fmt.Fprintln(stdout, "readback: empty")
	return 0
}

// NoRedGreenRefusal is why `jhon archive` refuses change in root: no RED
// before GREEN on record, how to record it, and which change archives
// without it.
func NoRedGreenRefusal(root, change string) string {
	return "archive refused: " + change + " has no RED before GREEN on record (`tests.red_before_green` in `jhon status " + change + " --json` is not true); nothing was moved. " +
		"Record it: " + RedGreenHint(change) + "; then archive again. " +
		"Only a change that alters no behaviour archives without it, while a file outside " + layout.For(root).Dir() + "/ differs from the default branch (before its merge): " +
		"the standard and the verified route both keep the RED and GREEN runs of a change that alters behaviour (contracts/ldd.md, \"The record\")."
}

// routeAt is the route of the change at root, as status reads it: only a
// project that is its repository's top level has a route of its own (one
// nested in another repository would read that one's diff); nil otherwise
// or when it cannot be read.
func (g gitReads) routeAt(root string) *Route {
	top := vcs.GitTopLevel(root)
	if top == "" || project.Realish(top) != project.Realish(root) {
		return nil
	}
	rt, err := g.routeOf(root)
	if err != nil {
		return nil
	}
	return &rt
}

// noBehaviourApplied is whether the change at root, on route, alters no
// behaviour and has applied it: every file that differs from the default
// branch outside the layout is one `jhon risk` reads as passive (a test or
// a document), and at least one does (T223). Its route records no tests by
// design, so it is archived without a RED before GREEN. An unknown route
// or base, or an empty diff (after its merge, or nothing applied), is not.
func (g gitReads) noBehaviourApplied(root string, route *Route) bool {
	if route == nil || route.Behaviour {
		return false
	}
	top := vcs.GitTopLevel(root)
	if top == "" {
		return false
	}
	base, _, ok := g.baseOf(top, nil)
	if !ok {
		return false
	}
	files, _ := g.addedOutsideRecord(top, base)
	return len(files) > 0
}

var (
	// A change name is one path segment.
	changeName = lazyre.New(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	isoDay     = lazyre.New(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
)

func exitCode(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() > 0 {
		return exit.ExitCode()
	}
	return 1
}

// copyTree copies the directory src to dst: directories, regular files with
// their mode, and symlinks as links.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o777)
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		default:
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			return os.WriteFile(target, b, info.Mode().Perm())
		}
	})
}

// diffTrees is how the trees at a and b differ, as `diff -r` names it:
// an entry only in one, a file whose content differs, or a different kind of
// entry; empty when they are the same.
func diffTrees(a, b string) []string {
	var out []string
	var walk func(ra, rb string)
	walk = func(ra, rb string) {
		names := func(dir string) []string {
			es, _ := os.ReadDir(dir)
			var n []string
			for _, e := range es {
				n = append(n, e.Name())
			}
			return n
		}
		na, nb := names(ra), names(rb)
		all := append(slices.Clone(na), nb...)
		slices.Sort(all)
		all = slices.Compact(all)
		for _, n := range all {
			pa, pb := filepath.Join(ra, n), filepath.Join(rb, n)
			ia, errA := os.Lstat(pa)
			ib, errB := os.Lstat(pb)
			switch {
			case errB != nil:
				out = append(out, fmt.Sprintf("Only in %s: %s", ra, n))
			case errA != nil:
				out = append(out, fmt.Sprintf("Only in %s: %s", rb, n))
			case ia.IsDir() && ib.IsDir():
				walk(pa, pb)
			case ia.Mode().Type() != ib.Mode().Type():
				out = append(out, fmt.Sprintf("File %s is a %s while file %s is a %s", pa, kind(ia), pb, kind(ib)))
			case ia.Mode()&fs.ModeSymlink != 0:
				la, _ := os.Readlink(pa)
				lb, _ := os.Readlink(pb)
				if la != lb {
					out = append(out, fmt.Sprintf("Symbolic links %s and %s differ", pa, pb))
				}
			default:
				ba, _ := os.ReadFile(pa)
				bb, _ := os.ReadFile(pb)
				if !bytes.Equal(ba, bb) {
					out = append(out, fmt.Sprintf("Files %s and %s differ", pa, pb))
				}
			}
		}
	}
	walk(a, b)
	return out
}

func kind(fi fs.FileInfo) string {
	switch {
	case fi.IsDir():
		return "directory"
	case fi.Mode()&fs.ModeSymlink != 0:
		return "symbolic link"
	}
	return "regular file"
}
