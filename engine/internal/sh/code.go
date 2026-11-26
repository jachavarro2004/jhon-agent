package sh

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"slices"
	"strings"
)

// An interpreter's inline code, shared by framework-guard and safety-guard:
// one recogniser of the code that writes or deletes a path it names.
var (
	// CodeInterpreter is the interpreters whose inline code the recognisers read, by program name.
	CodeInterpreter = lazyre.New(`^(python\d?(\.\d+)?|node(?:js)?|perl|ruby|php|deno|bun)$`)
	// PathTokens are the path-like tokens of inline code.
	PathTokens = lazyre.New(`[^\s'"` + "`" + `(),;=<>{}\[\]]+`)
	// CodeWrite is an API or operator that writes, deletes, moves, or links a file.
	CodeWrite = lazyre.New(strings.Join([]string{
		`open\([^)]*['"][wax+]`, perlOpenWrite, `\.write`, `writeFile`, `appendFile`, `>>`, `\bprint\s*\{`, `unlink`, `\brm(?:Sync|dir|dirSync)?\s*\(`,
		`rmtree`, `\bremove\s*\(`, `\brename`, `copyFile`, `cpSync`, `\bcp\s*\(`, `symlink`, `truncate`, `FileUtils\.`, `File\.delete`,
		`shutil\.(?:move|copy)`, `\bmkdir`, EditorDelete,
	}, "|"))
	// EditorDelete is an editor's own delete: vim's and nvim's delete() and
	// vim.fs.rm, libuv's fs_rmdir, emacs's delete-directory and delete-file
	// (T257). A method `.delete(` is not one: it is as often a map's or an
	// HTTP client's.
	EditorDelete = `(?:^|[^.\w]|\bfn\.)delete\s*\(|\bdelete-(?:directory|file)\b|\bvim\.fs\.rm\b|\bfs_rmdir\b`
	// FileWrite is code that writes, deletes or moves a file, whatever path
	// it names (T94b, L667): an open for writing, a file API that writes,
	// a filesystem module's delete, move or copy. A generic `.write(`,
	// `>>`, `.remove(` or `.rename(` is not one: it is as often a stream,
	// an operator, a list or a data frame. An open whose mode is a variable
	// counts, since the guard cannot tell it reads (L678), and so does
	// os.open with a write flag.
	FileWrite = lazyre.New(strings.Join([]string{
		`\bopen\s*\((?:[^()]|\([^()]*\))*,\s*(?:mode\s*=\s*)?['"][rwabtx+]*[wax+][rwabtx+]*['"]`, `\.open\s*\(\s*['"][wax]`, perlOpenWrite, `\bfdopen\b`,
		`\bopen\s*\((?:[^(),]|\([^()]*\))+,\s*(?:mode\s*=\s*)?[A-Za-z_]\w*\s*[,)]`, `\bos\.open\s*\([^)]*\bO_(?:WRONLY|RDWR|CREAT|APPEND|TRUNC)\b`,
		`write_(?:text|bytes)\s*\(`, `writeFile`, `appendFile`, `createWriteStream`, `\b(?:File|IO)\.(?:write|binwrite|delete|unlink|rename)\b`,
		`\bos\.(?:remove|unlink|rename|replace|rmdir|removedirs|truncate|symlink|link|makedirs|mkdir)\b`, `\bshutil\.(?:move|copy\w*|rmtree|chown|unpack_archive|make_archive)`, `\bfs(?:\.promises)?\.\w*(?:rm|unlink|rename|copy|cp|symlink|link|truncate|mkdir|write|append)`,
		`\b(?:unlink|rmtree|copyFile|cpSync|symlink|rmSync|renameSync|FileUtils\.\w+)\b`, `\.unlink\s*\(`, `\bprint\s*\{`,
		`\bfile_put_contents\b`, `\bDeno\.(?:write|remove|rename|copy|create|mkdir|truncate)`, `\bBun\.write\b`,
	}, "|"))
	// CodeExec is an API that runs another program (backticks run one in perl and ruby).
	CodeExec = lazyre.New("exec|spawn|system|popen|subprocess|`|\\bfork\\b|Command\\(")
	// perlOpenWrite is perl's open for writing: `open(F, ">path")`,
	// `open my $fh, '>', $p`, `>>`, `+<` and `+>` included.
	perlOpenWrite = `\bopen\s*\(?\s*(?:my\s+)?[$\w]+\s*,\s*['"]\s*(?:\+?>|\+<)`
	codeString    = lazyre.New(`'((?:[^'\\]|\\.)*)'|"((?:[^"\\]|\\.)*)"|` + "`((?:[^`\\\\]|\\\\.)*)`|" +
		// A word list: perl's qw(…) (any of its usual brackets or a slash), ruby's %w[…] and %W[…] (T257).
		`\bqw\s*(?:\(([^)]*)\)|\[([^\]]*)\]|\{([^}]*)\}|<([^>]*)>|/([^/]*)/)|%[wW](?:\[([^\]]*)\]|\(([^)]*)\)|\{([^}]*)\})`)
	escapedChar = lazyre.New(`\\(.)`)
)

