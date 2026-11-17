package sh

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// AnsiC is the text of an ANSI-C quoted string ($'…'), its escapes decoded.
func AnsiC(raw string) string {
	return ansiEscape.ReplaceAllStringFunc(raw, func(m string) string {
		e := m[1:]
		switch {
		case e[0] == 'x' || e[0] == 'u' || e[0] == 'U':
			n, _ := strconv.ParseUint(e[1:], 16, 32)
			return string(rune(n))
		case e[0] >= '0' && e[0] <= '7':
			n, _ := strconv.ParseUint(e, 8, 32)
			return string(rune(n))
		case e[0] == 'c' && len(e) > 1:
			return string(rune(e[1] & 31))
		}
		if s, ok := ansiSimple[e]; ok {
			return s
		}
		return m
	})
}

var (
	ansiEscape = lazyre.New(`\\(x[0-9a-fA-F]{1,2}|u[0-9a-fA-F]{1,4}|U[0-9a-fA-F]{1,8}|[0-7]{1,3}|c.|.)`)
	ansiSimple = map[string]string{"n": "\n", "t": "\t", "r": "\r", "a": "\x07", "b": "\b", "e": "\x1b", "E": "\x1b",
		"f": "\f", "v": "\v", `\`: `\`, "'": "'", `"`: `"`, "?": "?"}
)

// Base is a program's name: its path's last part.
func Base(p string) string { return p[strings.LastIndex(p, "/")+1:] }

var (
	// Assignment is a NAME=value word.
	Assignment = lazyre.New(`^[A-Za-z_][A-Za-z0-9_]*=`)
	// Shells are the shells whose -c string and standard input are read as commands.
	Shells = set("sh", "bash", "zsh", "dash", "ksh", "fish", "ash", "mksh", "posh", "yash")
	// FindExec are find's actions that run a program.
	FindExec = set("-exec", "-execdir", "-ok", "-okdir")
	// StdinNames are the words an operand may use to name standard input.
	StdinNames  = set("-", "/dev/stdin", "/dev/fd/0", "/proc/self/fd/0")
	xargsValues = set("-a", "-d", "-E", "-I", "-L", "-n", "-P", "-s", "--arg-file", "--delimiter", "--max-args", "--max-lines", "--max-procs", "--max-chars", "--eof")
	// Wrappers run their operand; each option's count is the values it takes.
	Wrappers = map[string]map[string]int{
		"env": {"-u": 1, "--unset": 1, "-C": 1, "--chdir": 1}, "exec": {"-a": 1}, "nohup": {}, "time": {}, "nice": {"-n": 1},
		"timeout": {"-s": 1, "--signal": 1, "-k": 1, "--kill-after": 1}, "stdbuf": {"-i": 1, "-o": 1, "-e": 1},
		"sudo": {"-u": 1, "-g": 1, "-C": 1, "-D": 1, "-h": 1, "-p": 1, "-r": 1, "-t": 1, "-U": 1}, "doas": {"-u": 1, "-C": 1}, "command": {},
	}
)

// XargsUnseen is the route marker of a command xargs runs with operands the reader cannot see.
const XargsUnseen = "xargs <unseen input>"

func set(xs ...string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

type codeOpt struct {
	long                []string
	short, stop, digits string
}

var (
	codeOpts = map[string]codeOpt{
		"python": {nil, "c", "WXQm", ""},
		"node":   {[]string{"--eval", "--print"}, "ep", "rCi", ""},
		"bun":    {[]string{"--eval", "--print"}, "ep", "r", ""},
		"perl":   {nil, "eE", "IMmixdDCF", "0l"},
		"ruby":   {nil, "e", "IrCEFxiK", "0l"},
		"php":    {nil, "r", "cdzf", ""},
	}
	python   = lazyre.New(`^python\d?(\.\d+)?$`)
	optStart = lazyre.New(`^-[A-Za-z0-9]`)
)

func interpreter(name string) string {
	if python.MatchString(name) {
		return "python"
	}
	if name == "nodejs" {
		return "node"
	}
	return name
}

// InlineCode is every piece of inline code an interpreter's words give, or nil.
func InlineCode(name string, w []string) []string {
	if name == "deno" {
		if k := slices.Index(w, "eval"); k > 0 {
			return []string{at(w, k+1)}
		}
		return nil
	}
	o, ok := codeOpts[interpreter(name)]
	if !ok {
		return nil
	}
	var code []string
	for k := 1; k < len(w); k++ {
		a := w[k]
		if a == "--" {
			break
		}
		if strings.HasPrefix(a, "--") {
			n, v, attached := strings.Cut(a, "=")
			if slices.Contains(o.long, n) {
				if !attached {
					k++
					v = at(w, k)
				}
				code = append(code, v)
			}
			continue
		}
		if !optStart.MatchString(a) {
			continue
		}
		for i := 1; i < len(a); i++ {
			ch := a[i]
			if strings.IndexByte(o.short, ch) >= 0 {
				if i+1 < len(a) {
					code = append(code, a[i+1:])
				} else {
					k++
					code = append(code, at(w, k))
				}
				break
			}
			if strings.IndexByte(o.stop, ch) >= 0 {
				break
			}
			if strings.IndexByte(o.digits, ch) >= 0 {
				for i+1 < len(a) && a[i+1] >= '0' && a[i+1] <= '7' {
					i++
				}
			}
		}
	}
	return code
}

var (
	interpValued = map[string]map[string]bool{
		"python": set("-W", "-X", "-Q"),
		"node":   set("-r", "--require", "--import", "--loader", "--experimental-loader", "-C", "--conditions", "--env-file", "--input-type", "--title"),
		"perl":   set(), "ruby": set("-I", "-r", "-E", "-C", "-F"), "php": set("-c", "-d", "-z", "-f"),
	}
	interpModes = lazyre.New(`^(?:-m|-V|--version|-h|--help|-v|--test|--check|-c|--run|--watch|-i|--interactive|-[a-l]*v)$`)
	shellS      = lazyre.New(`^-[A-Za-z]*s`)
	shellOptArg = lazyre.New(`^[-+][oO]$`)
	makeFileAt  = lazyre.New(`^--(?:file|makefile)=`)
)

// StdinRole is how a program reads its own program from standard input:
// "shell", "code", "make", or "".
func StdinRole(name string, w []string) string {
	args := w[1:]
	if Shells[name] {
		k := 0
		for k < len(args) && (args[k][0] == '-' || args[k][0] == '+') && args[k] != "-" && args[k] != "--" {
			if shellS.MatchString(args[k]) {
				return "shell"
			}
			if shellOptArg.MatchString(args[k]) || args[k] == "--rcfile" || args[k] == "--init-file" {
				k++
			}
			k++
		}
		if at(args, k) == "--" {
			k++
		}
		if k >= len(args) || StdinNames[args[k]] {
			return "shell"
		}
		return ""
	}
	// at and batch run the commands on their input later, as a shell would (L479).
	if (name == "at" || name == "batch") && !slices.ContainsFunc(args, func(a string) bool { return a == "-f" || strings.HasPrefix(a, "-f") && len(a) > 2 }) {
		return "shell"
	}
	if name == "source" || name == "." {
		if len(args) > 0 && args[0] != "-" && StdinNames[args[0]] {
			return "shell"
		}
		return ""
	}
	if valued, ok := interpValued[interpreter(name)]; ok {
		k := 0
		for k < len(args) && strings.HasPrefix(args[k], "-") && args[k] != "-" {
			if args[k] == "--" {
				k++
				break
			}
			n, _, _ := strings.Cut(args[k], "=")
			if interpModes.MatchString(n) {
				return ""
			}
			if valued[args[k]] {
				k += 2
			} else {
				k++
			}
		}
		if k >= len(args) || StdinNames[args[k]] {
			return "code"
		}
		return ""
	}
	if name == "deno" || name == "bun" {
		k := slices.Index(args, "run")
		if (k >= 0 && k+1 < len(args) && StdinNames[args[k+1]]) || (name == "bun" && at(args, 0) == "-") {
			return "code"
		}
		return ""
	}
	if name == "make" || name == "gmake" {
		for k, a := range args {
			v, ok := "", false
			switch {
			case a == "-f" || a == "--file" || a == "--makefile":
				v, ok = at(args, k+1), k+1 < len(args)
			case len(a) > 2 && strings.HasPrefix(a, "-f"):
				v, ok = a[2:], true
			case makeFileAt.MatchString(a):
				_, v, _ = strings.Cut(a, "=")
				ok = true
			}
			if ok && StdinNames[v] {
				return "make"
			}
		}
	}
	return ""
}

var (
	makeShell  = lazyre.New(`\$\(shell\s+([^)]*)\)`)
	makeInline = lazyre.New(`^[^\t#=]*[^:=]:[^=;]*;(.*)$`)
	recipeLead = lazyre.New(`^[@+\-\s]+`)
)

// MakeRecipes is the shell commands a makefile runs: its recipe lines and its $(shell …) calls.
func MakeRecipes(text string) string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		for _, m := range makeShell.FindAllStringSubmatch(line, -1) {
			out = append(out, m[1])
		}
		if strings.HasPrefix(line, "\t") {
			out = append(out, recipeLead.ReplaceAllString(line[1:], ""))
		} else if m := makeInline.FindStringSubmatch(line); m != nil {
			out = append(out, m[1])
		}
	}
	for i := range out {
		out[i] = strings.ReplaceAll(out[i], "$$", "$")
	}
	return strings.Join(out, "\n")
}

