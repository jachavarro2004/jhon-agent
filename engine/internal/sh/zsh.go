package sh

import (
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"mvdan.cc/sh/v3/syntax"
)

// GlobCode is the program word of the command a glob qualifier that runs
// code is read as (T228): its one argument is the glob as written. zsh
// treats a trailing parenthesised group of a glob as qualifiers
// (BARE_GLOB_QUAL, on by default, and `(#q…)`), and runs an e string
// (`*(e:'…':)`), a +command (`*(+name)`) and an o or O sort by either in
// the shell itself; a guard refuses the line, as it reads no such code.
// The parser reads such a group as bash's extended glob (`*(…)`, `?(…)`,
// `+(…)`, `@(…)`, `!(…)`); any other one is a syntax error, unreadable.
const GlobCode = "<zsh glob qualifier code>"

// GlobSubst is the program word of the command an assignment to zsh's
// options[globsubst] is read as (T228 L2): with GLOB_SUBST on, each
// unquoted expansion is a glob, whose qualifiers may run code. Its one
// argument is the assignment as written.
const GlobSubst = "<zsh GLOB_SUBST>"

// OptionName is a zsh option name as zsh compares it: case and the
// underscores (and, on the command line, the dashes) ignored.
func OptionName(s string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(s))
}

// promptVars are the prompts whose substitutions the shell runs when it
// expands them: bash's PS0 to PS4 (PS4 at each traced command, PS1 at each
// prompt), zsh's PROMPT, RPROMPT and kin under PROMPT_SUBST.
var promptVars = set("PS0", "PS1", "PS2", "PS3", "PS4", "PROMPT", "PROMPT2", "PROMPT3", "PROMPT4", "RPROMPT", "RPROMPT2",
	"RPS1", "RPS2", "SPROMPT")

// codeTables are zsh's tables whose entries are code: a function's body, an alias's value.
var codeTables = set("functions", "aliases", "galiases", "saliases", "dis_functions", "dis_aliases")

// codeOf is the commands an assignment of value to name (at index, for an
// array or a table) runs later (T228 L2): PROMPT_COMMAND's value, and a
// zsh function's or alias's entry, run as a command line; a prompt's
// substitutions run as it is expanded. A command variable's value
// (commandVars, L8) runs as a command line too, read when it parses, its
// `|` (LESSOPEN) or `!` (a git alias) dropped, and an ssh one's ssh left
// to the program that runs it; a git configuration the environment gives
// (GIT_CONFIG_VALUE_n, GIT_CONFIG_PARAMETERS) is read as a command,
// whatever its key. nil for any other.
func codeOf(name, index, value string) ([]*Command, error) {
	switch {
	case name == "PROMPT_COMMAND" || codeTables[name] && index != "":
		return Parse(value)
	case promptVars[name] && index == "":
		r := &reader{src: value}
		r.document(value)
		return r.cmds, r.err
	case commandVars[name] || gitConfigValue.MatchString(name):
		return commandValue(name, value), nil
	case name == "GIT_CONFIG_PARAMETERS":
		var out []*Command
		for _, m := range gitConfigParameter.FindAllStringSubmatch(value, -1) {
			out = append(out, commandValue(name, m[1])...)
		}
		return out, nil
	}
	return nil, nil
}

// commandVars are the variables whose value a program runs as a command
// line: git's (GIT_EXTERNAL_DIFF, GIT_PAGER, GIT_EDITOR, GIT_SSH,
// GIT_SSH_COMMAND, GIT_ASKPASS, GIT_SEQUENCE_EDITOR, GIT_PROXY_COMMAND),
// and the pager, editor and helper of git, man, crontab, less and the rest.
var commandVars = set("GIT_EXTERNAL_DIFF", "GIT_PAGER", "PAGER", "GIT_EDITOR", "EDITOR", "VISUAL", "GIT_SSH", "GIT_SSH_COMMAND",
	"GIT_ASKPASS", "SSH_ASKPASS", "SUDO_ASKPASS", "GIT_SEQUENCE_EDITOR", "GIT_PROXY_COMMAND", "LESSOPEN", "LESSCLOSE", "MANPAGER")

