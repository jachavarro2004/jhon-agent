// T228 L2: the same class as S1, a command the guard never saw. External
// runners (setsid, ionice, chrt, taskset, strace, watch, unbuffer and their
// kin) run the command after their own options; builtins and assignments
// run a string later (trap, zsh's sched and zpty, an alias, PROMPT_COMMAND,
// a prompt's substitutions, zsh's functions[…]). The guard reads each such
// command as it reads it bare. A runner option the reader does not know
// leaves the command unknown: the line is unreadable, never allowed as safe.
// A line that turns on zsh's GLOB_SUBST (setopt, set -o, unsetopt no…,
// emulate sh or ksh, options[globsubst]=on, zsh -o) is refused: each
// unquoted expansion is then a glob whose qualifiers may run code.

package guards

import (
	"strings"
	"testing"
)

// t228Runners are the forms that run a command, %s standing for it.
var t228Runners = []string{
	"setsid %s", "setsid -f %s", "setsid --wait %s", "ionice -c 3 %s", "ionice -c3 -n7 %s", "ionice --class 2 %s",
	"chrt -f 99 %s", "chrt 10 %s", "chrt --sched-runtime 5 -d 0 %s", "taskset -c 0 %s", "taskset 0x1 %s", "taskset -a -- 3 %s",
	"strace -f -o x %s", "strace -ff -e trace=file %s", "strace -fo x %s", "strace --output x %s", "ltrace -o x %s",
	"watch -x %s", "watch -n1 -x %s", "unbuffer -p %s", "chroot / %s", "chroot --userspec u:g / %s",
	"setpriv --reuid 0 --clear-groups %s", "nsenter -t 1 -m %s", "nsenter -t1 -mfile %s", "unshare -r %s", "unshare --map-user 0 -f %s",
	"firejail --noprofile %s", "firejail --private=/x -- %s", "bwrap --bind / / --dev /dev --unshare-all %s",
	"cgexec -g cpu:x %s", "cgexec -g cpu:x --sticky %s", "numactl -i all %s", "numactl --membind=0 %s",
	"prlimit --nofile=10 %s", "prlimit -n10 %s", "faketime 2020-01-01 %s", "faketime -f +1d %s", "xvfb-run -a %s",
	"xvfb-run -s '-screen 0 1x1x8' %s", "catchsegv %s", "valgrind --tool=none -q %s", "perf record -g -- %s",
	"perf stat -e cycles -- %s", "eatmydata %s", "fakeroot %s", "fakeroot -u -- %s", "gosu root %s", "su-exec root %s",
	"setuidgid u %s", "runuser -u x -- %s", "chronic -e %s", "ifne -n %s", "lckdo -q /tmp/l %s",
	"systemd-run --user %s", "systemd-run --user -p X=1 --wait %s", "systemd-run -u x -E A=1 %s",
	"nice -n 5 %s", "stdbuf -o0 %s", "timeout -s KILL 5 %s", "nohup %s", "env A=1 %s",
	"gdb -batch -ex run --args %s", "setsid chrt 1 taskset 1 %s", "- setsid %s", "repeat 2 strace %s",
}

// t228Strings are the forms that run a string later, %s standing for it.
var t228Strings = []string{
	"watch '%s'", "watch -n1 '%s'", "sg wheel -c '%s'", "sg wheel '%s'", "pee '%s'", "gdb -batch -ex 'shell %s'",
	"gdb -batch -ex '!%s'", "expect -c 'spawn %s'", "expect -c 'exec %s'", "expect -c 'system %s'", "runuser -c '%s'",
	"trap '%s' EXIT", "trap -- '%s' ERR INT", "trap '%s'", "sched +1 %s", "sched -o 10:00 %s", "zpty -b w '%s'", "zpty w %s",
	"alias x='%s'", "alias -g x='%s' y=z", "PROMPT_COMMAND='%s' bash -i", "export PROMPT_COMMAND='%s'",
	"PS4='$(%s)' bash -xc true", "export PS4='`%s`'; set -x; true", "PS1='$(%s)' zsh -i", "functions[x]='%s'",
	"aliases[x]='%s'", "env 'PS4=$(%s)' bash -xc true",
}

