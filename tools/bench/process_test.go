package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// step is one tool call of a scripted transcript and its result.
type step struct {
	tool, command string
	failed        bool
	output        string
	file          string // the edited file; x.py when empty
	owner         bool   // an owner message, not a tool call
}

// fileOf is s's file, x.py by default.
func (s step) fileOf() string { return cmpOr(s.file, "x.py") }

// claudeTranscript is steps as Claude Code's transcript records them.
func claudeTranscript(steps ...step) []byte {
	var b bytes.Buffer
	for i, s := range steps {
		if s.owner {
			line, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": s.output}})
			b.Write(append(line, '\n'))
			continue
		}
		id := "toolu_" + string(rune('a'+i))
		use := map[string]any{"type": "tool_use", "id": id, "name": s.tool, "input": map[string]any{"command": s.command, "file_path": s.fileOf()}}
		res := map[string]any{"type": "tool_result", "tool_use_id": id, "is_error": s.failed, "content": s.output}
		for _, r := range []map[string]any{
			{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{use}}},
			{"type": "user", "message": map[string]any{"role": "user", "content": []any{res}}},
		} {
			line, _ := json.Marshal(r)
			b.Write(append(line, '\n'))
		}
	}
	return b.Bytes()
}

// piTranscript is steps as pi's session file records them.
func piTranscript(steps ...step) []byte {
	var b bytes.Buffer
	b.WriteString(`{"type":"session","version":3,"id":"s"}` + "\n")
	for i, s := range steps {
		if s.owner {
			line, _ := json.Marshal(map[string]any{"type": "message", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": s.output}}}})
			b.Write(append(line, '\n'))
			continue
		}
		id := "call_" + string(rune('a'+i))
		call := map[string]any{"type": "toolCall", "id": id, "name": strings.ToLower(s.tool), "arguments": map[string]any{"command": s.command, "path": s.fileOf()}}
		for _, r := range []map[string]any{
			{"type": "message", "message": map[string]any{"role": "assistant", "content": []any{call}}},
			{"type": "message", "message": map[string]any{"role": "toolResult", "toolCallId": id, "toolName": strings.ToLower(s.tool), "isError": s.failed,
				"content": []any{map[string]any{"type": "text", "text": s.output}}}},
		} {
			line, _ := json.Marshal(r)
			b.Write(append(line, '\n'))
		}
	}
	return b.Bytes()
}

var (
	edit       = step{tool: "Edit"}
	write      = step{tool: "Write"}
	testFails  = step{tool: "Bash", command: "python3 -m unittest -q", failed: true, output: "FAILED (failures=1)"}
	testPasses = step{tool: "Bash", command: "cd repo && python3 -m unittest test_x", output: "Ran 3 tests\n\nOK"}
	jhonRun    = step{tool: "Bash", command: "jhon executions run --json", output: `{"red_before_green": false}`}
	jhonFails  = step{tool: "Bash", command: "jhon executions run", failed: true, output: "1 failed"}
	other      = step{tool: "Bash", command: "ls -la", output: "x.py"}
	// failedText is a run whose exit code the transcript lost, only its text.
	failedText = step{tool: "Bash", command: "python3 -m unittest", output: "Ran 2 tests\n\nFAILED (errors=1)"}
)

func TestTheTranscriptSaysWhetherTestsRanAfterTheLastEditAndRedBeforeGreen(t *testing.T) {
	cases := []struct {
		name         string
		steps        []step
		after, red   bool
		testRuns     int
		lastIsPassed bool
	}{
		{"red, edit, green", []step{edit, testFails, write, testPasses}, true, true, 2, true},
		{"edit after the last test", []step{testFails, edit, testPasses, edit}, false, true, 2, true},
		{"no test at all", []step{edit, other}, false, false, 0, false},
		{"green only", []step{edit, testPasses, testPasses}, true, false, 2, true},
		{"green then red", []step{edit, testPasses, testFails}, true, false, 2, false},
		{"jhon executions run counts", []step{edit, jhonFails, edit, jhonRun}, true, true, 2, true},
		{"a failure read from the text", []step{edit, failedText, edit, testPasses}, true, true, 2, true},
		{"tests and no edit", []step{testPasses}, true, false, 1, true},
	}
	for _, c := range cases {
		for name, transcript := range map[string][]byte{"claude": claudeTranscript(c.steps...), "pi": piTranscript(c.steps...)} {
			p := transcriptProcess(transcript, "python3 -m unittest")
			if p.TestsRunBeforeEnd != c.after || p.RedBeforeGreen != c.red || p.TestRuns != c.testRuns {
				t.Errorf("%s (%s): %+v, want after %t red %t runs %d", c.name, name, p, c.after, c.red, c.testRuns)
			}
		}
	}
}

// The LDD record an ldd run writes under openspec/ is what the variant asks
// for, never out of scope; a plain run's openspec/ files still are (L444).
func TestTheLDDRecordIsInScopeForTheLDDVariant(t *testing.T) {
	task := Task{Scope: []string{"pager.py"}, Tests: "test_*.py"}
	files := []string{"openspec/changes/fix-pager/tasks.md", "openspec/changes/fix-pager/execution-record.json", "pager.py", "notes.md"}
	m := &Metrics{}
	m.fromChanges(task, "ldd", "jhon", files)
	if !slices.Equal(m.OutOfScope, []string{"notes.md"}) {
		t.Errorf("ldd out of scope %q", m.OutOfScope)
	}
	for _, cond := range []string{"clean", "pi-clean"} {
		m = &Metrics{}
		m.fromChanges(task, "plain", cond, append(slices.Clone(files), "ldd/config.yaml"))
		if !slices.Equal(m.OutOfScope, []string{"openspec/changes/fix-pager/tasks.md", "openspec/changes/fix-pager/execution-record.json", "notes.md", "ldd/config.yaml"}) {
			t.Errorf("plain %s out of scope %q", cond, m.OutOfScope)
		}
	}
}

// Jhon always works under LDD (T72b, L554-L559): what a Jhon plain run's
// set-up writes under ldd/ is not out of scope; anything else still is.
func TestTheLDDSetUpIsInScopeForAJhonPlainRun(t *testing.T) {
	task := Task{Scope: []string{"pager.py"}, Tests: "test_*.py"}
	files := []string{"ldd/config.yaml", "ldd/changes/fix-pager/tasks.md", "pager.py", "README.md", ".gitignore", ".atl/skill-registry.md", "openspec/config.yaml"}
	for _, cond := range []string{"jhon", "pi-jhon"} {
		m := &Metrics{}
		m.fromChanges(task, "plain", cond, files)
		if !slices.Equal(m.OutOfScope, []string{"README.md", ".gitignore", ".atl/skill-registry.md", "openspec/config.yaml"}) || *m.OutOfScopeFiles != 4 {
			t.Errorf("plain %s out of scope %q", cond, m.OutOfScope)
		}
	}
}

func TestChangedFilesAreTestsOrInOrOutOfScope(t *testing.T) {
	task := Task{Scope: []string{"pager.py", "data/*"}, Tests: "test_*.py"}
	m := &Metrics{}
	m.fromChanges(task, "plain", "jhon", []string{"data/contacts.json", "notes.md", "pager.py", "sub/test_new.py", "tools/fix.py"})
	if m.TestChanged == nil || !*m.TestChanged || m.OutOfScopeFiles == nil || *m.OutOfScopeFiles != 2 ||
		!slices.Equal(m.OutOfScope, []string{"notes.md", "tools/fix.py"}) || len(m.ChangedFiles) != 5 {
		t.Fatalf("%+v", m)
	}
	m = &Metrics{}
	m.fromChanges(task, "plain", "jhon", nil)
	if m.TestChanged == nil || *m.TestChanged || m.OutOfScopeFiles == nil || *m.OutOfScopeFiles != 0 || m.ChangedFiles == nil {
		t.Fatalf("no change: %+v", m)
	}
}

// solution is a scratch directory holding files, to stand in for an agent's edits.
func solution(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestAFakeRunRecordsItsProcessFromTheRepoAndTheTranscript(t *testing.T) {
	f := newFake(t, "0.40")
	ref, err := os.ReadFile(filepath.Join("testdata", "solutions", "pagination-bug", "pager.py"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_SOLUTION", solution(t, map[string]string{"pager.py": string(ref), "test_more.py": "import unittest\n", "NOTES.md": "x\n",
		"__pycache__/pager.cpython-313.pyc": "x"}))
	transcript := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(transcript, claudeTranscript(testFails, edit, testPasses), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TRANSCRIPT", transcript)
	o, out := options(t, f, "pagination-bug", []string{"clean"}, 1)
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	m := metricsOf(t, o, "pagination-bug-plain-clean-1")
	if m.TestChanged == nil || !*m.TestChanged || m.OutOfScopeFiles == nil || *m.OutOfScopeFiles != 1 || !slices.Equal(m.OutOfScope, []string{"NOTES.md"}) ||
		!slices.Equal(m.ChangedFiles, []string{"NOTES.md", "pager.py", "test_more.py"}) {
		t.Errorf("repo facts %+v", m)
	}
	if m.TestsRunBeforeEnd == nil || !*m.TestsRunBeforeEnd || m.RedBeforeGreen == nil || !*m.RedBeforeGreen || m.RedBeforeGreenSource != "transcript" {
		t.Errorf("transcript facts %+v", m)
	}
	if !m.Success {
		t.Errorf("the hidden test failed: %+v", m)
	}
}

func TestAnLDDRunTakesRedBeforeGreenFromJhonStatus(t *testing.T) {
	f := newFake(t, "0.40")
	transcript := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(transcript, claudeTranscript(edit, jhonRun), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TRANSCRIPT", transcript)
	if err := os.WriteFile(filepath.Join(f.dir, "status.json"), []byte(`{"ok":true,"tests":{"passed":true,"red_before_green":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	o, out := options(t, f, "pagination-bug", []string{"jhon", "clean"}, 1)
	o.Variants = []string{"plain", "ldd"}
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	ldd := metricsOf(t, o, "pagination-bug-ldd-jhon-1")
	if ldd.RedBeforeGreen == nil || !*ldd.RedBeforeGreen || ldd.RedBeforeGreenSource != "ldd" || ldd.TestsRunBeforeEnd == nil || !*ldd.TestsRunBeforeEnd {
		t.Errorf("ldd %+v", ldd)
	}
	// Jhon always works under LDD (T72b, L554-L559): a Jhon plain run reads
	// jhon status too; the clean one does not.
	if m := metricsOf(t, o, "pagination-bug-plain-jhon-1"); m.RedBeforeGreen == nil || !*m.RedBeforeGreen || m.RedBeforeGreenSource != "ldd" {
		t.Errorf("plain jhon %+v", m)
	}
	if m := metricsOf(t, o, "pagination-bug-plain-clean-1"); m.RedBeforeGreen == nil || *m.RedBeforeGreen || m.RedBeforeGreenSource != "transcript" || m.TestChanged == nil || *m.TestChanged {
		t.Errorf("plain clean %+v", m)
	}
	n := 0
	for _, c := range f.calls(t) {
		if strings.HasPrefix(c, "jhon status --json") {
			n++
		}
	}
	if n != 2 {
		t.Errorf("jhon status ran %d times, want twice (the ldd and the Jhon plain run)", n)
	}
}

func TestAnLDDRunWithNoTestRecordFallsBackToTheTranscript(t *testing.T) {
	f := newFake(t, "0.40")
	transcript := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(transcript, claudeTranscript(edit, jhonFails, edit, jhonRun), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TRANSCRIPT", transcript)
	o, out := options(t, f, "pagination-bug", []string{"jhon"}, 1)
	o.Variants = []string{"ldd"}
	if err := Execute(o); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	m := metricsOf(t, o, "pagination-bug-ldd-jhon-1")
	if m.RedBeforeGreen == nil || !*m.RedBeforeGreen || m.RedBeforeGreenSource != "transcript" {
		t.Errorf("%+v", m)
	}
}

func TestAMissingTranscriptLeavesTheTranscriptFactsUnknown(t *testing.T) {
	m := &Metrics{}
	b, _ := json.Marshal(m)
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"test_changed", "tests_run_before_end", "red_before_green", "out_of_scope_files"} {
		if v, ok := back[k]; !ok || v != nil {
			t.Errorf("%s: %v (present %t), want null", k, v, ok)
		}
	}
}

// A test run in the background counts when its job ends, its pass or fail
// read from the job's exit code (L550): pi's bash_background, distilled from
// a real run, and Claude Code's Bash with run_in_background.
func TestABackgroundTestRunCountsWhenItsJobEnds(t *testing.T) {
	pi, err := os.ReadFile(filepath.Join("testdata", "pi-background-tests.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	piFails := strings.NewReplacer(`exited with code 0`, `exited with code 1`, `"succeeded":true,"exitCode":0`, `"succeeded":false,"exitCode":1`).Replace(string(pi))
	var piNoEnd []string
	for _, l := range strings.Split(string(pi), "\n") {
		if !strings.Contains(l, "jhon-tasks.notice") {
			piNoEnd = append(piNoEnd, l)
		}
	}
	claude := func(status, summary string) []byte {
		var b bytes.Buffer
		for _, r := range []map[string]any{
			{"type": "user", "message": map[string]any{"role": "user", "content": "Fix the ledger."}},
			{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": "toolu_red", "name": "Bash", "input": map[string]any{"command": "python3 -m unittest"}}}}},
			{"type": "user", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_red", "is_error": true, "content": "Ran 4 tests\n\nFAILED (failures=2)"}}}},
			{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": "toolu_edit", "name": "Edit", "input": map[string]any{"file_path": "ledger.py"}}}}},
			{"type": "user", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_edit", "content": "ok"}}}},
			{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": "toolu_bg", "name": "Bash", "input": map[string]any{"command": "python3 -m unittest; python3 run_suite.py", "run_in_background": true}}}}},
			{"type": "user", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_bg", "content": "Command running in background with ID: bash_1"}}}},
		} {
			line, _ := json.Marshal(r)
			b.Write(append(line, '\n'))
		}
		if status != "" {
			line, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": "<task-notification>\n<task-id>bash_1</task-id>\n<status>" + status + "</status>\n<summary>" + summary + "</summary>\n</task-notification>"}})
			b.Write(append(line, '\n'))
		}
		return b.Bytes()
	}
	cases := []struct {
		name       string
		transcript []byte
		after, red bool
		testRuns   int
	}{
		{"pi: red, edit, background green", pi, true, true, 2},
		{"pi: the background job fails", []byte(piFails), true, false, 2},
		{"pi: the background job never ends", []byte(strings.Join(piNoEnd, "\n")), false, false, 1},
		{"claude: red, edit, background green", claude("completed", `Background command "tests" completed (exit code 0)`), true, true, 2},
		{"claude: the background job fails", claude("failed", `Background command "tests" failed with exit code 1`), true, false, 2},
		{"claude: the background job never ends", claude("", ""), false, false, 1},
	}
	for _, c := range cases {
		p := transcriptProcess(c.transcript, "python3 -m unittest")
		if p.TestsRunBeforeEnd != c.after || p.RedBeforeGreen != c.red || p.TestRuns != c.testRuns {
			t.Errorf("%s: %+v, want after %t red %t runs %d", c.name, p, c.after, c.red, c.testRuns)
		}
	}
}
