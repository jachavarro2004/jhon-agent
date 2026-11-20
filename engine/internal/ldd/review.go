package ldd

import (
	"fmt"
	"github.com/jachavarro2004/jhon-agent/engine/internal/ldd/layout"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/jachavarro2004/jhon-agent/engine/internal/contract"
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"github.com/jachavarro2004/jhon-agent/engine/internal/project"
	"github.com/jachavarro2004/jhon-agent/engine/internal/vcs"
)

// This file is the reviews a change owes before delivery (contracts/ldd.md,
// "Delivery"), each with whether it is on record:
//
//   - verify, on the verified route: verify-report.md (`verdict: pass`)
//     (IndependentVerification);
//   - 4R, when the change's risk reaches the verified tier (not by size
//     alone), with only the lenses the change touches: review-4r.md naming
//     them on a `lenses:` line and stating `verdict: pass`
//     (contracts/review-4r.md);
//   - what `review_required` in openspec/config.yaml names for the change,
//     by its name or by an area (a path glob) its diff touches: verify or
//     4r. It always wins over the route.

// The reviews, in the order a change owes them.
const (
	ReviewVerify = "verify"
	ReviewFourR  = "4r"
	// ReviewUnreadable is a review_required entry jhon cannot read: never
	// done, it holds the PR until it is fixed.
	ReviewUnreadable = "review_required"
)

var reviewKinds = []string{ReviewVerify, ReviewFourR}

// Lenses are the 4R lenses a review-4r.md may name.
var Lenses = []string{"risk", "resilience", "reliability", "readability"}

// Review is one review a change owes: why (By), and whether it is on record.
type Review struct {
	Review string   `json:"review"`
	By     []string `json:"by"`
	Done   bool     `json:"done"`
	// Record is what holds it, when Done.
	Record string `json:"record,omitempty"`
	// Why is why an unreadable entry cannot be read (ReviewUnreadable,
	// whose By is its line).
	Why string `json:"why,omitempty"`
}

// Line is the review as status reads it: due and how, or done and by what.
func (v Review) Line() string {
	if v.Review == ReviewUnreadable {
		return "review_required in openspec/config.yaml has an entry jhon cannot read (" + strings.Join(v.By, "; ") + "): " + v.Why + "; fix it before the PR"
	}
	name := map[string]string{ReviewVerify: "verify", ReviewFourR: "4R"}[v.Review]
	why := strings.Join(v.By, "; ")
	if v.Done {
		return name + " done (" + why + "): " + v.Record
	}
	return name + " due (" + why + "): " + map[string]string{
		ReviewVerify: "launch the verify agent and record verify-report.md with `verdict: pass`",
		ReviewFourR:  "pick the pertinent lenses (" + strings.Join(Lenses, ", ") + ") by what the change touches and record them in review-4r.md with `lenses:` and `verdict: pass`",
	}[v.Review]
}

var lensesLine = lazyre.New(`(?m)^\s*(?:[-*]\s+)?(?:\*\*|__)?lenses(?:\*\*|__)?\s*:[ \t]*(.*)$`)

var (
	subjectLine = lazyre.New(`(?m)^subject:[ \t]*(\S*)[ \t]*\r?$`)
	baseLine    = lazyre.New(`(?m)^base:[ \t]*(\S*)[ \t]*\r?$`)
)

