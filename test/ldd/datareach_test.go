// A run whose change's slice touches a declared data-access path, and whose
// tests have no pytest-testmon record, says the real-database reach could not
// be checked (T138, L1064 KC3).
package ldd

import (
	"strings"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestExecutionsRunNamesAnUncheckedDataAccessReach(t *testing.T) {
	t.Parallel()
	const note = "lib/index.js is under a declared data-access path (executions_data_access), and whether a test declared as real-database (executions_database_tests) reached it in the recorded run could not be checked for lack of a pytest-testmon record of its tests; a slice commit is not refused for it"
	overrides := "test_command_override:\n  lib: true\n  app: true\n  tool: true\n  other: true\n"
	env := support.Env("XDG_CACHE_HOME=" + temp(t))
	for _, c := range []struct {
		config string
		noted  bool
	}{
		{"delivery: change\nexecutions_data_access: [lib/**]\nexecutions_database_tests: [lib/tests/*.py]\n" + overrides, true},
		{"delivery: change\n" + overrides, false},
	} {
		root := nodeWorkspace(t, map[string]string{"openspec/config.yaml": c.config})
		support.Write(t, root, map[string]string{"lib/index.js": "module.exports = 3;\n"})
		r := support.Jhon(t, root, env, "executions", "run", "c1")
		equal(t, r.Code, 0, r.Stderr)
		equal(t, strings.Contains(r.Stdout, "note       "+note+"\n"), c.noted, r.Stdout)
	}
}
