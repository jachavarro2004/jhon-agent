package sh

import (
	"reflect"
	"strings"
	"testing"
)

// T228: zsh's precommand modifiers (-, nocorrect, noglob, builtin) and
// `repeat N` run the command after them: it is read as it is read bare.
func TestProgramsZshPrecommands(t *testing.T) {
	// Each line's program: its route, then its argv.
	for src, want := range map[string]string{
		`- git push`:                      "- | git push",
		`nocorrect git push`:              "nocorrect | git push",
		`noglob git push`:                 "noglob | git push",
		`repeat 2 git push`:               "repeat | git push",
		`repeat 2 { git push }`:           "repeat | git push",
		`builtin command git push`:        "builtin command | git push",
		`builtin eval 'git push'`:         "builtin eval | git push",
		`repeat 3 noglob - nocorrect git`: "repeat noglob - nocorrect | git",
		`noglob`:                          "| noglob",
		`repeat 2`:                        "| repeat 2",
		`noglob - -x`:                     "noglob - | -x",
	} {
		cmds, err := Parse(src)
		if err != nil || len(cmds) != 1 {
			t.Fatalf("Parse(%q): %v %v", src, cmds, err)
		}
		ps, err := CommandPrograms(cmds[0], nil, nil, Opts{})
		if err != nil || len(ps) != 1 {
			t.Fatalf("%q: programs %+v %v", src, ps, err)
		}
		if got := strings.TrimSpace(strings.Join(ps[0].Via, " ") + " | " + strings.Join(ps[0].Argv, " ")); got != want {
			t.Errorf("%q: route and argv %q, want %q", src, got, want)
		}
	}
}

// T228: a glob qualifier that runs code (zsh's e string, +command, or an
// o/O sort by either; or one an expansion may supply) is read as a command
// of its own, GlobCode and the glob; one that runs none is not.
func TestParseGlobQualifierCode(t *testing.T) {
	globs := func(src string) []string {
		t.Helper()
		cmds, err := Parse(src)
		if err != nil {
			t.Fatalf("Parse(%q): %v", src, err)
		}
		var out []string
		for _, c := range cmds {
			if len(c.Words) == 2 && c.Words[0] == "<zsh glob qualifier code>" {
				out = append(out, c.Words[1])
			}
		}
		return out
	}
	for src, want := range map[string]string{
		`ls *(e:'x y':)`: `*(e:'x y':)`, `ls *(e{x})`: `*(e{x})`, `ls *(+f)`: `*(+f)`, `ls *(.N+f_1)`: `*(.N+f_1)`,
		`ls *(oe:x:)`: `*(oe:x:)`, `ls *(O+f)`: `*(O+f)`, `ls *(#qe:x:)`: `*(#qe:x:)`, `ls *(^-e:x:)`: `*(^-e:x:)`,
		`ls @(e:x:)`: `@(e:x:)`, `ls !(test.test)`: `!(test.test)`, `ls *($q)`: `*($q)`, `ls *(e$q)`: `*(e$q)`,
		`ls *(Lk+1e:x:)`: `*(Lk+1e:x:)`, `ls *(f:u+x:e:y:)`: `*(f:u+x:e:y:)`, `ls *(P:-e:e:x:)`: `*(P:-e:e:x:)`,
		`for f in *(e:x:); do :; done`: `*(e:x:)`, `x=(*(e:x:))`: `*(e:x:)`, `echo > *(e:x:)`: `*(e:x:)`,
		`[[ a == *(e:x:) ]]`: `*(e:x:)`, `ls *('e':x:)`: `*('e':x:)`, `ls *(\e:x:)`: `*(\e:x:)`, `ls $~g`: `$~g`, `ls a/$^~g`: `a/$^~g`,
	} {
		if got := globs(src); !reflect.DeepEqual(got, []string{want}) {
			t.Errorf("Parse(%q) glob code %q, want %q", src, got, want)
		}
	}
	for _, src := range []string{`ls *.go`, `ls *(.)`, `ls *(om[1])`, `ls *(Lk+100)`, `ls *(mh-1)`, `ls *(f:u+x:)`, `ls *(f+755)`,
		`ls *(:e)`, `ls *(.:s/e/x/)`, `ls *(P:-e:)`, `ls *(u:e:)`, `ls *(g:e+:)`, `ls *([1,e])`, `ls *(+)`, `ls *(e)`, `ls *(e:x)`,
		`ls !(*.test.go)`, `ls !(node_modules)`, `ls !(a|e:x:)`, `ls +(e|x)`, `echo '*(e:x:)'`, `echo "*(e:x:)"`, `ls *(#i)x`, `ls \$~g`, `echo "$~g"`, `echo '$~g'`, `ls ~/x`, `echo $HOME~`} {
		if got := globs(src); got != nil {
			t.Errorf("Parse(%q) glob code %q, want none", src, got)
		}
	}
	// A substitution inside an extended glob runs: its commands are read.
	if got := words(t, `ls @($(git push)|x)`); !reflect.DeepEqual(got, [][]string{{"git", "push"}, {"ls", "@($(git push)|x)"}}) {
		t.Errorf("the substitution inside a glob: %q", got)
	}
}
