package guard

import (
	"regexp"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// The fresh and scratch mktemp rules (freshVars, scratchVars) resolve a
// variable only on a line whose parsed syntax tree proves nothing can change
// what mktemp is or returns, or what the variable holds (T109 WU1, L793).
// It is an allowlist resting on a shell fact: only builtins, keywords,
// functions and assignments change the shell's own state (variables, PATH
// lookup, the hash table, aliases, cwd); an external program runs in a child
// and cannot. The tree is read, not the text. Fail closed: a line the parser
// cannot read resolves nothing, and so does one where anything anywhere
// (conditions, loops, pipelines, subshells, command and process
// substitutions) is not:
//   - a simple command whose program word is one plain unquoted literal
//     matching astProgram (no braces, $, quotes, backslash or glob) and is
//     not a bash builtin or keyword (astBuiltins), except the state-safe
//     astSafe and cd (allowed only before a mktemp with no option but -d
//     and -q: the owner's seed line, `cd . && …; tmp=$(mktemp -d)`);
//   - if/while/until/case/for-in, {…}, (…), pipes, && and ||, and `!`;
//   - an assignment (prefix ones too, a `for` variable too) that is plain,
//     not indexed, not +=, to an all-lowercase name (bash's special
//     variables, PATH TMPDIR IFS CDPATH BASH_CMDS BASH_ENV …, are
//     uppercase) that is not one zsh ties to a special parameter
//     (astZsh: path is PATH there); or the tracked one: the only statement-level assignment
//     of its name, its value `$(mktemp …)` (mktempValue); or a plain prefix
//     assignment to any other name before a program that is no builtin or
//     keyword (`APP_DB=$d/t.db node …`, T224): it is only that program's
//     environment. It marks the facts prefixed, which freshVars does not read;
//   - no function, coproc, select, time, [[ ]], (( )), let, declaration,
//     C-style for, arithmetic assignment, ${x=}/${x:=}, {var}> redirect, or
//     extended glob (zsh's glob qualifiers run code in the shell, T224 L8);
//   - no arithmetic context at all (L796): bash evaluates a variable's value
//     as an arithmetic expression there, so z="PATH=0" and $((z)) assigns
//     PATH: no $((…)), $[…], ${x:…}, ${!x} (an indirect name may carry a
//     subscript), ${x@…} (@P expands like a prompt), and no subscript but a
//     plain number, @ or *; and test or [ with no -v operand: no argument
//     whose literal text holds `[` or is -v, and at most one argument with
//     an expansion (two could be -v and a subscript).
// A variable resolves only when it is assigned exactly once in the line and
// that assignment is a tracked mktemp one.

// astProgram is a plain program word.
var astProgram = regexp.MustCompile(`^[A-Za-z0-9_./+-]+$`)

// astLower is an assignable name: bash's special variables are uppercase.
var astLower = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// astZsh are zsh's lowercase special parameters: path, cdpath, fpath,
// manpath, module_path, mailpath and psvar are tied to PATH, CDPATH, FPATH,
// MANPATH, MODULE_PATH, MAILPATH and PSVAR, so assigning one changes what
// a program word runs (L830); pipestatus reads as the last exit status and
// histchars keeps only three characters (T224). Never assignable, tracked or not.
var astZsh = set("path", "cdpath", "fpath", "manpath", "module_path", "mailpath", "psvar", "pipestatus", "histchars")

// astBuiltins are bash 5.2's builtins (compgen -b) and keywords (compgen -k),
// and zsh's builtins that assign a variable or change the shell's state
// (T224 L6: `print -vd /` assigns d, `strftime -sd / 0` too), so none of them
// is ever read as an external program; nor are `repeat`, `-` and `private`,
// which run a command or assign in the shell itself (L8).
var astBuiltins = set(".", ":", "[", "alias", "bg", "bind", "break", "builtin", "caller", "cd", "command", "compgen", "complete",
	"compopt", "continue", "declare", "dirs", "disown", "echo", "enable", "eval", "exec", "exit", "export", "false", "fc", "fg",
	"getopts", "hash", "help", "history", "jobs", "kill", "let", "local", "logout", "mapfile", "popd", "printf", "pushd", "pwd",
	"read", "readarray", "readonly", "return", "set", "shift", "shopt", "source", "suspend", "test", "times", "trap", "true",
	"type", "typeset", "ulimit", "umask", "unalias", "unset", "wait",
	"if", "then", "else", "elif", "fi", "case", "esac", "for", "select", "while", "until", "do", "done", "in", "function", "time",
	"{", "}", "!", "[[", "]]", "coproc",
	"print", "strftime", "zparseopts", "vared", "getln", "sysread", "zstat", "zformat", "zregexparse", "zmodload", "integer",
	"float", "emulate", "setopt", "unsetopt", "noglob", "zle", "autoload", "functions", "disable", "alias", "zstyle", "ztcp",
	"zsocket", "zselect", "sysopen", "rehash", "unhash", "unfunction", "r", "sched", "zpty", "zcompile", "pcre_match", "zsystem",
	"nocorrect", "limit", "unlimit", "private", "repeat", "-")

// astSafe are the builtins that change no shell state. printf is not one
// (printf -v assigns).
var astSafe = set("echo", "true", "false", ":", "test", "[", "pwd")

// astName is a shell variable name.
var astName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// astTrackable is a name a tracked mktemp assignment may set: not one of
// bash's special variables. `_` never is: the shell sets it to the last
// argument of each command, so after `echo ~` it is the home folder (T224).
func astTrackable(n string) bool {
	if n == "_" {
		return false
	}
	if astLower.MatchString(n) {
		return true
	}
	return astName.MatchString(n) && !strings.HasPrefix(n, "BASH") &&
		!set("PATH", "TMPDIR", "CDPATH", "ENV", "IFS", "HOME", "PWD", "OLDPWD", "SHELLOPTS", "GLOBIGNORE", "FIGNORE", "PS4",
			"POSIXLY_CORRECT", "HISTFILE", "HOSTNAME", "LANG", "LC_ALL", "LC_COLLATE", "LC_CTYPE", "OPTIND", "OPTARG", "REPLY",
			"RANDOM", "SECONDS", "LINENO", "COLUMNS", "LINES", "MAIL", "MAILPATH", "TIMEFORMAT", "EXECIGNORE", "INPUTRC",
			"PROMPT_COMMAND", "PS0", "PS1", "PS2", "PS3", "SRANDOM", "EPOCHREALTIME", "EPOCHSECONDS", "FUNCNAME", "GROUPS",
			"DIRSTACK", "COMP_WORDBREAKS", "HISTCONTROL", "HISTIGNORE", "TERM", "SHELL", "UID", "EUID", "PPID", "SHLVL")[n]
}

// mktempFacts is what a line's tree proves about its mktemp variables.
type mktempFacts struct {
	// assigns counts every assignment to each name.
	assigns map[string]int
	// mktemp is each name whose (single) assignment is a clean mktemp
	// substitution, with whether mktemp took only -d and -q.
	mktemp map[string]bool
	// moves is whether the line runs cd.
	moves bool
	// prefixed is whether a program runs with a prefix assignment to a
	// name that is not all-lowercase (T224).
	prefixed bool
}

// lineFacts reads line's tree; nil when it is unreadable or not proven safe.
func lineFacts(line string) *mktempFacts {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(line), "")
	if err != nil {
		return nil
	}
	facts := &mktempFacts{assigns: map[string]int{}, mktemp: map[string]bool{}}
	bad := false
	syntax.Walk(f, func(node syntax.Node) bool {
		if bad {
			return false
		}
		switch x := node.(type) {
		case *syntax.FuncDecl, *syntax.CoprocClause, *syntax.LetClause, *syntax.DeclClause, *syntax.ArithmCmd,
			*syntax.TestClause, *syntax.TimeClause, *syntax.CStyleLoop, *syntax.TestDecl:
			bad = true
		case *syntax.ForClause:
			it, ok := x.Loop.(*syntax.WordIter)
			if x.Select || !ok || !astLower.MatchString(it.Name.Value) || astZsh[it.Name.Value] {
				bad = true
				break
			}
			facts.assigns[it.Name.Value]++
		case *syntax.Redirect:
			if x.N != nil && strings.Trim(x.N.Value, "0123456789") != "" {
				bad = true
			}
		case *syntax.ArithmExp:
			bad = true
		case *syntax.ExtGlob:
			// zsh runs a glob qualifier's code in the shell itself: `*(e:'eval d=~':)`, `*(+f)` (T224 L8).
			bad = true
		case *syntax.ParamExp:
			if x.Exp != nil && (x.Exp.Op == syntax.AssignUnset || x.Exp.Op == syntax.AssignUnsetOrNull || x.Exp.Op == syntax.OtherParamOps) ||
				x.Slice != nil || x.Excl || x.Index != nil && !astIndex(x.Index) {
				bad = true
			}
		case *syntax.BinaryArithm:
			switch x.Op {
			case syntax.Assgn, syntax.AddAssgn, syntax.SubAssgn, syntax.MulAssgn, syntax.QuoAssgn, syntax.RemAssgn,
				syntax.AndAssgn, syntax.OrAssgn, syntax.XorAssgn, syntax.ShlAssgn, syntax.ShrAssgn:
				bad = true
			}
		case *syntax.UnaryArithm:
			if x.Op == syntax.Inc || x.Op == syntax.Dec {
				bad = true
			}
		case *syntax.CallExpr:
			// A program that is no builtin or keyword runs in a child: its prefix assignments are its environment.
			external := false
			if len(x.Args) > 0 {
				prog, ok := plainProgram(x.Args[0])
				external = ok && !astBuiltins[prog]
			}
			for _, a := range x.Assigns {
				if a.Name == nil || a.Append || a.Index != nil || a.Naked || a.Array != nil || astZsh[a.Name.Value] {
					bad = true
					break
				}
				facts.assigns[a.Name.Value]++
				if len(x.Args) == 0 && astTrackable(a.Name.Value) {
					if plain, ok := mktempValue(a.Value); ok {
						facts.mktemp[a.Name.Value] = plain
						continue
					}
				}
				if !astLower.MatchString(a.Name.Value) {
					bad = bad || !external
					facts.prefixed = true
				}
			}
			if len(x.Args) == 0 {
				break
			}
			prog, ok := plainProgram(x.Args[0])
			if !ok || astBuiltins[prog] && !astSafe[prog] && prog != "cd" {
				bad = true
			}
			facts.moves = facts.moves || prog == "cd"
			if (prog == "test" || prog == "[") && !astTestSafe(x.Args[1:]) {
				bad = true
			}
		}
		return !bad
	})
	if bad {
		return nil
	}
	return facts
}

