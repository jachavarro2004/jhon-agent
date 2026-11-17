// Package toml is a structural TOML reader, enough for pyproject.toml.
//
// Inference that rests on a manifest has to read its structure: which table a
// key sits in, and whether a value is a dependency entry or prose that merely
// mentions a package. A regex over the text cannot tell those apart, so this
// reads tables, keys, strings (all four forms), arrays and inline tables.
//
// Scalars other than strings and booleans (numbers, dates) are kept as their
// raw text: nothing here computes with them. Malformed input is an error, and
// the caller decides what an unreadable manifest means; it is never read as
// empty. It is a hand-written parser rather than a library, so a parse
// failure and a value's shape stay fixed.
package toml

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// A Value is a string, a bool, a []Value, or a *Table.
type Value = any

// A Table is a TOML table: values by key, keys in insertion order.
type Table struct {
	keys []string
	vals map[string]Value
}

// NewTable is an empty table.
func NewTable() *Table { return &Table{vals: map[string]Value{}} }

// Get is the value at key k, and whether the table holds it.
func (t *Table) Get(k string) (Value, bool) {
	v, ok := t.vals[k]
	return v, ok
}

// Keys are the table's keys in the order Object.keys gives them: integer
// keys ascending, then the others in insertion order.
func (t *Table) Keys() []string {
	var ints, rest []string
	for _, k := range t.keys {
		if js.ArrayIndex(k) {
			ints = append(ints, k)
		} else {
			rest = append(rest, k)
		}
	}
	for i := 1; i < len(ints); i++ {
		for j := i; j > 0 && indexLess(ints[j], ints[j-1]); j-- {
			ints[j], ints[j-1] = ints[j-1], ints[j]
		}
	}
	return append(ints, rest...)
}

func (t *Table) set(k string, v Value) {
	if _, ok := t.vals[k]; !ok {
		t.keys = append(t.keys, k)
	}
	t.vals[k] = v
}

func indexLess(a, b string) bool { return len(a) < len(b) || len(a) == len(b) && a < b }

// AsTable is v as a table, when it is one.
func AsTable(v Value) (*Table, bool) {
	t, ok := v.(*Table)
	return t, ok && t != nil
}

// TableAt is the table at a dotted path, or nil when any step is absent or not a table.
func TableAt(doc *Table, path ...string) *Table {
	t := doc
	for _, k := range path {
		if t == nil {
			return nil
		}
		v, _ := t.Get(k)
		t, _ = AsTable(v)
	}
	return t
}

// Error is a malformed document: the line and what was expected there.
type Error struct{ msg string }

func (e *Error) Error() string { return e.msg }

