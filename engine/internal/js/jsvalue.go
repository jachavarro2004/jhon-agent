package js

import (
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// A value read from JSON as JSON.parse holds it, for output that must print
// it back as JSON.stringify does: object keys in JavaScript's own order,
// numbers as JavaScript formats them, a lone surrogate escape kept (as
// WTF-8) and printed back escaped. encoding/json does none of these.
//
// A value is nil (null), bool, float64, string, []any, or *Object; a
// property an object lacks is Undefined{}.

// Undefined is JavaScript's undefined: a property a value does not have.
type Undefined struct{}

// Object is a JavaScript object parsed from JSON: values by key, string keys
// in first-insertion order.
type Object struct {
	keys []string
	vals map[string]any
}

// NewObject is an empty object.
func NewObject() *Object { return &Object{vals: map[string]any{}} }

// Obj is an object of key, value pairs, in that order ({k1: v1, k2: v2}).
func Obj(kv ...any) *Object {
	o := NewObject()
	for i := 0; i+1 < len(kv); i += 2 {
		o.Set(kv[i].(string), kv[i+1])
	}
	return o
}

// Set is o[k] = v: a new key goes last, an existing one keeps its place.
func (o *Object) Set(k string, v any) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

// Delete is delete o[k].
func (o *Object) Delete(k string) {
	if _, ok := o.vals[k]; !ok {
		return
	}
	delete(o.vals, k)
	o.keys = slices.DeleteFunc(o.keys, func(x string) bool { return x == k })
}

// Has is Object.hasOwn(o, k).
func (o *Object) Has(k string) bool { _, ok := o.vals[k]; return ok }

// Keys is Object.keys(o): array indices ascending, then the other keys in
// insertion order.
func (o *Object) Keys() []string {
	var ints, strs []string
	for _, k := range o.keys {
		if ArrayIndex(k) {
			ints = append(ints, k)
		} else {
			strs = append(strs, k)
		}
	}
	slices.SortFunc(ints, func(a, b string) int {
		if len(a) != len(b) {
			return len(a) - len(b)
		}
		return strings.Compare(a, b)
	})
	return append(ints, strs...)
}

// Clone is {...o}.
func (o *Object) Clone() *Object {
	c := NewObject()
	for _, k := range o.Keys() {
		c.Set(k, o.vals[k])
	}
	return c
}

// ArrayIndex is whether k is an array index, a key JavaScript orders first.
func ArrayIndex(k string) bool {
	if k == "0" {
		return true
	}
	if k == "" || k[0] < '1' || k[0] > '9' || len(k) > 10 {
		return false
	}
	n, err := strconv.ParseUint(k, 10, 64)
	return err == nil && n < 1<<32-1
}

// Prop is v[k] for a parsed value: Undefined{} unless v is an object
// holding k.
func Prop(v any, k string) any {
	if o, ok := v.(*Object); ok {
		if x, ok := o.vals[k]; ok {
			return x
		}
	}
	return Undefined{}
}

// IsObject is whether v is a plain object (not null, not an array).
func IsObject(v any) bool { _, ok := v.(*Object); return ok }

// Nullish is whether v is null or undefined, what ?? and ?. test.
func Nullish(v any) bool {
	switch v.(type) {
	case nil, Undefined:
		return true
	}
	return false
}

// Truthy is whether v is truthy: not undefined, null, false, 0, NaN, or "".
func Truthy(v any) bool {
	switch x := v.(type) {
	case nil, Undefined:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && !math.IsNaN(x)
	case string:
		return x != ""
	}
	return true
}

var errJSON = errors.New("invalid JSON")

// ParseJSON is JSON.parse.
func ParseJSON(s string) (any, error) {
	p := &jsonParser{s: s}
	p.space()
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	if p.space(); p.i != len(s) {
		return nil, errJSON
	}
	return v, nil
}

type jsonParser struct {
	s string
	i int
}

func (p *jsonParser) space() {
	for p.i < len(p.s) && strings.IndexByte(" \t\n\r", p.s[p.i]) >= 0 {
		p.i++
	}
}

func (p *jsonParser) literal(word string, v any) (any, error) {
	if !strings.HasPrefix(p.s[p.i:], word) {
		return nil, errJSON
	}
	p.i += len(word)
	return v, nil
}

func (p *jsonParser) value() (any, error) {
	if p.i >= len(p.s) {
		return nil, errJSON
	}
	switch c := p.s[p.i]; {
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
	case c == '"':
		return p.string()
	case c == 't':
		return p.literal("true", true)
	case c == 'f':
		return p.literal("false", false)
	case c == 'n':
		return p.literal("null", nil)
	case c == '-' || c >= '0' && c <= '9':
		return p.number()
	}
	return nil, errJSON
}

func (p *jsonParser) object() (any, error) {
	o := NewObject()
	p.i++
	p.space()
	if p.i < len(p.s) && p.s[p.i] == '}' {
		p.i++
		return o, nil
	}
	for {
		if p.i >= len(p.s) || p.s[p.i] != '"' {
			return nil, errJSON
		}
		k, err := p.string()
		if err != nil {
			return nil, err
		}
		p.space()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return nil, errJSON
		}
		p.i++
		p.space()
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		o.Set(k.(string), v)
		p.space()
		if p.i >= len(p.s) {
			return nil, errJSON
		}
		switch p.s[p.i] {
		case ',':
			p.i++
			p.space()
		case '}':
			p.i++
			return o, nil
		default:
			return nil, errJSON
		}
	}
}

