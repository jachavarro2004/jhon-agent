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

// This file is the native 4R (contracts/review-4r.md): the tool-less
// prompts `jhon review start`, `refute` and `validate` build from one
// frozen candidate, the shapes of the answers they ask for, and the
// reduction `jhon review record` writes to review-4r.md.

// PromptTokenCap is the estimated size past which a review prompt is
// refused: the slice is too large for one review.
const PromptTokenCap = 100000

// EstTokens is a prompt's estimated token count: its bytes over 3.5.
func EstTokens(bytes int) int { return int(float64(bytes) / 3.5) }

// ReviewFile is the 4R record's name in a change's directory.
const ReviewFile = "review-4r.md"

// ReviewExempt are the paths a review's subject leaves out: the change's
// own records (verify-report.md, the execution record, delivery.json) and
// its review-4r.md, all written after the review, relative to the git top
// level. Nil when change is "".
func ReviewExempt(top, change string) map[string]bool {
	if change == "" {
		return nil
	}
	out := execution.ChangeRecordPaths(top, change)
	out[path.Join(execution.ChangeDirInRepo(top, change), ReviewFile)] = true
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

// Finding is one lens finding (contracts/review-4r.md, "Findings").
type Finding struct {
	ID                string   `json:"id,omitempty"`
	Lens              string   `json:"lens,omitempty"`
	Location          string   `json:"location"`
	Severity          string   `json:"severity"`
	Claim             string   `json:"claim"`
	EvidenceClass     string   `json:"evidence_class,omitempty"`
	CausalDisposition string   `json:"causal_disposition,omitempty"`
	ProofRefs         []string `json:"proof_refs"`
}

// LensResult is one lens's answer.
type LensResult struct {
	SubjectHash string `json:"subject_hash"`
	Lens        string `json:"lens"`
	Inspection  struct {
		Status string   `json:"status"`
		Paths  []string `json:"paths"`
		Reason string   `json:"reason,omitempty"`
	} `json:"inspection"`
	Findings []Finding `json:"findings"`
	Evidence []string  `json:"evidence"`
}

// Verdict is the refuter's or the validator's answer on one finding.
type Verdict struct {
	FindingID string   `json:"finding_id"`
	Outcome   string   `json:"outcome"`
	Reason    string   `json:"reason"`
	ProofRefs []string `json:"proof_refs,omitempty"`
}

// RoleResult is the refuter's or the validator's answer.
type RoleResult struct {
	SubjectHash string    `json:"subject_hash"`
	Role        string    `json:"role"`
	Results     []Verdict `json:"results"`
}

// ReviewResults is what `jhon review validate` and `record` read: the lens
// results and the refuter's (and, for record, the validator's) answers.
type ReviewResults struct {
	Lenses    []LensResult `json:"lenses"`
	Refuter   *RoleResult  `json:"refuter"`
	Validator *RoleResult  `json:"validator"`
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

// ParseLensResults reads a JSON array of lens results.
func ParseLensResults(raw []byte) ([]LensResult, error) {
	var out []LensResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("the lens results are not a JSON array of lens results: %v", err)
	}
	return out, nil
}

// ParseReviewResults reads {"lenses": [...], "refuter": {...}, "validator": {...}}.
func ParseReviewResults(raw []byte) (ReviewResults, error) {
	var out ReviewResults
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("the results are not a JSON object {lenses, refuter, validator}: %v", err)
	}
	return out, nil
}

