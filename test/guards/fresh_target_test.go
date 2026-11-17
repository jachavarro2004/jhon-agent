// T109/T62 (L765, L796): a write whose whole target is a variable the
// same line assigned from `mktemp` is not resolved by safety-guard: the
// WU1 relaxation that let it pass was reverted (L796), so it asks at the
// top level and is refused in sub-agents, in Claude Code and in pi (whose
// bash, bash_background and monitor tools all send the command as a Bash
// payload, shell/lib/guards.ts claudeCall). The parsed-line gate the
// relaxation brought (lineFacts) stays: it only tightens the fresh and
// scratch mktemp rules scope-, route- and safety-guard share.
package guards

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// freshTargetL765 asks again, as at baseline 71086791 (L796).
var freshTargetL765 = []string{
	// The exact L765 command shape (smoke-6cb76991-opus slow-suite, pi monitor).
	`L=$(mktemp); python3 run_suite.py > "$L"`,
	`L=$(mktemp); python3 run_suite.py > "$L" 2>&1; tail -20 "$L"`,
	`L=$(mktemp); python3 run_suite.py >> "$L"`,
	"out=$(mktemp -d)/log; python3 run_suite.py > $out",
	"d=$(mktemp -d); cp a.py $d/",
	"L=$(mktemp) && python3 run_suite.py > ${L} 2>&1 && cat $L",
	`L=$(mktemp /tmp/suite.XXXXXX); python3 run_suite.py > "$L"`,
}

var freshTargetAsks = []string{
	// Reassigned after mktemp. The project lives under the test's temporary
	// root, which /tmp always is to the guards, so a sibling such as
	// ../other/x is a temp path under TMPDIR=/tmp: climb to / instead.
	`L=$(mktemp); L=../../../../../../../../etc/x; echo x > "$L"`,
	`L=$(mktemp); read L; echo x > "$L"`,
	// From another command or user input.
	`L=$(cat name.txt); echo x > "$L"`,
	`read L; echo x > "$L"`,
	// Climbing out of the temporary root.
	// Enough .. to reach / from any temporary root the test runs under: with
	// two, a TMPDIR of /tmp put the target in /tmp/<test>/etc/x, a temp path.
	"L=$(mktemp -d); echo x > $L/../../../../../../../../etc/x",
	// A template or -p directory outside the temporary root.
	"L=$(mktemp /etc/x.XXXX); echo x > $L",
	"L=$(mktemp -p ~/project); echo x > $L",
	"L=$(mktemp -p .); echo x > $L",
	// A failed mktemp leaves it empty.
	"L=$(mktemp) || true; echo x > $L",
	// Something else may have put a link or a file there.
	"d=$(mktemp -d); ln -s ~/.bashrc $d; cp a.py $d/",
	"d=$(mktemp -d); python3 mk.py $d; echo x > $d",
	"d=$(mktemp -d); cp -as ~ $d/; cp a.py $d/",
	// A deletion keeps its own rule (scratchVars, L783): a file mktemp is not resolved for rm.
	"L=$(mktemp) && python3 run_suite.py > ${L} 2>&1 && rm -f $L",
	// Verify of WU1 (L789): anything earlier in the line that can change
	// what mktemp is or returns, or what the variable holds.
	"L=$(mktemp); L[0]=/etc/x; echo x > $L",
	"L=$(mktemp); source ./set.sh; echo x > $L",
	"L=$(mktemp); . ./set.sh; echo x > $L",
	"L=$(mktemp); if true; then . ./set.sh; fi; echo x > $L",
	"mktemp() { echo /etc/x; }; L=$(mktemp); echo x > $L",
	"function mktemp { echo /etc/x; }; L=$(mktemp); echo x > $L",
	"shopt -s expand_aliases; alias mktemp='echo /etc/x'; L=$(mktemp); echo x > $L",
	"PATH=/tmp/evil:$PATH; L=$(mktemp); echo x > $L",
	"export PATH=<P>:$PATH; L=$(mktemp); echo x > $L",
	"hash -p /tmp/evil mktemp; L=$(mktemp); echo x > $L",
	"TMPDIR=/etc; L=$(mktemp); echo x > $L",
	"export TMPDIR=~/project; L=$(mktemp); echo x > $L",
	// Verify of the fix (L791): spellings a regex denylist misses, read
	// from the parsed line instead (T109 WU1, third round).
	"L=$(mktemp); printf -vL /etc/x; echo x > $L",
	"printf -vTMPDIR /etc; L=$(mktemp); echo x > $L",
	"L=$(mktemp); printf -v L /etc/x; echo x > $L",
	`L=$(mktemp); "source" ./set.sh; echo x > $L`,
	`L=$(mktemp); "." ./set.sh; echo x > $L`,
	`"alias" mktemp='echo /etc/x'; L=$(mktemp); echo x > $L`,
	"L=$(mktemp); while . ./set.sh; do break; done; echo x > $L",
	"L=$(mktemp); until . ./set.sh; do break; done; echo x > $L",
	"L=$(mktemp); if . ./set.sh; then true; fi; echo x > $L",
	"L=$(mktemp); x=1 . ./set.sh; echo x > $L",
	"L=$(mktemp); exec 3>/etc/x; echo x > $L",
	"L=$(mktemp); L[1]=/etc/x; echo x > $L",
	"CDPATH=/etc; L=$(mktemp); echo x > $L",
	"L=$(mktemp); ((L=1)); echo x > $L",
	"L=$(mktemp); let L=1; echo x > $L",
	"L=$(mktemp); for L in /etc/x; do true; done; echo x > $L",
	"L=$(mktemp); $CMD ./set.sh; echo x > $L",
	"IFS=/; L=$(mktemp); echo x > $L",
	"L=$(mktemp); echo ${L:=/etc/x} > $L",
	// Third verify (L793): brace expansion as the program word and the
	// command hash table; the allowlist (fourth round) reads neither.
	"L=$(mktemp); {printf,-vL,/etc/x}; echo x > $L",
	"L=$(mktemp); {source,./set.sh}; echo x > $L",
	"BASH_CMDS[mktemp]=./ev.sh; L=$(mktemp); echo x > $L",
	"BASH_CMDS=([mktemp]=./ev.sh); L=$(mktemp); echo x > $L",
	"L=$(mktemp); [[ -n x ]]; echo x > $L",
	"X=1; L=$(mktemp); echo x > $L",
	// Final verify (L796): bash evaluates a variable's value as arithmetic
	// in every arithmetic context, so z="PATH=0" and $((z)) assigns PATH.
	`y=P; z="${y}ATH=0"; : $((z)); L=$(mktemp); echo x > $L`,
	`y=T; z="${y}MPDIR=/etc"; : $[z]; L=$(mktemp); echo x > $L`,
	"L=$(mktemp); [ -v 'a[TMPDIR=0]' ]; echo x > $L",
	"z=PATH=0; L=$(mktemp); echo ${x:z}; echo x > $L",
	"z=PATH=0; L=$(mktemp); echo ${a[z]}; echo x > $L",
	"z=L=0; L=$(mktemp); ((z)); echo x > $L",
}

