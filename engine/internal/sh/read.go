package sh

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"maps"
	"path"
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
		// T228 L5: the long options with a separate value, env's -a, GNU time's -o and -f.
		"env": {"-u": 1, "--unset": 1, "-C": 1, "--chdir": 1, "-a": 1, "--argv0": 1, "-S": 1, "--split-string": 1}, "exec": {"-a": 1},
		"nohup": {}, "time": {"-o": 1, "-f": 1, "--output": 1, "--format": 1}, "nice": {"-n": 1, "--adjustment": 1},
		"timeout": {"-s": 1, "--signal": 1, "-k": 1, "--kill-after": 1}, "stdbuf": {"-i": 1, "-o": 1, "-e": 1, "--input": 1, "--output": 1, "--error": 1},
		"sudo": {"-u": 1, "-g": 1, "-C": 1, "-D": 1, "-h": 1, "-p": 1, "-r": 1, "-t": 1, "-U": 1, "-R": 1, "-T": 1, "--user": 1, "--group": 1,
			"--chdir": 1, "--prompt": 1, "--role": 1, "--type": 1, "--host": 1, "--other-user": 1, "--chroot": 1, "--command-timeout": 1},
		"doas": {"-u": 1, "-C": 1}, "command": {},
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
	// crontab - (or with no file) installs the table on its input, whose
	// commands cron runs later (T228 L5).
	if name == "crontab" {
		for i := 0; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "-u":
				i++
			case a == "-l" || a == "-r" || a == "-e" || a != "-" && !strings.HasPrefix(a, "-"):
				return ""
			}
		}
		return "cron"
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

// CronCommands is the shell commands a crontab runs: each entry's command,
// after its five time fields or its @keyword, up to an unescaped %, whose
// rest is the command's input (T228 L5). Comments and assignments run none.
func CronCommands(text string) string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") || Assignment.MatchString(f[0]) {
			continue
		}
		n := 5
		if strings.HasPrefix(f[0], "@") {
			n = 1
		}
		if len(f) <= n {
			continue
		}
		cmd := strings.Join(f[n:], " ")
		for i := 0; i < len(cmd); i++ {
			if cmd[i] == '\\' {
				i++
			} else if cmd[i] == '%' {
				cmd = cmd[:i]
				break
			}
		}
		out = append(out, cmd)
	}
	return strings.Join(out, "\n")
}

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
//
// For a feed the reader does not see (T257): File is the file it is when
// it is one on disk (`< f`, `cat f |`), which a guard reads there; Lost,
// that it holds text the line gives through a path the reader does not
// follow (a descriptor, a group, a program that passes its input or a
// file on, `echo "$x"`); Gen, that it is a program's own output for a
// shell's setup (its completion, init, hook or environment: `kubectl
// completion bash`, `direnv hook bash`, `ssh-agent -s`).
type Feed struct {
	Text   *string
	Opaque string
	File   string
	Lost   bool
	Gen    bool
}

func text(s string) *Feed { return &Feed{Text: &s} }

// StdinOf is what a simple command reads on its standard input, or nil.
func StdinOf(c *Command) *Feed {
	var feed *Feed
	for _, r := range c.Redirects {
		// `3< f`, `4<<EOF`: another descriptor, not the input (T257).
		if r.N != "" && r.N != "0" {
			continue
		}
		switch {
		case r.Op == "<<" || r.Op == "<<-":
			if r.Body != nil {
				feed = text(*r.Body)
			} else {
				feed = &Feed{Opaque: "a heredoc"}
			}
		case r.Op == "<<<":
			feed = text(r.Target + "\n")
		case r.Op == "<" && r.Target == "(…)":
			// `< <(…)`: the substitution's output (T257).
			feed = procFeed(r.From)
		case r.Op == "<" || r.Op == "<>":
			if r.Target == "/dev/null" {
				feed = text("")
			} else {
				feed = &Feed{Opaque: "the file " + r.Target, File: r.Target}
			}
		case strings.HasPrefix(r.Op, "<&"):
			feed = &Feed{Opaque: "the descriptor " + r.Target, Lost: true}
			if f := FdFeeds(c)[r.Target]; f != nil {
				feed = f
			}
		}
	}
	if feed != nil {
		return feed
	}
	if c.PipeOpaque {
		return &Feed{Opaque: "the output of a group or a subshell", Lost: true}
	}
	if c.PipeFrom != nil {
		return outputOf(c.PipeFrom)
	}
	return nil
}