// A Feed is what a command reads on its standard input: Text when the
// reader sees it, else Opaque, which says what it is.
type Feed struct {
	Text   *string
	Opaque string
}

func text(s string) *Feed { return &Feed{Text: &s} }

// StdinOf is what a simple command reads on its standard input, or nil.
func StdinOf(c *Command) *Feed {
	var feed *Feed
	for _, r := range c.Redirects {
		switch {
		case r.Op == "<<" || r.Op == "<<-":
			if r.Body != nil {
				feed = text(*r.Body)
			} else {
				feed = &Feed{Opaque: "a heredoc"}
			}
		case r.Op == "<<<":
			feed = text(r.Target + "\n")
		case r.Op == "<" || r.Op == "<>":
			if r.Target == "/dev/null" {
				feed = text("")
			} else {
				feed = &Feed{Opaque: "the file " + r.Target}
			}
		case strings.HasPrefix(r.Op, "<&"):
			feed = &Feed{Opaque: "the descriptor " + r.Target}
		}
	}
	if feed != nil {
		return feed
	}
	if c.PipeOpaque {
		return &Feed{Opaque: "the output of a group or a subshell"}
	}
	if c.PipeFrom != nil {
		return outputOf(c.PipeFrom)
	}
	return nil
}

var echoFlags = lazyre.New(`^-[neE]+$`)

