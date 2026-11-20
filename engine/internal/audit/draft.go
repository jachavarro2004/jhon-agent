package audit

// `jhon audit --all --draft <dir>` (T55): the findings that repeat across
// sessions, grouped by what they are and never by where, each group drafted
// as a task file with its evidence cited: the maturation loop's steps 1-3,
// for the owner to decide in a Proposal / owner decision section the audit
// never writes.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
)

// DraftItem is one occurrence of a group: where it was read and what the audit cited.
type DraftItem struct {
	Session, Transcript string
	Line                int
	At, Kind, Severity  string
	Evidence, Message   string
}

// DraftGroup is the findings of one key: check, anchor, runtime, agent type, and cause.
type DraftGroup struct {
	Key, Check, Anchor, Status, Severity string
	Runtime, AgentType, Cause, Owner     string
	Sessions                             int
	First, Last                          string
	Items                                []DraftItem
}

// DraftKey is a group's key: the first 12 hex characters of the sha256 of its parts.
func DraftKey(check, anchor, runtime, agentType, cause string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{check, anchor, runtime, agentType, cause}, "\x00")))
	return hex.EncodeToString(sum[:])[:12]
}

var (
	causeDate   = lazyre.New(`\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?Z?)?`)
	causePath   = lazyre.New(`[\w.~@+-]*(?:/[\w.~@+-]*)+`)
	causeFile   = lazyre.New(`\b[\w-]+\.(?:jsonl|json|md|go|ts|js|mjs|py|txt|yaml|yml|sh)\b`)
	causeHex    = lazyre.New(`\b[0-9a-fA-F]{7,}\b`)
	causeNumber = lazyre.New(`\d+`)
	causeSpace  = lazyre.New(`\s+`)
)

// normalCause is message without what changes from one occurrence to the
// next: dates, paths, file names, hashes, and numbers.
func normalCause(message string) string {
	m := strings.TrimPrefix(message, WouldBeViolation+": ")
	m = causeDate.ReplaceAllString(m, "<date>")
	m = causePath.ReplaceAllString(m, "<path>")
	m = causeFile.ReplaceAllString(m, "<file>")
	m = causeHex.ReplaceAllString(m, "<id>")
	m = causeNumber.ReplaceAllString(m, "N")
	return strings.TrimSpace(causeSpace.ReplaceAllString(m, " "))
}

// Cause is a finding's normalised signature, the part of its key that says
// why: for a check whose message names a change or a path, the form it
// took; for any other, its message without paths, lines, dates, and ids.
func Cause(check, message string) string {
	m := strings.TrimPrefix(message, WouldBeViolation+": ")
	switch check {
	case "edit-before-red":
		form := "before-red"
		if strings.Contains(m, "before its record") {
			form = "before-record"
		}
		if strings.Contains(m, "ran by hand") {
			form += "+hand-run"
		}
		return form
	case "isolation-bypass":
		if strings.Contains(m, "outside its own tree") {
			return "outside-own-tree"
		}
		return "refused-then-other-root"
	case "test-weakened":
		if strings.Contains(m, "removes a test") {
			return "removed-test"
		}
		return "skip-marker"
	case "archive-without-red":
		switch {
		case strings.Contains(m, "no GREEN"):
			return "no-green"
		case strings.Contains(m, "GREEN is unknown"):
			return "green-unknown"
		}
		return "no-red-in-session"
	case "checker-wrote", "scout-wrote":
		return check
	}
	return normalCause(m)
}

// SuspectedOwner is who a group's fix most likely belongs to, by rule: an
// audit error or a group of indeterminate findings to the dashboard (the
// audit's reader), pi alone to the shell, Claude Code or both to the core.
func SuspectedOwner(check string, runtimes, kinds []string) string {
	indeterminate := len(kinds) > 0 && !slices.ContainsFunc(kinds, func(k string) bool { return k != "indeterminate" })
	switch {
	case check == "audit-error" || indeterminate:
		return "dashboard"
	case len(runtimes) > 0 && !slices.ContainsFunc(runtimes, func(r string) bool { return r != "pi" }):
		return "shell"
	}
	return "core"
}

