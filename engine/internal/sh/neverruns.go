package sh

import (
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

// DefinedNames are the names src defines as a function or an alias, which
// then run in place of the program of that name (T228 L8); a line the
// parser cannot read defines none it can tell.
func DefinedNames(src string) map[string]bool {
	out := map[string]bool{}
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(src), "")
	if err != nil {
		return out
	}
	syntax.Walk(f, func(n syntax.Node) bool {
		switch x := n.(type) {
		case *syntax.FuncDecl:
			if x.Name != nil {
				out[x.Name.Value] = true
			}
		case *syntax.CallExpr:
			if len(x.Args) > 0 && x.Args[0].Lit() == "alias" {
				for _, a := range x.Args[1:] {
					if lit := a.Lit(); strings.Contains(lit, "=") {
						n, _, _ := strings.Cut(lit, "=")
						out[n] = true
					} else if v := (&reader{src: src}).word(a); strings.Contains(v, "=") {
						n, _, _ := strings.Cut(v, "=")
						out[n] = true
					}
				}
			}
		}
		return true
	})
	return out
}
