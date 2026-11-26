package ldd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// T253 verify: in a project in a subfolder of its repository, a run from
// the slice's own base whose slice changed the shared lib is never told to
// measure from the change's base on the default branch.
func TestExecutionsSpanNoteInASubfolderProjectNamesNoWiderBase(t *testing.T) {
	t.Parallel()
	top := tree(t, map[string]string{
		"mono/lib/package.json":             `{"name":"lib"}`,
		"mono/lib/index.js":                 "module.exports = 1;\n",
		"mono/app/package.json":             `{"name":"app","dependencies":{"lib":"file:../lib"}}`,
		"mono/app/index.js":                 "require('lib');\n",
		"mono/tool/package.json":            `{"name":"tool","dependencies":{"lib":"file:../lib"}}`,
		"mono/tool/index.js":                "require('lib');\n",
		"mono/other/package.json":           `{"name":"other"}`,
		"mono/other/index.js":               "module.exports = 2;\n",
		"mono/openspec/config.yaml":         "test_command_override:\n  lib: true\n  app: true\n  tool: true\n  other: true\n",
		"mono/openspec/changes/c1/tasks.md": threeSlices(0),
	})
	support.GitRepo(t, top)
	support.Git(t, top, "add", "-A")
	support.Git(t, top, "commit", "-qm", "base")
	main := strings.TrimSpace(support.Git(t, top, "rev-parse", "HEAD"))
	support.Git(t, top, "checkout", "-q", "-b", "feat")
	root := filepath.Join(top, "mono")
	support.Write(t, root, map[string]string{"lib/index.js": "module.exports = 3;\n", "openspec/changes/c1/tasks.md": threeSlices(1)})
	support.Git(t, top, "commit", "-qam", "slice 1")
	support.Write(t, root, map[string]string{"other/index.js": "module.exports = 4;\n", "openspec/changes/c1/tasks.md": threeSlices(2)})
	support.Git(t, top, "commit", "-qam", "slice 2")
	slice2 := strings.TrimSpace(support.Git(t, top, "rev-parse", "HEAD"))
	support.Write(t, root, map[string]string{"lib/index.js": "module.exports = 5;\n"})
	r := jhonGit(t, root, "executions", "run", "c1", "--affected", slice2, "--dry-run", "--json")
	equal(t, r.Code, 0, r.Out())
	noMatch(t, strings.Join(notesOf(t, r.Stdout), "\n"), "--affected "+main[:12])
}
