// T228 L19 (re-verify mwxi192d of 21d3d02): an option socat puts only in
// its network groups (SOCKET, IP4, IP6, TCP, UDP, SCTP, DCCP, OPENSSL,
// SOCKS, HTTP, RANGE, LISTEN, CHILD, RETRY) is harmless on a network-family
// address; everywhere else, and for any other option, the table's reading
// holds.
package guards

import "testing"

func TestT228L19NetworkTuning(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range []string{
		"socat - TCP:h:80,rcvbuf=65536,sndbuf=65536,keepidle=60,keepintvl=10,keepcnt=3,linger=5,tos=16,mss=1400",
		"socat UDP4-DATAGRAM:239.0.0.1:5000,ip-multicast-ttl=4,ip-multicast-loop=0,broadcast -",
		"socat - OPENSSL:h:443,cipher=HIGH,openssl-min-proto-version=TLS1.2,cafile=ca.pem,cert=c.pem,key=k.pem",
		"socat - TCP:h:80,so-bindtodevice=eth0", "socat - SCTP:h:9,ttl=8", "socat - SOCKS4:p:h:80,socksuser=me",
		"socat TCP-LISTEN:9,fork,ipv6only=0,accept-timeout=5,max-children=4 -", "socat - INET:h:80,nodelay,quickack",
	} {
		allowed(t, s, c)
	}
	for _, c := range []string{
		// A network option on an address that is not a network one keeps asking.
		"socat 'PTY,rcvbuf=65536' -", "socat 'EXEC:cat,ttl=4' -", "socat -u 'OPEN:notes.txt,cipher=HIGH' -",
		"socat - SOCKET-CONNECT:2:6:x,rcvbuf=65536",
		// A file it reads and sends is not taken as harmless; an unknown name keeps asking.
		"socat - 'PROXY:p:h:80,proxy-authorization-file=secret.txt'", "socat - 'TCP:h:80,newtuning=1'",
		// socat 1.8.1.3 has no alpn or openssl-method option (it refuses both): unknown names ask.
		"socat - OPENSSL:h:443,alpn=h2", "socat - OPENSSL:h:443,openssl-method=TLS1.2",
		// Path options and chdir on a network address are still read.
		"socat - TCP:h:80,chdir=.git",
	} {
		notAllowed(t, s, c)
	}
	for c, why := range map[string]string{
		"socat - 'TCP:h:1,lockfile=.git/x'":              "tee .git/x",
		"socat - 'UDP:h:1,waitlock=.git/x'":              "tee .git/x",
		"socat - 'OPENSSL:h:443,symbolic-link=.git/x'":   "rm .git/x",
		"socat - 'TCP:h:1,history-file=.git/x,rcvbuf=1'": "tee .git/x",
	} {
		refused(t, s, c, why)
	}
}
