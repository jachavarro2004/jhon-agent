// T152 follow-up (guarddiff against v0.3.7): xargs running a destructive
// program on operands the guard cannot see keeps the verdict it had in
// v0.3.7, asked of the owner at the top and refused to a sub-agent. A
// program xargs runs on visible operands is judged by its words, and a
// non-destructive one runs. Every case goes through `jhon hook pre-tool`.

package guards

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// xargsUnseen are the lines whose xargs runs a destructive program on input
// the guard cannot see, with that program: guarddiff's fuzz rows that
// v0.3.7 asked or refused (D1 and D2) and the brief's examples.
var xargsUnseen = [][2]string{
	// D1: a recursive delete.
	{"env X=1 echo '.git/config' | xargs rm -rf", "rm"},
	{"echo $TMPDIR/x | xargs rm -rf", "rm"},
	{"find . -name '*.tmp' | xargs rm -rf", "rm"},
	{"d=$(mktemp -d); ( sh -c 'echo \"$d/x\" | xargs rm -rf' )", "rm"},
	{"true || echo \"$(mktemp)\" | xargs rm -rf", "rm"},
	{"for f in a b; do env X=1 echo \"<HOME>/.local/bin/jhon\" | xargs rm -rf; done", "rm"},
	{"( echo $TMPDIR/x | xargs rm -rf )", "rm"},
	{"d=$(mktemp -d); eval '{ echo \"$d/x\" | xargs rm -rf; }'", "rm"},
	{"timeout 5 echo <TMP>/x | xargs rm -rf", "rm"},
	{"nohup echo ../x | xargs rm -rf &", "rm"},
	{"d=$(mktemp -d); eval 'echo \"$d/x\" | xargs rm -rf 2>&1 | tail -5'", "rm"},
	{"if [ -f x ]; then echo \"$TMPDIR/x\" | xargs rm -rf; fi", "rm"},
	{"d=$(mktemp -d); eval 'echo \"$d/x\" | xargs rm -rf' 2>&1 | tail -5", "rm"},
	{"d=$(mktemp -d); echo \"$d/x\" | xargs rm -rf", "rm"},
	{"if [ -f x ]; then env X=1 echo '<SCRATCH>/x' | xargs rm -rf; fi", "rm"},
	{"echo \"$(mktemp)\" | xargs rm -rf 2>&1 | tail -5", "rm"},
	// D2: a copy onto a credentials file the guard cannot see.
	{"timeout 5 echo <HOME>/.aws/credentials | xargs -I{} cp src/app.py {}", "cp"},
	{"sh -c 'nohup echo '\\''deep/sub/id_rsa'\\'' | xargs -I{} cp src/app.py {} &'", "cp"},
	// The rest of the destructive set, as in v0.3.7.
	{"fd -e pyc | xargs rm", "rm"},
	{"find . -name x | xargs rm", "rm"},
	{"cat list | xargs tee", "tee"},
	{"fd . | xargs -I{} mv {} /x", "mv"},
	{"find . -name x | xargs dd of=/dev/null", "dd"},
	{"find . -name x | xargs truncate -s 0", "truncate"},
	{"find . -name x | xargs shred", "shred"},
	{"find . -name x | xargs chmod 000", "chmod"},
	{"find . -name x | xargs sed -i s/a/b/", "sed"},
}

// xargsVisible are the brief's D1 and D2 lines whose operands the guard
// sees: v0.3.7 refused them by their words, and so does this build.
var xargsVisible = [][2]string{
	{"echo <HOME>/.local/bin/jhon | xargs rm -rf", `^safety-guard: refused: rm -r ` + regexp.QuoteMeta(sgHome) + `/\.local/bin/jhon: it is outside the project`},
	{"echo <HOME>/.aws/credentials | xargs -I{} cp x {}", `^safety-guard: refused: .*` + regexp.QuoteMeta(sgHome) + `/\.aws/credentials holds credentials`},
	{"echo deep/sub/id_rsa | xargs -I{} cp x {}", `^safety-guard: refused: .*deep/sub/id_rsa holds credentials`},
}

// xargsRuns are xargs lines that delete and overwrite nothing: they run.
var xargsRuns = []string{
	"find . -name '*.go' | xargs grep -n foo",
	"printf '%s\\n' a b | xargs echo",
	"find . -name x | xargs wc",
	"find . -name x | xargs cat",
	"echo dist | xargs rm -rf",
	"find . -name x | xargs touch",
	"find . -name x | xargs mkdir",
}

func xargsExpand(s sgEnv, command string) string {
	return s.expand(strings.ReplaceAll(command, "<HOME>", sgHome))
}

func TestXargsDestructiveUnseenOperandsAsked(t *testing.T) {
	t.Parallel()
	for _, x := range xargsUnseen {
		command, prog := x[0], x[1]
		t.Run(strconv.Quote(command), func(t *testing.T) {
			t.Parallel()
			s := newSG(t)
			c := xargsExpand(s, command)
			why := `unknown: xargs ` + prog + `: its operands come from input the guard cannot see`
			sub := s.preTool(t, c, sgOpts{})
			code(t, sub, 2, "sub-agent")
			matches(t, sub.Stderr, `^safety-guard: (.*; )?`+regexp.QuoteMeta(why)+`; it is refused outside a prompt the owner can answer`, "sub-agent")
			noDecision(t, sub.Stdout, "sub-agent")
			tp := s.preTool(t, c, top())
			code(t, tp, 0, "top-level")
			matches(t, askedWhy(t, tp.Stdout, "top-level"), `(^|; )`+regexp.QuoteMeta(why)+`($|;)`, "top-level")
		})
	}
}

func TestXargsDestructiveVisibleOperandsJudgedByTheirWords(t *testing.T) {
	t.Parallel()
	for _, x := range xargsVisible {
		command, want := x[0], x[1]
		t.Run(strconv.Quote(command), func(t *testing.T) {
			t.Parallel()
			s := newSG(t)
			c := xargsExpand(s, command)
			for _, o := range []sgOpts{{}, top()} {
				r := s.preTool(t, c, o)
				code(t, r, 2, c)
				matches(t, r.Stderr, want, c)
				noDecision(t, r.Stdout, c)
			}
		})
	}
}

func TestXargsNonDestructiveRuns(t *testing.T) {
	t.Parallel()
	for _, command := range xargsRuns {
		t.Run(strconv.Quote(command), func(t *testing.T) {
			t.Parallel()
			s := newSG(t)
			c := xargsExpand(s, command)
			for _, o := range []sgOpts{{}, top()} {
				r := s.preTool(t, c, o)
				code(t, r, 0, c)
				noDecision(t, r.Stdout, c)
				if r.Stderr != "" {
					t.Errorf("%s: stderr = %q", c, r.Stderr)
				}
			}
		})
	}
}
