package guards

import "testing"

// t228L7Data are lines that name a removal only as data to a program that
// never runs its arguments, by its own bare name, with no function or
// alias of that name on the line: they keep their verdict.
var t228L7Data = []string{
	"ls src", "cat notes.txt", `echo "rm -rf ~"`, `printf '%s\n' 'rm -rf ~'`, "printf -v x '%s' y", "test -v HOME",
	"[ -v HOME ]", `print -P '%F{red}x%f'`, "grep -rn 'rm -rf' .", "rg 'rm -rf ~' docs", "git -c core.sshCommand='ssh -i k' fetch",
	"GIT_SSH_COMMAND='ssh -o BatchMode=yes' git fetch", "EDITOR=vim git commit", "PAGER=cat git log", "git difftool -y",
	"git grep -O x", "git clone . x", "sed -n '/rm -rf/p' x.sh", "sed 's/a/b/' x", "ls *(e:'[[ -d $REPLY ]]':)",
}

// TestT228L7Blockers: each is refused or unreadable, never allowed.
func TestT228L7Blockers(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range t228L7Block {
		notAllowed(t, s, c)
	}
}

// TestT228L7AllowedAndData: harmless e strings and data keep their verdict.
func TestT228L7AllowedAndData(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range append(append([]string{}, t228L7Allow...), t228L7Data...) {
		allowed(t, s, c)
	}
}

// TestT228L7KnownHoles pins the lines L8 left outside this round: they are allowed as before.
func TestT228L7KnownHoles(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range t228L7Known {
		allowed(t, s, c)
	}
}
