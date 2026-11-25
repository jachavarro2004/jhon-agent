//go:build !linux

package integrations

// reaping runs run: outside Linux there is no child subreaper, and only
// the program's process group is killed when it returns.
func reaping(run func() error) error { return run() }

// mountedNoexec is false where jhon does not read the mount's flags.
var mountedNoexec = func(string) bool { return false }
