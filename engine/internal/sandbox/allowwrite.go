package sandbox

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// A sandbox.allow_write folder that does not exist yet (T239,
// odd/tasks/studio-integration-v1.md S5): bubblewrap binds only what exists,
// so such a folder stayed read-only inside the sandbox and the owner had to
// make it by hand (odd/tasks/jhon-studio.md L14). `jhon sandbox run` now
// makes it outside the sandbox before the sandbox starts (CreateAllowWrite),
// and binds it like any other: the smallest change, as binding a parent that
// exists would open its siblings too, and a folder made inside the sandbox
// would vanish with it. It makes one only where that is safe, and names
// every other one with why (MissingAllowWrite).

// MissingFolder is a sandbox.allow_write folder that does not exist, as
// MissingAllowWrite judges it: Create when the next sandboxed run makes it,
// else Why Jhon will not, with the fix.
type MissingFolder struct {
	Folder string `json:"folder"`
	Create bool   `json:"create"`
	Why    string `json:"why,omitempty"`
}

// MissingAllowWrite is each folder of in.AllowWrite, once, that does not
// exist and that no area a sandboxed command may write already holds (there
// the command makes it itself), in the settings' order. Jhon makes one only
// when it lies under the home (not the home itself), every part of it that
// exists below the home is a folder and no link, and it neither lies in nor
// holds a path the sandbox hides (the credentials, the session's sockets,
// the snapshots) or keeps read-only (Jhon's settings, state, releases and
// source, the runtimes' folders), nor is or holds a PATH folder. Anything
// else gets Why. It reads the disk only through in.Stat.
func MissingAllowWrite(in Input) []MissingFolder {
	var mounts []mount
	var out []MissingFolder
	seen := map[string]bool{}
	for _, f := range in.AllowWrite {
		if !filepath.IsAbs(f) {
			continue
		}
		f = filepath.Clean(f)
		if seen[f] {
			continue
		}
		seen[f] = true
		if _, _, ok := in.Stat(f); ok {
			continue
		}
		if mounts == nil {
			mounts, _ = layout(in, true)
		}
		why, writable := in.whyNotCreate(f, mounts)
		if writable {
			continue
		}
		out = append(out, MissingFolder{Folder: f, Create: why == "", Why: why})
	}
	return out
}

// whyNotCreate is why Jhon does not make folder f, which does not exist, or
// "" when it may; writable when an area a sandboxed command may write holds
// it already, so nothing is needed.
func (in Input) whyNotCreate(f string, mounts []mount) (why string, writable bool) {
	home := filepath.Clean(in.Home)
	f = in.homed(f)
	if why := in.kept(f); why != "" {
		return why, false
	}
	// The nearest folder of f that exists: where a command would make it.
	near := f
	for {
		if _, _, ok := in.Stat(near); ok || filepath.Dir(near) == near {
			break
		}
		near = filepath.Dir(near)
	}
	if m := innermost(mounts, realPath(near, in.Stat)); m != nil && m.kind == write {
		return "", true
	}
	if real, dir, ok := in.Stat(home); !ok || !dir || real != home || home == string(filepath.Separator) || f == home || !project.Within(f, home) {
		return "Jhon creates a missing folder only under the home folder (" + home + "): create it yourself (mkdir -p " + f + ")", false
	}
	rel, _ := filepath.Rel(home, f)
	cur := home
	parts := strings.Split(rel, string(filepath.Separator))
	for i, part := range parts {
		cur = filepath.Join(cur, part)
		real, dir, ok := in.Stat(cur)
		if !ok {
			// Each parent made on the way, from this first missing part to
			// f's own folder, passes the rules f does (L26 A1).
			for j := i; j < len(parts)-1; j++ {
				parent := filepath.Join(append([]string{home}, parts[:j+1]...)...)
				if why := in.kept(parent); why != "" {
					return "Jhon would create " + parent + " on the way, and " + why, false
				}
			}
			break
		}
		if real != cur {
			return cur + " is a link, and Jhon creates no folder through a link: set sandbox.allow_write to the folder it leads to, as the sandbox opens no link for writing", false
		}
		if !dir {
			return cur + " is a file, not a folder", false
		}
	}
	return "", false
}

// homed is f with the home's own way replaced by the home's real path
// (in.Home): the settings write a folder under ~/ with $HOME, which may be
// reached through a link (/home -> /var/home). Only the HOME variable as
// written (in.Env) is that way, and only when it leads to the home; a link
// elsewhere that leads to the home (a folder a command replaced with one)
// is not (T249). Nothing below the home is resolved.
func (in Input) homed(f string) string {
	home := filepath.Clean(in.Home)
	if !filepath.IsAbs(home) || project.Within(f, home) {
		return f
	}
	written := in.Env["HOME"]
	if !filepath.IsAbs(written) {
		return f
	}
	written = filepath.Clean(written)
	if written == home || !project.Within(f, written) {
		return f
	}
	if real, _, ok := in.Stat(written); !ok || real != home {
		return f
	}
	rel, err := filepath.Rel(written, f)
	if err != nil {
		return f
	}
	return filepath.Join(home, rel)
}