func TestFreshMktempTargetNotProvenAsks(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	support.Write(t, s.p, map[string]string{"a.py": "x = 1\n", "run_suite.py": "print(1)\n"})
	check := func(c, cwd string) {
		t.Helper()
		o := top()
		o.cwd = cwd
		askedWhy(t, s.bash(t, c, o).Stdout, "top level: "+c)
		o = sgOpts{cwd: cwd}
		code(t, s.bash(t, c, o), 2, "sub-agent: "+c)
	}
	for _, c := range append(slices.Clone(freshTargetL765), freshTargetAsks...) {
		c = s.expand(c)
		check(c, s.p)
		code(t, s.piCall(t, c, true), 2, "pi child: "+c)
		// pi's top level asks, or route-guard refuses first in a repository with no layout.
		if r := s.piCall(t, c, false); !strings.Contains(r.Stdout, `"ask"`) && r.Code != 2 {
			t.Errorf("pi top level: %s: no ask: %q %q", c, r.Stdout, r.Stderr)
		}
	}
	// A cd before mktemp changes what a relative -p names: run from the
	// temporary root, `-p .` after `cd <project>` is the project.
	check("cd "+s.p+"; L=$(mktemp -p .); echo x > $L", s.tmp)
	check("cd "+filepath.Join(s.p, "src")+" && d=$(mktemp -d -p .) && cp ../a.py $d/", s.tmp)
}

