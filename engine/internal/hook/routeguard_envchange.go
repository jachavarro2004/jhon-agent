package hook

import (
	"regexp"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

// guardedEnv is a variable an agent's line may not change (T219 S9, L14):
// the agent's marker, git configuration given through the environment, and
// what changes how the merge gate's shell line runs (its options, an exported
// function). ENV and BASH_ENV are not guarded: the hook's sh runs
// non-interactive in POSIX mode and reads neither, and apps use ENV (L17).
var guardedEnv = regexp.MustCompile(`^(?:` + AgentVariable + `|GIT_CONFIG_(?:PARAMETERS|COUNT|KEY_\w*|VALUE_\w*|GLOBAL|SYSTEM)|SHELLOPTS|BASHOPTS|BASH_FUNC_.*)$`)

// envGuardedPrefix is whether every variable with prefix p (`${!p@}`) may
// hold a guarded one.
func envGuardedPrefix(p string) bool {
	for _, n := range []string{AgentVariable, "GIT_CONFIG_", "SHELLOPTS", "BASHOPTS", "BASH_FUNC_"} {
		if strings.HasPrefix(n, p) || strings.HasPrefix(p, "GIT_CONFIG_") || strings.HasPrefix(p, "BASH_FUNC_") {
			return true
		}
	}
	return false
}

var (
	// indirectNames is `${!PREFIX@}` or `${!PREFIX*}`: every variable with that prefix.
	indirectNames = regexp.MustCompile(`^\$\{!([A-Za-z_][A-Za-z0-9_]*)[@*]\}$`)
	// braceList is one `{a,b,…}` list with no nested brace.
	braceList = regexp.MustCompile(`\{([^{}]*,[^{}]*)\}`)
)

// braces is w with its simple brace lists expanded, as bash expands them.
func braces(w string) []string {
	loc := braceList.FindStringSubmatchIndex(w)
	if loc == nil {
		return []string{w}
	}
	var out []string
	for _, alt := range strings.Split(w[loc[2]:loc[3]], ",") {
		out = append(out, braces(w[:loc[0]]+alt+w[loc[1]:])...)
		if len(out) > 64 {
			break
		}
	}
	return out
}

// guardedWord is why w (an assignment, an unset's or a declaration's name,
// its brace lists expanded) changes a guarded variable, "" when it does not:
// a guarded name, every variable with a prefix that may hold one, or a name
// the guard cannot read.
func guardedWord(w string) string {
	for _, e := range braces(w) {
		name := e
		if n, ok := assignment(e); ok {
			name = n
		}
		if m := indirectNames.FindStringSubmatch(name); m != nil {
			if envGuardedPrefix(m[1]) {
				return "it names every variable that starts with " + m[1] + ", " + AgentVariable + " or git's configuration among them"
			}
			continue
		}
		if guardedEnv.MatchString(name) {
			return "it changes " + name
		}
		if unnamed.MatchString(name) {
			return "it changes a variable whose name the guard cannot read (" + name + ")"
		}
	}
	return ""
}

// envChange is why command changes, as it runs, the environment the merge
// gate reads, "" when it does not (L14): what the line executes, never a
// name it only hands to a reader (echo, printenv, grep, rg, cat). Words are
// read as the shell gives them (quotes and backslashes removed) and simple
// brace lists expanded. It judges an assignment (alone or before a
// program), export, declare, typeset, readonly, local, unset, read, printf
// -v, mapfile, readarray, an exported function, `exec -c`, and env by any
// path or through command, builtin, exec, nohup, time or what the shared
// reader unwraps (envUnwrapped: -, noglob, repeat, nice, setsid, …): -i, -, an
// abbreviation of --ignore-environment, -u and --unset, -S, and the
// variables it sets. A shell's -c string and eval's words are read the same
// way. A line the parser cannot read is judged by its text.
func envChange(command string) string {
	return envChangeIn(command, 0)
}

func envChangeIn(src string, depth int) string {
	cmds, err := sh.Parse(src)
	if err != nil {
		if markerWord.MatchString(src) || configEnv.MatchString(src) {
			return "the line cannot be read, and it names " + AgentVariable + " or GIT_CONFIG_*"
		}
		return ""
	}
	for _, c := range cmds {
		if why := envCommand(c.Words, depth); why != "" {
			return why
		}
	}
	return ""
}

// assignment is the name w assigns (`NAME=…`, `NAME+=…`).
func assignment(w string) (string, bool) {
	name, _, ok := strings.Cut(w, "=")
	name = strings.TrimSuffix(name, "+")
	if !ok || name == "" || strings.ContainsAny(name, " /") {
		return "", false
	}
	return name, true
}

// envCommand judges one simple command's words.
func envCommand(words []string, depth int) string {
	k := 0
	for ; k < len(words); k++ {
		if !sh.Assignment.MatchString(words[k]) {
			break
		}
		if why := guardedWord(words[k]); why != "" {
			return why
		}
	}
	if k >= len(words) {
		return ""
	}
	args := words[k+1:]
	switch prog := sh.Base(words[k]); prog {
	case "command", "builtin", "nohup", "time":
		if prog == "command" && len(args) > 0 && (args[0] == "-v" || args[0] == "-V") {
			return ""
		}
		for len(args) > 0 && strings.HasPrefix(args[0], "-") {
			args = args[1:]
		}
		return envCommand(args, depth)
	case "exec":
		for len(args) > 0 && strings.HasPrefix(args[0], "-") && args[0] != "--" {
			if strings.Contains(args[0], "c") {
				return "`exec -c` starts the program with an empty environment, without " + AgentVariable
			}
			if strings.Contains(args[0], "a") && len(args) > 1 {
				args = args[1:]
			}
			args = args[1:]
		}
		if len(args) > 0 && args[0] == "--" {
			args = args[1:]
		}
		return envCommand(args, depth)
	case "env":
		return envArgs(args, depth)
	case "unset":
		for _, a := range args {
			if strings.HasPrefix(a, "-") {
				continue
			}
			if why := guardedWord(a); why != "" {
				return why
			}
		}
	case "export", "declare", "typeset", "readonly", "local":
		for _, a := range args {
			if strings.HasPrefix(a, "-") {
				if strings.Contains(a, "f") && (prog == "export" || strings.Contains(a, "x")) {
					return "it exports a shell function, which can replace a command of the merge gate's shell line"
				}
				continue
			}
			if why := guardedWord(a); why != "" {
				return why
			}
		}
	case "read", "mapfile", "readarray":
		// Options that take a value other than a name.
		valued := "dinNptu"
		if prog != "read" {
			valued = "dnOsuCc"
		}
		for i := 0; i < len(args); i++ {
			a := args[i]
			if len(a) == 2 && a[0] == '-' && strings.ContainsRune(valued, rune(a[1])) {
				i++
				continue
			}
			if strings.HasPrefix(a, "-") && a != "-a" {
				continue
			}
			if a == "-a" {
				if i++; i >= len(args) {
					break
				}
				a = args[i]
			}
			if why := guardedWord(a); why != "" {
				return why
			}
		}
	case "printf":
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "-v" {
				if why := guardedWord(args[i+1]); why != "" {
					return why
				}
			}
		}
	case "eval":
		if depth < 4 {
			return envChangeIn(strings.Join(args, " "), depth+1)
		}
	default:
		if sh.Shells[prog] && depth < 4 {
			for i, a := range args {
				if a == "-c" && i+1 < len(args) {
					return envChangeIn(args[i+1], depth+1)
				}
			}
		}
		return envUnwrapped(words[k:], depth)
	}
	return ""
}