// FourRDone is what holds the change's 4R, "" when nothing does: its
// review-4r.md naming at least one lens, only the four, and stating
// `verdict: pass` (ReportVerdict). A
// record with a `subject:` line (`jhon review record`) holds only while a
// fresh freeze against its `base:` gives that subject: a candidate changed
// since the review is a stale record. One without it is read as before.
func FourRDone(root, change string) string {
	dir := layout.At(root, layout.For(root).ChangeDir(change))
	if src, err := os.ReadFile(filepath.Join(dir, ReviewFile)); err == nil && ReportVerdict(string(src)) == "pass" && subjectHolds(root, change, string(src)) {
		if m := lensesLine.FindStringSubmatch(string(src)); m != nil {
			named := contract.InlineList(m[1])
			if named == nil {
				named = strings.FieldsFunc(m[1], func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\r' })
			}
			if len(named) > 0 && !slices.ContainsFunc(named, func(l string) bool { return !slices.Contains(Lenses, l) }) {
				return "review-4r.md (lenses: " + strings.Join(named, ", ") + "; verdict: pass)"
			}
		}
	}
	return ""
}

// subjectHolds is whether a 4R record's subject, when it states one, is the
// subject of a fresh freeze of the change against the record's base.
func subjectHolds(root, change, src string) bool {
	m := subjectLine.FindStringSubmatch(src)
	if m == nil {
		return true
	}
	b := baseLine.FindStringSubmatch(src)
	if b == nil || m[1] == "" || b[1] == "" {
		return false
	}
	f, err := vcs.FreezeExempt(root, b[1], ReviewExempt(root, change))
	return err == nil && f.Subject == m[1]
}

// Required is one `review_required` entry: a change (by name) or an area
// (a path glob), and the reviews it requires.
type Required struct {
	Change  string
	Area    string
	Reviews []string
}

// Unreadable is a `review_required` line jhon cannot read: its number in
// openspec/config.yaml, its text, and why. It is never ignored: status
// reports it and the PR waits until it is fixed.
type Unreadable struct {
	Line int
	Text string
	Why  string
}

// RequiredReviews is the `review_required` entries of an
// openspec/config.yaml that jhon can read (ReadRequired).
func RequiredReviews(config string) []Required {
	out, _ := ReadRequired(config)
	return out
}

// ReadRequired reads the `review_required` block of an openspec/config.yaml:
// a list (at any consistent indentation, column 0 too) of mappings, block
// or flow (`{change: c1, review: 4r}`), each naming a `change` or an `area`
// and its `review`: verify or 4r in any case, as a scalar, a flow list
// (`[verify, 4r]`) or a block list. Values may be quoted and lines carry
// `# comments`. A line it cannot read, an entry naming neither a change nor
// an area, or no review or another one, is unreadable: it fails closed.
func ReadRequired(config string) (out []Required, bad []Unreadable) {
	lines := strings.Split(strings.ReplaceAll(config, "\r\n", "\n"), "\n")
	key := -1
	for i, l := range lines {
		if k, v, ok := strings.Cut(yamlComment(l), ":"); ok && strings.TrimRight(k, " \t") == "review_required" {
			key = i
			if v = strings.TrimSpace(v); v != "" {
				if strings.HasPrefix(v, "[") {
					return flowEntries(v, i+1, strings.TrimSpace(l))
				}
				return nil, []Unreadable{{i + 1, strings.TrimSpace(l), "it is not a list of entries"}}
			}
			break
		}
	}
	if key < 0 {
		return nil, nil
	}
	type item struct {
		line  int
		text  string
		head  string // what follows `- ` on the item's first line
		col   int    // where head starts
		lines []int  // the item's further lines
	}
	var items []item
	itemCol := -1
	for i := key + 1; i < len(lines); i++ {
		raw := lines[i]
		text := strings.TrimSpace(yamlComment(raw))
		if text == "" {
			continue
		}
		col := len(raw) - len(strings.TrimLeft(raw, " \t"))
		dash := text == "-" || strings.HasPrefix(text, "- ")
		if col == 0 && !dash {
			break // the next key
		}
		switch {
		case itemCol < 0 && dash, col == itemCol && dash:
			itemCol = col
			head := strings.TrimSpace(text[1:])
			items = append(items, item{line: i + 1, text: strings.TrimSpace(raw), head: head, col: col + len(text) - len(head)})
		case itemCol >= 0 && col > itemCol && len(items) > 0:
			items[len(items)-1].lines = append(items[len(items)-1].lines, i)
		default:
			bad = append(bad, Unreadable{i + 1, strings.TrimSpace(raw), "it is not an entry of the list (`- change: ...` or `- area: ...`)"})
		}
	}
	for _, it := range items {
		fields := map[string][]value{}
		readable := true
		if strings.HasPrefix(it.head, "{") {
			m, ok := flowMapping(it.head, it.line, it.text)
			if !ok {
				bad = append(bad, Unreadable{it.line, it.text, "the flow mapping does not close"})
				continue
			}
			fields = m
			for _, i := range it.lines {
				bad = append(bad, Unreadable{i + 1, strings.TrimSpace(lines[i]), "it follows a flow mapping"})
				readable = false
			}
		} else {
			// A block mapping: keys at one column, a block list under a key
			// left empty.
			type kv struct {
				n          int
				text, body string
				col        int
			}
			var body []kv
			if it.head != "" {
				body = append(body, kv{it.line, it.text, it.head, it.col})
			}
			for _, i := range it.lines {
				raw := lines[i]
				body = append(body, kv{i + 1, strings.TrimSpace(raw), strings.TrimSpace(yamlComment(raw)), len(raw) - len(strings.TrimLeft(raw, " \t"))})
			}
			keyCol, open := -1, ""
			for _, b := range body {
				if keyCol < 0 {
					keyCol = b.col
				}
				if open != "" && b.col >= keyCol && (b.body == "-" || strings.HasPrefix(b.body, "- ")) {
					fields[open] = append(fields[open], value{unquote(strings.TrimSpace(b.body[1:])), b.n, b.text})
					continue
				}
				k, v, ok := strings.Cut(b.body, ":")
				k = strings.TrimSpace(k)
				if _, seen := fields[k]; !ok || b.col != keyCol || seen || !slices.Contains([]string{"change", "area", "review"}, k) {
					bad = append(bad, Unreadable{b.n, b.text, "it is not `change:`, `area:`, or `review:` of an entry"})
					readable = false
					continue
				}
				open = ""
				switch v = strings.TrimSpace(v); {
				case v == "":
					fields[k], open = []value{}, k
				case strings.HasPrefix(v, "["):
					list, ok := flowList(v, b.n, b.text)
					if !ok {
						bad = append(bad, Unreadable{b.n, b.text, "the flow list does not close"})
						readable = false
						continue
					}
					fields[k] = list
				default:
					fields[k] = []value{{unquote(v), b.n, b.text}}
				}
			}
		}
		if !readable {
			continue
		}
		if r, u := requiredOf(fields, it.line, it.text); u != nil {
			bad = append(bad, *u)
		} else {
			out = append(out, r)
		}
	}
	return out, bad
}

// value is a scalar of an entry and the line that holds it.
type value struct {
	s    string
	n    int
	text string
}

// requiredOf is the entry fields name, or why it is unreadable; the entry
// starts at line n (text).
func requiredOf(fields map[string][]value, n int, text string) (Required, *Unreadable) {
	one := func(k string) string {
		if len(fields[k]) == 1 {
			return fields[k][0].s
		}
		return ""
	}
	r := Required{Change: one("change"), Area: one("area")}
	if r.Change == "" && r.Area == "" {
		return r, &Unreadable{n, text, "the entry names neither `change` nor `area`"}
	}
	if len(fields["review"]) == 0 {
		return r, &Unreadable{n, text, "the entry names no review"}
	}
	for _, v := range fields["review"] {
		kind := strings.ToLower(v.s)
		if !slices.Contains(reviewKinds, kind) {
			return r, &Unreadable{v.n, v.text, "`" + v.s + "` is not a review: verify or 4r"}
		}
		if !slices.Contains(r.Reviews, kind) {
			r.Reviews = append(r.Reviews, kind)
		}
	}
	return r, nil
}

// flowEntries is `review_required: [{...}, ...]` on line n (text).
func flowEntries(v string, n int, text string) (out []Required, bad []Unreadable) {
	inner, opens := strings.CutPrefix(v, "[")
	inner, closes := strings.CutSuffix(inner, "]")
	if !opens || !closes {
		return nil, []Unreadable{{n, text, "the flow list does not open and close"}}
	}
	for _, part := range splitFlow(inner) {
		m, ok := flowMapping(part, n, text)
		if !ok {
			return nil, []Unreadable{{n, text, "an entry is not a flow mapping (`{change: c1, review: 4r}`)"}}
		}
		r, u := requiredOf(m, n, text)
		if u != nil {
			return nil, []Unreadable{*u}
		}
		out = append(out, r)
	}
	return out, nil
}

// flowMapping reads `{change: c1, review: [verify, 4r]}` on line n (text);
// ok is false when it does not open and close, or holds what is not `key: value`.
func flowMapping(v string, n int, text string) (map[string][]value, bool) {
	inner, opens := strings.CutPrefix(strings.TrimSpace(v), "{")
	inner, closes := strings.CutSuffix(inner, "}")
	if !opens || !closes {
		return nil, false
	}
	m := map[string][]value{}
	for _, part := range splitFlow(inner) {
		k, val, ok := strings.Cut(part, ":")
		if !ok {
			return nil, false
		}
		k, val = strings.TrimSpace(k), strings.TrimSpace(val)
		if _, seen := m[k]; seen || !slices.Contains([]string{"change", "area", "review"}, k) {
			return nil, false
		}
		if strings.HasPrefix(val, "[") {
			list, ok := flowList(val, n, text)
			if !ok {
				return nil, false
			}
			m[k] = list
		} else if val != "" {
			m[k] = []value{{unquote(val), n, text}}
		}
	}
	return m, true
}

// flowList reads `[verify, "4r"]` on line n (text).
func flowList(v string, n int, text string) ([]value, bool) {
	inner, opens := strings.CutPrefix(strings.TrimSpace(v), "[")
	inner, closes := strings.CutSuffix(inner, "]")
	if !opens || !closes {
		return nil, false
	}
	out := []value{}
	for _, part := range splitFlow(inner) {
		if strings.ContainsAny(part, "[]{}") {
			return nil, false
		}
		out = append(out, value{unquote(part), n, text})
	}
	return out, true
}

// splitFlow splits a flow collection's inside at its top-level commas,
// outside quotes; empty parts drop.
func splitFlow(s string) []string {
	var out []string
	depth, quote, from := 0, byte(0), 0
	for i := 0; i <= len(s); i++ {
		if i < len(s) {
			c := s[i]
			switch {
			case quote != 0:
				if c == quote {
					quote = 0
				}
				continue
			case c == '"' || c == '\'':
				quote = c
				continue
			case c == '[' || c == '{':
				depth++
				continue
			case c == ']' || c == '}':
				depth--
				continue
			case c != ',' || depth != 0:
				continue
			}
		}
		if part := strings.TrimSpace(s[from:i]); part != "" {
			out = append(out, part)
		}
		from = i + 1
	}
	return out
}

// yamlComment is line without its `# comment`: a # at its start or after
// a space, outside quotes.
func yamlComment(line string) string {
	quote := byte(0)
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return line[:i]
		}
	}
	return line
}

