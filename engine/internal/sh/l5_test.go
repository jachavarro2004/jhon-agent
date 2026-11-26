package sh

import (
	"strings"
	"testing"
)

// T228 L5: the reader's blockers, read as the bare command.
func TestL5ReaderBlockers(t *testing.T) {
	for src, want := range map[string]string{
		// Wrapper options with a separate value, and clusters.
		`nice --adjustment 5 git push`:      "nice | git push",
		`stdbuf --output L git push`:        "stdbuf | git push",
		`stdbuf -oL -e 0 git push`:          "stdbuf | git push",
		`env -a x git push`:                 "env | git push",
		`env --argv0 x git push`:            "env | git push",
		`env -vS'git push'`:                 "env | git push",
		`env -iS'git push'`:                 "env | git push",
		`exec -la x git push`:               "exec | git push",
		`exec -cla x git push`:              "exec | git push",
		`/usr/bin/time -o x -f %e git push`: "/usr/bin/time time | git push",
		`timeout --signal KILL 5 git push`:  "timeout | git push",
		`sudo -Eu root git push`:            "sudo | git push",
		// Runners the reader now reads.
		`flock /tmp/l git push`:                 "flock | git push",
		`flock -c 'git push' /tmp/l`:            "flock | git push",
		`busybox env git push`:                  "busybox env | git push",
		`git -c alias.x='!git push' x`:          "git | git push",
		`git -c alias.p='push origin' p`:        "git | git push origin",
		`git bisect run git push`:               "git | git push",
		`git rebase -x 'git push' main`:         "git | git push",
		`git submodule foreach git push`:        "git | git push",
		`echo '* * * * * git push' | crontab -`: "crontab <stdin> | git push",
		// An alias defined then used.
		"alias p='git push'\np origin":  "| git push origin",
		"alias x=git; x push":           "| git push",
		"alias -g G='git push'\necho G": "| echo git push",
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
	// Ordinary git lines read no command of their own.
	for _, src := range []string{`git -c user.name='A B' commit -m x`, `git -c core.quotepath=off status`, `git log --grep=rm`, `git config user.name 'A B'`} {
		if in, ok, err := Unwrap(strings.Fields(strings.NewReplacer("'", "").Replace(src))); ok || err != nil {
			t.Errorf("%q: reads %+v %v", src, in, err)
		}
	}
	for line, want := range map[string]string{
		"emulate csh": "emulate csh", "emulate -L csh": "emulate csh", "zsh --emulate csh -c x": "zsh --emulate csh",
		"setopt $o": "setopt $o", "emulate -c x sh": "emulate sh", "set -o $o": "set -o $o",
	} {
		w := strings.Fields(line)
		if got := GlobSubstOn(w[0], w[1:]); got != want {
			t.Errorf("%q: %q, want %q", line, got, want)
		}
	}
	for src, want := range map[string]string{
		`typeset -g "options[globsubst]=on"`: `options[globsubst]=on`,
		`: ${options[globsubst]:=on}`:        `${options[globsubst]:=on}`,
	} {
		cmds, err := Parse(src)
		if err != nil || len(cmds) < 2 || strings.Join(cmds[0].Words, " ") != GlobSubst+" "+want {
			t.Errorf("Parse(%q) = %v %v, want a GlobSubst command %q", src, cmds, err, want)
		}
	}
}

// T228 L5: CronCommands is each entry's command.
func TestCronCommands(t *testing.T) {
	got := CronCommands("# c\nMAILTO=x\n*/5 * * * * cd /x && make a % input\n@reboot git push\n\n")
	if got != "cd /x && make a \ngit push" {
		t.Errorf("CronCommands = %q", got)
	}
}
