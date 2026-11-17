package execution

import (
	"encoding/json"
	"os/exec"

	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
)

// testmonReachers prints, as JSON, for a record and the files named after it,
// each file some test's last execution ran and the names of those tests.
const testmonReachers = `import sqlite3, sys, json
c = sqlite3.connect("file:" + sys.argv[1] + "?mode=ro", uri=True)
files = sys.argv[2:]
out = {}
if files:
    q = "select f.filename, t.test_name from test_execution t join (select max(id) id from test_execution group by test_name) l on l.id = t.id join test_execution_file_fp x on x.test_execution_id = t.id join file_fp f on f.id = x.fingerprint_id where f.filename in (" + ",".join("?" * len(files)) + ") order by f.filename, t.test_name"
    for f, name in c.execute(q, files):
        if name not in out.setdefault(f, []):
            out[f].append(name)
print(json.dumps(out))`

// TestmonReachers is, for each of files (relative to the record's rootdir,
// the repository's top level) that a test of the record at data ran in its
// last execution, the names of those tests (`<path>::<test>`): by file, as
// TestmonReach counts them. ok is false when there is no record or python3
// cannot read it.
func TestmonReachers(data string, files []string) (map[string][]string, bool) {
	if !project.Exists(data) {
		return nil, false
	}
	out, err := exec.Command("python3", append([]string{"-I", "-c", testmonReachers, data}, files...)...).Output()
	if err != nil {
		return nil, false
	}
	reach := map[string][]string{}
	if json.Unmarshal(out, &reach) != nil {
		return nil, false
	}
	return reach, true
}

// testmonHeld prints, as JSON, those of the test modules named after a
// record that it holds a test of (`<module>::<test>`).
const testmonHeld = `import sqlite3, sys, json
c = sqlite3.connect("file:" + sys.argv[1] + "?mode=ro", uri=True)
held = {r[0] for r in c.execute("select distinct substr(test_name, 1, instr(test_name, '::') - 1) from test_execution where instr(test_name, '::') > 0")}
print(json.dumps([m for m in sys.argv[2:] if m in held]))`

// TestmonHeld is those of modules (test modules relative to the record's
// rootdir, the repository's top level) that the record at data holds a test
// of, in their order: what pytest-testmon can narrow, since a test it holds
// no record of (a new module's) it runs whatever changed. ok is false when
// there is no record or python3 cannot read it.
func TestmonHeld(data string, modules []string) ([]string, bool) {
	if !project.Exists(data) {
		return nil, false
	}
	out, err := exec.Command("python3", append([]string{"-I", "-c", testmonHeld, data}, modules...)...).Output()
	if err != nil {
		return nil, false
	}
	var held []string
	if json.Unmarshal(out, &held) != nil {
		return nil, false
	}
	return held, true
}
