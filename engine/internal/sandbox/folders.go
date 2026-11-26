package sandbox

// The folders a line outside the sandbox writes or works in (T251, L1731):
// every check before the line enters the registry reads them by their
// path, and a sandboxed command that ran until then may have moved one or
// put a link on its way. So once the line is in the registry, where no
// such command runs, each is opened as the shell and git reach it, with no
// link followed, and walked up to the root it must lie in
// (LineFoldersProblem): one code path for every kind, the repository's
// top, the working folder, a leading cd's folder, each -C of the chain and
// the folder a clone writes its new one in. A kind added later is one more
// entry of lineFolders.

import (
	"path/filepath"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

// lineFolder is a folder a line outside the sandbox writes or works in:
// what names it in a note, its path as the shell and git reach it (written:
// each -C appended to the one before, nothing cleaned), the root it must
// lie in on the disk ("" for none: a working folder outside every root,
// where only gh or docker run), whether its `..` parts are judged as a
// repository's (strict, dotDotProblem), whether it is the folder the
// process runs in (here), and whether git refusing a missing one is
// enough (missing: git cannot change into it, so it works nowhere). A
// clone's is its new folder as written (clone): what is anchored is the
// deepest part of it that exists when the check runs, read then, part by
// part (L1732 C2).
type lineFolder struct {
	what, written, root          string
	strict, here, missing, clone bool
}

// lineFolders are the folders line, run in in.Cwd, writes or works in, top
// being the git repository inside a sandbox.allow_write folder it works in
// ("" for a project's line, placeOf): the working folder, top, a leading
// cd's folder, each -C of each git in turn and, for a clone, its new
// folder, as written (its deepest existing part is found when it is
// checked, never from what was read before). Each one's root
// is top when it lies in top, else the project's root that holds it, the
// longest, else none; one path is listed once.
func (in Input) lineFolders(line, top string) []lineFolder {
	var roots []string
	for _, r := range in.ownRoots() {
		if filepath.IsAbs(r) {
			roots = append(roots, in.homed(filepath.Clean(r)))
		}
	}
	rootOf := func(written string) (string, bool) {
		c := in.homed(filepath.Clean(written))
		if top != "" && project.Within(c, top) {
			return top, true
		}
		root := ""
		for _, r := range roots {
			if project.Within(c, r) && len(r) > len(root) {
				root = r
			}
		}
		return root, false
	}
	var out []lineFolder
	add := func(f lineFolder) {
		if !filepath.IsAbs(f.written) {
			out = append(out, f)
			return
		}
		f.root, f.strict = rootOf(f.written)
		for i, o := range out {
			if o.written == f.written {
				out[i].here = o.here || f.here
				out[i].missing = o.missing && f.missing
				return
			}
		}
		out = append(out, f)
	}
	cwd := in.Cwd
	add(lineFolder{what: "the folder this line runs in, " + cwd + ",", written: cwd, here: true})
	if top != "" {
		add(lineFolder{what: "the repository " + top, written: top})
	}
	if dir, ok := leadingCd(line); ok {
		if !filepath.IsAbs(dir) {
			dir = cwd + "/" + dir
		}
		add(lineFolder{what: "the folder " + dir + " its leading cd names", written: dir, missing: true})
		cwd = dir
	}
	programs, _ := sh.JudgeString(line, nil, nil, sh.Opts{})
	for _, p := range programs {
		if len(p.Argv) == 0 || p.Argv[0] != "git" {
			continue
		}
		args := p.Argv[1:]
		dir, k := cwd, 0
		for ; k < len(args) && strings.HasPrefix(args[k], "-"); k++ {
			if args[k] == "-C" && k+1 < len(args) {
				k++
				if filepath.IsAbs(args[k]) {
					dir = args[k]
				} else {
					dir = dir + "/" + args[k]
				}
				add(lineFolder{what: "the folder " + dir + " git -C names", written: dir, missing: true})
			}
		}
		if k >= len(args) || args[k] != "clone" {
			continue
		}
		target := cloneTarget(args[k+1:])
		if target == "" || target[0] == '~' {
			continue
		}
		if !filepath.IsAbs(target) {
			target = dir + "/" + target
		}
		add(lineFolder{what: "the folder git clone writes " + target + " in", written: target, clone: true})
	}
	return out
}

// homedWritten is written, a path as written, with the home's own way (the
// HOME variable as written, leading to the home) replaced by the home's real
// path, as homed does, without cleaning the rest: a `..` after a link stays
// where the kernel takes it.
func (in Input) homedWritten(written string) string {
	home, w := filepath.Clean(in.Home), in.Env["HOME"]
	if !filepath.IsAbs(home) || !filepath.IsAbs(w) {
		return written
	}
	w = filepath.Clean(w)
	if w == home {
		return written
	}
	rest, ok := strings.CutPrefix(written, w)
	if !ok || rest != "" && rest[0] != '/' {
		return written
	}
	if real, _, ok := in.Stat(w); !ok || real != home {
		return written
	}
	return home + rest
}

// LineFoldersProblem is why the line, run in in.Cwd with top the git
// repository inside a sandbox.allow_write folder it works in ("" for a
// project's line), may not run outside the sandbox for the folders it
// writes or works in (lineFolders), nil when it may (L1729 B2, L1731):
// called once the line is in the registry, so no sandboxed command moves
// one again. Each is judged by foldersProblem.
//
// It reads the disk anew for every fact (Fresh, L1732): whatever in
// remembered before the registry (sandbox.Current's remembered Stat) is
// unreachable from here, so a folder made a link while the line waited is
// seen as one.
func (in Input) LineFoldersProblem(line, top string) *Problem {
	in = in.Fresh()
	return in.foldersProblem(in.lineFolders(line, top))
}

// changedFolder is the problem of a folder f that is not the one Jhon
// judged.
func changedFolder(f lineFolder, why string) *Problem {
	return &Problem{Reason: f.what + " changed after Jhon read it: " + why, Fix: "run the line again; if it is refused again, a command may be moving or replacing " + f.written + " or a folder on its way: find it and stop it"}
}
