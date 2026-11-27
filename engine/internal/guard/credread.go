package guard

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

// Credentials by real reach (T271, S10): a reader program on the read-only
// list (ReadOnly) prints what it reads, so an operand of one that reaches a
// credentials file is the owner's alone, in every mode. A word reaches one
// when it is a path that is one (by its name or a link's target, never a
// template), a path in a home credentials directory, a shell glob whose
// expansion holds one, or a selection glob (rg -g, grep --include, find
// -name feeding a reader, a git pathspec) that is credentials by name and
// finds such a file under its root (selectionReach). Never by spelling: a
// glob or pathspec that names files to leave them out (`!g`, --exclude,
// find's ! and -not, `:!`, `:^`, `:(exclude)`), a search that merely
// recurses into a folder, a selection glob whose plainest instance is no
// credentials name (`*.json`), and a program the guard does not read are
// not judged. The file tools' reading (ToolGlob, secrets-guard) is its own.

// credentialAlt is the fix a credentials read is refused with.
const credentialAlt = "read the template (such as .env.example) instead, or ask the owner for the value; the owner reads the file himself"

// contentReaders are the listed programs that print what they read, with
// the short option letters of each that take a value.
var contentReaders = map[string]string{
	"cat": "", "tac": "s", "head": "nc", "tail": "ncs", "nl": "bdfhilnpsvw", "fold": "w", "rev": "", "od": "AjNtw", "hexdump": "esn",
	"xxd": "cglosn", "strings": "nte", "base64": "w", "cut": "bcdf", "paste": "d", "column": "cosNdRtW", "sort": "kotTS", "uniq": "fsw",
	"diff": "CUIxXFL", "cmp": "in", "comm": "", "join": "12jteo", "jq": "fL", "grep": "efmABCdD", "egrep": "efmABCdD",
	"fgrep": "efmABCdD", "rg": "efgtTmABCMjErd",
}

// findSelects are find's tests that select entries by a glob.
var findSelects = set("-name", "-iname", "-path", "-ipath", "-wholename", "-iwholename")

// readerValued are the long options of the readers that take a separate value.
var readerValued = set("--regexp", "--file", "--max-count", "--after-context", "--before-context", "--context", "--devices",
	"--directories", "--label", "--binary-files", "--include", "--exclude", "--exclude-dir", "--exclude-from", "--group-separator",
	"--glob", "--iglob", "--type", "--type-not", "--type-add", "--type-clear", "--max-columns", "--threads", "--encoding", "--replace",
	"--max-depth", "--max-filesize", "--sort", "--sortr", "--color", "--colors", "--context-separator", "--field-context-separator",
	"--field-match-separator", "--path-separator", "--pre-glob", "--engine", "--dfa-size-limit", "--regex-size-limit", "--ignore-file",
	"--lines", "--bytes", "--key", "--field-separator", "--delimiter", "--fields", "--characters", "--width", "--indent", "--from-file",
	"--output", "--skip-fields", "--skip-chars", "--check-chars")

// credentialReads is the refusal of a listed reader whose operand reaches
// a credentials file, run in st's directory.
func credentialReads(p sh.Program, name string, args []string, ctx *Ctx, vars map[string]string, st *State) []Finding {
	var out []Finding
	refuse := func(word, hit string) {
		out = append(out, Finding{Level: "deny", What: name + " " + word, Why: CredentialsWhy(hit), Alt: credentialAlt})
	}
	dir := st.Dir
	if name == "git" {
		g := sh.GitParts(args)
		for i := 0; i+1 < len(g.Globals); i++ {
			if g.Globals[i] == "-C" && dir != "" {
				dir = pathOf(g.Globals[i+1], dir, ctx, vars)
			}
		}
		for _, w := range gitReadOperands(g) {
			if hit := wordReach(w[1], dir, ctx, vars, true); hit != "" {
				refuse(g.Sub+" "+w[0], hit)
			}
		}
		return out
	}
	letters, ok := contentReaders[name]
	if !ok {
		return nil
	}
	files, selectors := readerOperands(name, args, letters)
	fromFind := slices.ContainsFunc(p.Via, func(v string) bool { return strings.HasPrefix(v, "find -") })
	for _, w := range files {
		if w == "{}" && fromFind {
			for _, g := range st.FindSelect {
				if hit := selectionReach(g, st.FindPaths, dir, ctx, vars); hit != "" {
					refuse("{} (find -name "+g+")", hit)
				}
			}
			continue
		}
		if hit := wordReach(w, dir, ctx, vars, false); hit != "" {
			refuse(w, hit)
		}
	}
	roots := files
	if len(roots) == 0 {
		roots = []string{"."}
	}
	for _, g := range selectors {
		if hit := selectionReach(g, roots, dir, ctx, vars); hit != "" {
			refuse(g, hit)
		}
	}
	return out
}