var echoFlags = lazyre.New(`^-[neE]+$`)

// FdFeeds are what the command's own redirections give its descriptors
// other than the input (`3< f`, `3< <(…)`, `4<<< …`), by number (T257).
func FdFeeds(c *Command) map[string]*Feed {
	var out map[string]*Feed
	for _, r := range c.Redirects {
		if r.N == "" || r.N == "0" {
			continue
		}
		var f *Feed
		switch {
		case r.Op == "<<" || r.Op == "<<-":
			if r.Body != nil {
				f = text(*r.Body)
			}
		case r.Op == "<<<":
			f = text(r.Target + "\n")
		case r.Op == "<" && r.Target == "(…)":
			f = procFeed(r.From)
		case r.Op == "<" || r.Op == "<>":
			if r.Target == "/dev/null" {
				f = text("")
			} else {
				f = &Feed{Opaque: "the file " + r.Target, File: r.Target}
			}
		}
		if f != nil {
			if out == nil {
				out = map[string]*Feed{}
			}
			out[r.N] = f
		}
	}
	return out
}

// procFeed is the file a `<(…)` process substitution names, read as its
// command's output (outputOf); opaque when it is no single command (T257).
func procFeed(from *Command) *Feed {
	if from == nil {
		return &Feed{Opaque: "a process substitution of more than one command", Lost: true}
	}
	return outputOf(from)
}

// passOn are the programs whose output is text they are given or fetch:
// the programs that never run anything (cat, head, base64, jq, …), the
// filters and packers, tee and dd, and the fetchers. A feed from one the
// reader does not follow is lost text (Feed.Lost, T257).
var passOn = set("tee", "dd", "sed", "awk", "gawk", "mawk", "nawk", "sort", "gzip", "gunzip", "zcat", "xz", "unxz", "zstd", "bzip2",
	"bunzip2", "iconv", "envsubst", "yq", "column", "fmt", "pr", "expand", "unexpand", "curl", "wget", "nc", "ncat", "netcat", "socat",
	"ssh", "scp", "openssl", "gpg", "xclip", "xsel", "wl-paste", "pbpaste")

// setupWords are the words by which a program prints a shell's setup:
// its completion, init, hook, activation or environment (Feed.Gen).
var setupWords = set("completion", "completions", "complete", "init", "hook", "activate", "shellenv", "env", "export", "docker-env", "podman-env",
	"signin", "-s", "--sh", "--bash", "--zsh")

// setupPrograms print a shell's setup with no such word (an agent's
// environment, colours).
var setupPrograms = set("ssh-agent", "gpg-agent", "dircolors", "keychain")

// generator is whether the command p prints a shell's setup by its own
// code (Feed.Gen): one program that passes nothing on, runs no
// shell or code of its own, reads no input, and says it prints setup
// (`/home/linuxbrew/.linuxbrew/bin/brew shellenv` too).
func generator(p *Command) bool {
	if len(p.Words) == 0 || p.PipeFrom != nil || p.PipeOpaque || len(p.Procs) > 0 || len(p.Substs) > 0 {
		return false
	}
	name := Base(p.Words[0])
	if neverRuns[name] || passOn[name] || Shells[name] || CodeInterpreter.MatchString(name) || codeRunners[name] || StdinOf(p) != nil {
		return false
	}
	if _, ok := Wrappers[name]; ok {
		return false
	}
	return setupPrograms[name] || slices.ContainsFunc(p.Words[1:], func(w string) bool { return setupWords[w] })
}

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
	// cat of `<(…)` files gives their text, in order; of one file, that
	// file; tee gives its input on (T257).
	if name == "cat" && !literal && len(args) == len(p.Procs) && !slices.ContainsFunc(args, func(a string) bool { return a != procMarker }) {
		var b strings.Builder
		for _, from := range p.Procs {
			f := procFeed(from)
			if f.Text == nil {
				return &Feed{Opaque: "the output of cat", Lost: true}
			}
			b.WriteString(*f.Text)
		}
		return text(b.String())
	}
	if name == "cat" && len(args) == 1 && literal && args[0] != "-" && !strings.HasPrefix(args[0], "-") {
		return &Feed{Opaque: "the file " + args[0], File: args[0]}
	}
	if name == "tee" {
		if f := StdinOf(p); f != nil && f.Text != nil {
			return f
		}
	}
	if name == "" {
		name = "a command"
	}
	return &Feed{Opaque: "the output of " + name, Lost: carriesLine(p, name), Gen: generator(p) || selectsSetup(p, name)}
}

