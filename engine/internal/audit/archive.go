package audit

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
)

// archive-without-red (ldd/red-first, archive form, T56): `jhon archive
// <change>` (since T223 `jhon archive` itself refuses a change with no
// RED before GREEN on record, internal/ldd/archive.go; the check still
// reads sessions of older releases and the session's own runs) run in a
// session whose RED of that change, the
// first failing `jhon executions run <change>`, has no GREEN after it, a
// later run that passes, before the archive: once per change. A session
// with no RED of the change before its archive is indeterminate: its RED and
// GREEN may be another session's; so is one with a run after the RED whose
// verdict the transcript does not show. A run is read by its verdict lines
// (runFailed), or passes reused (its `(reused: it passed` line); one with
// neither, its output piped or cut, is unknown, unless it is an error of a
// single segment (a RED). A run's summary line, `jhon executions:
// <pass|fail>, exit N (...)`, is its verdict when the transcript shows it
// (T223): a builder that pipes its run through tail or grep shows that line
// and few or none of the projects' lines, so a summary that says pass is a
// GREEN and one that says fail is a RED, whatever project lines are left;
// a pass that tested nothing (`0 of 0 passed`, an affected run that
// selected no project) is no run at all, neither RED nor GREEN.
// The runs and the archives are read across the session's transcripts, the
// main thread's and every sub-agent's, then put in time order by their
// timestamps (T223: a RED in a builder and the GREEN in the main thread
// after it is RED then GREEN); a run after the archive does not count. An
// archive whose result is an error archived nothing, and `jhon archive
// --help` (or -h) is no archive.

// reusedPass is the line of a run that passed on the same tree before.
const reusedPass = "(reused: it passed"

// archiveRun is a `jhon archive <change>` segment; a change name starts
// with a letter or a digit (internal/ldd/archive.go), so a flag is none.
var archiveRun = lazyre.New(`(?:^|&&|\|\||[;|\n(])\s*(?:\w+=\S*\s+)*jhon\s+archive\s+([A-Za-z0-9][\w.-]*)`)

// runSummary is a run's last line, `jhon executions: <pass|fail>, exit N
// (<n> of <m> passed` (cli/cmd_executions.go): the whole run's verdict.
var runSummary = lazyre.New(`(?m)^jhon executions: (pass|fail), exit -?\d+ \(\d+ of (\d+) passed`)

// summaryVerdict is whether out shows a run's summary line, whether the
// last one it shows says fail, and whether that run tested nothing (0 of 0).
func summaryVerdict(out string) (shown, failed, empty bool) {
	ms := runSummary.FindAllStringSubmatch(out, -1)
	if ms == nil {
		return false, false, false
	}
	last := ms[len(ms)-1]
	return true, last[1] == "fail", last[2] == "0"
}

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
					if shown, summaryFailed, empty := summaryVerdict(out); shown {
						if empty {
							continue
						}
						failed, known = summaryFailed, true
					}
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
	for _, rs := range runs {
		slices.SortStableFunc(rs, func(x, y run) int { return strings.Compare(x.at, y.at) })
	}
	slices.SortStableFunc(archives, func(x, y archive) int { return strings.Compare(x.at, y.at) })
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
