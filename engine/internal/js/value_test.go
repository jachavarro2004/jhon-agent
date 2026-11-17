package js

import (
	"encoding/json"
	"testing"
)

func TestLower(t *testing.T) {
	if Lower("İAM") != "i̇am" || Lower("ÄB") != "äb" {
		t.Errorf("Lower = %q", Lower("İAM"))
	}
}

func TestString(t *testing.T) {
	for _, c := range []struct {
		v    any
		want string
	}{
		{nil, "null"}, {"x", "x"}, {true, "true"}, {2.5, "2.5"}, {json.Number("1e21"), "1e+21"},
		{[]any{1.0, nil, []any{"a", "b"}}, "1,,a,b"}, {map[string]any{}, "[object Object]"},
	} {
		if got := String(c.v); got != c.want {
			t.Errorf("String(%v) = %q, want %q", c.v, got, c.want)
		}
	}
}