func isBare(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

var (
	hexDigits = regexp.MustCompile(`^[0-9A-Fa-f]+$`)
	lineCont  = regexp.MustCompile(`^[ \t]*\r?\n`)
	rawScalar = regexp.MustCompile(`^(?:\d{4}-\d{2}-\d{2}[ T]\d{2}:[0-9:.+\-Z]*|[-+0-9A-Za-z_.:]+)`)
	escapes   = map[byte]string{'b': "\b", 't': "\t", 'n': "\n", 'f': "\f", 'r': "\r", '"': `"`, '\\': `\`, 'e': "\x1b"}
)

// parser reads src from i; a failure panics with an error that Parse returns.
type parser struct {
	src string
	i   int
}

// Parse reads a TOML document into its root table.
func Parse(src string) (root *Table, err error) {
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(error)
			if !ok {
				panic(r)
			}
			root, err = nil, e
		}
	}()
	p := &parser{src: src}
	return p.document(), nil
}

func (p *parser) fail(msg string) {
	line := strings.Count(p.src[:min(p.i, len(p.src))], "\n") + 1
	panic(&Error{fmt.Sprintf("line %d: %s", line, msg)})
}

// at is the byte at j, or 0 past the end.
func (p *parser) at(j int) byte {
	if j < len(p.src) {
		return p.src[j]
	}
	return 0
}

func (p *parser) has(s string) bool { return strings.HasPrefix(p.src[min(p.i, len(p.src)):], s) }

func (p *parser) skipSpace() {
	for p.i < len(p.src) && (p.src[p.i] == ' ' || p.src[p.i] == '\t') {
		p.i++
	}
}

func (p *parser) skipComment() {
	if p.at(p.i) == '#' {
		for p.i < len(p.src) && p.src[p.i] != '\n' {
			p.i++
		}
	}
}

// skipBlank skips whitespace, newlines and comments: what may separate array items.
func (p *parser) skipBlank() {
	for {
		p.skipSpace()
		switch {
		case p.i >= len(p.src):
			return
		case p.src[p.i] == '#':
			p.skipComment()
		case p.src[p.i] == '\n' || p.src[p.i] == '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *parser) expect(s string) {
	if !p.has(s) {
		p.fail(fmt.Sprintf("expected '%s'", s))
	}
	p.i += len(s)
}

func (p *parser) escape() string {
	if p.i >= len(p.src) {
		p.i++
		p.fail(`invalid escape '\undefined'`)
	}
	c := p.src[p.i]
	p.i++
	if s, ok := escapes[c]; ok {
		return s
	}
	if c == 'u' || c == 'U' {
		n := 4
		if c == 'U' {
			n = 8
		}
		hex := p.src[min(p.i, len(p.src)):min(p.i+n, len(p.src))]
		if !hexDigits.MatchString(hex) || len(hex) != n {
			p.fail("invalid unicode escape")
		}
		p.i += n
		cp, _ := strconv.ParseUint(hex, 16, 32)
		if cp > utf8.MaxRune {
			// String.fromCodePoint's RangeError, not a TomlError.
			panic(fmt.Errorf("Invalid code point %d", cp))
		}
		return string(rune(cp))
	}
	r, _ := utf8.DecodeRuneInString(p.src[p.i-1:])
	p.fail(fmt.Sprintf(`invalid escape '\%c'`, r))
	return ""
}

func (p *parser) str() string {
	if p.has(`"""`) {
		p.i += 3
		p.skipOpeningNewline()
		var out strings.Builder
		for {
			if p.i >= len(p.src) {
				p.fail("unterminated multi-line string")
			}
			if p.has(`"""`) {
				// Up to two quotes may sit right before the closing delimiter.
				extra := 0
				for p.at(p.i+3+extra) == '"' && extra < 2 {
					extra++
				}
				out.WriteString(strings.Repeat(`"`, extra))
				p.i += 3 + extra
				return out.String()
			}
			if p.src[p.i] == '\\' {
				p.i++
				// A line-ending backslash trims the newline and the whitespace after it.
				if lineCont.MatchString(p.src[min(p.i, len(p.src)):min(p.i+64, len(p.src))]) {
					for p.i < len(p.src) {
						r, size := utf8.DecodeRuneInString(p.src[p.i:])
						if !js.IsSpace(r) {
							break
						}
						p.i += size
					}
					continue
				}
				out.WriteString(p.escape())
				continue
			}
			out.WriteByte(p.src[p.i])
			p.i++
		}
	}
	if p.has("'''") {
		p.i += 3
		p.skipOpeningNewline()
		end := strings.Index(p.src[p.i:], "'''")
		if end < 0 {
			p.fail("unterminated multi-line literal string")
		}
		end += p.i
		extra := 0
		for p.at(end+3+extra) == '\'' && extra < 2 {
			extra++
		}
		out := p.src[p.i : end+extra]
		p.i = end + 3 + extra
		return out
	}
	if p.at(p.i) == '\'' {
		end := strings.IndexByte(p.src[p.i+1:], '\'')
		if end < 0 || strings.Contains(p.src[p.i+1:p.i+1+end], "\n") {
			p.fail("unterminated literal string")
		}
		out := p.src[p.i+1 : p.i+1+end]
		p.i += end + 2
		return out
	}
	p.expect(`"`)
	var out strings.Builder
	for {
		if p.i >= len(p.src) || p.src[p.i] == '\n' {
			p.fail("unterminated string")
		}
		c := p.src[p.i]
		p.i++
		if c == '"' {
			return out.String()
		}
		if c == '\\' {
			out.WriteString(p.escape())
		} else {
			out.WriteByte(c)
		}
	}
}

// skipOpeningNewline drops the newline right after a multi-line opening delimiter.
func (p *parser) skipOpeningNewline() {
	if p.at(p.i) == '\r' {
		p.i++
	}
	if p.at(p.i) == '\n' {
		p.i++
	}
}

