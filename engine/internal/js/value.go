package js

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Lower is String.prototype.toLowerCase where it differs from
// strings.ToLower: U+0130 lowers to "i" and U+0307, not "i". (Final sigma is
// not reproduced.)
func Lower(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "\u0130", "i\u0307"))
}

// String is String(v) for a value decoded from JSON (with UseNumber or
// without, or by ParseJSON): nil is null, an array joins its items with
// commas (null and undefined items empty), and an object is "[object Object]".
func String(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case Undefined:
		return "undefined"
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return NumberString(x)
	case json.Number:
		f, _ := strconv.ParseFloat(string(x), 64)
		return NumberString(f)
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			if !Nullish(e) {
				parts[i] = String(e)
			}
		}
		return strings.Join(parts, ",")
	default:
		return "[object Object]"
	}
}
