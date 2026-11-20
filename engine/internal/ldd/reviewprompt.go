package ldd

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/execution"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// This file is the native Prism review (contracts/review-prism.md): the tool-less
// prompts `jhon review start`, `refute` and `validate` build from one
// frozen candidate, the shapes of the answers they ask for, and the
// reduction `jhon review record` writes to review-prism.md.

// PromptTokenCap is the estimated size past which a review prompt is
// refused: the slice is too large for one review.
const PromptTokenCap = 100000

// EstTokens is a prompt's estimated token count: its bytes over 3.5.
func EstTokens(bytes int) int { return int(float64(bytes) / 3.5) }

// ReviewFile is the Prism record's name in a change's directory.
const ReviewFile = "review-prism.md"

// ReviewExempt are the paths a review's subject leaves out: the change's
// own records (verify-report.md, the execution record, delivery.json) and
// its review-prism.md, all written after the review, relative to the git top
// level. Nil when change is "".
func ReviewExempt(top, change string) map[string]bool {
	if change == "" {
		return nil
	}
	out := execution.ChangeRecordPaths(top, change)
	out[path.Join(execution.ChangeDirInRepo(top, change), ReviewFile)] = true
	out[path.Join(execution.ChangeDirInRepo(top, change), legacyReviewFile)] = true
	return out
}

// Fill is template with each {{name}} replaced by vars[name], in one pass:
// a value that holds `{{...}}` (candidate content) is never expanded.
func Fill(template string, vars map[string]string) string {
	var pairs []string
	for _, k := range slices.Sorted(maps.Keys(vars)) {
		pairs = append(pairs, "{{"+k+"}}", vars[k])
	}
	return strings.NewReplacer(pairs...).Replace(template)
}

// FilesBlock is the content of every path the frozen candidate changes,
// each between its own markers: deleted when the candidate lacks it, its
// size only when it is binary.
func FilesBlock(top string, f vcs.Frozen) string {
	var b strings.Builder
	for _, p := range f.Paths {
		body, ok := vcs.Show(top, f.Candidate, p)
		switch {
		case !ok:
			fmt.Fprintf(&b, "----- FILE %s (deleted) -----\n", p)
			continue
		case slices.Contains(body, 0):
			fmt.Fprintf(&b, "----- FILE %s (binary, %d bytes) -----\n", p, len(body))
			continue
		}
		fmt.Fprintf(&b, "----- FILE %s -----\n%s", p, body)
		if len(body) > 0 && body[len(body)-1] != '\n' {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "----- END FILE %s -----\n", p)
	}
	return b.String()
}

// ReasonsBlock is the risk reasons above passive, one per line.
func ReasonsBlock(reasons []Reason) string {
	var lines []string
	for _, r := range reasons {
		if r.Level == Passive {
			continue
		}
		file := r.File
		if file == "" {
			file = "(the change)"
		}
		lines = append(lines, fmt.Sprintf("- %s %s: %s", r.Level, file, r.Why))
	}
	if len(lines) == 0 {
		return "- (none above passive)"
	}
	return strings.Join(lines, "\n")
}

// Finding is one facet finding (contracts/review-prism.md, "Findings").
type Finding struct {
	ID                string   `json:"id,omitempty"`
	Facet             string   `json:"facet,omitempty"`
	Location          string   `json:"location"`
	Severity          string   `json:"severity"`
	Claim             string   `json:"claim"`
	EvidenceClass     string   `json:"evidence_class,omitempty"`
	CausalDisposition string   `json:"causal_disposition,omitempty"`
	ProofRefs         []string `json:"proof_refs"`
}

// FacetResult is one facet's answer.
type FacetResult struct {
	SubjectHash string `json:"subject_hash"`
	Facet       string `json:"facet"`
	Inspection  struct {
		Status string   `json:"status"`
		Paths  []string `json:"paths"`
		Reason string   `json:"reason,omitempty"`
	} `json:"inspection"`
	Findings []Finding `json:"findings"`
	Evidence []string  `json:"evidence"`
}