func (p *parser) keyPath() []string {
	var parts []string
	for {
		p.skipSpace()
		if c := p.at(p.i); c == '"' || c == '\'' {
			parts = append(parts, p.str())
		} else {
			start := p.i
			for p.i < len(p.src) && isBare(p.src[p.i]) {
				p.i++
			}
			if start == p.i {
				p.fail("expected a key")
			}
			parts = append(parts, p.src[start:p.i])
		}
		p.skipSpace()
		if p.at(p.i) != '.' {
			return parts
		}
		p.i++
	}
}

func (p *parser) value() Value {
	switch p.at(p.i) {
	case '"', '\'':
		return p.str()
	case '[':
		p.i++
		out := []Value{}
		for {
			p.skipBlank()
			if p.at(p.i) == ']' {
				p.i++
				return out
			}
			out = append(out, p.value())
			p.skipBlank()
			if p.at(p.i) == ',' {
				p.i++
				continue
			}
			if p.at(p.i) == ']' {
				p.i++
				return out
			}
			p.fail("expected ',' or ']' in an array")
		}
	case '{':
		p.i++
		out := NewTable()
		p.skipBlank()
		if p.at(p.i) == '}' {
			p.i++
			return out
		}
		for {
			p.skipBlank()
			path := p.keyPath()
			p.expect("=")
			p.skipSpace()
			p.assign(out, path, p.value())
			p.skipBlank()
			if p.at(p.i) == ',' {
				p.i++
				continue
			}
			if p.at(p.i) == '}' {
				p.i++
				return out
			}
			p.fail("expected ',' or '}' in an inline table")
		}
	}
	if p.has("true") && !isBare(p.at(p.i+4)) {
		p.i += 4
		return true
	}
	if p.has("false") && !isBare(p.at(p.i+5)) {
		p.i += 5
		return false
	}
	// Numbers and dates, kept as raw text; a date-time may hold one space.
	m := rawScalar.FindString(p.src[min(p.i, len(p.src)):min(p.i+64, len(p.src))])
	if m == "" {
		p.fail("expected a value")
	}
	p.i += len(m)
	return m
}

// table walks (creating) the tables along a dotted path; an array of tables resolves to its last entry.
func (p *parser) table(from *Table, path []string) *Table {
	t := from
	for _, k := range path {
		next, ok := t.Get(k)
		if !ok {
			next = NewTable()
			t.set(k, next)
		}
		if list, isList := next.([]Value); isList {
			next = nil
			if len(list) > 0 {
				next = list[len(list)-1]
			}
		}
		nt, isTable := AsTable(next)
		if !isTable {
			p.fail(fmt.Sprintf("'%s' is not a table", k))
		}
		t = nt
	}
	return t
}

func (p *parser) assign(into *Table, path []string, v Value) {
	t := p.table(into, path[:len(path)-1])
	k := path[len(path)-1]
	if _, dup := t.Get(k); dup {
		p.fail(fmt.Sprintf("duplicate key '%s'", k))
	}
	t.set(k, v)
}

func (p *parser) document() *Table {
	root := NewTable()
	current := root
	for {
		p.skipBlank()
		if p.i >= len(p.src) {
			return root
		}
		if p.src[p.i] == '[' {
			many := p.at(p.i+1) == '['
			if many {
				p.i += 2
			} else {
				p.i++
			}
			path := p.keyPath()
			if many {
				p.expect("]]")
				parent := p.table(root, path[:len(path)-1])
				k := path[len(path)-1]
				v, ok := parent.Get(k)
				if !ok {
					v = []Value{}
				}
				list, isList := v.([]Value)
				if !isList {
					p.fail(fmt.Sprintf("'%s' is not an array of tables", k))
				}
				entry := NewTable()
				parent.set(k, append(list, entry))
				current = entry
			} else {
				p.expect("]")
				current = p.table(root, path)
			}
		} else {
			path := p.keyPath()
			p.expect("=")
			p.skipSpace()
			p.assign(current, path, p.value())
		}
		p.skipSpace()
		p.skipComment()
		if p.i < len(p.src) && p.src[p.i] != '\n' && p.src[p.i] != '\r' {
			p.fail("expected the end of the line")
		}
	}
}
