// Package sh reads a shell command line the way the guards judge it:
// mvdan.cc/sh parses it, and this package reduces
// the syntax tree to simple commands, each with its words, redirections,
// raw text, and the command whose output its pipe reads. A substitution's
// commands come before the command that holds it.
package sh

import (
	"strings"
	"sync/atomic"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"mvdan.cc/sh/v3/syntax"
)

// Unreadable is a command line the reader cannot read.
type Unreadable struct{ Why string }

func (e *Unreadable) Error() string { return e.Why }

// A Redirect is one redirection: its operator, the descriptor written before
// it ("" when none: `2>` is N "2" and Op ">"), its target, and for a heredoc,
// its body and whether its delimiter was quoted.
type Redirect struct {
	Op, N, Target string
	Body          *string
	Quoted        bool
}

// A Command is one simple command.
type Command struct {
	Words     []string
	Redirects []*Redirect
	Raw       string
	// PipeFrom is the command whose output this one's pipe reads;
	// PipeOpaque, a pipe from a group or a subshell, which is not modelled.
	PipeFrom   *Command
	PipeOpaque bool
	// Loops are the `for` loops around it whose words are all literal: each
	// loop variable takes exactly these values, so a guard can judge the
	// command once per value.
	Loops []Loop
	// Scope is the subshells around it, outermost first: each `( … )`, `$(…)`,
	// or `<(…)` it runs in, by a number no other subshell read has. A cd in
	// one moves its own directory, never the enclosing one's. A funsub
	// (`${ …; }`) runs in the current shell and opens none.
	Scope []int64
	// Forward marks a command an ssh transport value runs on this machine
	// (ProxyCommand, LocalCommand, KnownHostsCommand) that is one plain
	// forward to its host (ForwardCommand, T228 L12): the connection the git
	// command already makes.
	Forward bool
}

// subshells numbers every subshell read, so that no two share a Scope entry.
var subshells atomic.Int64

// A Loop is a `for` loop's variable and the literal words it iterates.
type Loop struct {
	Var    string
	Values []string
}

type reader struct {
	src  string
	cmds []*Command
	// pipe is the feed the next simple command reads: a command, or opaque.
	pipe       *Command
	pipeOpaque bool
	piped      bool
	// prefix is words the next simple command starts with (`time`).
	prefix []string
	// loops are the literal `for` loops the reader is inside.
	loops []Loop
	// scope is the subshells the reader is inside.
	scope []int64
	err   error
}

// Parse reads src into simple commands, including those of every substitution.
func Parse(src string) ([]*Command, error) {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(src), "")
	if err != nil {
		return nil, &Unreadable{Why: err.Error()}
	}
	r := &reader{src: src}
	r.stmts(f.Stmts)
	if r.err != nil {
		return nil, r.err
	}
	return expandAliases(r.cmds), nil
}

// DefinesFunction is whether src defines a shell function anywhere, which
// then runs in place of the program it is named after; a line the parser
// cannot read counts as one that does.
func DefinesFunction(src string) bool {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(src), "")
	if err != nil {
		return true
	}
	found := false
	syntax.Walk(f, func(n syntax.Node) bool {
		if _, ok := n.(*syntax.FuncDecl); ok {
			found = true
		}
		return !found
	})
	return found
}

func (r *reader) text(n syntax.Node) string {
	return r.src[n.Pos().Offset():n.End().Offset()]
}

func (r *reader) stmts(ss []*syntax.Stmt) {
	for _, s := range ss {
		r.stmt(s)
	}
}

// nested reads the statements of a substitution, which feed no outer pipe.
func (r *reader) nested(ss []*syntax.Stmt) {
	r.substitution(ss, true)
}

// substitution reads the statements of a substitution, in a subshell of
// their own when sub is set.
func (r *reader) substitution(ss []*syntax.Stmt, sub bool) {
	pipe, opaque, piped, prefix := r.pipe, r.pipeOpaque, r.piped, r.prefix
	r.pipe, r.pipeOpaque, r.piped, r.prefix = nil, false, false, nil
	if sub {
		r.subshell(ss)
	} else {
		r.stmts(ss)
	}
	r.pipe, r.pipeOpaque, r.piped, r.prefix = pipe, opaque, piped, prefix
}

// cmdSubst reads a command substitution. A bash 5.3 funsub (`${ …; }`,
// `${| …; }`) runs in the current shell, so a cd in it moves the outer
// directory; `$(…)` and backquotes run in a subshell.
func (r *reader) cmdSubst(x *syntax.CmdSubst) {
	r.substitution(x.Stmts, !x.TempFile && !x.ReplyVar)
}