func (p *jsonParser) array() (any, error) {
	a := []any{}
	p.i++
	p.space()
	if p.i < len(p.s) && p.s[p.i] == ']' {
		p.i++
		return a, nil
	}
	for {
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		a = append(a, v)
		p.space()
		if p.i >= len(p.s) {
			return nil, errJSON
		}
		switch p.s[p.i] {
		case ',':
			p.i++
			p.space()
		case ']':
			p.i++
			return a, nil
		default:
			return nil, errJSON
		}
	}
}

func (p *jsonParser) hex4() (rune, bool) {
	if p.i+4 > len(p.s) {
		return 0, false
	}
	n, err := strconv.ParseUint(p.s[p.i:p.i+4], 16, 32)
	if err != nil || strings.ContainsAny(p.s[p.i:p.i+4], "+-_") {
		return 0, false
	}
	p.i += 4
	return rune(n), true
}

func isSurrogate(r rune) bool { return r >= 0xd800 && r <= 0xdfff }

// wtf8 appends r even when it is a lone surrogate, which UTF-8 cannot hold.
func wtf8(b []byte, r rune) []byte {
	if !isSurrogate(r) {
		return utf8.AppendRune(b, r)
	}
	return append(b, byte(0xe0|r>>12), byte(0x80|(r>>6)&0x3f), byte(0x80|r&0x3f))
}

func (p *jsonParser) string() (any, error) {
	p.i++
	var b []byte
	for {
		if p.i >= len(p.s) {
			return nil, errJSON
		}
		c := p.s[p.i]
		switch {
		case c == '"':
			p.i++
			return string(b), nil
		case c < 0x20:
			return nil, errJSON
		case c != '\\':
			b = append(b, c)
			p.i++
			continue
		}
		p.i++
		if p.i >= len(p.s) {
			return nil, errJSON
		}
		e := p.s[p.i]
		p.i++
		if simple := strings.IndexByte(`"\/bfnrt`, e); simple >= 0 {
			b = append(b, "\"\\/\b\f\n\r\t"[simple])
			continue
		}
		if e != 'u' {
			return nil, errJSON
		}
		r, ok := p.hex4()
		if !ok {
			return nil, errJSON
		}
		if r >= 0xd800 && r < 0xdc00 && strings.HasPrefix(p.s[p.i:], `\u`) {
			save := p.i
			p.i += 2
			if lo, ok := p.hex4(); ok && lo >= 0xdc00 && lo <= 0xdfff {
				b = utf8.AppendRune(b, 0x10000+(r-0xd800)<<10+(lo-0xdc00))
				continue
			}
			p.i = save
		}
		b = wtf8(b, r)
	}
}

func (p *jsonParser) number() (any, error) {
	start := p.i
	digits := func() int {
		n := 0
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			p.i++
			n++
		}
		return n
	}
	if p.s[p.i] == '-' {
		p.i++
	}
	if p.i < len(p.s) && p.s[p.i] == '0' {
		p.i++
	} else if digits() == 0 {
		return nil, errJSON
	}
	if p.i < len(p.s) && p.s[p.i] == '.' {
		p.i++
		if digits() == 0 {
			return nil, errJSON
		}
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		p.i++
		if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
			p.i++
		}
		if digits() == 0 {
			return nil, errJSON
		}
	}
	f, _ := strconv.ParseFloat(p.s[start:p.i], 64) // out of range: ±Inf, as JSON.parse
	return f, nil
}

// StringifyValue is JSON.stringify(v, null, 2) for a parsed value.
func StringifyValue(v any) string {
	var b strings.Builder
	writeValue(&b, v, "  ", "")
	return b.String()
}

