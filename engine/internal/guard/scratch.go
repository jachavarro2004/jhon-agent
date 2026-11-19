package guard

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// A directory a command line makes with `mktemp -d` is the agent's own
// scratch (T64c): a variable assigned one in the same line names it, so
// `d=$(mktemp -d) && … && rm -r $d` removes a directory under the temporary
// root, which the deletion rules allow. The rule is conservative; the line
// proves, or the variable stays unresolvable as before:
//   - outside single-quoted text the line has no backslash, backquote,
//     `#`, heredoc or `;;`, outside quoted text no newline, and it runs
//     no eval, source or `.`, and its parsed tree passes lineFacts
//     (scratch_ast.go, L791);
//   - the variable is assigned once in the whole line (`v=` or `v+=`),
//     as one whole command at the top level, `v=$(mktemp -d …)`, with -d
//     and at most -q, -t, -p <dir> (or --tmpdir=<dir>) and one template: a
//     template with a `/` under /tmp or $TMPDIR, one without under -t or
//     -p, no `..`;
//   - no word of the line is the variable's bare name (read, export,
//     unset, for, printf -v), no cd or pushd names it, nothing before
//     the assignment reads it, and no single-quoted text does (another
//     shell's, where it is unset);
//   - every reference is `$d` or `${d}`, alone or followed by a path with
//     no `..` part (`$d/out.jsonl`), quoted or not;
//   - the assignment is not run in a pipe, a subshell, a group or the
//     background, nor after `||`; it runs before every rm or rmdir that
//     names the variable: unconditionally (first in the line, or after
//     `;`), or in the same `&&` chain as the rm (T64c, L519);
//   - the line names no IFS; no ln, mv, cp, install, rsync, mount or link
//     names the variable, and nothing names it after an rm or rmdir of it.
// The variable then reads as a directory under the template's directory
// (the temporary root, or -p's) only where an rm or rmdir names it whole,
// `$d`, `"$d"` or `${d}` (L514): `$d/x`, `$d/`, a glob or a brace stays
// unresolvable, and so does every other program's use. A whole line `rm [-flags] $(mktemp -d)`
// removes the directory it makes.

// scratchSeg is one part of a line at its top level and the operators
// around it.
type scratchSeg struct{ text, before, after string }

var (
	scratchAssign = lazyre.New(`^([A-Za-z_][A-Za-z0-9_]*)="?\$\(\s*mktemp((?:\s+(?:[A-Za-z0-9_./=+-]+|"\$\{?TMPDIR\}?[A-Za-z0-9_./+-]*"|\$\{?TMPDIR\}?[A-Za-z0-9_./+-]*))*)\s*\)"?$`)
	mktempRmLine  = lazyre.New(`^\s*rm(?:\s+-[A-Za-z]+)*\s+"?\$\(\s*mktemp\s+-d\s*\)"?\s*$`)
	scratchBanned = set("eval", "source", ".")
	scratchMovers = set("cd", "pushd")
	// scratchWord is an operand naming a variable whole: `$d`, `${d}`, quoted or not.
	scratchWord = lazyre.New(`^"?\$(?:([A-Za-z_][A-Za-z0-9_]*)|\{([A-Za-z_][A-Za-z0-9_]*)\})"?$`)
	// ifsWord is IFS named anywhere: a line that may set it splits `$d` its own way.
	ifsWord = lazyre.New(`(?:^|[^A-Za-z0-9_])IFS(?:[^A-Za-z0-9_]|$)`)
	// scratchLinkers make, move or copy a path onto the directory, or mount on it.
	scratchLinkers = set("ln", "mv", "cp", "install", "rsync", "mount", "link")
	scratchFirst   = lazyre.New(`^[\s({!]*([^\s;&|()]+)`)
)

// scratchSegments is a line split at its top-level operators; ok is false
// for a line the rule does not read.
func scratchSegments(line string) ([]scratchSeg, bool) {
	// A backslash may stand only in single-quoted text, a newline in quoted
	// text, which is literal (T87b: a `python3 -c "…"` of several lines).
	bare := maskSingle(line)
	if strings.ContainsAny(bare, "\\`#") || strings.ContainsAny(maskDouble(bare), "\n\r") || strings.Contains(bare, "<<") || strings.Contains(bare, ";;") {
		return nil, false
	}
	var segs []scratchSeg
	in, depth, start, before := byte(0), 0, 0, ""
	cut := func(i int, op string) {
		segs = append(segs, scratchSeg{text: strings.TrimSpace(line[start:i]), before: before, after: op})
		start, before = i+len(op), op
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case in == '\'':
			if c == '\'' {
				in = 0
			}
		case c == '\'' && in == 0:
			in = c
		case c == '"':
			if in == '"' {
				in = 0
			} else {
				in = c
			}
		case c == '(':
			depth++
		case c == ')':
			if depth--; depth < 0 {
				return nil, false
			}
		case in != 0 || depth > 0:
		case c == '\n' || c == ';':
			cut(i, ";")
		case c == '&' && strings.HasPrefix(line[i:], "&&"):
			cut(i, "&&")
			i++
		case c == '&' && ((i > 0 && line[i-1] == '>') || strings.HasPrefix(line[i:], "&>")):
		case c == '&':
			cut(i, "&")
		case c == '|' && (strings.HasPrefix(line[i:], "||") || strings.HasPrefix(line[i:], "|&")):
			cut(i, line[i:i+2])
			i++
		case c == '|':
			cut(i, "|")
		}
	}
	if in != 0 || depth != 0 {
		return nil, false
	}
	cut(len(line), "")
	return segs, true
}

