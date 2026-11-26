package sandbox

// A git repository inside a sandbox.allow_write folder (T251,
// odd/tasks/v0-1-release.md L1721, L1722): the owner's sessions are pinned
// to one project while a second repository sits in a folder the owner made
// writable for every sandboxed command. Its delivery lines (git fetch,
// pull, push, ls-remote, clone, remote update, and gh) cannot run inside
// the sandbox any more than the project's (ssh refuses its drop-ins there),
// so such a repository counts as a project for them: the line runs outside
// with every check the project's lines get (the repository's git keys and
// the walk of its git folders, the clean PATH, the registry's reverse rule)
// and the path anchors of T248 (RepositoryProblem, CwdProblem). Nothing
// beyond what the owner already made writable runs outside: the repository
// must lie in a sandbox.allow_write folder the sandbox opens for writing,
// named with no link and no `..` on the way (L1729 B1: git follows a link
// before a `..` physically, so a cleaned path is another folder).

import (
	"path/filepath"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

// placeOf is where each git of command, started in in.Cwd in a project with
// roots, works, and the problem that keeps the line in the sandbox, nil
// when none: every git (and a leading `cd <folder> &&`, leadingCd, T181)
// works in a folder inside the roots, as ProfileAt always judged it, or
// every one in a folder of the same git repository inside a
// sandbox.allow_write folder (repositoryAt), whose top it gives ("" for the
// project). gh, and any other program of the line, runs in its working
// folder: one in such a repository is a line of it too (L1722, L1729 B3),
// where only git and gh run outside. A clone writes its new folder inside
// the project or that repository, whichever it works in.
//
// Each folder is judged as the shell and git reach it, the path as written
// (each -C appended to the one before, not cleaned): a folder of the
// repository with a `..` part keeps the line inside, and so does a project
// folder whose `..` follows a link, which git takes up from where the link
// leads (dotDotProblem, L1729 B1). A relative cd folder on a line zsh runs
// (zsh set) is refused: zsh may find it through a cdpath.
func (in Input) placeOf(command string, roots []string, zsh bool) (string, *Problem) {
	programs, err := sh.JudgeString(command, nil, nil, sh.Opts{})
	if err != nil {
		return "", &Problem{Reason: "the line cannot be read for the folders its git works in", Fix: "run the line yourself in a terminal"}
	}
	var real []string
	for _, r := range roots {
		if filepath.IsAbs(r) {
			real = append(real, realPath(r, in.Stat))
		}
	}
	name := "the project"
	if len(roots) > 0 {
		name = roots[0]
	}
	inProject := func(p string) bool { return within(realPath(p, in.Stat), real) }
	// other is a program on the line but git and gh (and its leading cd):
	// docker and a deploy tool run outside only from the project (L1722
	// counts the repository for git and gh).
	other, cwdPrograms := "", false
	for _, p := range programs {
		n := p.Argv[0]
		if n != "git" && n != "gh" && n != "cd" && other == "" {
			other = n
		}
		cwdPrograms = cwdPrograms || n != "git" && n != "cd"
	}
	top, used := "", false
	mixed := func(a, b string) *Problem {
		return &Problem{Reason: "the line works in more than one repository (" + a + " and " + b + "), and a line runs outside the sandbox in one only", Fix: "run it once for each repository, on a line of its own"}
	}
	// enter is the root the folder written lies in, "" for the project,
	// once the line works in no other.
	enter := func(written string) (string, *Problem) {
		dir := filepath.Clean(written)
		if inProject(dir) {
			if p := in.dotDotProblem(written, false); p != nil {
				return "", p
			}
			if top != "" {
				return "", mixed(top, name)
			}
			used = true
			return "", nil
		}
		if p := in.dotDotProblem(written, true); p != nil {
			return "", p
		}
		t, p := in.repositoryAt(dir, roots)
		switch {
		case p != nil:
			return "", p
		case other != "":
			return "", &Problem{Reason: other + " runs outside the sandbox only in the project, and " + dir + " is in a git repository inside a sandbox.allow_write folder, where only git and gh do", Fix: "run " + other + " on a line of its own, from the project or a session of that repository"}
		case used:
			return "", mixed(name, t)
		case top != "" && top != t:
			return "", mixed(top, t)
		}
		top = t
		return t, nil
	}
	cwd := in.Cwd
	if dir, ok := leadingCd(command); ok {
		if zsh && !filepath.IsAbs(dir) {
			return "", &Problem{Reason: "zsh may take the relative folder " + dir + " of the leading cd from a cdpath", Fix: "name the folder by its absolute path, as in cd /path/to/repository && git fetch origin"}
		}
		if !filepath.IsAbs(dir) {
			dir = cwd + "/" + dir
		}
		if _, p := enter(dir); p != nil {
			return "", p
		}
		cwd = dir
	} else if cwdPrograms && !inProject(filepath.Clean(cwd)) && in.repositoryTop(filepath.Clean(cwd)) != "" {
		// gh (or docker, a deploy tool) run in a repository of an allowed
		// folder: a line of that repository (B3).
		if _, p := enter(cwd); p != nil {
			return "", p
		}
	}
	for _, p := range programs {
		if p.Argv[0] != "git" {
			continue
		}
		args := p.Argv[1:]
		written, k := gitFolderWritten(args, cwd)
		root, problem := enter(written)
		if problem != nil {
			return "", problem
		}
		if k >= len(args) || args[k] != "clone" {
			continue
		}
		// A folder that starts with ~, quoted or not, gives work, as -C ~
		// does (gitDelivers): unquoted, the shell puts the home there
		// (T128h3, L1350).
		target := cloneTarget(args[k+1:])
		if target == "" || target[0] == '~' {
			return "", &Problem{Reason: "git clone names no folder Jhon can judge (none, or one that starts with ~)", Fix: "name the new folder, as in git clone <url> <folder>"}
		}
		if !filepath.IsAbs(target) {
			target = written + "/" + target
		}
		where, fix := "the project ("+name+")", "the project"
		inside := inProject(target)
		if root != "" {
			where, fix = "the repository it works in ("+root+")", "that repository"
			inside = within(realPath(target, in.Stat), []string{root})
		}
		if !inside {
			return "", &Problem{Reason: "git clone writes " + filepath.Clean(target) + ", which is outside " + where, Fix: "clone into " + fix + ", or run the line yourself in a terminal"}
		}
		if p := in.dotDotProblem(target, root != ""); p != nil {
			return "", p
		}
	}
	return top, nil
}

// gitFolderWritten is gitFolder with the folder as the shell and git reach
// it: each -C appended to the one before as written, nothing cleaned, so a
// `..` after a link stays where git takes it (dotDotProblem).
func gitFolderWritten(args []string, cwd string) (string, int) {
	dir, k := cwd, 0
	for ; k < len(args) && strings.HasPrefix(args[k], "-"); k++ {
		if args[k] == "-C" && k+1 < len(args) {
			k++
			if filepath.IsAbs(args[k]) {
				dir = args[k]
			} else {
				dir = dir + "/" + args[k]
			}
		}
	}
	return dir, k
}

// dotDotProblem is why written, a folder as the shell and git reach it,
// keeps a line in the sandbox for a `..` part, nil when it does not (L1729
// B1): git and the kernel take a `..` up from where a link before it leads,
// while a cleaned path drops the link, so a check of the cleaned path judges
// another folder. A repository's folder (strict) may hold no `..`; a
// project's may, as before, unless a link comes before it. The fix names
// the real path, the folder git would work in.
func (in Input) dotDotProblem(written string, strict bool) *Problem {
	parts := strings.Split(written, "/")
	if !hasDotDot(parts) {
		return nil
	}
	if strict {
		return &Problem{Reason: written + " goes up with `..`, and a line outside the sandbox works in a repository only by a path with no `..` (git goes up from where a link on the way leads, not from the path as written)", Fix: "name its real path (" + in.physical(written) + ") instead"}
	}
	cur, link := "/", ""
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			if link != "" {
				real := in.physical(written)
				return &Problem{Reason: written + " goes up with `..` after the link " + link + ", so git would work in " + real + ", not where the path says", Fix: "name the real path (" + real + ") instead"}
			}
			cur = filepath.Dir(cur)
			continue
		}
		cur = filepath.Join(cur, part)
		if _, isLink, _ := in.lstat(in.homed(cur)); isLink && link == "" {
			link = cur
		}
	}
	return nil
}

