package js

import (
	"math"
	"strconv"
	"strings"
	"unicode"
)

// LocaleCompare approximates String.prototype.localeCompare's root
// collation: punctuation before digits before letters, letters compared
// without case first, then lowercase before uppercase.
func LocaleCompare(a, b string) int {
	key := func(r rune) (int, rune) {
		switch {
		case unicode.IsDigit(r):
			return 1, r
		case unicode.IsLetter(r):
			return 2, unicode.ToLower(r)
		}
		return 0, r
	}
	ra, rb := []rune(a), []rune(b)
	for i := 0; i < len(ra) && i < len(rb); i++ {
		ca, ka := key(ra[i])
		cb, kb := key(rb[i])
		if ca != cb {
			return ca - cb
		}
		if ka != kb {
			return int(ka - kb)
		}
	}
	if len(ra) != len(rb) {
		return len(ra) - len(rb)
	}
	for i := range ra {
		if la, lb := unicode.IsLower(ra[i]), unicode.IsLower(rb[i]); la != lb {
			if la {
				return -1
			}
			return 1
		}
	}
	return strings.Compare(a, b)
}

// ToFixed is Number.prototype.toFixed(d) for a finite x: an exact tie
// rounds away from zero, where strconv rounds it to even.
func ToFixed(x float64, d int) string {
	if math.IsNaN(x) {
		return "NaN"
	}
	exact := strconv.FormatFloat(math.Abs(x), 'f', 40, 64)
	dot := strings.IndexByte(exact, '.')
	if rest := exact[dot+1+d:]; rest[0] == '5' && strings.Trim(rest[1:], "0") == "" {
		bump := math.Pow(10, -float64(d)) / 2
		return strconv.FormatFloat(x+math.Copysign(bump, x), 'f', d, 64)
	}
	return strconv.FormatFloat(x, 'f', d, 64)
}
