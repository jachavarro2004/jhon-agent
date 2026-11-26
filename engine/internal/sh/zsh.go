package sh

import (
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
// substitutions run as it is expanded. nil for any other.
func codeOf(name, index, value string) ([]*Command, error) {
	switch {
	case name == "PROMPT_COMMAND" || codeTables[name] && index != "":
		return Parse(value)
	case promptVars[name] && index == "":
		r := &reader{src: value}
		r.document(value)
		return r.cmds, r.err
	}
	return nil, nil
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
// a command line's, and a qualifier that runs code as a GlobCode command.
// The parser keeps the pattern as raw text, so its substitutions are read
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
	if qualifierRunsCode(pat) {
		glob := r.text(x)
		r.cmds = append(r.cmds, &Command{Words: []string{GlobCode, glob}, Raw: glob, Loops: append([]Loop(nil), r.loops...),
			Scope: append([]int64(nil), r.scope...)})
	}
}

// tildeExpansion is zsh's `$~name` (`$^~name`, `$=~name`), which bash reads
// as literal text: its value is read as a glob, qualifiers and all.
var tildeExpansion = lazyre.New(`(?:^|[^\\])\$[\^=]*~`)

// globSubst reads unquoted literal text that holds zsh's `$~name` as a
// GlobCode command: the value it globs may hold a qualifier that runs code.
func (r *reader) globSubst(lits string) {
	if tildeExpansion.MatchString(lits) {
		r.cmds = append(r.cmds, &Command{Words: []string{GlobCode, lits}, Raw: lits, Loops: append([]Loop(nil), r.loops...),
			Scope: append([]int64(nil), r.scope...)})
	}
}

// qualifierRunsCode is whether pat, the text of a glob's parenthesised
// group, is a zsh qualifier list that runs code. An unquoted `|` outside
// any nested group or expansion makes it an alternation, unless it opens
// with `#q`. An expansion in it may supply its qualifiers (zsh reads the
// expanded text as qualifiers): it may run code. Otherwise the list is
// read as zsh's glob.c reads it, quotes removed: the other qualifiers'
// arguments are skipped (f's mode, u's and g's id, P's word, a range, a
// subscript), so that only an e string with its closing delimiter, a
// + with a name, or an o or O sort by either counts; the modifiers after
// `:` run none. A character zsh refuses is skipped and the reading goes
// on, which can only find more code.
func qualifierRunsCode(pat string) bool {
	explicit := strings.HasPrefix(pat, "#q")
	if !explicit && alternation(pat) {
		return false
	}
	if strings.ContainsAny(pat, "$`") {
		return true
	}
	q := dequote(pat)
	if explicit {
		q = q[2:]
	}
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
	for i := 0; i < len(q); {
		c := q[i]
		i++
		switch c {
		case ':':
			return false
		case 'e':
			if _, ok := delimited(q, i); ok {
				return true
			}
		case '+':
			if i < len(q) && identChar(q[i]) {
				return true
			}
		case 'o', 'O':
			if i < len(q) {
				if _, ok := delimited(q, i+1); q[i] == 'e' && ok || q[i] == '+' && i+1 < len(q) && identChar(q[i+1]) {
					return true
				}
				i++
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
	return false
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

// dequote is s with its quotes removed and each backslash escape read.
func dequote(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'', '"':
		case '\\':
			if i+1 < len(s) {
				i++
				b.WriteByte(s[i])
			}
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
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
// itself, and `repeat N` runs it N times. A brace group after them,
// `repeat 2 { … }`, which the parser reads as words, is its commands.
// ok is false for any other program, or with no command after it.
func precommand(name string, w []string) (rest []string, ok bool) {
	k := 0
	switch name {
	case "-", "nocorrect", "noglob", "builtin":
		k = 1
	case "repeat":
		k = 2
	default:
		return nil, false
	}
	if len(w) <= k {
		return nil, false
	}
	rest = w[k:]
	if rest[0] == "{" {
		rest = rest[1:]
		if n := len(rest); n > 0 && rest[n-1] == "}" {
			rest = rest[:n-1]
		}
	}
	return rest, true
}
