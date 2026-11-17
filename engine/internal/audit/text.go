package audit

import (
	"github.com/jachavarro2004/jhon-agent/engine/internal/lazyre"
	"math"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/jachavarro2004/jhon-agent/engine/internal/isodate"
	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// The JavaScript string, path, and date behaviour the audit's records rely on.

var (
	space = js.Space
	// nonSpace is JavaScript's \S.
	nonSpace = `[^` + js.SpaceChars + `]`
	spaces   = lazyre.New(space + `+`)
)

// u16 is s as UTF-16 code units, the indices String.prototype.slice counts.
func u16(s string) []uint16 { return utf16.Encode([]rune(s)) }

// slice16 is s.slice(from, to) for 0 <= from: indices in UTF-16 code units.
func slice16(s string, from, to int) string {
	u := u16(s)
	to = min(max(to, 0), len(u))
	if from >= to {
		return ""
	}
	return string(utf16.Decode(u[from:to]))
}

// index16 is the UTF-16 index of byte offset i of s.
func index16(s string, i int) int { return js.Len(s[:i]) }

// short is `s`, single-lined and cut to n characters.
func short(s string, n int) string {
	one := js.Trim(spaces.ReplaceAllString(s, " "))
	if js.Len(one) > n {
		return slice16(one, 0, n-1) + "…"
	}
	return one
}

// ci is a regexp source written in lower case made case-insensitive the way
// JavaScript's i flag is: each letter outside a class becomes a class of its
// simple case folds, without Go's (?i) folds of ſ to s and K (Kelvin) to k.
func ci(pattern string) string {
	var b strings.Builder
	inClass := false
	for i := 0; i < len(pattern); {
		r, n := utf8.DecodeRuneInString(pattern[i:])
		switch {
		case r == '\\':
			_, m := utf8.DecodeRuneInString(pattern[i+1:])
			b.WriteString(pattern[i : i+1+m])
			i += 1 + m
			continue
		case r == '[':
			inClass = true
		case r == ']':
			inClass = false
		case !inClass && unicode.IsLetter(r):
			b.WriteString("[" + string(r))
			for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
				if f != 'ſ' && f != 'K' {
					b.WriteRune(f)
				}
			}
			b.WriteString("]")
			i += n
			continue
		}
		b.WriteString(pattern[i : i+n])
		i += n
	}
	return b.String()
}

// dateParse is Date.parse for the ISO 8601 forms transcripts and records
// write: milliseconds since the epoch, NaN when unreadable.
func dateParse(s string) float64 {
	if t, ok := isodate.Parse(s); ok {
		return float64(t.UnixMilli())
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return float64(t.UnixMilli())
	}
	return math.NaN()
}

// iso is new Date(ms).toISOString().
func iso(ms float64) string { return isodate.Format(time.UnixMilli(int64(ms)).UTC()) }

// baseName is node's path.basename(p): trailing slashes aside.
func baseName(p string) string {
	p = strings.TrimRight(p, "/")
	return p[strings.LastIndexByte(p, '/')+1:]
}

// normalize is node's path.posix.normalize: `.` and `..` resolved,
// duplicate slashes collapsed, a trailing slash kept.
func normalize(p string) string {
	if p == "" {
		return "."
	}
	trailing := strings.HasSuffix(p, "/")
	c := path.Clean(p)
	switch {
	case c == "/":
		return "/"
	case c == ".":
		if trailing {
			return "./"
		}
		return "."
	case trailing:
		return c + "/"
	}
	return c
}

// join is node's path.posix.join: the non-empty parts joined, normalized.
func join(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		return "."
	}
	return normalize(strings.Join(kept, "/"))
}

// normalizeRoot is a root normalized lexically: `.` and `..` resolved,
// duplicate slashes collapsed, a trailing slash stripped. No filesystem I/O.
func normalizeRoot(root string) string {
	n := normalize(root)
	if len(n) > 1 && strings.HasSuffix(n, "/") {
		return n[:len(n)-1]
	}
	return n
}

// absolute is a path as written, resolved against a working directory ("" for none) and normalised.
func absolute(p, cwd string) string {
	if strings.HasPrefix(p, "/") {
		return normalizeRoot(p)
	}
	if cwd == "" {
		cwd = "/"
	}
	return normalizeRoot(join(cwd, p))
}

// strProp is v[k] when it is a string.
func strProp(v any, k string) (string, bool) {
	s, ok := prop(v, k).(string)
	return s, ok
}

// has is v[k] !== undefined.
func has(v any, k string) bool {
	_, undef := prop(v, k).(js.Undefined)
	return !undef
}

// isTrue is v[k] === true.
func isTrue(v any, k string) bool {
	b, ok := prop(v, k).(bool)
	return ok && b
}

// count is a finite number, else 0.
func count(v any) float64 {
	if f, ok := v.(float64); ok && !math.IsInf(f, 0) && !math.IsNaN(f) {
		return f
	}
	return 0
}

// jsonObject is a command output read as one JSON object: the whole text,
// or its outermost braces; nil when neither is one.
func jsonObject(text string) *js.Object {
	tries := []string{js.Trim(text)}
	if a, b := strings.IndexByte(text, '{'), strings.LastIndexByte(text, '}'); a >= 0 && b > a {
		tries = append(tries, text[a:b+1])
	}
	for _, t := range tries {
		if v, err := js.ParseJSON(t); err == nil {
			if o, ok := v.(*js.Object); ok {
				return o
			}
		}
	}
	return nil
}

// orString is String(v ?? fallback).
func orString(v any, fallback string) string {
	if js.Nullish(v) {
		return fallback
	}
	return js.String(v)
}

// indexOf is Array.prototype.indexOf for strings.
func indexOf(xs []string, x string) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return -1
}

// at is xs[i], or "" past either end (undefined, read where it compares unequal to any word).
func at(xs []string, i int) (string, bool) {
	if i < 0 || i >= len(xs) {
		return "", false
	}
	return xs[i], true
}

// uniq is [...new Set(xs)].
func uniq(xs []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// prop is v[k] (js.Prop), a nil *js.Object read as undefined.
func prop(v any, k string) any {
	if o, ok := v.(*js.Object); ok && o == nil {
		return js.Undefined{}
	}
	return js.Prop(v, k)
}

// propList is v[k] when it is an array.
func propList(v any, k string) ([]any, bool) {
	l, ok := prop(v, k).([]any)
	return l, ok
}

// failedIDs is the ids of the tool calls whose result is an error.
func failedIDs(entries []*Entry) map[string]bool {
	out := map[string]bool{}
	for _, e := range entries {
		for _, r := range e.ToolResults {
			if r.IsError {
				out[r.ID] = true
			}
		}
	}
	return out
}

// lastText is the text of an agent's last assistant entry with text, and that entry; nil for none.
func lastText(entries []*Entry) (string, *Entry) {
	for i := len(entries) - 1; i >= 0; i-- {
		if e := entries[i]; e.Role == "assistant" && len(e.Texts) > 0 {
			return strings.Join(e.Texts, "\n"), e
		}
	}
	return "", nil
}