// scratchVars is the variables line proves scratch directories, each with
// the path it reads as.
func scratchVars(line string, ctx *Ctx) map[string]string {
	segs, ok := scratchSegments(line)
	facts := lineFacts(line)
	if !ok || ifsWord.MatchString(line) || facts == nil {
		return nil
	}
	for _, s := range segs {
		if m := scratchFirst.FindStringSubmatch(s.text); m != nil && scratchBanned[m[1]] {
			return nil
		}
	}
	out := map[string]string{}
	for i, s := range segs {
		m := scratchAssign.FindStringSubmatch(s.text)
		if m == nil {
			continue
		}
		name := m[1]
		dir, ok := mktempDir(m[2], ctx)
		if !ok || !facts.resolves(name) || !scratchUse(line, name, segs, i) {
			continue
		}
		out[name] = dir
	}
	return out
}

// scratchUse is whether name, assigned in segs[at], is used only as the rule allows.
func scratchUse(line, name string, segs []scratchSeg, at int) bool {
	q := regexp.QuoteMeta(name)
	if len(regexp.MustCompile(`(?:^|[^A-Za-z0-9_])`+q+`\+?=`).FindAllStringIndex(line, -1)) != 1 {
		return false
	}
	if slices.Contains(strings.FieldsFunc(line, func(r rune) bool { return strings.ContainsRune(" \t\n;&|()<>\"'", r) }), name) {
		return false
	}
	a := segs[at]
	if a.before == "||" || a.before == "|" || a.before == "|&" || a.after == "|" || a.after == "|&" || a.after == "&" {
		return false
	}
	ref := regexp.MustCompile(`\$(?:` + q + `|\{!?` + q + `)(?:[^A-Za-z0-9_]|$)`)
	// Every reference is `$d` or `${d}`, alone or before a path with no `..`.
	plain := regexp.MustCompile(`\$(?:` + q + `|\{` + q + `\})((?:/[^\s"';&|)$]*)?)(?:[\s"';&|)]|$)`)
	refs, plains := ref.FindAllString(line, -1), plain.FindAllStringSubmatch(line, -1)
	// A reference in single quotes is another shell's (`bash -c '… $d'`), where it is unset.
	if len(refs) != len(plains) || len(ref.FindAllString(maskSingle(line), -1)) != len(refs) || slices.ContainsFunc(plains, func(m []string) bool { return slices.Contains(strings.Split(m[1], "/"), "..") }) {
		return false
	}
	// The assignment runs before every rm of it: unconditionally (first, or
	// after `;`), or in the same `&&` chain as the rm.
	always := a.before == "" || a.before == ";"
	gated, removed := true, false
	for i, s := range segs {
		uses := ref.MatchString(s.text)
		if i < at && uses {
			return false
		}
		if i <= at {
			continue
		}
		first := ""
		if m := scratchFirst.FindStringSubmatch(s.text); m != nil {
			first = m[1]
		}
		if uses && (scratchMovers[first] || scratchLinkers[first] || removed) {
			return false
		}
		gated = gated && (s.before == "&&" || s.before == "|")
		rm := uses && (first == "rm" || first == "rmdir")
		if rm && !always && !gated {
			return false
		}
		// After an rm of it, the name may be made again as anything: nothing may name it later.
		removed = removed || rm
	}
	return true
}

