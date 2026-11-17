package js

import (
	"math"
	"testing"
)

func TestNumber(t *testing.T) {
	for s, want := range map[string]string{
		"": "0", " 12 ": "12", "0x1F": "31", "0b101": "5", "0o17": "15", "-0x1": "NaN", "1e3": "1000", ".5": "0.5",
		"5.": "5", "+Infinity": "Infinity", "-Infinity": "-Infinity", "inf": "NaN", "1_000": "NaN", "NaN": "NaN",
		"0x": "NaN", " 7\n": "7",
	} {
		if got := NumberString(Number(s)); got != want {
			t.Errorf("Number(%q) = %s, want %s", s, got, want)
		}
	}
}

// tenth is a variable so 0.1 + 0.2 is computed in float64, not folded as an exact constant.
var tenth = 0.1

func TestNumberString(t *testing.T) {
	for f, want := range map[float64]string{
		0: "0", math.Copysign(0, -1): "0", 1.5: "1.5", 123456789012345680000: "123456789012345680000", 1e21: "1e+21",
		1.5e-7: "1.5e-7", 0.000001: "0.000001", 1e-7: "1e-7", 1 << 53: "9007199254740992", -2.5e300: "-2.5e+300",
		tenth + 0.2: "0.30000000000000004", math.Inf(-1): "-Infinity",
	} {
		if got := NumberString(f); got != want {
			t.Errorf("NumberString(%v) = %s, want %s", f, got, want)
		}
	}
	if !IsInteger(3) || IsInteger(3.5) || IsInteger(math.Inf(1)) || IsInteger(math.NaN()) {
		t.Error("IsInteger")
	}
}
