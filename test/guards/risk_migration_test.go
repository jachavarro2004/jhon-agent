// A data migration takes the verified route as any other high risk (T148):
// a PR whose branch adds a root migrate.py, with its test and its record,
// opens only once an independent verification is on record; the same
// change without the migration opens on the standard route.

package guards

import (
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

func TestRouteGuardDataMigrationIsVerified(t *testing.T) {
	t.Parallel()
	pr := cmd("gh pr create --title x --body y")
	record := map[string]string{"openspec/changes/c1/tasks.md": "## Slice 1: a\n- [x] 1.1 a\n", "openspec/changes/c1/specs/a/spec.md": "# a\n", "openspec/changes/c1/execution-record.json": redGreenRecord}
	r := newRG(t, "")
	support.Write(t, r.p, map[string]string{"src/app.py": "X = 2\n", "migrate.py": "def migrate(path): pass\n", "tests/test_migrate.py": "def test_m(): pass\n"})
	support.Write(t, r.p, record)
	res := r.call(t, "Bash", pr, true)
	code(t, res, 2, "a data migration without a verification")
	matches(t, res.Stderr, `^route-guard: the route is verified \(risk high reaches the verified tier`, "the refusal names the route's reason")
	matches(t, res.Stderr, "launch the `jhon-checker` agent and record its verify-report.md", "the refusal says how")
	support.Write(t, r.p, map[string]string{"openspec/changes/c1/verify-report.md": "verdict: pass\n", "openspec/changes/c1/review-prism.md": "facets: safety\nverdict: pass\n"})
	code(t, r.call(t, "Bash", pr, true), 0, "a verified data migration")

	std := newRG(t, "")
	support.Write(t, std.p, map[string]string{"src/app.py": "X = 2\n", "tests/test_migrate.py": "def test_m(): pass\n"})
	support.Write(t, std.p, record)
	code(t, std.call(t, "Bash", pr, true), 0, "a test named after a migration is the standard route")
}