// t228Unknown are runners with an option the reader does not know, before a command.
var t228Unknown = []string{
	"strace --no-such-option %s", "chrt --bogus 5 %s", "setsid -Q %s", "ionice -z %s", "perf record -g %s",
	"gdb -batch -ex 'call system(1)' %s", "gdb -x cmds %s", "expect cmds.exp %s",
}

// t228RunnersAllowed are ordinary lines through the same programs, and option lines that are not GLOB_SUBST.
var t228RunnersAllowed = []string{
	"setsid -f ls", "watch -n1 'git status'", "watch -n 2 -x git status", "strace -c ls", "strace -f -o out ls src",
	"trap 'echo done' EXIT", "trap - EXIT", "trap -p", "trap EXIT", "taskset -p 1234", "taskset -pc 0 1234", "ionice -p 1",
	"chrt -m", "chrt -p 1234", "numactl -H", "sched", "sched -1", "alias ll='ls -l'", "alias", "nice ls", "timeout 5 ls",
	"setopt extendedglob", "setopt nullglob", "emulate -L zsh", "set -o pipefail", "setopt noglobsubst", "unsetopt globsubst",
	"set +o globsubst", "emulate zsh -c 'ls'", "PS1='> '", "export PS4='+ '", "gdb -batch -ex bt -ex 'info registers' ./a.out",
	"valgrind --leak-check=full ./a.out", "perf stat -- ls", "unshare -r -- ls", "xvfb-run -a npm test", "zpty -d w",
}

// t228GlobSubst are lines that turn on GLOB_SUBST.
var t228GlobSubst = []string{
	"setopt globsubst; ls $g", "setopt GLOB_SUBST", "setopt glob_subst", "setopt -- GlobSubst", "set -o globsubst",
	"set -o glob_subst", "set +o noglobsubst", "unsetopt NO_GLOB_SUBST", "emulate sh", "emulate -L ksh -c 'ls $g'",
	"emulate -R sh", "emulate zsh -o globsubst", "options[globsubst]=on", "options[GLOB_SUBST]=on; ls $g",
	"zsh -o globsubst -c 'ls $g'", "zsh --globsubst -c x", "zsh --emulate sh -c x", "zsh -o glob_subst x.zsh",
	"setopt -m 'glob*'", "setopt -m '*subst'", "builtin setopt globsubst", "eval 'setopt globsubst'",
	"sh -c 'setopt globsubst'", "noglob setopt GLOB_SUBST",
}

func t228Form(form, bare string) string { return strings.ReplaceAll(form, "%s", bare) }

// TestT228RunnersReadAsBare: each runner before a bare command gets the bare command's verdict, word for word.
func TestT228RunnersReadAsBare(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, b := range t228Bare {
		bare := s.expand(b)
		for _, o := range []sgOpts{{}, top()} {
			want := s.bash(t, bare, o)
			for _, f := range append(append([]string{}, t228Runners...), t228Strings...) {
				c := t228Form(f, bare)
				if got := s.bash(t, c, o); got.Code != want.Code || got.Stderr != want.Stderr || got.Stdout != want.Stdout {
					t.Errorf("top %v: %q gives %d %q %q; bare, %d %q %q", o.top, c, got.Code, got.Stderr, got.Stdout, want.Code, want.Stderr, want.Stdout)
				}
			}
		}
	}
}

// TestT228UnknownRunnerOptionsAreUnreadable: an option the reader does not know leaves the command unknown.
func TestT228UnknownRunnerOptionsAreUnreadable(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, f := range t228Unknown {
		for _, bare := range []string{"rm -rf ~", "ls"} {
			c := t228Form(f, bare)
			sub := s.bash(t, c, sgOpts{})
			code(t, sub, 2, c)
			if !strings.HasPrefix(sub.Stderr, "safety-guard: unreadable: the command cannot be read safely (") {
				t.Errorf("%q: not unreadable: %s", c, sub.Stderr)
			}
			tp := s.bash(t, c, top())
			code(t, tp, 0, c)
			if d, _, ok := decision(t, tp.Stdout); !ok || d != "ask" {
				t.Errorf("top: %q: no ask: %q", c, tp.Stdout)
			}
		}
	}
}

