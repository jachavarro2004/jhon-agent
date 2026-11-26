// T228 L18 (re-verify mwxgsurt of 5e983be): every socat option on every
// address is read through one table of socat's own option names and their
// aliases: harmless, a path it writes or removes, a folder a command runs in,
// or unreadable. A listener removes a file only with an unlink option.
package guards

import "testing"

func TestT228L18SocatOptionsOnEveryAddress(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for c, why := range map[string]string{
		// Canonical names and aliases of the options the reader reads, on any address.
		"socat 'SHELL:true,symbolic-link=.git/hooks/pre-commit' -": "rm .git/hooks/pre-commit",
		"socat -u READLINE,history=.git/config /dev/null":          "tee .git/config",
		"socat - 'SYSTEM:rm config,cd=.git'":                       "rm config",
		"socat - 'UNIX-SEND:/tmp/s,bind=.git/config'":              "rm .git/config",
		"socat -u 'OPEN:.git/config,new' /dev/null":                "rm .git/config",
		"socat -u 'OPEN:.git/config,delete' /dev/null":             "rm .git/config",
		"socat -u 'OPEN:.git/config,remove' /dev/null":             "rm .git/config",
		"socat -u 'OPEN:.git/config,truncate=0' /dev/null":         "tee .git/config",
		"socat -u 'OPEN:.git/config,o-create' /dev/null":           "tee .git/config",
		"socat 'UNIX-L:.git/config,unlink-early' /dev/null":        "rm .git/config",
	} {
		refused(t, s, c, why)
	}
	// An option the table does not know, or knows as unsafe here, on any address.
	for _, c := range []string{
		"socat 'PTY,umask=0' -", "socat 'EXEC:cat,path=/tmp/evil' -", "socat 'SYSTEM:x,shell=/tmp/evil' -", "socat 'EXEC:cat,setuid=root' -",
		"socat 'READLINE,newopt=1' -", "socat 'TCP:h:1,newopt=1' -", "socat STDIO,chroot=/ -", "socat - 'OPEN:notes.txt,fs-immutable'",
		"socat -u 'OPEN:notes.txt,chdir=/dev' -", "socat - 'OPEN:notes.txt,seek=5'", "socat -u 'OPEN:notes.txt,perm=0777' -",
		"socat 'PTY,link=./ttyV0,user=root' -", "socat 'EXEC:cat,ioctl-int=1:2' -", "socat - 'TCP:h:1,exec=x'",
		// socat reads the `b` as an option (and refuses it): an unknown option asks.
		"socat - 'SYSTEM:echo a,b'",
	} {
		notAllowed(t, s, c)
	}
	for _, c := range []string{
		"socat - /dev/ttyUSB0,b115200,raw,echo=0,vmin=1,vtime=0", "socat UNIX-LISTEN:/tmp/a.sock,fork,user=me,group=me,perm=0660 TCP:h:1",
		"socat - 'OPEN:out.txt,creat,perm=0644'", "socat -u 'OPEN:big.log,seek=100' -", "socat PTY,link=/tmp/ttyV0,waitslave,raw,echo=0 -",
		"socat 'SYSTEM:make,chdir=build' -", "socat 'EXEC:ls,cd=/tmp' -", "socat UNIX-LISTEN:.git/x.sock -", "socat 'UNIX-RECVFROM:./r.sock' -",
		"socat - TCP:h:1,nodelay,keepalive,connect-timeout=5", "socat OPENSSL:h:443,verify=0,cafile=ca.pem -",
		"socat TCP-LISTEN:9,fork,reuseaddr,range=10.0.0.0/8 -", "socat - PROXY:p:h:22,proxyport=3128", "socat PTY,link=./ttyV1,rawer -",
		"socat STDIO EXEC:bash,pty,stderr,setsid,sigint,sane,ctty", "socat - 'SYSTEM:echo hi,stderr'", "socat - UNIX-SEND:/tmp/s",
	} {
		allowed(t, s, c)
	}
}
