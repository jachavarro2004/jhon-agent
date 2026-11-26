package guards

import "testing"

// T228 L10: an ssh transport value is read with ssh's own option grammar
// (bundles, `--`, options after the host), its option names dequoted and
// case-folded; a word that names a local-command option in a form the
// grammar does not resolve leaves the line unreadable. --config-env reads
// its variable when the line sets it.

// t228L10Blocked are lines refused or unreadable, never allowed.
var t228L10Blocked = []string{
	`GIT_SSH_COMMAND='ssh -vvo ProxyCommand="rm -rf ~"' git fetch`,
	`GIT_SSH_COMMAND='ssh -Cqo PROXYCOMMAND="rm -rf ~"' git fetch`,
	`GIT_SSH_COMMAND="ssh -o 'proxycommand	rm -rf ~'" git fetch`,
	`GIT_SSH_COMMAND='ssh -p 22 -o ProxyCommand="rm -rf ~"' git fetch`,
	`GIT_SSH_COMMAND='ssh -i k -vo LocalCommand="rm -rf ~" -o PermitLocalCommand=yes' git fetch`,
	`GIT_SSH_COMMAND='ssh -4o KnownHostsCommand="rm -rf ~"' git fetch`,
	`GIT_SSH_COMMAND='scp -o ProxyCommand="rm -rf ~"' git fetch`,
	`GIT_SSH_COMMAND='sftp -Co ProxyCommand="rm -rf ~"' git fetch`,
	`git -c core.sshCommand="ssh -o 'Proxy''Command=rm -rf ~'" fetch`,
	`GIT_SSH_COMMAND='ssh -l proxycommand host' git fetch`,
	`X='ssh -vo ProxyCommand="rm -rf ~"' git --config-env core.sshCommand=X fetch`,
	`env X='rm -rf ~' git --config-env=core.pager=X log`,
	`X='!rm -rf ~' git --config-env=alias.y=X y`,
}

// t228L10Allowed are the ordinary transport lines: they stay allowed.
var t228L10Allowed = func() []string {
	var out []string
	for _, v := range []string{
		"ssh -i ~/.ssh/deploy", "ssh -p 2222", "ssh -o StrictHostKeyChecking=no", "ssh -o StrictHostKeyChecking=accept-new",
		"ssh -o UserKnownHostsFile=/dev/null", "ssh -o ControlMaster=auto -o ControlPath=/tmp/cm-%r@%h:%p -o ControlPersist=60",
		"ssh -vvv", "ssh -o ProxyJump=bastion", "ssh -J bastion", "ssh -o ProxyCommand=none", "ssh -F /dev/null", "ssh -4 -C",
		"ssh -vvv -o IdentitiesOnly=yes", "ssh -i k -p 2222 -o StrictHostKeyChecking=no", "ssh -o BatchMode=yes -o ConnectTimeout=5",
		"ssh -oIdentitiesOnly=yes -oServerAliveInterval=30", "ssh -o 'IdentityFile ~/.ssh/k'", "ssh -q -T", "ssh -A -x",
		"ssh -o LogLevel=ERROR",
	} {
		out = append(out, "GIT_SSH_COMMAND='"+v+"' git fetch", "git -c core.sshCommand='"+v+"' fetch")
	}
	return append(out, "git --config-env=core.pager=PAGER log", "PAGER=less git --config-env=core.pager=PAGER log")
}()

func TestT228L10SSHGrammarAndConfigEnv(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range t228L10Blocked {
		notAllowed(t, s, c)
	}
	for _, c := range t228L10Allowed {
		allowed(t, s, c)
	}
}