// outputOf is the output of a pipe's producer, as StdinOf states it.
func outputOf(p *Command) *Feed {
	prog, args := "", []string(nil)
	if len(p.Words) > 0 {
		prog, args = p.Words[0], p.Words[1:]
	}
	name := Base(prog)
	// $IFS prints, or is read by the shell that runs the text, as a word
	// break either way (L486).
	args = slices.Clone(args)
	for i, a := range args {
		args[i] = ifsRef.ReplaceAllString(a, " ")
	}
	literal := !slices.ContainsFunc(args, func(a string) bool { return strings.Contains(a, "$") || strings.Contains(a, "…") })
	if name == "echo" && literal {
		k, nl, esc := 0, true, false
		for k < len(args) && echoFlags.MatchString(args[k]) {
			if strings.Contains(args[k], "n") {
				nl = false
			}
			if strings.Contains(args[k], "e") {
				esc = true
			}
			k++
		}
		t := strings.Join(args[k:], " ")
		if esc {
			t = AnsiC(t)
		}
		if nl {
			t += "\n"
		}
		return text(t)
	}
	if name == "printf" && literal && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return text(printfText(args[0], args[1:]))
	}
	if name == "cat" && !slices.ContainsFunc(args, func(a string) bool { return a != "-" }) {
		if f := StdinOf(p); f != nil {
			return f
		}
	}
	if name == "" {
		name = "a command"
	}
	return &Feed{Opaque: "the output of " + name}
}

var (
	printfSpec  = lazyre.New(`%(?:%|[-+ #0-9.]*[sbdiqxXoufeEgGc])`)
	printfTakes = lazyre.New(`%[^%]`)
)

// printfText is printf's output for a format and its arguments, as far as a command line needs.
func printfText(format string, rest []string) string {
	f := AnsiC(format)
	var out strings.Builder
	i := 0
	for rounds := 0; ; {
		out.WriteString(printfSpec.ReplaceAllStringFunc(f, func(m string) string {
			if m == "%%" {
				return "%"
			}
			v := at(rest, i)
			i++
			if strings.HasSuffix(m, "b") {
				return AnsiC(v)
			}
			return v
		}))
		rounds++
		if !(i < len(rest) && printfTakes.MatchString(f) && rounds < 64) {
			break
		}
	}
	return out.String()
}

// A Program is one program a simple command runs.
type Program struct {
	Argv []string
	// Via is the route to it: the wrappers, shells, and paths it runs through.
	Via []string
	// Env names the variables set for it; Envv holds their values.
	Env  []string
	Envv map[string]string
	Name string
	// Chdir is the directories env -C or sudo -D moved it to; nil when none did.
	Chdir []string
	// Code is an interpreter's code, inline or read from its input.
	Code *string
	// Script is the text the program came from: a -c string, eval's, or a feed.
	Script *string
	// StdinOpaque says what input the program reads its program from, when the reader cannot see it.
	StdinOpaque string
	Redirects   []*Redirect
	Raw         *string
	// Scope is the subshells it runs in, outermost first (Command.Scope).
	Scope []int64
}

// Opts are what programs reads beyond the words: the standard input and the variables set before.
type Opts struct {
	Feed *Feed
	Envv map[string]string
}

func str(s string) *string { return &s }

