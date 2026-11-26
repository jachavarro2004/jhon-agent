// T257 (phase1-security L9, the advisories of T228's round-4 re-verify):
// what a program is given to read (its input, a `<(…)` file), an editor's
// or a language's own delete and list arguments, the variables programs
// run by their names, the git configuration files a line points git at,
// and route-guard's trust of a no-run name a line redefines. Each class is
// one reading the guard already had, extended: the class rule (T228 L6),
// the interpreter-code reader, the command-variable table (T228 L8), the
// -c setting reader (T228 L10) and the no-run trust (T228 L8).
package guards

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// classAsked wants c refused to a sub-agent and asked at the top level as
// unreadable, with why as the reason in the parentheses.
func classAsked(t *testing.T, s sgEnv, c, why string) {
	t.Helper()
	want := "the command cannot be read safely (" + why + "); "
	sub := s.bash(t, c, sgOpts{})
	code(t, sub, 2, "sub-agent: "+c)
	if !strings.HasPrefix(sub.Stderr, "safety-guard: unreadable: "+want) {
		t.Errorf("sub-agent: %q: %s", c, sub.Stderr)
	}
	tp := s.bash(t, c, top())
	code(t, tp, 0, "top: "+c)
	if d, reason, ok := decision(t, tp.Stdout); !ok || d != "ask" || !strings.HasPrefix(reason, "unreadable: "+want) {
		t.Errorf("top: %q: %q %q", c, tp.Stdout, tp.Stderr)
	}
}

const t257Git = "rm -r .git: it reaches the VCS directory .git"

// The text a program is given to read on its input (a here-string, a
// here-document, a pipe from a literal echo or printf) or as a `<(…)` file
// is read by the class rule as its arguments are, for a program the guard
// has no reading of and for a runner grammar that reads commands on its
// input (gdb).
func TestT257TextAProgramReadsIsReadByTheClassRule(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	const held = "and the text it reads (its input or a <(…) file) holds `rm -rf .git`, which it would refuse"
	for c, name := range map[string]string{
		"nvim -es <<< '!rm -rf .git'":                      "nvim",
		"sqlite3 x.db <<< '.shell rm -rf .git'":            "sqlite3",
		"echo '.system rm -rf .git' | sqlite3 x.db":        "sqlite3",
		"printf '.shell rm -rf .git\\n' | sqlite3 x.db":    "sqlite3",
		"gdb -q <<< 'shell rm -rf .git'":                   "gdb",
		"psql <<'EOF'\n\\! rm -rf .git\nEOF":               "psql",
		"nvim -es <<'EOF'\nset ff=unix\n!rm -rf .git\nEOF": "nvim",
		"lua <<< 'os.execute(\"rm -rf .git\")'":            "lua",
		"nvim -S <(echo '!rm -rf .git')":                   "nvim",
		"nix-shell <<< 'rm -rf .git'":                      "nix-shell",
	} {
		classAsked(t, s, c, name+" is not a program the guard knows never runs its arguments, "+held)
	}
}

// Data a program filters, writes, commits or shows keeps its verdict, and
// so does a program that never runs anything given such text.
func TestT257TextThatIsDataStaysAllowed(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range []string{
		"sqlite3 x.db <<< 'select 1'",
		"cat <<'EOF'\nrm -rf .git\nEOF",
		"sed 's/a/b/' <<< 'rm -rf .git'",
		"echo 'rm -rf .git' | awk '{print $1}'",
		"git commit -q -F - <<'EOF'\nfix: stop rm -rf .git in the cleanup\nEOF",
		"diff <(echo 'rm -rf .git') notes.txt",
		"diff <(sort a.txt) <(sort b.txt)",
		"nvim -es -c 'set ff=unix' -c wq notes.txt",
		"gdb -q -batch -ex bt ./a.out",
		"tee notes.txt <<< 'rm -rf .git'",
	} {
		allowed(t, s, c)
	}
}

