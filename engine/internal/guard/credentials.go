package guard

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// The files that hold credentials, which the edit tools (secrets-guard) and
// a shell write or delete (safety-guard) never touch, and the home
// directories the sandbox hides (ProtectedHome). A template of one
// (`.env.example`) holds none, and is read and edited like any file. A read
// is not judged: guards limit only destructive acts (T128f, T151, L1366).

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

// credentialHomes are the home directories that hold credentials under any
// name: an edit tool's write in one is refused. They are the
// credential half of protectedHome plus gcloud's: Jhon's own install
// (.config/jhon, .local/share/jhon, .local/bin/jhon) is write-protected,
// not secret, and its prose is read by every session (L641).
var credentialHomes = append(slices.DeleteFunc(slices.Clone(protectedHome), func(p string) bool {
	return p == ".config/jhon" || strings.HasPrefix(p, ".local/")
}), ".config/gcloud")

// HomeCredentialPath is whether an edit tool's path, from cwd, is in a home
// credentials directory (credentialHomes), through symlinks (T94, L638). A
// `~user` or other unresolved variable path is not.
func HomeCredentialPath(p, cwd string) bool {
	home := project.Home()
	if home == "" || p == "" {
		return false
	}
	var full string
	switch {
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
				if project.Within(l, d) {
					return true
				}
			}
		}
	}
	return false
}

// credentialGlobWord is a glob read as naming a credentials file: by
// CredentialGlob, or by CredentialFile (`x*.pem`).
func credentialGlobWord(w string) bool {
	return isGlob(w) && (CredentialGlob(w) || CredentialFile(w))
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