// mktempDir is the path `mktemp <args>` makes, read as a directory under
// its template's directory; ok is false for anything the rule does not read.
func mktempDir(argText string, ctx *Ctx) (string, bool) {
	var dirFlag, tFlag bool
	pdir, tmpl := "", ""
	args := strings.Fields(strings.ReplaceAll(argText, `"`, ""))
	for k := 0; k < len(args); k++ {
		a := args[k]
		switch {
		case a == "-d" || a == "--directory":
			dirFlag = true
		case a == "-q" || a == "--quiet":
		case a == "-t":
			tFlag = true
		case a == "-p" || a == "--tmpdir":
			if k++; k >= len(args) || pdir != "" {
				return "", false
			}
			pdir = args[k]
		case strings.HasPrefix(a, "--tmpdir="):
			if pdir != "" {
				return "", false
			}
			pdir = strings.TrimPrefix(a, "--tmpdir=")
		case strings.HasPrefix(a, "-") || tmpl != "":
			return "", false
		default:
			tmpl = a
		}
	}
	if !dirFlag || slices.Contains(strings.Split(pdir+"/"+tmpl, "/"), "..") {
		return "", false
	}
	tmp := os.Getenv("TMPDIR")
	if !filepath.IsAbs(tmp) {
		tmp = "/tmp"
	}
	expand := func(w string) string {
		for _, v := range []string{"${TMPDIR}", "$TMPDIR"} {
			if strings.HasPrefix(w, v) {
				return tmp + strings.TrimPrefix(w, v)
			}
		}
		return w
	}
	base := tmp
	if pdir != "" {
		if base = expand(pdir); !filepath.IsAbs(base) {
			base = filepath.Join(ctx.Cwd, base)
		}
	}
	switch {
	case tmpl == "":
		tmpl = "tmp.XXXXXXXXXX"
	case strings.Contains(tmpl, "/"):
		p := filepath.Clean(expand(tmpl))
		if tFlag || pdir != "" || !(project.Within(p, "/tmp") || project.Within(p, filepath.Clean(tmp))) {
			return "", false
		}
		return project.Realish(filepath.Dir(p)) + "/" + filepath.Base(p), true
	case !tFlag && pdir == "":
		return "", false // a bare template is made in the working directory
	}
	if strings.Contains(base, "$") {
		return "", false
	}
	return filepath.Join(project.Realish(filepath.Clean(base)), tmpl), true
}

// maskSingle is line with the text of each single-quoted string, read as
// the shell reads quotes, left out: `'…'` becomes `”`.
func maskSingle(line string) string {
	var b strings.Builder
	in := byte(0)
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case in == '\'':
			if c == '\'' {
				in = 0
				b.WriteByte(c)
			}
			continue
		case in == '"' && c == '\\' && i+1 < len(line):
			b.WriteByte(c)
			i++
			c = line[i]
		case in == 0 && c == '\'':
			in = c
		case c == '"':
			if in == '"' {
				in = 0
			} else if in == 0 {
				in = c
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// maskDouble is a maskSingle'd line with the text of each double-quoted
// string left out, so nothing a quote holds reads as a separator.
func maskDouble(bare string) string {
	var b strings.Builder
	in := false
	for i := 0; i < len(bare); i++ {
		if bare[i] == '"' {
			in = !in
		} else if in {
			continue
		}
		b.WriteByte(bare[i])
	}
	return b.String()
}

// mktempRm is whether the whole line removes the directory mktemp -d makes in it.
func mktempRm(line string) bool { return mktempRmLine.MatchString(line) }

// freshVars is the variables a line assigns a fresh `mktemp` file or
// directory to, each with the path it reads as (T94b, L667): a write to
// `$out` or `$d/x` lands in the temporary root (or mktemp's -p
// directory), never in the repository. The assignment is one whole command
// at the top level, made once in the line, not after `||` or in a pipe or
// the background, and not followed by `||` (`d=$(mktemp -d) || true` may
// leave it empty); nothing before it reads the variable. The line follows
// scratchSegments' rules, so a line it does not read resolves nothing,
// and lineFacts' (scratch_ast.go): the parsed line proves nothing redefines
// mktemp, PATH, TMPDIR or the variable.
func freshVars(line, cwd string) map[string]string {
	segs, ok := scratchSegments(line)
	facts := lineFacts(line)
	if !ok || ifsWord.MatchString(line) || facts == nil {
		return nil
	}
	out := map[string]string{}
	for i, s := range segs {
		m := scratchAssign.FindStringSubmatch(s.text)
		if m == nil {
			continue
		}
		name, args := m[1], m[2]
		if !facts.resolves(name) {
			continue
		}
		if !slices.Contains(strings.Fields(args), "-d") && !slices.Contains(strings.Fields(args), "--directory") {
			args = " -d" + args
		}
		p, ok := mktempDir(args, &Ctx{Cwd: cwd})
		if !ok || strings.Contains(p, "$") {
			continue
		}
		q := regexp.QuoteMeta(name)
		if len(regexp.MustCompile(`(?:^|[^A-Za-z0-9_])`+q+`\+?=`).FindAllStringIndex(line, -1)) != 1 {
			continue
		}
		if slices.Contains([]string{"||", "|", "|&"}, s.before) || slices.Contains([]string{"||", "|", "|&", "&"}, s.after) {
			continue
		}
		ref := regexp.MustCompile(`\$(?:` + q + `|\{!?` + q + `)(?:[^A-Za-z0-9_]|$)`)
		if slices.ContainsFunc(segs[:i], func(b scratchSeg) bool { return ref.MatchString(b.text) }) {
			continue
		}
		out[name] = p
	}
	return out
}
