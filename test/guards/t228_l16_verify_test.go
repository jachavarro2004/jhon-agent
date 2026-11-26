// T228 L16 re-verify: socat's unlink options, link= and socket listeners
// remove the file they name; PIPE writes one. Scratch and project uses stay allowed.
package guards

import "testing"

func TestT228L16VerifySocatRemovals(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range []string{
		"socat -u 'OPEN:.git/config,unlink-close' /dev/null", "socat -u 'OPEN:.git/config,unlink-early' /dev/null",
		"socat -U - 'GOPEN:.git/HEAD,unlink-close'", "socat -u 'OPEN:~/.bashrc,unlink-close' /dev/null",
		"socat PTY,link=.git/config /dev/null", "socat 'UNIX-LISTEN:.git/config,unlink-early' /dev/null",
		"socat -u 'UNIX-RECV:.git/HEAD,unlink-early' /dev/null", "socat -u - 'PIPE:.git/config'", "socat -u - 'PIPE:/dev/sda'",
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
		"socat UNIX-LISTEN:/tmp/dbg.sock,fork,unlink-early TCP:localhost:9229",
		"socat -d -d PTY,link=./ttyV0,raw,echo=0 PTY,link=./ttyV1,raw,echo=0",
		"socat PTY,link=/tmp/vtty,raw TCP:localhost:5555", "socat -u - PIPE:./fifo",
	} {
		for _, o := range []sgOpts{{}, top()} {
			if r := s.bash(t, c, o); r.Code != 0 || r.Stdout != "" || r.Stderr != "" {
				t.Errorf("top %v: %q: %d %q %q", o.top, c, r.Code, r.Stderr, r.Stdout)
			}
		}
	}
}
