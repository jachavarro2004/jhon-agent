package ldd

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T256 verify: Jhon's sandbox runs a command under --unshare-pid, where
// jhon is pid 2 and sees none of the host's processes. A sandboxed run's
// sweep must not take a live run's private copy for a killed run's: the
// run in the owner's terminal, which finishes last, publishes its record.
func TestExecutionsASandboxedRunDoesNotSweepALiveRunsCopy(t *testing.T) {
	t.Parallel()
	bwrap, err := exec.LookPath("bwrap")
	if err != nil || exec.Command(bwrap, "--dev-bind", "/", "/", "--proc", "/proc", "--unshare-pid", "true").Run() != nil {
		t.Skip("bubblewrap starts the other pid namespace")
	}
	p := newPytestProject(t)
	equal(t, p.jhon(t, nil, "executions", "prime").Code, 0)
	q := p.worktree(t)
	mine, theirs := p.otherRecord(t, 0), p.otherRecord(t, 1)
	host := exec.Command(support.Bin(t), "executions", "run", "c1", "--affected", "base", "--json")
	host.Dir, host.Env = p.root, support.Env(append(append([]string{}, p.env...), "FAKE_UV_RECORD="+mine, "FAKE_UV_SLEEP=6")...)
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	for end := time.Now().Add(30 * time.Second); !slices.ContainsFunc(leftBeside(t, p), func(n string) bool { return strings.HasSuffix(n, ".run") }); {
		if time.Now().After(end) {
			t.Fatal("the host's run made no private copy")
		}
		time.Sleep(50 * time.Millisecond)
	}
	r := support.Run(t, support.Cmd{Args: []string{bwrap, "--dev-bind", "/", "/", "--proc", "/proc", "--unshare-pid", "--die-with-parent", support.Bin(t), "executions", "run", "c1", "--affected", "base", "--json"},
		Dir: q.root, Env: support.Env(append(append([]string{}, q.env...), "FAKE_UV_RECORD="+theirs)...)})
	equal(t, r.Code, 0, r.Out())
	if err := host.Wait(); err != nil {
		t.Fatal(err)
	}
	equal(t, p.shared(t), support.Read(t, mine), "the run that finished last publishes its record")
}