// runtimeOf is the runtime of an audited session, from its path when the audit could not read it.
func runtimeOf(a Audited) string {
	if a.Result != nil && a.Result.Runtime != "" {
		return a.Result.Runtime
	}
	if IsPiSession(a.Path) {
		return "pi"
	}
	return "claude-code"
}

// GroupDrafts groups every finding and audit error of results by key and
// keeps the groups of at least min occurrences, in key order.
func GroupDrafts(results []Audited, min int) []DraftGroup {
	groups := map[string]*DraftGroup{}
	seen := map[string]map[string]bool{}
	add := func(a Audited, check, anchor, status, agentType, cause string, it DraftItem) {
		rt := runtimeOf(a)
		key := DraftKey(check, anchor, rt, agentType, cause)
		g := groups[key]
		if g == nil {
			g = &DraftGroup{Key: key, Check: check, Anchor: anchor, Status: status, Runtime: rt, AgentType: agentType, Cause: cause}
			groups[key], seen[key] = g, map[string]bool{}
		}
		if !filepath.IsAbs(it.Transcript) {
			it.Transcript = filepath.Join(filepath.Dir(a.Path), it.Transcript)
		}
		it.Session = a.Path
		g.Items = append(g.Items, it)
		if !seen[key][a.Path] {
			seen[key][a.Path] = true
			g.Sessions++
		}
	}
	for _, a := range results {
		if a.Result == nil {
			continue
		}
		for _, f := range a.Result.Findings {
			add(a, f.Check, f.Anchor, f.ValidationStatus, f.AgentType, Cause(f.Check, f.Message),
				DraftItem{Transcript: f.Transcript, Line: f.Line, At: f.At, Kind: f.Kind, Severity: f.Severity, Evidence: f.Evidence, Message: strings.TrimPrefix(f.Message, WouldBeViolation+": ")})
		}
		for _, e := range a.Result.Errors {
			add(a, "audit-error", "", "", "", normalCause(e.Message),
				DraftItem{Transcript: e.Transcript, Line: e.Line, Kind: "audit-error", Severity: "audit-error", Message: e.Message})
		}
	}
	var out []DraftGroup
	for _, key := range slices.Sorted(func(yield func(string) bool) {
		for k := range groups {
			if !yield(k) {
				return
			}
		}
	}) {
		g := groups[key]
		if len(g.Items) < min {
			continue
		}
		slices.SortStableFunc(g.Items, func(x, y DraftItem) int {
			if c := strings.Compare(x.At, y.At); c != 0 {
				return c
			}
			if c := strings.Compare(x.Transcript, y.Transcript); c != 0 {
				return c
			}
			return x.Line - y.Line
		})
		var kinds []string
		g.Severity = "suspect"
		for _, it := range g.Items {
			kinds = append(kinds, it.Kind)
			if it.Severity == "violation" || it.Severity == "audit-error" {
				g.Severity = it.Severity
			}
			if it.At != "" && (g.First == "" || it.At < g.First) {
				g.First = it.At
			}
			if it.At > g.Last {
				g.Last = it.At
			}
		}
		g.Owner = SuspectedOwner(g.Check, []string{g.Runtime}, kinds)
		out = append(out, *g)
	}
	return out
}

// FileName is the group's draft file: `<check>-<key>.md`.
func (g DraftGroup) FileName() string { return g.Check + "-" + g.Key + ".md" }

// frontMatter is the draft's front matter; its counts are what --refresh rewrites.
func (g DraftGroup) frontMatter() string {
	or := func(s string) string {
		if s == "" {
			return "-"
		}
		return s
	}
	return fmt.Sprintf("---\nkey: %s\ncheck: %s\nanchor: %s\nseverity: %s\nruntime: %s\nagent_type: %s\nsessions: %d\noccurrences: %d\nfirst: %s\nlast: %s\nowner: %s\n---\n",
		g.Key, g.Check, or(g.Anchor), g.Severity, g.Runtime, or(g.AgentType), g.Sessions, len(g.Items), or(g.First), or(g.Last), g.Owner)
}

