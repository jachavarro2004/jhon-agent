package guards

import "testing"

// T228 L12: a transport's local commands keep every destructive and
// local-harm reading; only their outward classes are the git command's own
// connection. Bare ssh lines, and a bastion command outside a transport
// value, are judged as before.
func TestT228L12TransportKeepsLocalHarm(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range []string{
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="ssh -W %h:%p bastion; rm -rf ~"' git fetch`,
		`git -c core.sshCommand='ssh -o ProxyCommand="rm -rf ~"' fetch`,
		`X='ssh -o ProxyCommand="rm -rf ~"' git --config-env=core.sshCommand=X fetch`,
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="git push --force"' git fetch`,
	} {
		code(t, s.bash(t, c, sgOpts{}), 2, c)
		code(t, s.bash(t, c, top()), 2, c)
	}
	for _, c := range []string{
		"git -c core.sshCommand='ssh -o ProxyCommand=\"ssh -W %h:%p bastion\"' fetch",
		"X='ssh -o ProxyCommand=\"ssh -W %h:%p bastion\"' git --config-env=core.sshCommand=X fetch",
		"export GIT_SSH_COMMAND='ssh -o ProxyCommand=\"ssh -W %h:%p bastion\"'; git fetch",
	} {
		allowed(t, s, c)
	}
	// Not widened: a bare ssh line is asked as it was, and so is a pager that is ssh.
	for _, c := range []string{"ssh -W x:22 bastion", "ssh -o ProxyCommand='ssh -W %h:%p bastion' host", "PAGER='ssh bastion' git log"} {
		code(t, s.bash(t, c, sgOpts{}), 2, c)
		if r := s.bash(t, c, top()); r.Code != 0 || r.Stdout == "" {
			t.Errorf("top: %q not asked: %d %q", c, r.Code, r.Stdout)
		}
	}
}