func with(ps []Program, f func(p *Program)) []Program {
	for i := range ps {
		f(&ps[i])
	}
	return ps
}

// ifsUnreadable is the StdinOpaque of the note a word built by an IFS
// expansion with an operation adds (L784).
const ifsUnreadable = "a word an ${IFS…} expansion builds (L784)"

// Programs is the programs a simple command's words run, unwrapped.
// A word an IFS expansion with an operation builds adds an unreadable note
// (the program itself, its input opaque) after the programs the words are
// read as: the note never replaces the reading (L838), so the line's
// verdict is the strictest of the two.
func Programs(words, via, env []string, opts Opts) ([]Program, error) {
	ps, self, readable, err := programs(words, via, env, opts)
	if readable || self == nil || slices.ContainsFunc(ps, func(p Program) bool { return p.StdinOpaque == ifsUnreadable }) {
		return ps, err
	}
	note := *self
	note.StdinOpaque = ifsUnreadable
	return append(ps, note), err
}

// programs is Programs' reading; self is the program the words name (nil
// when they name none) and readable is false when a word is built by an IFS
// expansion with an operation.
func programs(words, via, env []string, opts Opts) (ps []Program, self *Program, readable bool, err error) {
	w, readable := splitIFS(words)
	envv := maps.Clone(opts.Envv)
	if envv == nil {
		envv = map[string]string{}
	}
	var assigned []string
	for len(w) > 0 && Assignment.MatchString(w[0]) {
		name, value, _ := strings.Cut(w[0], "=")
		assigned = append(assigned, name)
		envv[name] = value
		w = w[1:]
	}
	env = append(slices.Clone(env), assigned...)
	if len(w) == 0 {
		return nil, nil, readable, nil
	}
	prog := w[0]
	name := Base(prog)
	route := via
	if strings.Contains(prog, "/") {
		route = append(slices.Clone(via), prog)
	}
	self0 := Program{Argv: w, Via: route, Env: env, Envv: maps.Clone(envv), Name: name}
	ps, err = readPrograms(w, name, route, env, envv, opts)
	return ps, &self0, readable, err
}

