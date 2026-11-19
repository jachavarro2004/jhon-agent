package hook

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/guard"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
	"mvdan.cc/sh/v3/syntax"
)

const frameworkRule = "no Jhon agent writes the framework's source, releases, current pointer, CLI link, owned projections, or " +
	"~/.claude/CLAUDE.md, or runs git on them (docket AV2). A framework rule that blocks project work is reported as a " +
	"blocker; changing it is a separate framework docket, outside Jhon agents. Stop and report this refusal verbatim; " +
	"never reach the same effect another way (contracts/runtime-boundaries.md)."

// byHand is what an agent does instead of running Jhon's own code (T81, L596).
const byHand = "Jhon's own code is never run or imported by hand: launch a sub-agent with the subagent tool (pi) or the Agent tool " +
	"(Claude Code), and read the change with inspect. "

var (
	// scriptRunners run a script file whose code the guard never reads: what
	// it runs is known only from the paths it is handed.
	scriptRunners = map[string]bool{"node": true, "deno": true, "bun": true, "tsx": true, "ts-node": true, "perl": true, "ruby": true, "php": true, "Rscript": true, "lua": true}
	pythonName    = lazyre.New(`^python[0-9.]*$`)
	pathTokens    = lazyre.New(`[^\s'"` + "`" + `(),;=<>{}[\]|&]+`)
)

// expand resolves a word's leading ~ against HOME.
func expand(w string) string {
	if w == "~" {
		return project.Home()
	}
	if strings.HasPrefix(w, "~/") {
		return project.Resolve(project.Home(), w[2:])
	}
	return w
}

