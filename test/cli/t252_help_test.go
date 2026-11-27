// T252 (odd/tasks/v0-1-release.md L1721, L1740): `jhon sandbox --help`
// describes every note a run can end with, the two T252 added included: the
// line given to a delivery line whose leading cd has a .. part, and the
// note on a docker daemon the hidden socket keeps out of reach.
package cli

import (
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestT252SandboxHelpDescribesTheNewNotes(t *testing.T) {
	t.Parallel()
	m := newMachine(t)
	r := support.Jhon(t, m.proj, m.env, "sandbox", "--help")
	help := strings.Join(strings.Fields(r.Stdout), " ")
	for _, want := range []string{
		"a line already alone is told what it holds; a line whose leading cd names its folder with a .. part is given the line with the folder's absolute path, which runs outside);",
		"(<tools> run ... of their own for two or more); with sandbox.docker off, any exit but a signal's whose output says docker cannot reach its daemon ends last with one line saying the sandbox hides the socket, so this says nothing of whether Docker runs, that a docker line of its own in the project runs outside, and that jhon settings set sandbox.docker true shows it; and with telemetry on",
	} {
		if r.Code != 0 || !strings.Contains(help, want) {
			t.Errorf("jhon sandbox --help: exit %d, lacks\n%s\nin\n%s", r.Code, want, help)
		}
	}
}
