package render

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T146g G3 (L1273, L1274): a small change keeps no tasks.md, archive, light
// verify or todo list, and every text that would ask for one exempts it in
// the same words: the orchestrator, the light-verify rule, and pi's todo
// tool text, which asked for a list at "any change to code". G2: the
// orchestrator and the delivery contract order a delivery GREEN run, commit,
// `jhon deliver check`, never a run after the commit.
const smallKeepsNo = "small change (`jhon status` says so) keeps no tasks.md, archive, light verify or todo list"

func TestT146gSmallChangeExemptInTheSameWords(t *testing.T) {
	t.Parallel()
	read := func(rel string) string { return flat(support.Read(t, filepath.Join(repoRoot, rel))) }
	light := read("prose/contracts/ldd.md")
	light = light[strings.Index(light, "### Light verify"):]
	light = light[:strings.Index(light, "## Delivery")]
	for name, text := range map[string]string{
		"prose/orchestrator.md":         read("prose/orchestrator.md"),
		"contracts/ldd.md Light verify": light,
		"shell/lib/ui/shell-todo.ts":    read("shell/lib/ui/shell-todo.ts"),
	} {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(smallKeepsNo)) {
			t.Errorf("%s does not exempt a small change in the shared words %q", name, smallKeepsNo)
		}
	}
	if s := read("shell/lib/ui/shell-todo.ts"); strings.Contains(s, "any change to code") {
		t.Error("pi's todo text still asks for a list at any change to code")
	}
}

func TestT146gDeliveryOrderIsRunCommitCheck(t *testing.T) {
	t.Parallel()
	order := "after the GREEN run, commit the tree it tested, then `jhon deliver check"
	for _, rel := range []string{"prose/orchestrator.md", "prose/contracts/delivery.md"} {
		s := flat(support.Read(t, filepath.Join(repoRoot, rel)))
		if !strings.Contains(s, order) {
			t.Errorf("%s does not order a delivery %q", rel, order)
		}
		if strings.Contains(s, "on the clean tree at HEAD") {
			t.Errorf("%s still asks for a run after the commit", rel)
		}
	}
}
