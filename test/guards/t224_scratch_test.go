// T224 S1, S3: a variable the line assigns from `$(mktemp -d)` names a fresh
// scratch folder, so removing it is no false alarm. The owner's session line
// (sanitized) ran the assignment after a `cd … &&` and gave the folder to a
// program through an uppercase prefix assignment (`APP_DB=$d/t.db node …`);
// the rm came after `;`. A cd that fails at run time skips the assignment, so
// that line stays refused, with a refusal that names the safe form (L4, L5);
// the same line with the folder assigned first is allowed. Every
// still-refused form S1 lists keeps refusing.

package guards

import (
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// t224OwnersLine is the refused line of the owner's session (2026-11-23),
// names made neutral.
const t224OwnersLine = `cd <P>/apps/cli && d=$(mktemp -d) && APP_DB=$d/t.db API_KEY= node --import tsx src/main.ts items add a -k b >/dev/null && APP_DB=$d/t.db API_KEY= node --import tsx src/main.ts items add c -a b >/dev/null && APP_DB=$d/t.db API_KEY= node --import tsx src/main.ts items | head -6; rm -rf $d`

// t224OwnersSafeLine is the owner's line in the safe form the refusal names:
// the scratch folder assigned first, on its own.
const t224OwnersSafeLine = `d=$(mktemp -d); cd <P>/apps/cli && APP_DB=$d/t.db API_KEY= node --import tsx src/main.ts items add a -k b >/dev/null && APP_DB=$d/t.db API_KEY= node --import tsx src/main.ts items add c -a b >/dev/null && APP_DB=$d/t.db API_KEY= node --import tsx src/main.ts items | head -6; rm -rf "$d"`

// t224SafeForm is the sentence the refusal of an assignment after `cd … &&` ends its reason with.
const t224SafeForm = `assign the scratch folder first, on its own: d=$(mktemp -d); cd <dir> && …; rm -rf "$d"`

var t224Allowed = []string{
	t224OwnersSafeLine,
	"d=$(mktemp -d); cd <P>/apps/cli && cat $d/x; rm -rf $d",
	// An uppercase prefix assignment to an external program.
	"d=$(mktemp -d) && APP_DB=$d/t.db python3 cli.py && rm -rf $d",
	// The spec's forms.
	`d=$(mktemp -d) && rm -rf "$d"`, "d=$(mktemp -d) && rm -rf $d", "d=$(mktemp -d -t t224.XXXXXX) && rm -rf $d",
	`d=$(mktemp -d) && python3 cli.py > "$d/out" && cat "$d/out" && rm -rf "$d"`, "d=$(mktemp -d -p /tmp) && rm -rf $d",
	"d=$(mktemp -d --tmpdir) && rm -rf $d", `d=$(mktemp -d --tmpdir t224.XXXXXX) && rm -rf "$d"`,
	"d=$(mktemp -d --tmpdir=/tmp) && rm -rf $d",
	"d=$(mktemp -d) && python3 cli.py convert data/supplier.csv $d/out && cat $d/out; rm -rf $d",
}

var t224Refused = []string{
	// The owner's line and the cd forms: a cd that fails skips the assignment (L4 B2).
	t224OwnersLine, "cd <P>/apps/cli && d=$(mktemp -d) && cat $d/x; rm -rf $d",
	"cd <P> && cd apps/cli && d=$(mktemp -d) && node src/main.ts $d/out; rm -rf \"$d\"",
	"chmod 0 <P>/locked; cd <P>/locked && d=$(mktemp -d); rm -rf \"$d\"/*",
	"rmdir <P>/locked; cd <P>/locked && XDG_CONFIG_HOME=$(mktemp -d); rm -rf $XDG_CONFIG_HOME",
	"mv <P>/locked <P>/gone; cd <P>/locked && d=$(mktemp -d); rm -rf $d", "cd +1 && d=$(mktemp -d); rm -rf $d",
	"cd =ls && d=$(mktemp -d); rm -rf $d", `cd <P>/apps/cli && GOPATH=$(mktemp -d) && node x; rm -rf "$GOPATH"/*`,
	"chmod 0 locked; cd locked && XDG_CONFIG_HOME=$(mktemp -d); rm -rf $XDG_CONFIG_HOME",
	"cd +1 && XDG_CONFIG_HOME=$(mktemp -d); rm -rf $XDG_CONFIG_HOME",
	// `"$d"/*` is refused everywhere (L8, L9): a failed mktemp leaves d empty, and "$d"/* is /* (L4 B1);
	// a zsh builtin may reassign d first (L6 B4).
	`d=$(mktemp -d) && rm -rf "$d"/*`, `d=$(mktemp -d) && python3 cli.py > "$d/out" && cat "$d/out" && rm -rf "$d"/*`,
	`d=$(mktemp -d -t t224.XXXXXX) && rm -rf "$d"/*`, `d=$(mktemp -d) && rm -rf "${d}"/*`,
	`d=$(mktemp -d); python3 cli.py > "$d/out"; rm -rf "$d"/*`, `d=$(mktemp -d -t x); rm -rf "$d"/*`,
	`d=$(mktemp -d /tmp/aXX); rm -rf "$d"/*`, `d=$(mktemp -d -p /tmp/x); rm -rf "$d"/*`,
	`d=$(mktemp -d /tmp/nonexistent-t224/XXXXXX); rm -rf "$d"/*`, `d=$(mktemp -d) || true; rm -rf "$d"/*`,
	`d=$(mktemp -d -t x) || rm -rf "$d"/*`, `d=$(mktemp -d -t x) && true || rm -rf "$d"/*`,
	`d=$(mktemp -d) && cd <P> && rm -rf "$d"/*`, `d=$(mktemp -d) | rm -rf "$d"/*`, `d=$(mktemp -d) & rm -rf "$d"/*`,
	// Reassigned.
	"d=$(mktemp -d); d=/; rm -rf $d", `d=$(mktemp -d); d=/; rm -rf "$d"/*`, "cd <P> && d=$(mktemp -d) && d=/; rm -rf $d",
	"d=$(mktemp -d) && d=/ node x; rm -rf $d", "cd <P> && d=$(mktemp -d) && APP=1 d=/ node x; rm -rf $d",
	// Assigned from anything else.
	"d=$(pwd); rm -rf $d", `d=$(pwd); rm -rf "$d"/*`, `d=/; rm -rf "$d"`, `d=$(echo /) && rm -rf "$d"/*`, "d=$(mktemp); rm -rf $d/..",
	"cd <P> && d=$(pwd) && true; rm -rf $d",
	// Used before the assignment.
	"rm -rf $d; d=$(mktemp -d)", `rm -rf "$d"/*; d=$(mktemp -d)`, "rm -rf $d; cd <P> && d=$(mktemp -d)",
	// The assignment may not run before the rm: a chain member that can fail, `||`, a cd that cannot be proven.
	"python3 cli.py && d=$(mktemp -d) && true; rm -rf $d", "cd <P>/missing && d=$(mktemp -d) && true; rm -rf $d",
	"cd $HOME && d=$(mktemp -d) && true; rm -rf $d", "cd - && d=$(mktemp -d) && true; rm -rf $d", "cd && d=$(mktemp -d); rm -rf $d/x",
	"cd <P> || d=$(mktemp -d); rm -rf $d", "true || cd <P> && d=$(mktemp -d); rm -rf $d", "cd <P> | d=$(mktemp -d); rm -rf $d",
	`cd "<P>" && d=$(mktemp -d) && true; rm -rf $d`, "cd <P> && d=$(mktemp -d -p .) && true; rm -rf $d",
	// `$d/..`, or extra path parts that leave it.
	"d=$(mktemp -d) && rm -rf $d/..", `d=$(mktemp -d) && rm -rf "$d"/..`, "cd <P> && d=$(mktemp -d) && true; rm -rf $d/..",
	"d=$(mktemp -d) && rm -rf $d/../..", `d=$(mktemp -d) && rm -rf "$d"/*/..`, `d=$(mktemp -d) && rm -rf "$d"/../*`,
	`d=$(mktemp -d) && rm -rf "$d"/x/../..`, `d=$(mktemp -d) && rm -rf "$d"/.*`, `d=$(mktemp -d) && rm -rf "$d"/*/`,
	`d=$(mktemp -d) && rm -rf "$d/"*`, `d=$(mktemp -d) && rm -rf "$d"/**`,
	// The folder replaced by a link before the glob.
	`d=$(mktemp -d) && rm -r $d && ln -s $HOME/proj $d && rm -rf "$d"/*`, `d=$(mktemp -d) && ln -s $HOME $d/h && rm -rf "$d"/*/x`,
	// `${d:-/}`.
	"d=$(mktemp -d) && rm -rf ${d:-/}", `d=$(mktemp -d) && rm -rf "${d:-/}"/*`, "cd <P> && d=$(mktemp -d) && true; rm -rf ${d:-/}",
	// A different variable.
	"d=$(mktemp -d) && rm -rf $e", `d=$(mktemp -d) && rm -rf "$e"/*`, "cd <P> && d=$(mktemp -d) && true; rm -rf $e",
	// Another target beside it.
	"d=$(mktemp -d) && rm -rf $d $HOME", `d=$(mktemp -d) && rm -rf "$d"/* $HOME`, "cd <P> && d=$(mktemp -d) && true; rm -rf $d $HOME",
	// `$_` is the shell's last argument of the command before: `echo ~` makes it the home folder (T224, found on main).
	"_=$(mktemp -d); echo ~; rm -rf $_", `_=$(mktemp -d); echo ~; rm -rf "$_"`, "_=$(mktemp -d); echo ~; rm -rf ${_}",
	"_=$(mktemp -d) && rm -rf $_", `_=$(mktemp -d) && rm -rf "$_"/*`, "cd <P> && _=$(mktemp -d) && true; rm -rf $_",
	// zsh builtins that assign the name glued to an option or as an operand (L6 B4).
	`d=$(mktemp -d) && print -vd / && rm -rf "$d"/*`, `d=$(mktemp -d) && print -nvd ~ && rm -rf "$d"/*`,
	`d=$(mktemp -d) && zmodload zsh/datetime && strftime -sd / 0 && rm -rf "$d"/*`, "d=$(mktemp -d) && print -vd ~ && rm -rf $d",
	"d=$(mktemp -d); print -v d ~; rm -rf $d", "d=$(mktemp -d); zparseopts -A d x; rm -rf $d", "d=$(mktemp -d); vared d; rm -rf $d",
	"d=$(mktemp -d); getln d; rm -rf $d", "d=$(mktemp -d); sysread d; rm -rf $d",
	// zsh's current-shell gadgets: a glob qualifier's code, repeat, `-` and private run in the shell itself (L8, L9).
	"x=d; d=$(mktemp -d) && : /*(e:'eval $x=$HOME':) && rm -rf $d", "x=d; d=$(mktemp -d) && repeat 1 typeset -g $x=$HOME && rm -rf $d",
	"x=d; d=$(mktemp -d) && - eval $x=$HOME && rm -rf $d", `x=d; d=$(mktemp -d); private $x=$HOME; rm -rf "$d"`,
	`x=d; d=$(mktemp -d) && : /*(e:'eval $x=$HOME':) && rm -rf "$d"/*`, `x=d; d=$(mktemp -d) && repeat 1 typeset -g $x=$HOME && rm -rf "$d"/*`,
	`x=d; d=$(mktemp -d) && - eval $x=$HOME && rm -rf "$d"/*`, `x=d; d=$(mktemp -d); private $x=$HOME; rm -rf "$d"/*`,
	// An uppercase prefix assignment to a builtin, or one that changes how the line splits.
	"d=$(mktemp -d) && CDPATH=/ cd x && rm -rf $d", "d=$(mktemp -d) && TMPDIR=/ : && rm -rf $d", "d=$(mktemp -d) && IFS=/ node x && rm -rf $d",
}

func t224Tree(t *testing.T) sgEnv {
	t.Helper()
	s := newSG(t)
	support.Write(t, s.p, map[string]string{"apps/cli/src/main.ts": "x\n", "cli.py": "print(1)\n", "data/supplier.csv": "a,b\n",
		"locked/.keep": "", "+1/.keep": "", "=ls/.keep": ""})
	return s
}

func TestT224ScratchFolderRemovalIsAllowed(t *testing.T) {
	t.Parallel()
	s := t224Tree(t)
	for _, c := range t224Allowed {
		c = s.expand(c)
		s.code(t, c, sgOpts{}, 0, "a sub-agent removes the line's own mktemp -d folder")
		s.code(t, c, top(), 0, "the top level removes the line's own mktemp -d folder")
		for _, child := range []bool{false, true} {
			if r := s.piCall(t, c, child); r.Code != 0 {
				t.Errorf("pi (child %v): %q exits %d: %s%s", child, c, r.Code, r.Stderr, r.Stdout)
			}
		}
	}
}

func TestT224ScratchFolderRemovalKeepsRefusingTheRest(t *testing.T) {
	t.Parallel()
	s := t224Tree(t)
	for _, c := range t224Refused {
		c = s.expand(c)
		r := s.bash(t, c, sgOpts{})
		code(t, r, 2, c)
		if !strings.Contains(r.Stderr, "safety-guard: refused: rm -r") {
			t.Errorf("%q: the refusal names no rm: %s", c, r.Stderr)
		}
		if r := s.piCall(t, c, true); r.Code != 2 {
			t.Errorf("pi child: %q exits %d: %s", c, r.Code, r.Stderr)
		}
	}
	// The HOME target is refused on its own, the scratch folder is not named.
	r := s.bash(t, `d=$(mktemp -d) && rm -rf $d $HOME`, sgOpts{})
	matches(t, r.Stderr, `rm -r \$HOME: it reaches the home directory`, "HOME")
	if strings.Contains(r.Stderr, `rm -r $d:`) || strings.Contains(r.Stderr, "rm -r $(…):") {
		t.Errorf("the scratch folder is refused: %s", r.Stderr)
	}
}

// L5: the refusal of a scratch folder assigned after `cd … &&` names the safe
// form, for the sub-agent, the top level and pi; other refusals do not.
func TestT224RefusalNamesTheSafeForm(t *testing.T) {
	t.Parallel()
	s := t224Tree(t)
	for _, c := range []string{t224OwnersLine, "cd <P>/apps/cli && d=$(mktemp -d) && cat $d/x; rm -rf $d", "cd +1 && XDG_CONFIG_HOME=$(mktemp -d); rm -rf $XDG_CONFIG_HOME",
		`cd <P>/apps/cli && GOPATH=$(mktemp -d) && node x; rm -rf "$GOPATH"/*`} {
		c = s.expand(c)
		for _, o := range []sgOpts{{}, top()} {
			r := s.bash(t, c, o)
			code(t, r, 2, c)
			if !strings.Contains(r.Stderr, "rm -r $(…): its target cannot be resolved; "+t224SafeForm+". This is destructive") &&
				!strings.Contains(r.Stderr, "rm -r $(…)/*: its target cannot be resolved; "+t224SafeForm+". This is destructive") {
				t.Errorf("%q: the refusal does not name the safe form: %s", c, r.Stderr)
			}
		}
		if r := s.piCall(t, c, true); !strings.Contains(r.Stderr, t224SafeForm) {
			t.Errorf("pi child: %q: the refusal does not name the safe form: %s", c, r.Stderr)
		}
	}
	for _, c := range []string{"d=$(mktemp -d); d=/; rm -rf $d", "rm -rf $d; d=$(mktemp -d)", `d=$(mktemp -d -t x); rm -rf "$d"/*`, "d=$(pwd) && rm -rf $d",
		// L6 B5: no mktemp -d folder the rule reads.
		"cd <P> && d=$(mktemp) && true; rm -rf $d", "cd <P> && d=$(mktemp -d /etc/xXXX) && true; rm -rf $d"} {
		c = s.expand(c)
		if r := s.bash(t, c, sgOpts{}); strings.Contains(r.Stderr, "assign the scratch folder first") {
			t.Errorf("%q: the safe form is named for another refusal: %s", c, r.Stderr)
		}
	}
}
