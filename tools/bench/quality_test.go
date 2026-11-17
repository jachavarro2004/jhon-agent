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

func TestReworkIsAnEditOfAFileItsTestsPassedForInTheSameMessage(t *testing.T) {
	editA, editB := step{tool: "Edit", file: "a.py"}, step{tool: "Write", file: "b.py"}
	owner := step{owner: true, output: "next message"}
	cases := []struct {
		name  string
		steps []step
		want  int
	}{
		{"edit, green, edit again", []step{editA, testPasses, editA}, 1},
		{"twice after green", []step{editA, testPasses, editA, step{tool: "MultiEdit", file: "a.py"}}, 2},
		{"edit, red, edit", []step{editA, testFails, editA, testPasses}, 0},
		{"another file after green", []step{editA, testPasses, editB}, 0},
		{"green again resets nothing", []step{editA, testPasses, editA, testPasses, editA}, 2},
		{"a new owner message starts over", []step{editA, testPasses, owner, editA}, 0},
		{"no tests", []step{editA, editA}, 0},
	}
	for _, c := range cases {
		for name, transcript := range map[string][]byte{"claude": claudeTranscript(c.steps...), "pi": piTranscript(c.steps...)} {
			if got := transcriptProcess(transcript, "python3 -m unittest").Rework; got != c.want {
				t.Errorf("%s (%s): rework %d, want %d", c.name, name, got, c.want)
			}
		}
	}
}

func TestRegressionsAreChecksThatFailAfterPassing(t *testing.T) {
	checks := []MessageCheck{{After: 1, Check: 1, Passed: true}, {After: 2, Check: 1, Passed: false}, {After: 2, Check: 2, Passed: false},
		{After: 3, Check: 1, Passed: true}, {After: 3, Check: 2, Passed: true}, {After: 3, Check: 3, Passed: true}}
	if got := regressions(checks); got != 1 {
		t.Errorf("regressions %d, want 1 (check 1 after message 2; check 2 never passed before)", got)
	}
	if got := finalChecks(checks); got != (Rate{Yes: 3, Known: 3}) {
		t.Errorf("final %+v", got)
	}
}

func TestPerMessageMetricsComeFromTheStreamBetweenBoundaries(t *testing.T) {
	claude := strings.Join([]string{
		`{"type":"bench_message","n":1,"phase":"start","at_ms":0}`,
		`{"type":"result","num_turns":4,"usage":{"input_tokens":10,"output_tokens":100,"cache_read_input_tokens":1000,"cache_creation_input_tokens":500}}`,
		`{"type":"bench_message","n":1,"phase":"end","at_ms":1500,"reason":"settled"}`,
		`{"type":"bench_message","n":2,"phase":"start","at_ms":1700}`,
		`{"type":"result","num_turns":2,"usage":{"input_tokens":3,"output_tokens":30,"cache_read_input_tokens":2000,"cache_creation_input_tokens":0}}`,
		`{"type":"bench_message","n":2,"phase":"end","at_ms":2700,"reason":"settled"}`}, "\n")
	got := messageMetrics([]byte(claude), 0)
	want := []MessageMetrics{{N: 1, InputTokens: 10, OutputTokens: 100, CacheReadTokens: 1000, CacheWriteTokens: 500, Turns: 4, WallMs: 1500, End: "settled"},
		{N: 2, InputTokens: 3, OutputTokens: 30, CacheReadTokens: 2000, Turns: 2, WallMs: 1000, End: "settled"}}
	if !slices.Equal(got, want) {
		t.Errorf("claude\n%+v\nwant\n%+v", got, want)
	}
	pi := strings.Join([]string{
		`{"type":"bench_message","n":1,"phase":"start","at_ms":5}`,
		`{"type":"turn_start"}`,
		`{"type":"message_end","message":{"role":"assistant","usage":{"input":10,"output":100,"cacheRead":1000,"cacheWrite":50}}}`,
		`{"type":"message_end","message":{"role":"user","usage":{"input":99}}}`,
		`{"type":"turn_start"}`,
		`{"type":"message_end","message":{"role":"assistant","usage":{"input":1,"output":1,"cacheRead":1,"cacheWrite":1}}}`,
		`{"type":"agent_settled"}`,
		`{"type":"bench_message","n":1,"phase":"end","at_ms":905,"reason":"settled"}`}, "\n")
	if got := messageMetrics([]byte(pi), 0); !slices.Equal(got, []MessageMetrics{{N: 1, InputTokens: 11, OutputTokens: 101, CacheReadTokens: 1001, CacheWriteTokens: 51, Turns: 2, WallMs: 900, End: "settled"}}) {
		t.Errorf("pi %+v", got)
	}
	// A single-message run is one message over the whole stream.
	single := `{"type":"result","num_turns":7,"usage":{"output_tokens":900}}`
	if got := messageMetrics([]byte(single), 4200); !slices.Equal(got, []MessageMetrics{{N: 1, OutputTokens: 900, Turns: 7, WallMs: 4200}}) {
		t.Errorf("single %+v", got)
	}
}

