package audit

import (
	"strings"
	"testing"
)

// edit-before-red reads the RED the bench runs really showed (T43, L415):
// a run behind a pipe, a run only at a shell segment's start, and a test
// run by hand before the code edit.

const (
	pipedRun   = "jhon executions run " + acme + " 2>&1 | tail -25"
	failedRun  = "FAIL: test_x\n<== .: exit 1"
	passedRun  = "Ran 3 tests\nOK\n<== .: exit 0"
	commitRun  = `git commit -m "fix totals, verified with jhon executions run ` + acme + `"`
	compound   = "jhon executions run " + acme + " | tail -3; jhon deliver check " + acme
	handTest   = "cd svca && python3 -m unittest"
	handFailed = "FAIL: test_x\nFAILED (failures=1)"
)

func ccCase(t *testing.T, calls ...tcall) []Finding {
	t.Helper()
	return findingsOf(t, "edit-before-red", user("go"), delegate("d01", "jhon-builder", acme, calls...))
}

func piCase(t *testing.T, calls ...pcall) []Finding {
	t.Helper()
	steps := []pstep{puser("go")}
	for _, c := range calls {
		steps = append(steps, pmain(c))
	}
	return piFindings(t, "edit-before-red", steps...)
}

func pfail(command, result string) pcall {
	c := pbash(command, result)
	c.isError = true
	return c
}

func pedit(p string) pcall {
	return pcall{name: "edit", args: obj("path", testRoot+"/"+p, "edits", []any{obj("oldText", "a", "newText", "b")}), result: "ok"}
}

func TestEditBeforeRedRealCases(t *testing.T) {
	const testFile, codeFile = "svca/tests/test_totals.py", "svca/src/totals.py"
	cases := []struct {
		name string
		cc   []tcall
		pi   []pcall
		want int
	}{
		{"clean-piped-red",
			[]tcall{codeEdit(testFile), bash(pipedRun, failedRun), codeEdit(codeFile), bash(pipedRun, passedRun), bash(commitRun, "Exit code 128\nAuthor identity unknown", true)},
			[]pcall{pedit(testFile), pbash(pipedRun, failedRun), pedit(codeFile), pbash(pipedRun, passedRun), pfail(commitRun, "Exit code 128\nAuthor identity unknown")}, 0},
		{"clean-compound",
			[]tcall{codeEdit(testFile), bash(pipedRun, failedRun), codeEdit(codeFile), bash(compound, "<== .: exit 0\nmissing: tested revision", true)},
			[]pcall{pedit(testFile), pbash(pipedRun, failedRun), pedit(codeFile), pfail(compound, "<== .: exit 0\nmissing: tested revision")}, 0},
		{"violation",
			[]tcall{codeEdit(codeFile), bash(pipedRun, failedRun)},
			[]pcall{pedit(codeFile), pbash(pipedRun, failedRun)}, 1},
		{"hand-red",
			[]tcall{codeEdit(testFile), bash(handTest, handFailed, true), codeEdit(codeFile), bash(pipedRun, failedRun)},
			[]pcall{pedit(testFile), pfail(handTest, handFailed), pedit(codeFile), pbash(pipedRun, failedRun)}, 1},
	}
	for _, c := range cases {
		for rt, got := range map[string][]Finding{"claude": ccCase(t, c.cc...), "pi": piCase(t, c.pi...)} {
			if len(got) != c.want {
				t.Errorf("%s %s: want %d findings, got %d %+v", c.name, rt, c.want, len(got), got)
				continue
			}
			if c.name == "violation" {
				eq(t, c.name+" "+rt+" kind", got[0].Kind, "would-be-violation")
			}
			if c.name == "hand-red" {
				f := got[0]
				eq(t, c.name+" "+rt+" severity", f.Severity, "suspect")
				eq(t, c.name+" "+rt+" kind", f.Kind, "finding")
				if !strings.Contains(f.Message, "the failing test ran by hand at line ") || !strings.Contains(f.Message, "not through `jhon executions run`") {
					t.Errorf("%s %s message: %s", c.name, rt, f.Message)
				}
			}
		}
	}
}

// A run counts only at a shell segment's start, after quotes are stripped.
func TestExecutionsRunSegment(t *testing.T) {
	for cmd, want := range map[string]string{
		"jhon executions run acme":                 "acme",
		"cd svca && jhon executions run acme":      "acme",
		"X=1 Y=2 jhon executions run acme | tail":  "acme",
		"(jhon executions run acme)":               "acme",
		"true; jhon executions run acme":           "acme",
		"echo hi\njhon executions run acme":        "acme",
		`git commit -m "jhon executions run acme"`: "",
		`echo 'jhon executions run acme'`:          "",
		"echo jhon executions run acme":            "",
	} {
		eq(t, cmd, runChange(cmd), want)
	}
}

// Edits between a change's RED and its GREEN are that change's (T47, L441):
// they are not held against the next change's RED. One RED per behaviour
// change (contracts/tdd.md).
func TestEditBeforeRedResetsAtTheChangesGreen(t *testing.T) {
	const a, b, codeFile = "acme-a", "acme-b", "svca/src/totals.py"
	run := func(change string) string { return "jhon executions run " + change + " 2>&1 | tail -25" }
	cases := []struct {
		name string
		cc   []tcall
		pi   []pcall
		want int
	}{
		{"red, edit, green twice",
			[]tcall{bash(run(a), failedRun), codeEdit(codeFile), bash(run(a), passedRun), bash(run(b), failedRun), codeEdit(codeFile), bash(run(b), passedRun)},
			[]pcall{pbash(run(a), failedRun), pedit(codeFile), pbash(run(a), passedRun), pbash(run(b), failedRun), pedit(codeFile), pbash(run(b), passedRun)}, 0},
		{"single-segment runs, no verdict line",
			[]tcall{red(a), codeEdit(codeFile), green(a), red(b), codeEdit(codeFile), green(b)},
			[]pcall{pfail("jhon executions run "+a, "1 failed"), pedit(codeFile), pbash("jhon executions run "+a, "all passed"),
				pfail("jhon executions run "+b, "1 failed"), pedit(codeFile), pbash("jhon executions run "+b, "all passed")}, 0},
		{"an edit after a's GREEN, before b's RED",
			[]tcall{bash(run(a), failedRun), codeEdit(codeFile), bash(run(a), passedRun), codeEdit(codeFile), bash(run(b), failedRun), bash(run(b), passedRun)},
			[]pcall{pbash(run(a), failedRun), pedit(codeFile), pbash(run(a), passedRun), pedit(codeFile), pbash(run(b), failedRun), pbash(run(b), passedRun)}, 1},
	}
	for _, c := range cases {
		for rt, got := range map[string][]Finding{"claude": ccCase(t, c.cc...), "pi": piCase(t, c.pi...)} {
			if len(got) != c.want {
				t.Errorf("%s %s: want %d findings, got %d %+v", c.name, rt, c.want, len(got), got)
			} else if c.want == 1 && !strings.Contains(got[0].Message, "changes code of "+b+" before its RED") {
				t.Errorf("%s %s message: %s", c.name, rt, got[0].Message)
			}
		}
	}
}
