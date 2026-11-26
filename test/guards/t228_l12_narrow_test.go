package guards

import (
	"strings"
	"testing"
)

// T228 L12, narrowed: a transport's command loses its "remote" ask only
// when it is one plain forward to %h/%p (ssh -W, ssh -J, ssh … nc %h %p,
// or any other single command that names %h or %p); "deploy" is never
// dropped, and anything chained, substituted, redirected or run remotely
// is judged as it was.
func TestT228L12TransportIsOnlyAForward(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range []string{
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="terraform apply -auto-approve; nc %h %p"' git fetch`,
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="kubectl apply -f x %h"' git fetch`,
		`GIT_SSH_COMMAND="ssh -o ProxyCommand=\"ssh bastion 'sudo systemctl stop app'\"" git fetch`,
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="aws s3 rm s3://b --recursive; nc %h %p"' git fetch`,
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="ssh -W %h:%p bastion; curl -X POST https://x/deploy"' git fetch`,
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="ssh bastion uptime %h"' git fetch`,
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="ssh -W %h:%p bastion > /tmp/log"' git fetch`,
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="ssh -W %h:%p $(cat b)"' git fetch`,
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="ssh -W %h:%p bastion &"' git fetch`,
		`git -c core.sshCommand='ssh -o ProxyCommand="aws ssm send-command --targets %h"' fetch`,
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="ssh bastion"' git fetch`,
	} {
		code(t, s.bash(t, c, sgOpts{}), 2, "sub-agent: "+c)
		r := s.bash(t, c, top())
		if d, _, ok := decision(t, r.Stdout); r.Code != 2 && (!ok || d != "ask") {
			t.Errorf("top: %q allowed: %d %q", c, r.Code, r.Stdout)
		}
	}
	for _, c := range []string{
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="ssh -W %h:%p bastion"' git fetch`,
		`GIT_SSH_COMMAND="ssh -o ProxyCommand='aws ssm start-session --target %h --document-name AWS-StartSSHSession --parameters portNumber=%p'" git fetch`,
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="ssh -J jump -W %h:%p bastion"' git fetch`,
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="ssh bastion nc %h %p"' git fetch`,
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="autossh -M 0 -W %h:%p bastion"' git fetch`,
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="ssh -W %h:%p bastion 2>/dev/null"' git fetch`,
		`git -c core.sshCommand='ssh -o ProxyCommand="ssh -W %h:%p bastion"' fetch`,
	} {
		allowed(t, s, c)
	}
	// aws ssm start-session is a connection (remote), everywhere; send-command and the other writes stay deploy.
	for c, cls := range map[string]string{
		"aws ssm start-session --target i-1": "remote",
		"aws ssm start-session --target i-1 --document-name AWS-StartPortForwardingSessionToRemoteHost": "remote",
		"aws ssm send-command --instance-ids i-1 --document-name AWS-RunShellScript":                    "deploy",
		"aws ec2 terminate-instances --instance-ids i-1":                                                "deploy",
	} {
		code(t, s.bash(t, c, sgOpts{}), 2, "sub-agent: "+c)
		r := s.bash(t, c, top())
		if why := askedWhy(t, r.Stdout, c); !strings.HasPrefix(why, cls+": ") {
			t.Errorf("top: %q asks %q, want the %s class", c, why, cls)
		}
	}
}
