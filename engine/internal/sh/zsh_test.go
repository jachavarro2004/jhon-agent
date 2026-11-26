package sh

import (
	"reflect"
	"strings"
	"testing"
)

// T228: zsh's precommand modifiers (-, nocorrect, noglob, builtin) and
// `repeat N` run the command after them: it is read as it is read bare.
// L5: a `--` after them, and a brace group or brace list, are read too.
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
		`noglob -- git push`:              "noglob | git push",
		`repeat 2 -- git push`:            "repeat | git push",
		`repeat 2 {git push}`:             "repeat | git push",
		`{git push}`:                      "| git push",
		`{git,push} origin`:               "| git push origin",
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

// T228, L6: the code a glob qualifier runs (zsh's e string, +command, or
// an o/O sort by either) is read as the command line it is, before the
// command that holds the glob; a qualifier an expansion may supply is a
// command of its own, GlobCode and the glob; one that runs none adds none.
func TestParseGlobQualifierCode(t *testing.T) {
	read := func(src string) (code, globs []string) {
		t.Helper()
		cmds, err := Parse(src)
		if err != nil {
			t.Fatalf("Parse(%q): %v", src, err)
		}
		for _, c := range cmds[:len(cmds)-1] {
			if len(c.Words) == 2 && c.Words[0] == GlobCode {
				globs = append(globs, c.Words[1])
			} else {
				code = append(code, strings.Join(c.Words, " "))
			}
		}
		return code, globs
	}
	for src, want := range map[string][]string{
		`ls *(e:'x y':)`: {"x y"}, `ls *(e{x})`: {"x"}, `ls *(+f)`: {"f"}, `ls *(.N+f_1)`: {"f_1"}, `ls *(oe:x:)`: {"x"},
		`ls *(O+f)`: {"f"}, `ls *(#qe:x:)`: {"x"}, `ls *(^-e:x:)`: {"x"}, `ls @(e:x:)`: {"x"}, `ls !(test.test)`: {"t.te"},
		`ls !(tests)`: {"t"}, `ls *(Lk+1e:x:)`: {"x"}, `ls *(f:u+x:e:y:)`: {"y"}, `ls *(P:-e:e:x:)`: {"x"},
		`ls *('e':x:)`: {"x"}, `ls *(\e:x:)`: {"x"}, `ls *(e:a:+b)`: {"a", "b"}, `ls ${x:-*(e:'x y':)}`: {"x y"},
		`ls ${x+*(+f)}`: {"f"},
	} {
		if code, globs := read(src); !reflect.DeepEqual(code, want) || globs != nil {
			t.Errorf("Parse(%q): code %q and globs %q, want code %q", src, code, globs, want)
		}
	}
	for src, want := range map[string]string{
		`ls *($q)`: `*($q)`, `ls *(e$q)`: `*(e$q)`, `ls $~g`: `$~g`, `ls a/$^~g`: `a/$^~g`, `a=($~g)`: `$~g`, `a+=(x $~g)`: `$~g`,
		`for i in $~g; do :; done`: `$~g`, `ls ${x:-$~g}`: `$~g`, `echo $~~g`: `$~~g`, `echo $~1`: `$~1`, `echo $=^~g`: `$=^~g`,
	} {
		cmds, err := Parse(src)
		if err != nil {
			t.Fatalf("Parse(%q): %v", src, err)
		}
		var globs []string
		for _, c := range cmds {
			if len(c.Words) == 2 && c.Words[0] == GlobCode {
				globs = append(globs, c.Words[1])
			}
		}
		if !reflect.DeepEqual(globs, []string{want}) {
			t.Errorf("Parse(%q) glob code %q, want %q", src, globs, want)
		}
	}
	for _, src := range []string{`ls *.go`, `ls *(.)`, `ls *(om[1])`, `ls *(Lk+100)`, `ls *(mh-1)`, `ls *(f:u+x:)`, `ls *(f+755)`,
		`ls *(:e)`, `ls *(.:s/e/x/)`, `ls *(P:-e:)`, `ls *(u:e:)`, `ls *(g:e+:)`, `ls *([1,e])`, `ls *(+)`, `ls *(e)`, `ls *(e:x)`,
		`ls !(*.test.go)`, `ls !(node_modules)`, `ls !(a|e:x:)`, `ls +(e|x)`, `echo '*(e:x:)'`, `echo "*(e:x:)"`, `ls *(#i)x`,
		`ls \$~g`, `echo "$~g"`, `echo '$~g'`, `ls ~/x`, `echo $HOME~`, `echo $~`, `ls !(dist)`} {
		if cmds, err := Parse(src); err != nil || len(cmds) != 1 {
			t.Errorf("Parse(%q) = %v, %v; want the command alone", src, cmds, err)
		}
	}
	// A substitution inside an extended glob runs: its commands are read.
	if got := words(t, `ls @($(git push)|x)`); !reflect.DeepEqual(got, [][]string{{"git", "push"}, {"ls", "@($(git push)|x)"}}) {
		t.Errorf("the substitution inside a glob: %q", got)
	}
}