// unquote is a scalar without its quotes.
func unquote(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// AreaMatches is whether path (repository-relative, slash-separated) lies
// in area: a glob whose `**` is any run of characters, `*` any but `/`, and
// `?` one but `/`, matching the path or a directory holding it.
func AreaMatches(area, path string) bool {
	area = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(area, "./"), "/"), "/")
	var re strings.Builder
	for i := 0; i < len(area); i++ {
		switch {
		case strings.HasPrefix(area[i:], "**/"):
			re.WriteString(`(?:.*/)?`)
			i += 2
		case strings.HasPrefix(area[i:], "**"):
			re.WriteString(`.*`)
			i++
		case area[i] == '*':
			re.WriteString(`[^/]*`)
		case area[i] == '?':
			re.WriteString(`[^/]`)
		default:
			re.WriteString(regexp.QuoteMeta(area[i : i+1]))
		}
	}
	m, err := regexp.MatchString(`^`+re.String()+`(?:/.*)?$`, path)
	return err == nil && m
}

// ReviewsOf is the reviews the route r and the project's review_required
// ask of changes (directories under openspec/changes/ of the repository
// whose top level is top, an archived one as archive/<name>), in the order
// verify, 4r; each is done when one of the changes has it on record.
// Each review_required entry jhon cannot read comes first, as a
// ReviewUnreadable that is never done: it may name any change.
func ReviewsOf(top string, changes []string, r Route) []Review {
	by := map[string][]string{}
	if r.Level == RouteVerified {
		by[ReviewVerify] = append(by[ReviewVerify], "route verified")
	}
	if rank(r.Tier) >= 0 && rank(r.Tier) >= rank(r.VerifiedTier) {
		by[ReviewFourR] = append(by[ReviewFourR], "risk "+string(r.Tier))
	}
	config := ""
	if b, err := project.ReadLddConfig(top); err == nil {
		config = b
	}
	required, bad := ReadRequired(config)
	var out []Review
	for _, u := range bad {
		out = append(out, Review{Review: ReviewUnreadable, By: []string{fmt.Sprintf("line %d: %s", u.Line, u.Text)}, Why: u.Why})
	}
	for _, q := range required {
		why := ""
		switch {
		case q.Change != "" && slices.ContainsFunc(changes, func(c string) bool { return filepath.Base(c) == q.Change }):
			why = "review_required: change " + q.Change
		case q.Area != "" && slices.ContainsFunc(r.files, func(f string) bool { return AreaMatches(q.Area, f) }):
			why = "review_required: area " + q.Area
		default:
			continue
		}
		for _, kind := range q.Reviews {
			if !slices.Contains(by[kind], why) {
				by[kind] = append(by[kind], why)
			}
		}
	}
	for _, kind := range reviewKinds {
		if by[kind] == nil {
			continue
		}
		v := Review{Review: kind, By: by[kind]}
		for _, c := range changes {
			if v.Record = recordOf(top, c, kind); v.Record != "" {
				v.Done = true
				break
			}
		}
		out = append(out, v)
	}
	return out
}

// recordOf is what holds review kind for change, "" when nothing does.
func recordOf(top, change, kind string) string {
	switch kind {
	case ReviewVerify:
		if v := IndependentVerification(top, change); v != nil {
			return v.By
		}
	case ReviewFourR:
		return FourRDone(top, change)
	}
	return ""
}
