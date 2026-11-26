//go:build !linux

package sandbox

import "os"

// Enter runs nothing off Linux, where the sandbox does not run.
func Enter(args []string) int {
	os.Stderr.WriteString("jhon sandbox: the sandbox does not run on this system, so nothing was run\n")
	return 126
}