// CodeWrites is whether inline code names a path isTarget accepts and writes or deletes something.
func CodeWrites(code string, isTarget func(string) bool) bool {
	return CodeWrite.MatchString(code) && slices.ContainsFunc(PathTokens.FindAllString(code, -1), isTarget)
}

// CodeStrings is the string literals of inline code, in order, unescaped as far as a command line needs.
func CodeStrings(code string) []string {
	var out []string
	for _, m := range codeString.FindAllStringSubmatchIndex(code, -1) {
		for g := 1; 2*g < len(m); g++ {
			if m[2*g] < 0 {
				continue
			}
			if g <= 3 {
				out = append(out, escapedChar.ReplaceAllString(code[m[2*g]:m[2*g+1]], "$1"))
			} else {
				out = append(out, strings.Fields(code[m[2*g]:m[2*g+1]])...)
			}
			break
		}
	}
	return out
}

// ── shell writes of a path: a segment-level recogniser over the raw command ──

var heredocStart = lazyre.New(`<<-?\s*(['"]?)(\w+)(['"]?)`)

// WithoutBodies is a command's text with every heredoc body removed.
func WithoutBodies(cmd string) string {
	var out []string
	end := ""
	inBody := false
	for _, l := range strings.Split(cmd, "\n") {
		if inBody {
			if strings.TrimSpace(l) == end {
				inBody = false
			}
			continue
		}
		out = append(out, l)
		for _, m := range heredocStart.FindAllStringSubmatch(l, -1) {
			if m[1] == "" || m[1] == m[3] {
				end, inBody = m[2], true
				break
			}
		}
	}
	return strings.Join(out, "\n")
}

var shellWord = lazyre.New(`'([^']*)'|"((?:[^"\\]|\\.)*)"|(\S+)`)

// Words is the shell words of one segment, quotes removed: enough to read operands and redirections.
func Words(s string) []string {
	var out []string
	for _, m := range shellWord.FindAllStringSubmatchIndex(s, -1) {
		for g := 1; g <= 3; g++ {
			if m[2*g] >= 0 {
				out = append(out, s[m[2*g]:m[2*g+1]])
				break
			}
		}
	}
	return out
}

// A Write is a segment that writes a target path, and why.
type Write struct{ Segment, Why string }

var (
	segments     = lazyre.New(`&&|\|\||;|\||\n`)
	redirectOp   = lazyre.New(`^(\d*>>?|&>>?|>\|)$`)
	redirectWord = lazyre.New(`^(\d*>>?|&>>?)\S`)
	redirectLead = lazyre.New(`^\d*>>?|^&>>?|^>\|`)
	inPlace      = lazyre.New(`^-[a-zA-Z]*i`)
	opWriters    = set("cp", "mv", "rm", "truncate", "install", "ln", "touch", "rsync", "patch", "dd", "shred", "unlink")
)

// InPlace is whether a sed or perl argument edits in place.
func InPlace(a string) bool { return inPlace.MatchString(a) || strings.HasPrefix(a, "--in-place") }

// ShellWrites is every segment of a command, heredoc bodies aside, that
// writes a path isTarget accepts: a redirection to it, a program that
// writes its operand, or an interpreter's inline code that names it and
// writes. isTarget receives each word as written.
func ShellWrites(cmd string, isTarget func(string) bool) []Write {
	var out []Write
	for _, seg := range segments.Split(WithoutBodies(cmd), -1) {
		if seg = strings.TrimSpace(seg); seg == "" {
			continue
		}
		ws := Words(seg)
		prog := Base(at(ws, 0))
		// `2>&1`, `>&2`, `2>&-`: a descriptor duplication or close, never a file.
		target := func(w string) bool {
			t := redirectLead.ReplaceAllString(w, "")
			return !strings.HasPrefix(t, "&") && isTarget(t)
		}
		redirected := false
		for i, w := range ws {
			if (redirectOp.MatchString(w) && target(at(ws, i+1))) || (redirectWord.MatchString(w) && target(w)) {
				redirected = true
				break
			}
		}
		if redirected {
			out = append(out, Write{seg, "a redirection"})
			continue
		}
		if CodeInterpreter.MatchString(prog) && CodeWrites(seg, isTarget) {
			out = append(out, Write{seg, prog + " code"})
			continue
		}
		if len(ws) < 2 || !slices.ContainsFunc(ws[1:], func(w string) bool { return !strings.HasPrefix(w, "-") && isTarget(w) }) {
			continue
		}
		why := ""
		switch {
		case prog == "tee":
			why = "tee"
		case (prog == "sed" || prog == "perl") && slices.ContainsFunc(ws, InPlace):
			why = prog + " -i"
		case prog == "git" && slices.Contains([]string{"mv", "rm", "checkout", "restore", "apply"}, at(ws, 1)):
			why = "git " + ws[1]
		case opWriters[prog]:
			why = prog
		}
		if why != "" {
			out = append(out, Write{seg, why})
		}
	}
	return out
}