// selectors pick lines of their input and add none: a shell's setup they
// select from is still that setup (`go env | grep GOPATH`). sed, awk and
// tr can write any text, so they are none.
var selectors = set("grep", "egrep", "fgrep", "head", "tail", "sort", "uniq", "cat", "tee")

// selectsSetup is whether p, one of selectors with literal words, selects
// from a shell's setup a program prints (Feed.Gen).
func selectsSetup(p *Command, name string) bool {
	if !selectors[name] || slices.ContainsFunc(p.Words[1:], func(a string) bool { return strings.ContainsAny(a, "$`") || strings.Contains(a, "…") }) {
		return false
	}
	f := StdinOf(p)
	return f != nil && f.Gen
}

// carriesLine is whether the output of p, a pipe's producer, holds text
// the line gives that the reader did not follow (Feed.Lost): a shell's or
// an interpreter's (its code is the line's), or one of a program that
// passes text on (passOn, neverRuns) whose input is the line's text or
// whose words hold a substitution or a variable. Files it reads on disk
// and what it fetches are not the line's.
func carriesLine(p *Command, name string) bool {
	if Shells[name] || CodeInterpreter.MatchString(name) {
		// Its code given inline is the line's; a script file's is not.
		return slices.ContainsFunc(p.Words[1:], shellC.MatchString) || InlineCode(name, p.Words) != nil
	}
	if !neverRuns[name] && !passOn[name] {
		return false
	}
	if f := StdinOf(p); f != nil && (f.Text != nil || f.Lost) {
		return true
	}
	// A substitution's output it passes on, or a variable echo prints.
	return slices.ContainsFunc(p.Words[1:], func(a string) bool {
		return strings.Contains(a, "…") || (name == "echo" || name == "printf") && strings.ContainsAny(a, "$`")
	})
}

var (
	printfSpec  = lazyre.New(`%(?:%|[-+ #0-9.]*[sbdiqxXoufeEgGc])`)
	printfTakes = lazyre.New(`%[^%]`)
)

// PrintfVar is the variable `printf -v <name> <format> [args]` sets and
// its value, when the words are such a printf with literal words (T257).
func PrintfVar(words []string) (name, value string, ok bool) {
	if len(words) < 4 || Base(words[0]) != "printf" || words[1] != "-v" ||
		slices.ContainsFunc(words[3:], func(a string) bool { return strings.Contains(a, "$") || strings.Contains(a, "…") }) {
		return "", "", false
	}
	return words[2], printfText(words[3], words[4:]), true
}

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
	// Forward marks a program of a transport's plain forward (Command.Forward).
	Forward bool
	// Input is the text the program is given to read that the reader sees:
	// its standard input's, and each `<(…)` file's among its words (T257).
	Input []string
	// InputFile is the file on disk its input is (`< f`, `cat f |`), and
	// InputLost what it reads that holds text the reader lost (Feed.Lost),
	// "" when none (T257).
	InputFile, InputLost string
	// ScriptFile is the file on disk a shell, an interpreter, make or a
	// sourced file reads its own program from (`bash < f`, `cat f | sh`),
	// read as ScriptRole (StdinRole) says (T257).
	ScriptFile, ScriptRole string
}

// Opts are what programs reads beyond the words: the standard input, the
// files the `<(…)` among the words name (Command.Procs, in their order) and
// the variables set before.
type Opts struct {
	Feed   *Feed
	Procs  []*Feed
	Substs []*Feed
	Fds    map[string]*Feed
	Envv   map[string]string
}

// procMarker is how a word shows a `<(…)` process substitution.
const procMarker = "<(…)"

// withInput is self with the text opts gives it to read (Program.Input,
// InputFile, InputLost).
func withInput(self Program, opts Opts) Program {
	feeds := append([]*Feed{opts.Feed}, opts.Procs...)
	// A descriptor's path among its words (`-f /dev/fd/3`, `--file=/dev/fd/3`)
	// is a file it reads: what the line's redirection gives it, else lost.
	for _, a := range self.Argv[min(1, len(self.Argv)):] {
		if i := strings.Index(a, "/dev/fd/"); i < 0 && !strings.Contains(a, "/proc/") {
			continue
		}
		for _, p := range []string{a, a[strings.IndexByte(a, '=')+1:], strings.TrimPrefix(a, "-f")} {
			if fdPath.MatchString(p) && !StdinNames[p] {
				f := opts.Fds[path.Base(p)]
				if f == nil {
					f = &Feed{Opaque: "the descriptor " + p, Lost: true}
				}
				feeds = append(feeds, f)
				break
			}
		}
	}
	for i, f := range feeds {
		switch {
		case f == nil:
		case f.Text != nil:
			if strings.TrimSpace(*f.Text) != "" {
				self.Input = append(self.Input, *f.Text)
			}
		case f.Lost && self.InputLost == "":
			self.InputLost = f.Opaque
		case i == 0 && f.File != "":
			self.InputFile = f.File
		}
	}
	return self
}