// envUnwrapped judges what a program that runs another runs, read by the
// shared reader (sh.Unwrap, T228 L2): zsh's precommand modifiers and
// repeat, a wrapper (nice, timeout, sudo, …), an external runner (setsid,
// chrt, strace, watch, …) or a builtin that runs a string later (trap,
// sched, zpty, alias, emulate -c). Its words are judged as the line's own,
// its strings as a shell's -c string. A runner whose options the reader
// cannot read, so that the command it runs cannot be told, is judged by its
// words: one that is env, exec, unset or an export, or changes a guarded
// variable, is found.
func envUnwrapped(words []string, depth int) string {
	in, ok, err := sh.Unwrap(words)
	if !ok {
		return ""
	}
	if err != nil {
		for _, w := range words[1:] {
			if b := sh.Base(w); b == "env" || b == "exec" || b == "unset" || b == "export" || guardedWord(w) != "" {
				return "the line cannot be read (" + err.Error() + "), and it runs " + w
			}
		}
		if src := strings.Join(words, " "); markerWord.MatchString(src) || configEnv.MatchString(src) {
			return "the line cannot be read (" + err.Error() + "), and it names " + AgentVariable + " or GIT_CONFIG_*"
		}
		return ""
	}
	for _, s := range in.Strings {
		if depth < 4 {
			if why := envChangeIn(s, depth+1); why != "" {
				return why
			}
		}
	}
	// Words are fewer at each step: no bound is needed.
	for _, w := range in.Words {
		if why := envCommand(w, depth); why != "" {
			return why
		}
	}
	return ""
}

// envArgs judges env's arguments: its options, the variables it sets, and
// the program it runs.
func envArgs(args []string, depth int) string {
	cleared := "env " + "starts the program with an empty environment, without " + AgentVariable
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-" || a == "-i":
			return cleared
		case a == "--":
			return envCommand(args[i+1:], depth)
		case strings.HasPrefix(a, "--"):
			name, value, attached := strings.Cut(a, "=")
			switch {
			case len(name) >= 3 && strings.HasPrefix("--ignore-environment", name):
				return cleared
			case len(name) >= 3 && strings.HasPrefix("--unset", name):
				if !attached && i+1 < len(args) {
					i++
					value = args[i]
				}
				if why := guardedWord(value); why != "" {
					return why
				}
			case len(name) >= 3 && strings.HasPrefix("--split-string", name):
				return "env -S splits a string the guard does not read into the program's words"
			case len(name) >= 3 && strings.HasPrefix("--chdir", name) && !attached:
				i++
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case 'i':
					return cleared
				case 'S':
					return "env -S splits a string the guard does not read into the program's words"
				case 'u', 'C':
					value := a[j+1:]
					if value == "" && i+1 < len(args) {
						i++
						value = args[i]
					}
					if a[j] == 'u' {
						if why := guardedWord(value); why != "" {
							return why
						}
					}
					j = len(a)
				}
			}
		default:
			if sh.Assignment.MatchString(a) {
				if why := guardedWord(a); why != "" {
					return why
				}
				continue
			}
			return envCommand(args[i:], depth)
		}
	}
	return ""
}