// readPrograms reads the programs of the words w (IFS split, assignments
// taken): name is the first word's base name and route its route.
func readPrograms(w []string, name string, route, env []string, envv map[string]string, opts Opts) ([]Program, error) {
	self := Program{Argv: w, Via: route, Env: env, Envv: envv, Name: name}
	sub := func(extra string) []string { return append(slices.Clone(route), extra) }
	inner := Opts{Feed: opts.Feed, Envv: envv}
	if name == "command" && (at(w, 1) == "-v" || at(w, 1) == "-V") {
		return []Program{self}, nil
	}
	if values, ok := Wrappers[name]; ok {
		k := 1
		var chdir *string
		for k < len(w) && (strings.HasPrefix(w[k], "-") || (name == "env" && Assignment.MatchString(w[k]))) {
			a := w[k]
			if name == "env" && (a == "-S" || a == "--split-string" || (len(a) > 2 && strings.HasPrefix(a, "-S")) || strings.HasPrefix(a, "--split-string=")) {
				s := ""
				switch {
				case a == "-S" || a == "--split-string":
					s = at(w, k+1)
				case strings.HasPrefix(a, "-S"):
					s = a[2:]
				default:
					s = a[15:]
				}
				return JudgeString(s, sub(name), env, inner)
			}
			if name == "env" && Assignment.MatchString(a) {
				n, v, _ := strings.Cut(a, "=")
				env = append(slices.Clone(env), n)
				envv[n] = v
				k++
				continue
			}
			switch {
			case (name == "env" && (a == "-C" || a == "--chdir")) || (name == "sudo" && (a == "-D" || a == "--chdir")):
				chdir = str(at(w, k+1))
			case (name == "env" || name == "sudo") && strings.HasPrefix(a, "--chdir="):
				chdir = str(a[8:])
			case name == "env" && len(a) > 2 && strings.HasPrefix(a, "-C"):
				chdir = str(a[2:])
			case name == "sudo" && len(a) > 2 && strings.HasPrefix(a, "-D"):
				chdir = str(a[2:])
			}
			k += 1 + values[a]
		}
		if name == "timeout" && k < len(w) {
			k++ // the duration
		}
		var rest []string
		if k < len(w) {
			rest = w[k:]
		}
		ps, err := Programs(rest, sub(name), env, inner)
		if err != nil || chdir == nil {
			return ps, err
		}
		return with(ps, func(p *Program) { p.Chdir = append([]string{*chdir}, p.Chdir...) }), nil
	}
	if name == "eval" {
		return JudgeString(strings.Join(w[1:], " "), sub("eval"), env, inner)
	}
	// busybox runs its applet as the program it names (L477).
	if name == "busybox" && len(w) > 1 && !strings.HasPrefix(w[1], "-") {
		return Programs(w[1:], sub("busybox"), env, inner)
	}
	// at -f reads its commands from a file the reader does not show (L486).
	if (name == "at" || name == "batch") && slices.ContainsFunc(w[1:], func(a string) bool { return a == "-f" || strings.HasPrefix(a, "-f") && len(a) > 2 }) {
		self.StdinOpaque = "a file of commands it does not show (at -f)"
		return []Program{self}, nil
	}
	if ps, ok, err := runners(name, w, self, sub, env, inner); ok {
		return ps, err
	}
	// script runs its -c (--command) string as sh -c does (L477).
	if name == "script" {
		for k := 1; k < len(w); k++ {
			a := w[k]
			switch {
			case a == "--":
				k = len(w)
			case a == "--command" || a == "-c":
				return JudgeString(at(w, k+1), sub("script -c"), env, inner)
			case strings.HasPrefix(a, "--command="):
				return JudgeString(a[len("--command="):], sub("script -c"), env, inner)
			case !strings.HasPrefix(a, "--") && strings.HasPrefix(a, "-") && strings.Contains(a, "c"):
				// A cluster (-qc): c takes the rest of the cluster, else the next word.
				if rest := a[strings.Index(a, "c")+1:]; rest != "" {
					return JudgeString(rest, sub("script -c"), env, inner)
				}
				return JudgeString(at(w, k+1), sub("script -c"), env, inner)
			}
		}
	}
	if Shells[name] {
		for k := 1; k < len(w); k++ {
			if shellC.MatchString(w[k]) {
				// The words after the string are its $0, $1, …: the guard
				// resolves a script it runs or sources by them (L294).
				in := inner
				in.Envv = Positional(envv, w[min(k+2, len(w)):])
				return JudgeString(at(w, k+1), sub(name+" -c"), env, in)
			}
		}
	}
	if name == "xargs" {
		k, replace, unseen := 1, "", false
		hasReplace := false
		// -n and -L run the command once per few operands or lines; -0
		// and -d split the input on NUL or a delimiter, whole items each.
		perArgs, perLines := 0, 0
		delim := ""
		for k < len(w) && strings.HasPrefix(w[k], "-") {
			a := w[k]
			count := func(v string) int {
				n, err := strconv.Atoi(v)
				if err != nil || n < 1 {
					return 1
				}
				return n
			}
			switch {
			case a == "-n" || a == "--max-args":
				perArgs = count(at(w, k+1))
			case strings.HasPrefix(a, "--max-args="):
				perArgs = count(a[len("--max-args="):])
			case len(a) > 2 && strings.HasPrefix(a, "-n"):
				perArgs = count(a[2:])
			case a == "-L" || a == "--max-lines":
				perLines = count(at(w, k+1))
			case strings.HasPrefix(a, "--max-lines="):
				perLines = count(a[len("--max-lines="):])
			case len(a) > 2 && strings.HasPrefix(a, "-L"):
				perLines = count(a[2:])
			}
			// A cluster of bare letters (-r0, -0r) may end in 0 or d (L491).
			if len(a) > 2 && a[1] != '-' && strings.Trim(a[1:], "0rtpx") == "" && strings.Contains(a, "0") {
				delim = "\x00"
			}
			if i := strings.IndexByte(a, 'd'); len(a) > 2 && a[1] != '-' && i > 1 && strings.Trim(a[1:i], "0rtpx") == "" {
				if i == len(a)-1 {
					delim = AnsiC(at(w, k+1))
					k++
				} else {
					delim = AnsiC(a[i+1:])
				}
			}
			switch {
			case a == "-0" || a == "--null":
				delim = "\x00"
			case a == "-d" || a == "--delimiter":
				delim = AnsiC(at(w, k+1))
			case strings.HasPrefix(a, "--delimiter="):
				delim = AnsiC(a[len("--delimiter="):])
			case len(a) > 2 && strings.HasPrefix(a, "-d"):
				delim = AnsiC(a[2:])
			}
			switch {
			case a == "-I":
				replace, hasReplace = at(w, k+1), true
				if k+1 >= len(w) {
					replace = "{}"
				}
			case len(a) > 2 && strings.HasPrefix(a, "-I"):
				replace, hasReplace = a[2:], true
			case a == "-i" || a == "--replace":
				replace, hasReplace = "{}", true
			case len(a) > 2 && strings.HasPrefix(a, "-i"):
				replace, hasReplace = a[2:], true
			case strings.HasPrefix(a, "--replace="):
				replace, hasReplace = a[10:], true
			}
			if a == "-a" || a == "--arg-file" || (len(a) > 2 && strings.HasPrefix(a, "-a")) || strings.HasPrefix(a, "--arg-file=") {
				unseen = true
			}
			if xargsValues[a] {
				k += 2
			} else {
				k++
			}
		}
		if k >= len(w) {
			return []Program{self}, nil
		}
		cmd := w[k:]
		out := []Program{self}
		if !unseen && opts.Feed != nil && opts.Feed.Text != nil {
			// The operands are visible: run the command with them, as xargs would.
			// Every item is read, however many: the command line bounds them,
			// and one left unread is one an agent could hide a command in.
			items := slices.DeleteFunc(xargsSplit.Split(*opts.Feed.Text, -1), func(s string) bool { return s == "" })
			lines := slices.DeleteFunc(strings.Split(*opts.Feed.Text, "\n"), func(s string) bool { return strings.TrimSpace(s) == "" })
			if delim != "" {
				items = slices.DeleteFunc(strings.Split(strings.TrimSuffix(*opts.Feed.Text, "\n"), delim), func(s string) bool { return s == "" })
				lines = items
			}
			var runs [][]string
			switch {
			case hasReplace:
				// -I replaces a whole line.
				for _, it := range lines {
					run := make([]string, len(cmd))
					for i, x := range cmd {
						run[i] = strings.ReplaceAll(x, replace, it)
					}
					runs = append(runs, run)
				}
			case perArgs > 0 || perLines > 0:
				// One run per line, and with -n per n operands too: the
				// reading that runs the most of the input as commands.
				n := max(perLines, 1)
				for i := 0; i < len(lines); i += n {
					runs = append(runs, append(slices.Clone(cmd), lines[i:min(i+n, len(lines))]...))
				}
				if perArgs > 0 {
					for i := 0; i < len(items); i += perArgs {
						runs = append(runs, append(slices.Clone(cmd), items[i:min(i+perArgs, len(items))]...))
					}
				}
			default:
				runs = [][]string{append(slices.Clone(cmd), items...)}
			}
			for _, run := range runs {
				ps, err := Programs(run, sub("xargs"), env, Opts{Envv: envv})
				if err != nil {
					return nil, err
				}
				out = append(out, ps...)
			}
			return out, nil
		}
		// The command receives operands from input this reader cannot see:
		// one that runs them as commands is unreadable (L486).
		if RunsCode(cmd) {
			self.StdinOpaque = "input it does not show (an argument file or a pipe)"
			return []Program{self}, nil
		}
		ps, err := Programs(append(slices.Clone(cmd), "<xargs input>"), sub(XargsUnseen), env, Opts{Envv: envv})
		return append(out, ps...), err
	}
	if name == "find" {
		found := []Program{self}
		for k := 1; k < len(w); k++ {
			if !FindExec[w[k]] {
				continue
			}
			e := k + 1
			for e < len(w) && w[e] != ";" && w[e] != "+" {
				e++
			}
			ps, err := Programs(w[k+1:e], sub("find "+w[k]), env, Opts{Envv: envv})
			if err != nil {
				return nil, err
			}
			found = append(found, ps...)
			k = e
		}
		return found, nil
	}
	if code := InlineCode(name, w); code != nil {
		self.Code = str(strings.Join(code, "\n"))
		return []Program{self}, nil
	}
	if opts.Feed != nil {
		if role := StdinRole(name, w); role != "" {
			feed := opts.Feed
			if feed.Text == nil {
				self.StdinOpaque = feed.Opaque
				return []Program{self}, nil
			}
			if role == "code" {
				self.Code, self.Script = str(*feed.Text), str(*feed.Text)
				return []Program{self}, nil
			}
			t := *feed.Text
			if role == "make" {
				t = MakeRecipes(t)
			}
			ps, err := JudgeString(t, sub(name+" <stdin>"), env, Opts{Envv: envv})
			if err != nil {
				return nil, err
			}
			return append([]Program{self}, with(ps, func(p *Program) {
				if p.Script == nil {
					p.Script = str(*feed.Text)
				}
			})...), nil
		}
	}
	return []Program{self}, nil
}