// hasDotDot is whether one of parts is `..`.
func hasDotDot(parts []string) bool {
	for _, p := range parts {
		if p == ".." {
			return true
		}
	}
	return false
}

// physical is written, an absolute folder, as the kernel resolves it: each
// part from / in turn, a link replaced by its real path (in.Stat) before a
// `..` takes its parent.
func (in Input) physical(written string) string {
	cur := "/"
	for _, part := range strings.Split(written, "/") {
		switch part {
		case "", ".":
			continue
		case "..":
			cur = filepath.Dir(cur)
			continue
		}
		next := filepath.Join(cur, part)
		if r, _, ok := in.Stat(next); ok {
			next = r
		}
		cur = next
	}
	return cur
}

// repositoryTop is the nearest folder from p (clean, homed as written) up
// to the sandbox.allow_write folder that holds it that holds a .git entry,
// "" when p is in no allowed folder or in no repository there. It judges
// nothing else (repositoryAt does).
func (in Input) repositoryTop(p string) string {
	p = in.homed(p)
	allow, _ := in.allowedFolder(p)
	if allow == "" {
		return ""
	}
	for cur := p; ; cur = filepath.Dir(cur) {
		if _, _, ok := in.Stat(filepath.Join(cur, ".git")); ok {
			return cur
		}
		if cur == allow || filepath.Dir(cur) == cur {
			return ""
		}
	}
}

