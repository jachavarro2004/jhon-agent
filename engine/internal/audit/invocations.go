package audit

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"slices"
	"strings"
	"sync"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// The audit's own reading of a command line: the programs it runs and the route each is reached by. It is
// not the guards' reader (internal/shell): the checks read what a refused
// call named, read loosely, not as the guard judged it.

// Invocation is one program run by a command line, and the route it was
// reached by; Args are its tokens after the program; Code is an
// interpreter's inline code (nil for none).
type Invocation struct {
	Program, Sub, Via, Raw string
	Args                   []string
	Code                   *string
}

var shells = []string{"sh", "bash", "zsh", "dash", "ksh", "fish"}

var interpreters = map[string][]string{
	"python": {"-c"}, "python2": {"-c"}, "python3": {"-c"}, "node": {"-e", "--eval", "-p", "--print"}, "deno": {"eval"},
	"bun": {"-e", "--eval"}, "perl": {"-e", "-E"}, "ruby": {"-e"}, "php": {"-r"},
}

// wrappers run their operand; the value is how many values each option takes.
var wrappers = map[string]map[string]int{
	"env": {"-u": 1, "-C": 1, "-S": 0}, "exec": {"-a": 1}, "nohup": {}, "time": {}, "nice": {"-n": 1},
	"timeout": {"-s": 1, "-k": 1}, "stdbuf": {}, "sudo": {"-u": 1, "-g": 1}, "doas": {"-u": 1}, "rtk": {},
}

// valueOptions are the options that take a separate value, per program, when finding a subcommand.
var valueOptions = map[string][]string{"git": {"-C", "-c", "--git-dir", "--work-tree", "--namespace", "--exec-path"}}

// neutral builtins: a refusal of one names nothing worth guarding.
var neutral = []string{"cd", "echo", "printf", "true", "false", "test", "[", "pwd", "export", "set", "unset", "sleep", "exit", "return", ":",
	// Shell keywords, which a loop or a branch leaves where a program stands.
	"for", "while", "until", "do", "done", "if", "then", "else", "elif", "fi", "case", "esac", "in", "select", "function", "begin", "end", "{", "}"}

type simple struct {
	tokens []string
	raw    string
}

var (
	redirectAlone = lazyre.New(`^\d*(>>?|<<?|&>)$`)
	redirectWord  = lazyre.New(`^\d*(>>?|<<?|&>)`)
	assignment    = lazyre.New(`^[A-Za-z_]\w*=`)
	shellC        = lazyre.New(`^-[a-z]*c[a-z]*$`)
)

// simpleCommands splits a command line into simple commands: tokens plus the raw text.
func simpleCommands(cmd string) []simple {
	var out []simple
	var tokens []string
	var tok strings.Builder
	has, start, i, depth := false, 0, 0, 0
	endTok := func() {
		if has {
			tokens = append(tokens, tok.String())
		}
		tok.Reset()
		has = false
	}
	endCmd := func(to int) {
		endTok()
		if len(tokens) > 0 {
			out = append(out, simple{tokens, js.Trim(cmd[start:to])})
		}
		tokens = nil
	}
	ch := func(k int) byte {
		if k < 0 || k >= len(cmd) {
			return 0
		}
		return cmd[k]
	}
	for i < len(cmd) {
		c := cmd[i]
		switch {
		case c == '\'':
			e := strings.IndexByte(cmd[i+1:], '\'')
			if e < 0 {
				e = len(cmd)
			} else {
				e += i + 1
			}
			tok.WriteString(cmd[i+1 : e])
			has = true
			i = e + 1
			continue
		case c == '"':
			j := i + 1
			for j < len(cmd) && cmd[j] != '"' {
				if cmd[j] == '\\' && j+1 < len(cmd) {
					tok.WriteByte(cmd[j+1])
					j += 2
					continue
				}
				tok.WriteByte(cmd[j])
				j++
			}
			has = true
			i = j + 1
			continue
		case c == '\\' && i+1 < len(cmd):
			if cmd[i+1] != '\n' {
				tok.WriteByte(cmd[i+1])
				has = true
			}
			i += 2
			continue
		case c == '$' && ch(i+1) == '(':
			depth++
			tok.WriteString("$(")
			has = true
			i += 2
			continue
		case depth > 0:
			if c == ')' {
				depth--
			}
			tok.WriteByte(c)
			has = true
			i++
			continue
		case c == '#' && !has:
			for i < len(cmd) && cmd[i] != '\n' {
				i++
			}
			continue
		case c == ' ' || c == '\t':
			endTok()
			i++
			continue
		case c == '&' && (ch(i-1) == '>' || ch(i-1) == '<' || ch(i+1) == '>'):
			tok.WriteByte(c)
			has = true
			i++
			continue
		case c == '\n' || c == ';' || c == '|' || c == '&' || c == '(' || c == ')':
			endCmd(i)
			if (c == '&' && ch(i+1) == '&') || (c == '|' && ch(i+1) == '|') {
				i += 2
			} else {
				i++
			}
			start = i
			continue
		}
		tok.WriteByte(c)
		has = true
		i++
	}
	endCmd(len(cmd))
	// Drop redirections: `2>&1`, `>file`, and `>` followed by its target.
	var kept []simple
	for _, s := range out {
		var t []string
		for k := 0; k < len(s.tokens); k++ {
			if redirectAlone.MatchString(s.tokens[k]) {
				k++
				continue
			}
			if redirectWord.MatchString(s.tokens[k]) {
				continue
			}
			if x := s.tokens[k]; x != "{" && x != "}" && x != "!" {
				t = append(t, x)
			}
		}
		if len(t) > 0 {
			kept = append(kept, simple{t, s.raw})
		}
	}
	return kept
}

