package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The usage a transcript records, read from fixtures: pi's message.usage
// with its cost, Claude Code's message.usage without one.

func init() { zone = time.UTC }

func piLine(at, model string, input, output, read, write, cost float64, withCost bool) string {
	u := map[string]any{"input": input, "output": output, "cacheRead": read, "cacheWrite": write, "totalTokens": input + output + read + write}
	if withCost {
		u["cost"] = map[string]any{"input": 0, "total": cost}
	}
	b, _ := json.Marshal(map[string]any{"type": "message", "timestamp": at, "message": map[string]any{"role": "assistant", "provider": "p", "model": model, "usage": u}})
	return string(b)
}

func claudeLine(at, id, model string, input, output, read, create float64) string {
	b, _ := json.Marshal(map[string]any{"type": "assistant", "timestamp": at, "message": map[string]any{"id": id, "model": model, "role": "assistant",
		"usage": map[string]any{"input_tokens": input, "output_tokens": output, "cache_read_input_tokens": read, "cache_creation_input_tokens": create}}})
	return string(b)
}

func TestUsageOfAPiSessionCarriesItsCostByDayAndModel(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "2026-11-01T10-00-00-000Z_01a1871b-4a53-7504-abd1-df609beb96cf.jsonl")
	write(t, main, strings.Join([]string{
		`{"type":"session","id":"01a1871b","timestamp":"2026-11-01T10:00:00.000Z","cwd":"/w/p"}`,
		`{"type":"message","timestamp":"2026-11-01T10:00:01.000Z","message":{"role":"user","content":"hi"}}`,
		piLine("2026-11-01T10:00:02.000Z", "opus", 10, 20, 30, 40, 0.5, true),
		piLine("2026-11-01T11:00:00.000Z", "opus", 1, 2, 3, 4, 0.25, true),
		piLine("2026-11-02T09:00:00.000Z", "haiku", 5, 5, 0, 0, 0, false),
		`not json`,
	}, "\n")+"\n")
	sub := filepath.Join(strings.TrimSuffix(main, ".jsonl"), "subagents", "2026-11-01T10-00-05-000Z_toolu_1.jsonl")
	os.MkdirAll(filepath.Dir(sub), 0o755)
	write(t, sub, piLine("2026-11-01T10:00:06.000Z", "haiku", 100, 0, 0, 0, 0.125, true)+"\n")
	got := UsageOf(main)
	want := []UsageRow{
		{Day: "2026-11-01", Model: "haiku", Input: 100, Total: 100, Cost: ptr(0.125), Messages: 1},
		{Day: "2026-11-01", Model: "opus", Input: 11, Output: 22, CacheRead: 33, CacheWrite: 44, Total: 110, Cost: ptr(0.75), Messages: 2},
		{Day: "2026-11-02", Model: "haiku", Input: 5, Output: 5, Total: 10, Messages: 1},
	}
	eqJSON(t, got.Rows, want, "pi rows (sub-agents counted; cost only where recorded)")
}

func TestUsageOfAClaudeCodeSessionCountsEachMessageOnceWithNoCost(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, idA+".jsonl")
	write(t, main, strings.Join([]string{
		`{"type":"user","timestamp":"2026-11-03T08:00:00.000Z","message":{"role":"user","content":"go"}}`,
		// One response streamed as two entries: the last one's usage counts, once.
		claudeLine("2026-11-03T08:00:01.000Z", "msg_1", "claude-opus-5-5", 4, 10, 1000, 300),
		claudeLine("2026-11-03T08:00:02.000Z", "msg_1", "claude-opus-5-5", 4, 260, 1000, 300),
		claudeLine("2026-11-03T08:00:03.000Z", "msg_2", "<synthetic>", 0, 0, 0, 0),
	}, "\n")+"\n")
	sub := filepath.Join(dir, idA, "subagents", "agent-a1.jsonl")
	os.MkdirAll(filepath.Dir(sub), 0o755)
	write(t, sub, claudeLine("2026-11-03T08:00:05.000Z", "msg_3", "claude-haiku-5-5", 1, 1, 1, 1)+"\n")
	eqJSON(t, UsageOf(main).Rows, []UsageRow{
		{Day: "2026-11-03", Model: "claude-haiku-5-5", Input: 1, Output: 1, CacheRead: 1, CacheWrite: 1, Total: 4, Messages: 1},
		{Day: "2026-11-03", Model: "claude-opus-5-5", Input: 4, Output: 260, CacheRead: 1000, CacheWrite: 300, Total: 1564, Messages: 1},
	}, "claude rows")
	if rows := UsageOf(filepath.Join(dir, "missing.jsonl")).Rows; len(rows) != 0 {
		t.Errorf("a missing transcript has no usage: %v", rows)
	}
}

func TestTheSummaryCarriesTheUsageUnderANewKey(t *testing.T) {
	dir := projects(t)
	write(t, filepath.Join(dir, "-home-u-Projects-acme", idA+".jsonl"), claudeLine("2026-11-03T08:00:01.000Z", "msg_1", "claude-opus-5-5", 1, 2, 3, 4)+"\n")
	s := serve(t, dir)
	r := s.get(t, "/api/session/"+idA, "GET")
	var data struct {
		Usage Usage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(r.body), &data); err != nil || len(data.Usage.Rows) != 1 || data.Usage.Rows[0].Total != 10 {
		t.Errorf("/api/session carries the session's usage: %s", r.body)
	}
	sum, _ := apiSessions(t, s)[0]["summary"].(map[string]any)
	usage, _ := sum["usage"].(map[string]any)
	rows, _ := usage["rows"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["cost"] != nil {
		t.Errorf("the summary carries the usage, cost null for Claude Code: %v", sum["usage"])
	}
	if !strings.HasPrefix(summaryKey(Session{}), "v6:") {
		t.Errorf("a summary without usage is audited again: key %q", summaryKey(Session{}))
	}
}

func ptr(f float64) *float64 { return &f }

func eqJSON(t *testing.T, got, want any, what string) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Errorf("%s:\n got %s\nwant %s", what, g, w)
	}
}