// fdPath is a path that names one of the process's descriptors.
var fdPath = lazyre.New(`^/(?:dev/fd|proc/(?:self|thread-self|\d+)/fd)/\d+$`)

// procScript is how a program reads its own program from a `<(…)` file
// among w, as StdinRole names it with that word read as /dev/stdin (`bash
// <(…)`, `source <(…)`, `python3 <(…)`, `make -f <(…)`), with that file's
// feed; "" when it reads none (T257). The words'
// substitutions are matched to procs by their order, and only when every
// one is there.
func procScript(name string, w []string, procs []*Feed, fds map[string]*Feed) (string, *Feed) {
	// A descriptor's path other than the input's (`bash /dev/fd/3 3<
	// …`): what it holds is not followed (T257).
	for i, a := range w {
		if fdPath.MatchString(a) && !StdinNames[a] {
			read := slices.Clone(w)
			read[i] = "/dev/stdin"
			if role := StdinRole(name, read); role != "" {
				if f := fds[path.Base(a)]; f != nil {
					return role, f
				}
				return role, &Feed{Opaque: "the descriptor " + a, Lost: true}
			}
		}
	}
	n := 0
	for _, a := range w {
		n += strings.Count(a, procMarker)
	}
	if n == 0 || n != len(procs) || StdinRole(name, w) != "" {
		return "", nil
	}
	k := 0
	for i, a := range w {
		if a != procMarker {
			k += strings.Count(a, procMarker)
			continue
		}
		read := slices.Clone(w)
		read[i] = "/dev/stdin"
		if role := StdinRole(name, read); role != "" {
			return role, procs[k]
		}
		k++
	}
	return "", nil
}

// evalLine is the line eval runs from its words: each `$(…)` or backquoted
// substitution read as its command's output where the reader sees it
// (`eval "$(echo …)"`), kept as written where it is a shell's setup a
// program prints (`eval "$(direnv hook bash)"`, Feed.Gen); any other is
// unreadable (T257). The substitutions are matched to substs by their
// order, and only when every one is there.
func evalLine(words []string, substs []*Feed) (string, []string, error) {
	line := strings.Join(words, " ")
	n := len(substMarks.FindAllStringIndex(line, -1))
	// A substitution a variable holds is a computed command, out of scope as before.
	if n == 0 || len(substs) == 0 {
		return line, nil, nil
	}
	if n != len(substs) {
		return "", nil, &Unreadable{Why: "eval runs a substitution the guard cannot tell"}
	}
	k := 0
	var err error
	var files []string
	out := substMarks.ReplaceAllStringFunc(line, func(m string) string {
		f := substs[k]
		k++
		switch {
		case f.Text != nil:
			return strings.TrimRight(*f.Text, "\n")
		case f.File != "":
			files = append(files, f.File)
			return ""
		case f.Gen:
			return m
		}
		if err == nil {
			err = &Unreadable{Why: "eval runs " + f.Opaque + ", which the guard cannot read"}
		}
		return m
	})
	return out, files, err
}

