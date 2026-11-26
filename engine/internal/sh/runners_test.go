package sh

import (
	"errors"
	"strings"
	"testing"
)

// route is each program of src: its route, then its argv.
func route(t *testing.T, src string) ([]string, error) {
	t.Helper()
	cmds, err := Parse(src)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, c := range cmds {
		ps, err := CommandPrograms(c, nil, nil, Opts{})
		if err != nil {
			return nil, err
		}
		for _, p := range ps {
			out = append(out, strings.TrimSpace(strings.Join(p.Via, " ")+" | "+strings.Join(p.Argv, " ")))
		}
	}
	return out, nil
}

// T228 L2: a runner's command is read through its options; a builtin's or
// an assignment's string that runs later is read as a command line.
func TestRunnersReadTheirCommand(t *testing.T) {
	for src, want := range map[string]string{
		`chrt -f 99 git push`:                   "chrt | git push",
		`taskset -c 0 git push`:                 "taskset | git push",
		`strace -fo out -e trace=file git push`: "strace | git push",
		`ionice -c3 -n 7 git push`:              "ionice | git push",
		`nsenter -t 1 -mfile git push`:          "nsenter | git push",
		`bwrap --bind / / --dev /dev git push`:  "bwrap | git push",
		`firejail --private=/x git push`:        "firejail | git push",
		`watch -n1 'git push'`:                  "watch | git push",
		`watch -x git push`:                     "watch | git push",
		`sg wheel -c 'git push'`:                "sg | git push",
		`gdb -batch -ex 'shell git push'`:       "gdb | git push",
		`gdb -batch -ex run --args git push`:    "gdb | git push",
		`expect -c 'spawn -noecho git push'`:    "expect | git push",
		`runuser -u x -- git push`:              "runuser | git push",
		`perf stat -e x -- git push`:            "perf | git push",
		`trap -- 'git push' EXIT`:               "trap | git push",
		`sched +1:00 git push`:                  "sched | git push",
		`zpty -b w git push`:                    "zpty | git push",
		`alias p='git push'`:                    "alias | git push",
		`emulate sh -c 'git push'`:              "| emulate sh -c git push",
		`systemd-run --user -p X=1 git push`:    "systemd-run | git push",
		`taskset -p 1234`:                       "| taskset -p 1234",
		`trap - EXIT`:                           "| trap - EXIT",
		`chrt -m`:                               "| chrt -m",
		`env 'PS4=$(git push)' bash -x`:         "env | git push",
		`find . | xargs nohup rm -rf`:           "| find .",
	} {
		got, err := route(t, src)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if !strings.Contains(strings.Join(got, "\n"), want) {
			t.Errorf("%q: programs %q, want one %q", src, got, want)
		}
	}
	if got, _ := route(t, `find . | xargs nohup rm -rf`); !strings.Contains(strings.Join(got, "\n"), "xargs <unseen input> nohup | rm -rf") {
		t.Errorf("xargs nohup: %q", got)
	}
	if got, _ := route(t, `emulate sh -c 'git push'`); len(got) != 2 || got[1] != "emulate | git push" {
		t.Errorf("emulate -c: %q", got)
	}
}

// T228 L2: Unwrap is what a program that runs another runs, one level down.
func TestUnwrap(t *testing.T) {
	for src, want := range map[string]string{
		"noglob env -i x": "words [env -i x]", "repeat 2 { env -i x }": "words [env -i x]", "setsid -f env -i x": "words [env -i x]",
		"timeout -s 9 5 env x": "words [env x]", "nice -n 5 env x": "words [env x]", "watch -n1 'env -i x'": "strings [env -i x]",
		"trap 'unset X' EXIT": "strings [unset X]", "emulate sh -c 'x'": "self strings [x]", "taskset -p 1": "self", "env -S 'a b'": "strings [a b]",
		"git status": "none",
	} {
		cmds, err := Parse(src)
		if err != nil || len(cmds) != 1 {
			t.Fatalf("Parse(%q): %v %v", src, cmds, err)
		}
		in, ok, err := Unwrap(cmds[0].Words)
		got := "none"
		if ok {
			got = ""
			if in.Self {
				got = "self "
			}
			if in.Strings != nil {
				got += "strings [" + strings.Join(in.Strings, ",") + "]"
			}
			for _, w := range in.Words {
				got += "words [" + strings.Join(w, " ") + "]"
			}
			got = strings.TrimSpace(got)
		}
		if err != nil || got != want {
			t.Errorf("Unwrap(%q) = %q, %v; want %q", src, got, err, want)
		}
	}
}

