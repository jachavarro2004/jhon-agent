package js

import (
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

var decimal = regexp.MustCompile(`^[+-]?(?:Infinity|(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?)$`)

// Number is Number(s) for a string: trimmed, "" is 0, a decimal literal
// (Infinity included) or a 0x, 0o, or 0b integer is its value, and anything
// else is NaN. strconv alone would also take "inf", "nan", hex floats, and
// underscores.
func Number(s string) float64 {
	s = Trim(s)
	if s == "" {
		return 0
	}
	if len(s) > 2 && s[0] == '0' {
		base := map[byte]int{'x': 16, 'X': 16, 'o': 8, 'O': 8, 'b': 2, 'B': 2}[s[1]]
		if base != 0 {
			n, ok := new(big.Int).SetString(s[2:], base)
			if !ok || strings.ContainsAny(s[2:], "+-_") {
				return math.NaN()
			}
			f, _ := new(big.Float).SetInt(n).Float64()
			return f
		}
	}
	if !decimal.MatchString(s) {
		return math.NaN()
	}
	switch strings.TrimLeft(s, "+-") {
	case "Infinity":
		if s[0] == '-' {
			return math.Inf(-1)
		}
		return math.Inf(1)
	}
	f, _ := strconv.ParseFloat(s, 64) // out of range: ±Inf, as in JavaScript
	return f
}

// IsInteger is Number.isInteger.
func IsInteger(f float64) bool { return !math.IsInf(f, 0) && !math.IsNaN(f) && f == math.Trunc(f) }

// NumberString is String(n), the text a template literal gives a number:
// the shortest round-trip digits, positional up to 21 digits, exponential
// beyond them and below 1e-6.
func NumberString(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case f == 0:
		return "0"
	case f < 0:
		return "-" + NumberString(-f)
	case math.IsInf(f, 1):
		return "Infinity"
	}
	e := strconv.FormatFloat(f, 'e', -1, 64) // d.ddde±x
	mant, exp, _ := strings.Cut(e, "e")
	digits := strings.Replace(mant, ".", "", 1)
	x, _ := strconv.Atoi(exp)
	k, n := len(digits), x+1
	switch {
	case k <= n && n <= 21:
		return digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return "0." + strings.Repeat("0", -n) + digits
	}
	sign := "+"
	if n-1 < 0 {
		sign = "-"
	}
	out := digits[:1]
	if k > 1 {
		out += "." + digits[1:]
	}
	return out + "e" + sign + strconv.Itoa(max(n-1, 1-n))
}