var (
	// gitConfigValue is GIT_CONFIG_VALUE_n.
	gitConfigValue = lazyre.New(`^GIT_CONFIG_VALUE_\d+$`)
	// gitConfigParameter is one 'key'='value' of GIT_CONFIG_PARAMETERS.
	gitConfigParameter = lazyre.New(`'[^']*'='([^']*)'`)
)

// commandValue is the commands value runs as the command line of the
// variable name, nil when it does not parse.
func commandValue(name, value string) []*Command {
	value = strings.TrimLeft(strings.TrimSpace(value), "|!")
	cmds, err := Parse(value)
	if err != nil {
		return nil
	}
	if strings.Contains(name, "SSH") {
		cmds = slices.DeleteFunc(cmds, func(c *Command) bool { b := Base(at(c.Words, 0)); return b == "ssh" || b == "scp" })
	}
	return cmds
}

// assigned reads what an assignment runs later (codeOf), and an assignment
// to zsh's options[globsubst] (or one whose key or table it cannot tell)
// as a GlobSubst command.
func (r *reader) assigned(name, index, value string, array *syntax.ArrayExpr) {
	if name == "options" && (index != "" || array != nil) {
		text := strings.Trim(index, `"'`)
		label := name + "[" + index + "]=" + value
		if array != nil {
			text, label = r.text(array), name+"="+r.text(array)
		}
		if strings.Contains(OptionName(text), "globsubst") || strings.ContainsAny(text, "$`") {
			r.cmds = append(r.cmds, &Command{Words: []string{GlobSubst, label}, Raw: label, Loops: append([]Loop(nil), r.loops...),
				Scope: append([]int64(nil), r.scope...)})
		}
		return
	}
	values, idx := []string{value}, index
	if array != nil {
		// An array's (or a table's) every element.
		values, idx = nil, "[]"
		for _, e := range array.Elems {
			if v, ok := literal(e.Value); ok {
				values = append(values, v)
			}
		}
	}
	for _, v := range values {
		cmds, err := codeOf(name, idx, v)
		if err != nil && r.err == nil {
			r.err = err
		}
		r.cmds = append(r.cmds, cmds...)
	}
}

// literal is w's value when it holds no expansion, quotes removed.
func literal(w *syntax.Word) (string, bool) {
	if w == nil {
		return "", false
	}
	var b strings.Builder
	for _, p := range w.Parts {
		switch x := p.(type) {
		case *syntax.Lit:
			b.WriteString(unescape(x.Value, false))
		case *syntax.SglQuoted:
			if x.Dollar {
				b.WriteString(AnsiC(x.Value))
			} else {
				b.WriteString(x.Value)
			}
		case *syntax.DblQuoted:
			for _, q := range x.Parts {
				l, ok := q.(*syntax.Lit)
				if !ok {
					return "", false
				}
				b.WriteString(unescape(l.Value, true))
			}
		default:
			return "", false
		}
	}
	return b.String(), true
}

// documentCommands is the commands of the substitutions of s, read as a
// here-document's (document); nil when it does not parse.
func documentCommands(s string) []*Command {
	r := &reader{src: s}
	r.document(s)
	if r.err != nil {
		return nil
	}
	return r.cmds
}

// document reads the substitutions of s, text read as a here-document's
// (quotes are no quotes there: a substitution inside them is read too).
func (r *reader) document(s string) {
	if !strings.ContainsAny(s, "$`") {
		return
	}
	w, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Document(strings.NewReader(s))
	if err != nil {
		if r.err == nil {
			r.err = &Unreadable{Why: "the text " + s + ": " + err.Error()}
		}
		return
	}
	sub := &reader{src: s, loops: r.loops, scope: r.scope}
	sub.word(w)
	r.cmds = append(r.cmds, sub.cmds...)
	if sub.err != nil && r.err == nil {
		r.err = sub.err
	}
}