// Verdict is the challenger's or the arbiter's answer on one finding.
type Verdict struct {
	FindingID string   `json:"finding_id"`
	Outcome   string   `json:"outcome"`
	Reason    string   `json:"reason"`
	ProofRefs []string `json:"proof_refs,omitempty"`
}

// RoleResult is the challenger's or the arbiter's answer.
type RoleResult struct {
	SubjectHash string    `json:"subject_hash"`
	Role        string    `json:"role"`
	Results     []Verdict `json:"results"`
}

// ReviewResults is what `jhon review validate` and `record` read: the facet
// results and the challenger's (and, for record, the arbiter's) answers.
type ReviewResults struct {
	Facets     []FacetResult `json:"facets"`
	Challenger *RoleResult   `json:"challenger"`
	Arbiter    *RoleResult   `json:"arbiter"`
}

var (
	reviewSeverities   = []string{"BLOCKER", "CRITICAL", "WARNING", "SUGGESTION"}
	reviewEvidence     = []string{"deterministic", "inferential"}
	reviewDispositions = []string{"introduced", "behavior_activated", "worsened", "pre_existing", "unknown"}
)

// severeFinding is BLOCKER or CRITICAL.
func severeFinding(f Finding) bool { return f.Severity == "BLOCKER" || f.Severity == "CRITICAL" }

// sameSubject is whether hash names subject, bare or as sha256:<hex>.
func sameSubject(hash, subject string) bool { return strings.TrimPrefix(hash, "sha256:") == subject }

// ParseFacetResults reads a JSON array of facet results.
func ParseFacetResults(raw []byte) ([]FacetResult, error) {
	var out []FacetResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("the facet results are not a JSON array of facet results: %v", err)
	}
	return out, nil
}

// ParseReviewResults reads {"facets": [...], "challenger": {...}, "arbiter": {...}}.
func ParseReviewResults(raw []byte) (ReviewResults, error) {
	var out ReviewResults
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("the results are not a JSON object {facets, challenger, arbiter}: %v", err)
	}
	return out, nil
}

// CheckFacets validates the facet results against subject and gives each
// finding its id (<facet>-<n>, from 1) and facet; the error names the first
// problem.
func CheckFacets(results []FacetResult, subject string) ([]FacetResult, error) {
	if len(results) == 0 {
		return nil, errors.New("no facet result")
	}
	seen := map[string]bool{}
	out := slices.Clone(results)
	for i := range out {
		r := &out[i]
		where := fmt.Sprintf("facet result %d", i+1)
		switch {
		case !slices.Contains(Facets, r.Facet):
			return nil, fmt.Errorf("%s: facet %q is not one of %s", where, r.Facet, strings.Join(Facets, ", "))
		case seen[r.Facet]:
			return nil, fmt.Errorf("%s: facet %s answers twice", where, r.Facet)
		case !sameSubject(r.SubjectHash, subject):
			return nil, fmt.Errorf("%s (%s): subject_hash %q is not the frozen subject %s", where, r.Facet, r.SubjectHash, subject)
		case r.Inspection.Status != "completed" && r.Inspection.Status != "unavailable":
			return nil, fmt.Errorf("%s (%s): inspection.status must be completed or unavailable", where, r.Facet)
		case r.Inspection.Status == "unavailable" && (strings.TrimSpace(r.Inspection.Reason) == "" || len(r.Findings) > 0):
			return nil, fmt.Errorf("%s (%s): an unavailable inspection carries a reason and no findings", where, r.Facet)
		case r.Findings == nil:
			return nil, fmt.Errorf("%s (%s): findings must be an array", where, r.Facet)
		}
		seen[r.Facet] = true
		for n := range r.Findings {
			f := &r.Findings[n]
			f.ID, f.Facet = fmt.Sprintf("%s-%d", r.Facet, n+1), r.Facet
			f.CausalDisposition = strings.ReplaceAll(f.CausalDisposition, "-", "_")
			switch {
			case strings.TrimSpace(f.Location) == "" || strings.TrimSpace(f.Claim) == "":
				return nil, fmt.Errorf("finding %s: location and claim are required", f.ID)
			case !slices.Contains(reviewSeverities, f.Severity):
				return nil, fmt.Errorf("finding %s: severity %q is not one of %s", f.ID, f.Severity, strings.Join(reviewSeverities, ", "))
			case f.EvidenceClass != "" && !slices.Contains(reviewEvidence, f.EvidenceClass):
				return nil, fmt.Errorf("finding %s: evidence_class %q is not one of %s", f.ID, f.EvidenceClass, strings.Join(reviewEvidence, ", "))
			case f.CausalDisposition != "" && !slices.Contains(reviewDispositions, f.CausalDisposition):
				return nil, fmt.Errorf("finding %s: causal_disposition %q is not one of %s", f.ID, f.CausalDisposition, strings.Join(reviewDispositions, ", "))
			case severeFinding(*f) && (f.EvidenceClass == "" || f.CausalDisposition == "" || len(f.ProofRefs) == 0):
				return nil, fmt.Errorf("finding %s: a %s finding states evidence_class, causal_disposition and proof_refs", f.ID, f.Severity)
			}
		}
	}
	return out, nil
}

