// T109 WU3 (L784 b, c): two false positives removed, the credential hard
// line kept (L671, L686). The refusals of deliberate constructions that fed
// a framework path to a script (L808, L824, L829, L830) went with T152
// (L1311): framework-guard reads only what a Bash line visibly runs.

package guards

import (
	"strings"
	"testing"
)

// (b) framework-guard charged a protected path named anywhere in the line to
// a script run from a file (L578: `python3 -m unittest … && cat
// ~/.local/share/jhon/current/prose/contracts/ldd.md`). A path only a plain
// reader names, its output shown and not fed anywhere, is no script's. Since
// T152 (L1311) framework-guard reads only the program a Bash line visibly
// runs, so a deliberate construction feeding the path to a script is no
// longer refused either (the sandbox holds a write there, T128f).
func TestFrameworkGuardReaderPathIsNotTheScripts(t *testing.T) {
	t.Parallel()
	e := newFwEnv(t)
	for _, command := range []string{
		"python3 -m unittest discover -s tests -v && cat " + e.current + "/prose/contracts/ldd.md",
		"python3 -m unittest discover -s tests; cat " + e.current + "/prose/contracts/ldd.md",
		"node --test test/a.test.js && head -50 " + e.fw + "/README.md",
		"cat " + e.current + "/prose/contracts/ldd.md && python3 run_tests.py",
		"wc -l " + e.fw + "/README.md && python3 run_tests.py",
		"stat " + e.fw + "/README.md; python3 run_tests.py",
		"nl " + e.fw + "/README.md; python3 run_tests.py",
		"ls -la " + e.fw + "/src && python3 run_tests.py",
		"ls -R " + e.fw + " && python3 run_tests.py",
		"ls " + e.fw + "/src/*.ts && python3 run_tests.py",
		"tail -f " + e.fw + "/README.md; python3 run_tests.py",
		"cat -- " + e.fw + "/README.md && python3 run_tests.py",
		"cat " + e.fw + "/README.md 2>/dev/null && python3 run_tests.py",
		"cat " + e.fw + "/README.md >/dev/null && python3 run_tests.py",
		"cat " + e.fw + "/README.md 2>&1 && python3 run_tests.py",
	} {
		fwAllowed(t, e.guard(t, "Bash", cmd(command), fwWho{}), command)
	}
}

// (c) code naming the word credentials in prose (a prompt string), with a
// home lookup elsewhere in the code, was read as a credentials file (L596).
func TestCodeNamingTheWordCredentialsInProse(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range []string{
		`node -e "import('/x/shell/lib/agents.ts').then(m=>m.runAgent({task:'review how credentials are loaded from HOME'}))"`,
		`python3 -c "import os; print(os.path.expanduser('~'), 'the credentials are rotated weekly')"`,
	} {
		for _, o := range []sgOpts{{}, top()} {
			s.code(t, c, o, 0, "prose naming the word credentials")
		}
		for _, child := range []bool{false, true} {
			if r := s.piCall(t, c, child); r.Code != 0 {
				t.Errorf("pi (child %v): %q exits %d: %s%s", child, c, r.Code, r.Stderr, r.Stdout)
			}
		}
	}
}

// L830 advisory: zsh ties the lowercase path to PATH (cdpath, fpath, … to
// their uppercase arrays), so a line assigning one may change what the
// reader is; it got no exemption, which T152 (L1311) made moot: the line is
// judged by the program it visibly runs.
func TestFrameworkGuardZshSpecialNameDropsTheExemption(t *testing.T) {
	t.Parallel()
	e := newFwEnv(t)
	f := e.fw + "/src/a.ts"
	// A lowercase name zsh does not tie passes.
	command := "paths=/tmp/e; cat " + f + "; python3 /tmp/t.py"
	fwAllowed(t, e.guard(t, "Bash", cmd(command), fwWho{}), command)
}

// Verify of round 2: a plain read stays exempt past the parser's 1 KiB read
// buffer. (Its ;& and ;;& bodies, refused until T152, are judged by the
// line's visible program.)
func TestFrameworkGuardCaseFallthroughAndLongLines(t *testing.T) {
	t.Parallel()
	e := newFwEnv(t)
	f := e.fw + "/src/a.ts"
	const P = "python3 /tmp/t.py"
	for _, n := range []int{1023, 1024, 1025, 2048, 4096} {
		pad := strings.Repeat("x", n)
		for _, command := range []string{
			": " + pad + "; cat " + f + "; " + P,
			"# " + pad + "\ncat " + f + "; " + P,
		} {
			fwAllowed(t, e.guard(t, "Bash", cmd(command), fwWho{}), command)
		}
	}
}