// StringifyValueCompact is JSON.stringify(v) for a parsed value.
func StringifyValueCompact(v any) string {
	var b strings.Builder
	writeValue(&b, v, "", "")
	return b.String()
}

func writeValue(b *strings.Builder, v any, step, indent string) {
	inner := indent + step
	open, sep, colon, end := "\n"+inner, ",\n"+inner, ": ", "\n"+indent
	if step == "" {
		open, sep, colon, end = "", ",", ":", ""
	}
	switch x := v.(type) {
	case nil, Undefined:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case float64:
		if math.IsInf(x, 0) || math.IsNaN(x) {
			b.WriteString("null")
		} else {
			b.WriteString(NumberString(x))
		}
	case string:
		quoteValue(b, x)
	case []any:
		if len(x) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[" + open)
		for i, item := range x {
			if i > 0 {
				b.WriteString(sep)
			}
			writeValue(b, item, step, inner)
		}
		b.WriteString(end + "]")
	case *Object:
		n := 0
		for _, k := range x.Keys() {
			item := x.vals[k]
			if _, ok := item.(Undefined); ok {
				continue
			}
			if n == 0 {
				b.WriteString("{" + open)
			} else {
				b.WriteString(sep)
			}
			n++
			quoteValue(b, k)
			b.WriteString(colon)
			writeValue(b, item, step, inner)
		}
		if n == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString(end + "}")
	}
}

// quoteValue is JSON.stringify of a string: `"`, `\`, and control
// characters escaped, a lone surrogate as \udxxx, everything else as itself.
func quoteValue(b *strings.Builder, s string) {
	const hex = "0123456789abcdef"
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			b.WriteString(`\"`)
		case c == '\\':
			b.WriteString(`\\`)
		case c == '\b':
			b.WriteString(`\b`)
		case c == '\f':
			b.WriteString(`\f`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			b.WriteString(`\r`)
		case c == '\t':
			b.WriteString(`\t`)
		case c < 0x20:
			b.WriteString(`\u00` + string(hex[c>>4]) + string(hex[c&0xf]))
		case lone(s, i):
			r := rune(c&0x0f)<<12 | rune(s[i+1]&0x3f)<<6 | rune(s[i+2]&0x3f)
			b.WriteString(`\u` + strconv.FormatInt(int64(r), 16))
			i += 2
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
}

// lone is whether s holds a WTF-8 surrogate at byte i.
func lone(s string, i int) bool {
	return s[i] == 0xed && i+2 < len(s) && s[i+1] >= 0xa0 && s[i+1] <= 0xbf && s[i+2] >= 0x80 && s[i+2] <= 0xbf
}

// WellFormed is the text Buffer.from(s, "utf8") encodes, and so what a
// write of s to a stream sends: each lone surrogate as U+FFFD.
func WellFormed(s string) string {
	if !strings.Contains(s, "\xed") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if lone(s, i) {
			b.WriteString("�")
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// DecodeUTF8 is buffer.toString("utf8"): each maximal invalid subsequence
// becomes U+FFFD, as the WHATWG decoder node uses replaces it.
func DecodeUTF8(p []byte) string {
	if utf8.Valid(p) {
		return string(p)
	}
	var b strings.Builder
	needed, seen, lower, upper := 0, 0, byte(0x80), byte(0xbf)
	var cp rune
	for i := 0; i < len(p); i++ {
		c := p[i]
		if needed == 0 {
			switch {
			case c <= 0x7f:
				b.WriteByte(c)
			case c >= 0xc2 && c <= 0xdf:
				needed, cp = 1, rune(c&0x1f)
			case c >= 0xe0 && c <= 0xef:
				if c == 0xe0 {
					lower = 0xa0
				} else if c == 0xed {
					upper = 0x9f
				}
				needed, cp = 2, rune(c&0xf)
			case c >= 0xf0 && c <= 0xf4:
				if c == 0xf0 {
					lower = 0x90
				} else if c == 0xf4 {
					upper = 0x8f
				}
				needed, cp = 3, rune(c&7)
			default:
				b.WriteString("�")
			}
			continue
		}
		if c < lower || c > upper {
			needed, seen, cp, lower, upper = 0, 0, 0, 0x80, 0xbf
			b.WriteString("�")
			i--
			continue
		}
		lower, upper = 0x80, 0xbf
		cp = cp<<6 | rune(c&0x3f)
		if seen++; seen == needed {
			b.WriteRune(cp)
			needed, seen, cp = 0, 0, 0
		}
	}
	if needed != 0 {
		b.WriteString("�")
	}
	return b.String()
}
