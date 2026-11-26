// T228 L18 re-verify: socket and TLS tuning on network addresses stays
// allowed; path options and chdir on those addresses are still read.
package guards

import "testing"

func TestT228L18VerifyNetworkTuning(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range []string{
		"socat UDP4-RECVFROM:5353,ip-add-membership=224.0.0.251:0.0.0.0,fork -",
		"socat - TCP:h:80,so-rcvbuf=65536", "socat - OPENSSL:h:443,openssl-min-proto-version=TLS1.2",
		"socat UDP4-DATAGRAM:239.0.0.1:5000,ttl=4 -", "socat TCP6-LISTEN:8080,ipv6only=1,fork TCP:localhost:3000",
	} {
		for _, o := range []sgOpts{{}, top()} {
			if r := s.bash(t, c, o); r.Code != 0 || r.Stdout != "" || r.Stderr != "" {
				t.Errorf("top %v: %q: %d %q %q", o.top, c, r.Code, r.Stderr, r.Stdout)
			}
		}
	}
	for _, c := range []string{"socat - TCP:h:80,chdir=.git", "socat - 'TCP:h:1,lockfile=.git/x'", "socat PTY,symbolic-link=.git/config /dev/null"} {
		if r := s.bash(t, c, sgOpts{}); r.Code != 2 {
			t.Errorf("sub-agent: %q not blocked: %d %q", c, r.Code, r.Stderr)
		}
	}
}
