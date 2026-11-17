package dashboard

import (
	"bytes"
	"encoding/json"
	"math"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// The dashboard reads a report as a parsed JSON value (js.ParseJSON), and
// prints what it builds as compact JSON, keys in their own order
// (js.StringifyValueCompact). These are the operators
// the view applies to such values.

// or is v ?? d.
func or(v, d any) any {
	if js.Nullish(v) {
		return d
	}
	return v
}

// num is v as arithmetic converts it: undefined is NaN, null 0.
func num(v any) float64 {
	switch x := v.(type) {
	case nil:
		return 0
	case bool:
		if x {
			return 1
		}
		return 0
	case float64:
		return x
	case string:
		return js.Number(x)
	case []any:
		return js.Number(js.String(x))
	}
	return math.NaN()
}

// list is v as an array; anything else is empty.
func list(v any) []any {
	a, _ := v.([]any)
	return a
}

// strictEqual is a === b.
func strictEqual(a, b any) bool {
	switch x := a.(type) {
	case nil, bool, string, js.Undefined:
		return a == b
	case float64:
		y, ok := b.(float64)
		return ok && x == y
	case *js.Object:
		y, ok := b.(*js.Object)
		return ok && x == y
	}
	return false // an array: no two are the same in a report
}

// sameValueZero is how a Map compares keys: === where NaN equals NaN.
func sameValueZero(a, b any) bool {
	x, ok1 := a.(float64)
	y, ok2 := b.(float64)
	if ok1 && ok2 && math.IsNaN(x) && math.IsNaN(y) {
		return true
	}
	return strictEqual(a, b)
}

// toValue is a report as the JSON value it prints as: a parsed value as it
// is, a Go value (a report built in Go) through its JSON.
func toValue(v any) any {
	switch v.(type) {
	case nil, bool, float64, string, *js.Object, js.Undefined:
		return v
	}
	if a, ok := v.([]any); ok {
		out := make([]any, len(a))
		for i, e := range a {
			out[i] = toValue(e)
		}
		return out
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		panic(err)
	}
	out, err := js.ParseJSON(b.String())
	if err != nil {
		panic(err)
	}
	return out
}