// AllFindings are the findings of the facet results, in order.
func AllFindings(results []FacetResult) []Finding {
	var out []Finding
	for _, r := range results {
		out = append(out, r.Findings...)
	}
	return out
}

// CheckRole validates a challenger's (outcomes refuted, stands) or a
// arbiter's (confirmed, unconfirmed) answer: one verdict, with a reason,
// for each of ids and no other; a confirmation carries its proof_refs.
func CheckRole(r *RoleResult, role string, subject string, ids []string) error {
	if r == nil {
		return fmt.Errorf("no %s result", role)
	}
	outcomes := map[string][]string{"challenger": {"refuted", "stands"}, "arbiter": {"confirmed", "unconfirmed"}}[role]
	switch {
	case r.Role != "" && r.Role != role:
		return fmt.Errorf("the %s result names role %q", role, r.Role)
	case !sameSubject(r.SubjectHash, subject):
		return fmt.Errorf("the %s result's subject_hash %q is not the frozen subject %s", role, r.SubjectHash, subject)
	}
	seen := map[string]bool{}
	for _, v := range r.Results {
		switch {
		case !slices.Contains(ids, v.FindingID):
			return fmt.Errorf("the %s answers %q, a finding it was not given", role, v.FindingID)
		case seen[v.FindingID]:
			return fmt.Errorf("the %s answers %s twice", role, v.FindingID)
		case !slices.Contains(outcomes, v.Outcome):
			return fmt.Errorf("the %s's outcome for %s is %q, not one of %s", role, v.FindingID, v.Outcome, strings.Join(outcomes, ", "))
		case strings.TrimSpace(v.Reason) == "":
			return fmt.Errorf("the %s's verdict on %s gives no reason", role, v.FindingID)
		case v.Outcome == "confirmed" && len(v.ProofRefs) == 0:
			return fmt.Errorf("the arbiter confirms %s without proof_refs", v.FindingID)
		}
		seen[v.FindingID] = true
	}
	for _, id := range ids {
		if !seen[id] {
			return fmt.Errorf("the %s gives no verdict on %s", role, id)
		}
	}
	return nil
}

// Standing are the findings the challenger did not refute.
func Standing(findings []Finding, challenger *RoleResult) []Finding {
	refuted := map[string]bool{}
	for _, v := range challenger.Results {
		refuted[v.FindingID] = v.Outcome == "refuted"
	}
	return slices.DeleteFunc(slices.Clone(findings), func(f Finding) bool { return refuted[f.ID] })
}

// IDs are the findings' ids.
func IDs(findings []Finding) []string {
	out := []string{}
	for _, f := range findings {
		out = append(out, f.ID)
	}
	return out
}

