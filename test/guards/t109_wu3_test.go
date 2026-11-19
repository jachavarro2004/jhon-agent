// T109 WU3 (L784 b, c): two false positives removed, the credential hard
// line kept (L671, L686).

package guards

import (
	"strings"
	"testing"
)

// (b) framework-guard charged a protected path named anywhere in the line to
// a script run from a file (L578: `python3 -m unittest … && cat
// ~/.local/share/jhon/current/prose/contracts/ldd.md`). A path only a plain
// reader names, its output shown and not fed anywhere, is no script's.
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
	// The path still reaches the script: in its heredoc text, as its
	// argument, through a substitution, a pipe, or a file a reader writes.
	for _, command := range []string{
		"cat > /tmp/t.py <<'PY'\nopen('" + e.fw + "/src/a.ts','w').write('x')\nPY\npython3 /tmp/t.py",
		"python3 /tmp/t.py \"$(cat " + e.fw + "/src/a.ts)\"",
		"python3 /tmp/t.py $(echo " + e.fw + "/src/a.ts)",
		"cat " + e.fw + "/src/a.ts | python3 /tmp/t.py",
		"cat " + e.fw + "/src/a.ts > /tmp/p.txt && python3 /tmp/t.py",
		"echo " + e.fw + "/src/a.ts > /tmp/p.txt && python3 /tmp/t.py",
		"python3 /tmp/t.py && bash -c 'cat " + e.fw + "/src/a.ts'",
		"(cat " + e.fw + "/src/a.ts) && python3 /tmp/t.py",
		// L808: anything that changes what the reader is or where its output
		// goes leaves the line ungated (WU1 lineFacts) or the reader not at
		// the top with only standard output streams.
		"cat(){ python3 /tmp/t.py \"$@\"; }; cat " + e.fw + "/src/a.ts",
		"function cat { python3 /tmp/t.py \"$@\"; }; cat " + e.fw + "/src/a.ts",
		"PATH=/tmp/e:$PATH; cat " + e.fw + "/src/a.ts; python3 /tmp/t.py",
		"exec 1>/tmp/p; ls -d " + e.fw + "/src/a.ts; python3 /tmp/t.py",
		"exec 3>/tmp/p 1>&3; ls -d " + e.fw + "/src/a.ts; python3 /tmp/t.py",
		"exec 3>/tmp/p; ls -d " + e.fw + "/src/a.ts >&3; python3 /tmp/t.py",
		"exec > >(python3 /tmp/t.py); ls -d " + e.fw + "/src/a.ts",
		"{ cat " + e.fw + "/src/a.ts; } > /tmp/p; python3 /tmp/t.py",
		"cat " + e.fw + "/src/a.ts; source /tmp/t.sh; python3 /tmp/t.py",
		"shopt -s expand_aliases\nalias cat=python3\ncat " + e.fw + "/src/a.ts; python3 /tmp/t.py",
		"cat " + e.fw + "/src/a.ts >&3; python3 /tmp/t.py",
		"cat " + e.fw + "/src/a.ts 1>&3; python3 /tmp/t.py",
		"coproc cat " + e.fw + "/src/a.ts; python3 /tmp/t.py",
		"cat " + e.fw + "/src/a.ts & python3 /tmp/t.py",
		"if true; then cat " + e.fw + "/src/a.ts; fi; python3 /tmp/t.py",
		"! { cat " + e.fw + "/src/a.ts; } 2>/tmp/p; python3 /tmp/t.py",
	} {
		fwRefused(t, e.guard(t, "Bash", cmd(command), fwWho{}), command)
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
	// Credential reads stay refused: built home credential paths, a
	// whole-string name, and prose beside a credential home. A whole path
	// outside the project is the sandbox's (T128f).
	for _, c := range []string{
		`node -e "require('fs').readFileSync(require('os').homedir()+'/.aws/credentials')"`,
		`python3 -c "import os; open(os.path.join(os.path.expanduser('~'), '.aws', 'credentials')).read()"`,
		`python3 -c "import os,subprocess; subprocess.run('cat the credentials', shell=True, cwd=os.path.expanduser('~/.aws'))"`,
		`python3 -c "import os; print(os.path.expanduser('~'), 'see credentials.json now')"`,
		`python3 -c "import os; print(os.path.expanduser('~'), 'see ~/credentials')"`,
		// L808 (c) pins: built home credential paths.
		`python3 -c "from pathlib import Path; print(open(Path.home()/'.aws'/'credentials').read())"`,
		`python3 -c "import os; h=os.path.expanduser('~'); print(open(f'{h}/.aws/credentials').read())"`,
		`python3 -c "import os; print(open(os.path.expanduser('\x7e/.aws/credentials')).read())"`,
		`python3 -c "import os; print(os.path.expanduser('~'), 'see credentials/ now')"`,
		`python3 -c "import os; print(open(os.path.join(os.path.expanduser('~'), '.'+'aws', 'x credentials'.split()[1])).read())"`,
	} {
		for _, o := range []sgOpts{{}, top()} {
			s.code(t, c, o, 2, "a credentials read")
		}
		for _, child := range []bool{false, true} {
			if r := s.piCall(t, c, child); r.Code != 2 {
				t.Errorf("pi (child %v): %q exits %d, want 2: %s%s", child, c, r.Code, r.Stderr, r.Stdout)
			}
		}
	}
}