// FrameworkGuard refuses every Jhon agent's file-tool write of the framework
// (framework-guard, docket AV2) and a Bash line that runs Jhon's own code by
// hand (T81). A Bash write outside the project is the sandbox's (T128f).
func FrameworkGuard(args []string, io IO) int {
	scoped, name, rest := owner(args)
	var roots []string
	for _, r := range rest {
		if r = strings.TrimSpace(r); r != "" {
			roots = append(roots, r)
		}
	}
	var p Payload
	block := func(reason string) int {
		fmt.Fprintf(io.Err, "framework-guard: %s\n%s", reason, staleNote(p))
		return 2
	}
	p, ok := read(io.In)
	if !ok {
		return block("hook payload is not valid JSON")
	}
	if !scoped || name == "" {
		return block("the guard was rendered without --agent <owner>; the render is broken")
	}
	if othersCall(scoped, name, p) {
		return 0
	}
	if len(roots) == 0 {
		return block("no protected paths were given; the render is broken")
	}
	tool := p.Str("tool_name")
	if tool != "Edit" && tool != "MultiEdit" && tool != "Write" && tool != "Bash" {
		return 0
	}
	input := p.Obj("tool_input")
	cwd := p.Cwd()

	// Each protected path as written and through its real path.
	var guarded []string
	for _, r := range roots {
		abs := project.Resolve("", r)
		guarded = append(guarded, abs, project.Realish(abs))
	}
	hit := func(w, dir string) string {
		if w == "" {
			return ""
		}
		abs := project.Resolve(dir, expand(w))
		for _, q := range []string{abs, project.Realish(abs)} {
			if at := slices.IndexFunc(guarded, func(r string) bool { return project.Within(q, r) }); at >= 0 {
				return roots[at/2]
			}
		}
		return ""
	}

	if tool != "Bash" {
		_, paths, _ := writes(p)
		if len(paths) == 0 {
			return block(tool + " call carries no file_path")
		}
		for _, raw := range paths {
			if root := hit(raw, cwd); root != "" {
				return block(fmt.Sprintf("%s of %s is under the protected path %s: %s", tool, raw, root, frameworkRule))
			}
		}
		return 0
	}

	// A Bash line is the sandbox's (T128f): outside the project it writes
	// nothing. What stays here is the T81 rule, which no sandbox enforces:
	// Jhon's own code is never run or imported by hand.
	command, _ := input["command"].(string)
	if strings.TrimSpace(command) == "" {
		return block("Bash call carries no command")
	}
	cmds, err := sh.Parse(command)
	if err != nil {
		return 0
	}
	// A function substitution's output may be the program the line runs
	// (`${ echo python3 /tmp/t.py; }`), which no reader below sees: a line
	// holding one and naming a protected path is refused (L834).
	if funsub.MatchString(command) {
		for _, t := range pathTokens.FindAllString(command, -1) {
			if named := hit(t, cwd); named != "" {
				return block(fmt.Sprintf("cannot read this command safely (a function substitution ${ …; }), and it names the protected path %s: %s", named, frameworkRule))
			}
		}
	}

	// A protected path anywhere in the command's text, a heredoc body
	// included: a script the command writes and then runs may carry it.
	// A path only a plain reader names, its output shown and fed to no
	// program or file, reaches no script (T109 WU3, L784 b).
	named := ""
	for _, t := range pathTokens.FindAllString(withoutShownReads(command), -1) {
		if named = hit(t, cwd); named != "" {
			break
		}
	}
	// Walk the programs in order, following each cd.
	dir := cwd
	judge := func(p sh.Program, c *sh.Command) string {
		raw := c.Raw
		if p.Raw != nil {
			raw = *p.Raw
		}
		prog, args := "", []string(nil)
		if len(p.Argv) > 0 {
			prog, args = p.Argv[0], p.Argv[1:]
		}
		// Interpreter code naming a protected path runs or imports Jhon's own
		// code by hand (T81, L596: sub-agents launched from inline node code
		// importing shell/lib/agents.ts, outside every Jhon tool).
		interpreter := scriptRunners[p.Name] || pythonName.MatchString(p.Name)
		if p.Code != nil && interpreter {
			for _, t := range sh.PathTokens.FindAllString(*p.Code, -1) {
				if root := hit(t, dir); root != "" {
					return fmt.Sprintf("`%s` runs %s code naming the protected path %s. %s%s", raw, p.Name, root, byHand, frameworkRule)
				}
			}
		}
		// A script run from a file is code the guard cannot read: handed a
		// protected path, run inside a protected directory, or written in the
		// same command with a protected path in its text, it may run or import
		// Jhon's own code.
		if p.Code == nil && len(args) > 0 && (interpreter || sh.Shells[p.Name]) {
			root := hit(dir, dir)
			for _, a := range args {
				if root == "" && !strings.HasPrefix(a, "-") {
					root = hit(a, dir)
				}
			}
			if root == "" {
				root = named
			}
			if root != "" {
				return fmt.Sprintf("`%s` runs a script, whose code this guard cannot read, on the protected path %s. %s%s", raw, root, byHand, frameworkRule)
			}
		}
		if (p.Name == "cd" || p.Name == "pushd") && prog != "" {
			to := "~"
			if i := slices.IndexFunc(args, func(a string) bool { return !strings.HasPrefix(a, "-") }); i >= 0 {
				to = args[i]
			}
			dir = project.Resolve(dir, expand(to))
		}
		return ""
	}
	for _, c := range cmds {
		ps, err := sh.CommandPrograms(c, nil, nil, sh.Opts{})
		if err != nil {
			continue
		}
		for _, prog := range ps {
			// env -C: this program runs there; the command's own directory does not move.
			outer := dir
			for _, d := range prog.Chdir {
				dir = project.Resolve(dir, expand(d))
			}
			if why := judge(prog, c); why != "" {
				return block(why)
			}
			if len(prog.Chdir) > 0 {
				dir = outer
			}
		}
	}
	return 0
}

// shownReaders are programs that only read the files they name and print them.
var shownReaders = map[string]bool{"cat": true, "head": true, "tail": true, "wc": true, "ls": true, "stat": true, "nl": true}

var (
	// replays is text that can hand a later program an earlier command's
	// words: $_ and ${_…}, $BASH_COMMAND and its kin, fc, history, and a
	// history expansion (! before anything but a blank).
	replays = lazyre.New(`\$_|\$\{[!#]?_|BASH_|HIST|\bfc\b|\bhistory\b|![^\s]|\^`)
	// plainWord is a reader word that means in the source text exactly what
	// it reads as: no quote, backslash, expansion, brace or control operator.
	plainWord = lazyre.New(`^[A-Za-z0-9_./~*?@%+,:=\[\]-]+$`)
	// funsub is the opening of a bash function substitution, ${ …; } or
	// ${| …; }: its body runs in the current shell, and its output may be
	// the program the line runs, which the reader does not see (L834).
	funsub = lazyre.New(`\$\{[ \t\n|]`)
)

