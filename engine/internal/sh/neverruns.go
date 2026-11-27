package sh

import (
	"slices"
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

// Untrusted is the name DefinedNames gives for a line on which no
// program name can be trusted to be the program of that name.
const Untrusted = "*"

// SourcedPrefix opens the name DefinedNames gives a file the line sources
// by a literal path: a guard that reads the file reads its names there.
const SourcedPrefix = "source:"

// Untrusting is whether defined (DefinedNames) trusts no name: Untrusted,
// or a sourced file nobody read.
func Untrusting(defined map[string]bool) bool {
	for n := range defined {
		if n == Untrusted || strings.HasPrefix(n, SourcedPrefix) {
			return true
		}
	}
	return false
}

// DefinedNames are the names src defines as a function or an alias, which
// then run in place of the program of that name (T228 L8): also those an
// eval string or a shell's -c string it can read defines (T257). A line
// that defines names the reader cannot see (eval of a string it cannot
// read, a file sourced by no literal path, hash -p, alias, enable -f) has
// Untrusted among them: no name is trusted there; a file sourced by a
// literal path is SourcedPrefix and its path. A line the parser cannot
// read defines none it can tell.
func DefinedNames(src string) map[string]bool {
	out := map[string]bool{}
	definedNames(src, out, 0)
	return out
}

func definedNames(src string, out map[string]bool, depth int) {
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
				out[Untrusted] = true
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
					out[Untrusted] = true
				} else {
					definedNames(strings.Join(words, " "), out, depth+1)
				}
			case name == "source" || name == ".":
				if f := at(words, 0); readable && f != "" && !strings.HasPrefix(f, "-") && !StdinNames[f] && !fdPath.MatchString(f) {
					out[SourcedPrefix+f] = true
				} else {
					out[Untrusted] = true
				}
			case name == "hash" && slices.ContainsFunc(words, func(w string) bool { return strings.HasPrefix(w, "-") && strings.Contains(w, "p") }),
				name == "enable" && slices.ContainsFunc(words, func(w string) bool { return strings.HasPrefix(w, "-") && strings.Contains(w, "f") }):
				out[Untrusted] = true
			case Shells[name]:
				for i, w := range words {
					if shellC.MatchString(w) && i+1 < len(words) {
						definedNames(words[i+1], out, depth+1)
						break
					}
				}
			}
		}
		return true
	})
}
