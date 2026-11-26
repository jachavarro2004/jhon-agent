// T228 L17 (re-verify mwxelg03 of 5a7fa93): socat is closed by class.
// Every socat address that names a filesystem path, and the link=,
// lockfile=, waitlock=, history= and unix bind= options, are read by one
// reader: a write is judged as tee, a removal (an unlink-* option, a unix
// socket listener or receiver, link=, a unix bind=) as rm, and an address
// that names a path and carries an option outside a short harmless list is
// unreadable.
package guards

import "testing"

func TestT228L17SocatByClass(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for c, why := range map[string]string{
		// Removals.
		"socat -u 'OPEN:.git/config,unlink-late' /dev/null":   "rm .git/config",
		"socat 'UNIX-L:.git/config' /dev/null":                "rm .git/config",
		"socat 'UNIX-RECVFROM:.git/HEAD' -":                   "rm .git/HEAD",
		"socat - 'UNIX-CONNECT:/tmp/s,bind=.git/config'":      "rm .git/config",
		"socat - 'UNIX-SENDTO:/tmp/s,bind=.git/config'":       "rm .git/config",
		"socat 'PTY,link=.git/hooks/pre-commit' -":            "rm .git/hooks/pre-commit",
		"socat 'EXEC:cat,pty,link=.git/config' -":             "rm .git/config",
		"socat -u 'TCP:h:1,unlink-early,link=/dev/sda' -":     "rm /dev/sda",
		"socat 'unix-listen:.git/config,unlink-early,fork' -": "rm .git/config",
		// Writes.
		"socat 'READLINE,history=.git/config' -": "tee .git/config",
		"socat 'PTY,lockfile=.git/config' -":     "tee .git/config",
		"socat 'PTY,waitlock=.git/config' -":     "tee .git/config",
		"socat - FIFO:.git/config":               "tee .git/config",
		"socat - ECHO:.git/config":               "tee .git/config",
		"socat -U 'PIPE:/dev/nvme0n1' -":         "tee /dev/nvme0n1",
	} {
		refused(t, s, c, why)
	}
	// A path-naming address with an option outside the harmless list is unreadable.
	for _, c := range []string{
		"socat -u 'OPEN:notes.txt,perm=0777' -", "socat -u 'OPEN:notes.txt,user=root' -", "socat - 'UNIX-CONNECT:/tmp/s,newoption=1'",
		"socat 'PTY,link=./ttyV0,mode=777' -", "socat - '/dev/ttyUSB0,b115200,seek=9'", "socat 'READLINE,history=./h,prompt=x,chroot=/' -",
	} {
		notAllowed(t, s, c)
	}
	for _, c := range []string{
		// Sockets in the project or scratch, links in the project or /tmp, the serial and capture lines.
		"socat UNIX-LISTEN:./app.sock,fork,unlink-early TCP:localhost:3000", "socat UNIX-LISTEN:/tmp/a.sock,unlink-close -",
		"socat 'UNIX-RECV:/tmp/r.sock,unlink-early' -", "socat - UNIX-CONNECT:/var/run/docker.sock", "socat - UNIX:/tmp/s", "socat - LOCAL:/tmp/s",
		"socat -d -d pty,raw,echo=0 pty,raw,echo=0", "socat PTY,link=/tmp/ttyV0,raw,echo=0,wait-slave PTY,link=./ttyV1,rawer",
		"socat - /dev/ttyUSB0,b115200,raw,echo=0,crnl", "socat /dev/ttyS0,b9600,cs8,parenb=0,cstopb=0,clocal=1,crtscts=0,raw -",
		"socat -u /dev/ttyACM0,raw,ignoreeof STDOUT", "socat -lf socat.log - TCP:h:1", "socat -r in.dump -R out.dump - TCP:h:1",
		"socat -u FILE:in.bin TCP:h:9", "socat -u - PIPE:./fifo", "socat - PIPE", "socat READLINE,history=./.socat_history TCP:h:1",
		"socat TCP-LISTEN:8080,fork,reuseaddr TCP:localhost:3000", "socat OPENSSL-LISTEN:4443,cert=server.pem,verify=0,fork TCP:localhost:80",
		"socat STDIO EXEC:cat,pty,stderr,setsid,ctty", "socat - TCP:h:1,nodelay", "socat - OPEN:notes.txt,creat,trunc,append",
		"socat -u OPEN:.git/config,rdonly STDOUT", "socat -u - 'OPEN:out.txt,creat'", "socat -U - OPEN:.git/config",
		"ncat -o out.txt h 1", "nc -o dump h 1", "socat - /dev/null",
	} {
		allowed(t, s, c)
	}
}
