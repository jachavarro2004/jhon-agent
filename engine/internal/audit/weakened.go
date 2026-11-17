package audit

import (
	"fmt"
	"path"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
)

// test-weakened (tdd/no-weaken, T56): in one transcript, after a failing
// test run (a RED `jhon executions run`, or a failing test run by hand) and
// before the next passing one, an Edit of a test file whose new text adds
// a skip marker the old text lacked, or holds fewer test blocks than the
// old text: the test made to stop failing instead of the code made to pass;
// once per transcript. A Write is not read: the file before it is not in
// the transcript.

var (
	skipMarker = lazyre.New(`\bt\.Skip(?:Now|f)?\(|@pytest\.mark\.(?:skip|xfail)|\bpytest\.skip\(|@unittest\.skip|\bunittest\.skip\(|\.skip\(|\bx(?:it|test|describe)\(|\b(?:it|test)\.todo\(|#\[ignore\]|@Disabled\b|@Ignore\b`)
	testBlock  = lazyre.New(`\bfunc Test\w*\(|\bdef test_?\w*\(|\b(?:it|test|describe)\(\s*['"` + "`" + `]|#\[test\]|@Test\b`)
)

// testFile is whether file is a test: under a test directory, or named as one.
func testFile(file string) bool {
	f := "/" + strings.TrimPrefix(file, "/")
	return !strings.Contains(f, "/openspec/") && !behaviourFile(f) && !notCode[strings.ToLower(path.Ext(f))]
}

// weakens is how an edit's old and new text weaken a test, or "".
func weakens(old, new string) string {
	switch {
	case len(skipMarker.FindAllString(new, -1)) > len(skipMarker.FindAllString(old, -1)):
		return "adds a skip marker"
	case len(testBlock.FindAllString(new, -1)) < len(testBlock.FindAllString(old, -1)):
		return "removes a test block"
	}
	return ""
}

// editPairs are an Edit's or MultiEdit's old and new texts.
func editPairs(u ToolUse) [][2]string {
	if list, ok := propList(u.Input, "edits"); ok {
		var out [][2]string
		for _, x := range list {
			o, _ := strProp(x, "old_string")
			n, _ := strProp(x, "new_string")
			out = append(out, [2]string{o, n})
		}
		return out
	}
	o, _ := strProp(u.Input, "old_string")
	n, _ := strProp(u.Input, "new_string")
	return [][2]string{{o, n}}
}

func checkTestWeakened(s *Session, emit emitFunc) {
	for _, t := range s.transcripts() {
		runs := map[string]string{} // tool use id → command: an executions run, or a test run by hand
		failing := 0                // the line of the failing run the next pass closes, or 0
	transcript:
		for _, e := range t.Entries {
			for _, r := range e.ToolResults {
				c, ok := runs[r.ID]
				if !ok {
					continue
				}
				failed := handRed(r)
				if runChange(c) != "" {
					failed = runFailed(c, r)
				}
				if failed {
					if failing == 0 {
						failing = e.Line
					}
				} else {
					failing = 0
				}
			}
			for _, u := range e.ToolUses {
				if c, ok := strProp(u.Input, "command"); ok && (u.Name == "Bash" || u.Name == "bash") {
					if runChange(c) != "" || testRunner.MatchString(unquoted(c)) {
						runs[u.ID] = c
					}
					continue
				}
				tool, file, ok := writeOf(u)
				if !ok || failing == 0 || tool == "Write" || tool == "write" || !testFile(file) {
					continue
				}
				for _, p := range editPairs(u) {
					if how := weakens(p[0], p[1]); how != "" {
						emit(emitted{check: "test-weakened", severity: "violation", transcript: t.File, line: e.Line, agent: t.Agent, agentType: t.AgentType,
							evidence: short(tool+" "+file, 120),
							message:  fmt.Sprintf("after the failing test run at line %d, this %s of %s %s before any run passes: a failing test is made to pass by the code, never weakened (contracts/tdd.md)", failing, tool, file, how)})
						break transcript
					}
				}
			}
		}
	}
}
