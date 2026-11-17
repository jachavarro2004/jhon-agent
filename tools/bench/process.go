package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strings"
)

// The process-quality metrics (T41) say how a run worked, which the hidden
// test misses. Each is nil in metrics.json when it is unknown: a run
// recorded before them, or a run whose transcript is missing.
//
//   - test_changed: a test file (the task's `tests` pattern) was added or
//     modified, by the run repo's changes since its start commit;
//   - tests_run_before_end: the transcript shows the project's tests run
//     (Bash with the task's test_command, or `jhon executions run`) after
//     the last Edit/Write; a run in the background (pi's bash_background,
//     Claude Code's Bash with run_in_background) counts once its job ends,
//     passing on exit code 0, at the place it was started;
//   - red_before_green: a test run failed before a later one passed, from
//     `jhon status --json` tests.red_before_green in an LDD run that has the
//     record, else from the transcript; red_before_green_source says which;
//   - out_of_scope_files: changed files outside the task's `scope`, tests
//     excluded (they are always allowed);
//   - rework (T46): edits (Edit, MultiEdit, Write, NotebookEdit) of a file
//     that was edited earlier in the same owner message and that a passing
//     test run (as tests_run_before_end reads one) has covered since: every
//     such edit counts, as the tests had passed for the file already. A new owner message starts over, so a requirement change that
//     edits the same file is not rework; Bash-made edits are not seen.

// changedFiles are the repository paths repo changed since start: committed,
// modified, deleted or new and not ignored; Python's byte code is left out.
func changedFiles(repo, start string, env []string) ([]string, error) {
	var files []string
	for _, args := range [][]string{{"diff", "--name-only", "-z", start}, {"ls-files", "--others", "--exclude-standard", "-z"}} {
		c := exec.Command("git", args...)
		c.Dir, c.Env = repo, env
		out, err := c.Output()
		if err != nil {
			return nil, fmt.Errorf("git %s: %v", strings.Join(args, " "), err)
		}
		for _, f := range strings.Split(string(out), "\x00") {
			if f != "" && !strings.HasSuffix(f, ".pyc") && !strings.Contains("/"+f, "/__pycache__/") {
				files = append(files, f)
			}
		}
	}
	slices.Sort(files)
	return slices.Compact(files), nil
}