// freshPoisoned are lines whose `out=$(mktemp -d)` scope- and route-guard
// must not resolve (L789 B1-B5): <D> is a path in the repository. The
// `$(mktemp -d)/name` form WU1 added is gone (L796), so the probes write
// under the directory, the form the shared rule still resolves.
var freshPoisoned = []string{
	"out=$(mktemp -d); out[0]=<D>; echo x > $out/x",
	"out=$(mktemp -d); source ./set.sh; echo x > $out/x",
	"out=$(mktemp -d); . ./set.sh; echo x > $out/x",
	"mktemp() { echo <D>; }; out=$(mktemp -d); echo x > $out/x",
	"function mktemp { echo <D>; }; out=$(mktemp -d); echo x > $out/x",
	"shopt -s expand_aliases; alias mktemp='echo <D>'; out=$(mktemp -d); echo x > $out/x",
	"PATH=/tmp/evil:$PATH; out=$(mktemp -d); echo x > $out/x",
	"export PATH=.:$PATH; out=$(mktemp -d); echo x > $out/x",
	"hash -p /tmp/evil mktemp; out=$(mktemp -d); echo x > $out/x",
	"TMPDIR=<D>; out=$(mktemp -d); echo x > $out/x",
	"export TMPDIR=<D>; out=$(mktemp -d); echo x > $out/x",
	// L791.
	"printf -vout <D>; out=$(mktemp -d); echo x > $out/x",
	"out=$(mktemp -d); printf -vout <D>; echo x > $out/x",
	"printf -vTMPDIR <D>; out=$(mktemp -d); echo x > $out/x",
	`out=$(mktemp -d); "source" ./set.sh; echo x > $out/x`,
	`out=$(mktemp -d); "." ./set.sh; echo x > $out/x`,
	`"alias" mktemp='echo <D>'; out=$(mktemp -d); echo x > $out/x`,
	"out=$(mktemp -d); while . ./set.sh; do break; done; echo x > $out/x",
	"out=$(mktemp -d); x=1 . ./set.sh; echo x > $out/x",
	"CDPATH=.; out=$(mktemp -d); echo x > $out/x",
	// L793.
	"{printf,-vout,<D>}; out=$(mktemp -d); echo x > $out/x",
	"out=$(mktemp -d); {source,./set.sh}; echo x > $out/x",
	"BASH_CMDS[mktemp]=./ev.sh; out=$(mktemp -d); echo x > $out/x",
	// L796: arithmetic contexts.
	`y=T; z="${y}MPDIR=<D>"; : $((z)); out=$(mktemp -d); echo x > $out/x`,
	`y=P; z="${y}ATH=.:0"; : $[z]; out=$(mktemp -d); echo x > $out/x`,
	"out=$(mktemp -d); [ -v 'a[out=0]' ]; echo x > $out/x",
	"z=TMPDIR=0; echo ${x:z}; out=$(mktemp -d); echo x > $out/x",
	"z=TMPDIR=0; echo ${a[z]}; out=$(mktemp -d); echo x > $out/x",
	// The reverted `$(mktemp -d)/name` form resolves nothing.
	"out=$(mktemp -d)/x; echo x > $out",
	// L830/L831: zsh ties the lowercase path (cdpath, fpath …) to PATH.
	"path=$(mktemp -d); python3 run.py > $path/log",
	`path=$(mktemp -d); python3 run.py > "$path/log"`,
	"cdpath=$(mktemp -d); python3 run.py > $cdpath/log",
}

// scratchPoisoned are T64c lines, `d=$(mktemp -d) && rm -r $d`, where
// something earlier may change what mktemp returns or what d holds, so the
// rm may reach <P>/src (L791, pre-existing): the scratch rule resolves
// nothing on them.
var scratchPoisoned = []string{
	"mktemp() { echo <P>/src; }; d=$(mktemp -d) && rm -r $d",
	"function mktemp { echo <P>/src; }; d=$(mktemp -d) && rm -r $d",
	"shopt -s expand_aliases; alias mktemp='echo <P>/src'; d=$(mktemp -d) && rm -r $d",
	`"alias" mktemp='echo <P>/src'; d=$(mktemp -d) && rm -r $d`,
	"PATH=/tmp/evil:$PATH; d=$(mktemp -d) && rm -r $d",
	"hash -p /tmp/evil mktemp; d=$(mktemp -d) && rm -r $d",
	"TMPDIR=<P>; d=$(mktemp -d) && rm -r $d",
	"export TMPDIR=<P>; d=$(mktemp -d) && rm -r $d",
	"printf -vTMPDIR <P>; d=$(mktemp -d) && rm -r $d",
	"d=$(mktemp -d) && printf -vd <P>/src && rm -r $d",
	`d=$(mktemp -d) && "source" ./set.sh && rm -r $d`,
	"d=$(mktemp -d) && while . ./set.sh; do break; done && rm -r $d",
	// L793.
	"d=$(mktemp -d) && {printf,-vd,<P>/src} && rm -r $d",
	"{printf,-vTMPDIR,<P>}; d=$(mktemp -d) && rm -r $d",
	"d=$(mktemp -d) && {source,./set.sh} && rm -r $d",
	"BASH_CMDS[mktemp]=./ev.sh; d=$(mktemp -d) && rm -r $d",
	// L796: arithmetic contexts (pre-existing at baseline).
	`y=T; z="${y}MPDIR=<P>"; : $((z)); d=$(mktemp -d) && rm -r $d`,
	`y=T; z="${y}MPDIR=<P>"; : $[z]; d=$(mktemp -d) && rm -r $d`,
	"d=$(mktemp -d) && [ -v 'a[d=<P>/src]' ] && rm -r $d",
	"z=d=<P>/src; d=$(mktemp -d) && echo ${x:z} && rm -r $d",
	"z=d=<P>/src; d=$(mktemp -d) && echo ${a[z]} && rm -r $d",
	// Verify of WU1 (L805).
	"z=d=<P>/src; w='-v a[z]'; d=$(mktemp -d) && test $w && rm -r $d",
	"z=d=<P>/src; b='[z]'; v=-v; d=$(mktemp -d) && [ $v a$b ] && rm -r $d",
	"z=d=<P>/src; d=$(mktemp -d) && echo ${a[z+0]} && rm -r $d",
	"z=d=<P>/src; d=$(mktemp -d) && case x in ${a[z]}) ;; esac && rm -r $d",
}

