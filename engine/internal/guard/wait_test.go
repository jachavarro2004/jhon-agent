package guard

import "testing"

func TestUnboundedWait(t *testing.T) {
	for command, want := range map[string]bool{
		// The two loops two workers left sleeping (a client project, 2026-10-27).
		`until grep -q "restored\|nothing to restore" /tmp/t/baj2oduy0.output; do sleep 3; done`: true,
		`cd x && until grep -q formatted out.txt; do sleep 3; done; tail out.txt`:                true,
		`while kill -0 4008129 2>/dev/null; do sleep 15; done; tail -3 f`:                        true,
		`until gh pr checks 715 | rg -q 'ci-success\s+(pass|fail)'; do sleep 30; done`:           true,
		`bash -c 'until test -s f; do sleep 2; done'`:                                            true,
		`while true; sleep 3; end`:           true,
		`while true; do date; sleep 1; done`: true,
		// Bounded, or no wait at all.
		`timeout 600 bash -c 'until test -s f; do sleep 2; done'`:                         false,
		`timeout -k 5 600 sh -c 'while ! test -s f; do sleep 2; done'`:                    false,
		`for i in $(seq 1 55); do grep -q EXIT= f && break; sleep 10; done; tail f`:       false,
		`for i in (seq 1 55); if rg -q EXIT= f; break; end; sleep 10; end`:                false,
		`while read -r line; do echo "$line"; sleep 1; done < f`:                          false,
		`while IFS= read -r l; do sleep 1; done < f`:                                      false,
		`sleep 100; cat out | tail -40`:                                                   false,
		`until make; do echo retry; done`:                                                 false,
		`ps -eo pid,args | rg 'until ! pgrep -f "pytest -q"' | awk '{print $1}'; sleep 1`: false,
	} {
		if got := UnboundedWait(command); got != want {
			t.Errorf("UnboundedWait(%q) = %v, want %v", command, got, want)
		}
	}
}
