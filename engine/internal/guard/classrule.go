package guard

import (
	"regexp"
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
// keeps that reading, and one that never runs its arguments keeps its
// verdict whatever they say.

// neverRuns are the programs that never run their arguments: they read,
// print, compare or make files and text, or find or signal processes by a
// pattern, and take no option that runs a program (rg's --pre is the
// exception modelled takes out). Not here, as they do run one: sed (its e
// command), awk (system()), sort (--compress-program), tar (--to-command),
// less and more (! and LESSOPEN), vim, find, xargs.
var neverRuns = set("ls", "cat", "tac", "head", "tail", "wc", "grep", "egrep", "fgrep", "rg", "echo", "printf", "print", "diff", "cmp",
	"comm", "uniq", "cut", "tr", "paste", "column", "nl", "fold", "rev", "od", "xxd", "hexdump", "strings", "file", "stat", "du",
	"df", "basename", "dirname", "realpath", "readlink", "pwd", "which", "whereis", "true", "false", "test", "[", "date", "sleep",
	"seq", "base64", "md5sum", "sha1sum", "sha256sum", "sha512sum", "cksum", "jq", "printenv", "id", "whoami", "uname", "hostname",
	"tree", "touch", "mkdir", ":", "type", "command", "ps", "pgrep", "pkill", "pidof", "kill", "killall")

// rgRuns is rg's option that runs a program on each file it searches.
var rgRuns = regexp.MustCompile(`^--pre(?:=|$)`)

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

// modelled is whether the guard reads a program exactly: it never runs its
// arguments (neverRuns), or the reader or the guard reads what it runs.
func modelled(p sh.Program, name string, args []string) bool {
	if name == "rg" && containsFunc(args, rgRuns.MatchString) {
		return false
	}
	if neverRuns[name] || p.Code != nil || classRuns(p.Argv) || isRunner(name) ||
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
// themselves, an option's value with the words after it (`--command=rm
// app -rf ~`), and each piece of an argument cut at classSeparators.
func classLines(args []string) [][]string {
	lines := [][]string{args}
	for i, a := range args {
		if _, v, ok := strings.Cut(a, "="); ok && strings.HasPrefix(a, "-") && v != "" {
			lines = append(lines, append([]string{v}, args[i+1:]...))
		}
		if !strings.ContainsAny(a, classSeparators+" ") {
			continue
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
	if ctx.noClass || depth >= 2 || modelled(p, name, args) {
		return nil
	}
	inner := *ctx
	inner.noClass, inner.targets = true, nil
	for _, l := range classLines(args) {
		for j := range l {
			if !classCandidate.MatchString(l[j]) || !classRuns(l[j:]) {
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