// substMarks are the markers a word shows its substitutions by.
var substMarks = lazyre.New("\\$\\(…\\)|`…`")

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
	self := withInput(Program{Argv: w, Via: route, Env: env, Envv: envv, Name: name}, opts)
	sub := func(extra string) []string { return append(slices.Clone(route), extra) }
	inner := Opts{Feed: opts.Feed, Procs: opts.Procs, Substs: opts.Substs, Fds: opts.Fds, Envv: envv}
	if name == "command" && (at(w, 1) == "-v" || at(w, 1) == "-V") {
		return []Program{self}, nil
	}
	// zsh's precommand modifiers and repeat run the command after them: it is read as it is read bare (T228).
	if rest, ok := precommand(name, w); ok {
		if strings.HasPrefix(w[0], "{") {
			// A brace group or list is its words, on no route of its own.
			return Programs(rest, route, env, inner)
		}
		return Programs(rest, sub(name), env, inner)
	}
	// External runners, and builtins that run a string later (T228 L2).
	if ps, ok, err := runnerPrograms(name, w, self, sub, env, inner); ok {
		return ps, err
	}
	if _, ok := Wrappers[name]; ok {
		wr := wrapped(name, w)
		if wr.split != nil {
			return JudgeString(*wr.split, sub(name), env, inner)
		}
		// later is what env's assignments run later (codeOf): a prompt's substitutions, PROMPT_COMMAND.
		var later []Program
		for _, a := range wr.assigns {
			n, v, _ := strings.Cut(a, "=")
			env = append(slices.Clone(env), n)
			envv[n] = v
			cmds, err := codeOf(n, "", v)
			if err != nil {
				return nil, err
			}
			for _, c := range cmds {
				ps, err := CommandPrograms(c, sub(name), env, inner)
				if err != nil {
					return nil, err
				}
				later = append(later, ps...)
			}
		}
		ps, err := Programs(wr.rest, sub(name), env, inner)
		ps = append(later, ps...)
		if err != nil || wr.chdir == nil {
			return ps, err
		}
		return with(ps, func(p *Program) { p.Chdir = append([]string{*wr.chdir}, p.Chdir...) }), nil
	}
	if name == "eval" {
		line, files, err := evalLine(w[1:], opts.Substs)
		if err != nil {
			return nil, err
		}
		// `eval "$(cat f)"` runs f in the shell, as `source f` does: read on disk.
		for _, f := range files {
			line = "source '" + strings.ReplaceAll(f, "'", `'\''`) + "'\n" + line
		}
		return JudgeString(line, sub("eval"), env, inner)
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
				// The string is the first word after the options, which may follow
				// -c (`bash -c -e '…'`, T228 L8).
				s := k + 1
				for s < len(w) && len(w[s]) > 1 && (w[s][0] == '-' || w[s][0] == '+') && w[s] != "--" {
					if shellOptArg.MatchString(w[s]) {
						s++
					}
					s++
				}
				if at(w, s) == "--" {
					s++
				}
				// The words after the string are its $0, $1, …: the guard
				// resolves a script it runs or sources by them (L294).
				in := inner
				in.Envv = Positional(envv, w[min(s+1, len(w)):])
				ps, err := JudgeString(at(w, s), sub(name+" -c"), env, in)
				// zsh started with GLOB_SUBST on (T228 L2).
				if l := GlobSubstOn(name, w[1:s]); l != "" {
					ps = append([]Program{{Argv: []string{GlobSubst, l}, Via: route, Env: env, Envv: envv, Name: GlobSubst}}, ps...)
				}
				return ps, err
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
			// A wrapper's or a shell's own words are still read, without the
			// operands it does not show (`xargs nohup rm -rf`, T228 L2).
			if len(cmd) == 0 {
				return []Program{self}, nil
			}
			ps, err := Programs(cmd, sub(XargsUnseen), env, Opts{Envv: envv})
			return append([]Program{self}, ps...), err
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
	// A script read from a `<(…)` file is read as one read on the input (T257).
	feed, role := opts.Feed, ""
	if feed != nil {
		role = StdinRole(name, w)
	}
	if r, f := procScript(name, w, opts.Procs, opts.Fds); r != "" {
		role, feed = r, f
	}
	if feed != nil {
		if role != "" {
			if feed.Text == nil {
				// What the guard cannot read fails closed (T257): a file on
				// disk is read there, a shell's setup a program prints is
				// its own; anything else is unreadable.
				self.StdinOpaque = feed.Opaque
				if feed.File != "" {
					self.ScriptFile, self.ScriptRole = feed.File, role
					return []Program{self}, nil
				}
				if !feed.Gen {
					return nil, &Unreadable{Why: name + " runs what it reads from " + feed.Opaque + ", which the guard cannot read"}
				}
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
			if role == "cron" {
				t = CronCommands(t)
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
	opts.Procs, opts.Substs, opts.Fds = nil, nil, FdFeeds(c)
	for _, from := range c.Procs {
		opts.Procs = append(opts.Procs, procFeed(from))
	}
	for _, from := range c.Substs {
		opts.Substs = append(opts.Substs, procFeed(from))
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
		p.Forward = p.Forward || c.Forward
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

// Runners that run their operand later or elsewhere (L479): parallel's
// jobs (flock's command is read with the other runners, runs). Each is read as the program itself and what it runs; ok is
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
