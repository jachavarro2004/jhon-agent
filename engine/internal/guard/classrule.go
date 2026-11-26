package guard

import (
	"regexp"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/sh"
)

// The class rule (T228 L6, the owner's decision): any program may run its
// arguments as a command (a runner nobody listed: tmux send-keys, nix-shell
// --run, conda run, entr, watchexec, awk's system(), vim -c '!…', …), so a
// program the guard has no reading of, given among its words a command the
// guard would refuse or ask, makes the line unreadable: asked at the top
// level, refused to a sub-agent. A program the guard reads exactly (a
// runner grammar, git, find, xargs, a shell's -c, an interpreter's code)
// keeps that reading, and one that never runs its arguments
// (sh.NeverRuns), called by its bare name, keeps its verdict whatever they
// say.

// rgRuns are rg's options that run a program: --pre on each file it
// searches, --hostname-bin for its hyperlinks.
var rgRuns = regexp.MustCompile(`^--(?:pre|hostname-bin)(?:=|$)`)

// classCandidate is a word that may name the program a command line runs:
// a plain name or an absolute path, never `s/rm` or a URL.
var classCandidate = regexp.MustCompile(`^(?:/[A-Za-z0-9_./+-]*/)?[A-Za-z0-9_.+-]+$`)

// classSeparators split an argument into the command lines it may hold:
// a shell's operators, quotes, substitutions, awk's and vim's `(`, `{`, `!`, and `=`.
const classSeparators = ";&|(){}`\"'!=\n\t<>,"

// classRuns is whether the guard reads a program as one that runs others,
// or judges its words itself: what classify finds in a command that opens
// with it is the command's own effect (rm and its kin, chmod, dd, git, gh,
// find, jhon, a shell, a wrapper or runner the reader unwraps).
func classRuns(words []string) bool {
	name := sh.Base(words[0])
	if rmLike[name] || disk[name] || xargsDestroy[name] || strings.HasPrefix(name, "mkfs.") || sh.Shells[name] ||
		name == "find" || name == "git" || name == "gh" || name == "jhon" || name == "eval" || name == "chgrp" {
		return true
	}
	_, ok, _ := sh.Unwrap(words)
	return ok
}

// trusted is whether p's program word names the program it is called by:
// a bare name (not ./ls, bin/cat or /usr/local/bin/ls, which may be any
// program), with no function or alias of that name on the line and no
// PATH of the line's own (T228 L8).
func trusted(p sh.Program, ctx *Ctx) bool {
	return !strings.Contains(at(p.Argv, 0), "/") && !ctx.defined[p.Name] && !slices.Contains(p.Env, "PATH") && p.Envv["PATH"] == ""
}

// modelled is whether the guard reads a program exactly: it never runs its
// arguments (sh.NeverRuns) by a name it can trust, or the reader or the
// guard reads what it runs.
func modelled(p sh.Program, name string, args []string, ctx *Ctx) bool {
	if sh.NeverRuns(name) && trusted(p, ctx) || p.Code != nil || classRuns(p.Argv) || isRunner(name) ||
		paneMultiplexers[name] || name == "rtk" || name == "cd" || name == "pushd" || name == "source" || name == "." ||
		name == "parallel" || name == "at" || name == "batch" || name == "make" || name == "gmake" || name == sh.GlobCode || name == sh.GlobSubst {
		return true
	}
	return pythonName.MatchString(name) && at(args, 0) == "-m"
}

// isRunner is whether runnerTarget reads name, a package runner (npx, uv, poetry, …).
func isRunner(name string) bool {
	_, ok := runners[name]
	return ok
}

func containsFunc(xs []string, f func(string) bool) bool {
	for _, x := range xs {
		if f(x) {
			return true
		}
	}
	return false
}

// classLines is the word lists args may hold as command lines: args
// themselves; an option's value with the words after it (`--command=rm
// app -rf ~`); each argument read as a shell line (T228 L8: its quotes,
// escapes and substitutions as the shell reads them, `rm "-rf" "$HOME"`),
// and the arguments joined as one (keys a terminal is sent one by one);
// and, for text that is not a shell line (awk's, vim's), each piece of an
// argument cut at classSeparators.
func classLines(args []string) [][]string {
	lines := [][]string{args}
	shell := func(s string) {
		if cmds, err := sh.Parse(s); err == nil {
			for _, c := range cmds {
				lines = append(lines, c.Words)
			}
		}
	}
	// Keys sent one by one (`tmux send-keys r m ' ' …`): each run of the arguments joined.
	for i := 0; i+1 < len(args) && i < 16; i++ {
		shell(strings.Join(args[i:], ""))
	}
	for i, a := range args {
		if _, v, ok := strings.Cut(a, "="); ok && strings.HasPrefix(a, "-") && v != "" {
			lines = append(lines, append([]string{v}, args[i+1:]...))
		}
		if !strings.ContainsAny(a, classSeparators+" \\$") {
			continue
		}
		shell(a)
		// An editor's or a REPL's shell escape (`!cmd`, `:!cmd`).
		if t := strings.TrimLeft(a, ":!"); t != a {
			shell(t)
		}
		for _, piece := range strings.FieldsFunc(a, func(r rune) bool { return strings.ContainsRune(classSeparators, r) }) {
			if f := strings.Fields(piece); len(f) > 0 {
				lines = append(lines, f)
			}
		}
	}
	return lines
}

// classFinding is the unreadable question of a program the guard has no
// reading of (modelled) when a command among its words (classLines, each
// suffix that opens with a program classRuns names) is one the guard would
// refuse or ask: nil otherwise. The candidates are judged as a program of
// their own, on a copy of the state, with the rule off.
func classFinding(p sh.Program, name string, args []string, ctx *Ctx, vars map[string]string, st *State, depth int) *Finding {
	if ctx.noClass || depth >= 2 {
		return nil
	}
	// rg runs the program its --pre (or --hostname-bin, or a config file) names.
	if name == "rg" && (containsFunc(args, rgRuns.MatchString) || slices.Contains(p.Env, "RIPGREP_CONFIG_PATH")) {
		u := unreadableAsk(&sh.Unreadable{Why: "rg runs the program its --pre, --hostname-bin or config file names, which the guard does not read"})
		return &u
	}
	if modelled(p, name, args, ctx) {
		return nil
	}
	inner := *ctx
	inner.noClass, inner.targets = true, nil
	for _, l := range classLines(args) {
		for j := range l {
			// A program the guard judges, or an interpreter given its code inline (T228 L8).
			if !classCandidate.MatchString(l[j]) || !classRuns(l[j:]) && sh.InlineCode(sh.Base(l[j]), l[j:]) == nil {
				continue
			}
			cst := &State{Dir: st.Dir, Lost: st.Lost, Links: st.Links.clone()}
			for _, f := range judgeProgramWords(l[j:], nil, &inner, vars, cst, depth+1, append(append([]string(nil), p.Via...), name), p.Envv) {
				if f.Level == "deny" || f.Level == "ask" && f.Cls != unreadable {
					end := min(j+4, len(l))
					u := unreadableAsk(&sh.Unreadable{Why: name + " is not a program the guard knows never runs its arguments, and they hold `" +
						strings.Join(l[j:end], " ") + "`, which it would " + map[string]string{"deny": "refuse", "ask": "ask"}[f.Level]})
					return &u
				}
			}
		}
	}
	return nil
}
