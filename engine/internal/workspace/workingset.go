package workspace

// The session's working set (T281, S12): the repositories and folders the
// owner declared as his work, where nothing is "outside the project". It is
// the project of a directory, its worktrees, the listed and joined members
// of its workspace with theirs (ProjectRoots), and each sandbox.allow_write
// folder the sandbox opens for writing (AllowWriteFolders). Every place that
// asks whether a path is inside the project asks the set: the guards'
// outside-write and removal judgements, a bounded writer's surfaces, the
// repository route-guard judges a gh line in. The OS sandbox reads the same
// set from the disk (DiskRoots and its own sandbox.allow_write).
//
// Outside it stay a repository declared nowhere, a listed member that has
// not joined, and a sibling that joined a primary whose config does not
// list it (Unlisted): such a sibling is said, with the line that lists it,
// never admitted, since the primary's config is the declaration.

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/grant"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
)

// Set is a session's working set: Project, the roots of its project and
// workspace (ProjectRoots), and Shared, its sandbox.allow_write folders.
type Set struct {
	Project []string
	Shared  []string
}

// WorkingSet is the working set of a session in dir.
func WorkingSet(dir string) Set {
	return Set{Project: ProjectRoots(dir), Shared: AllowWriteFolders()}
}

// Roots are every folder of the set: the project's roots, then the shared
// folders.
func (s Set) Roots() []string {
	out := slices.Clone(s.Project)
	for _, f := range s.Shared {
		if !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out
}

// Holds is whether abs lies in the set, by its real path.
func (s Set) Holds(abs string) bool {
	real := project.Realish(abs)
	return slices.ContainsFunc(s.Roots(), func(r string) bool {
		return project.Within(real, r) || project.Within(real, project.Realish(r))
	})
}

// AllowWriteFolders are the sandbox.allow_write folders the sandbox opens
// for writing, each by its real path: absolute, neither / nor the home, and
// reached through no link but the home's own way (the sandbox opens no link
// for writing, so neither does the set). None when the settings cannot be
// read, as for the sandbox.
func AllowWriteFolders() []string {
	home := filepath.Clean(project.Home())
	realHome := project.Realish(home)
	var out []string
	for _, f := range settings.Paths("sandbox.allow_write") {
		if !filepath.IsAbs(f) {
			continue
		}
		f = filepath.Clean(f)
		want := f
		if rest, ok := strings.CutPrefix(f, home+string(filepath.Separator)); ok {
			want = filepath.Join(realHome, rest)
		}
		real := project.Realish(f)
		if real != want || real == string(filepath.Separator) || real == realHome || slices.Contains(out, real) {
			continue
		}
		out = append(out, real)
	}
	return out
}

// ModeOf is the owner's mode for the project root is in, as a session in
// the project of session takes it (T281, the owner's decision: one mode for
// the workspace): its own record (grant.ModeOf), and for a member the
// primary's config lists and that joined, with no record of its own, the
// primary's, with from the primary, when the session is in that workspace
// (the primary, or a member of it, the member itself included). A member
// with its own record keeps it (`jhon mode normal` there keeps it normal);
// a repository only sandbox.allow_write names, a sibling the primary does
// not list, a listed one that has not joined, and a session outside the
// workspace never take another's.
func ModeOf(root, session string) (rec grant.ModeRecord, from string) {
	rec = grant.ModeOf(root)
	if rec.Since != "" || rec.Root == "" {
		return rec, ""
	}
	info := Of(rec.Root)
	if info.Role != Member {
		return rec, ""
	}
	if own := grant.Root(project.Root(session)); own != rec.Root && own != info.Primary {
		if s := Of(own); s.Role != Member || s.Primary != info.Primary {
			return rec, ""
		}
	}
	if primary := grant.ModeOf(info.Primary); primary.Since != "" {
		return primary, info.Primary
	}
	return rec, ""
}

// Unlisted is a sibling that joined a primary whose LDD config lists no
// path to it, so it is no member: its main worktree, the primary, and the
// entry that would list it (a name and the path relative to the primary).
type Unlisted struct {
	Root, Primary, Name, Rel string
}

// UnlistedAt is the unlisted sibling the repository path is in is (a file
// or a folder, by its nearest existing folder), and whether it is one.
func UnlistedAt(path string) (Unlisted, bool) {
	dir := project.Realish(path)
	for {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			break
		}
		up := filepath.Dir(dir)
		if up == dir {
			return Unlisted{}, false
		}
		dir = up
	}
	main := Main(dir)
	if main == "" {
		return Unlisted{}, false
	}
	return unlistedOf(main, Main)
}

// UnlistedOf are the siblings whose pointers name primary while its config
// lists no path to them, in the pointers' order.
func UnlistedOf(primary string) []Unlisted {
	var out []Unlisted
	for _, p := range Pointers() {
		if p.Primary != primary {
			continue
		}
		if u, ok := unlistedOf(p.Root, Main); ok {
			out = append(out, u)
		}
	}
	return out
}

// unlistedOf is UnlistedAt for the repository whose main worktree is main.
func unlistedOf(main string, mainOf func(string) string) (Unlisted, bool) {
	p, ok := PointerOf(main)
	if !ok || mainOf(p.Primary) != p.Primary {
		return Unlisted{}, false
	}
	members, _, _ := membersOf(p.Primary, mainOf)
	if slices.ContainsFunc(members, func(l Listed) bool { return l.top == main }) {
		return Unlisted{}, false
	}
	rel, err := filepath.Rel(p.Primary, main)
	if err != nil {
		rel = main
	}
	return Unlisted{Root: main, Primary: p.Primary, Name: memberNameFor(filepath.Base(main)), Rel: filepath.ToSlash(rel)}, true
}

// memberNameFor is a member name ([a-z0-9-]+) for a folder's base name.
func memberNameFor(base string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	if name := strings.Trim(b.String(), "-"); name != "" {
		return name
	}
	return "member"
}

// Note is what an unlisted sibling is, with the edit that makes it a
// member: the entry under `workspace:` in the primary's LDD config, which
// an agent can write as any edit there.
func (u Unlisted) Note() string {
	return fmt.Sprintf("%s joined the workspace of %s, whose LDD config lists no path to it, so it is no member and stays outside the session's working set; to make it one, add `%s: %s` under `workspace:` in %s (or the owner runs `jhon workspace leave` in it)",
		u.Root, u.Primary, u.Name, u.Rel, project.LddConfigFile(u.Primary))
}
