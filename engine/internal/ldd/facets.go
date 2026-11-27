package ldd

import (
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
)

// resilienceCode is added code that starts processes, runs concurrently, or
// retries and times out: what the failure facet inspects.
var resilienceCode = lazyre.New(`(?i)(exec\.Command|subprocess|child_process|\bspawn\(|\bgo func\b|sync\.(Mutex|RWMutex|WaitGroup)|\basyncio\b|\bthreading\b|Promise\.all|\bretr(y|ies)\b|\bbackoff\b|\btimeout\b|WithTimeout|WithDeadline)`)

// facetsOf is the Prism facets a diff calls for, in Facets order
// (contracts/review-prism.md, "Facets"): safety for security, data, contracts
// and dependencies; failure for delivery and for code that starts processes,
// runs concurrently or retries; behavior for code; clarity for a contract
// others consume. A diff that calls for none falls back to behavior, so a
// due Prism review always has a facet.
func facetsOf(changed []Changed, reasons []Reason) []string {
	added := map[string]string{}
	for _, c := range changed {
		added[c.File] = c.Added
	}
	want := map[string]bool{}
	for _, r := range reasons {
		switch {
		case r.Level == Passive:
		case r.Why == "security path", r.Why == "migration or schema", r.Why == "destructive data statement",
			r.Why == "dependency manifest", strings.HasPrefix(r.Why, "the diff cannot be read"):
			want["safety"] = true
		case r.Why == "contract others consume":
			want["safety"], want["clarity"] = true, true
		case r.Why == "delivery or infrastructure":
			want["failure"] = true
		default:
			want["behavior"] = true
			if r.Level == High && strings.HasPrefix(r.Why, "project rule") {
				want["safety"] = true
			}
		}
		// One file that calls for failure is enough: no other is read (T268).
		if r.Level != Passive && !want["failure"] && resilient(added[r.File]) {
			want["failure"] = true
		}
	}
	out := slices.DeleteFunc(slices.Clone(Facets), func(l string) bool { return !want[l] })
	if len(out) == 0 {
		return []string{"behavior"}
	}
	return out
}
