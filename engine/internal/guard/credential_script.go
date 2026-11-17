package guard

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

// A script the allow-list admits (scriptGlobs) globs only relative to the
// directory it runs in: its text can name no other directory, load no code
// the guard does not read, and run no program that searches or moves on
// its own. Anything else keeps the name rule.

var (
	// scriptElsewhere is text that names or reaches another directory: an
	// absolute path, a home, a `..` part, the environment, the arguments or
	// input, the script's own place, a change of directory, a directory
	// walk, decoded text, or code built at run time.
	scriptElsewhere = lazyre.New(`['"` + "`" + `]\s*(?:/|~|file:)|(?:^|[/'"` + "`" + `\s(,=])\.\.(?:[/'"` + "`" + `\s),]|$)|\$\{|\b(?:` +
		`expanduser|expandvars|homedir|home|HOME|USERPROFILE|environ|getenv|process\.env|Bun|argv|stdin|input|readline|` +
		`chdir|cwd|getcwd|root_dir|dir_fd|dirname|__dirname|__file__|__filename|tmpdir|gettempdir|tempfile|mkdtemp|` +
		`realpath|abspath|resolve|parent|parents|pardir|walk|scandir|listdir|readdir|opendir|pathlib|PurePath|Path|` +
		`eval|exec|Function|fromCharCode|atob|Buffer|b64decode|base64|codecs|unhexlify|decode|importlib|__import__|` +
		`createRequire|Deno|globalThis|meta|cd|pushd|popd)\b`)
	// jsImport is a module a JavaScript text loads, and jsDynamic a load the guard cannot read.
	jsImport  = lazyre.New(`(?:\bfrom\s*|\bimport\s*|\brequire\s*\(\s*)['"]([^'"]*)['"]`)
	jsDynamic = lazyre.New(`\b(?:import|require)\s*\(\s*[^'"\s]`)
	// pyImport is the modules a Python line loads.
	pyImport = lazyre.New(`(?m)^\s*(?:from\s+(\S+)\s+import\b|import\s+([^#\n]+))`)
	// scriptModules are the modules an admitted script may load: none of
	// them globs from a directory the script does not name.
	scriptModules = []string{"node:child_process", "child_process", "node:fs", "fs", "node:fs/promises", "fs/promises", "node:util", "util",
		"node:assert", "assert", "fast-glob", "glob", "globby", "tinyglobby",
		"json", "re", "sys", "subprocess", "fnmatch", "yaml", "hashlib", "datetime", "time", "collections", "typing", "itertools",
		"functools", "math", "csv", "dataclasses", "textwrap", "string", "shlex"}
	// searchers are the programs a script's strings may not run: each
	// searches, moves, or runs other text on its own.
	searchers = set("find", "rg", "grep", "egrep", "fgrep", "tar", "git", "rsync", "zip", "unzip", "fd", "fdfind", "ag", "ack", "cp", "mv",
		"xargs", "cd", "pushd", "popd", "sh", "bash", "zsh", "dash", "ksh", "fish", "env", "sudo", "eval", "exec", "source", "tree", "du",
		"rm", "ln", "node", "python", "python3", "bun", "deno", "npx", "tsx", "perl", "ruby", "builtin", "command")
)

// scriptCan is whether a script's text, Python or else JavaScript, can
// reach no directory but the one it runs in, through the relative strings
// it holds.
func scriptCan(text string, python bool) bool {
	if scriptElsewhere.MatchString(text) || jsDynamic.MatchString(text) {
		return false
	}
	if !python {
		return !slices.ContainsFunc(jsImport.FindAllStringSubmatch(text, -1), func(m []string) bool { return !slices.Contains(scriptModules, m[1]) })
	}
	for _, m := range pyImport.FindAllStringSubmatch(text, -1) {
		mods := m[1]
		if mods == "" {
			mods = m[2]
		}
		for _, mod := range strings.Split(mods, ",") {
			if mod = strings.Fields(mod + " x")[0]; !slices.Contains(scriptModules, mod) {
				return false
			}
		}
	}
	return true
}

// shadowed is whether a Python script's directory holds a module of the
// name of one it imports, which Python loads before the standard one.
func shadowed(text, dir string) bool {
	for _, m := range pyImport.FindAllStringSubmatch(text, -1) {
		for _, mod := range strings.Split(m[1]+","+m[2], ",") {
			if mod = strings.Fields(mod + " x")[0]; mod == "x" {
				continue
			}
			for _, p := range []string{mod + ".py", mod} {
				if _, err := os.Lstat(filepath.Join(dir, p)); err == nil {
					return true
				}
			}
		}
	}
	return false
}

var (
	// codeHome is code that asks for the home directory.
	codeHome = lazyre.New(`\b(?:expanduser|homedir|home|HOME|USERPROFILE)\b`)
	// upSegment is a `..` part of a path, which leaves the directory the code runs in.
	upSegment = lazyre.New(`(?:^|[/\\])\.\.(?:[/\\]|$)`)
)

// credentialHomeNamed is code naming a home credentials directory by part.
var credentialHomeNamed = lazyre.New(`\.(?:ssh|gnupg|aws|config|netrc)\b|\bgcloud\b|\bgh\b`)

// proseCredentialWords is whether every string of code holding the bare
// word credentials is prose (T109 WU3, L784 c): three words or more and no
// path part, as a prompt or message is, never a file name.
func proseCredentialWords(code string) bool {
	for _, s := range append(sh.CodeStrings(code), sh.CodeStrings(joinConcat(code))...) {
		ws := strings.FieldsFunc(s, func(r rune) bool {
			return r == ' ' || r == '\t' || r == '\n' || r == '\'' || r == '"' || r == '=' || r == ','
		})
		if slices.ContainsFunc(ws, func(w string) bool { return strings.EqualFold(w, "credentials") }) && (len(ws) < 3 || strings.ContainsAny(s, "/\\~$`")) {
			return false
		}
	}
	return true
}

// wholeCredentialStrings is the strings of code that are one word each.
func wholeCredentialStrings(code string) []string {
	var out []string
	for _, s := range append(sh.CodeStrings(code), sh.CodeStrings(joinConcat(code))...) {
		if t := strings.TrimSpace(s); !strings.ContainsAny(t, " \t\n") {
			out = append(out, t)
		}
	}
	return out
}

// codeCredentialsElsewhere refuses code whose strings glob or name a
// credentials file (by the name rule) under an absolute path, `~`, or a
// `..` part, or
// anywhere when the code asks for the home directory (T28 G2): no
// allow-list can expand it there.
func codeCredentialsElsewhere(name, code string) []Finding {
	home := codeHome.MatchString(code)
	var out []Finding
	seen := map[string]bool{}
	prose := proseCredentialWords(code)
	for _, w := range stringWords(code) {
		named := credentialGlobWord(w) || CredentialFile(w)
		if named && strings.EqualFold(w, "credentials") && prose && !credentialHomeNamed.MatchString(code) && !slices.Contains(wholeCredentialStrings(code), w) {
			continue
		}
		if !named || seen[w] || !(home || strings.HasPrefix(w, "/") || strings.HasPrefix(w, "~") || upSegment.MatchString(w)) {
			continue
		}
		seen[w] = true
		out = append(out, denyCredentials(name+" code naming "+w, CredentialsWhy(w)))
	}
	return out
}
