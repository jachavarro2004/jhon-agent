//go:build !linux

package sandbox

import (
	"errors"
	"os"
)

// errNotLinux is every descriptor's error off Linux, where the sandbox does
// not run.
var errNotLinux = errors.New("the sandbox does not run on this system")

// openNoLink opens nothing off Linux.
func openNoLink(path string) (*os.File, error) { return nil, errNotLinux }

// openBind opens nothing off Linux.
func openBind(in Input, m mount) (*os.File, *Problem) {
	return nil, &Problem{Reason: errNotLinux.Error(), Fix: "none on this system: it needs Linux and bubblewrap"}
}

// Inherit passes nothing off Linux.
func (b *Bound) Inherit() error { return errNotLinux }