// evidence is the draft's Evidence section, heading included.
func (g DraftGroup) evidence() string {
	var b strings.Builder
	b.WriteString("## Evidence\n\n")
	for _, it := range g.Items {
		cite := it.Message
		if it.Evidence != "" {
			cite = it.Evidence + ": " + it.Message
		}
		at := it.Severity
		if it.At != "" {
			at = it.At + ", " + it.Severity
		}
		fmt.Fprintf(&b, "- %s:%d (%s) %s\n", it.Transcript, it.Line, at, cite)
	}
	return b.String()
}

// Markdown is the group's draft.
func (g DraftGroup) Markdown() string {
	who := strings.TrimSpace(g.Runtime + " " + g.AgentType)
	status := g.Anchor
	if g.Status != "" {
		status += ", " + g.Status
	}
	finding := fmt.Sprintf("%s (%s) in %s sessions, %d time(s) in %d session(s). Cause: %s", g.Check, status, who, len(g.Items), g.Sessions, g.Cause)
	if g.Anchor == "" {
		finding = fmt.Sprintf("%s in %s sessions, %d time(s) in %d session(s). Cause: %s", g.Check, who, len(g.Items), g.Sessions, g.Cause)
	}
	return g.frontMatter() + "\n# " + g.Check + ": " + who + "\n\n## Finding\n\n" + finding + "\n\nExample: " + g.Items[0].Message + "\n\n" +
		g.evidence() + "\n## Suspected owner\n\n" + g.Owner +
		" (by rule: pi only → shell; Claude Code or both → core; audit-error or indeterminate → dashboard)\n\n## Proposal / owner decision\n\n"
}

// DraftOutcome is what WriteDrafts did with each group's file.
type DraftOutcome struct{ Written, Refreshed, Kept []string }

// WriteDrafts writes each group's draft under dir. An existing draft is
// kept as it is; with refresh, only its front matter's counts and its
// Evidence section are rewritten, every other byte kept.
func WriteDrafts(dir string, groups []DraftGroup, refresh bool) (DraftOutcome, error) {
	var out DraftOutcome
	if len(groups) == 0 {
		return out, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return out, err
	}
	for _, g := range groups {
		path := filepath.Join(dir, g.FileName())
		old, err := os.ReadFile(path)
		switch {
		case os.IsNotExist(err):
			if err := os.WriteFile(path, []byte(g.Markdown()), 0o600); err != nil {
				return out, err
			}
			out.Written = append(out.Written, path)
		case err != nil:
			return out, err
		case !refresh:
			out.Kept = append(out.Kept, path)
		default:
			next, ok := refreshed(string(old), g)
			if !ok {
				out.Kept = append(out.Kept, path)
				continue
			}
			if next != string(old) {
				if err := os.WriteFile(path, []byte(next), 0o600); err != nil {
					return out, err
				}
			}
			out.Refreshed = append(out.Refreshed, path)
		}
	}
	return out, nil
}

// refreshedFields are the front matter's lines --refresh rewrites.
var refreshedFields = []string{"severity", "sessions", "occurrences", "first", "last"}

// refreshed is draft with g's counts and Evidence; ok is false when the
// draft has no front matter or no Evidence section to rewrite.
func refreshed(draft string, g DraftGroup) (string, bool) {
	fresh := map[string]string{}
	for _, line := range strings.Split(g.frontMatter(), "\n") {
		if k, _, ok := strings.Cut(line, ": "); ok {
			fresh[k] = line
		}
	}
	end := strings.Index(draft, "\n---\n")
	if !strings.HasPrefix(draft, "---\n") || end < 0 {
		return "", false
	}
	lines := strings.Split(draft[:end], "\n")
	for i, line := range lines {
		if k, _, ok := strings.Cut(line, ": "); ok && slices.Contains(refreshedFields, k) {
			lines[i] = fresh[k]
		}
	}
	draft = strings.Join(lines, "\n") + draft[end:]
	start := strings.Index(draft, "\n## Evidence\n")
	if start < 0 {
		return "", false
	}
	start++
	stop := strings.Index(draft[start+1:], "\n## ")
	if stop < 0 {
		return "", false
	}
	stop += start + 2
	return draft[:start] + g.evidence() + "\n" + draft[stop:], true
}
