package guard

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// The owner handles credentials: no agent reads, edits, or deletes a file
// that holds them. A template of one (`.env.example`) holds none, and is read
// and edited like any file.

var (
	// credentialName is a file name that holds credentials: an env file
	// (`.env`, `.env.production`, `.envrc`), a private key or keystore, or a
	// credentials file.
	credentialName = lazyre.New(`(?i)^(?:\.env(?:\..+)?|\.envrc|id_(?:rsa|dsa|ecdsa|ed25519)|.+\.(?:pem|key|p12|pfx|jks|keystore)|credentials(?:\.json)?|\.netrc|\.pgpass|\.pypirc)$`)
	// templateWords are the name parts that make a file a template, which
	// holds placeholders and no credentials: `.env.example`, `.env.local.sample`.
	templateWords = []string{"example", "examples", "sample", "template", "tpl", "dist", "defaults", "schema"}
	// credentialSamples are the names a glob is tried against.
	credentialSamples = []string{".env", ".env.local", ".env.production", "id_rsa", "server.key", "cert.pem", "credentials.json"}
	// globLiteral is a glob's wildcards and classes, which name no part of a file.
	globLiteral = lazyre.New(`\[[^\]]*\]|[*?]`)
	// globLead is what a glob word carries before its pattern: a git
	// pathspec's magic (`:(top,glob)`, `:/`, `:!`), or an `@` of a file of names.
	globLead = lazyre.New(`^(?::(?:\([^)]*\)|[/!^]*:?)|@)`)
)

// Credentials is the class of a refusal for a credentials file (Finding.Cls).
const Credentials = "credentials"

// CredentialFile is whether the file p names holds credentials: its name
// is one credentialName reads and no part of it marks a template.
func CredentialFile(p string) bool {
	base := filepath.Base(p)
	if !credentialName.MatchString(base) {
		return false
	}
	parts := strings.Split(strings.ToLower(base), ".")
	return !slices.ContainsFunc(parts, func(x string) bool { return slices.Contains(templateWords, x) })
}

// CredentialGlob is whether a glob pattern's last part can match a file
// that holds credentials, by its name alone and ignoring case (`:(icase)`,
// find -iname).
func CredentialGlob(pattern string) bool {
	base := strings.ToLower(filepath.Base(globLead.ReplaceAllString(pattern, "")))
	// A glob names a credentials file only by part of its name: `.env*` or
	// `*.key` does, and `*` or `dist/*` does not.
	if !strings.ContainsAny(base, "*?[") || len(globLiteral.ReplaceAllString(base, "")) < 3 {
		return false
	}
	return slices.ContainsFunc(credentialSamples, func(s string) bool {
		ok, err := path.Match(base, s)
		return err == nil && ok && CredentialFile(s)
	})
}

// extensionGlob is whether a credentials glob names a credentials file only
// by its extension: every sample it matches is a name with a stem and an
// extension (credentials.json), and the glob matches any name with that
// extension too (data.json), which holds none (T87b, L630-L631). `*.json` is
// one; `.env*`, `*.pem`, `id_*` and `cred*.json` name one by name. A glob
// with pathspec magic is never one.
func extensionGlob(pattern string) bool {
	if globLead.MatchString(pattern) || !CredentialGlob(pattern) || CredentialFile(pattern) {
		return false
	}
	base := strings.ToLower(filepath.Base(pattern))
	for _, s := range credentialSamples {
		if ok, err := path.Match(base, s); err != nil || !ok || !CredentialFile(s) {
			continue
		}
		if strings.HasPrefix(s, ".") || path.Ext(s) == "" {
			return false
		}
		probe := "data" + path.Ext(s)
		if ok, _ := path.Match(base, probe); !ok || CredentialFile(probe) {
			return false
		}
	}
	return true
}

// credentialHomes are the home directories that hold credentials under any
// name: an extension glob in one, or above one, is refused. They are the
// credential half of protectedHome plus gcloud's: Jhon's own install
// (.config/jhon, .local/share/jhon, .local/bin/jhon) is write-protected,
// not secret, and its prose is read by every session (L641).
var credentialHomes = append(slices.DeleteFunc(slices.Clone(protectedHome), func(p string) bool {
	return p == ".config/jhon" || strings.HasPrefix(p, ".local/")
}), ".config/gcloud")