// extGlob reads an extended glob: the substitutions its pattern runs, as
// a command line's, and the code its zsh qualifiers run (qualifiers). The
// parser keeps the pattern as raw text, so its substitutions are read
// here, as a here-document's (quotes are no quotes there: a substitution
// inside them is read too). A process substitution in it is unreadable.
func (r *reader) extGlob(x *syntax.ExtGlob) {
	pat := x.Pattern.Value
	if strings.Contains(pat, "<(") || strings.Contains(pat, ">(") {
		if r.err == nil {
			r.err = &Unreadable{Why: "a process substitution inside an extended glob"}
		}
		return
	}
	r.document(pat)
	r.qualifiers(pat, r.text(x))
}

// qualifiers reads the code a glob's parenthesised group pat runs as zsh
// qualifiers (qualifierCodes): each e string and +command is read as the
// command line it is, and judged as it is judged bare (T228 L6: zsh runs
// `!(tests)`'s e string `t`, which is no removal); its quotes are the
// shell's (L8: `e:'sh -c "…"':` stays a shell running a string). A group
// whose qualifiers an expansion may supply is a GlobCode command, glob as
// written.
func (r *reader) qualifiers(pat, glob string) {
	q := qualifierCodes(pat)
	if q.dynamic {
		r.globCode(glob)
		return
	}
	// A reading that does not parse (a quote one reading cuts) is left out;
	// code no reading parses is unreadable.
	read, texts := false, false
	seen := map[string]bool{}
	for _, readings := range q.codes {
		for _, c := range readings {
			texts = true
			cmds, err := Parse(c)
			if err != nil {
				continue
			}
			read = true
			// Readings that agree add their commands once.
			for _, cmd := range cmds {
				if k := strings.Join(cmd.Words, "\x00"); !seen[k] {
					seen[k] = true
					r.cmds = append(r.cmds, cmd)
				}
			}
		}
	}
	if texts && !read && r.err == nil {
		r.err = &Unreadable{Why: "the code of the glob qualifier " + glob + " does not parse"}
	}
}

// globRewrites is whether the qualifiers of a glob's group pat may make its
// matches other names than the files it matches (T228 L8): a P word, a
// modifier (:h, :a, :s, :r, :e, :t, …), a +command, an e string that does
// more than test (it may set REPLY or reply), or qualifiers an expansion
// supplies. A removal of such a glob has a target the guard cannot resolve.
func globRewrites(pat string) bool {
	q := qualifierCodes(pat)
	return q.dynamic || q.rewrite
}

// globCode adds a GlobCode command for glob.
func (r *reader) globCode(glob string) {
	r.cmds = append(r.cmds, &Command{Words: []string{GlobCode, glob}, Raw: glob, Loops: append([]Loop(nil), r.loops...),
		Scope: append([]int64(nil), r.scope...)})
}

// paramExp reads what a parameter expansion runs beyond its substitutions
// (T228 L5): the qualifiers of a glob in its default word (`${x:-*(e:…:)}`,
// `${x-…}`, `${x:+…}`, `${x:=…}`), and an assignment to zsh's
// options[globsubst] (`${options[globsubst]:=on}`) as a GlobSubst command.
func (r *reader) paramExp(x *syntax.ParamExp) {
	if x.Exp == nil || x.Exp.Word == nil {
		return
	}
	if x.Param != nil && x.Param.Value == "options" && x.Index != nil &&
		(x.Exp.Op == syntax.AssignUnset || x.Exp.Op == syntax.AssignUnsetOrNull) {
		if key := r.text(x.Index); strings.Contains(OptionName(key), "globsubst") || strings.ContainsAny(key, "$`") {
			label := r.text(x)
			r.cmds = append(r.cmds, &Command{Words: []string{GlobSubst, label}, Raw: label, Loops: append([]Loop(nil), r.loops...),
				Scope: append([]int64(nil), r.scope...)})
		}
	}
	for _, g := range parenGroups(r.text(x.Exp.Word)) {
		r.qualifiers(g, r.text(x))
	}
}

