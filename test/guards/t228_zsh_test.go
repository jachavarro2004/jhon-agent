// T228 S1: zsh runs the command after `repeat N`, the `-` precommand,
// nocorrect and noglob (and builtin, command, exec) as it runs it bare, so
// safety-guard reads it exactly as it reads it bare. A glob qualifier that
// runs code (an e string, a +command, an o/O sort by either, or one an
// expansion may supply) runs it in the shell itself: the line is refused.
// A command substitution inside an extended glob runs as well, and is read
// as one anywhere else is. Ordinary globs, qualifiers that run no code, and
// the modifiers before a harmless command keep their verdicts.

package guards

import (
	"strings"
	"testing"
)

// t228Bare are bare commands with each verdict: refused, asked (outward), the owner's act, allowed.
var t228Bare = []string{"rm -rf ~", "rm -rf <OTHER>", "git push origin main", "jhon mode yolo", "ls src", "git status"}

// t228Before are the words zsh reads a command behind, each a form of S1 or the same class.
var t228Before = []string{
	"repeat 2 ", "repeat 1 ", "- ", "nocorrect ", "noglob ",
	"builtin ", "builtin command ", "builtin - ", "builtin exec ", "command - ", "exec - ",
	"noglob command ", "nocorrect nohup ", "- builtin ", "time noglob ", "repeat 3 noglob - nocorrect ",
}

// t228Code are lines whose glob qualifiers' code an expansion may supply: refused.
var t228Code = []string{"ls *($q)", "ls *(e$q)", "g='*(e:x:)'; ls $~g", "ls $^~g", "ls src/$=~g", "a=($~g)", "echo $~1"}

// t228CodeRemoves are lines whose glob qualifiers run a removal (L6: the
// code is read as the command line it is, and refused as the bare removal).
var t228CodeRemoves = []string{
	"ls *(e:'rm -rf ~':)", "ls *(e{rm -rf ~})", "ls *(.e:'rm -rf ~':)", "ls @(e:'rm -rf ~':)", "ls !(e:'rm -rf ~':)",
	"ls *(oe:'rm -rf ~':)", "ls *(#qe:'rm -rf ~':)", "ls src/**/*(e:'rm -rf ~':)", "for f in *(e:'rm -rf ~':); do echo $f; done",
	"x=(*(e:'rm -rf ~':))", "echo hi > *(e:'rm -rf ~':)", `sh -c "ls *(e:'rm -rf ~':)"`, "echo $(ls *(e:'rm -rf ~':))",
	"ls ${x:-*(e:'rm -rf ~':)}", "ls *(e-'rm -rf ~'-)",
}

// t228CodeHarmless are lines whose glob qualifiers run code that removes nothing: allowed, as the code is bare (L6).
var t228CodeHarmless = []string{
	"ls *(e:true:)", "ls *(+cleanup)", "ls *(.e:'true':)", "ls *(oe:'REPLY=x':)", "ls *(#qe:x:)", "ls !(test.test)",
	"ls !(tests)", "for f in *(e:x:); do echo $f; done", "eval 'ls *(e:x:)'", "zsh -c 'ls *(e:x:)'",
}

// t228SubstInGlob are lines that run a removal in a substitution inside an extended glob.
var t228SubstInGlob = []string{
	"ls @($(rm -rf ~)|x)", "ls !(`rm -rf ~`|y)", "ls +(${x:-$(rm -rf ~)}|y)", "ls ?($((1+$(rm -rf ~)))|y)",
	"case a in @($(rm -rf ~))) ;; esac", "ls *($(rm -rf ~))",
}

// t228Allowed are ordinary globs, qualifiers that run no code, and the modifiers before a harmless command.
var t228Allowed = []string{
	"ls *.go", "ls src/**/*(.)", "ls *(om[1])", "ls -d *(/)", "ls *(N)", "ls *(Lk+100)", "ls *(mh-1)", "ls *(f:u+x:)",
	"ls *(:e)", "ls *(.:t:r)", "ls *(P:-e:)", "ls *(u:root:)", "ls *(Y1)", "ls *([1,3])", "ls *(%b)",
	"ls !(*.test.go)", "ls !(node_modules|dist)", "ls @(a|b).go", "ls +(e|x)", "ls !(node_modules)",
	"echo '*(e:x:)'", `echo "*(e:x:)"`, `echo \*\(e:x:\)`, "case x in @(a|b)) echo y;; esac",
	"noglob ls *", "noglob git status", `ls \$~g`, `echo "$~g"`, "echo '$~g'", "nocorrect ls", "- ls", "repeat 3 echo hi", "builtin echo hi",
}

