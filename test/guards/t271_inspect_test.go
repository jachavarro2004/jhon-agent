// T271 rule 1, read-only by construction: an inspecting agent's line is
// judged by what it runs. A line made only of programs that cannot write,
// run a program the guard cannot read, or reach the network passes, however
// it is spelt (pipes, `&&`, quoting, subshells, loops, substitutions); a line
// that runs anything else is refused with the program named and a fix. The
// guard is a filter, not a container: the sandbox contains.

package guards

import (
	"strconv"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// readOnlyForms are the inspect forms an inspector is rendered with: Jhon's
// own readers, which no generic rule reads.
var readOnlyForms = append(append([]string{}, inspectAllow...), "codegraph explore", "codegraph query")

// roLine runs inspect-guard for a reviewer on command, run in cwd, with env (support.Env() when nil).
func roLine(t *testing.T, env []string, cwd, command string) support.Result {
	t.Helper()
	return hook(t, env, "inspect-guard", append([]string{"--agent", "prism-safety"}, readOnlyForms...),
		map[string]any{"tool_name": "Bash", "cwd": cwd, "tool_input": cmd(command), "agent_type": "prism-safety"})
}

// roRefused fails unless r refuses with the guard's name, a reason and a fix.
func roRefused(t *testing.T, r support.Result, label string) {
	t.Helper()
	if r.Code != 2 {
		t.Errorf("%s: exit %d, want a refusal: %s%s", label, r.Code, r.Stdout, r.Stderr)
		return
	}
	first := strings.Split(r.Stderr, "\n")[0]
	matches(t, first, `^inspect-guard: \S.*\. Instead: \S`, label+": the refusal names what and a fix")
}

func TestT271InspectReadOnlyLinesPass(t *testing.T) {
	t.Parallel()
	r := newInspectRepos(t)
	support.Write(t, r.project, map[string]string{"src/a.go": "package a\n", "docs/b.md": "alpha\n", "pkg.json": "{\"name\": \"a\"}\n", ".env.example": "KEY=\n"})
	for _, command := range []string{
		"git grep -n -e alpha -e beta HEAD -- src/a.go docs/b.md",
		`git grep -n "alpha\|beta" HEAD -- src`,
		`git grep -n alpha\|beta HEAD -- src`,
		`git log --oneline -3 -S"(\"x\", \"y\"" HEAD -- src`,
		"git cat-file -t HEAD",
		"git ls-tree -r --name-only HEAD -- src",
		"git blame -L 1,1 HEAD -- README.md",
		"git diff HEAD~1 HEAD -- . ':!docs/old'",
		"git diff HEAD -- 'src/*/a.go'",
		"git diff HEAD -- src/*.go",
		"git -C " + r.other + " log --oneline -3",
		"cd " + r.other + " && git status --short",
		"git -C " + r.worktree + " grep -n alpha HEAD -- src",
		"git diff HEAD | grep -n alpha | head -20",
		"git log --oneline | wc -l && git status --short",
		"for f in src/a.go docs/b.md; do grep -c alpha \"$f\"; done",
		"grep -rn alpha src 2>/dev/null | sort | uniq -c",
		"find src -name '*.go' -exec grep -l package {} +",
		"git diff HEAD~1 -- src/$(echo a).go",
		"rg -n alpha -g '*.go' src | head",
		"cat src/a.go | tr a-z A-Z | cut -c1-20",
		"ls -la; pwd; echo done",
		"codegraph explore alpha",
		"codegraph callers alpha | head -5",
		"while read -r line; do echo \"$line\"; done < src/a.go",
		"x=src; ls \"$x\"",
		"test -f src/a.go && head -5 src/a.go || echo none",
		"(cd src && ls)",
		"git diff HEAD >/dev/null 2>&1; echo $?",
		"jq .name pkg.json",
		"grep -n KEY .env.example",
		"git ls-files | xargs grep -l alpha",
		"diff <(git show HEAD:README.md) README.md",
		"git branch --list 'feat*'",
		"git tag -l",
		"git config --get user.name",
		"git stash list",
		"git worktree list",
		"date +%F",
	} {
		t.Run(strconv.Quote(command), func(t *testing.T) {
			t.Parallel()
			code(t, roLine(t, nil, r.project, command), 0, command)
		})
	}
}

func TestT271InspectRefusesWhatCanWriteRunOrFetch(t *testing.T) {
	t.Parallel()
	r := newInspectRepos(t)
	for _, command := range []string{
		// A write through a listed program, a redirection or a writing subcommand.
		"git diff > out.txt", "git diff 2>err.txt", "git diff | tee out.txt", "sort -o out.txt a.txt", "sort --output=out.txt a.txt",
		"find src -delete", "find src -fprint out.txt", "find src -name x -exec rm {} +", "find src -execdir sh -c 'echo x' \\;", "find src -okdir rm {} \\;",
		"uniq a.txt out.txt", "xxd a.bin out.hex", "tree -o out.txt", "file -C -m magic", "date -s 2020-01-01", "base64 -o out a",
		"touch a", "mkdir d", "cp a b", "sed -i s/a/b/ a.txt", "sed -n p a.txt", "awk '{print}' a.txt",
		"git checkout .", "git branch newb", "git tag v9", "git config user.name x", "git stash drop", "git worktree add ../w",
		"git reflog expire --all", "git -c core.pager=sh log", "git --exec-path=/x log", "git diff --output=f", "git diff --outp=f",
		"git diff --ext-diff", "git show --textconv HEAD", "git grep -O alpha", "git grep --open-files-in-pager=sh alpha",
		"git cat-file --filters HEAD:a", "codegraph init .", "codegraph sync",
		// A program the guard cannot read, or one run through another.
		"rg --pre=sh alpha", "rg --pre sh alpha", "rg -z alpha", "rg --search-zip alpha", "fd -x rm", "fd -HX rm", "xargs rm < list.txt", "git ls-files | xargs sort -o x",
		"python3 -c 'print(1)'", "bash -c 'ls'", "eval ls", "source x.sh", ". x.sh", "f() { ls; }; f", "alias ls=rm; ls",
		"./run.sh", "/usr/bin/cat a", "$cmd a", "timeout 5 git log", "command ls", "exec ls", "time ls", "ls &", "coproc ls",
		"trap 'rm x' EXIT", "declare x=1", "let x=1", "[[ -f a ]]", "echo $((1+2))", "echo ${x@P}", "echo ${!x}",
		// The environment a program runs with, and the session's own.
		"PATH=/tmp git log", "export x=1", "X=1 git log", "HOME=/tmp ls", "read HOME", "for PATH in a; do ls; done",
		"printf -v IFS x", "printenv", "env",
		// A word the shell may turn into an option of a program that has a writing one.
		"git diff *", "rg alpha *", "find * -name x", "sort ?.txt", "git diff \"$(cat rev.txt)\"",
		// The network, other users, other processes.
		"curl https://example.invalid", "wget https://example.invalid", "git fetch", "git ls-remote origin", "git remote show origin",
		"gh pr view 1", "ssh host ls", "sudo ls", "kill 1", "pkill x",
		// A line that does not parse.
		"git diff -G a_(b|c)_d", "git diff -G a|b|c -- x",
	} {
		t.Run(strconv.Quote(command), func(t *testing.T) {
			t.Parallel()
			roRefused(t, roLine(t, nil, r.project, command), command)
		})
	}
}

// An assigned name is lowercase and absent from the environment the hook
// runs in: reassigning an exported name changes what the next program is.
func TestT271InspectAssignedNamesStayOutOfTheEnvironment(t *testing.T) {
	t.Parallel()
	r := newInspectRepos(t)
	env := support.Env("t271_present=1")
	code(t, roLine(t, env, r.project, "t271_absent=src; ls \"$t271_absent\""), 0, "a lowercase name absent from the environment")
	for _, command := range []string{"t271_present=src; ls", "read -r t271_present < a.txt", "for t271_present in a; do ls; done", "t271_present=1 git log"} {
		roRefused(t, roLine(t, env, r.project, command), command)
	}
}

// Reading a credentials file is refused to an inspector as to every agent
// (S10); a template, an exclusion and a search that merely recurses pass.
func TestT271InspectCredentialContentsRefused(t *testing.T) {
	t.Parallel()
	r := newInspectRepos(t)
	support.Write(t, r.project, map[string]string{".env": "KEY=1\n", ".env.example": "KEY=\n", "conf/id_ed25519": "k\n", "src/a.go": "package a\n"})
	for _, command := range []string{"cat .env", "grep -n KEY .env", "git show HEAD:.env", "head -1 conf/id_*", "rg KEY -g 'id_ed25519*' ."} {
		r := roLine(t, nil, r.project, command)
		if r.Code != 2 || !strings.Contains(r.Stderr, "credentials") {
			t.Errorf("%s: exit %d, want a credentials refusal: %s", command, r.Code, r.Stderr)
		}
	}
	for _, command := range []string{"cat .env.example", "rg KEY -g '!.env*' .", "git diff HEAD -- . ':!.env' ':(exclude)conf'", "grep -rn KEY src", "ls -la conf"} {
		code(t, roLine(t, nil, r.project, command), 0, command)
	}
}

// pi's inspect tool hands the guard the line it runs; with no forms given,
// Jhon's own readers are refused and read-only programs still pass.
func TestT271InspectWithoutFormsStillReadsReadOnlyLines(t *testing.T) {
	t.Parallel()
	r := newInspectRepos(t)
	run := func(command string) support.Result {
		return hook(t, nil, "inspect-guard", nil, map[string]any{"tool_name": "Bash", "cwd": r.project, "tool_input": cmd(command)})
	}
	code(t, run("git log --oneline -1 | cat"), 0, "a read-only line")
	roRefused(t, run("codegraph callers x"), "a reader no form names")
}
