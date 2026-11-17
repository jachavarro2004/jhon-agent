package ldd

import (
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
)

// resilienceCode is added code that starts processes, runs concurrently, or
// retries and times out: what the resilience lens inspects.
var resilienceCode = lazyre.New(`(?i)(exec\.Command|subprocess|child_process|\bspawn\(|\bgo func\b|sync\.(Mutex|RWMutex|WaitGroup)|\basyncio\b|\bthreading\b|Promise\.all|\bretr(y|ies)\b|\bbackoff\b|\btimeout\b|WithTimeout|WithDeadline)`)

// lensesOf is the 4R lenses a diff calls for, in Lenses order
// (contracts/review-4r.md, "Lenses"): risk for security, data, contracts and
// dependencies; resilience for delivery and for code that starts processes,
// runs concurrently or retries; reliability for code; readability for a
// contract others consume. A diff that calls for none falls back to
// reliability, so a due 4R always has a lens.
func lensesOf(changed []Changed, reasons []Reason) []string {
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
			want["risk"] = true
		case r.Why == "contract others consume":
			want["risk"], want["readability"] = true, true
		case r.Why == "delivery or infrastructure":
			want["resilience"] = true
		default:
			want["reliability"] = true
			if r.Level == High && strings.HasPrefix(r.Why, "project rule") {
				want["risk"] = true
			}
		}
		if r.Level != Passive && resilienceCode.MatchString(added[r.File]) {
			want["resilience"] = true
		}
	}
	out := slices.DeleteFunc(slices.Clone(Lenses), func(l string) bool { return !want[l] })
	if len(out) == 0 {
		return []string{"reliability"}
	}
	return out
}
