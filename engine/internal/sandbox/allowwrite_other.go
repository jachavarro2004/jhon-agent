//go:build !linux

package sandbox

import "errors"

// mkdirUnder makes nothing off Linux, where the sandbox does not run.
func mkdirUnder(home, folder string) error {
	return errors.New("the sandbox does not run on this system")
}
