package main

import (
	"math"
	"slices"
)

// median is the middle of ms, the mean of the two middle values for an even
// count, 0 for none; ms is left as it came.
func median(ms []float64) float64 {
	if len(ms) == 0 {
		return 0
	}
	s := slices.Clone(ms)
	slices.Sort(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// maxOf is the largest of ms, 0 for none.
func maxOf(ms []float64) float64 {
	if len(ms) == 0 {
		return 0
	}
	return slices.Max(ms)
}

// round is x to places decimals.
func round(x float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(x*p) / p
}

// per100 is n per 100 of total, to three decimals; 0 when total is 0.
func per100(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return round(100*float64(n)/float64(total), 3)
}