// subshell reads statements that run in a subshell of their own.
func (r *reader) subshell(ss []*syntax.Stmt) {
	r.scope = append(r.scope, subshells.Add(1))
	r.stmts(ss)
	r.scope = r.scope[:len(r.scope)-1]
}

// emit adds a simple command, which takes the pending pipe and prefix.
func (r *reader) emit(s *syntax.Stmt, words []string, redirects []*Redirect) *Command {
	if len(r.prefix) > 0 && len(words) > 0 {
		words = append(append([]string{}, r.prefix...), words...)
	}
	r.prefix = nil
	end := s.Pos().Offset()
	if s.Cmd != nil {
		end = max(end, s.Cmd.End().Offset())
	}
	for _, rd := range s.Redirs {
		end = max(end, rd.End().Offset())
	}
	c := &Command{Words: words, Redirects: redirects, Raw: strings.TrimSpace(r.src[s.Pos().Offset():end]), Loops: append([]Loop(nil), r.loops...),
		Scope: append([]int64(nil), r.scope...)}
	if r.piped {
		c.PipeFrom, c.PipeOpaque = r.pipe, r.pipeOpaque
		r.pipe, r.pipeOpaque, r.piped = nil, false, false
	}
	r.cmds = append(r.cmds, c)
	return c
}

// stmt reads one statement; it returns the simple command it is, if it is one.
func (r *reader) stmt(s *syntax.Stmt) *Command {
	switch x := s.Cmd.(type) {
	case *syntax.CallExpr:
		var words []string
		for _, a := range x.Assigns {
			words = append(words, r.assign(a))
		}
		for _, w := range x.Args {
			words = append(words, r.word(w))
		}
		return r.emit(s, words, r.redirects(s.Redirs))
	case *syntax.DeclClause:
		words := []string{x.Variant.Value}
		for _, a := range x.Args {
			words = append(words, r.assign(a))
		}
		return r.emit(s, words, r.redirects(s.Redirs))
	case nil:
		if rs := r.redirects(s.Redirs); len(rs) > 0 {
			return r.emit(s, nil, rs)
		}
		return nil
	case *syntax.BinaryCmd:
		if x.Op == syntax.Pipe || x.Op == syntax.PipeAll {
			from := r.stmt(x.X)
			r.piped, r.pipe, r.pipeOpaque = true, from, from == nil
			last := r.stmt(x.Y)
			r.piped = false
			return last
		}
		r.stmt(x.X)
		r.stmt(x.Y)
	case *syntax.Block:
		r.stmts(x.Stmts)
	case *syntax.Subshell:
		r.subshell(x.Stmts)
	case *syntax.IfClause:
		for c := x; c != nil; c = c.Else {
			r.stmts(c.Cond)
			r.stmts(c.Then)
		}
	case *syntax.WhileClause:
		r.stmts(x.Cond)
		r.stmts(x.Do)
	case *syntax.ForClause:
		r.walk(x.Loop)
		if l, ok := literalLoop(x.Loop); ok && !changesDir(x.Do) {
			r.loops = append(r.loops, l)
			r.stmts(x.Do)
			r.loops = r.loops[:len(r.loops)-1]
		} else {
			r.stmts(x.Do)
		}
	case *syntax.CaseClause:
		r.word(x.Word)
		for _, it := range x.Items {
			for _, p := range it.Patterns {
				r.word(p)
			}
			r.stmts(it.Stmts)
		}
	case *syntax.FuncDecl:
		r.stmt(x.Body)
	case *syntax.TimeClause:
		if x.Stmt != nil {
			r.prefix = []string{"time"}
			if x.PosixFormat {
				r.prefix = append(r.prefix, "-p")
			}
			r.stmt(x.Stmt)
			r.prefix = nil
		}
	case *syntax.CoprocClause:
		r.stmt(x.Stmt)
	default:
		// [[ … ]], (( … )), let: no program, but a substitution runs one.
		r.walk(x)
	}
	// A compound command's own redirections: a command with no words.
	if rs := r.redirects(s.Redirs); len(rs) > 0 {
		r.emit(s, nil, rs)
	}
	return nil
}

