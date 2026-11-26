package sh

import (
	"errors"
	"testing"
)

// T228 L19: socat's network-group options are harmless on a network-family
// address and unread elsewhere; the table's other readings hold everywhere.
func TestSocatNetworkOptions(t *testing.T) {
	for _, line := range []string{
		"socat - TCP:h:80,so-rcvbuf=1,rcvbuf=1,sndbuf=1,keepidle=1,keepintvl=1,keepcnt=1,linger=1,tos=1,mss=1",
		"socat UDP4-RECVFROM:5353,ip-add-membership=224.0.0.251:0.0.0.0,fork -", "socat UDP4-DATAGRAM:239.0.0.1:5000,ttl=4,broadcast -",
		"socat - OPENSSL:h:443,openssl-min-proto-version=TLS1.2,cipher=HIGH,dhparam=d.pem,egd=e", "socat TCP6-LISTEN:8080,ipv6only=1,fork -",
		"socat - SCTP-CONNECT:h:1,ttl=1", "socat - DCCP:h:1,ccid=2", "socat - SOCKS4A:p:h:1,socksuser=u", "socat - PROXY:p:h:1,proxy-auth=u:p",
		"socat - VSOCK-CONNECT:1:2,rcvbuf=1", "socat - TCP:h:1,setsockopt-int=1:2:3",
	} {
		if _, err := socatRuns(splitWords(line)); err != nil {
			t.Errorf("socatRuns(%q): %v", line, err)
		}
	}
	for _, line := range []string{
		"socat PTY,rcvbuf=1 -", "socat EXEC:cat,ttl=4 -", "socat -u OPEN:f,cipher=HIGH -", "socat - SOCKET-CONNECT:2:6:x,rcvbuf=1",
		"socat - UNIX-CONNECT:s,sndbuf=1", "socat - PROXY:p:h:1,proxy-authorization-file=f", "socat - PROXY:p:h:1,proxyauthfile=f",
		"socat - OPENSSL:h:443,alpn=h2", "socat - TCP:h:80,chdir=.git", "socat - TCP:h:1,udplite-send-cscov=1",
	} {
		if _, err := socatRuns(splitWords(line)); !errors.As(err, new(*Unreadable)) {
			t.Errorf("socatRuns(%q): %v, want unreadable", line, err)
		}
	}
	for line, want := range map[string]string{
		"socat - TCP:h:1,lockfile=x": "tee", "socat - TCP:h:1,history-file=x,rcvbuf=1": "tee", "socat - OPENSSL:h:1,symbolic-link=x": "rm",
		"socat - UNIX-CONNECT:s,bind=x": "rm",
	} {
		in, err := socatRuns(splitWords(line))
		if err != nil || len(in.Words) != 1 || in.Words[0][0] != want || in.Words[0][1] != "x" {
			t.Errorf("socatRuns(%q) = %+v, %v; want %s x", line, in, err, want)
		}
	}
}
