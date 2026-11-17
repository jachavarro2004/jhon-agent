package main

import (
	"slices"
	"strings"
	"testing"
)

// The result records of a headless claude -p run whose orchestrator
// launched a background agent (T90 probe, L754, claude 2.1.323): the
// process writes a result when the first turn ends, wakes when the agent
// finishes and writes a second one. total_cost_usd is the process's
// running total (equal in both); num_turns and usage are per segment
// (2 then 1 turns; the second usage is only the wake-up turn's call).
const twoResultProbe = `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"assistant","message":{"usage":{"output_tokens":16}}}
{"type":"assistant","message":{"usage":{"output_tokens":24}}}
{"type":"system","subtype":"task_notification"}
{"type":"result","subtype":"success","is_error":false,"stop_reason":"end_turn","num_turns":2,"duration_ms":3056,"result_index":0,"total_cost_usd":0.1355807,"usage":{"input_tokens":4,"cache_creation_input_tokens":19721,"cache_read_input_tokens":43070,"output_tokens":168},"session_id":"s1"}
{"type":"system","subtype":"init","session_id":"s1"}
{"type":"assistant","message":{"usage":{"output_tokens":8}}}
{"type":"result","subtype":"success","is_error":false,"stop_reason":"end_turn","num_turns":1,"duration_ms":1516,"result_index":1,"total_cost_usd":0.1355807,"usage":{"input_tokens":4,"cache_creation_input_tokens":528,"cache_read_input_tokens":31664,"output_tokens":9},"session_id":"s1"}`

func TestAClaudeRunWithTwoResultsCountsItsCostOnce(t *testing.T) {
	f := claudeStream([]byte(twoResultProbe))
	if f.CostUSD != 0.1355807 {
		t.Errorf("cost %v, want 0.1355807 (the running total, once)", f.CostUSD)
	}
	if f.Turns != 3 || f.OutputTokens != 177 {
		t.Errorf("turns %d output %v, want 3 and 177 (per-segment fields summed)", f.Turns, f.OutputTokens)
	}
}

func TestAClaudeRunWithTwoResultsSumsItsSegmentsPerMessage(t *testing.T) {
	stream := `{"type":"bench_message","n":1,"phase":"start","at_ms":0}` + "\n" + twoResultProbe + "\n" +
		`{"type":"bench_message","n":1,"phase":"end","at_ms":4600,"reason":"settled"}`
	got := messageMetrics([]byte(stream), 0)
	want := []MessageMetrics{{N: 1, InputTokens: 8, OutputTokens: 177, CacheReadTokens: 74734, CacheWriteTokens: 20249, Turns: 3, WallMs: 4600, End: "settled"}}
	if !slices.Equal(got, want) {
		t.Errorf("\n%+v\nwant\n%+v", got, want)
	}
}

// Each message of a multi-message run is its own claude process (resumed
// session), so each process's running total adds; a later record of the
// same process (result_index > 0) replaces its earlier total.
func TestEachClaudeProcessCostAddsOnce(t *testing.T) {
	second := strings.ReplaceAll(twoResultProbe, "0.1355807", "0.05")
	second = strings.Replace(second, `"result_index":1,"total_cost_usd":0.05`, `"result_index":1,"total_cost_usd":0.07`, 1)
	f := claudeStream([]byte(twoResultProbe + "\n" + second))
	if want := 0.1355807 + 0.07; f.CostUSD < want-1e-9 || f.CostUSD > want+1e-9 {
		t.Errorf("cost %v, want %v", f.CostUSD, want)
	}
	// Records without result_index (older Claude Code) keep the old sum.
	old := `{"type":"result","num_turns":1,"total_cost_usd":0.25}` + "\n" + `{"type":"result","num_turns":1,"total_cost_usd":0.5}`
	if f := claudeStream([]byte(old)); f.CostUSD != 0.75 || f.Turns != 2 {
		t.Errorf("old %+v", f)
	}
}