// walk reads the substitutions anywhere under n.
func (r *reader) walk(n syntax.Node) {
	if n == nil {
		return
	}
	syntax.Walk(n, func(m syntax.Node) bool {
		switch y := m.(type) {
		case *syntax.CmdSubst:
			r.cmdSubst(y)
			return false
		case *syntax.ProcSubst:
			r.nested(y.Stmts)
			return false
		case *syntax.ExtGlob:
			r.extGlob(y)
			return false
		case *syntax.Word:
			// zsh's `$~name` in a word the walk reaches: an array's, a loop's, a default word's (T228 L5).
			r.globSubstParts(y.Parts)
			// A subscript's substitution in a quoted word ([[ -v 'a[$(…)]' ]], T228 L8).
			if v, ok := literal(y); ok {
				r.subscripts(v)
			}
		case *syntax.ParamExp:
			r.paramExp(y)
		}
		return true
	})
}

func (r *reader) assign(a *syntax.Assign) string {
	if a.Naked {
		if a.Value != nil {
			v := r.word(a.Value)
			if n, val, ok := strings.Cut(v, "="); ok {
				// A quoted `name[key]=value` (typeset -g "options[globsubst]=on", T228 L5).
				index := ""
				if i := strings.IndexByte(n, '['); i > 0 && strings.HasSuffix(n, "]") {
					n, index = n[:i], n[i+1:len(n)-1]
				}
				r.assigned(n, index, val, nil)
			}
			return v
		}
		if a.Name != nil {
			return a.Name.Value
		}
	}
	name := ""
	if a.Name != nil {
		name = a.Name.Value
	}
	op := "="
	if a.Append {
		op = "+="
	}
	// A subscript is arithmetic: a substitution in it runs (T228).
	index := ""
	if a.Index != nil {
		r.walk(a.Index)
		index = r.text(a.Index)
	}
	if a.Array != nil {
		r.walk(a.Array)
		r.assigned(name, index, "", a.Array)
		return name + op + r.text(a.Array)
	}
	if a.Value == nil {
		return name + op
	}
	v := r.word(a.Value)
	r.assigned(name, index, v, nil)
	return name + op + v
}

// word is a word as the guards read it: quotes removed, ANSI-C decoded, a
// parameter as written, and a substitution as a marker.
func (r *reader) word(w *syntax.Word) string {
	if w == nil {
		return ""
	}
	v := r.parts(w.Parts, false)
	r.subscripts(v)
	return v
}

// arithSubscript is a name with a subscript that holds a substitution:
// `a[$(…)]`, `a[`…`]`.
var arithSubscript = lazyre.New("[A-Za-z_][A-Za-z0-9_]*\\[([^\\]]*(?:\\$\\(|`)[^\\]]*)\\]")

// subscripts reads the substitutions in a subscript a word's text holds,
// quoted or not (T228 L8): bash evaluates such a subscript as arithmetic
// wherever the text is used as a name or a number (test -v, [ -v, printf
// -v, kill -l, declare -i, $((x)), [[ $x -eq 0 ]]), running them. A
// subscript that does not parse runs nothing the reader can tell.
func (r *reader) subscripts(v string) {
	for _, m := range arithSubscript.FindAllStringSubmatch(v, -1) {
		if !strings.Contains(m[1], "$(…)") && !strings.Contains(m[1], "`…`") {
			r.cmds = append(r.cmds, documentCommands(m[1])...)
		}
	}
}

func (r *reader) parts(ps []syntax.WordPart, dq bool) string {
	var b strings.Builder
	// lits is the run of unquoted literals read last: the parser splits
	// zsh's `$~name` into `$` and `~name` (globSubst).
	lits := ""
	defer func() { r.globSubst(lits) }()
	for _, p := range ps {
		if _, ok := p.(*syntax.Lit); !ok || dq {
			r.globSubst(lits)
			lits = ""
		}
		switch x := p.(type) {
		case *syntax.Lit:
			if !dq {
				lits += x.Value
			}
			b.WriteString(unescape(x.Value, dq))
		case *syntax.SglQuoted:
			if x.Dollar {
				b.WriteString(AnsiC(x.Value))
			} else {
				b.WriteString(x.Value)
			}
		case *syntax.DblQuoted:
			b.WriteString(r.parts(x.Parts, true))
		case *syntax.CmdSubst:
			r.cmdSubst(x)
			if x.Backquotes {
				b.WriteString("`…`")
			} else {
				b.WriteString("$(…)")
			}
		case *syntax.ArithmExp:
			r.walk(x)
			b.WriteString("$((…))")
		case *syntax.ProcSubst:
			r.nested(x.Stmts)
			if x.Op == syntax.CmdIn {
				b.WriteString("<(…)")
			} else {
				b.WriteString(">(…)")
			}
		default:
			// A parameter expansion or an extended glob, as written.
			r.walk(x)
			b.WriteString(r.text(x))
			// A glob whose qualifiers may rename its matches names what the
			// reader cannot tell (T228 L8): a substitution's marker says so.
			if g, ok := x.(*syntax.ExtGlob); ok && globRewrites(g.Pattern.Value) {
				b.WriteString("$(…)")
			}
		}
	}
	return b.String()
}