// parenGroups is the text of each top-level parenthesised group of t that
// is no substitution (`$(…)`, `<(…)`, `>(…)`), outside quotes.
func parenGroups(t string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(t); i++ {
		switch c := t[i]; {
		case c == '\\':
			i++
		case c == '\'' && depth == 0:
			if j := strings.IndexByte(t[i+1:], '\''); j >= 0 {
				i += j + 1
			}
		case c == '(' && depth > 0:
			depth++
		case c == '(' && (i == 0 || !strings.ContainsRune("$<>", rune(t[i-1]))):
			depth, start = 1, i+1
		case c == '(':
			// A substitution: skip to its close.
			n := 1
			for i++; i < len(t) && n > 0; i++ {
				if t[i] == '(' {
					n++
				} else if t[i] == ')' {
					n--
				}
			}
			i--
		case c == ')' && depth > 0:
			if depth--; depth == 0 {
				out = append(out, t[start:i])
			}
		}
	}
	return out
}

// tildeExpansion is zsh's `$~name` (`$^~name`, `$=~name`, `$~1`, `$~@`, …),
// which bash reads as literal text: its value is read as a glob, qualifiers
// and all. `$~` with no name after it is the text it is.
var tildeExpansion = lazyre.New(`(?:^|[^\\])\$[\^=~]*~[\^=~#+]*[A-Za-z_0-9@*{]`)

// globSubst reads unquoted literal text that holds zsh's `$~name` as a
// GlobCode command: the value it globs may hold a qualifier that runs code.
func (r *reader) globSubst(lits string) {
	if tildeExpansion.MatchString(lits) {
		r.globCode(lits)
	}
}

// globSubstParts reads the runs of unquoted literals among parts (the
// parser splits `$~name` into `$` and `~name`) with globSubst.
func (r *reader) globSubstParts(parts []syntax.WordPart) {
	lits := ""
	for _, p := range parts {
		if l, ok := p.(*syntax.Lit); ok {
			lits += l.Value
			continue
		}
		r.globSubst(lits)
		lits = ""
	}
	r.globSubst(lits)
}

// A qualifierList is what a glob's parenthesised group runs as a zsh
// qualifier list (qualifierCodes).
type qualifierList struct {
	// codes are the code it runs, each as the texts it may be read as.
	codes [][]string
	// dynamic is set when an expansion in it, outside its code, may supply
	// its qualifiers (zsh reads the expanded text as qualifiers).
	dynamic bool
	// rewrite is set when its matches may become other names (globRewrites).
	rewrite bool
}

// qualifierCodes reads pat, the text of a glob's parenthesised group, as a
// zsh qualifier list: each closed e string's code and each +command's name
// (also as an o or O sort). An unquoted `|` outside any nested group or
// expansion makes it an alternation, unless it opens with `#q`. The list
// is read as zsh's glob.c reads it: the other qualifiers' arguments are
// skipped (f's mode, u's and g's id, P's word, a range, a subscript). It is
// read twice, which can only find more code: with the quotes removed, as
// zsh compares the characters (its code taken from the text as written, so
// a quoted string in it stays one), and with a quoted delimiter skipped
// (`*(e-'rm -rf ~'-)`, its code as written and with its outer quotes off).
func qualifierCodes(pat string) qualifierList {
	var out qualifierList
	explicit := strings.HasPrefix(pat, "#q")
	if !explicit && alternation(pat) {
		return out
	}
	if explicit {
		pat = pat[2:]
	}
	q, at := dequoteMap(pat)
	plain := scanQualifiers(q, delimited, func(i, j int) string { return pat[at[i] : at[j-1]+1] })
	// The same code with its quotes and escapes removed, as zsh may read
	// `e:rm -rf\ ~:`: one more reading, never in place of the others.
	bare := scanQualifiers(q, delimited, func(i, j int) string { return q[i:j] })
	raw := scanQualifiers(pat, quotedDelimited, func(i, j int) string { return pat[i:j] })
	// Text outside the code an expansion may turn into qualifiers.
	rest := pat
	for _, c := range raw.codes {
		rest = strings.Replace(rest, c.text, "", 1)
	}
	out.dynamic = strings.ContainsAny(rest, "$`")
	out.rewrite = plain.rewrite || raw.rewrite
	seen := map[string]bool{}
	for _, c := range append(append(plain.codes, raw.codes...), bare.codes...) {
		// Code quoted whole (`e:'…':`) is the text inside its quotes.
		texts := []string{c.text}
		if n := len(c.text); n >= 2 && (c.text[0] == '\'' || c.text[0] == '"') && c.text[n-1] == c.text[0] {
			texts = []string{c.text[1 : n-1]}
		}
		var fresh []string
		for _, t := range texts {
			if !seen[t] {
				seen[t] = true
				fresh = append(fresh, t)
			}
			if !c.sort && !pureTest(t) {
				out.rewrite = true
			}
			// A command the code computes (`$(echo rm) -rf ~`) cannot be told: as an expansion's qualifiers.
			if computedProgram(t) {
				out.dynamic = true
			}
		}
		if fresh != nil {
			out.codes = append(out.codes, fresh)
		}
	}
	return out
}