// L824 B1: a reader is exempt by its place in the parsed tree, never by its
// text: a top-level copy does not exempt the same text inside a group, a
// body, a pipeline, a background job or a redirected block.
func TestFrameworkGuardDecoyCopyDoesNotExemptEnclosedReader(t *testing.T) {
	t.Parallel()
	e := newFwEnv(t)
	r := "ls -d " + e.fw + "/src/a.ts"
	c := "cat " + e.fw + "/src/a.ts"
	for _, command := range []string{
		r + "; { " + r + "; } | python3 /tmp/t.py",
		r + "; { " + r + "; } > /tmp/p; python3 /tmp/t.py",
		r + "; ( " + r + " ) | python3 /tmp/t.py",
		r + "; " + r + " | python3 /tmp/t.py",
		r + " | python3 /tmp/t.py; " + r,
		r + "; " + r + " > /tmp/p; python3 /tmp/t.py",
		r + "; " + r + " & python3 /tmp/t.py",
		r + "; if true; then " + r + "; fi > /tmp/p; python3 /tmp/t.py",
		r + "; if true; then " + r + "; fi | python3 /tmp/t.py",
		r + "; while true; do " + r + "; break; done | python3 /tmp/t.py",
		r + "; for i in 1; do " + r + "; done > /tmp/p; python3 /tmp/t.py",
		r + "; case x in x) " + r + ";; esac | python3 /tmp/t.py",
		r + "; python3 /tmp/t.py \"$(" + r + ")\"",
		r + "; python3 /tmp/t.py <(" + r + ")",
		r + "\n{ " + r + "\n} | python3 /tmp/t.py",
		c + " && { " + c + "; } 2>&1 | python3 /tmp/t.py",
		c + "; " + c + " >&3; python3 /tmp/t.py",
		c + "; coproc " + c + "; python3 /tmp/t.py",
		"! " + c + "; ! { " + c + "; } > /tmp/p; python3 /tmp/t.py",
		c + "; bash -c '" + c + " | python3 /tmp/t.py'",
	} {
		fwRefused(t, e.guard(t, "Bash", cmd(command), fwWho{}), command)
	}
}

// L824 B2: only the reader's own bytes are left out of the search; its text
// inside another word, path or heredoc body stays visible.
func TestFrameworkGuardReaderTextDoesNotBreakAProtectedPath(t *testing.T) {
	t.Parallel()
	e := newFwEnv(t)
	skill := e.skill + "/SKILL.md"
	body := "cat > /tmp/t.py <<'PY'\nopen('" + skill + "','w').write('x')\nPY\npython3 /tmp/t.py"
	r := "ls -d " + e.fw + "/src/a.ts"
	for _, command := range []string{
		"ls; " + body,
		"ls && " + body,
		body + "; ls",
		"nl; cat > /tmp/t.py <<'PY'\nopen('" + e.fw + "/src/nl','w').write('x')\nPY\npython3 /tmp/t.py",
		"stat; cat > /tmp/t.py <<'PY'\nopen('" + e.fw + "/src/stat','w').write('x')\nPY\npython3 /tmp/t.py",
		"cat > /tmp/t.py <<'PY'\nimport os\nos.system('''" + r + " > /tmp/p''')\nPY\n" + r + "; python3 /tmp/t.py",
		"echo " + r + " > /tmp/p; " + r + "; python3 /tmp/t.py",
		"echo x" + r + " > /tmp/p; " + r + "; python3 /tmp/t.py",
		r + "; cat > /tmp/t.py <<'PY'\n" + r + "\nPY\npython3 /tmp/t.py",
	} {
		fwRefused(t, e.guard(t, "Bash", cmd(command), fwWho{}), command)
	}
}

