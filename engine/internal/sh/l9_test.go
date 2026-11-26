package sh

import (
	"reflect"
	"strings"
	"testing"
)

// T228 L9: what an ssh command line runs on this machine through its -o options.
func TestSSHCommands(t *testing.T) {
	for line, want := range map[string][]string{
		`ssh -o ProxyCommand=x h`:                                          {"x"},
		`ssh -oProxyCommand="nc %h %p" h`:                                  {"nc %h %p"},
		`ssh -o 'proxycommand y z' h`:                                      {"y z"},
		`ssh -o LocalCommand=a -o PermitLocalCommand=yes`:                  {"a"},
		`scp -o KnownHostsCommand=b f h:`:                                  {"b"},
		`ssh -o ProxyCommand=none -o IdentitiesOnly=yes -v -F /dev/null h`: nil,
	} {
		cmds, err := Parse(line)
		if err != nil || len(cmds) != 1 {
			t.Fatalf("Parse(%q): %v", line, err)
		}
		if got := SSHCommands(cmds[0].Words); !reflect.DeepEqual(got, want) {
			t.Errorf("SSHCommands(%q) = %q, want %q", line, got, want)
		}
	}
	for src, want := range map[string]string{
		`GIT_SSH_COMMAND='ssh -o ProxyCommand="git push"' git fetch`:    "| git push",
		`git -c core.sshCommand='ssh -o ProxyCommand="git push"' fetch`: "git | git push",
		`git -c core.gitProxy='git push' fetch git://x/y`:               "git | git push",
		`git -c remote.origin.uploadpack='git push' fetch origin`:       "git | git push",
		`git -c browser.x.cmd='git push' help -w log`:                   "git | git push",
	} {
		got, err := route(t, src)
		if err != nil || !strings.Contains(strings.Join(got, "\n"), want) {
			t.Errorf("%q: programs %q %v, want one %q", src, got, err, want)
		}
	}
	for _, src := range []string{`GIT_SSH_COMMAND='ssh -o IdentitiesOnly=yes' git fetch`, `git -c core.sshCommand='ssh -v' fetch`} {
		if got, _ := route(t, src); strings.Contains(strings.Join(got, "\n"), "| ssh") {
			t.Errorf("%q: ssh is read as a program of its own: %q", src, got)
		}
	}
}