func firstOperand(args []string, program string) string {
	withValue := valueOptions[program]
	for k := 0; k < len(args); k++ {
		if slices.Contains(withValue, args[k]) {
			k++
			continue
		}
		if strings.HasPrefix(args[k], "-") {
			continue
		}
		return args[k]
	}
	return ""
}

// Invocations is the programs a command line runs, unwrapping wrappers and `sh -c`.
func Invocations(cmd string) []Invocation { return invocations(cmd, nil) }

func invocations(cmd string, via []string) []Invocation {
	var out []Invocation
	for _, sc := range simpleCommands(cmd) {
		t := sc.tokens
		v := slices.Clone(via)
		assigned := false
		for len(t) > 0 && assignment.MatchString(t[0]) {
			t = t[1:]
			assigned = true
		}
		if assigned {
			v = append(v, "env")
		}
		for len(t) > 0 {
			base := baseName(t[0])
			wrapper, isWrapper := wrappers[base]
			if base == "command" && len(t) > 1 && (t[1] == "-v" || t[1] == "-V") {
				break
			}
			if !isWrapper && base != "command" {
				break
			}
			if strings.Contains(t[0], "/") {
				v = append(v, t[0])
			} else {
				v = append(v, base)
			}
			k := 1
			for k < len(t) && (strings.HasPrefix(t[k], "-") || (base == "env" && assignment.MatchString(t[k]))) {
				k += 1 + wrapper[t[k]]
			}
			if base == "timeout" && k < len(t) {
				k++ // the duration
			}
			t = t[min(k, len(t)):]
		}
		if len(t) == 0 {
			continue
		}
		prog := t[0]
		base := baseName(prog)
		// A program named by path is its own route: `/usr/bin/git` is not `git`.
		route := slices.Clone(v)
		if strings.Contains(prog, "/") {
			route = append(route, prog)
		}
		routeText := strings.Join(route, " > ")
		if routeText == "" {
			routeText = "plain"
		}
		if slices.Contains(shells, base) {
			c := indexAfter(t, 1, shellC.MatchString)
			if c > 0 && c+1 < len(t) {
				out = append(out, invocations(t[c+1], slices.Concat(route, []string{base + " -c"}))...)
				continue
			}
		}
		if flags, ok := interpreters[base]; ok {
			c := indexAfter(t, 1, func(x string) bool { return slices.Contains(flags, x) })
			if c > 0 && c+1 < len(t) {
				code := t[c+1]
				out = append(out, Invocation{Program: base, Via: routeText, Raw: sc.raw, Args: t[1:], Code: &code})
				continue
			}
		}
		out = append(out, Invocation{Program: base, Sub: firstOperand(t[1:], base), Via: routeText, Raw: sc.raw, Args: t[1:]})
	}
	return out
}

// indexAfter is the first index k >= from whose element f accepts, or -1.
func indexAfter(xs []string, from int, f func(string) bool) int {
	for k := from; k < len(xs); k++ {
		if f(xs[k]) {
			return k
		}
	}
	return -1
}

// unquotedStrings are the quoted strings a command holds: what unquoted empties.
var unquotedStrings = lazyre.New(`'[^']*'|"(?:[^"\\]|\\` + js.Dot + `)*"`)

// unquoted is a command with every quoted string emptied: what the shell itself reads as syntax.
func unquoted(cmd string) string { return unquotedStrings.ReplaceAllLiteralString(cmd, "''") }

var (
	heredocOpen   = lazyre.New(`<<-?` + space + `*(\w+)`)
	heredocQuoted = lazyre.New(`<<-?` + space + `*['"](\w+)['"]`)
)

// WithoutHeredocs is a command line without the bodies of its heredocs: their text is data, not commands.
// A line with no `<<` opens none: unquoted only puts two single quotes
// where a quoted string was, so it never makes a `<<` the line lacks, and
// the line is not run through the regexes.
func WithoutHeredocs(command string) string {
	var out []string
	end, open := "", false
	for _, l := range strings.Split(command, "\n") {
		if open {
			if js.Trim(l) == end {
				open = false
			}
			continue
		}
		out = append(out, l)
		if !strings.Contains(l, "<<") {
			continue
		}
		m := heredocOpen.FindStringSubmatch(unquoted(l))
		if m == nil {
			m = heredocQuoted.FindStringSubmatch(l)
		}
		if m != nil {
			end, open = m[1], true
		}
	}
	return strings.Join(out, "\n")
}

// codeless is the invocations that run a program, not an interpreter's inline code.
func codeless(xs []Invocation) []Invocation {
	var out []Invocation
	for _, x := range xs {
		if x.Code == nil {
			out = append(out, x)
		}
	}
	return out
}

// Parsed command lines, by text: the checks and measurements read the same
// command many times, so each is parsed once (and first during the
// concurrent load, which warms these).
var plainParsed, rawParsed sync.Map

func memo(cache *sync.Map, cmd string, parse func(string) []Invocation) []Invocation {
	if v, ok := cache.Load(cmd); ok {
		return v.([]Invocation)
	}
	out := parse(cmd)
	cache.Store(cmd, out)
	return out
}

// plain is Invocations(WithoutHeredocs(cmd)), parsed once. Callers do not modify it.
func plain(cmd string) []Invocation {
	return memo(&plainParsed, cmd, func(c string) []Invocation { return Invocations(WithoutHeredocs(c)) })
}

// raw is Invocations(cmd), heredoc bodies included, parsed once.
func raw(cmd string) []Invocation { return memo(&rawParsed, cmd, Invocations) }
