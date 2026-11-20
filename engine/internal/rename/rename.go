// Package rename holds the names T177 (L1453, L1454) retired: the agents
// explore, worker and verify, now jhon-scout, jhon-builder and
// jhon-checker; the 4R review, now the Prism review, its lenses now facets
// and its six agents prism-*; and the review_required values 4r and verify,
// now prism and checker. For one version an old name is read as its new
// one with a one-line warning; `jhon settings migrate` and `jhon profile`
// rewrite it. A record written under an old name stays readable and is
// shown under the new one. The aliases go in RemovedIn.
package rename

import "fmt"

// RemovedIn is the release that stops reading the old names.
const RemovedIn = "v0.4"

// agents are the retired agent names and the name each one is now.
var agents = map[string]string{
	"explore":            "jhon-scout",
	"worker":             "jhon-builder",
	"verify":             "jhon-checker",
	"review-risk":        "prism-safety",
	"review-resilience":  "prism-failure",
	"review-readability": "prism-clarity",
	"review-reliability": "prism-behavior",
	"review-refuter":     "prism-challenger",
	"review-validator":   "prism-arbiter",
}

// facets are the retired lens names of the review and the facet each one is now.
var facets = map[string]string{
	"risk":        "safety",
	"resilience":  "failure",
	"readability": "clarity",
	"reliability": "behavior",
}

// reviews are the retired review_required values and the value each one is now.
var reviews = map[string]string{
	"4r":     "prism",
	"verify": "checker",
}

// Agent is name's current agent name, and whether name is a retired one.
func Agent(name string) (string, bool) {
	if now, ok := agents[name]; ok {
		return now, true
	}
	return name, false
}

// AgentNow is name's current agent name: a retired one mapped, any other as is.
func AgentNow(name string) string {
	now, _ := Agent(name)
	return now
}

// Facet is name's current facet name, and whether name is a retired one.
func Facet(name string) (string, bool) {
	if now, ok := facets[name]; ok {
		return now, true
	}
	return name, false
}

// FacetNow is name's current facet name: a retired one mapped, any other as is.
func FacetNow(name string) string {
	now, _ := Facet(name)
	return now
}

// Review is a review_required value's current name (lower case expected),
// and whether it is a retired one.
func Review(value string) (string, bool) {
	if now, ok := reviews[value]; ok {
		return now, true
	}
	return value, false
}

// Warning is the one-line warning for a retired name read where (a file and
// a key, say): what it is now, until when the old one is read, and the
// command that rewrites it.
func Warning(where, what, old, now string) string {
	return fmt.Sprintf("%s: %s %q is now %q; the old name is read until %s (jhon settings migrate rewrites it)", where, what, old, now, RemovedIn)
}

// AgentOld are the retired agent names, for a reader that must find a
// record written under one.
func AgentOld(now string) []string {
	var out []string
	for old, n := range agents {
		if n == now {
			out = append(out, old)
		}
	}
	return out
}