// TestT228ModifiersReadAsBare: each modifier before a bare command gets the bare command's verdict, word for word.
func TestT228ModifiersReadAsBare(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, b := range t228Bare {
		bare := s.expand(b)
		for _, o := range []sgOpts{{}, top()} {
			// Each form, and the bare command it is read as (under sh -c, the route it names).
			forms := map[string]string{`sh -c "noglob ` + bare + `"`: `sh -c "` + bare + `"`, "repeat 2 { " + bare + " }": bare, "eval - " + bare: bare}
			for _, p := range t228Before {
				forms[p+bare] = bare
			}
			for c, b := range forms {
				got, want := s.bash(t, c, o), s.bash(t, b, o)
				if got.Code != want.Code || got.Stderr != want.Stderr || got.Stdout != want.Stdout {
					t.Errorf("top %v: %q gives %d %q %q; %q, %d %q %q", o.top, c, got.Code, got.Stderr, got.Stdout, b, want.Code, want.Stderr, want.Stdout)
				}
			}
		}
	}
}

// TestT228QualifierCodeIsRefused: a line whose glob qualifier runs code is refused, at the top level too.
func TestT228QualifierCodeIsRefused(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range t228Code {
		for _, o := range []sgOpts{{}, top()} {
			r := s.bash(t, c, o)
			code(t, r, 2, c)
			if !strings.Contains(r.Stderr, "safety-guard: refused: the glob qualifier ") || !strings.Contains(r.Stderr, ": zsh runs its code (an e string or a +command) in the shell, which the guard does not read") {
				t.Errorf("top %v: %q: the refusal names no glob qualifier: %s", o.top, c, r.Stderr)
			}
			if r.Stdout != "" {
				t.Errorf("top %v: %q: stdout %q", o.top, c, r.Stdout)
			}
		}
	}
	r := s.bash(t, "ls *($q)", sgOpts{})
	if !strings.Contains(r.Stderr, "safety-guard: refused: the glob qualifier *($q): zsh runs its code") {
		t.Errorf("the refusal does not quote the glob: %s", r.Stderr)
	}
	bare := s.bash(t, "rm -rf ~", sgOpts{}).Stderr
	for _, c := range t228CodeRemoves {
		for _, o := range []sgOpts{{}, top()} {
			r := s.bash(t, c, o)
			code(t, r, 2, c)
			if !o.top && r.Stderr != bare {
				t.Errorf("%q: %q, want the bare removal's %q", c, r.Stderr, bare)
			}
		}
	}
	for _, c := range t228CodeHarmless {
		allowed(t, s, c)
	}
}

// TestT228SubstitutionInsideAGlob: a removal in a substitution inside an extended glob is refused as the bare one is.
func TestT228SubstitutionInsideAGlob(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	bare := s.bash(t, "rm -rf ~", sgOpts{}).Stderr
	why := strings.TrimSuffix(strings.SplitN(bare, ". ", 2)[0], ".")
	if !strings.HasPrefix(why, "safety-guard: refused: rm -r ~: it reaches the home directory") {
		t.Fatalf("the bare removal: %q", bare)
	}
	for _, c := range t228SubstInGlob {
		for _, o := range []sgOpts{{}, top()} {
			r := s.bash(t, c, o)
			code(t, r, 2, c)
			if !strings.Contains(r.Stderr, "rm -r ~: it reaches the home directory") {
				t.Errorf("top %v: %q: the removal is not named: %s", o.top, c, r.Stderr)
			}
		}
	}
}

// TestT228OrdinaryGlobsStayAllowed: globs and qualifiers that run no code, and the modifiers before a harmless command.
func TestT228OrdinaryGlobsStayAllowed(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range t228Allowed {
		for _, o := range []sgOpts{{}, top()} {
			r := s.bash(t, c, o)
			code(t, r, 0, c)
			if r.Stdout != "" || r.Stderr != "" {
				t.Errorf("top %v: %q: output %q %q", o.top, c, r.Stdout, r.Stderr)
			}
		}
	}
}