// T228 L2: an option a runner's grammar does not know leaves its command unknown.
func TestRunnersUnknownOptionsAreUnreadable(t *testing.T) {
	for _, src := range []string{`strace --nope ls`, `chrt --bogus 5 ls`, `setsid -Q ls`, `perf record -g ls`, `gdb -x f ls`,
		`gdb -batch -ex 'call system(1)'`, `expect x.exp`, `expect -c 'set c [exec ls]'`} {
		if _, err := route(t, src); !errors.As(err, new(*Unreadable)) {
			t.Errorf("%q: read as %v", src, err)
		}
	}
}

// T228 L2: what an assignment runs later is read: PROMPT_COMMAND, a prompt's
// substitutions, zsh's functions[…] and aliases[…], a subscript; and
// options[globsubst] is a GlobSubst command.
func TestAssignmentsThatRunLater(t *testing.T) {
	for src, want := range map[string][]string{
		`PROMPT_COMMAND='git push' bash -i`: {"git", "push"},
		`PROMPT_COMMAND=('git push')`:       {"git", "push"},
		`export PS4='$(git push)'`:          {"git", "push"},
		`PS1='x` + "`git push`" + `'`:       {"git", "push"},
		`functions[f]='git push'`:           {"git", "push"},
		`a[$(git push)]=1`:                  {"git", "push"},
		`options[GLOB_SUBST]=on`:            {GlobSubst, "options[GLOB_SUBST]=on"},
		`options[$k]=on`:                    {GlobSubst, "options[$k]=on"},
		`options+=(globsubst on)`:           {GlobSubst, "options=(globsubst on)"},
	} {
		if got := words(t, src); len(got) == 0 || strings.Join(got[0], " ") != strings.Join(want, " ") {
			t.Errorf("Parse(%q) = %q, want first %q", src, got, want)
		}
	}
	for _, src := range []string{`PS1='> '`, `PROMPT_COMMAND=`, `options[nullglob]=on`, `x[1]=2`, `PS4="+ "`} {
		if got := words(t, src); len(got) != 1 {
			t.Errorf("Parse(%q) = %q, want the assignment alone", src, got)
		}
	}
}

// T228 L2: GLOB_SUBST turned on, by any spelling.
func TestGlobSubstOn(t *testing.T) {
	for line, want := range map[string]string{
		"setopt GLOB_SUBST": "setopt GLOB_SUBST", "setopt -- glob-subst": "setopt glob-subst", "unsetopt No_Glob_Subst": "unsetopt No_Glob_Subst",
		"set -o globsubst": "set -o globsubst", "set +o noglobsubst": "set +o noglobsubst", "set -oglob_subst": "set -oglob_subst",
		"emulate -L sh": "emulate sh", "emulate zsh -o globsubst": "emulate -o globsubst", "emulate -R ksh -c x": "emulate ksh",
		"zsh --emulate sh -c x": "zsh --emulate sh", "zsh --glob-subst x": "zsh --glob-subst", "zsh -o globsubst -c x": "zsh -o globsubst",
		"setopt -m 'glob*'": "setopt -m glob*",
		"setopt nullglob":   "", "unsetopt globsubst": "", "setopt noglobsubst": "", "emulate zsh": "", "emulate zsh -c 'emulate'": "",
		"set -o pipefail": "", "zsh -c 'setopt'": "", "zsh -c x -o globsubst": "", "unsetopt -m 'glob*'": "",
	} {
		w := strings.Fields(strings.ReplaceAll(line, "'", ""))
		if got := GlobSubstOn(w[0], w[1:]); got != want {
			t.Errorf("%q: %q, want %q", line, got, want)
		}
	}
}