func TestScratchDirPoisonedRmRefused(t *testing.T) {
	t.Parallel()
	s := scratchTree(t)
	p := scopePrompt
	sc := newScope(t, &p)
	repo, env := noLayoutRepo(t)
	for _, c := range scratchPoisoned {
		e := s.expand(c)
		s.code(t, e, sgOpts{}, 2, "safety-guard sub-agent")
		if r := s.piCall(t, e, true); r.Code != 2 {
			t.Errorf("pi child: %q exits %d: %s", e, r.Code, r.Stderr)
		}
		d := strings.ReplaceAll(c, "<P>", sc.repo)
		code(t, sc.guard(t, "Bash", map[string]any{"command": d}), 2, "scope-guard: "+d)
		rc := strings.ReplaceAll(c, "<P>", repo)
		for rt, pl := range rgPayloads(repo, rc) {
			code(t, hook(t, env, "route-guard", nil, pl), 2, rt+" route-guard: "+rc)
		}
	}
	// `d[0]=x` sets what $d reads, and every guard reads $d as x: the scratch
	// rule must not read it as the mktemp directory, so a target the guard
	// refuses on its own stays refused (an in-project one is judged as itself).
	c := "d=$(mktemp -d) && d[0]=/etc && rm -r $d"
	r := s.bash(t, c, sgOpts{})
	code(t, r, 2, c)
	matches(t, r.Stderr, `rm -r /etc`, c)
	if r := s.piCall(t, c, true); r.Code != 2 {
		t.Errorf("pi child: %q exits %d: %s", c, r.Code, r.Stderr)
	}
	// L796: z="d=0" and $((z)) set d to 0, so rm -r $d removes /etc/0;
	// outside the repository, safety-guard alone judges it.
	c = `cd /etc && d=$(mktemp -d) && y=D && z="${y,}=0" && : $((z)) && rm -r $d`
	s.code(t, c, sgOpts{}, 2, "safety-guard sub-agent")
	if r := s.piCall(t, c, true); r.Code != 2 {
		t.Errorf("pi child: %q exits %d: %s", c, r.Code, r.Stderr)
	}
}

func TestFreshMktempPoisonedScopeAndRoute(t *testing.T) {
	t.Parallel()
	p := scopePrompt
	sc := newScope(t, &p)
	code(t, sc.guard(t, "Bash", map[string]any{"command": "out=$(mktemp -d); echo x > $out/x"}), 0, "scope-guard control")
	repo, env := noLayoutRepo(t)
	for _, c := range freshPoisoned {
		d := strings.ReplaceAll(c, "<D>", "docs")
		code(t, sc.guard(t, "Bash", map[string]any{"command": d}), 2, "scope-guard: "+d)
		rc := strings.ReplaceAll(c, "<D>", "src")
		for rt, pl := range rgPayloads(repo, rc) {
			code(t, hook(t, env, "route-guard", nil, pl), 2, rt+" route-guard: "+rc)
		}
	}
	// A commit whose output goes to the line's mktemp file is still delivery.
	for rt, pl := range rgPayloads(repo, "L=$(mktemp); git commit -m x > $L") {
		r := hook(t, env, "route-guard", nil, pl)
		code(t, r, 2, rt+" commit > $L")
		if !strings.Contains(r.Stderr, "LDD is not set up in this project") {
			t.Errorf("%s commit > $L: %q", rt, r.Stderr)
		}
	}
}
