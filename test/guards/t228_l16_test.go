// T228 L16 (re-verify mwxck6ec of 96411a7): every socat address that
// writes a file is judged as tee writing it. On either side creat, trunc
// and ftruncate (any spelling, any value) write; on the side socat writes
// (both without -u or -U) every OPEN, GOPEN, CREATE or bare path writes;
// a serial character device (/dev/tty*, /dev/pts/*, /dev/serial/*) is not
// judged as a socat write, every other device is. The verifier's verbatim
// rows and test were not in this worktree: these are written from L16.
package guards

import "testing"

func TestT228L16SocatWrites(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range []string{
		// The side socat writes: any file address, with no option.
		"socat -u - OPEN:.git/config", "socat - .git/config", "socat - GOPEN:.git/config", "socat - FILE:.git/config", "socat -u 'file:.git/config,trunc' -", "socat - OPEN:.git/config,o-wronly",
		"socat -U OPEN:.git/HEAD -", "socat -u - /dev/sda", "socat TCP:h:1 .git/hooks/pre-commit",
		// The side -u or -U leaves read-only, with an option that truncates or creates.
		"socat -u 'OPEN:.git/config,trunc' /dev/null", "socat -U - 'OPEN:.git/HEAD,o-trunc'", "socat -u 'OPEN:.git/config,ftruncate=0' -",
		"socat -u 'GOPEN:.git/hooks/pre-commit,creat=0' -", "socat -U - 'OPEN:.git/hooks/x,o_creat'", "socat -u CREATE:.git/hooks/x -",
		// Block devices and every other device stay judged; the path's shape is read once cleaned.
		"socat - /dev/nvme0n1", "socat -u - /dev/vda", "socat - /dev/loop0", "socat - /dev/dm-0", "socat - /dev/mem",
		"socat - /dev/ttyUSB0/../sda", "socat - /dev/serial/../sda",
	} {
		refused(t, s, c, "it writes")
	}
	refused(t, s, "socat -u - OPEN:~/.aws/credentials", "~/.aws/credentials")
	for _, c := range []string{
		// The owner's serial work.
		"socat - /dev/ttyUSB0,b115200,raw,echo=0", "socat /dev/ttyACM0,raw -", "socat - /dev/ttyS0", "socat - /dev/pts/3",
		"socat - /dev/serial/by-id/usb-FTDI_x-if00-port0,raw", "socat -u - OPEN:/dev/ttyUSB0", "socat - /dev/tty",
		"socat PTY,link=/tmp/ttyV0,raw PTY,link=/tmp/ttyV1,raw",
		// A read-only side with no option that writes, and writes the guard allows.
		"socat -u OPEN:.git/config STDOUT", "socat -u FILE:.git/config STDOUT", "socat -u - 'OPEN:out.txt,creat'", "socat -U - OPEN:.git/config", "socat - OPEN:notes.txt",
		"socat -u TCP-LISTEN:9 OPEN:out.txt,creat,trunc", "socat - /dev/null", "socat -u TCP:h:1 /dev/stdout",
		// Rounds 8 and 9.
		"socat -lf socat.log - TCP:h:1", "ncat -o out.txt h 1", "ncat --output=out.txt h 1", "ncat -x hex.log h 1", "nc -o dump h 1",
		"socat - 'SYSTEM:echo a,b'", "ncat --ssl h 443", "ncat --proxy-type socks5 --proxy p:1 h 22", "socat - TCP:h:22",
	} {
		allowed(t, s, c)
	}
}
