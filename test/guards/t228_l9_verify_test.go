// T228 L9 re-verify: ssh's bundled -o and quoted option names, and git's
// --config-env with its variable on the line, are read.
package guards

import "testing"

func TestT228L9VerifySSHAndConfigEnv(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range []string{
		`GIT_SSH_COMMAND='ssh -vo ProxyCommand="rm -rf ~"' git fetch`,
		`GIT_SSH_COMMAND='ssh -4oProxyCommand="rm -rf ~"' git fetch`,
		`git -c core.sshCommand='ssh -qo ProxyCommand="rm -rf ~"' fetch`,
		`GIT_SSH_COMMAND='ssh -o "\"ProxyCommand\"=rm -rf ~"' git fetch`,
		`X='rm -rf ~' git --config-env=remote.origin.uploadpack=X fetch origin`,
		`X='rm -rf ~' git --config-env=core.sshCommand=X fetch`,
		`export X='rm -rf ~'; git --config-env=core.pager=X log`,
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
	for _, c := range []string{`GIT_SSH_COMMAND='ssh -vvv -o IdentitiesOnly=yes' git fetch`, `GIT_SSH_COMMAND='ssh -o ProxyCommand=none' git fetch`} {
		for _, o := range []sgOpts{{}, top()} {
			if r := s.bash(t, c, o); r.Code != 0 || r.Stdout != "" || r.Stderr != "" {
				t.Errorf("top %v: %q: %d %q %q", o.top, c, r.Code, r.Stderr, r.Stdout)
			}
		}
	}
}