// computedProgram is whether code runs a program its words compute: a
// command whose program word holds an expansion or a substitution.
func computedProgram(code string) bool {
	cmds, err := Parse(code)
	if err != nil {
		return false
	}
	for _, c := range cmds {
		k := 0
		for k < len(c.Words) && Assignment.MatchString(c.Words[k]) {
			k++
		}
		if k < len(c.Words) && strings.ContainsAny(c.Words[k], "$`…") {
			return true
		}
	}
	return false
}

// pureTests are the commands an e string may run and still leave REPLY
// and reply as they are: a test (`[[ … ]]` runs none).
var pureTests = set("test", "[", "true", "false", ":")

// pureTest is whether code only tests: it parses, and each of its
// commands is a pureTests one that names no REPLY or reply assignment.
func pureTest(code string) bool {
	cmds, err := Parse(code)
	if err != nil {
		return false
	}
	for _, c := range cmds {
		if len(c.Words) == 0 || !pureTests[c.Words[0]] {
			return false
		}
		for _, w := range c.Words {
			if replyWord.MatchString(w) {
				return false
			}
		}
	}
	return true
}

// replyWord is an assignment to REPLY or reply.
var replyWord = lazyre.New(`^(?:REPLY|reply)(?:\[.*\])?\+?=`)

// dequoteMap is s with its quotes removed and each backslash escape read,
// and for each of its bytes the index in s it comes from.
func dequoteMap(s string) (string, []int) {
	var b strings.Builder
	var at []int
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'', '"':
		case '\\':
			if i+1 < len(s) {
				i++
				b.WriteByte(s[i])
				at = append(at, i)
			}
		default:
			b.WriteByte(s[i])
			at = append(at, i)
		}
	}
	return b.String(), at
}

// quotedDelimited is delimited with the quoted text in q skipped.
func quotedDelimited(q string, i int) (int, bool) {
	if i >= len(q) {
		return i, false
	}
	end := q[i]
	switch end {
	case '(':
		end = ')'
	case '[':
		end = ']'
	case '{':
		end = '}'
	case '<':
		end = '>'
	}
	for j := i + 1; j < len(q); j++ {
		switch c := q[j]; {
		case c == end:
			return j + 1, true
		case c == '\\':
			j++
		case c == '\'' || c == '"':
			k := strings.IndexByte(q[j+1:], c)
			if k < 0 {
				return i, false
			}
			j += k + 1
		}
	}
	return i, false
}

// A qcode is a piece of code a qualifier list runs, and whether it is a sort key.
type qcode struct {
	text string
	sort bool
}

// A qscan is one reading of a qualifier list: its code, and whether a P
// word, a modifier or a +command may make its matches other names.
type qscan struct {
	codes   []qcode
	rewrite bool
}

