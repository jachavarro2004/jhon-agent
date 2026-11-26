// T228 L15 re-verify: every file or device socat may write is judged as tee
// writing it; reads and serial lines stay allowed.
package guards

import "testing"

func TestT228L15VerifySocatWrites(t *testing.T) {
	t.Parallel()
	s := newSG(t)
	for _, c := range []string{
		"socat -u - OPEN:.git/config", "socat - .git/config", "socat -u - /dev/sda", "socat -u - OPEN:/dev/sda",
		"socat -u - OPEN:~/.aws/credentials", "socat -u - 'OPEN:.git/config,ftruncate=0'",
		"socat -u 'OPEN:.git/config,trunc' /dev/null", "socat -U - 'OPEN:.git/HEAD,o-trunc'",
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
	for _, c := range []string{"socat - /dev/ttyUSB0,b115200,raw,echo=0", "socat -u OPEN:.git/config STDOUT", "socat -lf socat.log - TCP:h:1",
		"socat -u - 'OPEN:out.txt,creat'", "socat -U - OPEN:.git/config"} {
		for _, o := range []sgOpts{{}, top()} {
			if r := s.bash(t, c, o); r.Code != 0 || r.Stdout != "" || r.Stderr != "" {
				t.Errorf("top %v: %q: %d %q %q", o.top, c, r.Code, r.Stderr, r.Stdout)
			}
		}
	}
}
