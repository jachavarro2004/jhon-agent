package js

import (
	"slices"
	"testing"
)

// A parsed value prints back as JSON.stringify prints it: integer keys
// first, numbers in JavaScript's form, a lone surrogate escaped.
func TestValueRoundTrip(t *testing.T) {
	v, err := ParseJSON(`{"b":1.50,"a":[true,null,"\ud800x"],"10":{},"1":1e21,"b":2}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := StringifyValueCompact(v); got != `{"1":1e+21,"10":{},"b":2,"a":[true,null,"\ud800x"]}` {
		t.Errorf("compact = %s", got)
	}
	if got := StringifyValue(Obj("a", []any{1.0}, "u", Undefined{})); got != "{\n  \"a\": [\n    1\n  ]\n}" {
		t.Errorf("indented = %q", got)
	}
	for _, bad := range []string{"", "{", `{"a":1,}`, "\ufeff{}", "01"} {
		if _, err := ParseJSON(bad); err == nil {
			t.Errorf("ParseJSON(%q) accepted", bad)
		}
	}
}

func TestWellFormedAndDecode(t *testing.T) {
	v, _ := ParseJSON(`"a\udc00\ud800b"`)
	if got := WellFormed(v.(string)); got != "a��b" {
		t.Errorf("WellFormed = %q", got)
	}
	if got := DecodeUTF8([]byte("a\xe2\x82A\xffé")); got != "a�A�é" {
		t.Errorf("DecodeUTF8 = %q", got)
	}
}

func TestTruthy(t *testing.T) {
	for _, v := range []any{nil, Undefined{}, false, 0.0, ""} {
		if Truthy(v) {
			t.Errorf("Truthy(%v)", v)
		}
	}
	for _, v := range []any{true, 1.0, "x", []any{}, NewObject()} {
		if !Truthy(v) {
			t.Errorf("!Truthy(%v)", v)
		}
	}
}

// JSON.parse and JSON.stringify(v, null, 2) as JavaScript gives them: key
// order, number form, escapes, and the inputs JSON.parse refuses.
func TestJSONLikeJavaScript(t *testing.T) {
	for in, want := range map[string]string{
		`{"b": 1, "a": [], "2": {}, "10": "x", "1": null}`: "{\n  \"1\": null,\n  \"2\": {},\n  \"10\": \"x\",\n  \"b\": 1,\n  \"a\": []\n}",
		`{"a": 1, "b": 2, "a": 3}`:                         "{\n  \"a\": 3,\n  \"b\": 2\n}",
		`[1.50, 1e21, 1e-7, 123e-20, -0, 0.000001, 1E400]`: "[\n  1.5,\n  1e+21,\n  1e-7,\n  1.23e-18,\n  0,\n  0.000001,\n  null\n]",
		`"\ud800 😀   <&> \u0001 \/"`:                       "\"\\ud800 😀   <&> \\u0001 /\"",
		`"4294967295"`:                                     `"4294967295"`,
	} {
		v, err := ParseJSON(in)
		if err != nil {
			t.Fatalf("ParseJSON(%s): %v", in, err)
		}
		if got := StringifyValue(v); got != want {
			t.Errorf("StringifyValue(%s) = %s, want %s", in, got, want)
		}
	}
	for _, in := range []string{`{"a": 1,}`, `01`, `1.`, `"\x"`, "\"\t\"", `nul`, `[1] 2`, `{'a': 1}`} {
		if _, err := ParseJSON(in); err == nil {
			t.Errorf("ParseJSON(%s) accepted", in)
		}
	}
	o, _ := ParseJSON(`{"4294967295": 1, "4294967294": 2}`)
	if got := o.(*Object).Keys(); !slices.Equal(got, []string{"4294967294", "4294967295"}) {
		t.Errorf("array index order = %q", got)
	}
	for v, want := range map[any]string{Undefined{}: "undefined", nil: "null", 2.5: "2.5", true: "true"} {
		if got := String(v); got != want {
			t.Errorf("String = %q", got)
		}
	}
	arr, _ := ParseJSON(`[1, null, [2, "a"], {}]`)
	if got := String(arr); got != "1,,2,a,[object Object]" {
		t.Errorf("String array = %q", got)
	}
}