// scanQualifiers reads the qualifier list q: its delimited arguments found
// by delim, its code's text taken by text from q's indices.
func scanQualifiers(q string, delim func(string, int) (int, bool), text func(i, j int) string) (out qscan) {
	delimited := delim
	num := func(i int) int {
		for i < len(q) && q[i] >= '0' && q[i] <= '9' {
			i++
		}
		return i
	}
	signed := func(i int) int {
		if i < len(q) && (q[i] == '+' || q[i] == '-') {
			i++
		}
		return num(i)
	}
	// code reads the e string or +command at i (after its e or +), the index after it.
	code := func(c byte, i int, sort bool) int {
		if c == '+' {
			j := i
			for j < len(q) && identChar(q[j]) {
				j++
			}
			if j > i {
				out.codes = append(out.codes, qcode{text(i, j), sort})
				out.rewrite = out.rewrite || !sort
			}
			return j
		}
		if j, ok := delimited(q, i); ok {
			if j-1 > i+1 {
				out.codes = append(out.codes, qcode{text(i+1, j-1), sort})
			}
			return j
		}
		return i
	}
	for i := 0; i < len(q); {
		c := q[i]
		i++
		switch c {
		case ':':
			// Modifiers: they rewrite each match (:h, :a, :s/…/…/, …).
			out.rewrite = out.rewrite || i < len(q)
			return out
		case 'e', '+':
			i = code(c, i, false)
		case 'o', 'O':
			if i < len(q) {
				if q[i] == 'e' || q[i] == '+' {
					i = code(q[i], i+1, true)
				} else {
					i++
				}
			}
		case 'f':
			i = modeSpec(q, i)
		case 'u', 'g':
			if i < len(q) && q[i] >= '0' && q[i] <= '9' {
				i = num(i)
			} else if j, ok := delimited(q, i); ok {
				i = j
			}
		case 'P':
			out.rewrite = true
			if j, ok := delimited(q, i); ok {
				i = j
			}
		case 'a', 'm', 'c':
			if i < len(q) && strings.IndexByte("hmwMsd", q[i]) >= 0 {
				i++
			}
			i = signed(i)
		case 'L':
			if i < len(q) && strings.IndexByte("pPkKmMgGtT", q[i]) >= 0 {
				i++
			}
			i = signed(i)
		case 'l':
			i = signed(i)
		case 'd', 'Y':
			i = num(i)
		case '%':
			if i < len(q) && (q[i] == 'b' || q[i] == 'c') {
				i++
			}
		case '[':
			if j := strings.IndexByte(q[i:], ']'); j >= 0 {
				i += j + 1
			}
		}
	}
	return out
}

// delimited is the index after a delimited argument that starts at i,
// zsh's get_strarg: its first character is the delimiter, `(`, `[`, `{`
// and `<` closed by their pair; ok is false when it is never closed.
func delimited(q string, i int) (int, bool) {
	if i >= len(q) {
		return i, false
	}
	end := q[i]
	switch end {
	case '(':
		end = ')'
	case '[':
		end = ']'
	case '{':
		end = '}'
	case '<':
		end = '>'
	}
	j := strings.IndexByte(q[i+1:], end)
	if j < 0 {
		return i, false
	}
	return i + 1 + j + 1, true
}

// modeSpec is the index after f's mode argument at i (zsh's
// qgetmodespec): an octal form, `[=+-]` and digits, or a delimited one.
func modeSpec(q string, i int) int {
	if i >= len(q) {
		return i
	}
	if c := q[i]; c == '=' || c == '+' || c == '-' || c == '?' || c >= '0' && c <= '7' {
		if c == '=' || c == '+' || c == '-' {
			i++
		}
		for i < len(q) && (q[i] >= '0' && q[i] <= '7' || q[i] == '?') {
			i++
		}
		return i
	}
	if j, ok := delimited(q, i); ok {
		return j
	}
	return i
}

