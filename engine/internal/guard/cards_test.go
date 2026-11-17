package guard

import "testing"

// Every guard pre-tool runs has a card, and no card names a guard that does
// not run: a guard without a reason to exist is not kept.
func TestEveryGuardHasACard(t *testing.T) {
	if len(Cards) != len(Matchers) {
		t.Errorf("%d cards for %d guards", len(Cards), len(Matchers))
	}
	for i, m := range Matchers {
		c, ok := CardOf(m.Guard)
		if !ok || c.Covers == "" || c.Because == "" {
			t.Errorf("%s has no full card: %+v", m.Guard, c)
		}
		if ok && Cards[i].Guard != m.Guard {
			t.Errorf("card %d is %s, the guard %s", i, Cards[i].Guard, m.Guard)
		}
	}
}