// A shell, a sourced file, an interpreter or make given a `<(…)` file runs
// it as it would run the same text on its input: the bare verdict.
func TestT257ProcessSubstitutionIsTheScriptItsProgramRuns(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range []string{
		"bash <(echo 'rm -rf .git')",
		"source <(echo 'rm -rf .git')",
		". <(echo 'rm -rf .git')",
		"sh <(printf 'rm -rf .git\\n')",
		"bash < <(echo 'rm -rf .git')",
		"make -f <(printf 'x:\\n\\trm -rf .git\\n')",
	} {
		refused(t, s, c, t257Git)
	}
	refused(t, s, "python3 <(echo 'import shutil; shutil.rmtree(\".git\")')",
		"python3 code: it writes or deletes a VCS directory, the grants directory, or a Claude Code settings file")
	allowed(t, s, "source <(echo 'echo hi')")
}

// An editor's own delete and a list of words a language hands a program
// are read as the guard reads an interpreter's code; perl's qw() and
// ruby's %w[] are lists too.
func TestT257EditorDeletesAndListArguments(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	const code = "they hold code: it writes or deletes a VCS directory, the grants directory, or a Claude Code settings file, which it would refuse"
	for c, name := range map[string]string{
		"nvim --headless -c 'call delete(\".git\", \"rf\")' -c q":               "nvim",
		"nvim --headless -c 'lua vim.fn.delete(\".git\", \"rf\")' -c q":         "nvim",
		"nvim --headless -c 'lua vim.fs.rm(\".git\", {recursive = true})' -c q": "nvim",
		"emacs --batch --eval '(delete-directory \".git\" t)'":                  "emacs",
	} {
		classAsked(t, s, c, name+" is not a program the guard knows never runs its arguments, and "+code)
	}
	classAsked(t, s, "nvim --headless -c 'call delete(expand(\"~\"), \"rf\")' -c q",
		"nvim is not a program the guard knows never runs its arguments, and they hold code deleting ~: it reaches the home directory, which it would refuse")
	for _, c := range []string{
		"nvim -c 'call system([\"rm\", \"-rf\", \".git\"])' -c q",
		"nvim --headless -c 'lua vim.fn.system({\"rm\", \"-rf\", \".git\"})' -c q",
	} {
		classAsked(t, s, c, "nvim is not a program the guard knows never runs its arguments, and they hold `rm -rf .git`, which it would refuse")
	}
	refused(t, s, "perl -e 'system qw(rm -rf .git)'", "perl code running "+t257Git)
	refused(t, s, "perl -e 'system(qw[rm -rf .git])'", "perl code running "+t257Git)
	refused(t, s, "ruby -e 'system(*%w[rm -rf .git])'", "ruby code running "+t257Git)
	for _, c := range []string{
		"perl -e 'print qw(a b c)'",
		"python3 -c 'import requests; requests.delete(\"http://localhost:8000/items/1\")'",
		"nvim --headless '+Lazy! sync' +qa",
		"nvim --headless -c 'lua print(vim.fn.stdpath(\"data\"))' -c q",
	} {
		allowed(t, s, c)
	}
}

// A variable named as programs name the one whose value they run (a
// pager, an editor, a browser, an askpass helper, *_COMMAND, *_CMD) is
// read as the command it runs, set before the program, exported or given
// to env; a browser list is split at `:`.
func TestT257CommandVariablesByTheirName(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range []string{
		"SYSTEMD_PAGER='rm -rf .git' systemctl status",
		"script -qec \"SYSTEMD_PAGERSECURE=0 SYSTEMD_PAGER='rm -rf .git' systemctl status\" /dev/null",
		"BROWSER='rm -rf .git' python3 -m webbrowser http://localhost:8000",
		"BROWSER='firefox:rm -rf .git' xdg-open http://localhost:8000",
		"export BROWSER='rm -rf .git'; xdg-open http://localhost:8000",
		"GH_BROWSER='rm -rf .git' gh browse",
		"env FZF_DEFAULT_COMMAND='rm -rf .git' fzf --filter a",
		"KUBE_EDITOR='rm -rf .git' kubectl edit deploy/x",
		// The list's own entries keep their reading.
		"PAGER='rm -rf .git' man ls",
	} {
		refused(t, s, c, t257Git)
	}
	for _, c := range []string{
		"BROWSER=firefox xdg-open http://localhost:8000",
		"BROWSER=/usr/bin/firefox:/usr/bin/chromium xdg-open http://localhost:8000",
		"FZF_DEFAULT_COMMAND='fd --type f' fzf --filter a",
		"EDITOR=nvim git commit",
		"MANPAGER='nvim +Man!' man ls",
		"SYSTEMD_PAGER= systemctl status",
		"build_cmd='go build ./...'; $build_cmd",
	} {
		allowed(t, s, c)
	}
}

