// T228 L15 (re-verify mwxajz2p of 7359faa): one canonical ssh keyword
// reader, SSM spellings fail closed, socat's quotes and groups and ncat's
// long-option prefixes are read, and socat's and nc's file-writing
// addresses and options are judged as the writes they are.
package guards

import (
	"strings"
	"testing"
)

// TestT228L15SSHKeywordSpellings: a keyword ending in (or wholly in) double
// quotes is the keyword ssh reads, at the top, in SSHCommands and in a forward.
func TestT228L15SSHKeywordSpellings(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range []string{
		`GIT_SSH_COMMAND=$'ssh -o \'Local"Command" rm -rf ~\' -o \'PermitLocal"Command" yes\'' git fetch`,
		`GIT_SSH_COMMAND=$'ssh -o \'Proxy"Command" rm -rf ~\'' git fetch`,
		`GIT_SSH_COMMAND=$'ssh -o \'"KnownHostsCommand"=rm -rf ~\'' git fetch`,
		`git -c core.sshCommand=$'ssh -o \'proxy"command" rm -rf ~\'' fetch`,
	} {
		refused(t, s, c, "rm -r ~: it reaches the home directory")
	}
	// At the top, ssh's words are not read for its options: the class rule leaves the line unreadable.
	for _, c := range []string{`ssh -o 'Local"Command" rm -rf ~' -o 'PermitLocal"Command" yes' h`, `ssh -o 'KnownHosts"Command"=rm -rf ~' h`} {
		notAllowed(t, s, c)
	}
	// In a forward, a keyword written with a quote is never a forward's.
	for _, c := range []string{
		`GIT_SSH_COMMAND=$'ssh -o ProxyCommand="ssh -W %h:%p -o \'Send\\"Env\\" TOKEN\' b"' git fetch`,
		`GIT_SSH_COMMAND=$'ssh -o ProxyCommand="ssh -W %h:%p -o \'Remote\\"Forward\\" 9:x:22\' b"' git fetch`,
		`GIT_SSH_COMMAND=$'ssh -o ProxyCommand="ssh -W %h:%p -o \'Strict\\"HostKeyChecking\\" no\' b"' git fetch`,
		`GIT_SSH_COMMAND=$'ssh -o ProxyCommand="ssh -W %h:%p -o \'\\"ForwardAgent\\" yes\' b"' git fetch`,
		`GIT_SSH_COMMAND=$'ssh -o ProxyCommand="ssh -W %h:%p -o \'Dynamic\\"Forward\\"=1080\' b"' git fetch`,
	} {
		notAllowed(t, s, c)
	}
	for _, c := range []string{
		`GIT_SSH_COMMAND=$'ssh -o ProxyCommand="ssh -W %h:%p -o StrictHostKeyChecking=no -o \'ServerAliveInterval 30\' b"' git fetch`, pc("ssh -W %h:%p -oConnectTimeout=5 b"),
		`GIT_SSH_COMMAND='ssh -o "IdentitiesOnly yes" -i k' git fetch`,
	} {
		allowed(t, s, c)
	}
	// An ordinary key at the top is still read as ssh: a remote act, not unreadable.
	c := "ssh -o StrictHostKeyChecking=no -o 'Server\"AliveInterval\" 30' h uptime"
	if why := askedWhy(t, s.bash(t, c, top()).Stdout, c); why != "remote: ssh" {
		t.Errorf("top: %q asks %q, want remote: ssh", c, why)
	}
}