// TestT228GlobSubstIsRefused: a line that turns on GLOB_SUBST is refused, at the top level too.
func TestT228GlobSubstIsRefused(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range t228GlobSubst {
		for _, o := range []sgOpts{{}, top()} {
			r := s.bash(t, c, o)
			code(t, r, 2, c)
			if !strings.HasPrefix(r.Stderr, "safety-guard: refused: ") || !strings.Contains(r.Stderr, ": it turns on zsh's GLOB_SUBST, which reads each unquoted expansion as a glob, whose qualifiers may run code the guard does not read") {
				t.Errorf("top %v: %q: %s", o.top, c, r.Stderr)
			}
		}
	}
	r := s.bash(t, "setopt GLOB_SUBST", sgOpts{})
	if !strings.HasPrefix(r.Stderr, "safety-guard: refused: setopt GLOB_SUBST: it turns on") {
		t.Errorf("the refusal does not name the option: %s", r.Stderr)
	}
}

// TestT228RunnerLinesStayAllowed: ordinary lines through the same programs keep their verdicts.
func TestT228RunnerLinesStayAllowed(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range t228RunnersAllowed {
		for _, o := range []sgOpts{{}, top()} {
			r := s.bash(t, c, o)
			code(t, r, 0, c)
			if r.Stdout != "" || r.Stderr != "" {
				t.Errorf("top %v: %q: output %q %q", o.top, c, r.Stdout, r.Stderr)
			}
		}
	}
}

// TestT228XargsRunnersReadTheirCommand: xargs running a wrapper on input it
// does not show is read as xargs running the wrapped program bare: asked.
func TestT228XargsRunnersReadTheirCommand(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, o := range []sgOpts{{}, top()} {
		want := s.bash(t, "find . | xargs rm -rf", o)
		for _, w := range []string{"nohup", "setsid", "timeout 5", "strace -f", "nice -n 5", "chrt 1"} {
			c := "find . | xargs " + w + " rm -rf"
			if got := s.bash(t, c, o); got.Code != want.Code || got.Stderr != want.Stderr || got.Stdout != want.Stdout {
				t.Errorf("top %v: %q gives %d %q %q; bare, %d %q %q", o.top, c, got.Code, got.Stderr, got.Stdout, want.Code, want.Stderr, want.Stdout)
			}
		}
	}
}

// TestT228ExitTrapRemovesTheScratchFolder: an EXIT trap removing the line's
// own mktemp -d folder gets the verdict of the same removal at the line's
// end; any other trap action keeps its own reading.
func TestT228ExitTrapRemovesTheScratchFolder(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for trapped, bare := range map[string]string{
		`d=$(mktemp -d); trap 'rm -rf "$d"' EXIT; ls`:            `d=$(mktemp -d); ls; rm -rf "$d"`,
		`d=$(mktemp -d); trap -- 'rm -rf "$d"' EXIT INT; ls`:     `d=$(mktemp -d); ls; rm -rf "$d"`,
		`trap 'rm -rf "$d"' 0; d=$(mktemp -d) && ls`:             `d=$(mktemp -d) && ls; rm -rf "$d"`,
		`d=$(mktemp -d); trap 'rm -rf "$d"' EXIT; d=/; ls`:       `d=$(mktemp -d); d=/; ls; rm -rf "$d"`,
		`d=$(mktemp -d); trap 'rm -rf "$d" ~' EXIT; ls`:          `d=$(mktemp -d); ls; rm -rf "$d" ~`,
		`d=$(mktemp -d); trap 'rm -rf "$d"' DEBUG; ls`:           `eval 'rm -rf "$d"'`,
		`d=$(mktemp -d); trap 'rm -rf "$d"/*' EXIT; ls`:          `d=$(mktemp -d); ls; rm -rf "$d"/*`,
		`d=$(mktemp -d); trap 'rm -rf $d' EXIT; trap - EXIT; ls`: `eval 'rm -rf $d'`,
	} {
		for _, o := range []sgOpts{{}, top()} {
			got, want := s.bash(t, trapped, o), s.bash(t, bare, o)
			if got.Code != want.Code || (got.Code == 0) != (want.Code == 0) || (got.Stderr == "") != (want.Stderr == "") {
				t.Errorf("top %v: %q gives %d %q; %q, %d %q", o.top, trapped, got.Code, got.Stderr, bare, want.Code, want.Stderr)
			}
		}
	}
}