// CheckLenses validates the lens results against subject and gives each
// finding its id (<lens>-<n>, from 1) and lens; the error names the first
// problem.
func CheckLenses(results []LensResult, subject string) ([]LensResult, error) {
	if len(results) == 0 {
		return nil, errors.New("no lens result")
	}
	seen := map[string]bool{}
	out := slices.Clone(results)
	for i := range out {
		r := &out[i]
		where := fmt.Sprintf("lens result %d", i+1)
		switch {
		case !slices.Contains(Lenses, r.Lens):
			return nil, fmt.Errorf("%s: lens %q is not one of %s", where, r.Lens, strings.Join(Lenses, ", "))
		case seen[r.Lens]:
			return nil, fmt.Errorf("%s: lens %s answers twice", where, r.Lens)
		case !sameSubject(r.SubjectHash, subject):
			return nil, fmt.Errorf("%s (%s): subject_hash %q is not the frozen subject %s", where, r.Lens, r.SubjectHash, subject)
		case r.Inspection.Status != "completed" && r.Inspection.Status != "unavailable":
			return nil, fmt.Errorf("%s (%s): inspection.status must be completed or unavailable", where, r.Lens)
		case r.Inspection.Status == "unavailable" && (strings.TrimSpace(r.Inspection.Reason) == "" || len(r.Findings) > 0):
			return nil, fmt.Errorf("%s (%s): an unavailable inspection carries a reason and no findings", where, r.Lens)
		case r.Findings == nil:
			return nil, fmt.Errorf("%s (%s): findings must be an array", where, r.Lens)
		}
		seen[r.Lens] = true
		for n := range r.Findings {
			f := &r.Findings[n]
			f.ID, f.Lens = fmt.Sprintf("%s-%d", r.Lens, n+1), r.Lens
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

// AllFindings are the findings of the lens results, in order.
func AllFindings(results []LensResult) []Finding {
	var out []Finding
	for _, r := range results {
		out = append(out, r.Findings...)
	}
	return out
}

// CheckRole validates a refuter's (outcomes refuted, stands) or a
// validator's (confirmed, unconfirmed) answer: one verdict, with a reason,
// for each of ids and no other; a confirmation carries its proof_refs.
func CheckRole(r *RoleResult, role string, subject string, ids []string) error {
	if r == nil {
		return fmt.Errorf("no %s result", role)
	}
	outcomes := map[string][]string{"refuter": {"refuted", "stands"}, "validator": {"confirmed", "unconfirmed"}}[role]
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
			return fmt.Errorf("the validator confirms %s without proof_refs", v.FindingID)
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

// Standing are the findings the refuter did not refute.
func Standing(findings []Finding, refuter *RoleResult) []Finding {
	refuted := map[string]bool{}
	for _, v := range refuter.Results {
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

// Reduction is the 4R's reduction (contracts/review-4r.md, "Reduction"):
// the refuted findings drop; of those that stand, the pre-existing ones are
// follow-ups, the severe ones the validator confirms are the correction set
// and the severe ones it does not are noted for verify; the rest are
// information. The verdict passes with no correction set and no lens
// unavailable.
type Reduction struct {
	Lenses        []string  `json:"lenses"`
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
	out := Reduction{Lenses: []string{}, CorrectionSet: []Finding{}, ForVerify: []Finding{}, FollowUps: []Finding{},
		Information: []Finding{}, Refuted: []Finding{}, Unavailable: []string{}}
	for _, l := range Lenses {
		for _, r := range rr.Lenses {
			if r.Lens == l {
				out.Lenses = append(out.Lenses, l)
				if r.Inspection.Status == "unavailable" {
					out.Unavailable = append(out.Unavailable, l+": "+flatLine(r.Inspection.Reason))
				}
			}
		}
	}
	outcome := map[string]string{}
	for _, v := range append(slices.Clone(rr.Refuter.Results), rr.Validator.Results...) {
		if outcome[v.FindingID] != "refuted" {
			outcome[v.FindingID] = v.Outcome
		}
	}
	for _, f := range AllFindings(rr.Lenses) {
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

// RecordText is review-4r.md for change: the lenses, the verdict, the
// subject and base it binds to, then the findings by where the reduction
// put them.
func RecordText(change, subject, base string, r Reduction) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 4R: %s\n\nlenses: %s\nverdict: %s\nsubject: %s\nbase: %s\n", change, strings.Join(r.Lenses, ", "), r.Verdict, subject, base)
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
		b.WriteString("\n## Unavailable lenses\n\n")
		for _, u := range r.Unavailable {
			fmt.Fprintf(&b, "- %s\n", u)
		}
	}
	return b.String()
}
