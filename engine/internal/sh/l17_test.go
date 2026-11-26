package sh

import (
	"errors"
	"reflect"
	"testing"
)

// T228 L17: socat by class: every path an address names is a write, a
// removal, or (with an option outside socatOptions) unreadable.
func TestSocatPathsByClass(t *testing.T) {
	type res struct{ writes, removes []string }
	for line, want := range map[string]res{
		"socat -u OPEN:f,unlink-close /dev/null":    {writes: []string{"/dev/null"}, removes: []string{"f"}},
		"socat -u OPEN:f,unlink-early /dev/null":    {writes: []string{"/dev/null"}, removes: []string{"f"}},
		"socat -U - GOPEN:f,unlink-late":            {removes: []string{"f"}},
		"socat PTY,link=g /dev/null":                {writes: []string{"/dev/null"}, removes: []string{"g"}},
		"socat EXEC:cat,pty,link=g -":               {removes: []string{"g"}},
		"socat UNIX-LISTEN:s,unlink-early -":        {removes: []string{"s"}},
		"socat UNIX-L:s -":                          {}, // T228 L18: a listener removes only with an unlink option
		"socat -u UNIX-RECV:s /dev/null":            {writes: []string{"/dev/null"}},
		"socat UNIX-RECVFROM:s -":                   {},
		"socat - UNIX-CONNECT:s,bind=b":             {removes: []string{"b"}},
		"socat - UNIX-SENDTO:s,bind=b":              {removes: []string{"b"}},
		"socat - UNIX-CONNECT:/var/run/docker.sock": {},
		"socat - UNIX:s":                            {},
		"socat - LOCAL:s":                           {},
		"socat - TCP:h:1,bind=0.0.0.0:9":            {},
		"socat TCP-LISTEN:9,fork,unlink-early -":    {},
		"socat -u - PIPE:f":                         {writes: []string{"f"}},
		"socat -U PIPE:f -":                         {writes: []string{"f"}},
		"socat - FIFO:f":                            {writes: []string{"f"}},
		"socat - ECHO:f":                            {writes: []string{"f"}},
		"socat - PIPE":                              {},
		"socat READLINE,history=h -":                {writes: []string{"h"}},
		"socat PTY,lockfile=l -":                    {writes: []string{"l"}},
		"socat PTY,waitlock=l -":                    {writes: []string{"l"}},
		"socat - NEWKIND:f":                         {writes: []string{"f"}},
		"socat - /dev/ttyUSB0,b115200,raw,echo=0":   {},
		"socat -u /dev/ttyS0,unlink-close -":        {removes: []string{"/dev/ttyS0"}},
		"socat - OPEN:f,creat,trunc,append":         {writes: []string{"f"}},
		"socat -u OPEN:f,rdonly -":                  {},
		"socat -d -d pty,raw,echo=0 pty,raw,echo=0": {},
	} {
		in, err := socatRuns(splitWords(line))
		var got res
		for _, w := range in.Words {
			switch w[0] {
			case "tee":
				got.writes = append(got.writes, w[1])
			case "rm":
				got.removes = append(got.removes, w[1])
			}
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("socatRuns(%q) = %+v, %v; want %+v", line, got, err, want)
		}
	}
	// A path-naming address with an option outside the harmless list.
	for _, line := range []string{
		"socat -u OPEN:f,perm=0777 -", "socat -u OPEN:f,user=root -", "socat - UNIX-CONNECT:s,newoption=1", "socat PTY,link=g,mode=777 -",
		"socat - /dev/ttyUSB0,b115200,seek=9", "socat READLINE,history=h,chroot=/ -", "socat - NEWKIND:f,x=1",
	} {
		if _, err := socatRuns(splitWords(line)); !errors.As(err, new(*Unreadable)) {
			t.Errorf("socatRuns(%q): %v, want unreadable", line, err)
		}
	}
	// No path: options are not this reader's to judge.
	for _, line := range []string{"socat TCP-LISTEN:8080,fork,reuseaddr,nodelay TCP:h:1", "socat OPENSSL-LISTEN:443,cert=c.pem,verify=0 -",
		"socat STDIO EXEC:cat,pty,stderr,setsid,ctty", "socat - TCP:h:1,keepalive,connect-timeout=5", "socat ABSTRACT-LISTEN:x,fork -"} {
		if _, err := socatRuns(splitWords(line)); err != nil {
			t.Errorf("socatRuns(%q): %v", line, err)
		}
	}
}