// selectionReach is the credentials file a selection glob reaches under
// roots, read from dir: "" unless the glob is credentials by name
// (namesCredentials), with no walk at all; for one that is, the first such
// file a bounded walk of each root meets, tool data aside (layout.ToolData),
// or the glob itself when a root cannot be resolved or walked to the end
// (fail closed: it names credentials).
func selectionReach(glob string, roots []string, dir string, ctx *Ctx, vars map[string]string) string {
	glob = globLead.ReplaceAllString(glob, "")
	for _, alt := range braces(glob) {
		if !namesCredentials(alt) {
			continue
		}
		for _, root := range roots {
			abs := ""
			if dir != "" {
				abs = pathOf(root, dir, ctx, vars)
			}
			if abs == "" {
				return glob
			}
			if info, err := os.Stat(abs); err == nil && !info.IsDir() {
				abs = filepath.Dir(abs)
			}
			found, ok := globReachesBelowSkipping(abs, strings.TrimPrefix(alt, "/"), layout.ToolData)
			if found || !ok {
				return glob
			}
		}
	}
	return ""
}

// namesCredentials is whether a glob is credentials by name, read with no
// file: its plainest instance, each `*` or `**` filled with nothing or with
// a plain word, `?` with a letter and a class with one of its members, is a
// credentials file by name (CredentialFile: never a template). `.env*`,
// `**/.env`, `*.pem` and `id_rsa*` are; `*.json` (word.json), `cred*` and
// `id_*` are not, whatever the tree holds.
func namesCredentials(glob string) bool {
	return CredentialFile(plainInstance(glob, "")) || CredentialFile(plainInstance(glob, "word"))
}