// headCommit is repo's HEAD commit.
func headCommit(repo string, env []string) (string, error) {
	c := exec.Command("git", "rev-parse", "HEAD")
	c.Dir, c.Env = repo, env
	out, err := c.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// fromChanges fills the repository facts from the files a run changed. An
// LDD variant's record under openspec/ or ldd/ is what the variant asks
// for, never out of scope (L444, L483); so is the ldd/ a Jhon plain run sets
// up (L554-L559).
func (m *Metrics) fromChanges(t Task, variant, cond string, files []string) {
	m.ChangedFiles, m.OutOfScope = []string{}, []string{}
	changed := false
	for _, f := range files {
		m.ChangedFiles = append(m.ChangedFiles, f)
		switch {
		case t.IsTest(f):
			changed = true
		case isLDD(variant) && (strings.HasPrefix(f, "openspec/") || strings.HasPrefix(f, "ldd/")):
		case setsUpLDD(cond, variant) && strings.HasPrefix(f, "ldd/"):
		case !t.InScope(f):
			m.OutOfScope = append(m.OutOfScope, f)
		}
	}
	n := len(m.OutOfScope)
	m.TestChanged, m.OutOfScopeFiles = &changed, &n
}

// transcriptFacts are what a transcript says about the run's tests.
type transcriptFacts struct {
	TestsRunBeforeEnd bool
	RedBeforeGreen    bool
	TestRuns          int
	Rework            int
	Refusals          int
	// Ceremony are the jhon executions, archive and deliver commands run.
	Ceremony Ceremony
}

// toolCall is one tool call of a transcript, in order, with its result,
// or an owner message (owner true, no name).
type toolCall struct {
	name, command, file string
	failed, refused     bool
	owner               bool
	// background is a run started in the background: job is its job id,
	// ended whether the transcript reported the job's end.
	background, ended bool
	job               string
}

// testFailed reads a failure from a test run's output when the exit code
// did not reach the transcript: unittest's summary or a failing status.
var testFailed = regexp.MustCompile(`FAILED \(|"status":\s*"fail`)

// The end of a background job: pi's jhon-tasks notice, and Claude Code's
// task notification (its documented shape: no recorded run has one yet).
var (
	piJobEnd       = regexp.MustCompile(`^Background job (\S+) .*?exited with code (\d+)`)
	claudeJobStart = regexp.MustCompile(`running in background with ID: (\S+)`)
	claudeJobEnd   = regexp.MustCompile(`(?s)^\s*<task-notification>.*<task-id>([^<]+)</task-id>.*<status>([^<]+)</status>`)
	claudeJobExit  = regexp.MustCompile(`exit code (\d+)`)
)

// toolCalls are the tool calls of a Claude Code transcript or a pi session,
// each once: a tool_use whose id came before is a replay.
func toolCalls(b []byte) []toolCall {
	var calls []*toolCall
	byID := map[string]*toolCall{}
	result := func(id string, isErr bool, text string) {
		if c := byID[id]; c != nil {
			c.failed = isErr || testFailed.MatchString(text)
			c.refused = isErr && refusalText.MatchString(text)
			if m := claudeJobStart.FindStringSubmatch(text); c.background && c.job == "" && m != nil {
				c.job = m[1]
			}
		}
	}
	// jobEnd ends the background run of job, failing unless exit code 0.
	jobEnd := func(job string, failed bool, text string) {
		for _, c := range calls {
			if c.background && c.job == job && job != "" {
				c.ended, c.failed = true, failed || testFailed.MatchString(text)
			}
		}
	}
	for _, r := range records(b) {
		// Claude Code (2.1.310) writes api-request-blob records that replay
		// earlier records (T47); they are no new call, result nor message.
		if str(r["type"]) == "api-request-blob" {
			continue
		}
		msg := at(r, "message")
		if str(at(msg, "role")) == "toolResult" {
			result(str(at(msg, "toolCallId")), at(msg, "isError") == true, contentText(at(msg, "content")))
			if c := byID[str(at(msg, "toolCallId"))]; c != nil && c.background {
				c.job = cmpOr(c.job, str(at(msg, "details", "jhonTasks", "id")))
			}
			continue
		}
		if str(at(msg, "customType")) == "jhon-tasks.notice" {
			text := contentText(at(msg, "content"))
			if m := piJobEnd.FindStringSubmatch(text); m != nil {
				jobEnd(m[1], m[2] != "0", text)
			}
			continue
		}
		if text, ok := at(msg, "content").(string); ok && str(at(msg, "role")) == "user" {
			if m := claudeJobEnd.FindStringSubmatch(text); m != nil {
				failed := m[2] != "completed"
				if e := claudeJobExit.FindStringSubmatch(text); e != nil {
					failed = e[1] != "0"
				}
				jobEnd(m[1], failed, text)
				continue
			}
		}
		if ownerMessage(r) {
			calls = append(calls, &toolCall{owner: true})
			continue
		}
		for _, p := range list(at(msg, "content")) {
			switch str(at(p, "type")) {
			case "tool_use", "toolCall":
				id := str(at(p, "id"))
				if _, seen := byID[id]; seen && id != "" {
					continue
				}
				input := at(p, "input")
				if input == nil {
					input = at(p, "arguments")
				}
				c := &toolCall{name: strings.ToLower(str(at(p, "name"))), command: str(at(input, "command")),
					file: cmpOr(str(at(input, "file_path")), cmpOr(str(at(input, "path")), str(at(input, "notebook_path"))))}
				if c.name == "bash_background" {
					c.name, c.background = "bash", true
				}
				c.background = c.background || at(input, "run_in_background") == true
				calls, byID[id] = append(calls, c), c
			case "tool_result":
				result(str(at(p, "tool_use_id")), at(p, "is_error") == true, contentText(at(p, "content")))
			}
		}
	}
	out := make([]toolCall, len(calls))
	for i, c := range calls {
		out[i] = *c
	}
	return out
}

// ownerMessage is whether r is a message the owner typed: a user message
// whose content is text (Claude Code: a string or text parts; pi: role
// user), not a tool result nor Claude Code's meta records.
func ownerMessage(r map[string]any) bool {
	msg := at(r, "message")
	if str(at(msg, "role")) != "user" || at(r, "isMeta") == true {
		return false
	}
	if _, ok := at(msg, "content").(string); ok {
		return true
	}
	parts := list(at(msg, "content"))
	return len(parts) > 0 && !slices.ContainsFunc(parts, func(p any) bool { return str(at(p, "type")) != "text" })
}

// contentText is a tool result's text: a string, or its text parts joined.
func contentText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	var parts []string
	for _, p := range list(v) {
		parts = append(parts, str(at(p, "text")))
	}
	return strings.Join(parts, "\n")
}

