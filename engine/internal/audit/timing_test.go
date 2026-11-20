package audit

import (
	"math"
	"testing"
)

// Where a thread's time went (timing.go).

func stamp(s string) string { return "2026-10-30T10:" + s + "Z" }

func TestThreadTimingSplitsModelToolAndWaiting(t *testing.T) {
	id := func(s string) *string { return &s }
	u := func(in, created, read float64) *Usage { return &Usage{in, created, read} }
	tr := &Transcript{Agent: "a1", AgentType: "jhon-builder", Entries: []*Entry{
		{Role: "user", Timestamp: stamp("00:00.000"), Prompt: id("do it")},
		// 20s to the first response line, 5s more for its second line: model.
		{Role: "assistant", Timestamp: stamp("00:20.000"), MessageID: id("m1")},
		{Role: "assistant", Timestamp: stamp("00:25.000"), MessageID: id("m1"), Usage: u(10, 90, 0)},
		// 30s until the tool result: the tool's.
		{Role: "user", Timestamp: stamp("00:55.000"), ToolResults: []ToolResult{{ID: "t1"}}},
		{Role: "assistant", Timestamp: stamp("01:05.000"), MessageID: id("m2"), Usage: u(10, 0, 90)},
		// A notification after a minute: waiting, neither model nor tool.
		{Role: "user", Timestamp: stamp("02:05.000"), Prompt: id("<task-notification>")},
		{Role: "assistant", Timestamp: stamp("02:07.000"), MessageID: id("m3"), Usage: u(0, 0, 100)},
	}}
	x := threadTiming(tr, true)
	if x.Turns != 3 || x.ModelMs != 37000 || x.ToolMs != 30000 || x.FirstResponseMs != 20000 {
		t.Errorf("turns %d, model %v, tool %v, first response %v; want 3, 37000, 30000, 20000", x.Turns, x.ModelMs, x.ToolMs, x.FirstResponseMs)
	}
	if x.Processed != 300 || x.CacheRead != 190 || math.Abs(x.CacheHit-190.0/300) > 1e-9 {
		t.Errorf("processed %v, cache read %v, hit %v", x.Processed, x.CacheRead, x.CacheHit)
	}
	if main := threadTiming(tr, false); !math.IsNaN(main.FirstResponseMs) {
		t.Errorf("the main thread's first response: %v, want NaN", main.FirstResponseMs)
	}
	if empty := threadTiming(&Transcript{}, true); empty.Turns != 0 || !math.IsNaN(empty.CacheHit) || !math.IsNaN(empty.FirstResponseMs) {
		t.Errorf("an empty thread: %+v", empty)
	}
}

func TestPiAssistantLinesAreEachAResponse(t *testing.T) {
	tr := &Transcript{Entries: []*Entry{
		{Role: "user", Timestamp: stamp("00:00.000")},
		{Role: "assistant", Timestamp: stamp("00:04.000"), Usage: &Usage{Input: 5}},
		{Role: "user", Timestamp: stamp("00:05.000"), ToolResults: []ToolResult{{ID: "t"}}},
		{Role: "assistant", Timestamp: stamp("00:09.000"), Usage: &Usage{Input: 5}},
	}}
	if x := threadTiming(tr, false); x.Turns != 2 || x.ModelMs != 8000 || x.ToolMs != 1000 || x.Processed != 10 {
		t.Errorf("%+v", x)
	}
}

func TestCompactionsAreReadFromBothRuntimes(t *testing.T) {
	r := piAudit(t, pstart("tui"), puser("go"), pstep{kind: "compaction", text: "hook", n: 120000}, pstep{kind: "compaction", n: 90000}, preply("done"))
	got := r.Timing.Compactions
	if len(got) != 2 || got[0].Trigger != "extension" || got[0].TokensBefore != 120000 || got[1].Trigger != "" || got[1].TokensBefore != 90000 || got[0].At == "" {
		t.Errorf("pi: %+v", got)
	}
	boundary := obj("type", "system", "subtype", "compact_boundary", "content", "Conversation compacted", "uuid", "u-c", "sessionId", sessionID,
		"version", "2.1.280", "timestamp", isoAt(5), "compactMetadata", obj("trigger", "auto", "preTokens", 967290.0, "durationMs", 64663.0))
	c := auditOf(t, user("go"), step{kind: "raw", raw: boundary}, step{kind: "reply", text: "done"})
	if got := c.Timing.Compactions; len(got) != 1 || got[0] != (Compaction{isoAt(5), "auto", 967290, 64663}) {
		t.Errorf("claude code: %+v", got)
	}
}
