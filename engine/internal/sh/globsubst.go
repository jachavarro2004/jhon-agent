package sh

import (
	"path"
	"strings"
)

// GlobSubstOn is the words of a program that turn on zsh's GLOB_SUBST, or
// "": setopt globsubst (any case, underscores or dashes; -m with a pattern
// that matches it), unsetopt noglobsubst, set -o globsubst or +o
// noglobsubst, emulate sh or ksh (which turn it on) or with -o globsubst,
// and zsh started with any of these. Turning it on is refused whatever
// follows: no ordinary line does it, and an expansion after it may come
// from anywhere.
func GlobSubstOn(name string, args []string) string {
	on := func(o string) bool { return OptionName(o) == "globsubst" }
	off := func(o string) bool { return OptionName(o) == "noglobsubst" }
	// pair reads -o NAME, +o NAME and -oNAME.
	pair := func(i int) (string, bool) {
		a := args[i]
		switch {
		case a == "-o" && i+1 < len(args) && on(args[i+1]), a == "+o" && i+1 < len(args) && off(args[i+1]):
			return name + " " + a + " " + args[i+1], true
		case strings.HasPrefix(a, "-o") && on(a[2:]), strings.HasPrefix(a, "+o") && off(a[2:]):
			return name + " " + a, true
		}
		return "", false
	}
	switch name {
	case "setopt", "unsetopt":
		want := on
		if name == "unsetopt" {
			want = off
		}
		for i, a := range args {
			if a == "-m" {
				for _, p := range args[i+1:] {
					if ok, _ := path.Match(OptionName(p), "globsubst"); ok && name == "setopt" {
						return name + " -m " + p
					}
				}
				return ""
			}
			if want(a) {
				return name + " " + a
			}
		}
	case "set":
		for i := range args {
			if l, ok := pair(i); ok {
				return l
			}
		}
	case "emulate":
		for i := 0; i < len(args); i++ {
			a := args[i]
			if l, ok := pair(i); ok {
				return l
			}
			switch {
			case a == "-o" || a == "+o":
				i++
			case a == "sh" || a == "ksh":
				return "emulate " + a
			case len(a) > 1 && a[0] == '-' && strings.Contains(a, "c"):
				// -c's string is code (read on its own), not an option.
				return ""
			}
		}
	case "zsh":
		for i := 0; i < len(args); i++ {
			a := args[i]
			if l, ok := pair(i); ok {
				return l
			}
			switch {
			case a == "-o" || a == "+o":
				i++
			case strings.HasPrefix(a, "--"):
				n, v, eq := strings.Cut(a[2:], "=")
				label := "zsh " + a
				if n == "emulate" && !eq {
					i++
					v = at(args, i)
					label += " " + v
				}
				if on(n) || n == "emulate" && (v == "sh" || v == "ksh") {
					return label
				}
			case !strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "+"), strings.HasPrefix(a, "-") && strings.Contains(a, "c"):
				// The options end at -c's string or the script.
				return ""
			}
		}
	}
	return ""
}
