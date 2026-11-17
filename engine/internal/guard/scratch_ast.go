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
//     of its name, its value `$(mktemp …)` (mktempValue);
//   - no function, coproc, select, time, [[ ]], (( )), let, declaration,
//     C-style for, arithmetic assignment, ${x=}/${x:=}, or {var}> redirect;
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
// a program word runs (L830). Never assignable, tracked or not.
var astZsh = set("path", "cdpath", "fpath", "manpath", "module_path", "mailpath", "psvar")

// astBuiltins are bash 5.2's builtins (compgen -b) and keywords (compgen -k).
var astBuiltins = set(".", ":", "[", "alias", "bg", "bind", "break", "builtin", "caller", "cd", "command", "compgen", "complete",
	"compopt", "continue", "declare", "dirs", "disown", "echo", "enable", "eval", "exec", "exit", "export", "false", "fc", "fg",
	"getopts", "hash", "help", "history", "jobs", "kill", "let", "local", "logout", "mapfile", "popd", "printf", "pushd", "pwd",
	"read", "readarray", "readonly", "return", "set", "shift", "shopt", "source", "suspend", "test", "times", "trap", "true",
	"type", "typeset", "ulimit", "umask", "unalias", "unset", "wait",
	"if", "then", "else", "elif", "fi", "case", "esac", "for", "select", "while", "until", "do", "done", "in", "function", "time",
	"{", "}", "!", "[[", "]]", "coproc")

// astSafe are the builtins that change no shell state. printf is not one
// (printf -v assigns).
var astSafe = set("echo", "true", "false", ":", "test", "[", "pwd")

// astName is a shell variable name.
var astName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// astTrackable is a name a tracked mktemp assignment may set: not one of
// bash's special variables.
func astTrackable(n string) bool {
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
					bad = true
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