// unescape drops the backslashes the shell removes: before any character
// unquoted, before $ ` " \ and a newline inside double quotes; a
// backslash-newline is a line continuation.
func unescape(s string, dq bool) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		next := s[i+1]
		switch {
		case next == '\n':
		case !dq || strings.IndexByte("$`\"\\", next) >= 0:
			b.WriteByte(next)
		default:
			b.WriteByte('\\')
			b.WriteByte(next)
		}
		i++
	}
	return b.String()
}

func quotedWord(w *syntax.Word) bool {
	for _, p := range w.Parts {
		switch x := p.(type) {
		case *syntax.SglQuoted, *syntax.DblQuoted:
			return true
		case *syntax.Lit:
			if strings.Contains(x.Value, `\`) {
				return true
			}
		}
	}
	return false
}

func (r *reader) redirects(rs []*syntax.Redirect) []*Redirect {
	var out []*Redirect
	for _, rd := range rs {
		red := &Redirect{Op: rd.Op.String(), Target: r.word(rd.Word)}
		if rd.N != nil {
			red.N = rd.N.Value
		}
		if rd.Word != nil && len(rd.Word.Parts) == 1 {
			if _, ok := rd.Word.Parts[0].(*syntax.ProcSubst); ok {
				red.Target = "(…)" // a process substitution is no file
			}
		}
		if rd.Op == syntax.Hdoc || rd.Op == syntax.DashHdoc {
			red.Quoted = quotedWord(rd.Word)
			body := ""
			if rd.Hdoc != nil {
				var err error
				if body, err = r.heredoc(int(rd.Hdoc.Pos().Offset()), red.Target, rd.Op == syntax.DashHdoc, red.Quoted); err != nil && r.err == nil {
					r.err = err
				}
			}
			red.Body = &body
		}
		out = append(out, red)
	}
	return out
}

// heredoc is a heredoc's body read from its first line to its delimiter.
// An unquoted body that runs a command substitution is unreadable.
func (r *reader) heredoc(from int, delim string, strip, quoted bool) (string, error) {
	var body []string
	for i := from; i < len(r.src); {
		nl := strings.IndexByte(r.src[i:], '\n')
		line := r.src[i:]
		if nl >= 0 {
			line = r.src[i : i+nl]
			i += nl + 1
		} else {
			i = len(r.src)
		}
		text := line
		if strip {
			text = strings.TrimLeft(line, "\t")
		}
		if text == delim {
			out := strings.Join(body, "\n")
			if len(body) > 0 {
				out += "\n"
			}
			return out, nil
		}
		if !quoted && (strings.Contains(line, "$(") || strings.Contains(line, "`")) {
			return "", &Unreadable{Why: "a command substitution in an unquoted heredoc"}
		}
		body = append(body, text)
	}
	return "", &Unreadable{Why: "a heredoc with no closing " + delim + " line"}
}

// changesDir is whether a loop body runs cd, pushd, or popd: its commands
// are judged one at a time, so a directory change between them would be
// judged out of order, and such a loop's variable stays unresolved.
func changesDir(body []*syntax.Stmt) bool {
	found := false
	for _, s := range body {
		syntax.Walk(s, func(n syntax.Node) bool {
			if c, ok := n.(*syntax.CallExpr); ok && len(c.Args) > 0 {
				switch c.Args[0].Lit() {
				case "cd", "pushd", "popd":
					found = true
				}
			}
			return !found
		})
	}
	return found
}

// literalLoop is a `for <var> in <words>` loop whose every word is a literal,
// with no expansion or quoting a guard would have to evaluate.
func literalLoop(l syntax.Loop) (Loop, bool) {
	it, ok := l.(*syntax.WordIter)
	if !ok || it.Name == nil || len(it.Items) == 0 {
		return Loop{}, false
	}
	out := Loop{Var: it.Name.Value}
	for _, w := range it.Items {
		lit := w.Lit()
		if lit == "" {
			return Loop{}, false
		}
		out.Values = append(out.Values, lit)
	}
	return out, true
}
