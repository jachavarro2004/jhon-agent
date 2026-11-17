package audit

import (
	"fmt"
	lddlayout "github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"path"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
)

// edit-before-red (ldd/red-first, T31.6, L252 F): in one transcript, the
// first Edit or Write of a code file (not a test, a document, or anything
// under openspec/) is held against each change the transcript runs through
// `jhon executions run <change>`. It came before that change's RED, the
// first such run that failed, or before its record,
// openspec/changes/<change>/tasks.md, written after it: once per change.
// A change never run to a RED in the transcript is not read: nothing names
// it there. A write through Bash is not read. The code edits between a
// change's RED and its GREEN, a later run of it that passes, are its own
// implementation (T47, one RED per behaviour change); after its GREEN the
// next code edit is the next change's first.
//
// A run counts only where it starts a shell segment, outside quotes (T43,
// L415): a commit message naming it is not a run. Its RED is its own
// verdict, a `<== <label>: exit N` or `timeout after` line, whatever a pipe
// did to the exit code; with no verdict line, an error of a single-segment
// command. A failing test run by hand (not through executions) before the
// first code edit makes the finding a suspect: the RED came, just not
// through `jhon executions run`.

var executionsRun = lazyre.New(`(?:^|&&|\|\||[;|\n(])\s*(?:\w+=\S*\s+)*jhon\s+executions\s+run\s+([\w.-]+)`)

// verdict is one project's line of a run: `<== <label>: exit N` or `timeout after`.
var verdict = lazyre.New(`(?m)^<== .*?: (?:exit (-?\d+)|timeout after)`)

// testRunner is a test command run by hand; testFailed its output when a test failed.
var (
	testRunner = lazyre.New(`\b(?:python3?\s+-m\s+(?:unittest|pytest)|pytest|go\s+test|npm\s+(?:run\s+)?test|node\s+--test|bun\s+test|cargo\s+test|npx\s+(?:jest|vitest))\b`)
	testFailed = lazyre.New(`FAILED \(|FAIL:|--- FAIL|\b\d+ failed`)
)

// runChange is the change command runs through `jhon executions run`, or "".
func runChange(command string) string {
	if m := executionsRun.FindStringSubmatch(unquoted(command)); m != nil {
		return m[1]
	}
	return ""
}

// chained is whether command, unquoted, chains or pipes segments.
func chained(command string) bool {
	c := unquoted(command)
	return strings.ContainsAny(c, ";|") || strings.Contains(c, "&&")
}

// runFailed is whether a run's result is a RED: a verdict line with a
// non-zero exit or a timeout; with none, an error of a single segment.
func runFailed(command string, r ToolResult) bool {
	text := r.Text + "\n" + r.Shown
	if vs := verdict.FindAllStringSubmatch(text, -1); vs != nil {
		for _, v := range vs {
			if v[1] != "0" {
				return true
			}
		}
		return false
	}
	return r.IsError && !chained(command)
}

// handRed is whether a hand-run test command's result shows a failing test.
func handRed(r ToolResult) bool {
	return r.IsError || testFailed.MatchString(r.Text+"\n"+r.Shown)
}

// recordOf is the change whose tasks.md file is, or "".
var recordOf = lazyre.New(`(?:^|/)(?:` + lddlayout.DirsPattern() + `)/changes/([\w.-]+)/tasks\.md$`)

// notCode are the extensions of documents and images: no behaviour.
var notCode = map[string]bool{".md": true, ".mdx": true, ".markdown": true, ".txt": true, ".rst": true, ".adoc": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true, ".webp": true, ".ico": true, ".pdf": true}

// behaviourFile is whether file is code that alters behaviour: not a test,
// a document, or the LDD record.
func behaviourFile(file string) bool {
	f := "/" + strings.TrimPrefix(file, "/")
	base := path.Base(f)
	switch {
	case strings.Contains(f, "/openspec/"), notCode[strings.ToLower(path.Ext(f))]:
		return false
	case strings.Contains(f, "/test/"), strings.Contains(f, "/tests/"), strings.Contains(f, "/__tests__/"), strings.Contains(f, "/testdata/"):
		return false
	case strings.HasPrefix(base, "test_"), strings.Contains(base, "_test."), strings.Contains(base, ".test."), strings.Contains(base, ".spec."):
		return false
	}
	return true
}

func checkEditBeforeRed(s *Session, emit emitFunc) {
	type edit struct {
		tool, file string
		line       int
	}
	for _, t := range s.transcripts() {
		var first *edit
		hand := 0                   // the line of a failing hand-run test before first, or 0
		runs := map[string]string{} // tool use id → change
		cmds := map[string]string{} // tool use id → its command
		tests := map[string]bool{}  // tool use id of a hand-run test
		red := map[string]bool{}
		open := "" // the change whose RED came and whose GREEN has not
		reported := map[string]bool{}
		report := func(change, why string, line, handLine int) {
			if reported[change] {
				return
			}
			reported[change] = true
			severity, message := "violation", fmt.Sprintf("%s %s changes code of %s before its %s (line %d): the RED test and the record the change needs come before the first behaviour-changing edit (contracts/ldd.md, contracts/tdd.md)",
				first.tool, first.file, change, why, line)
			if handLine > 0 {
				severity = "suspect"
				message += fmt.Sprintf("; the failing test ran by hand at line %d, not through `jhon executions run`", handLine)
			}
			emit(emitted{check: "edit-before-red", severity: severity, transcript: t.File, line: first.line, agent: t.Agent, agentType: t.AgentType,
				evidence: short(first.tool+" "+first.file, 120), message: message})
		}
		for _, e := range t.Entries {
			for _, r := range e.ToolResults {
				if tests[r.ID] && first == nil && hand == 0 && handRed(r) {
					hand = e.Line
				}
				change, ok := runs[r.ID]
				if !ok {
					continue
				}
				failed := runFailed(cmds[r.ID], r)
				if !failed && change == open {
					// The change's GREEN: its own edits since its RED were
					// its implementation, the next change's first edit is
					// read afresh.
					first, hand, open = nil, 0, ""
				}
				if red[change] || !failed {
					continue
				}
				red[change] = true
				if first != nil {
					report(change, "RED, the first failing `jhon executions run "+change+"`", e.Line, hand)
				}
				first, hand, open = nil, 0, change
			}
			for _, u := range e.ToolUses {
				if c, ok := strProp(u.Input, "command"); ok && (u.Name == "Bash" || u.Name == "bash") {
					if change := runChange(c); change != "" {
						runs[u.ID], cmds[u.ID] = change, c
					} else if testRunner.MatchString(unquoted(c)) {
						tests[u.ID] = true
					}
					continue
				}
				file, _ := strProp(u.Input, "file_path")
				if file == "" {
					file, _ = strProp(u.Input, "path")
				}
				if (u.Name != "Edit" && u.Name != "Write" && u.Name != "edit" && u.Name != "write") || file == "" {
					continue
				}
				if m := recordOf.FindStringSubmatch(file); m != nil && first != nil {
					report(m[1], "record openspec/changes/"+m[1]+"/tasks.md", e.Line, 0)
				}
				if first == nil && behaviourFile(file) {
					first = &edit{u.Name, file, e.Line}
				}
			}
		}
	}
}
