// Package js reproduces the JavaScript semantics Jhon's records and outputs
// depend on where Go's differ: what \s, `.`, and String.prototype.trim
// count as whitespace and line terminators, string order and length in
// UTF-16 code units, and the text JSON.stringify(v, null, 2) prints.
package js

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf16"
)

// SpaceChars is the content of a regexp class for JavaScript's \s (and the
// characters trim removes): Go's \s lacks \v and every non-ASCII space.
const SpaceChars = `\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

// Space is JavaScript's \s as a Go regexp class.
const Space = `[` + SpaceChars + `]`

// Dot is JavaScript's `.` without the s flag: anything but a line terminator.
const Dot = `[^\n\r\x{2028}\x{2029}]`

// LineStart is JavaScript's ^ under the m flag, for a pattern compiled with
// (?m): a line also starts after \r, U+2028, or U+2029, which this consumes.
const LineStart = `(?:^|[\r\x{2028}\x{2029}])`

// IsSpace is whether r is JavaScript whitespace or a line terminator.
func IsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// Trim is String.prototype.trim.
func Trim(s string) string { return strings.TrimFunc(s, IsSpace) }

// Len is a string's length in UTF-16 code units, as String.prototype.length.
func Len(s string) int { return len(utf16.Encode([]rune(s))) }

// PadEnd is String.prototype.padEnd with spaces.
func PadEnd(s string, n int) string {
	if l := Len(s); l < n {
		return s + strings.Repeat(" ", n-l)
	}
	return s
}

// Compare orders strings as JavaScript compares them: by UTF-16 code units,
// the order Array.prototype.sort gives without a comparator. For slices.SortFunc.
func Compare(a, b string) int {
	x, y := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(x) && i < len(y); i++ {
		if x[i] != y[i] {
			return int(x[i]) - int(y[i])
		}
	}
	return len(x) - len(y)
}

// Stringify is JSON.stringify(v, null, 2): two-space indent, no HTML
// escaping, U+2028 and U+2029 written as themselves. A struct's json tags
// give the keys in their declared order.
func Stringify(v any) (string, error) { return stringify(v, "  ") }

// StringifyCompact is JSON.stringify(v): Stringify without whitespace.
func StringifyCompact(v any) (string, error) { return stringify(v, "") }

func stringify(v any, indent string) (string, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if indent != "" {
		enc.SetIndent("", indent)
	}
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return unescapeSeparators(strings.TrimSuffix(b.String(), "\n")), nil
}

// unescapeSeparators turns encoding/json's \u2028 and \u2029 escapes back into the
// characters, leaving an escaped backslash followed by "u2028" alone.
func unescapeSeparators(s string) string {
	if !strings.Contains(s, `\u202`) {
		return s
	}
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			out.WriteByte(s[i])
			continue
		}
		if rest := s[i:]; strings.HasPrefix(rest, `\u2028`) || strings.HasPrefix(rest, `\u2029`) {
			out.WriteRune(rune(0x2028 + int(rest[5]-'8')))
			i += 5
			continue
		}
		out.WriteString(s[i : i+2])
		i++
	}
	return out.String()
}
