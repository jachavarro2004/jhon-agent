package sandbox

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// Unused is a writable area the sandbox does not bind because it is or
// passes through a link (L28, "No seguir enlaces nunca"): what names it, its
// path as written, the first link on its way, and its real path, the one to
// name instead.
type Unused struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Link string `json:"link,omitempty"`
	Real string `json:"real"`
	// PATHFolder is the PATH folder a cache or store under the home holds,
	// for which it is not bound writable (L29); "" for a link.
	PATHFolder string `json:"path_folder,omitempty"`
}

// Line is what status, doctor and the note on a refused write say of u.
func (u Unused) Line() string {
	if u.Link == "" {
		return u.Name + " " + u.Path + " is not used for writing: it holds the PATH folder " + u.PATHFolder + ", where a sandboxed command could put a program run later outside the sandbox: move the tool's bin out of the cache, or take it off PATH"
	}
	how := "it passes through the link " + u.Link
	if u.Link == filepath.Clean(u.Path) {
		how = "it is a link"
	}
	return u.Name + " " + u.Path + " is not used for writing: " + how + ", and the sandbox opens no link for writing: name the real path (" + u.Real + ") instead"
}

// UnusedWriteAreas are the writable areas of in, each once, that exist and
// are or pass through a link, or are a cache or store under the home that
// holds a PATH folder (L29), so the sandbox binds none of them: the caches,
// the sandbox.allow_write folders, the toolchains' caches and stores, Jhon's
// telemetry folder, the scratch roots, $TMPDIR and the project's folders
// (its roots, <root>-worktrees included, the common dir and the listed
// worktrees, T249), in that order.
func UnusedWriteAreas(in Input) []Unused {
	sources := slices.Concat(in.areas(), []areaSource{{"Jhon's telemetry folder", in.Telemetry, false}})
	for _, p := range in.Tmp {
		sources = append(sources, areaSource{"the scratch root", p, false})
	}
	sources = append(sources, areaSource{"$TMPDIR", in.Env["TMPDIR"], false})
	for _, p := range slices.Concat(in.Roots, []string{in.CommonDir}, in.Worktrees) {
		sources = append(sources, areaSource{"the project's folder", p, false})
	}
	var out []Unused
	seen := map[string]bool{}
	for _, s := range sources {
		if !filepath.IsAbs(s.path) {
			continue
		}
		p := filepath.Clean(s.path)
		if seen[p] {
			continue
		}
		seen[p] = true
		real, _, ok := in.Stat(p)
		if !ok {
			continue
		}
		if link := in.linkOn(p); link != "" {
			out = append(out, Unused{Name: s.name, Path: p, Link: link, Real: real})
		} else if dir := in.holdsPATH(p); s.cache && dir != "" {
			out = append(out, Unused{Name: s.name, Path: p, Real: real, PATHFolder: dir})
		}
	}
	return out
}

// Kept is one of Jhon's own paths a sandboxed command must not be able to
// replace through a sandbox.allow_write folder (L28): what it is, and its
// path.
type Kept struct{ What, Path string }

// keptFix is the fix of KeptProblem's problems.
const keptFix = "name a concrete subfolder in sandbox.allow_write, such as ~/.local/share/<app>, not a folder that holds Jhon's own files or a PATH folder"

