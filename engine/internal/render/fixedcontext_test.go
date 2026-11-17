package render

import "testing"

// fixedContext is the text every first call of runtime carries before any
// task: the always-on layer as AlwaysOnBundle gives it plus the
// orchestrator's body as that runtime renders it (T146 FA3).
func fixedContext(e *engine, runtime string) (bundle, body int) {
	return len(e.alwaysOnBundle(runtime)), len(e.bodyFor(runtime, e.mainAgent(piMain)))
}

func TestFixedContextBytes(t *testing.T) {
	e := engineAt(t, repo, "--validate")
	for _, rt := range []string{"claude-code", "pi"} {
		b, o := fixedContext(e, rt)
		t.Logf("%s: bundle %d + orchestrator %d = %d", rt, b, o, b+o)
	}
}