// trapSignals are the signals a cleanup trap may name: the shell's exit and
// the interrupts. Run at any of them, a scratch variable holds the folder or
// nothing (it is assigned once, from mktemp -d).
var trapSignals = set("EXIT", "0", "INT", "TERM", "HUP", "QUIT", "SIGINT", "SIGTERM", "SIGHUP", "SIGQUIT")

// trapExit reads the line's one top-level cleanup trap (T228 L2, L5): a
// trap whose action is quoted text, single (run at exit, `trap 'rm -rf
// "$d"' EXIT`) or double with plain variables in it (expanded as the trap
// is set, `trap "rm -rf $d" EXIT`), and whose signals are trapSignals. It
// gives the action's word as the reader reads it (the trap command it
// belongs to), the action to judge, and the scratch folders scratchVars
// proves for the line read as that action run at its end, the trap a
// no-op. The proof reads the idiom's common forms as their plain ones: a
// newline is `;`, `set -e` (-u, -x, -o pipefail …) and `exit [N]` change no
// variable (:), `local d` before the assignment declares it (:), `local`,
// `readonly`, `export` or `declare` of `d=$(mktemp -d …)` is that
// assignment, `cd "$d"` moves into the folder and `cp … "$d"` copies into
// it, neither changing d (:, cat), and `export TMPDIR=$(mktemp -d)` is
// read under a name of its own. A trap set with double quotes must come
// after its variables' assignments, run unconditionally. Every other form,
// and any abuse scratchVars refuses (another assignment by any route, a
// second trap, `$d/..`, IFS, …), gives "" and nil.
func trapExit(line string, ctx *Ctx) (word, action string, scratch map[string]string) {
	// An escaped quote inside double quotes (`trap "rm -rf \"$d\"" EXIT`) is
	// one the scratch rule does not read: the trap's segment keeps it, a
	// placeholder stands for it elsewhere.
	const quote = "\x01"
	segs, ok := scratchSegments(strings.ReplaceAll(flattenLines(line), `\"`, quote))
	if !ok {
		return "", "", nil
	}
	for i := range segs {
		if strings.Contains(segs[i].text, quote) && !strings.HasPrefix(strings.TrimSpace(segs[i].text), "trap") {
			return "", "", nil
		}
		segs[i].text = strings.ReplaceAll(segs[i].text, quote, `\"`)
	}
	at := -1
	for i, s := range segs {
		if m := scratchFirst.FindStringSubmatch(s.text); m != nil && m[1] == "trap" {
			if at >= 0 {
				return "", "", nil
			}
			at = i
		}
	}
	if at < 0 {
		return "", "", nil
	}
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(segs[at].text), "")
	if err != nil || len(f.Stmts) != 1 || len(f.Stmts[0].Redirs) > 0 {
		return "", "", nil
	}
	call, ok := f.Stmts[0].Cmd.(*syntax.CallExpr)
	if !ok || len(call.Assigns) > 0 || len(call.Args) < 3 {
		return "", "", nil
	}
	args := call.Args[1:]
	if v, _ := wordLit(args[0]); v == "--" {
		args = args[1:]
	}
	if len(args) < 2 || len(args[0].Parts) != 1 {
		return "", "", nil
	}
	for _, s := range args[1:] {
		if v, lit := wordLit(s); !lit || !trapSignals[v] {
			return "", "", nil
		}
	}
	var names []string
	switch q := args[0].Parts[0].(type) {
	case *syntax.SglQuoted:
		if q.Dollar {
			return "", "", nil
		}
		word = q.Value
	case *syntax.DblQuoted:
		var b strings.Builder
		for _, p := range q.Parts {
			switch x := p.(type) {
			case *syntax.Lit:
				b.WriteString(x.Value)
			case *syntax.ParamExp:
				if x.Param == nil || x.Exp != nil || x.Index != nil || x.Slice != nil || x.Repl != nil || x.Excl || x.Length || x.Width || x.Names != 0 {
					return "", "", nil
				}
				b.WriteString("$" + x.Param.Value)
				names = append(names, x.Param.Value)
			default:
				return "", "", nil
			}
		}
		word = unescape(b.String())
		if strings.ContainsRune(word, '\\') {
			return "", "", nil
		}
	default:
		return "", "", nil
	}
	// The proof's action: a reference in single quotes inside a double-quoted
	// trap was expanded when it was set, so it is a plain one.
	proofAction := word
	if names != nil {
		proofAction = regexp.MustCompile(`'(\$[A-Za-z_][A-Za-z0-9_]*)'`).ReplaceAllString(word, `"$1"`)
	}
	action = word
	var b strings.Builder
	assigned := map[string]int{}
	for i, s := range segs {
		t := s.text
		if m := scratchAssign.FindStringSubmatch(trapDecl.ReplaceAllString(t, "")); m != nil {
			if _, ok := assigned[m[1]]; !ok {
				assigned[m[1]] = i
			}
		}
		switch w := strings.Fields(t); {
		case i == at, trapNoop(w):
			t = ":"
		case trapDecl.MatchString(t) && len(w) == 2 && astName.MatchString(w[1]):
			// A declaration with no value, before its assignment.
			if _, ok := assigned[w[1]]; !ok {
				t = ":"
			}
		case trapDecl.MatchString(t) && scratchAssign.MatchString(trapDecl.ReplaceAllString(t, "")):
			t = trapDecl.ReplaceAllString(t, "")
		case len(w) >= 2 && w[0] == "cd" && trapRef.MatchString(w[len(w)-1]) && (len(w) == 2 || len(w) == 3 && w[1] == "--"):
			t = ":"
		case len(w) > 0 && w[0] == "cp":
			t = "cat" + t[2:]
		}
		b.WriteString(t + " " + s.after + " ")
	}
	proof := b.String() + "; " + proofAction
	// TMPDIR assigned from mktemp -d is the line's own scratch folder under a name of its own.
	if regexp.MustCompile(`(?:^|[;&|]\s*)TMPDIR="?\$\(\s*mktemp`).MatchString(strings.TrimSpace(proof)) {
		rename := strings.NewReplacer("${TMPDIR}", "${jhon_tmpdir}", "$TMPDIR", "$jhon_tmpdir", "TMPDIR=", "jhon_tmpdir=")
		proof, action = rename.Replace(proof), rename.Replace(action)
	}
	scratch = scratchVars(proof, ctx)
	if len(scratch) == 0 {
		return "", "", nil
	}
	// A double-quoted trap expands its variables as it is set: after their assignments, which always run.
	for _, n := range names {
		i, ok := assigned[n]
		if n == "TMPDIR" {
			i, ok = assigned["TMPDIR"]
		}
		if !ok || i > at || segs[i].before != "" && segs[i].before != ";" {
			return "", "", nil
		}
	}
	return word, action, scratch
}

