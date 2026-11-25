//go:build !unix

package integrations

import "os/exec"

// ownGroup leaves cmd as it is where there are no process groups: the cut
// kills the program alone (Jhon runs on Linux; on Windows, inside WSL 2).
func ownGroup(*exec.Cmd) {}

// killGroup does nothing where there are no process groups.
func killGroup(*exec.Cmd) {}