func TestAMultiMessageRunChecksEachMessageOnACopyAndCountsRegressions(t *testing.T) {
	for _, cond := range []string{"clean", "pi-jhon"} {
		t.Run(cond, func(t *testing.T) {
			f := newFake(t)
			t.Setenv("FAKE_SOLUTIONS", solution(t, map[string]string{"1/state.txt": "one\n", "2/state.txt": "broken\n", "2/two.txt": "two\n"}))
			o := multiOptions(t, f, "check_1: grep -q one state.txt\ncheck_2: test -f two.txt && test -f test_acceptance.py\n", cond)
			if err := Execute(o); err != nil {
				t.Fatal(err)
			}
			name := "pagination-bug-plain-" + cond + "-1"
			m := metricsOf(t, o, name)
			want := []MessageCheck{{After: 1, Check: 1, Passed: true}, {After: 2, Check: 1, Passed: false}, {After: 2, Check: 2, Passed: true}}
			if !slices.Equal(m.ChecksPerMessage, want) || m.Regressions == nil || *m.Regressions != 1 {
				t.Errorf("checks %+v regressions %v", m.ChecksPerMessage, m.Regressions)
			}
			if slices.Contains(m.ChangedFiles, "test_acceptance.py") || slices.ContainsFunc(m.ChangedFiles, func(f string) bool { return strings.HasPrefix(f, "checks") }) {
				t.Errorf("a check left files in the agent's repo: %q", m.ChangedFiles)
			}
			if len(m.Messages) != 2 || m.Messages[0].N != 1 || m.Messages[1].Turns == 0 || m.Messages[1].End != "settled" {
				t.Errorf("messages %+v", m.Messages)
			}
			if b, err := os.ReadFile(filepath.Join(o.Results, "runs", name, "checks.txt")); err != nil || !strings.Contains(string(b), "after message 2, check 1: FAIL") {
				t.Errorf("checks.txt %q %v", b, err)
			}
		})
	}
}

func TestTheReportShowsResultsThenFidelityThenCost(t *testing.T) {
	dir := t.TempDir()
	checks := []MessageCheck{{After: 1, Check: 1, Passed: true}, {After: 2, Check: 1, Passed: false}, {After: 2, Check: 2, Passed: true}}
	writeMetrics(t, dir,
		Metrics{Task: "invoice-slices", Condition: "jhon", Repeat: 1, Success: true, Turns: 9, ChecksPerMessage: checks, Regressions: ip(1), Rework: ip(2), RedBeforeGreen: bp(true),
			Messages: []MessageMetrics{{N: 1, Turns: 5, CacheWriteTokens: 900, WallMs: 1000}, {N: 2, Turns: 4, CacheWriteTokens: 100, WallMs: 3000}}},
		Metrics{Task: "invoice-slices", Condition: "pi-jhon", Repeat: 1, Success: true, Turns: 7, ChecksPerMessage: checks[:1], Regressions: ip(0), Rework: ip(0), RedBeforeGreen: bp(true)},
		Metrics{Task: "invoice-slices", Condition: "clean", Repeat: 1, Success: false, Turns: 6, Regressions: ip(0), Rework: ip(1), RedBeforeGreen: bp(false)},
	)
	var out bytes.Buffer
	if err := Report(dir, &out); err != nil {
		t.Fatal(err)
	}
	table := out.String()
	results, fidelity, byCond, cost := strings.Index(table, "results (quality first)"), strings.Index(table, "methodology fidelity"),
		strings.Index(table, "by condition"), strings.Index(table, "tokens and time")
	if results < 0 || fidelity < results || byCond < fidelity || cost < byCond {
		t.Fatalf("sections out of order (results %d, fidelity %d, by condition %d, cost %d):\n%s", results, fidelity, byCond, cost, table)
	}
	for _, s := range []string{"final checks", "regressions", "rework", "1/2", "per message", "message 2"} {
		if !strings.Contains(table, s) {
			t.Errorf("table lacks %q:\n%s", s, table)
		}
	}
	var rep struct {
		Rows        []Row
		ByCondition []ConditionSummary `json:"by_condition"`
	}
	b, _ := os.ReadFile(filepath.Join(dir, "report.json"))
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.ByCondition) != 3 || rep.ByCondition[0].Condition != "jhon" || rep.ByCondition[0].Passed != (Rate{Yes: 1, Known: 1}) ||
		rep.ByCondition[0].Regressions != 1 || rep.ByCondition[0].Rework != 2 || rep.ByCondition[0].FinalChecks != (Rate{Yes: 1, Known: 2}) {
		t.Errorf("by condition %+v", rep.ByCondition)
	}
	if q := rep.Rows[0].Quality; q.FinalChecks != (Rate{Yes: 1, Known: 2}) || q.Regressions != 1 || q.Rework == nil || *q.Rework != 2 {
		t.Errorf("row quality %+v", q)
	}
}

// replay is Claude Code's api-request-blob record (2.1.310) that repeats the
// transcript's records from the first to the last, tool_use ids and all.
func replay(t *testing.T, transcript []byte, kind string) []byte {
	t.Helper()
	var b bytes.Buffer
	for _, r := range records(transcript) {
		r["type"] = kind
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(append(line, '\n'))
	}
	return b.Bytes()
}

// A replayed tool_use is neither a new edit nor a new run (T47).
func TestClaudeCodeReplaysAreNotNewToolCalls(t *testing.T) {
	write, edit := step{tool: "Write", file: "a.py"}, step{tool: "Edit", file: "a.py"}
	first := claudeTranscript(write, testPasses)
	for _, kind := range []string{"api-request-blob", "assistant"} {
		blob := slices.Concat(first, replay(t, claudeTranscript(write), kind))
		if got := transcriptProcess(blob, "python3 -m unittest").Rework; got != 0 {
			t.Errorf("%s: a replayed Write counted as rework %d", kind, got)
		}
		// The replayed tool_use alone, its result not repeated.
		use, _, _ := bytes.Cut(claudeTranscript(testFails), []byte("\n"))
		failed := slices.Concat(claudeTranscript(testFails), replay(t, use, kind))
		if calls := toolCalls(failed); len(calls) != 1 || !calls[0].failed {
			t.Errorf("%s: a replay hid the failed run: %+v", kind, calls)
		}
	}
	if got := transcriptProcess(claudeTranscript(write, testPasses, edit), "python3 -m unittest").Rework; got != 1 {
		t.Errorf("a real second edit: rework %d, want 1", got)
	}
}
