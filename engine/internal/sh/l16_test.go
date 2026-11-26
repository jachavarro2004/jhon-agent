package sh

import (
	"reflect"
	"testing"
)

// T228 L16: every socat address that writes a file is read as tee writing
// it; a serial character device, by its path's shape, is not.
func TestSocatWriteSides(t *testing.T) {
	tee := func(p string) Inner { return Inner{Words: [][]string{{"tee", p}}} }
	for line, want := range map[string]Inner{
		"socat - OPEN:f":                tee("f"),
		"socat - GOPEN:f":               tee("f"),
		"socat - FILE:f":                tee("f"),
		"socat -u FILE:f -":             {},
		"socat -u file:f,trunc -":       tee("f"),
		"socat - f/g":                   tee("f/g"),
		"socat - OPEN:f,o-wronly":       tee("f"),
		"socat -u - OPEN:f":             tee("f"),
		"socat -U OPEN:f -":             tee("f"),
		"socat -u OPEN:f -":             {},
		"socat -U - OPEN:f":             {},
		"socat -u OPEN:f,trunc -":       tee("f"),
		"socat -u OPEN:f,ftruncate=0 -": tee("f"),
		"socat -u GOPEN:f,creat=0 -":    tee("f"),
		"socat -U - OPEN:f,o-creat":     tee("f"),
		"socat -U - OPEN:f,O-TRUNC":     tee("f"),
		"socat -u CREATE:f -":           tee("f"),
		"socat -u OPEN:f,append -":      {},
		"socat -u TCP:h:1,trunc -":      {},
		"socat - /dev/sda":              tee("/dev/sda"),
		"socat - /dev/nvme0n1":          tee("/dev/nvme0n1"),
		"socat - /dev/mem":              tee("/dev/mem"),
		"socat - /dev/ttyUSB0/../sda":   tee("/dev/ttyUSB0/../sda"),
		"socat - /dev/ttyUSB0,raw":      {},
		"socat - /dev/ttyACM1":          {},
		"socat - /dev/ttyS0":            {},
		"socat - /dev/tty":              {},
		"socat - /dev/pts/3":            {},
		"socat - /dev/serial/by-id/x":   {},
		"socat -u - OPEN:/dev/ttyUSB0":  {},
		"socat - //dev/ttyS0":           {},
		"socat - dev/ttyS0":             tee("dev/ttyS0"),
		"socat - /dev/pts/../sda":       tee("/dev/pts/../sda"),
		"socat PTY,link=/tmp/a PTY":     {Words: [][]string{{"rm", "/tmp/a"}}}, // T228 L17: link= replaces its path
		"socat - TCP:h:1":               {},
		"socat -u TCP-LISTEN:9 STDOUT":  {},
		"socat STDIO EXEC:cat":          {Strings: []string{"cat"}},
	} {
		if got, err := socatRuns(splitWords(line)); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("socatRuns(%q) = %+v, %v, want %+v", line, got, err, want)
		}
	}
	for p, want := range map[string]bool{
		"/dev/ttyUSB0": true, "/dev/ttyACM0": true, "/dev/ttyS0": true, "/dev/tty": true, "/dev/pts/0": true, "/dev/serial/by-path/x": true,
		"/dev/sda": false, "/dev/nvme0n1": false, "/dev/vda": false, "/dev/loop0": false, "/dev/dm-0": false, "/dev/mem": false,
		"/dev/tty0/../sda": false, "/dev/ttyx/y": false, "/dev/serial": false, "/dev/pts": false, "dev/ttyS0": false, "/tmp/ttyS0": false,
	} {
		if got := socatSerial(p); got != want {
			t.Errorf("socatSerial(%q) = %v, want %v", p, got, want)
		}
	}
}
