package guards

import "testing"

// T228 L9 (re-verify mwx439vt): an ssh value of a git transport variable or
// key is no exemption (its ProxyCommand, LocalCommand and
// KnownHostsCommand run), and the git configuration keys whose value git
// runs as a command are read; an ordinary ssh value, and an ordinary pager,
// stay allowed.
var t228L9Refused = []string{
	`GIT_SSH_COMMAND='ssh -o ProxyCommand="rm -rf ~"' git fetch`,
	`git -c core.sshCommand='ssh -o ProxyCommand="rm -rf ~"' fetch`,
	`GIT_SSH_COMMAND='ssh -oLocalCommand="rm -rf ~" -o PermitLocalCommand=yes' git fetch`,
	`GIT_SSH_COMMAND="ssh -o 'KnownHostsCommand rm -rf ~'" git fetch`,
	`export GIT_SSH_COMMAND='ssh -o ProxyCommand="rm -rf ~"'; git fetch`,
	`git -c core.gitProxy='rm -rf ~' fetch git://x/y`,
	`git -c core.alternateRefsCommand='rm -rf ~' fetch .`,
	`git -c remote.origin.uploadpack='rm -rf ~' fetch origin`,
	`git -c remote.origin.receivepack='rm -rf ~' push origin`,
	`git -c sendemail.sendmailcmd='rm -rf ~' send-email x`,
	`git -c web.browser=x -c browser.x.cmd='rm -rf ~' help -w log`,
	`git -c submodule.x.update='!rm -rf ~' submodule update`,
	`git -c gpg.ssh.defaultKeyCommand='rm -rf ~' commit -S -m x`,
	`git -c instaweb.httpd='rm -rf ~' instaweb`,
}

var t228L9Allowed = []string{
	"GIT_SSH_COMMAND='ssh -o IdentitiesOnly=yes' git fetch",
	"git -c core.sshCommand='ssh -v' fetch",
	"GIT_SSH_COMMAND='ssh -i ~/.ssh/deploy -F /dev/null' git fetch",
	"GIT_PAGER=cat git log",
	"git -c core.gitProxy=/usr/bin/true fetch",
}

func TestT228L8VerifyGitCommands(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range t228L9Refused {
		for _, o := range []sgOpts{{}, top()} {
			code(t, s.bash(t, c, o), 2, c)
		}
	}
	for _, c := range t228L9Allowed {
		allowed(t, s, c)
	}
}
