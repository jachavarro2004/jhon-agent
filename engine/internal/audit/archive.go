package audit

import (
	"fmt"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
)

// archive-without-red (ldd/red-first, archive form, T56): `jhon archive
// <change>` (`jhon archive` itself refuses nothing for want of a RED,
// internal/ldd/archive.go) run in a session whose RED of that change, the
// first failing `jhon executions run <change>`, has no GREEN after it, a
// later run that passes, before the archive: once per change. A session
// with no RED of the change before its archive is indeterminate: its RED and
// GREEN may be another session's; so is one with a run after the RED whose
// verdict the transcript does not show. A run is read by its verdict lines
// (runFailed), or passes reused (its `(reused: it passed` line); one with
// neither, its output piped or cut, is unknown, unless it is an error of a
// single segment (a RED). The runs and the archive are read
// across the session's transcripts, in time order; an archive whose result
// is an error archived nothing.

// reusedPass is the line of a run that passed on the same tree before.
const reusedPass = "(reused: it passed"

var archiveRun = lazyre.New(`(?:^|&&|\|\||[;|\n(])\s*(?:\w+=\S*\s+)*jhon\s+archive\s+([\w.-]+)`)

func checkArchiveWithoutRed(s *Session, emit emitFunc) {
	type run struct {
		at              string
		failed, unknown bool
	}
	type archive struct {
		t      *Transcript
		line   int
		at     string
		change string
	}
	runs := map[string][]run{}
	var archives []archive
	for _, t := range s.transcripts() {
		cmds := map[string]string{}
		for _, e := range t.Entries {
			for _, r := range e.ToolResults {
				c, ok := cmds[r.ID]
				if !ok {
					continue
				}
				if change := runChange(c); change != "" {
					out := r.Text + "\n" + r.Shown
					failed := runFailed(c, r)
					known := failed || verdict.MatchString(out) || strings.Contains(out, reusedPass)
					runs[change] = append(runs[change], run{e.Timestamp, failed, !known})
				} else if m := archiveRun.FindStringSubmatch(unquoted(c)); m != nil && !r.IsError {
					archives = append(archives, archive{t, e.Line, e.Timestamp, m[1]})
				}
			}
			for _, u := range e.ToolUses {
				if c, ok := strProp(u.Input, "command"); ok && (u.Name == "Bash" || u.Name == "bash") {
					cmds[u.ID] = c
				}
			}
		}
	}
	reported := map[string]bool{}
	for _, a := range archives {
		if reported[a.change] {
			continue
		}
		reported[a.change] = true
		red, green, unknown := false, false, false
		for _, r := range runs[a.change] {
			switch {
			case r.at > a.at:
			case r.failed:
				red = true
			case red && r.unknown:
				unknown = true
			case red:
				green = true
			}
		}
		switch {
		case red && !green && unknown:
			emit(emitted{check: "archive-without-red", severity: "suspect", kind: "indeterminate", transcript: a.t.File, line: a.line, agent: a.t.Agent, agentType: a.t.AgentType,
				evidence: "jhon archive " + a.change,
				message:  fmt.Sprintf("jhon archive %s after its RED, and a later `jhon executions run %s` whose verdict the transcript does not show: its GREEN is unknown", a.change, a.change)})
		case !red:
			emit(emitted{check: "archive-without-red", severity: "suspect", kind: "indeterminate", transcript: a.t.File, line: a.line, agent: a.t.Agent, agentType: a.t.AgentType,
				evidence: "jhon archive " + a.change,
				message:  fmt.Sprintf("jhon archive %s with no RED of it in this session, no failing `jhon executions run %s` before: its RED and GREEN, if any, are another session's", a.change, a.change)})
		case !green:
			emit(emitted{check: "archive-without-red", severity: "violation", transcript: a.t.File, line: a.line, agent: a.t.Agent, agentType: a.t.AgentType,
				evidence: "jhon archive " + a.change,
				message:  fmt.Sprintf("jhon archive %s with no GREEN: no run of it passed after its RED and before the archive; a change is archived after its RED and then its GREEN (contracts/ldd.md)", a.change)})
		}
	}
}