// KeptProblem is why the sandbox of in runs nothing while a
// sandbox.allow_write folder holds one of kept (Jhon's release and the
// folder `current` leads to, the jhon program and where it leads, the source
// checkout, the protected paths) where a sandboxed command may change it,
// as mayChange walks it (renaming a plain folder around it: one bound
// read-only directly inside the allowed folder is a mount point no command
// can move, S10), or while one is or holds a PATH folder outside the
// project, where a command could put a program the owner runs later (L28,
// "Rechazar el allow_write que las contenga"). nil when none does.
func KeptProblem(in Input, kept []Kept) *Problem {
	var roots []string
	for _, r := range in.ownRoots() {
		if filepath.IsAbs(r) {
			roots = append(roots, filepath.Clean(r), realPath(r, in.Stat))
		}
	}
	both := func(p string) []string { return []string{filepath.Clean(p), realPath(p, in.Stat)} }
	// holder is the sandbox.allow_write folder that is or holds p, as
	// written or where its existing part leads, "" when none is.
	holder := func(p string) string {
		for _, a := range in.AllowWrite {
			if !filepath.IsAbs(a) {
				continue
			}
			for _, x := range both(a) {
				for _, y := range both(p) {
					if project.Within(y, x) {
						return a
					}
				}
			}
		}
		return ""
	}
	for _, k := range kept {
		if !filepath.IsAbs(k.Path) {
			continue
		}
		if a := holder(k.Path); a != "" && mayChange(in, k.Path) {
			return &Problem{Reason: "sandbox.allow_write folder " + a + " holds " + k.What + " (" + k.Path + "), which a sandboxed command could then replace by renaming a folder around it", Fix: keptFix}
		}
	}
	for _, dir := range in.Path {
		if !filepath.IsAbs(dir) || slices.ContainsFunc(both(dir), func(d string) bool { return within(d, roots) }) {
			continue
		}
		if a := holder(dir); a != "" {
			how := " holds the PATH folder "
			if slices.Contains(both(a), filepath.Clean(dir)) || slices.Contains(both(a), realPath(dir, in.Stat)) {
				how = " is the PATH folder "
			}
			return &Problem{Reason: "sandbox.allow_write folder " + a + how + dir + ", where a sandboxed command could put a program run later outside the sandbox", Fix: keptFix}
		}
	}
	// The folders a program PATH finds through a link leads to count as PATH
	// folders (T249 A4): ~/.local/bin/claude leads into ~/.local/share/claude.
	if len(in.AllowWrite) == 0 {
		return nil
	}
	for _, l := range pathLinks(in.Path, roots) {
		for _, folder := range l.folders {
			if a := holder(folder); a != "" {
				return &Problem{Reason: "sandbox.allow_write folder " + a + " holds " + folder + ", where the program " + l.link + " on PATH leads, so a sandboxed command could change a program run later outside the sandbox", Fix: keptFix}
			}
		}
	}
	return nil
}

// pathLink is a link in a PATH folder and the folders on its way: the
// folder of each target it names in turn, as written and where it leads.
type pathLink struct {
	link    string
	folders []string
}

// pathLinks are the links in the PATH folders of path outside roots (real
// paths and as written), each with the folders its targets lie in, read
// from the disk: os.ReadDir, os.Readlink and filepath.EvalSymlinks. A
// target in the same folder as its link adds nothing, and a link whose
// every hop stays there is not resolved further.
func pathLinks(path, roots []string) []pathLink {
	var out []pathLink
	seen := map[string]bool{}
	for _, dir := range path {
		if !filepath.IsAbs(dir) || seen[filepath.Clean(dir)] {
			continue
		}
		dir = filepath.Clean(dir)
		seen[dir] = true
		real, err := filepath.EvalSymlinks(dir)
		if err != nil || within(dir, roots) || within(real, roots) || real != dir && seen[real] {
			continue
		}
		// One folder read once: /bin -> usr/bin is /usr/bin.
		seen[real] = true
		// Read where the folder is: a relative target's .. goes up from there.
		entries, err := os.ReadDir(real)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.Type()&fs.ModeSymlink == 0 {
				continue
			}
			link := filepath.Join(dir, e.Name())
			var folders []string
			add := func(f string) {
				if f != dir && f != real && !slices.Contains(folders, f) {
					folders = append(folders, f)
				}
			}
			cur := filepath.Join(real, e.Name())
			for range maxLinks {
				target, err := os.Readlink(cur)
				if err != nil {
					break
				}
				if !filepath.IsAbs(target) {
					target = filepath.Join(filepath.Dir(cur), target)
				}
				add(filepath.Dir(filepath.Clean(target)))
				fi, err := os.Lstat(target)
				if err != nil || fi.Mode()&fs.ModeSymlink == 0 {
					break
				}
				cur = target
			}
			// A link whose every hop stays in its own folder (python ->
			// python3.13) leads nowhere else; one that leaves it is read to
			// its end, through any link on the way there.
			if len(folders) == 0 {
				continue
			}
			if end, err := filepath.EvalSymlinks(link); err == nil {
				add(filepath.Dir(end))
			}
			out = append(out, pathLink{link: link, folders: folders})
		}
	}
	return out
}