// L824 advisories: a line that can replay the reader's last word ($_, history)
// gets no exemption, and a reader word must be plain in the source text.
func TestFrameworkGuardReaderWordReplayedOrQuoted(t *testing.T) {
	t.Parallel()
	e := newFwEnv(t)
	f := e.fw + "/src/a.ts"
	for _, command := range []string{
		"cat " + f + "; python3 /tmp/t.py $_",
		"cat " + f + "; python3 /tmp/t.py \"$_\"",
		"cat " + f + "; python3 /tmp/t.py ${_}",
		"cat " + f + "; python3 /tmp/t.py \"${_}\"",
		"cat " + f + "; x=$_ python3 /tmp/t.py",
		"cat " + f + "; x=$_; python3 /tmp/t.py $x",
		"cat " + f + "; python3 /tmp/t.py <<< \"$_\"",
		"cat " + f + "; python3 /tmp/t.py <<EOF\n$_\nEOF",
		"cat " + f + "; python3 /tmp/t.py !$",
		"cat " + f + "; python3 /tmp/t.py !!",
		"cat " + f + "; python3 /tmp/t.py \"$BASH_COMMAND\"",
		"cat " + f + "; fc -ln -1 | python3 /tmp/t.py",
		"cat " + f + "; history | python3 /tmp/t.py",
		"cat $'" + f + "'; python3 /tmp/t.py",
		"cat $'" + f + "' && python3 /tmp/t.py",
		"$'cat' " + f + "; python3 /tmp/t.py",
		"cat $\"" + f + "\"; python3 /tmp/t.py",
		"cat " + f + " <<< x; python3 /tmp/t.py",
		"x=1 cat " + f + "; python3 /tmp/t.py",
	} {
		fwRefused(t, e.guard(t, "Bash", cmd(command), fwWho{}), command)
	}
}

// L829 verify: the exemption may not rest on a mismatch between the parser
// and bash. A line continuation inside $_ replays the reader's last word,
// and a heredoc whose delimiter bash does not accept (a CR after it, or a
// backslash-continued body line) leaves the reader's text on the script's
// stdin. Proven in bash 5.3.20.
func TestFrameworkGuardParserAndBashMustAgree(t *testing.T) {
	t.Parallel()
	e := newFwEnv(t)
	f := e.fw + "/src/a.ts"
	const P = "python3 /tmp/t.py"
	for _, command := range []string{
		// C1: $_ written with a line continuation.
		"cat " + f + "; " + P + " $\\\n_",
		"cat " + f + "; " + P + " \"$\\\n_\"",
		"cat " + f + "; " + P + " ${\\\n_}",
		"cat " + f + "; " + P + " $\\\n{_}",
		"cat " + f + "; " + P + " $\\\n\\\n_",
		"cat " + f + "; " + P + " \"a ${\\\n_} b\"",
		"cat " + f + "; x=$\\\n_; " + P + " $x",
		"cat " + f + "; x=$\\\n{_}; " + P + " \"$x\"",
		"cat " + f + "; x=$\\\n_ " + P,
		"cat " + f + "; " + P + " <<< $\\\n_",
		"cat " + f + "; " + P + " <<EOF\n$\\\n_\nEOF",
		"cat " + f + "; echo $\\\n_ > /tmp/p; " + P,
		"cat " + f + "; echo $\\\n_ | " + P,
		"cat " + f + " && " + P + " \"${\\\n_}\"",
		"ls -d " + f + "\n" + P + " $\\\n_",
		"ls -d " + f + "; " + P + " ${ echo $\\\n_; }",
		// C2: a CR after the delimiter keeps the heredoc open in bash.
		P + " <<EOF\nx\nEOF\r\nls -d " + f + "\nEOF\n",
		P + " <<'EOF'\nEOF\r\ncat " + f + "\nEOF",
		P + " <<EOF\r\nx\nEOF\nls -d " + f + "\nEOF\r\n",
		P + " <<EOF\nx\nEOF\r\nls -d " + f + "\nEOF\n: done",
		"cat <<EOF | " + P + "\nEOF\r\nls -d " + f + "\nEOF\n",
		"cat > /tmp/t.py <<'EOF'\nEOF\r\nls -d " + f + "\nEOF\n" + P,
		"x=$(cat <<EOF\nEOF\r\nls -d " + f + "\nEOF\n); " + P + " $x", // pin
		// C3: a backslash-continued body line is not the delimiter either.
		P + " <<-EOF\n\tx\\\n\tEOF\nls -d " + f + "\nEOF",
		P + " <<-EOF\nx\\\n\tEOF\nls -d " + f + "\nEOF",
		P + " <<-EOF \n EOF\nEOF\\\nEOF\\\n\t\tEOF\nls -d " + f + "\nEOF\n",
		P + " <<EOF\n\tx\\\n\tEOF\nls -d " + f + "\nEOF", // pin
	} {
		fwRefused(t, e.guard(t, "Bash", cmd(command), fwWho{}), command)
	}
}

