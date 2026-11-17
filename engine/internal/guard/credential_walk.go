package guard

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// A glob the allow-list admits (credential_allow.go) is expanded here: a
// bounded walk that never follows a link, and that fails closed when it
// cannot finish.

// globMaxEntries bounds the entries one glob's expansion reads.
const globMaxEntries = 100000

var errUnsafe = errors.New("the glob cannot be expanded safely")

// plainGlob is a glob word the allow-list can expand: letters, digits,
// `._-/`, and the wildcards `*?[]!^`. A brace, a `$`, a `%` or a `{`
// builds a name the guard cannot see, and is never admitted.
var plainGlob = lazyre.New(`^[A-Za-z0-9._/*?\[\]!^-]+$`)

// relativeGlob is whether a glob word names only paths under the directory
// it runs in: plain, not absolute, no `..` part, no leading `~`, `:`, `@`
// or `-`.
func relativeGlob(w string) bool {
	if !plainGlob.MatchString(w) || strings.HasPrefix(w, "/") || strings.HasPrefix(w, "-") {
		return false
	}
	for _, seg := range strings.Split(w, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// globReachesFrom is whether a relative glob, expanded from dir as a shell
// does, reaches a credentials file at any depth below its literal
// directories (`i18n/**.json` walks i18n). ok is false when it cannot tell:
// a literal directory that is a link, an unreadable or too large tree.
func globReachesFrom(dir, glob string) (found, ok bool) {
	segs := strings.Split(strings.TrimPrefix(glob, "./"), "/")
	base := dir
	i := 0
	for ; i < len(segs)-1 && !isGlob(segs[i]); i++ {
		base = filepath.Join(base, segs[i])
		info, err := os.Lstat(base)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return false, true // nothing there to expand to
		case err != nil || info.Mode()&fs.ModeSymlink != 0:
			return false, false
		case !info.IsDir():
			return false, true
		}
	}
	return globReachesBelow(base, strings.Join(segs[i:], "/"))
}

// globReachesBelow is whether glob matches, at any depth under base, a
// credentials file: a name CredentialFile reads (never a template), or a
// linked file whose target is one. It reads more than the glob would
// (looseGlob), never less; ok is false when the walk meets a linked
// directory, a link it cannot read, an unreadable directory, or more than
// globMaxEntries entries.
func globReachesBelow(base, glob string) (found, ok bool) {
	info, err := os.Lstat(base)
	if errors.Is(err, fs.ErrNotExist) {
		return false, true
	}
	if err != nil || info.Mode()&fs.ModeSymlink != 0 {
		return false, false
	}
	if !info.IsDir() {
		return false, true
	}
	re := regexp.MustCompile(`(?is)^(?:.*/)?` + looseGlob(glob) + `$`)
	n := 0
	err = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return errUnsafe
		}
		if p == base {
			return nil
		}
		if n++; n > globMaxEntries {
			return errUnsafe
		}
		rel, _ := filepath.Rel(base, p)
		hit := re.MatchString(filepath.ToSlash(rel))
		if hit && strings.HasPrefix(d.Name(), "-") {
			return errUnsafe // a name the program would read as an option
		}
		if d.Type()&fs.ModeSymlink != 0 {
			// A linked directory may lead anywhere; a linked file is read by its target's name too.
			target, terr := filepath.EvalSymlinks(p)
			info, serr := os.Stat(p)
			if terr != nil || serr != nil || info.IsDir() {
				return errUnsafe
			}
			if hit && (CredentialFile(d.Name()) || CredentialFile(target)) {
				found = true
				return fs.SkipAll
			}
			return nil
		}
		if hit && !d.IsDir() && CredentialFile(d.Name()) {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return false, false
	}
	return found, true
}

// looseGlob is a glob as a regular expression that matches every path the
// glob matches, and more: `*` crosses `/`, `**/` may be nothing, a class,
// an alternative or an extended pattern is any text; the caller ignores case.
func looseGlob(glob string) string {
	closing := map[byte]byte{'[': ']', '{': '}', '(': ')'}
	var b strings.Builder
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch {
		case strings.HasPrefix(glob[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.IndexByte("*!@+|^", c) >= 0:
			b.WriteString(".*")
		case c == '?':
			b.WriteString(".")
		case closing[c] != 0 && strings.IndexByte(glob[i+1:], closing[c]) >= 0:
			b.WriteString(".*")
			i += strings.IndexByte(glob[i+1:], closing[c]) + 1
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String()
}

// gitProject is whether dir is inside root, and root is its git work tree's top.
func gitProject(dir, root string) bool {
	top, ok := project.ReadGit(dir, "rev-parse", "--show-toplevel")
	return ok && root != "" && project.Realish(top) == project.Realish(root) && project.Within(project.Realish(dir), project.Realish(root))
}

// ToolGlob is the secrets guard's reading of a tool's glob (Grep's glob,
// Glob's pattern) searched under path from cwd: refused when it is a
// credentials glob by its name (CredentialGlob), save when the guard proves
// it safe — the glob relative and plain, path inside cwd's project (a
// leading `~` is the home directory), and the glob reaching, at any depth
// under path, no credentials file.
func ToolGlob(glob, path, cwd string) bool {
	// A glob searched in a home credentials directory reads one under any name (T87c).
	if isGlob(glob) {
		dir := cwd
		switch {
		case path == "~" || strings.HasPrefix(path, "~/"):
			dir = filepath.Join(project.Home(), strings.TrimPrefix(path[1:], "/"))
		case strings.HasPrefix(path, "~") || strings.ContainsAny(path, "$`"):
			dir = ""
		case path != "":
			dir = project.Resolve(cwd, path)
		}
		if dir != "" && (homeCredentialGlob(glob, dir, project.Home()) || homeCredentialGlob(glob, project.Realish(dir), project.Home())) {
			return true
		}
	}
	if !credentialGlobWord(glob) {
		return false
	}
	if extensionGlob(glob) {
		// An extension glob is walked wherever its path leads (T87b).
		if strings.HasPrefix(path, "~") || strings.ContainsAny(path, "$`") {
			return true
		}
		dir := cwd
		if path != "" {
			dir = project.Resolve(cwd, path)
		}
		if info, err := os.Stat(dir); err == nil && !info.IsDir() {
			dir = filepath.Dir(dir)
		}
		return extensionGlobRefused(glob, dir, project.Home())
	}
	root := project.Root(cwd)
	if !gitProject(cwd, root) || !relativeGlob(glob) {
		return true
	}
	dir := cwd
	switch {
	case path == "~" || strings.HasPrefix(path, "~/"):
		dir = filepath.Join(project.Home(), strings.TrimPrefix(path[1:], "/"))
	case strings.HasPrefix(path, "~") || strings.ContainsAny(path, "$`"):
		return true
	case path != "":
		dir = project.Resolve(cwd, path)
	}
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	real := project.Realish(dir)
	if !project.Within(real, project.Realish(root)) {
		return true
	}
	found, ok := globReachesBelow(real, glob)
	return found || !ok
}
