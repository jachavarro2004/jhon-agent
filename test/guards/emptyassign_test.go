package guards

import (
	"strings"
	"testing"
)

// T160: safety-guard reads a Bash line that assigns an empty quoted string
// (`out=""`) and allows it, top level and sub-agent: no panic, no decision.
// It read the empty double-quoted word's first part and crashed with exit 2.
func TestSafetyGuardEmptyAssign(t *testing.T) {
	t.Parallel()
	for _, command := range []string{`out=""`, `out=""; ls`, `out=''`, `out="x"`} {
		for _, o := range []sgOpts{top(), {}} {
			s := newSG(t)
			r := s.bash(t, command, o)
			label := command + map[bool]string{true: " (top)", false: " (sub-agent)"}[o.top]
			if r.Code != 0 {
				t.Errorf("%s: exits %d, want 0: %s", label, r.Code, r.Stderr)
			}
			if strings.Contains(r.Stderr, "panic") || strings.Contains(r.Stderr, "index out of range") {
				t.Errorf("%s: %s", label, r.Stderr)
			}
			noDecision(t, r.Stdout, label)
		}
	}
}