// Reduction is the Prism review's reduction (contracts/review-prism.md, "Reduction"):
// the refuted findings drop; of those that stand, the pre-existing ones are
// follow-ups, the severe ones the arbiter confirms are the correction set
// and the severe ones it does not are noted for verify; the rest are
// information. The verdict passes with no correction set and no facet
// unavailable.
type Reduction struct {
	Facets        []string  `json:"facets"`
	Verdict       string    `json:"verdict"`
	CorrectionSet []Finding `json:"correction_set"`
	ForVerify     []Finding `json:"for_verify"`
	FollowUps     []Finding `json:"follow_ups"`
	Information   []Finding `json:"information"`
	Refuted       []Finding `json:"refuted"`
	Unavailable   []string  `json:"unavailable"`
}

// Reduce is the reduction of checked results.
func Reduce(rr ReviewResults) Reduction {
	out := Reduction{Facets: []string{}, CorrectionSet: []Finding{}, ForVerify: []Finding{}, FollowUps: []Finding{},
		Information: []Finding{}, Refuted: []Finding{}, Unavailable: []string{}}
	for _, l := range Facets {
		for _, r := range rr.Facets {
			if r.Facet == l {
				out.Facets = append(out.Facets, l)
				if r.Inspection.Status == "unavailable" {
					out.Unavailable = append(out.Unavailable, l+": "+flatLine(r.Inspection.Reason))
				}
			}
		}
	}
	outcome := map[string]string{}
	for _, v := range append(slices.Clone(rr.Challenger.Results), rr.Arbiter.Results...) {
		if outcome[v.FindingID] != "refuted" {
			outcome[v.FindingID] = v.Outcome
		}
	}
	for _, f := range AllFindings(rr.Facets) {
		switch {
		case outcome[f.ID] == "refuted":
			out.Refuted = append(out.Refuted, f)
		case f.CausalDisposition == "pre_existing":
			out.FollowUps = append(out.FollowUps, f)
		case severeFinding(f) && outcome[f.ID] == "confirmed":
			out.CorrectionSet = append(out.CorrectionSet, f)
		case severeFinding(f):
			out.ForVerify = append(out.ForVerify, f)
		default:
			out.Information = append(out.Information, f)
		}
	}
	out.Verdict = "pass"
	if len(out.CorrectionSet) > 0 || len(out.Unavailable) > 0 {
		out.Verdict = "fail"
	}
	return out
}

// flatLine is s on one line: a model's text never starts a record line.
func flatLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// RecordText is review-prism.md for change: the facets, the verdict, the
// subject and base it binds to, then the findings by where the reduction
// put them.
func RecordText(change, subject, base string, r Reduction) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Prism review: %s\n\nfacets: %s\nverdict: %s\nsubject: %s\nbase: %s\n", change, strings.Join(r.Facets, ", "), r.Verdict, subject, base)
	section := func(title string, fs []Finding) {
		if len(fs) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n## %s\n\n", title)
		for _, f := range fs {
			fmt.Fprintf(&b, "- [%s] %s %s: %s", f.ID, f.Severity, flatLine(f.Location), flatLine(f.Claim))
			if len(f.ProofRefs) > 0 {
				refs := make([]string, len(f.ProofRefs))
				for i, p := range f.ProofRefs {
					refs[i] = flatLine(p)
				}
				fmt.Fprintf(&b, " (proof: %s)", strings.Join(refs, "; "))
			}
			b.WriteString("\n")
		}
	}
	section("Correction set", r.CorrectionSet)
	section("For verify (unconfirmed)", r.ForVerify)
	section("Follow-ups (pre-existing)", r.FollowUps)
	section("Findings (information)", r.Information)
	section("Refuted", r.Refuted)
	if len(r.Unavailable) > 0 {
		b.WriteString("\n## Unavailable facets\n\n")
		for _, u := range r.Unavailable {
			fmt.Fprintf(&b, "- %s\n", u)
		}
	}
	return b.String()
}