// repositoryAt is the top of the git repository inside a
// sandbox.allow_write folder that dir, a folder a line's git works in
// outside the project (roots), lies in: the nearest folder from dir up to
// the allowed folder that holds a .git entry, read as written with the
// home's own way taken (homed). It is one only when dir, as written, is in
// an allowed folder and neither is nor passes through a link (linkOn: the
// line runs outside where it names, and an allowed folder reached through
// a link is not one the sandbox binds), and when the sandbox opens the
// top for writing (layout): a repository in an allowed folder the sandbox
// does not bind (the home, a link) or in a folder it keeps read-only
// (Jhon's own) is none the owner made writable. Otherwise the problem says
// which, with what the owner can do: for an allowed folder that is or
// passes through a link, to set the real folder (L1729 B4).
func (in Input) repositoryAt(dir string, roots []string) (string, *Problem) {
	dir = filepath.Clean(dir)
	p := in.homed(dir)
	setReal := func(real, a string) string {
		return "set sandbox.allow_write to the real folder (" + real + ") instead of " + a + " (jhon settings set sandbox.allow_write <folders>,…)"
	}
	linked := func() *Problem {
		return &Problem{Reason: dir + " is or passes through the link " + in.linkOn(p) + ", and a line outside the sandbox reaches a repository only by a path with no link", Fix: "name its real path (" + realPath(p, in.Stat) + ") instead"}
	}
	allow, written := in.allowedFolder(p)
	if allow == "" {
		if a, _ := in.allowedFolder(realPath(p, in.Stat)); a != "" && in.linkOn(p) != "" {
			return "", linked()
		}
		// An allowed folder that is a link leads here: the sandbox binds
		// no link, so the owner names the folder it leads to.
		for _, a := range in.AllowWrite {
			if !filepath.IsAbs(a) {
				continue
			}
			h := in.homed(filepath.Clean(a))
			if ra := realPath(h, in.Stat); ra != h && in.linkOn(h) != "" && project.Within(realPath(p, in.Stat), ra) {
				return "", &Problem{Reason: dir + " is in " + ra + ", where the sandbox.allow_write folder " + a + " leads, and the sandbox opens no link for writing, so it counts as no project", Fix: setReal(ra, a)}
			}
		}
		where := "a project"
		if len(roots) > 0 {
			where = "the project (" + roots[0] + ")"
		}
		return "", &Problem{
			Reason: dir + " is neither in " + where + " nor in a git repository inside a sandbox.allow_write folder",
			Fix:    "run the line in a session of that repository (jhon start there), add a folder that holds the repository to sandbox.allow_write (jhon settings set sandbox.allow_write <folder>,…) if sandboxed commands may write it, or run the line yourself in a terminal",
		}
	}
	if l := in.linkOn(allow); l != "" {
		return "", &Problem{Reason: "the sandbox.allow_write folder " + written + " is or passes through the link " + l + ", and the sandbox opens no link for writing, so " + dir + " counts as no project", Fix: setReal(realPath(allow, in.Stat), written)}
	}
	if in.linkOn(p) != "" {
		return "", linked()
	}
	top := in.repositoryTop(p)
	if top == "" {
		return "", &Problem{Reason: dir + " is in the sandbox.allow_write folder " + written + " but in no git repository inside it", Fix: "only a git repository there counts as a project: run the line in one, or run it yourself in a terminal"}
	}
	mounts, _ := layout(in, true)
	if m := innermost(mounts, realPath(top, in.Stat)); m == nil || m.kind != write {
		return "", &Problem{Reason: "the repository " + top + " lies in the sandbox.allow_write folder " + written + ", which the sandbox does not open for writing there (jhon sandbox status says why), so it does not count as a project", Fix: "run the line yourself in a terminal"}
	}
	return top, nil
}

// allowedFolder is the first sandbox.allow_write folder of in that is p or
// holds it, both as written with the home's own way taken (homed), and
// that folder as the settings write it; "" when none does.
func (in Input) allowedFolder(p string) (allow, written string) {
	for _, a := range in.AllowWrite {
		if !filepath.IsAbs(a) {
			continue
		}
		if h := in.homed(filepath.Clean(a)); project.Within(p, h) {
			return h, a
		}
	}
	return "", ""
}

// LineGitFolderProblem is GitFolderProblem for dir, a folder a line that
// runs outside the sandbox works in, whose git folders git names as gitDir
// and commonDir (T251): with top, the git repository inside a
// sandbox.allow_write folder the line works in (Outside), a dir inside top
// has its git folders inside top, walked as the project's are; any other
// dir is judged against the project's own folders.
func (in Input) LineGitFolderProblem(top, dir, gitDir, commonDir string) string {
	if top != "" && project.Within(in.homed(filepath.Clean(dir)), top) {
		return gitFolderWalk(gitDir, commonDir, []string{top}, "the repository "+top)
	}
	return in.GitFolderProblem(gitDir, commonDir)
}
