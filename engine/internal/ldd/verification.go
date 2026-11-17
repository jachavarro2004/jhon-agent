// Package ldd holds the LDD records a change carries:
// status, context, risk, specs, and delivery.
package ldd

import (
	"encoding/json"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// This file is a change's independent verification on record
// (contracts/ldd.md, "Delivery"): its
// verify-report.md stating `verdict: pass`, or a Judgment Day record under
// its judgments/ closed APPROVED. It reads no vocabulary, so the audit loads
// even when the vocabulary is broken and validation must report that.
// `jhon deliver check` passes the full record validation; the audit reads
// the CLOSE event alone.

// The case-insensitive words are spelled out as classes: Go's (?i) also folds
// ſ to s and K (Kelvin) to k, JavaScript's i flag without u does not.
var verdictLine = lazyre.New(`^` + js.Space + `*(?:[-*]` + js.Space + `+)?(?:\*\*|__)?[vV][eE][rR][dD][iI][cC][tT](?:\*\*|__)?` +
	js.Space + `*:` + js.Space + "*(?:\\*\\*|__|`)?" + js.Space + `*([pP][aA][sS][sS]|[fF][aA][iI][lL])\b`)

// ReportVerdict is the verdict a verify-report.md states, lowercased: the
// first line that reads `verdict: pass` or `verdict: fail` (a YAML key, a
// bullet, or bold Markdown, in any case; `PASS WITH WARNINGS` is a pass), or
// "" when it states none. The report is plain Markdown; nothing else in it is
// parsed.
func ReportVerdict(src string) string {
	for _, line := range strings.Split(src, "\n") {
		if m := verdictLine.FindStringSubmatch(strings.TrimSuffix(line, "\r")); m != nil {
			return strings.ToLower(m[1])
		}
	}
	return ""
}

var closeEvent = lazyre.New("(?m)" + js.LineStart + "## CLOSE\n+```json\n([\\s\\S]*?)\n```")

// ClosedVerdict is the verdict of a Judgment Day record's CLOSE event,
// unvalidated; "" when it has none.
func ClosedVerdict(src string) string {
	m := closeEvent.FindStringSubmatch(src)
	if m == nil {
		return ""
	}
	var event any
	if json.Unmarshal([]byte(m[1]), &event) != nil {
		return ""
	}
	obj, _ := event.(map[string]any)
	v, _ := obj["verdict"].(string)
	return v
}

// A VerdictFunc reads a judgment record's verdict ("" for none); it may fail on an invalid record.
type VerdictFunc func(src string) (string, error)

// Verification names a change's independent verification and the file that records it.
type Verification struct {
	By   string `json:"by"`
	File string `json:"file"`
}

// IndependentVerification is the change's independent verification, or nil
// when there is none. verdictOf reads a judgment record (nil: ClosedVerdict);
// a record it fails on verifies nothing. The error is a judgments/ that
// cannot be listed.
func IndependentVerification(root, change string, verdictOf VerdictFunc) (*Verification, error) {
	dir := layout.At(root, layout.For(root).ChangeDir(change))
	report := filepath.Join(dir, "verify-report.md")
	if src, err := os.ReadFile(report); err == nil && ReportVerdict(string(src)) == "pass" {
		return &Verification{"verify-report.md (verdict: pass)", report}, nil
	}
	f, err := approvedJudgmentOf(dir, verdictOf)
	if f == "" {
		return nil, err
	}
	return &Verification{"judgments/" + f + " (APPROVED)", filepath.Join(dir, "judgments", f)}, nil
}

// approvedJudgment is the newest Judgment Day record under the change
// directory dir's judgments/ that verdictOf (nil: ClosedVerdict) reads
// APPROVED, by file name; "" when none is.
func approvedJudgment(dir string, verdictOf VerdictFunc) string {
	f, _ := approvedJudgmentOf(dir, verdictOf)
	return f
}

// approvedJudgmentOf is approvedJudgment; the error is a judgments/ that
// cannot be listed.
func approvedJudgmentOf(dir string, verdictOf VerdictFunc) (string, error) {
	if verdictOf == nil {
		verdictOf = func(src string) (string, error) { return ClosedVerdict(src), nil }
	}
	judgments := filepath.Join(dir, "judgments")
	if _, err := os.Stat(judgments); err != nil {
		return "", nil
	}
	entries, err := os.ReadDir(judgments)
	if err != nil {
		return "", err
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	slices.SortFunc(names, func(a, b string) int { return js.Compare(b, a) })
	for _, f := range names {
		src, err := os.ReadFile(filepath.Join(judgments, f))
		if err != nil {
			continue
		}
		if v, err := verdictOf(string(src)); err == nil && v == "APPROVED" {
			return f, nil
		}
	}
	return "", nil
}

// BranchVerified is whether a change the branch in the repository whose top
// level is top carries, active or archived, has its independent
// verification on record (IndependentVerification, judgments read by
// ClosedVerdict).
func BranchVerified(top string) bool {
	active, archived := BranchChanges(top)
	for _, c := range active {
		if v, _ := IndependentVerification(top, c, nil); v != nil {
			return true
		}
	}
	for _, c := range archived {
		if v, _ := IndependentVerification(top, filepath.Join("archive", c), nil); v != nil {
			return true
		}
	}
	return false
}