// kept is why f, a folder Jhon would make, lies in or holds a path the
// sandbox hides or keeps read-only, or is or holds a PATH folder; "" when it
// does none of these. The paths are taken as written and, where they exist
// in part, where their existing head leads, whether or not they exist.
func (in Input) kept(f string) string {
	hidden, own := in.keptPaths()
	overlap := func(paths []string) (string, bool, bool) {
		for _, p := range paths {
			if !filepath.IsAbs(p) {
				continue
			}
			for _, q := range []string{filepath.Clean(p), realPath(p, in.Stat)} {
				if project.Within(f, q) {
					return q, true, true
				}
				if project.Within(q, f) {
					return q, false, true
				}
			}
		}
		return "", false, false
	}
	if p, inside, ok := overlap(hidden); ok {
		if inside {
			return "it lies in " + p + ", which the sandbox hides: pick a folder elsewhere"
		}
		return "it holds " + p + ", which the sandbox hides: pick a folder that holds none"
	}
	if p, inside, ok := overlap(own); ok {
		const what = ", one of Jhon's or the runtimes' own folders, which the sandbox keeps read-only: "
		if inside {
			return "it lies in " + p + what + "pick a folder elsewhere"
		}
		return "it holds " + p + what + "pick a folder that holds none"
	}
	// A folder inside a PATH folder holds no program PATH finds; the PATH
	// folder itself, or one that holds it, would.
	for _, dir := range in.Path {
		if !filepath.IsAbs(dir) {
			continue
		}
		for _, q := range []string{filepath.Clean(dir), realPath(dir, in.Stat)} {
			switch {
			case q == f:
				return "it is " + q + ", a PATH folder, which the sandbox keeps read-only: pick a folder that is not on PATH"
			case project.Within(q, f):
				return "it holds " + q + ", a PATH folder, which the sandbox keeps read-only: pick a folder that holds none"
			}
		}
	}
	return ""
}

// keptPaths are the paths the sandbox of in hides and the ones it keeps
// read-only as Jhon's or the runtimes' own, each whether or not it exists:
// the lists layout mounts.
func (in Input) keptPaths() (hidden, own []string) {
	credentials, framework := guard.ProtectedHome()
	config := in.xdg("XDG_CONFIG_HOME", ".config")
	for _, rel := range slices.Concat(credentials, moreCredentials) {
		hidden = append(hidden, in.under(rel))
		if moved, ok := strings.CutPrefix(rel, ".config/"); ok {
			hidden = append(hidden, filepath.Join(config, moved))
		}
	}
	hidden = append(hidden, snapshots...)
	hidden = append(hidden, in.under(".snapshots"), in.Env["XDG_RUNTIME_DIR"], in.RuntimeDir, in.Registry)
	hidden = append(hidden, in.socketPaths()...)
	own = slices.Concat(in.Protected, []string{filepath.Join(config, "jhon"), filepath.Join(in.xdg("XDG_DATA_HOME", ".local/share"), "jhon")})
	for _, rel := range slices.Concat(framework, runtimeHomes) {
		own = append(own, in.under(rel))
	}
	return hidden, own
}

// innermost is the innermost mount of mounts (layout's, parents first) that
// is p or a folder that holds it; nil when none does: the machine's
// read-only root.
func innermost(mounts []mount, p string) *mount {
	var at *mount
	for i, m := range mounts {
		if m.path == p || m.dir && project.Within(p, m.path) {
			at = &mounts[i]
		}
	}
	return at
}

// CreateAllowWrite makes each folder MissingAllowWrite lets Jhon make, with
// its missing parents (mode 0755 less the umask), walking from / with no
// link followed (mkdirUnder): a link that appears on the way after the
// check stops it. created are the folders made; missing the ones still
// missing that Jhon will not make, and the ones it could not, each with why.
func CreateAllowWrite(in Input) (created []string, missing []MissingFolder) {
	for _, m := range MissingAllowWrite(in) {
		if !m.Create {
			missing = append(missing, m)
			continue
		}
		if err := mkdirUnder(filepath.Clean(in.Home), in.homed(m.Folder)); err != nil {
			m.Create, m.Why = false, "Jhon could not create it ("+err.Error()+"): create it yourself (mkdir -p "+m.Folder+")"
			missing = append(missing, m)
			continue
		}
		created = append(created, m.Folder)
	}
	return created, missing
}