var (
	// trapDecl is a declaration keyword before an assignment or a name.
	trapDecl = regexp.MustCompile(`^(?:local|readonly|export|declare|typeset)\s+`)
	// trapRef is a word that is a variable, quoted or not.
	trapRef = regexp.MustCompile(`^"?\$\{?[A-Za-z_][A-Za-z0-9_]*\}?"?$`)
	// trapSet is a set option that changes no variable: -e, -u, -x, -o pipefail and kin.
	trapSet = regexp.MustCompile(`^(?:[-+][euxvo]+|errexit|nounset|pipefail|xtrace|verbose)$`)
)

// trapNoop is whether w, a command's words, changes no variable and moves
// nowhere: `set` of trapSet options, or `exit [N]`.
func trapNoop(w []string) bool {
	switch {
	case len(w) == 0:
		return false
	case w[0] == "set" && len(w) > 1:
		for _, a := range w[1:] {
			if !trapSet.MatchString(a) {
				return false
			}
		}
		return true
	case w[0] == "exit":
		return len(w) == 1 || len(w) == 2 && strings.Trim(w[1], "0123456789") == ""
	}
	return false
}

// flattenLines is line with each newline outside quotes a `;`, or a space
// after an operator or an opening word, so the scratch rule reads a line of
// several commands as it reads one.
func flattenLines(line string) string {
	var b strings.Builder
	in := byte(0)
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case in == '\'':
			if c == '\'' {
				in = 0
			}
		case in == '"':
			if c == '\\' && i+1 < len(line) {
				b.WriteByte(c)
				i++
				c = line[i]
			} else if c == '"' {
				in = 0
			}
		case c == '\'' || c == '"':
			in = c
		case c == '\n':
			prev := strings.TrimRight(b.String(), " \t")
			last := prev[strings.LastIndexAny(prev, " \t;")+1:]
			if prev == "" || strings.ContainsRune("&|({;", rune(prev[len(prev)-1])) || last == "do" || last == "then" || last == "else" {
				c = ' '
			} else {
				b.WriteString(";")
				c = ' '
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// astIndex is a subscript bash cannot evaluate as arithmetic on a
// variable: a plain number, @ or *.
func astIndex(e syntax.ArithmExpr) bool {
	w, ok := e.(*syntax.Word)
	if !ok || len(w.Parts) != 1 {
		return false
	}
	l, ok := w.Parts[0].(*syntax.Lit)
	return ok && (l.Value == "@" || l.Value == "*" || strings.Trim(l.Value, "0123456789") == "" && l.Value != "")
}

// astTestSafe is whether test or [ with args cannot take a -v operand
// (whose subscript bash evaluates as arithmetic, L796).
func astTestSafe(args []*syntax.Word) bool {
	expanded := 0
	for _, a := range args {
		if v, lit := wordLit(a); lit {
			if v == "-v" || strings.Contains(v, "[") {
				return false
			}
			continue
		}
		expanded++
		if strings.Contains(litText(a.Parts), "[") {
			return false
		}
	}
	return expanded <= 1
}

// litText is the literal text in parts, expansions left out.
func litText(parts []syntax.WordPart) string {
	var b strings.Builder
	for _, p := range parts {
		switch x := p.(type) {
		case *syntax.Lit:
			b.WriteString(x.Value)
		case *syntax.SglQuoted:
			b.WriteString(x.Value)
		case *syntax.DblQuoted:
			b.WriteString(litText(x.Parts))
		}
	}
	return b.String()
}

// plainProgram is w's value when it is one plain unquoted literal matching
// astProgram, or `:` or `[`.
func plainProgram(w *syntax.Word) (string, bool) {
	if w == nil || len(w.Parts) != 1 {
		return "", false
	}
	l, ok := w.Parts[0].(*syntax.Lit)
	if !ok || !(astProgram.MatchString(l.Value) || l.Value == ":" || l.Value == "[") {
		return "", false
	}
	return l.Value, true
}

// resolves is whether name may resolve: assigned once, by a clean mktemp,
// and with no cd on the line unless mktemp took only -d and -q.
func (f *mktempFacts) resolves(name string) bool {
	if f == nil || f.assigns[name] != 1 {
		return false
	}
	plain, ok := f.mktemp[name]
	return ok && (plain || !f.moves)
}

// mktempValue is whether w is `$(mktemp …)`, quoted or not, and whether
// mktemp took only -d and -q.
func mktempValue(w *syntax.Word) (plain, ok bool) {
	if w == nil || len(w.Parts) == 0 {
		return false, false
	}
	parts := w.Parts
	if dq, isDq := parts[0].(*syntax.DblQuoted); isDq && len(parts) == 1 {
		parts = dq.Parts
	}
	// `""` is a quoted word with no parts: no mktemp (T160).
	if len(parts) == 0 {
		return false, false
	}
	cs, isCs := parts[0].(*syntax.CmdSubst)
	if !isCs || len(cs.Stmts) != 1 {
		return false, false
	}
	if len(parts) != 1 {
		return false, false
	}
	st := cs.Stmts[0]
	call, isCall := st.Cmd.(*syntax.CallExpr)
	if !isCall || len(st.Redirs) > 0 || st.Negated || st.Background || st.Coprocess || len(call.Assigns) > 0 || len(call.Args) == 0 {
		return false, false
	}
	if prog, lit := plainProgram(call.Args[0]); !lit || prog != "mktemp" {
		return false, false
	}
	plain = true
	for _, a := range call.Args[1:] {
		v, lit := wordLit(a)
		if !lit {
			if !tmpdirWord(a) {
				return false, false
			}
			plain = false
			continue
		}
		if !slices.Contains([]string{"-d", "-q", "-dq", "-qd", "--directory", "--quiet"}, v) {
			plain = false
		}
	}
	return plain, true
}

// tmpdirWord is a word whose only expansion is a plain $TMPDIR or ${TMPDIR}
// (a template under it; mktempDir reads the rest).
func tmpdirWord(w *syntax.Word) bool {
	var ok func(parts []syntax.WordPart) bool
	ok = func(parts []syntax.WordPart) bool {
		for _, p := range parts {
			switch x := p.(type) {
			case *syntax.Lit, *syntax.SglQuoted:
			case *syntax.DblQuoted:
				if !ok(x.Parts) {
					return false
				}
			case *syntax.ParamExp:
				if x.Param == nil || x.Param.Value != "TMPDIR" || x.Excl || x.Length || x.Width || x.Index != nil || x.Slice != nil || x.Repl != nil || x.Names != 0 || x.Exp != nil {
					return false
				}
			default:
				return false
			}
		}
		return true
	}
	return ok(w.Parts)
}

// wordLit is w's value when it holds no expansion (quotes resolved).
func wordLit(w *syntax.Word) (string, bool) {
	var b strings.Builder
	var add func(parts []syntax.WordPart) bool
	add = func(parts []syntax.WordPart) bool {
		for _, p := range parts {
			switch x := p.(type) {
			case *syntax.Lit:
				// An unquoted literal keeps its backslashes in Value; drop them.
				b.WriteString(unescape(x.Value))
			case *syntax.SglQuoted:
				if x.Dollar {
					return false
				}
				b.WriteString(x.Value)
			case *syntax.DblQuoted:
				if !add(x.Parts) {
					return false
				}
			default:
				return false
			}
		}
		return true
	}
	if !add(w.Parts) {
		return "", false
	}
	return b.String(), true
}

// unescape is a literal with each backslash escape read.
func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