// homeCredentialGlob is whether glob, run from dir, can expand into a home
// credentials directory (credentialHomes) or name one: its leading `~`,
// `$HOME` or `${HOME}`, or its absolute or dir-relative path, puts it under
// home, and its parts match a credentials directory's as the shell would (a
// leading dot only by a dot; `**` by anything). Those directories hold
// credentials under any name, so even `*` there reads one (T87c, L636).
func homeCredentialGlob(glob, dir, home string) bool {
	if home == "" || !isGlob(glob) {
		return false
	}
	var full string
	switch {
	case glob == "~":
		full = home
	case strings.HasPrefix(glob, "~/"):
		full = filepath.Join(home, glob[2:])
	case strings.HasPrefix(glob, "$HOME/"):
		full = filepath.Join(home, glob[len("$HOME/"):])
	case strings.HasPrefix(glob, "${HOME}/"):
		full = filepath.Join(home, glob[len("${HOME}/"):])
	case filepath.IsAbs(glob):
		full = filepath.Clean(glob)
	case dir != "" && !strings.HasPrefix(glob, "~") && !strings.HasPrefix(glob, "$"):
		full = filepath.Join(dir, glob)
	case varCredentialGlob(glob):
		return true
	default:
		return false
	}
	rel, err := filepath.Rel(home, full)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return false
	}
	parts := strings.Split(rel, "/")
	for _, r := range credentialHomes {
		if homePartsMatch(parts, strings.Split(r, "/")) {
			return true
		}
	}
	return false
}

// varCredentialGlob is whether glob starts with a variable other than
// HOME (`$H`, `${X}`, `"$D"`) whose path then names a home credentials
// directory by its name sequence, at any depth: the variable may hold HOME
// or a directory above it (T87d, L637).
func varCredentialGlob(glob string) bool {
	g := strings.NewReplacer(`"`, "", `'`, "").Replace(glob)
	if !strings.HasPrefix(g, "$") {
		return false
	}
	rest := g[1:]
	if strings.HasPrefix(rest, "{") {
		end := strings.IndexByte(rest, '}')
		if end < 0 {
			return false
		}
		rest = rest[end+1:]
	} else {
		rest = strings.TrimLeftFunc(rest, func(r rune) bool {
			return r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		})
	}
	if !strings.HasPrefix(rest, "/") {
		return false
	}
	parts := strings.Split(strings.Trim(path.Clean(rest), "/"), "/")
	for off := range parts {
		for _, r := range credentialHomes {
			if homePartsMatch(parts[off:], strings.Split(r, "/")) {
				return true
			}
		}
	}
	return false
}

// HomeCredentialPath is whether a tool's path, from cwd, is in a home
// credentials directory (credentialHomes), through symlinks; when the tool
// searches recursively (Grep, Glob), also whether it is at or above one —
// home itself included — since the search descends into it whatever its
// glob or type filter (T87d, L637). An empty path is cwd for a recursive
// search. A `~user` or other unresolved variable path keeps today's reading.
func HomeCredentialPath(p, cwd string, recursive bool) bool {
	home := project.Home()
	if home == "" || (p == "" && !recursive) {
		return false
	}
	var full string
	switch {
	case p == "":
		full = cwd
	case p == "~" || p == "$HOME" || p == "${HOME}":
		full = home
	case strings.HasPrefix(p, "~/"):
		full = filepath.Join(home, p[2:])
	case strings.HasPrefix(p, "$HOME/"):
		full = filepath.Join(home, p[len("$HOME/"):])
	case strings.HasPrefix(p, "${HOME}/"):
		full = filepath.Join(home, p[len("${HOME}/"):])
	case strings.HasPrefix(p, "~") || strings.ContainsAny(p, "$`"):
		return false
	default:
		full = project.Resolve(cwd, p)
	}
	if full == "" {
		return false
	}
	for _, l := range []string{full, project.Realish(full)} {
		for _, r := range credentialHomes {
			for _, d := range []string{filepath.Join(home, r), project.Realish(filepath.Join(home, r))} {
				if project.Within(l, d) || (recursive && project.Within(d, l)) {
					return true
				}
			}
		}
	}
	return false
}

// homePartsMatch is whether glob parts can name the credentials directory
// parts dir, or a path under or above it on its way.
func homePartsMatch(parts, dir []string) bool {
	for i := 0; i < len(parts) && i < len(dir); i++ {
		g, d := parts[i], dir[i]
		if g == "**" {
			return true
		}
		if ok, err := path.Match(g, d); err != nil || !ok || (strings.HasPrefix(d, ".") && !strings.HasPrefix(g, ".")) {
			return false
		}
	}
	return true
}

// extensionGlobRefused is whether an extension glob, expanded from cwd,
// can reach a credentials file: it is not plain (a `~`, `$`, brace, or
// pathspec magic the guard cannot resolve), its directory is in or above a
// home credentials directory, or a walk of its directory at any depth meets
// a credentials file or cannot finish (credential_walk.go).
func extensionGlobRefused(glob, cwd, home string) bool {
	if cwd == "" || home == "" || !plainGlob.MatchString(glob) {
		return true
	}
	start, rel := cwd, glob
	if strings.HasPrefix(glob, "/") {
		start, rel = "/", strings.TrimLeft(glob, "/")
	}
	lit, globbed := start, false
	for _, seg := range strings.Split(rel, "/") {
		switch {
		case globbed && seg == "..":
			return true // the walk reads names below, never above
		case isGlob(seg):
			globbed = true
		case !globbed:
			lit = filepath.Join(lit, seg)
		}
	}
	if !globbed {
		return true
	}
	for _, l := range []string{lit, project.Realish(lit)} {
		for _, r := range credentialHomes {
			d := filepath.Join(home, r)
			if project.Within(l, d) || project.Within(d, l) {
				return true
			}
		}
	}
	found, ok := globReachesFrom(start, rel)
	return found || !ok
}

