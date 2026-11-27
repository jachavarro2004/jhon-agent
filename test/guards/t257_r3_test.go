// T257 round 3 (verify mwya5vcm of b32b8a4): the two false refusals round
// 2 brought. A sourced file leaves a no-run name untrusted only when the
// line uses that name after the source and the file is unreadable or
// defines it; a value is judged only where it reaches a place that runs it.
package guards

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// A readable activate file that defines no no-run name leaves grep trusted
// (route-guard); one that defines it, one the guard cannot read, and a
// name used after them do not.
func TestT257SourcedFileLeavesNamesTrusted(t *testing.T) {
	t.Parallel()
	m := newMG(t)
	support.Write(t, m.p, map[string]string{
		".venv/bin/activate": "deactivate () { unset VIRTUAL_ENV; }\nexport VIRTUAL_ENV=/x\nexport PATH=\"$VIRTUAL_ENV/bin:$PATH\"\n",
		"defs.sh":            "ls() { \"$@\"; }\n",
	})
	for _, c := range []string{
		"source .venv/bin/activate && grep -rn 'env -u JHON_AGENT' . ; git status",
		". .venv/bin/activate; ls env -u JHON_AGENT",
		// Used before the source: the source changes nothing it ran.
		"ls env -u JHON_AGENT; source \"$UNSET_DIR/x.sh\"",
	} {
		r := m.bash(t, m.p, c)
		code(t, r, 0, c)
		if r.Stderr != "" || r.Stdout != "" {
			t.Errorf("%q: output %q %q", c, r.Stdout, r.Stderr)
		}
	}
	for c, name := range map[string]string{
		"source defs.sh; ls env -u JHON_AGENT git status":             "ls",
		"source \"$UNSET_DIR/x.sh\"; ls env -u JHON_AGENT git status": "ls",
	} {
		r := m.bash(t, m.p, c)
		code(t, r, 2, c)
		if want := "route-guard: `" + c + "` would turn off the merge gate git runs in this repository (hook.jhon-merge-gate, T219): " + name +
			" may run its words: it changes JHON_AGENT: "; !strings.HasPrefix(r.Stderr, want) {
			t.Errorf("%q: %q, want it to open with %q", c, r.Stderr, want)
		}
	}
}

// The safety guard: `source "$HOME/.cargo/env"` (a readable file that
// defines no name) leaves grep trusted.
func TestT257SourcedCargoEnvLeavesGrepTrusted(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	home := mkdir(t, filepath.Join(s.scratch, "home"))
	support.Write(t, home, map[string]string{".cargo/env": "#!/bin/sh\ncase \":${PATH}:\" in *:\"$HOME/.cargo/bin\":*) ;; *) export PATH=\"$HOME/.cargo/bin:$PATH\" ;; esac\n"})
	s.env = support.Env("HOME="+home, "XDG_STATE_HOME="+s.state, "TMPDIR="+s.tmp)
	allowed(t, s, `source "$HOME/.cargo/env"; grep -rn 'rm -rf .git' .`)
	allowed(t, s, `source "$HOME/.cargo/env" && cargo build`)
	support.Write(t, home, map[string]string{".bad/env": "grep() { \"$@\"; }\n"})
	classAsked(t, s, `source "$HOME/.bad/env"; grep rm -rf .git`,
		"grep is not a program the guard knows never runs its arguments, and they hold `rm -rf .git`, which it would refuse")
}

// Prose in a variable that is only an argument is data; a value that
// reaches a place that runs it keeps its verdict.
func TestT257ValuesAreJudgedWhereTheyRun(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range []string{
		`MSG='fix: git push failed on CI'; git commit -m "$MSG"`,
		`msg="git push rejected"; echo "$msg"`,
		`export NOTE='never rm -rf .git'; echo "$NOTE"`,
		`TITLE='git push --force broke main'; printf '%s\n' "$TITLE"`,
	} {
		allowed(t, s, c)
	}
	for _, c := range []string{
		"CC='rm -rf .git;:' make",
		"env CC='rm -rf .git;:' make",
		"export CC='rm -rf .git;:'; make",
		"printf -v GIT_EDITOR 'rm -rf .git'; git commit",
		"BASH_ENV=<(echo 'rm -rf .git') bash -c :",
		"X='rm -rf .git'; $X",
		`X='rm -rf .git'; eval "$X"`,
		`X='rm -rf .git'; sh -c "$X"`,
	} {
		refused(t, s, c, t257Git)
	}
	classAsked(t, s, "PERL5OPT='-MPOSIX;system(\"rm -rf .git\")' perl -e 1",
		"the variable PERL5OPT holds `rm -rf .git`, which a program may run and the guard would refuse")
}