// Positional is envv with the shell's positional parameters $0, $1, … set
// to args, and none left over from an outer shell.
func Positional(envv map[string]string, args []string) map[string]string {
	out := maps.Clone(envv)
	if out == nil {
		out = map[string]string{}
	}
	for n := range out {
		if positionalName.MatchString(n) {
			delete(out, n)
		}
	}
	for i, a := range args {
		out[strconv.Itoa(i)] = a
	}
	return out
}

var (
	positionalName = lazyre.New(`^\d+$`)
	shellC         = lazyre.New(`^-[a-zA-Z]*c[a-zA-Z]*$`)
	xargsSplit     = lazyre.New(`[\s\x00]+`)
)

// CommandPrograms is the programs of one parsed command, with its own input or the one it inherits.
func CommandPrograms(c *Command, via, env []string, opts Opts) ([]Program, error) {
	if f := StdinOf(c); f != nil {
		opts.Feed = f
	}
	ps, err := Programs(c.Words, via, env, opts)
	redirects := c.Redirects
	if redirects == nil {
		redirects = []*Redirect{} // set: an outer command's redirections never replace it
	}
	return with(ps, func(p *Program) {
		if p.Redirects == nil {
			p.Redirects = redirects
		}
		if p.Raw == nil {
			p.Raw = str(c.Raw)
		}
		if len(c.Scope) > 0 {
			p.Scope = append(slices.Clone(c.Scope), p.Scope...)
		}
	}), err
}