// withoutShownReads is command with the bytes of each shown read blanked: a
// top-level plain reader (shownReaders) of plain words whose output goes
// only to the screen. A reader is known by its node in the parsed tree and
// blanked by that node's byte range in command, never by its text (L824):
// the same text anywhere else stays visible. It is a gate, not a denylist
// (L808): nothing is blanked unless the whole line passes the WU1 allowlist
// (guard.LineIsPlain: no function, no exec/alias/shopt/source/eval, no
// PATH=) and cannot replay an earlier command's words (replays). Nor is
// anything blanked in text bash and the parser may read differently (L830):
// a carriage return, a backslash-newline (a line continuation, `$\`+newline
// +`_` is $_ to bash), or any heredoc (bash decides where its body ends: a
// CR after the delimiter or a continued body line keeps it open, and the
// "reader" after it is really the body).
func withoutShownReads(command string) string {
	if strings.Contains(command, "\r") || strings.Contains(command, "\\\n") || replays.MatchString(command) || !guard.LineIsPlain(command) {
		return command
	}
	out := []byte(command)
	for _, s := range shownReads(command) {
		from, to := int(s.Pos().Offset()), int(s.End().Offset())
		// An offset that does not map onto command exempts nothing.
		if !s.Pos().IsValid() || !s.End().IsValid() || from < 0 || to <= from || to > len(command) {
			return command
		}
		for i := from; i < to; i++ {
			out[i] = ' '
		}
	}
	return string(out)
}

// shownReads is each statement of command that is a shown read: a simple
// command that is a top-level statement — joined only by &&, ||, ;, newline
// or a leading !, never inside a group, subshell, coproc, loop, if/case
// body, pipeline, substitution or background job, with no redirect on any
// enclosing node (L808) — whose program is a shownReaders name, whose words
// are all plain in the source text, and whose own redirects keep its
// output on the screen. A command holding any heredoc (<< or <<-) has none
// (L830).
func shownReads(command string) []*syntax.Stmt {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
	if err != nil {
		return nil
	}
	heredoc := false
	syntax.Walk(f, func(n syntax.Node) bool {
		if r, ok := n.(*syntax.Redirect); ok && (r.Op == syntax.Hdoc || r.Op == syntax.DashHdoc || r.Hdoc != nil) {
			heredoc = true
		}
		return !heredoc
	})
	if heredoc {
		return nil
	}
	text := func(n syntax.Node) string {
		from, to := int(n.Pos().Offset()), int(n.End().Offset())
		if !n.Pos().IsValid() || !n.End().IsValid() || from < 0 || to < from || to > len(command) {
			return ""
		}
		return command[from:to]
	}
	// plain is w's text when w is one literal the source spells as it reads.
	plain := func(w *syntax.Word) (string, bool) {
		if w == nil || len(w.Parts) != 1 {
			return "", false
		}
		l, ok := w.Parts[0].(*syntax.Lit)
		if !ok || l.Value != text(w) || !plainWord.MatchString(l.Value) {
			return "", false
		}
		return l.Value, true
	}
	var reads []*syntax.Stmt
	var visit func(s *syntax.Stmt)
	visit = func(s *syntax.Stmt) {
		if s == nil || s.Background || s.Coprocess || s.Disown {
			return
		}
		switch x := s.Cmd.(type) {
		case *syntax.BinaryCmd:
			if len(s.Redirs) > 0 || x.Op != syntax.AndStmt && x.Op != syntax.OrStmt {
				return
			}
			visit(x.X)
			visit(x.Y)
		case *syntax.CallExpr:
			if len(x.Assigns) > 0 || len(x.Args) == 0 {
				return
			}
			for i, a := range x.Args {
				if v, ok := plain(a); !ok || i == 0 && !shownReaders[v] {
					return
				}
			}
			for _, r := range s.Redirs {
				if !shownRedirect(r, plain) {
					return
				}
			}
			reads = append(reads, s)
		}
	}
	for _, s := range f.Stmts {
		visit(s)
	}
	return reads
}

// shownRedirect is whether r leaves a reader's output on the screen: a
// plain file read on stdin, stdout or stderr sent to /dev/null, or a file
// descriptor duplication naming only 1, 2 or - (L808).
func shownRedirect(r *syntax.Redirect, plain func(*syntax.Word) (string, bool)) bool {
	std := func(v string) bool { return v == "1" || v == "2" }
	if r.Hdoc != nil {
		return false
	}
	target, ok := plain(r.Word)
	if !ok {
		return false
	}
	switch r.Op {
	case syntax.RdrIn:
		return r.N == nil || r.N.Value == "0"
	case syntax.RdrOut, syntax.AppOut, syntax.RdrAll, syntax.AppAll:
		return (r.N == nil || std(r.N.Value)) && target == "/dev/null"
	case syntax.DplOut, syntax.DplIn:
		return (r.N == nil || std(r.N.Value)) && (std(target) || target == "-")
	}
	return false
}
