package render

import "testing"

// fixedContext is the text every first call of runtime carries before any
// task: the always-on layer as AlwaysOnBundle gives it plus the
// orchestrator's body as that runtime renders it (T146 FA3).
func fixedContext(e *engine, runtime string) (bundle, body int) {
	return len(e.alwaysOnBundle(runtime)), len(e.bodyFor(runtime, e.mainAgent(piMain)))
}

// T146 FA3a (L1218): the fixed context is at most 70% of fix/t145b's
// (Claude Code 32879 bytes, pi 32661), each rule moved out of it read on
// demand (ondemand.go). Its pointers name the install's real path (T146c
// K3), so it is measured at a representative home, not the test's long
// temporary one.
func TestFixedContextBytes(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	e.home = "/home/owner"
	for rt, before := range map[string]int{"claude-code": 32879, "pi": 32661} {
		b, o := fixedContext(e, rt)
		t.Logf("%s: bundle %d + orchestrator %d = %d (fix/t145b %d, %.1f%%)", rt, b, o, b+o, before, 100*float64(b+o)/float64(before))
		if limit := before * 70 / 100; b+o > limit {
			t.Errorf("%s's fixed context is %d bytes, over 70%% of fix/t145b's %d (%d)", rt, b+o, before, limit)
		}
	}
}