// editTools are the tools that edit a file, lowercased (Claude Code and pi).
var editTools = []string{"edit", "multiedit", "write", "notebookedit"}

// transcriptProcess reads the transcript b: whether the tests (testCommand,
// or `jhon executions run`, through Bash) ran after the last edit, and
// whether a failing test run came before a later passing one.
func transcriptProcess(b []byte, testCommand string) transcriptFacts {
	var f transcriptFacts
	red := false
	// edited are the files edited in this owner message: true once a
	// passing test run covered the edit.
	edited := map[string]bool{}
	for _, c := range toolCalls(b) {
		if c.owner {
			clear(edited)
			continue
		}
		if c.refused {
			f.Refusals++
		}
		if c.name == "bash" {
			for _, k := range ceremonyCommand.FindAllStringSubmatch(c.command, -1) {
				switch k[1] {
				case "executions":
					f.Ceremony.Executions++
				case "archive":
					f.Ceremony.Archive++
				case "deliver":
					f.Ceremony.Deliver++
				}
			}
		}
		if slices.Contains(editTools, c.name) {
			f.TestsRunBeforeEnd = false
			if green, ok := edited[c.file]; green {
				f.Rework++
			} else if !ok {
				edited[c.file] = false
			}
			continue
		}
		if c.name != "bash" || !(strings.Contains(c.command, testCommand) || strings.Contains(c.command, "jhon executions run")) {
			continue
		}
		// A background run whose job never reported its end is no run.
		if c.background && !c.ended {
			continue
		}
		f.TestRuns++
		f.TestsRunBeforeEnd = true
		if c.failed {
			red = true
			continue
		}
		if red {
			f.RedBeforeGreen = true
		}
		for file := range edited {
			edited[file] = true
		}
	}
	return f
}

// lddRedBeforeGreen is tests.red_before_green of `jhon status --json`, nil
// when the change has no test record.
func lddRedBeforeGreen(status []byte) *bool {
	var s map[string]any
	if json.Unmarshal(bytes.TrimSpace(status), &s) != nil {
		return nil
	}
	if v, ok := at(s, "tests", "red_before_green").(bool); ok {
		return &v
	}
	return nil
}

// fromTranscript fills the transcript facts, keeping an LDD red_before_green
// and the stream's refusals already read.
func (m *Metrics) fromTranscript(b []byte, testCommand string) {
	f := transcriptProcess(b, testCommand)
	m.TestsRunBeforeEnd, m.Rework = &f.TestsRunBeforeEnd, &f.Rework
	// The stream's count, when read, stands (streamRefusals).
	if m.Refusals == nil {
		m.Refusals = &f.Refusals
	}
	if m.Ceremony != nil {
		m.Ceremony.add(f.Ceremony)
	}
	if m.RedBeforeGreen == nil {
		m.RedBeforeGreen, m.RedBeforeGreenSource = &f.RedBeforeGreen, "transcript"
	}
}
