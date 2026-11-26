// T228 L17 re-verify: options on every socat address are read (canonical
// names, chdir, unlisted aliases); ordinary serial, listener and tail forms stay allowed.
package guards

import "testing"

func TestT228L17VerifySocatOptionsEverywhere(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range []string{
		"socat PTY,symbolic-link=.git/config /dev/null", "socat 'SYSTEM:true,pty,symbolic-link=.git/config' -",
		"socat EXEC:/bin/bash,pty,symbolic-link=.git/config -", "socat -u READLINE,history-file=.git/config /dev/null",
		"socat - 'SYSTEM:rm config,chdir=.git'", "socat - 'EXEC:rm -rf data,chdir=/'", "socat - 'SHELL:rm config,chdir=.git'",
		"socat -u /dev/null 'UNIX-SEND:/tmp/s,bind=.git/config,unlink-early'",
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
		"socat UNIX-LISTEN:/run/user/1000/app.sock,fork TCP:localhost:8080",
		"socat - /dev/ttyUSB0,b115200,raw,echo=0,nonblock,min=1,time=0",
		"socat -u FILE:/var/log/syslog,seek-end=0,ignoreeof -",
		"socat UNIX-LISTEN:/tmp/app.sock,fork,mode=600 TCP:localhost:8080",
		"socat PTY,link=/tmp/ttyV0,raw,echo=0,waitslave TCP:host:9000",
		"socat -u - 'CREATE:out.bin,perm=0644'",
	} {
		for _, o := range []sgOpts{{}, top()} {
			if r := s.bash(t, c, o); r.Code != 0 || r.Stdout != "" || r.Stderr != "" {
				t.Errorf("top %v: %q: %d %q %q", o.top, c, r.Code, r.Stderr, r.Stdout)
			}
		}
	}
}
