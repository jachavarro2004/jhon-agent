// T228 L10 re-verify: the ssh grammar's edges are read, and an ordinary
// ProxyCommand transport (a bastion, SSM, cloudflared) is the connection
// git makes, not an outward act of its own.
package guards

import "testing"

func TestT228L10VerifyGrammarEdges(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range []string{
		`GIT_SSH_COMMAND='ssh -46AaCfGgKkMNnqsTtVvXxYyZoProxyCommand=rm\ -rf\ ~' git fetch`,
		`GIT_SSH_COMMAND='ssh -vl x -oProxyCommand="rm -rf ~"' git fetch`,
		`GIT_SSH_COMMAND='ssh h -vo ProxyCommand="rm -rf ~"' git fetch`,
		`GIT_SSH_COMMAND='ssh -o "Proxy\"Command\"=rm -rf ~"' git fetch`,
		`GIT_SSH_COMMAND='scp -3o ProxyCommand="rm -rf ~"' git fetch`,
		`export X=less; X='rm -rf ~'; git --config-env=core.pager=X log`,
		`Y='rm -rf ~'; X="${Y}" git --config-env=core.pager=X log`,
	} {
		if r := s.bash(t, c, sgOpts{}); r.Code != 2 {
			t.Errorf("sub-agent: %q not blocked: %d %q", c, r.Code, r.Stderr)
		}
		if r := s.bash(t, c, top()); r.Code == 0 {
			if d, _, ok := decision(t, r.Stdout); !ok || (d != "ask" && d != "deny") {
				t.Errorf("top: %q allowed: %q", c, r.Stdout)
			}
		}
	}
	for _, c := range []string{
		`GIT_SSH_COMMAND='ssh -o PermitLocalCommand=yes -o LocalCommand="echo connected"' git fetch`,
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="cloudflared access ssh --hostname %h"' git fetch`,
		`GIT_SSH_COMMAND='ssh -o PermitLocalCommand=no' git fetch`,
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="ssh -W %h:%p bastion"' git fetch`,
		`GIT_SSH_COMMAND="ssh -o ProxyCommand='aws ssm start-session --target %h --document-name AWS-StartSSHSession --parameters portNumber=%p'" git fetch`,
	} {
		for _, o := range []sgOpts{{}, top()} {
			if r := s.bash(t, c, o); r.Code != 0 || r.Stdout != "" || r.Stderr != "" {
				t.Errorf("top %v: %q: %d %q %q", o.top, c, r.Code, r.Stderr, r.Stdout)
			}
		}
	}
}
