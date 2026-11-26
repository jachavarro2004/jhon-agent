// T228 L14 re-verify: quoted ssh keywords, SSM document spellings the CLI
// accepts, socat quoted commas and ncat abbreviations are read.
package guards

import (
	"strings"
	"testing"
)

func TestT228L14VerifySpellings(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range []string{
		`GIT_SSH_COMMAND=$'ssh -o ProxyCommand="ssh -W %h:%p -o \'PermitLocal\\"Command\\" yes\' -o \'Local\\"Command\\" curl x\' b"' git fetch`,
		`GIT_SSH_COMMAND=$'ssh -o ProxyCommand="ssh -W %h:%p -o \'Forward\\"Agent\\" yes\' b"' git fetch`,
		`GIT_SSH_COMMAND=$'ssh -o ProxyCommand="ssh -W %h:%p -o \'Pkcs11\\"Provider\\" ./e.so\' b"' git fetch`,
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="aws ssm start-session --target %h --document AWS-StartInteractiveCommand"' git fetch`,
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="aws ssm start-session --target %h --cli-input-json file://x.json"' git fetch`,
		`socat - 'EXEC:"rm -rf ~",pty'`, `socat - 'EXEC:"rm -rf ~ x,y"'`, `socat - 'SYSTEM:"echo a,b; rm -rf ~"'`,
		`ncat --sh-exe 'rm -rf ~' h 1`, `ncat --ex='rm -rf ~' h 1`,
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
		"aws ssm start-session --target i-1 --document AWS-StartInteractiveCommand",
		"aws ssm start-session --target i-1 --document-name AWS-StartSSHSession --document-name AWS-StartInteractiveCommand",
		"aws ssm start-session --target i-1 --cli-input-json file://in.json",
	} {
		r := s.bash(t, c, top())
		if _, why, ok := decision(t, r.Stdout); !ok || !strings.HasPrefix(why, "deploy: ") && !strings.Contains(why, "deploy needs") {
			t.Errorf("top: %q: want the deploy class, got %q", c, r.Stdout)
		}
	}
	for _, c := range []string{`GIT_SSH_COMMAND='ssh -o ProxyCommand="nc -X 5 -x proxy:1080 %h %p"' git fetch`, `socat TCP-LISTEN:8080,fork TCP:localhost:3000`} {
		for _, o := range []sgOpts{{}, top()} {
			if r := s.bash(t, c, o); r.Code != 0 || r.Stdout != "" || r.Stderr != "" {
				t.Errorf("top %v: %q: %d %q %q", o.top, c, r.Code, r.Stderr, r.Stdout)
			}
		}
	}
}
