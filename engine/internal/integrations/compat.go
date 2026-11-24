package integrations

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/jachavarro2004/jhon-agent/engine/internal/version"
)

// A cli tool says in `<binary> integration --json` what jhon it needs (T237,
// odd/tasks/studio-integration-v1.md S5; integrations/README.md, "A cli
// integration"): requires_jhon, a SemVer range such as ">=0.5.0-rc.1", and
// contract, the contract its answer speaks. jhon start, setup and doctor
// judge the answer against the running jhon (Judge): one that does not fit
// is off for that run, with one note, and the core goes on as it does with
// the tool off. An older tool that names neither is accepted.

// Contract is the contract of `<binary> integration --json` this jhon
// speaks; an answer that names none speaks it.
const Contract = "jhon.integration/v1"

// Fit is how a tool's answer fits the running jhon.
type Fit struct {
	// Off is the one note that keeps the tool off for this run: its version,
	// what it requires, and the fix; "" when it fits.
	Off string
	// NoMinimum is an answer that declares no requires_jhon (an older tool).
	NoMinimum bool
	// Unchecked is a development build of jhon (version.IsDev), which
	// satisfies any minimum: the range was not checked.
	Unchecked bool
}

// Judge is how a, m's program's answer, fits jhon, the running version: off
// when it speaks a contract other than Contract, when its requires_jhon
// cannot be read, or when jhon is not in its range, compared by SemVer
// precedence (0.5.0-rc.1 < 0.5.0; build metadata ignored). A development
// build passes any range, never another contract.
func Judge(m Manifest, a Answer, jhon string) Fit {
	tool := m.Doctor.Binary
	named := tool + " " + a.Version
	if a.Version == "" {
		named = tool + " (no version)"
	}
	if c := a.contract(); c != Contract {
		requires := ""
		if a.RequiresJhon != "" {
			requires = " (it requires jhon " + a.RequiresJhon + ")"
		}
		return Fit{Off: fmt.Sprintf("%s speaks %s%s, and this jhon %s speaks %s: %s is off for this run; update jhon (jhon update), or install a %s version that speaks %s",
			named, c, requires, jhon, Contract, tool, tool, Contract)}
	}
	if a.RequiresJhon == "" {
		return Fit{NoMinimum: true}
	}
	fix := fmt.Sprintf("%s is off for this run; update jhon (jhon update), or install a %s version that supports jhon %s", tool, tool, jhon)
	r, ok := parseRange(a.RequiresJhon)
	if !ok {
		return Fit{Off: fmt.Sprintf("%s requires jhon %q, a range this jhon %s cannot read: %s", named, a.RequiresJhon, jhon, fix)}
	}
	if version.IsDev(jhon) {
		return Fit{Unchecked: true}
	}
	if !r.holds(jhon) {
		return Fit{Off: fmt.Sprintf("%s requires jhon %s, and this is jhon %s: %s", named, a.RequiresJhon, jhon, fix)}
	}
	return Fit{}
}

// contract is the contract a speaks: its own, else Contract.
func (a Answer) contract() string {
	if a.Contract == "" {
		return Contract
	}
	return a.Contract
}

// A versionRange is requires_jhon read: alternatives joined by `||`, each a
// set of comparators every one of which must hold.
type versionRange [][]comparator

// A comparator is an operator (>=, <=, >, <, =; none is =) and a full
// MAJOR.MINOR.PATCH version, with an optional pre-release and build, as "v"
// semver reads it.
type comparator struct{ op, v string }

var (
	comparatorPattern = regexp.MustCompile(`^(>=|<=|>|<|=)?\s*v?(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)$`)
	operatorAlone     = regexp.MustCompile(`^(>=|<=|>|<|=)$`)
)

// parseRange reads s; false when any part of it is not a comparator: a
// partial version (1.x, >=0.5), a caret or tilde range, or an empty set.
func parseRange(s string) (versionRange, bool) {
	var r versionRange
	for _, alt := range strings.Split(s, "||") {
		words := strings.Fields(alt)
		var set []comparator
		for i := 0; i < len(words); i++ {
			w := words[i]
			if operatorAlone.MatchString(w) && i+1 < len(words) {
				i++
				w += words[i]
			}
			m := comparatorPattern.FindStringSubmatch(w)
			if m == nil || !semver.IsValid("v"+m[2]) {
				return nil, false
			}
			set = append(set, comparator{m[1], "v" + m[2]})
		}
		if len(set) == 0 {
			return nil, false
		}
		r = append(r, set)
	}
	return r, true
}

// holds is whether v is in r.
func (r versionRange) holds(v string) bool {
	for _, set := range r {
		all := true
		for _, c := range set {
			if !c.holds("v" + v) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func (c comparator) holds(v string) bool {
	n := semver.Compare(v, c.v)
	switch c.op {
	case ">=":
		return n >= 0
	case "<=":
		return n <= 0
	case ">":
		return n > 0
	case "<":
		return n < 0
	}
	return n == 0
}
