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

// t228Code are lines whose glob qualifiers run code.
var t228Code = []string{
	"ls *(e:'rm -rf ~':)", "ls *(e:true:)", "ls *(e{rm -rf ~})", "ls *(+cleanup)", "ls *(.e:'true':)", "ls *(N+f)",
	"ls *(oe:'REPLY=x':)", "ls *(O+f)", "ls *(#qe:x:)", "ls *(^e:x:)", "ls *(-e:x:)", "ls @(e:x:)", "ls !(e:x:)",
	"ls +(e:x:)", "ls ?(e:x:)", "ls src/**/*(e:x:)", "ls !(test.test)", "ls *($q)", "ls *(e$q)",
	"for f in *(e:x:); do echo $f; done", "x=(*(e:x:))", "echo hi > *(e:x:)", `sh -c "ls *(e:x:)"`,
	"eval 'ls *(e:x:)'", "echo $(ls *(e:x:))", "zsh -c 'ls *(e:x:)'", "x=d; : /*(e:'eval $x=$HOME':)",
	"noglob echo $(ls *(+f))", "g='*(e:x:)'; ls $~g", "ls $^~g", "ls src/$=~g",
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
	r := s.bash(t, "ls *(e:'rm -rf ~':)", sgOpts{})
	if !strings.Contains(r.Stderr, "safety-guard: refused: the glob qualifier *(e:'rm -rf ~':): zsh runs its code") {
		t.Errorf("the refusal does not quote the glob: %s", r.Stderr)
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