// JudgeString is the programs of a command string a wrapper, a shell, or
// eval runs; each keeps the string as its Script.
func JudgeString(s string, via, env []string, opts Opts) ([]Program, error) {
	cmds, err := Parse(s)
	if err != nil {
		return nil, err
	}
	var out []Program
	for _, c := range cmds {
		ps, err := CommandPrograms(c, via, env, opts)
		if err != nil {
			return nil, err
		}
		out = append(out, with(ps, func(p *Program) {
			if p.Script == nil {
				p.Script = str(s)
			}
		})...)
	}
	return out, nil
}

// Runners that run their operand later or elsewhere (L479): flock's
// command and parallel's jobs. Each is read as the program itself and what it runs; ok is
// false for any other program.
func runners(name string, w []string, self Program, sub func(string) []string, env []string, inner Opts) ([]Program, bool, error) {
	judge := func(s, how string) ([]Program, bool, error) {
		ps, err := JudgeString(s, sub(how), env, inner)
		return append([]Program{self}, ps...), true, err
	}
	run := func(words []string, how string) ([]Program, bool, error) {
		ps, err := Programs(words, sub(how), env, inner)
		return append([]Program{self}, ps...), true, err
	}
	switch name {
	case "flock":
		valued := set("-w", "--wait", "--timeout", "-E", "--conflict-exit-code")
		k, file := 1, false
		for ; k < len(w); k++ {
			a := w[k]
			switch {
			case a == "-c" || a == "--command":
				return judge(at(w, k+1), "flock -c")
			case strings.HasPrefix(a, "--command="):
				return judge(a[len("--command="):], "flock -c")
			case valued[a]:
				k++
			case strings.HasPrefix(a, "-"):
			case !file:
				file = true
			default:
				return run(w[k:], "flock")
			}
		}
	case "parallel":
		return parallelPrograms(w, self, judge, run, inner)
	}
	return nil, false, nil
}

// RunsCode is whether cmd, a command a runner gives its input to, runs that
// input as commands: a shell, a wrapper or a runner, or no command at all.
func RunsCode(cmd []string) bool {
	if len(cmd) == 0 {
		return true
	}
	name := Base(cmd[0])
	_, wrapper := Wrappers[name]
	return Shells[name] || wrapper || codeRunners[name] || slices.Contains(cmd, "{}") && cmd[0] == "{}"
}

// codeRunners run what they are given as commands.
var codeRunners = set("eval", "at", "batch", "parallel", "xargs", "flock", "script", "busybox", "setsid", "watch")

// TypedLines is text typed into a terminal (a pane multiplexer's
// send-text or send-keys) as the lines it runs: each written line end (\n, \r, ^M,
// \012, \015) ends a line (L486).
func TypedLines(text string) string {
	return typedEnds.Replace(text)
}

var typedEnds = strings.NewReplacer(`\n`, "\n", `\r`, "\n", "^M", "\n", "^J", "\n", `\012`, "\n", `\015`, "\n", "\r", "\n")

type judgeFunc func(s, how string) ([]Program, bool, error)
type runFunc func(words []string, how string) ([]Program, bool, error)