// plainInstance is glob with each wildcard filled: `*` runs with fill, `?`
// with a letter, a class with one of its members.
func plainInstance(glob, fill string) string {
	var b strings.Builder
	for i := 0; i < len(glob); i++ {
		switch c := glob[i]; c {
		case '*':
			for i+1 < len(glob) && glob[i+1] == '*' {
				i++
			}
			b.WriteString(fill)
		case '?':
			b.WriteByte('a')
		case '\\':
			if i+1 < len(glob) {
				i++
				b.WriteByte(glob[i])
			}
		case '[':
			end := -1
			if i+2 <= len(glob) {
				end = strings.IndexByte(glob[i+2:], ']')
			}
			if end < 0 {
				b.WriteByte(c)
				continue
			}
			b.WriteByte(classMember(glob[i+1 : i+2+end]))
			i += end + 2
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// classMember is one character a glob class matches: its first, or for a
// negated class the first of a few letters it leaves out.
func classMember(class string) byte {
	if class[0] != '!' && class[0] != '^' {
		return class[0]
	}
	for _, c := range []byte("axqz_") {
		if !strings.ContainsRune(class[1:], rune(c)) {
			return c
		}
	}
	return 'a'
}

// readerOperands is the files a reader's words name and the selection
// globs it searches its folders with; a pattern given as an operand (grep,
// rg, jq's filter) is neither, and an exclusion is not read.
func readerOperands(name string, args []string, letters string) (files, selectors []string) {
	search := name == "grep" || name == "egrep" || name == "fgrep" || name == "rg"
	if name == "rg" && slices.Contains(args, "--files") {
		return nil, nil // it lists names, never contents
	}
	patternGiven := false
	var ops []string
	for k := 0; k < len(args); k++ {
		a := args[k]
		if a == "--" {
			ops = append(ops, args[k+1:]...)
			break
		}
		if strings.HasPrefix(a, "--") {
			n, v, eq := strings.Cut(a, "=")
			if !eq && readerValued[n] && k+1 < len(args) {
				v = args[k+1]
				k++
			}
			switch n {
			case "--regexp", "--file", "--from-file":
				patternGiven = true
				if n != "--regexp" {
					files = append(files, v)
				}
			case "--include", "--glob", "--iglob":
				if !strings.HasPrefix(v, "!") {
					selectors = append(selectors, v)
				}
			case "--rawfile", "--slurpfile":
				files = append(files, at(args, k+2))
				k += 2
			case "--arg", "--argjson":
				k += 2
			}
			continue
		}
		if len(a) > 1 && a[0] == '-' {
		cluster:
			for i := 1; i < len(a); i++ {
				c := a[i]
				if strings.IndexByte(letters, c) < 0 {
					continue
				}
				v := a[i+1:]
				if v == "" && k+1 < len(args) {
					v = args[k+1]
					k++
				}
				switch {
				case (c == 'e' || c == 'f') && (search || name == "jq"):
					patternGiven = true
					if c == 'f' {
						files = append(files, v)
					}
				case c == 'g' && name == "rg" && !strings.HasPrefix(v, "!"):
					selectors = append(selectors, v)
				case c == 'X' && name == "diff":
					files = append(files, v)
				}
				break cluster
			}
			continue
		}
		ops = append(ops, a)
	}
	if (search || name == "jq") && !patternGiven && len(ops) > 0 {
		ops = ops[1:]
	}
	return append(files, ops...), selectors
}

// gitNamesOnly are the options with which a reading git command prints
// names, counts or types, never a file's contents.
var gitNamesOnly = map[string]map[string]bool{
	"diff":     set("--name-only", "--name-status", "--stat", "--numstat", "--shortstat", "--dirstat", "--summary", "--quiet", "--exit-code"),
	"show":     set("--name-only", "--name-status", "--stat", "--numstat", "--shortstat", "--dirstat", "--summary", "-s", "--no-patch"),
	"grep":     set("-l", "--files-with-matches", "--name-only", "-L", "--files-without-match", "-c", "--count", "-q", "--quiet"),
	"cat-file": set("-t", "-s", "-e"),
}

// gitReadOperands is the paths a reading git command prints the contents
// of: a `rev:path`'s path, and each operand that is no option, with a
// pathspec's magic read (an exclusion is none). log prints them only with
// a patch. Each is the word as written and the path it names.
func gitReadOperands(g sh.Git) [][2]string {
	switch g.Sub {
	case "show", "diff", "grep", "blame", "annotate", "cat-file":
	case "log", "whatchanged":
		if !slices.ContainsFunc(g.Rest, func(a string) bool { return a == "-p" || a == "-u" || a == "--patch" || strings.HasPrefix(a, "-L") }) {
			return nil
		}
	default:
		return nil
	}
	for _, a := range g.Rest {
		if a == "--" {
			break
		}
		if n, _, _ := strings.Cut(a, "="); gitNamesOnly[g.Sub][n] {
			return nil
		}
	}
	var out [][2]string
	dashes, pattern := false, g.Sub == "grep"
	for _, a := range g.Rest {
		if a == "--" {
			dashes = true
			continue
		}
		if !dashes && strings.HasPrefix(a, "-") {
			if g.Sub == "grep" && (a == "-e" || a == "-f" || strings.HasPrefix(a, "--regexp") || strings.HasPrefix(a, "-e")) {
				pattern = false
			}
			continue
		}
		if pattern && !dashes {
			pattern = false
			continue
		}
		if _, path, ok := strings.Cut(a, ":"); ok && !strings.HasPrefix(a, ":") && !dashes {
			out = append(out, [2]string{a, path})
			continue
		}
		if p, ok := pathspec(a); ok {
			out = append(out, [2]string{a, p})
		}
	}
	return out
}

// pathspec is a git pathspec's pattern with its magic read: ok is false for
// an exclusion (`:!`, `:^`, `:(exclude)`).
func pathspec(a string) (string, bool) {
	if !strings.HasPrefix(a, ":") {
		return a, true
	}
	rest := a[1:]
	if strings.HasPrefix(rest, "(") {
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			return a, true
		}
		for _, m := range strings.Split(rest[1:end], ",") {
			if m == "exclude" {
				return "", false
			}
		}
		return rest[end+1:], true
	}
	if strings.HasPrefix(rest, "!") || strings.HasPrefix(rest, "^") {
		return "", false
	}
	return strings.TrimLeft(rest, "/:"), true
}

// wordReach is the credentials file the word w, read in dir, reaches: its
// name, a link's target, a home credentials directory, or what its glob
// expands to there ("" for none). A git pathspec glob is git's to expand,
// through the tree (ToolGlob). A word the guard cannot resolve (a
// substitution, an unknown variable) is not judged.
func wordReach(w, dir string, ctx *Ctx, vars map[string]string, gitSpec bool) string {
	if w == "" || strings.Contains(w, "…") {
		return ""
	}
	for _, alt := range braces(w) {
		if alt != w {
			if hit := wordReach(alt, dir, ctx, vars, gitSpec); hit != "" {
				return hit
			}
			continue
		}
		if !isGlob(w) {
			if CredentialFile(w) {
				return w
			}
			abs := pathOf(w, dir, ctx, vars)
			if abs != "" && (CredentialFile(project.Realish(abs)) || HomeCredentialPath(abs, dir, false)) {
				return w
			}
			continue
		}
		if gitSpec {
			// A pathspec with a wildcard selects as rg -g does.
			if hit := selectionReach(w, []string{"."}, dir, ctx, vars); hit != "" {
				return hit
			}
			continue
		}
		// A glob in or into a home credentials directory reads one under any name (T87c).
		if homeCredentialGlob(w, dir, ctx.Home) {
			return w
		}
		if abs := pathOf(w, dir, ctx, vars); abs != "" {
			if hit := shellGlobHit(abs); hit != "" {
				return hit
			}
		}
	}
	return ""
}

// shellGlobHit is a credentials file the shell glob abs expands to, as
// bash does: a name with a leading dot only by a pattern that spells the
// dot; `**/`, which zsh (and bash's globstar) reads as any depth, is walked
// (fail closed). A `**` not followed by `/` is `*`.
func shellGlobHit(abs string) string {
	if strings.Contains(abs, "**/") {
		// The shell reads every match at any depth; the walk, from the
		// glob's literal folders, leaves tool data out, and fails closed
		// only for a glob named as credentials.
		segs := strings.Split(abs, "/")
		k := slices.IndexFunc(segs, isGlob)
		base := strings.Join(segs[:k], "/")
		if base == "" {
			base = "/"
		}
		found, ok := globReachesBelowSkipping(base, strings.Join(segs[k:], "/"), layout.ToolData)
		if found || !ok && namesCredentials(filepath.Base(abs)) {
			return abs
		}
		return ""
	}
	matches, err := filepath.Glob(abs)
	if err != nil {
		return ""
	}
	dot := strings.HasPrefix(filepath.Base(abs), ".")
	for _, m := range matches {
		if strings.HasPrefix(filepath.Base(m), ".") && !dot {
			continue
		}
		if CredentialFile(m) || CredentialFile(project.Realish(m)) || HomeCredentialPath(m, "/", false) {
			return m
		}
	}
	return ""
}

// braces is a word's brace alternatives, as the shell expands them before
// globbing (`{.env,a}`): the word itself when it holds none.
func braces(w string) []string {
	open := strings.IndexByte(w, '{')
	if open < 0 {
		return []string{w}
	}
	depth := 0
	parts, start := []string(nil), open+1
	for i := open; i < len(w); i++ {
		switch w[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				parts = append(parts, w[start:i])
				if len(parts) < 2 {
					return []string{w}
				}
				var out []string
				for _, p := range parts {
					out = append(out, braces(w[:open]+p+w[i+1:])...)
				}
				return out
			}
		case ',':
			if depth == 1 {
				parts = append(parts, w[start:i])
				start = i + 1
			}
		}
	}
	return []string{w}
}
