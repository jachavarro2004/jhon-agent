package js

import (
	"slices"
	"testing"
)

func TestStringify(t *testing.T) {
	got, err := Stringify(map[string]any{"a": "<&>\u2028\\u2028", "b": []string{}})
	want := "{\n  \"a\": \"<&>\u2028\\\\u2028\",\n  \"b\": []\n}"
	if err != nil || got != want {
		t.Errorf("Stringify = %q, want %q", got, want)
	}
}

func TestStrings(t *testing.T) {
	if Trim("\ufeff\u00a0 x \u2028") != "x" || Trim("\u0085x") != "\u0085x" {
		t.Error("Trim removes JavaScript whitespace only")
	}
	if Len("a😀") != 3 || PadEnd("é", 3) != "é  " {
		t.Error("lengths are UTF-16 code units")
	}
	names := []string{"😀", "\uff01", "B", "a"}
	slices.SortFunc(names, Compare)
	if !slices.Equal(names, []string{"B", "a", "😀", "\uff01"}) {
		t.Errorf("Compare orders by UTF-16 code units: %q", names)
	}
}

func TestStringifyCompact(t *testing.T) {
	got, err := StringifyCompact(map[string]any{"a": "<&> ", "b": []any{1, nil}})
	if want := "{\"a\":\"<&> \",\"b\":[1,null]}"; err != nil || got != want {
		t.Errorf("StringifyCompact = %q, want %q", got, want)
	}
}