// identChar is a character of a +command's name: a letter, a digit or `_`.
func identChar(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// alternation is whether pat holds an unquoted `|` outside any nested
// group, substitution, parameter expansion or quotes: zsh reads such a
// parenthesised group as alternatives, never as qualifiers.
func alternation(pat string) bool {
	depth, braces := 0, 0
	for i := 0; i < len(pat); i++ {
		switch c := pat[i]; {
		case c == '\\':
			i++
		case c == '\'' || c == '`':
			j := strings.IndexByte(pat[i+1:], c)
			if j < 0 {
				return false
			}
			i += j + 1
		case c == '"':
			for i++; i < len(pat) && pat[i] != '"'; i++ {
				if pat[i] == '\\' {
					i++
				}
			}
			if i >= len(pat) {
				return false
			}
		case c == '$' && i+1 < len(pat) && pat[i+1] == '{':
			braces++
			i++
		case c == '}' && braces > 0:
			braces--
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == '|' && depth == 0 && braces == 0:
			return true
		}
	}
	return false
}

// precommand is the words a zsh precommand modifier runs (T228): `-`,
// nocorrect, noglob and builtin run the command after them in the shell
// itself, and `repeat N` runs it N times; a `--` after them is skipped
// (L5). A brace group, after them or as a command (`repeat 2 { … }`,
// zsh's `{cmd …}`, which the parser reads as words), is its commands; so
// is a brace list as the command's first word (`{a,b}` is `a b`). ok is
// false for any other program, or with no command after it.
func precommand(name string, w []string) (rest []string, ok bool) {
	k := 0
	switch {
	case name == "-" || name == "nocorrect" || name == "noglob" || name == "builtin":
		k = 1
	case name == "repeat":
		k = 2
	case len(w) > 0 && strings.HasPrefix(w[0], "{") && w[0] != "{" && w[0] != "{}":
		k = 0
	default:
		return nil, false
	}
	if len(w) <= k {
		return nil, false
	}
	rest = w[k:]
	if k > 0 && rest[0] == "--" {
		if rest = rest[1:]; len(rest) == 0 {
			return nil, false
		}
	}
	if g := group(rest); g != nil {
		return g, true
	}
	if k == 0 {
		return nil, false
	}
	return rest, true
}

// braceList is a word that is one brace list, `{a,b,…}`.
var braceList = lazyre.New(`^\{([^{}]*,[^{}]*)\}$`)

// group is the words of a brace group or a brace list that w opens with,
// or nil: `{ a b }` and `{a b}` are a b; `{a,b} c` is a b c.
func group(w []string) []string {
	switch {
	case len(w) == 0:
		return nil
	case braceList.MatchString(w[0]):
		return append(strings.Split(braceList.FindStringSubmatch(w[0])[1], ","), w[1:]...)
	case w[0] == "{":
		w = w[1:]
	case strings.HasPrefix(w[0], "{") && w[0] != "{}":
		w = append([]string{w[0][1:]}, w[1:]...)
	default:
		return nil
	}
	w = slices.Clone(w)
	if n := len(w); n > 0 && w[n-1] == "}" {
		w = w[:n-1]
	} else if n > 0 && strings.HasSuffix(w[n-1], "}") {
		w[n-1] = strings.TrimSuffix(w[n-1], "}")
	}
	if len(w) == 0 || w[0] == "" {
		return nil
	}
	return w
}

// expandAliases reads a command whose program word is an alias the line
// defines before it (an `alias` command earlier in cmds) as the alias's
// value, its words before the command's own (T228 L5): bash with
// expand_aliases, and zsh, expand an alias a line defined on a later line,
// and the reader expands it on the same line too, which fails closed. A
// zsh global alias (alias -g) is read wherever its name stands as a word.
// An alias's value is read as a command line where it is defined (runs).
func expandAliases(cmds []*Command) []*Command {
	normal, global := map[string]string{}, map[string]string{}
	for _, c := range cmds {
		if at(c.Words, 0) == "alias" {
			g := false
			for _, a := range c.Words[1:] {
				if strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
					g = g || strings.Contains(a, "g")
					continue
				}
				if n, v, ok := strings.Cut(a, "="); ok && n != "" {
					if g {
						global[n] = v
					} else {
						normal[n] = v
					}
				}
			}
			continue
		}
		if len(normal)+len(global) == 0 {
			continue
		}
		words := c.Words
		// An alias's value may open with another alias: a bounded number of times.
		for range 8 {
			k := 0
			for k < len(words) && Assignment.MatchString(words[k]) {
				k++
			}
			v, ok := normal[at(words, k)]
			if !ok {
				break
			}
			words = append(append(slices.Clone(words[:k]), aliasWords(v)...), words[k+1:]...)
		}
		if len(global) > 0 {
			var out []string
			for _, w := range words {
				if v, ok := global[w]; ok {
					out = append(out, aliasWords(v)...)
				} else {
					out = append(out, w)
				}
			}
			words = out
		}
		c.Words = words
	}
	return cmds
}

// aliasWords is the words an alias's value gives a command that uses it:
// its last command's, as the reader reads it.
func aliasWords(v string) []string {
	cmds, err := Parse(v)
	if err != nil || len(cmds) == 0 {
		return strings.Fields(v)
	}
	return cmds[len(cmds)-1].Words
}
