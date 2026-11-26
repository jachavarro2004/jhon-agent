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
// for text that is not a shell line (awk's, vim's), each piece of an
// argument cut at classSeparators; and an argument's quoted strings in a
// row as one argv, as an editor's or a language's list gives a program
// (`system(["rm", "-rf", "~"])`, T257).
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
		if strs := sh.CodeStrings(a); len(strs) > 1 {
			lines = append(lines, strs)
		}
	}
	return lines
}

// classText is the word lists and the code a program's arguments and the
// text it reads hold (Program.Input: its standard input, a `<(…)` file),
// labelled for the class rule's question: "they" for the arguments, the
// input for what it reads (T257).
type classText struct {
	label string
	texts []string
	lines [][]string
}

func classTexts(args, input []string) []classText {
	out := []classText{{"they hold", args, classLines(args)}}
	if len(input) > 0 {
		var lines [][]string
		for _, in := range input {
			lines = append(lines, classLines([]string{in})[1:]...)
		}
		out = append(out, classText{"the text it reads (its input or a <(…) file) holds", input, lines})
	}
	return out
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
	texts := classTexts(args, ownInput(p, name, ctx))
	if modelled(p, name, args, ctx) {
		// A program read exactly still reads its input as its own (gdb's
		// commands, expect's script): only that is judged (T257).
		if len(texts) == 1 {
			return nil
		}
		texts = texts[1:]
	}
	inner := *ctx
	inner.noClass, inner.targets = true, nil
	for _, t := range texts {
		for _, l := range t.lines {
			for j := range l {
				// A program the guard judges, or an interpreter given its code inline (T228 L8).
				if !classCandidate.MatchString(l[j]) || !classRuns(l[j:]) && sh.InlineCode(sh.Base(l[j]), l[j:]) == nil {
					continue
				}
				cst := &State{Dir: st.Dir, Lost: st.Lost, Links: st.Links.clone()}
				for _, f := range judgeProgramWords(l[j:], nil, &inner, vars, cst, depth+1, append(append([]string(nil), p.Via...), name), p.Envv) {
					if f.Level == "deny" || f.Level == "ask" && f.Cls != unreadable {
						end := min(j+4, len(l))
						return classAsk(name, t.label, "`"+strings.Join(l[j:end], " ")+"`, which it would "+map[string]string{"deny": "refuse", "ask": "ask"}[f.Level])
					}
				}
			}
		}
		// What an editor or a language deletes or writes through its own
		// API (vim's delete(), nvim's os.remove, emacs's
		// delete-directory), read as an interpreter's code is (T257).
		code := strings.Join(t.texts, "\n")
		q := p
		q.Name, q.Code = name, &code
		for _, f := range judgeCode(q, name, &inner, vars, st.Dir, st.Lost, depth+1) {
			if f.Level == "deny" {
				return classAsk(name, t.label, strings.TrimSpace("code "+strings.TrimPrefix(strings.TrimPrefix(f.What, name+" code"), " "))+": "+f.Why+", which it would refuse")
			}
		}
	}
	return nil
}

// inputData are programs whose input is data they filter, pack or show,
// never commands: their own commands are their arguments (awk's, sed's) or
// keys typed at a terminal (less's).
var inputData = set("sed", "awk", "gawk", "mawk", "nawk", "sort", "tar", "less", "more", "most", "gzip", "gunzip", "zcat", "xz",
	"unxz", "zstd", "bzip2", "bunzip2", "iconv", "envsubst", "patch", "yq", "column", "fmt", "pr", "expand", "unexpand", "sponge")

// ownInput is the text p is given to read (Program.Input) when p may read
// it as commands of its own (T257): not when the reader already read it as
// p's program (a shell's or an interpreter's script, make's, crontab's,
// at's), when p never runs anything by a name the guard trusts, when it is
// data p filters or writes (inputData, the removal and writing programs,
// git, gh, jhon), xargs's or parallel's operands, or the input a wrapper
// hands the program it runs, which is judged with it.
func ownInput(p sh.Program, name string, ctx *Ctx) []string {
	if len(p.Input) == 0 || p.Code != nil || sh.StdinRole(name, p.Argv) != "" || sh.Shells[name] || sh.NeverRuns(name) && trusted(p, ctx) ||
		inputData[name] || rmLike[name] || disk[name] || xargsDestroy[name] || name == "git" || name == "gh" || name == "jhon" ||
		name == "find" || name == "xargs" || name == "parallel" || name == "at" || name == "batch" || name == "crontab" {
		return nil
	}
	if in, ok, _ := sh.Unwrap(p.Argv); ok && len(in.Words) > 0 {
		return nil
	}
	return p.Input
}

// classAsk is the class rule's question: name is no program the guard
// knows never runs its arguments, and what label names holds what.
func classAsk(name, label, what string) *Finding {
	u := unreadableAsk(&sh.Unreadable{Why: name + " is not a program the guard knows never runs its arguments, and " + label + " " + what})
	return &u
}
