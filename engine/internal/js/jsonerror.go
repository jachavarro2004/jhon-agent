package js

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
)

// JSONParse is JSON.parse with V8's error messages ("Unexpected end of JSON
// input", "Expected ':' after property name in JSON at position 4 (line 1
// column 5)"), for output that prints them, as the renderer does when
// settings.json is not JSON.
func JSONParse(s string) (any, error) {
	v, err := ParseJSON(s)
	if err == nil {
		return v, nil
	}
	if msg := JSONSyntaxError(s); msg != "" {
		return nil, errors.New(msg)
	}
	return nil, err
}

// JSONSyntaxError is the message V8's JSON.parse throws for s; "" when s
// is valid JSON.
func JSONSyntaxError(s string) string {
	p := &v8json{s: utf16.Encode([]rune(s))}
	if err := p.document(); err != nil {
		return err.Error()
	}
	return ""
}

// v8json walks JSON as V8's parser does, over UTF-16 code units, only to
// name the first error the way it does.
type v8json struct {
	s   []uint16
	pos int
}

var errEnd = errors.New("Unexpected end of JSON input")

func (p *v8json) eos() bool { return p.pos >= len(p.s) }

func (p *v8json) peek() uint16 { return p.s[p.pos] }

func (p *v8json) space() {
	for !p.eos() {
		switch p.peek() {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

// at is an error naming the position, its line, and its column.
func (p *v8json) at(msg string) error { return p.after(msg + " in JSON") }

// after is at without " in JSON", which V8 leaves out after a document.
func (p *v8json) after(msg string) error {
	line, last := 1, -1
	for i := 0; i < p.pos && i < len(p.s); i++ {
		if p.s[i] == '\n' {
			line, last = line+1, i
		}
	}
	return fmt.Errorf("%s at position %d (line %d column %d)", msg, p.pos, line, p.pos-last)
}

// unexpected is the error for the unit at the position: the end of input,
// or the token with the source around it.
func (p *v8json) unexpected() error {
	if p.eos() {
		return errEnd
	}
	const context = 10
	src := func(from, to int) string { return string(utf16.Decode(p.s[max(0, from):min(len(p.s), to)])) }
	token := src(p.pos, p.pos+1)
	var around string
	switch n := len(p.s); {
	case n <= 2*context:
		around = `"` + src(0, n) + `"`
	case p.pos < context:
		around = `"` + src(0, p.pos+context) + `"...`
	case p.pos < n-context:
		around = `..."` + src(p.pos-context, p.pos+context) + `"...`
	default:
		around = `..."` + src(p.pos-context, n) + `"`
	}
	return fmt.Errorf("Unexpected token '%s', %s is not valid JSON", token, around)
}

func (p *v8json) document() error {
	p.space()
	if err := p.value(); err != nil {
		return err
	}
	p.space()
	if !p.eos() {
		return p.after("Unexpected non-whitespace character after JSON")
	}
	return nil
}

func (p *v8json) value() error {
	if p.eos() {
		return errEnd
	}
	switch c := p.peek(); {
	case c == '"':
		return p.str()
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
	case c == 't':
		return p.literal("true")
	case c == 'f':
		return p.literal("false")
	case c == 'n':
		return p.literal("null")
	}
	return p.unexpected()
}

func (p *v8json) literal(word string) error {
	for i := 0; i < len(word); i++ {
		if p.eos() {
			return errEnd
		}
		if p.peek() != uint16(word[i]) {
			return p.unexpected()
		}
		p.pos++
	}
	return nil
}

func (p *v8json) object() error {
	p.pos++ // {
	p.space()
	if !p.eos() && p.peek() == '}' {
		p.pos++
		return nil
	}
	first := true
	for {
		if p.eos() || p.peek() != '"' {
			if first {
				return p.at("Expected property name or '}'")
			}
			return p.at("Expected double-quoted property name")
		}
		first = false
		if err := p.str(); err != nil {
			return err
		}
		p.space()
		if p.eos() || p.peek() != ':' {
			return p.at("Expected ':' after property name")
		}
		p.pos++
		p.space()
		if err := p.value(); err != nil {
			return err
		}
		p.space()
		if p.eos() || (p.peek() != ',' && p.peek() != '}') {
			return p.at("Expected ',' or '}' after property value")
		}
		if p.peek() == '}' {
			p.pos++
			return nil
		}
		p.pos++
		p.space()
	}
}

func (p *v8json) array() error {
	p.pos++ // [
	p.space()
	if !p.eos() && p.peek() == ']' {
		p.pos++
		return nil
	}
	for {
		if err := p.value(); err != nil {
			return err
		}
		p.space()
		if p.eos() || (p.peek() != ',' && p.peek() != ']') {
			return p.at("Expected ',' or ']' after array element")
		}
		if p.peek() == ']' {
			p.pos++
			return nil
		}
		p.pos++
		p.space()
	}
}

func (p *v8json) str() error {
	p.pos++ // "
	for {
		if p.eos() {
			return p.at("Unterminated string")
		}
		c := p.peek()
		switch {
		case c == '"':
			p.pos++
			return nil
		case c < 0x20:
			return p.at("Bad control character in string literal")
		case c == '\\':
			p.pos++
			if p.eos() {
				return errEnd
			}
			e := p.peek()
			if e == 'u' {
				for i := 0; i < 4; i++ {
					p.pos++
					if p.eos() || !isHex(p.peek()) {
						return p.at("Bad Unicode escape")
					}
				}
			} else if !strings.ContainsRune(`"\/bfnrt`, rune(e)) {
				return p.at("Bad escaped character")
			}
		}
		p.pos++
	}
}

func isHex(c uint16) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func (p *v8json) digits() int {
	n := 0
	for !p.eos() && p.peek() >= '0' && p.peek() <= '9' {
		p.pos++
		n++
	}
	return n
}

func (p *v8json) number() error {
	if p.peek() == '-' {
		p.pos++
		if p.eos() || p.peek() < '0' || p.peek() > '9' {
			return p.at("No number after minus sign")
		}
	}
	if p.peek() == '0' {
		p.pos++
		if !p.eos() && p.peek() >= '0' && p.peek() <= '9' {
			return p.at("Unexpected number")
		}
	} else {
		p.digits()
	}
	if !p.eos() && p.peek() == '.' {
		p.pos++
		if p.digits() == 0 {
			return p.at("Unterminated fractional number")
		}
	}
	if !p.eos() && (p.peek() == 'e' || p.peek() == 'E') {
		p.pos++
		if !p.eos() && (p.peek() == '+' || p.peek() == '-') {
			p.pos++
		}
		if p.digits() == 0 {
			return p.at("Exponent part is missing a number")
		}
	}
	return nil
}
