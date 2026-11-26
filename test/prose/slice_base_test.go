// Package prose checks rules of the framework's prose that no command
// enforces, read from prose/ as the renderer ships them.
package prose

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// flat is s with every run of whitespace one space, as a reader sees prose.
func flat(s string) string { return strings.Join(strings.Fields(s), " ") }

// T253 (odd/tasks/v0-1-release.md L1721): an orchestrator briefed a slice's
// run as `jhon executions run <change> --affected origin/main`, which
// selected every service depending on the shared library the change had
// touched in an earlier slice (hours), where the slice's own base selected
// about 5 minutes. Beside the rule that a slice's tests run as
// `--affected <base>`, the orchestrator is told which base that is: the
// commit the slice started from, never the default branch, and a brief
// names that commit.
func TestASlicesRunTakesTheCommitTheSliceStartedFrom(t *testing.T) {
	body := flat(support.Read(t, filepath.Join(support.Root(), "prose", "orchestrator.md")))
	rule := "before its commit, a slice's tests run as `jhon executions run <change> --affected <base>` from the worktree that holds the change"
	base := "Its `<base>` is the commit the slice started from, never the default branch (`origin/main`), which reaches every earlier slice and each project depending on what they changed; a brief names that commit."
	i, j := strings.Index(body, rule), strings.Index(body, base)
	if i < 0 {
		t.Fatalf("orchestrator.md lacks the slice's run rule %q", rule)
	}
	if j < 0 {
		t.Fatalf("orchestrator.md does not say which base a slice's run takes: %q", base)
	}
	// Beside the rule: after it, in the same bullet.
	if j < i || strings.Contains(body[i:j], " - **") || j-i > 400 {
		t.Errorf("the base is not said beside the slice's run rule:\n%s", body[min(i, j):max(i, j)+len(base)])
	}
	// No prose asks for the default branch as a slice's base.
	if m := regexp.MustCompile("--affected origin/(?:main|HEAD|master)`").FindString(body); m != "" {
		t.Errorf("orchestrator.md names %s as a base", m)
	}
}