// L830 advisory: zsh ties the lowercase path to PATH (cdpath, fpath, … to
// their uppercase arrays), so a line assigning one may change what the
// reader is; it gets no exemption.
func TestFrameworkGuardZshSpecialNameDropsTheExemption(t *testing.T) {
	t.Parallel()
	e := newFwEnv(t)
	f := e.fw + "/src/a.ts"
	for _, command := range []string{
		"path=$T/e; cat " + f + "; python3 /tmp/t.py",
		"path=/tmp/e; cat " + f + "; python3 /tmp/t.py",
		"path=/tmp/e cat /dev/null; cat " + f + "; python3 /tmp/t.py",
		"for path in /tmp/e; do :; done; cat " + f + "; python3 /tmp/t.py",
		"fpath=/tmp/e; cat " + f + " && python3 /tmp/t.py",
	} {
		fwRefused(t, e.guard(t, "Bash", cmd(command), fwWho{}), command)
	}
	// Any other lowercase name keeps it.
	command := "paths=/tmp/e; cat " + f + "; python3 /tmp/t.py"
	fwAllowed(t, e.guard(t, "Bash", cmd(command), fwWho{}), command)
}

// Verify of round 2: a bash function substitution ${ …; } runs in the
// current shell, so what it does counts; and a reader inside it, or one
// whose last word it replays, is not a shown read.
func TestFrameworkGuardFunctionSubstitution(t *testing.T) {
	t.Parallel()
	e := newFwEnv(t)
	f := e.fw + "/src/a.ts"
	const P = "python3 /tmp/t.py"
	for _, command := range []string{
		": ${ exec >/tmp/p; }; cat " + f + "; " + P,
		": ${ exec 3>/tmp/p; }; cat " + f + " >&3; " + P,
		": ${ PATH=/tmp/e:$PATH; }; cat " + f + "; " + P,
		": ${ alias cat=x; }; cat " + f + "; " + P,
		"y=${ cat " + f + "; }; " + P + " $y",
		"y=${ ls -d " + f + "; }; " + P,
		P + " ${ cat " + f + "; }",
		"cat " + f + "; " + P + " ${ echo $_; }",
		"cat " + f + "; : ${ " + P + " $_; }",
		"ls -d " + f + "; ${ echo " + P + "; }",
	} {
		fwRefused(t, e.guard(t, "Bash", cmd(command), fwWho{}), command)
	}
}

// Verify of round 2: ;& and ;;& bodies are bodies too (L824 B1 used ;;),
// and a plain read stays exempt past the parser's 1 KiB read buffer.
func TestFrameworkGuardCaseFallthroughAndLongLines(t *testing.T) {
	t.Parallel()
	e := newFwEnv(t)
	f := e.fw + "/src/a.ts"
	const P = "python3 /tmp/t.py"
	r := "ls -d " + f
	for _, command := range []string{
		"case x in x) " + r + " ;& esac; " + P,
		"case x in x) cat " + f + " ;;& esac; " + P,
		r + "; case x in x) " + r + " ;& esac | " + P,
	} {
		fwRefused(t, e.guard(t, "Bash", cmd(command), fwWho{}), command)
	}
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
