package sh

import (
	"slices"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// neverRuns are the programs that never run their arguments: they read,
// print, compare or make files and text, or find or signal processes by a
// pattern, and take no option that runs a program (rg's --pre and
// --hostname-bin are the exceptions the guard reads). test, [, printf and
// print stay: the reader reads what they run, a subscript's substitution
// (test -v, printf -v, T228 L8) and print -P's prompt. Not here, as they do
// run one: sed (its e command, read by the reader), awk (system()), sort
// (--compress-program), tar (--to-command), less and more (! and
// LESSOPEN), vim, find, xargs. The class rule (guard) and route-guard's
// environment check trust this list only for a bare name with no function
// or alias of that name on the line.
var neverRuns = set("ls", "cat", "tac", "head", "tail", "wc", "grep", "egrep", "fgrep", "rg", "echo", "printf", "print", "diff", "cmp",
	"comm", "uniq", "cut", "tr", "paste", "column", "nl", "fold", "rev", "od", "xxd", "hexdump", "strings", "file", "stat", "du",
	"df", "basename", "dirname", "realpath", "readlink", "pwd", "which", "whereis", "true", "false", "test", "[", "date", "sleep",
	"seq", "base64", "md5sum", "sha1sum", "sha256sum", "sha512sum", "cksum", "jq", "printenv", "id", "whoami", "uname", "hostname",
	"tree", "touch", "mkdir", ":", "type", "command", "ps", "pgrep", "pkill", "pidof", "kill", "killall")

// NeverRuns is whether name is a program that never runs its arguments (neverRuns).
func NeverRuns(name string) bool { return neverRuns[name] }

// Untrusted opens the name DefinedNames gives the place of a command that
// may define names the reader cannot see (eval of a string it cannot read,
// a file sourced through a descriptor or a substitution, hash -p, alias,
// enable -f): Untrusted and the command's byte offset in the line. No name
// is trusted for a command that runs after it (Trusts).
const Untrusted = "*@"

// SourcedPrefix opens the name DefinedNames gives a file the line
// sources: SourcedPrefix, the source's byte offset, ":" and its word as
// written. A guard reads the file (the names it defines are the line's
// then) or, when it cannot, puts an Untrusted place there.
const SourcedPrefix = "source:"

// UntrustedAt is the Untrusted name of a command at byte offset at.
func UntrustedAt(at int) string { return Untrusted + strconv.Itoa(at) }

// Sourced splits a SourcedPrefix name into its offset and word.
func Sourced(name string) (at int, word string, ok bool) {
	rest, ok := strings.CutPrefix(name, SourcedPrefix)
	if !ok {
		return 0, "", false
	}
	n, word, ok := strings.Cut(rest, ":")
	at, err := strconv.Atoi(n)
	return at, word, ok && err == nil
}

// Trusts is whether defined (DefinedNames) trusts name for a command at
// byte offset at (-1: a place the line does not show, read as its end): no
// function or alias of that name, and no Untrusted place or sourced file
// nobody read before it (T257: a name used before a source, or after a
// readable file that defines no such name, is trusted).
func Trusts(defined map[string]bool, name string, at int) bool {
	if defined[name] {
		return false
	}
	for n := range defined {
		var from int
		if rest, ok := strings.CutPrefix(n, Untrusted); ok {
			from, _ = strconv.Atoi(rest)
		} else if a, _, ok := Sourced(n); ok {
			from = a
		} else {
			continue
		}
		if at < 0 || at > from {
			return false
		}
	}
	return true
}

// DefinedNames are the names src defines as a function or an alias, which
// then run in place of the program of that name (T228 L8): also those an
// eval string or a shell's -c string it can read defines (T257), and the
// Untrusted places and sourced files (SourcedPrefix) where it may define
// names the reader cannot see. A line the parser cannot read defines none
// it can tell.
func DefinedNames(src string) map[string]bool {
	out := map[string]bool{}
	definedNames(src, out, 0, -1)
	return out
}

// definedNames adds src's names to out; base is the offset in the line of
// the command whose string src is, -1 for the line itself.
func definedNames(src string, out map[string]bool, depth, base int) {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(src), "")
	if err != nil || depth > 4 {
		return
	}
	syntax.Walk(f, func(n syntax.Node) bool {
		switch x := n.(type) {
		case *syntax.FuncDecl:
			if x.Name != nil {
				out[x.Name.Value] = true
			}
		case *syntax.CallExpr:
			if len(x.Args) == 0 {
				break
			}
			at := base
			if at < 0 {
				at = int(x.Pos().Offset())
			}
			first, _ := literal(x.Args[0])
			var words []string
			readable := true
			for _, a := range x.Args[1:] {
				v, ok := literal(a)
				readable = readable && ok
				words = append(words, v)
			}
			switch name := Base(first); {
			case name == "alias":
				out[UntrustedAt(at)] = true
				for _, a := range x.Args[1:] {
					if lit := a.Lit(); strings.Contains(lit, "=") {
						n, _, _ := strings.Cut(lit, "=")
						out[n] = true
					} else if v := (&reader{src: src}).word(a); strings.Contains(v, "=") {
						n, _, _ := strings.Cut(v, "=")
						out[n] = true
					}
				}
			case name == "eval":
				if !readable {
					out[UntrustedAt(at)] = true
				} else {
					definedNames(strings.Join(words, " "), out, depth+1, at)
				}
			case name == "source" || name == ".":
				w := ""
				if len(x.Args) > 1 {
					w = (&reader{src: src}).word(x.Args[1])
				}
				if w == "" || strings.HasPrefix(w, "-") || strings.Contains(w, "…") || StdinNames[w] || fdPath.MatchString(w) {
					out[UntrustedAt(at)] = true
				} else {
					out[SourcedPrefix+strconv.Itoa(at)+":"+w] = true
				}
			case name == "hash" && slices.ContainsFunc(words, func(w string) bool { return strings.HasPrefix(w, "-") && strings.Contains(w, "p") }),
				name == "enable" && slices.ContainsFunc(words, func(w string) bool { return strings.HasPrefix(w, "-") && strings.Contains(w, "f") }):
				out[UntrustedAt(at)] = true
			case Shells[name]:
				for i, w := range words {
					if shellC.MatchString(w) && i+1 < len(words) {
						definedNames(words[i+1], out, depth+1, at)
						break
					}
				}
			}
		}
		return true
	})
}
