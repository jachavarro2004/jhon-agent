package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// ccDenial is a Claude Code stream whose top-level call of tool the
// headless runtime denied (an ask nobody answered, listed in the result's
// permission_denials), as smoke-6cb76991-opus account-import-plain-jhon-1
// shows for engram's mem_save (L761).
func ccDenial(tool string) []byte {
	enc := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	return []byte(strings.Join([]string{
		enc(map[string]any{"type": "assistant", "parent_tool_use_id": nil, "message": map[string]any{"role": "assistant",
			"content": []any{map[string]any{"type": "tool_use", "id": "toolu_denied", "name": tool, "input": map[string]any{"command": "echo x"}}}}}),
		enc(map[string]any{"type": "user", "parent_tool_use_id": nil, "message": map[string]any{"role": "user",
			"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_denied", "is_error": true,
				"content": "Claude requested permissions to use " + tool + ", but you haven't granted it yet."}}}}),
		enc(map[string]any{"type": "result", "permission_denials": []any{map[string]any{"tool_name": tool, "tool_use_id": "toolu_denied"}}}),
	}, "\n") + "\n")
}

// A denial of a tool no Jhon guard judges (engram's, through the bench's
// --allowedTools) is no refusal: it is kept as a denial naming its tool. The
// same denial of Bash, which the guards judge, is one (a guard's ask the
// headless runtime denied).
func TestADenialOfAToolNoGuardJudgesIsNoRefusal(t *testing.T) {
	b := ccDenial("mcp__engram__mem_save")
	if got := guardEvents(b); !slices.Equal(got, []guardEvent{{Kind: "denial", Tool: "mcp__engram__mem_save"}}) {
		t.Errorf("events %+v, want one engram denial", got)
	}
	if n := streamRefusals(b); n == nil || *n != 0 {
		t.Errorf("refusals %v, want 0", deref(n))
	}
	if n := streamRefusals(ccDenial("Bash")); n == nil || *n != 1 {
		t.Errorf("a denied Bash ask: refusals %v, want 1", deref(n))
	}
}

// refusalCheck is certify.json's refusals check of invoice-slices plain jhon.
func refusalCheck(t *testing.T, c Certification) CertCheck {
	t.Helper()
	for _, k := range c.Checks {
		if k.Metric == "refusals and declined dialogs" && k.Variant == "plain" && k.Condition == "jhon" {
			return k
		}
	}
	t.Fatalf("no refusals check: %+v", c.Checks)
	return CertCheck{}
}

// certify counts refusals the same way on both sides (L761): from each
// run's stream when it has one, so a baseline whose metrics.json predates
// the stream count (T105) is recounted; a run with no stream keeps
// metrics.json's count, and the report says so.
func TestCertifyCountsRefusalsTheSameWayOnBothSides(t *testing.T) {
	base, cand := fixture(t, 1, refusedOnce("jhon"))
	putStream(t, cand, "invoice-slices-plain-jhon-1", ccRefusal("echo refuse-me", ""), false)
	// The baseline's metrics.json says 0, its stream shows the refusal.
	putStream(t, base, "invoice-slices-plain-jhon-1", ccRefusal("echo refuse-me", ""), false)
	dir := t.TempDir()
	_, out, _ := certify(t, nil, "--baseline", base, "--candidate", cand, "--out", dir)
	c := readCert(t, dir)
	if k := refusalCheck(t, c); !k.Pass || k.Detail != "0.33 per run vs baseline 0.33" {
		t.Errorf("check %+v\n%s", k, out)
	}
	for _, want := range []string{
		"note: baseline: refusals recounted from the stream in 1 of 12 runs; 11 have no readable stream.jsonl and keep metrics.json's count",
		"note: candidate: refusals recounted from the stream in 1 of 24 runs; 23 have no readable stream.jsonl and keep metrics.json's count",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
}

// A candidate whose only "refusal" is engram's denial (counted by T105's
// metrics.json) has none; the denial stays visible as a note.
func TestCertifyShowsADenialOfAToolNoGuardJudgesApart(t *testing.T) {
	base, cand := fixture(t, 1, refusedOnce("jhon"))
	putStream(t, cand, "invoice-slices-plain-jhon-1", ccDenial("mcp__engram__mem_save"), false)
	dir := t.TempDir()
	_, out, _ := certify(t, nil, "--baseline", base, "--candidate", cand, "--out", dir)
	if k := refusalCheck(t, readCert(t, dir)); !k.Pass || k.Detail != "0 per run vs baseline 0" {
		t.Errorf("check %+v\n%s", k, out)
	}
	want := "note: candidate: Claude Code permission denials of tools no Jhon guard judges, not counted as refusals: mcp__engram__mem_save 1 (invoice-slices-plain-jhon-1)"
	if !strings.Contains(out, want) {
		t.Errorf("no %q in\n%s", want, out)
	}
}
