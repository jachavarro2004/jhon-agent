package sh

import (
	"errors"
	"reflect"
	"testing"
)

// T228 L18: every option on every socat address is read through
// socatOptions; a listener removes only with an unlink option; chdir moves
// a command.
func TestSocatOptionsEveryAddress(t *testing.T) {
	for line, want := range map[string]Inner{
		"socat PTY,symbolic-link=g -":                                   {Words: [][]string{{"rm", "g"}}},
		"socat SYSTEM:true,pty,symbolic-link=g -":                       {Words: [][]string{{"rm", "g"}}, Strings: []string{"true"}},
		"socat -u READLINE,history-file=h -":                            {Words: [][]string{{"tee", "h"}}},
		"socat - SYSTEM:x,chdir=d":                                      {Strings: []string{"cd d && x"}},
		"socat - SHELL:x,cd=d":                                          {Strings: []string{"cd d && x"}},
		"socat - EXEC:x,chdir=a_b":                                      {Strings: []string{"cd 'a b' && x"}},
		"socat - UNIX-SEND:s,bind=b":                                    {Words: [][]string{{"rm", "b"}}},
		"socat - UNIX-SEND:s":                                           {},
		"socat -u OPEN:f,new /dev/null":                                 {Words: [][]string{{"rm", "f"}, {"tee", "/dev/null"}}},
		"socat -u OPEN:f,delete -":                                      {Words: [][]string{{"rm", "f"}}},
		"socat -u OPEN:f,truncate=0 -":                                  {Words: [][]string{{"tee", "f"}}},
		"socat -u OPEN:f,o-create -":                                    {Words: [][]string{{"tee", "f"}}},
		"socat UNIX-LISTEN:s,fork -":                                    {},
		"socat UNIX-LISTEN:s,fork,mode=600 -":                           {},
		"socat UNIX-LISTEN:s,unlink-early -":                            {Words: [][]string{{"rm", "s"}}},
		"socat -u - CREATE:f,perm=0644":                                 {Words: [][]string{{"tee", "f"}}},
		"socat - OPEN:f,creat,user=me":                                  {Words: [][]string{{"tee", "f"}}},
		"socat -u FILE:f,seek-end=0,ignoreeof -":                        {},
		"socat -u OPEN:f,seek=9 -":                                      {},
		"socat - /dev/ttyUSB0,b115200,raw,echo=0,nonblock,min=1,time=0": {},
		"socat PTY,link=/tmp/t,raw,echo=0,waitslave -":                  {Words: [][]string{{"rm", "/tmp/t"}}},
		"socat - TCP:h:1,bind=0.0.0.0:9,nodelay":                        {},
		"socat TCP-LISTEN:9,fork,unlink-early -":                        {},
	} {
		got, err := socatRuns(splitWords(line))
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("socatRuns(%q) = %+v, %v; want %+v", line, got, err, want)
		}
	}
	for _, line := range []string{
		"socat PTY,umask=0 -", "socat EXEC:x,path=/tmp -", "socat SYSTEM:x,shell=/tmp/s -", "socat EXEC:x,setuid=root -",
		"socat READLINE,newopt=1 -", "socat - TCP:h:1,newopt=1", "socat STDIO,chroot=/ -", "socat - OPEN:f,fs-immutable",
		"socat -u OPEN:f,chdir=/dev -", "socat - OPEN:f,seek=5", "socat -u OPEN:f,perm=0777 -", "socat PTY,link=g,user=root -",
		"socat EXEC:x,ioctl-int=1:2 -", "socat - TCP:h:1,exec=x", "socat - SYSTEM:echo_a,b", "socat - SOCKET-LISTEN:1:0:x,unlink-early",
		"socat - OPEN:f,lseek64-set=1", "socat PTY,link=g,openpty -",
	} {
		if _, err := socatRuns(splitWords(line)); !errors.As(err, new(*Unreadable)) {
			t.Errorf("socatRuns(%q): %v, want unreadable", line, err)
		}
	}
}
