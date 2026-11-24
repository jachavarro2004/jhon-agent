package guard

import (
	"strings"
	"testing"
)

// T224 S1: an uppercase prefix assignment to a program that is no builtin is
// that program's environment; scratchVars reads the line, freshVars does not.
func TestLineFactsUppercasePrefixOnAProgram(t *testing.T) {
	for line, want := range map[string]bool{
		"d=$(mktemp -d) && APP_DB=$d/t.db node x": true, "d=$(mktemp -d) && A=1 B= python3 x | head -1": true,
		"d=$(mktemp -d) && TMPDIR=/ : x": false, "d=$(mktemp -d) && CDPATH=/ cd x": false, "d=$(mktemp -d) && X=1 true": false,
		"d=$(mktemp -d) && PATH=/x export y": false, "d=$(mktemp -d) && X=1 $p y": false, "d=$(mktemp -d) && X=1": false,
		"d=$(mktemp -d) && d=/ node x": false, "d=$(mktemp -d) && path=/x node y": false,
	} {
		f := lineFacts(line)
		if got := f != nil && f.resolves("d"); got != want {
			t.Errorf("%q: resolves %v, want %v", line, got, want)
		}
	}
	if v := freshVars("d=$(mktemp -d) && APP_DB=$d/t.db node x > $d/out", "/work/p"); v != nil {
		t.Errorf("freshVars reads a prefixed line: %v", v)
	}
	if v := freshVars("d=$(mktemp -d) && node x > $d/out", "/work/p"); v["d"] == "" {
		t.Errorf("freshVars no longer reads a plain line: %v", v)
	}
}

// T224: `_` is the shell's last argument of the command before, never a
// tracked mktemp variable.
func TestUnderscoreIsNeverTracked(t *testing.T) {
	for _, line := range []string{"_=$(mktemp -d); echo ~; rm -rf $_", "_=$(mktemp); echo ~; echo x > $_", "_=$(mktemp -d) && rm -rf $_"} {
		if f := lineFacts(line); f != nil && f.resolves("_") {
			t.Errorf("%q: _ resolves", line)
		}
		if v, ok := freshVars(line, "/work/p")["_"]; ok {
			t.Errorf("%q: freshVars holds _ = %q", line, v)
		}
		if v, ok := scratchVars(line, &Ctx{Cwd: "/work/p"})["_"]; ok {
			t.Errorf("%q: scratchVars holds _ = %q", line, v)
		}
	}
}

// T224 L4 B2, L5: an assignment after `cd … &&` is never proven run before
// an rm outside its chain; that refusal names the safe form, no other does.
func TestScratchAfterCdAndItsHint(t *testing.T) {
	ctx := &Ctx{Cwd: t.TempDir()}
	for _, line := range []string{"cd . && d=$(mktemp -d) && true; rm -rf $d", "cd / && d=$(mktemp -d); rm -rf $d", "cd +1 && d=$(mktemp -d); rm -rf \"$d\"/*"} {
		sv := scratchVars(line, ctx)
		if _, ok := sv["d"]; ok {
			t.Errorf("%q: d resolves", line)
		}
		if h := scratchHint(line, sv, ctx); h != scratchSafeForm {
			t.Errorf("%q: hint %q", line, h)
		}
	}
	for _, line := range []string{"d=$(mktemp -d); cd / && true; rm -rf $d", "cd / && d=$(mktemp -d) && rm -rf $d", "d=$(mktemp -d); d=/; rm -rf $d",
		"x && d=$(pwd); rm -rf $d", "cd / && d=$(mktemp -d); echo $d"} {
		if h := scratchHint(line, scratchVars(line, ctx), ctx); h != "" {
			t.Errorf("%q: hint %q", line, h)
		}
	}
}

// T224 L8: zsh runs a glob qualifier's code, `repeat`, `-` and `private`
// in the shell itself, so any of them may assign the variable first.
func TestZshCurrentShellGadgetsUnproveD(t *testing.T) {
	ctx := &Ctx{Cwd: t.TempDir()}
	for _, line := range []string{"x=d; d=$(mktemp -d) && : /*(e:'eval $x=$HOME':) && rm -rf $d", "d=$(mktemp -d) && echo *(+f) && rm -rf $d",
		"x=d; d=$(mktemp -d) && repeat 1 eval $x=$HOME && rm -rf $d", "x=d; d=$(mktemp -d) && - eval $x=$HOME && rm -rf $d",
		"x=d; d=$(mktemp -d) && private $x=$HOME && rm -rf $d"} {
		if v, ok := scratchVars(line, ctx)["d"]; ok {
			t.Errorf("%q: d resolves to %q", line, v)
		}
	}
}

// T224 S1: GNU mktemp's --tmpdir takes its directory only after `=`.
func TestMktempDirBareTmpdir(t *testing.T) {
	t.Setenv("TMPDIR", "/tmp")
	ctx := &Ctx{Cwd: "/work/p"}
	for args, want := range map[string]string{
		" -d --tmpdir": "/tmp/tmp.XXXXXXXXXX", " -d --tmpdir x.XXXXXX": "/tmp/x.XXXXXX", " -d --tmpdir=/tmp x.XXXXXX": "/tmp/x.XXXXXX",
		" -d -p /tmp x.XXXXXX": "/tmp/x.XXXXXX", " -d --tmpdir /tmp/x.XXXXXX": "", " -d --tmpdir -p /tmp": "", " -d --tmpdir ../x.XXXXXX": "",
	} {
		got, ok := mktempDir(args, ctx)
		if !ok {
			got = ""
		}
		if got != want {
			t.Errorf("mktemp%s: %q, want %q", args, got, want)
		}
	}
}

// T224 L6 B4: zsh's assigning builtins are no external programs, and a word
// that is the name glued to an option cluster or with a subscript may assign it.
func TestScratchNameGluedOrAssignedByZshBuiltins(t *testing.T) {
	ctx := &Ctx{Cwd: t.TempDir()}
	for _, line := range []string{"d=$(mktemp -d) && print -vd / && rm -rf $d", "d=$(mktemp -d) && strftime -sd / 0 && rm -rf $d",
		"d=$(mktemp -d) && zmodload zsh/datetime && rm -rf $d", "d=$(mktemp -d) && tool -nvd / && rm -rf $d", "d=$(mktemp -d) && tool -A d && rm -rf $d",
		"d=$(mktemp -d) && tool 'd[1]' / && rm -rf $d", "d=$(mktemp -d) && zstyle -s x y d && rm -rf $d", "d=$(mktemp -d); sysread d; rm -rf $d"} {
		if v, ok := scratchVars(line, ctx)["d"]; ok {
			t.Errorf("%q: d resolves to %q", line, v)
		}
	}
	for _, line := range []string{"d=$(mktemp -d) && rm -rf $d", "d=$(mktemp -d -t x.XXXX) && tool -x dd && rm -rf $d", "dir=$(mktemp -d) && ls -d $dir && rm -rf $dir"} {
		if _, ok := scratchVars(line, ctx)[strings.SplitN(line, "=", 2)[0]]; !ok {
			t.Errorf("%q: the folder no longer resolves", line)
		}
	}
	if lineFacts("d=$(mktemp -d) && print x") != nil {
		t.Error("print is read as an external program")
	}
}