// credentialWord finds a credentials file named in code or shell text, as a
// quoted or bare path whose last part is one: `open('.env')`, `cat
// config/.env.local`.
var credentialWord = lazyre.New(`(?:^|[\s'"(=:,/])((?:\.env(?:\.[A-Za-z0-9_-]+)*|\.envrc|id_(?:rsa|dsa|ecdsa|ed25519)|credentials\.json|\.netrc|\.pgpass|\.pypirc))(?:$|[\s'")\],;])`)

// codeCredentials is the credentials files code names (CredentialsIn) but
// those it names only as string words outside the project (codeOutside).
func codeCredentials(code, dir string, ctx *Ctx, vars map[string]string) []string {
	words := strings.FieldsFunc(strings.Join(sh.CodeStrings(code), "\n"), func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\'' || r == '"' || r == '=' || r == ','
	})
	var out []string
	for _, c := range CredentialsIn(code) {
		outside, named := 0, 0
		for _, w := range words {
			if filepath.Base(w) == c && strings.ContainsAny(w, "/~") && codeOutside(code, w, dir, ctx, vars) {
				outside++
			}
		}
		for _, m := range credentialWord.FindAllStringSubmatch(code, -1) {
			if m[1] == c {
				named++
			}
		}
		if outside == 0 || outside != named {
			out = append(out, c)
		}
	}
	return out
}

// codeOutside is whether the string word w of code names a path outside
// the project, the sandbox's (T128f): every literal of it is the whole
// string, a call's first argument or an assigned value, never joined to
// another part (`cwd + '/.env'`, `join(root, '/x')`), and it resolves
// outside the project (projectPath).
func codeOutside(code, w, dir string, ctx *Ctx, vars map[string]string) bool {
	if a := pathOf(w, dir, ctx, vars); a == "" || projectPath(a, ctx) {
		return false
	}
	seen := false
	for i := strings.Index(code, w); i >= 0; {
		end := i + len(w)
		if i == 0 || end >= len(code) || !strings.ContainsRune("'\"`", rune(code[i-1])) || code[end] != code[i-1] {
			return false
		}
		before := strings.TrimRight(code[:i-1], " \t")
		after := strings.TrimLeft(code[end+1:], " \t")
		if (before != "" && !strings.ContainsRune("(=[:", rune(before[len(before)-1]))) || (after != "" && !strings.ContainsRune("),;]\n", rune(after[0]))) {
			return false
		}
		seen = true
		j := strings.Index(code[end:], w)
		if j < 0 {
			break
		}
		i = end + j
	}
	return seen
}

// projectPath is whether abs, written or resolved, may lie in the project:
// its roots, its worktrees, or the directory the call runs in; a glob, where
// its base holds one of them too.
func projectPath(abs string, ctx *Ctx) bool {
	ours := slices.Concat(ctx.Roots, ctx.Worktrees, []string{ctx.Cwd})
	for _, p := range []string{abs, project.Realish(abs)} {
		g, globbed := globBase(p)
		if slices.ContainsFunc(ours, func(r string) bool {
			return r != "" && (project.Within(p, r) || (globbed && (project.Within(g, r) || project.Within(r, g))))
		}) {
			return true
		}
	}
	return false
}

// CredentialsIn is the credentials files text names, each once.
func CredentialsIn(text string) []string {
	var out []string
	for _, m := range credentialWord.FindAllStringSubmatch(text, -1) {
		if CredentialFile(m[1]) && !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

// revWord is a git object name `<rev>:<path>`, the index's `:<path>` and
// `:<stage>:<path>` included.
var revWord = lazyre.New(`^(?:[^\s:\-][^\s:]*)?(?::[0-3])?:([^:].*)$`)

// revPath is the path of a `<rev>:<path>` word (`HEAD:*.json`,
// `main:config/.env`): git reads it from history, so it names a
// credentials file by its name alone (T94, L636).
func revPath(w string) (string, bool) {
	m := revWord.FindStringSubmatch(w)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// CredentialsWhy is the refusal of touching a credentials file; a glob is
// named as the pattern it is.
func CredentialsWhy(name string) string {
	what := name + " holds credentials"
	if isGlob(name) {
		what = "`" + name + "` matches a credentials file"
	}
	return what + ", which the owner handles: no agent reads, edits, or deletes it. " +
		"A template such as .env.example is read and edited like any file; ask the owner for a value the work needs"
}