// TestT228L15SSMSpellings: a shortened --document-name is read, the last one
// wins, and --cli-input-json or --cli-input-yaml is a deploy.
func TestT228L15SSMSpellings(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for c, cls := range map[string]string{
		"aws ssm start-session --target i-1 --document-nam AWS-StartInteractiveCommand":                                 "deploy",
		"aws ssm start-session --target i-1 --docu=AWS-StartNonInteractiveCommand":                                      "deploy",
		"aws ssm start-session --target i-1 --cli-input-yaml file://in.yaml":                                            "deploy",
		"aws ssm start-session --target i-1 --cli-input-j file://in.json --document-name AWS-StartSSHSession":           "deploy",
		"aws ssm start-session --target i-1 --document-name AWS-StartInteractiveCommand --document AWS-StartSSHSession": "remote",
		"aws ssm start-session --target i-1 --document=AWS-StartPortForwardingSession":                                  "remote",
	} {
		code(t, s.bash(t, c, sgOpts{}), 2, "sub-agent: "+c)
		if why := askedWhy(t, s.bash(t, c, top()).Stdout, c); !strings.HasPrefix(why, cls+": ") {
			t.Errorf("top: %q asks %q, want the %s class", c, why, cls)
		}
	}
	notAllowed(t, s, pc("aws ssm start-session --target %h --cli-input-yaml file://x.yaml"))
	notAllowed(t, s, pc("aws ssm start-session --target %h --document-nam AWS-StartInteractiveCommand"))
	allowed(t, s, pc("aws ssm start-session --target %h --document AWS-StartSSHSession --parameters portNumber=%p"))
}

// TestT228L15SocatQuotesAndNcatPrefixes: socat's quotes, backslashes and
// bracket groups are read as socat reads them before an option comma; ncat
// long options are matched by prefix, as getopt_long does.
func TestT228L15SocatQuotesAndNcatPrefixes(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range []string{
		`socat - 'exec:"rm -rf ~",pty'`, `socat - "SYSTEM:'rm -rf ~',pty"`, `socat - 'SYSTEM:echo [a,b]; rm -rf ~'`,
		`socat - 'SYSTEM:echo a\,b; rm -rf ~'`, `socat 'SYSTEM:"echo a!!b"; rm -rf ~' -`, `socat - 'EXEC:rm -rf ~,pty,stderr'`,
		`ncat --sh 'rm -rf ~' h 1`, `ncat --exe 'rm -rf ~' h 1`, `ncat --sh-exec='rm -rf ~' h 1`, `ncat -lc 'rm -rf ~' 9`,
	} {
		refused(t, s, c, "rm -r ~: it reaches the home directory")
	}
	for _, c := range []string{"socat - TCP:h:22", "socat - 'SYSTEM:echo \"a,b\"'", "ncat --ssl h 443", "ncat --proxy-type socks5 --proxy p:1 h 22"} {
		allowed(t, s, c)
	}
}

// TestT228L15SocatAndNcWrites: socat's file-writing addresses and -lf, ncat
// -o/-x and nc -o write the file they name, judged as tee writing it is.
func TestT228L15SocatAndNcWrites(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range []string{
		"socat - CREATE:.git/hooks/pre-commit", "socat - creat:.git/hooks/pre-commit", "socat - OPEN:.git/hooks/post-merge,creat",
		"socat - OPEN:.git/hooks/post-merge,trunc", "socat TCP:h:1 GOPEN:.git/hooks/pre-push,trunc", "socat - .git/hooks/pre-push,o-trunc",
		"socat - OPEN:.git/hooks/post-merge,append", "socat -lf .git/hooks/pre-commit - TCP:h:1", "ncat -x .git/hooks/pre-commit h 1",
		"ncat --hex-dump .git/hooks/pre-commit h 1", "ncat -o .git/hooks/pre-commit h 1", "ncat --out=.git/hooks/pre-commit h 1",
		"nc -o .git/hooks/pre-commit h 1", "socat - /dev/sda,trunc", "socat -U OPEN:.git/hooks/pre-commit,creat TCP:h:1",
	} {
		refused(t, s, c, "it writes")
	}
	for _, c := range []string{
		"socat -u OPEN:.git/config STDOUT", "socat - OPEN:notes.txt,creat,trunc", "socat - /dev/ttyUSB0,raw,echo=0",
		"socat -u TCP:h:1 OPEN:notes.txt,creat", "socat -U - OPEN:.git/config", "ncat -o notes.txt h 1", "socat -lf socat.log - TCP:h:1",
	} {
		allowed(t, s, c)
	}
}
