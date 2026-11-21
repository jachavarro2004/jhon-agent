package render

import (
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T195 (L1607, L1609): two builders on pi's agents rail both read
// "## Why dele…", the first line of their briefs, and the owner could not
// tell which sub-agent to look at. The delegation rules ask for a short label
// of what each launched sub-agent does, the one the rail and task list show.
func TestDelegationAsksForAnAgentLabel(t *testing.T) {
	t.Parallel()
	d := flat(support.Read(t, treePath(repoRoot, "contracts/delegation.md")))
	match(t, d, "Give each launch a short label of what the sub-agent does, a few words such as `web: categories page`", "contracts/delegation.md asks for a label")
	match(t, d, "never a heading of its brief", "contracts/delegation.md: a heading is no label")
}
