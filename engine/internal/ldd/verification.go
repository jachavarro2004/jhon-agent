// Package ldd holds the LDD records a change carries:
// status, context, risk, specs, and delivery.
package ldd

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"os"
	"path/filepath"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// This file is a change's independent verification on record
// (contracts/ldd.md, "Delivery"): its
// verify-report.md stating `verdict: pass`. It reads no vocabulary, so the
// audit loads even when the vocabulary is broken and validation must report
// that.

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

// Verification names a change's independent verification and the file that records it.
type Verification struct {
	By   string `json:"by"`
	File string `json:"file"`
}

// IndependentVerification is the change's independent verification, or nil
// when there is none.
func IndependentVerification(root, change string) *Verification {
	report := filepath.Join(layout.At(root, layout.For(root).ChangeDir(change)), "verify-report.md")
	if src, err := os.ReadFile(report); err == nil && ReportVerdict(string(src)) == "pass" {
		return &Verification{"verify-report.md (verdict: pass)", report}
	}
	return nil
}

// BranchVerified is whether a change the branch in the repository whose top
// level is top carries, active or archived, has its independent
// verification on record (IndependentVerification).
func BranchVerified(top string) bool {
	active, archived := BranchChanges(top)
	for _, c := range active {
		if IndependentVerification(top, c) != nil {
			return true
		}
	}
	for _, c := range archived {
		if IndependentVerification(top, filepath.Join("archive", c)) != nil {
			return true
		}
	}
	return false
}