// parallelPrograms reads GNU parallel. Its arguments come after ::: (or
// from :::: files, -a, or its input); with no command, or the command {}
// alone, each argument is a command line, as GNU parallel runs it; with
// one, each argument is quoted into it ({} or appended) and the line runs
// through a shell. Arguments the reader cannot see, given to a command that
// runs them as commands, are unreadable (L486).
func parallelPrograms(w []string, self Program, judge judgeFunc, run runFunc, inner Opts) ([]Program, bool, error) {
	valued := set("-j", "--jobs", "-S", "--sshlogin", "-a", "--arg-file", "-d", "--delimiter", "--colsep", "-C", "-I", "--joblog", "--results", "--tmpdir", "-N", "-n", "-L", "--timeout", "--halt", "--env", "--workdir", "--wd")
	k, argfile, pipe := 1, false, false
	for k < len(w) && strings.HasPrefix(w[k], "-") && w[k] != ":::" && w[k] != "::::" {
		if w[k] == "-a" || w[k] == "--arg-file" || strings.HasPrefix(w[k], "--arg-file=") {
			argfile = true
		}
		if w[k] == "--pipe" || w[k] == "--block" || w[k] == "--pipepart" || strings.HasPrefix(w[k], "--block=") {
			pipe = true
		}
		if valued[w[k]] {
			k++
		}
		k++
	}
	end := len(w)
	for i := k; i < len(w); i++ {
		if w[i] == ":::" || w[i] == "::::" {
			end = i
			break
		}
	}
	template := w[min(k, end):end]
	if len(template) == 1 && template[0] == "{}" {
		template = nil
	}
	var args []string
	for i := end; i < len(w); i++ {
		switch w[i] {
		case ":::":
		case "::::":
			argfile = true
			i++
		default:
			args = append(args, w[i])
		}
	}
	hasSep := end < len(w)
	// --pipe feeds the input to the command, as `| <command>` does (L491):
	// a command that runs code runs the input as a script.
	if pipe {
		if !RunsCode(template) {
			return run(template, "parallel --pipe")
		}
		if inner.Feed == nil || inner.Feed.Text == nil {
			self.StdinOpaque = "input it does not show (parallel --pipe)"
			return []Program{self}, true, nil
		}
		return judge(*inner.Feed.Text, "parallel --pipe")
	}
	if !hasSep && !argfile && inner.Feed != nil {
		if inner.Feed.Text == nil {
			argfile = true
		} else {
			args = slices.DeleteFunc(strings.Split(*inner.Feed.Text, "\n"), func(s string) bool { return strings.TrimSpace(s) == "" })
		}
	}
	if argfile {
		if RunsCode(template) {
			self.StdinOpaque = "arguments it does not show (an argument file or a pipe)"
			return []Program{self}, true, nil
		}
		return run(append(slices.Clone(template), "<parallel input>"), "parallel")
	}
	// Every argument is read, however many (L479 review).
	var out []Program
	if len(template) == 0 {
		for _, a := range args {
			ps, _, err := judge(a, "parallel")
			if err != nil {
				return nil, true, err
			}
			out = append(out, ps[1:]...)
		}
		return append([]Program{self}, out...), true, nil
	}
	t := strings.Join(template, " ")
	if len(args) == 0 {
		args = []string{""}
	}
	// A bare shell given each argument runs it as a script path; read the
	// argument as the commands such a script would hold (L491).
	if Shells[Base(template[0])] && !slices.ContainsFunc(template[1:], func(a string) bool { return shellC.MatchString(a) }) {
		for _, a := range args {
			ps, _, err := judge(a, "parallel "+template[0])
			if err != nil {
				return nil, true, err
			}
			out = append(out, ps[1:]...)
		}
	}
	for _, a := range args {
		// parallel quotes an argument it puts into its command, as GNU parallel does.
		q := "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		c := t
		if strings.Contains(c, "{}") {
			c = strings.ReplaceAll(c, "{}", q)
		} else if a != "" {
			c += " " + q
		}
		ps, _, err := judge(c, "parallel")
		if err != nil {
			return nil, true, err
		}
		out = append(out, ps[1:]...)
	}
	return append([]Program{self}, out...), true, nil
}

// ifsRef is $IFS or ${IFS} in a word.
var ifsRef = lazyre.New(`\$\{IFS\}|\$IFS\b`)

// ifsOp is an IFS expansion with an operation or indirection (${IFS%??},
// ${IFS:0:1}, ${IFS/ /x}, ${!IFS}): the reader does not work out what it
// expands to (L784).
var ifsOp = lazyre.New(`\$\{(?:!IFS|IFS[^}A-Za-z0-9_])`)

// splitIFS is words with each $IFS or ${IFS} read as the word break it
// expands to (L486): jhon${IFS}ldd${IFS}off is jhon ldd off. readable is
// false when a word holds an IFS expansion with an operation: the command
// is then unreadable (L784).
// A shell's -c string is not split here: it is a command line of its own,
// read (and split) when it runs.
func splitIFS(words []string) (out []string, readable bool) {
	out = make([]string, 0, len(words))
	readable = true
	shell := len(words) > 0 && Shells[Base(words[0])]
	for i, w := range words {
		if shell && i > 0 && shellC.MatchString(words[i-1]) {
			out = append(out, w)
			continue
		}
		if ifsOp.MatchString(w) {
			readable = false
		}
		if !ifsRef.MatchString(w) {
			out = append(out, w)
			continue
		}
		out = append(out, strings.Fields(ifsRef.ReplaceAllString(w, " "))...)
	}
	return out, readable
}
