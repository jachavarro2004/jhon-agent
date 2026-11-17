package hook

import (
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
)

// startStateWrites follows a commit route-guard accepted that closes a slice
// (T134 H4): no hook runs after a Bash command in either runtime, so for
// each change the commit closes a slice of, it starts `jhon context <change>
// --phase apply --save-state --after <HEAD>`, detached, which waits for the
// commit to land and writes the change's state to Engram. Nothing starts
// when Engram is off or does not resolve; nothing it does reaches the
// commit, which never waits for it.
func startStateWrites(top string) {
	// A Go test binary runs route-guard in process: it is not jhon, and
	// cannot run `jhon context`.
	if testing.Testing() || !settings.On("integrations.engram") || ldd.EngramBin() == "" {
		return
	}
	exe, err := os.Executable()
	head, _ := ldd.HeadOf(top)
	if err != nil || head == "" {
		return
	}
	for _, change := range ldd.ClosingChanges(top) {
		cmd := exec.Command(exe, "context", change, "--phase", "apply", "--root", top, "--save-state", "--after", head)
		cmd.Dir = top
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if cmd.Start() == nil {
			_ = cmd.Process.Release()
		}
	}
}
