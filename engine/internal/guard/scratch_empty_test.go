package guard

import "testing"

// T160: an assignment of an empty quoted string is no `$(mktemp …)`; reading
// it never panics (mktempValue indexed the empty double-quoted word's parts).
func TestLineFactsEmptyQuotedAssignment(t *testing.T) {
	for _, line := range []string{`out=""`, `out=''`, `out="" X=1`, `out=""; echo hi`, `d=$(mktemp -d); out=""`} {
		facts := lineFacts(line)
		if facts != nil && facts.resolves("out") {
			t.Errorf("%q: out resolves as a mktemp file", line)
		}
		if v, ok := freshVars(line, "/work/p")["out"]; ok {
			t.Errorf("%q: freshVars holds out = %q", line, v)
		}
	}
	// The quoted mktemp it unwraps still reads as one.
	for _, line := range []string{`out="$(mktemp)"`, `out=$(mktemp -d)`} {
		if facts := lineFacts(line); facts == nil || !facts.resolves("out") {
			t.Errorf("%q: out does not resolve as a mktemp file", line)
		}
	}
}