// Refusal is the note for a write the sandbox of in refused to path (T128g
// G3, T239): whether the path exists (not said where the sandbox hides it)
// and why it is read-only, with the setting that opens it or that none
// does: a folder of missing (CreateAllowWrite's) that holds it, or that it
// holds as mkdir -p met it first; no writable area (sandbox.allow_write
// opens it, but for a file directly in the home, the home itself or a path
// outside it, which no setting should open: the owner decides, L26 A4); a
// PATH folder, Jhon's or the runtimes' own, or a hidden path (none opens
// it); or a writable area, where the file system itself refused it. It
// ends with the stop, its control characters escaped (escapeControls, L26
// A3). With path "", RefusalNote's.
func Refusal(in Input, path string, missing []MissingFolder) string {
	if path == "" || !filepath.IsAbs(path) {
		return RefusalNote(path)
	}
	return escapeControls(refusal(in, path, missing))
}

// refusal is Refusal before its control characters are escaped.
func refusal(in Input, path string, missing []MissingFolder) string {
	p := filepath.Clean(path)
	const stop = "; that is a stop: tell the owner the path and why, never write it elsewhere"
	head := "the sandbox refused a write to " + path
	exists := ", which does not exist"
	_, _, found := in.Stat(p)
	if found {
		exists = ", which exists"
	}
	mounts, _ := layout(in, true)
	real := realPath(p, in.Stat)
	m := innermost(mounts, real)
	where := func(m *mount) string {
		if m.path == real {
			return "it is " + m.path
		}
		return "it is in " + m.path
	}
	switch {
	case m == nil:
		// An area the sandbox does not use as it is or passes through a
		// link (L28).
		for _, u := range UnusedWriteAreas(in) {
			if project.Within(p, u.Path) || project.Within(real, u.Real) {
				return head + exists + "; " + u.Line() + stop
			}
		}
		for _, f := range missing {
			if f.Why == "" {
				continue
			}
			fr := realPath(f.Folder, in.Stat)
			// In the folder, or a missing folder on the way to it (mkdir -p
			// met that one first).
			if project.Within(p, f.Folder) || project.Within(real, fr) || !found && (project.Within(f.Folder, p) || project.Within(fr, real)) {
				return head + exists + "; sandbox.allow_write names " + f.Folder + ", which does not exist and Jhon did not create it: " + f.Why + stop
			}
		}
		// A file directly in the home (~/.bashrc), the home itself and a
		// path outside it get no setting suggested (L26 A4).
		switch in.homeDepth(p, real) {
		case outsideHome:
			return head + exists + "; no folder the sandbox opens for writing holds it, and no setting should open it: it lies outside the home folder; whether to write it is the owner's decision" + stop
		case inHome:
			return head + exists + "; no folder the sandbox opens for writing holds it, and no setting should open it: it lies directly in the home folder; whether to write it is the owner's decision" + stop
		}
		return head + exists + "; no folder the sandbox opens for writing holds it: sandbox.allow_write opens it, or a folder that holds it, from the next command (jhon settings set sandbox.allow_write <folder>,…)" + stop
	case m.sealed && !m.shown:
		return head + "; " + where(m) + ", which the sandbox hides (credentials, session sockets, snapshots): no setting opens it" + stop
	case m.kind == write:
		return head + exists + "; " + where(m) + ", a folder the sandbox opens for writing, so the file system itself refused it, not the sandbox: no setting of Jhon's opens it" + stop
	case m.guarded:
		what := "folders"
		if !m.dir {
			what = "files"
		}
		return head + exists + "; " + where(m) + ", one of Jhon's or the runtimes' own " + what + ", read-only by design: no setting opens it" + stop
	case m.shown:
		return head + exists + "; " + where(m) + ", a container socket the sandbox shows read-only: no setting opens it" + stop
	}
	return head + exists + "; " + where(m) + ", a PATH folder, which stays read-only so that no sandboxed command changes a program run later: sandbox.allow_write does not open it" + stop
}

// Where a refused path lies against the home (homeDepth).
const (
	belowHome   = iota // in a folder under the home
	inHome             // directly in the home folder
	outsideHome        // the home itself, or outside it
)

// homeDepth is where p lies, as written (homed) and where its links lead
// (real), whichever is nearer the home or outside it; a home that is / or
// not absolute leaves every path outside it.
func (in Input) homeDepth(p, real string) int {
	home := filepath.Clean(in.Home)
	depth := func(x string) int {
		switch {
		case !filepath.IsAbs(home) || home == string(filepath.Separator) || x == home || !project.Within(x, home):
			return outsideHome
		case filepath.Dir(x) == home:
			return inHome
		}
		return belowHome
	}
	return max(depth(in.homed(p)), depth(real))
}