// A git configuration file the line points git at (GIT_CONFIG_GLOBAL,
// GIT_CONFIG_SYSTEM, -c or --config-env include.path) is read as git reads
// it, and each setting as -c's is.
func TestT257GitConfigurationFilesAreRead(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	bad, ok := filepath.Join(s.scratch, "bad.cfg"), filepath.Join(s.scratch, "ok.cfg")
	support.Write(t, s.scratch, map[string]string{
		"bad.cfg": "[alias]\n\tx = !rm -rf .git\n[core]\n\tfsmonitor = rm -rf .git\n",
		"ok.cfg":  "[user]\n\tname = me\n[core]\n\tpager = less -R\n[include]\n\tpath = " + bad + "\n",
	})
	for _, c := range []string{
		"GIT_CONFIG_GLOBAL=" + bad + " git x",
		"GIT_CONFIG_SYSTEM=" + bad + " git status",
		"export GIT_CONFIG_GLOBAL=" + bad + "; git status",
		"git -c include.path=" + bad + " x",
		"git -c includeIf.onbranch:main.path=" + bad + " status",
		"C=" + bad + " git --config-env=include.path=C status",
		// An include in the file is read too.
		"GIT_CONFIG_GLOBAL=" + ok + " git status",
	} {
		refused(t, s, c, t257Git)
	}
	support.Write(t, s.scratch, map[string]string{"plain.cfg": "[user]\n\tname = me\n[core]\n\tpager = less -R\n"})
	plain := filepath.Join(s.scratch, "plain.cfg")
	for _, c := range []string{
		"GIT_CONFIG_GLOBAL=/dev/null git status",
		"GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null git log --oneline -1",
		"GIT_CONFIG_GLOBAL=" + plain + " git log --oneline -1",
		"git -c include.path=" + plain + " status",
		"GIT_CONFIG_GLOBAL=" + filepath.Join(s.scratch, "missing.cfg") + " git status",
	} {
		allowed(t, s, c)
	}
}

// route-guard trusts a program that never runs its arguments only by a
// name the line does not define as a function or an alias, as the safety
// guard's class rule does (T228 L8).
func TestT257EnvChangeDoesNotTrustARedefinedName(t *testing.T) {
	t.Parallel()
	m := newMG(t)
	for c, name := range map[string]string{
		`ls() { "$@"; }; ls env -u JHON_AGENT git status`:   "ls",
		`cat() { "$@"; }; cat env -u JHON_AGENT git status`: "cat",
	} {
		r := m.bash(t, m.p, c)
		code(t, r, 2, c)
		if want := "route-guard: `" + c + "` would turn off the merge gate git runs in this repository (hook.jhon-merge-gate, T219): " + name +
			" may run its words: it changes JHON_AGENT: "; !strings.HasPrefix(r.Stderr, want) {
			t.Errorf("%q: %q, want it to open with %q", c, r.Stderr, want)
		}
	}
	for _, c := range []string{"ls env -u JHON_AGENT", "echo env -u JHON_AGENT", "f() { ls; }; f && git status"} {
		code(t, m.bash(t, m.p, c), 0, c)
	}
}
