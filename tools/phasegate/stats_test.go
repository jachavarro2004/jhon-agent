package main

import (
	"slices"
	"testing"
)

func TestMedianAndMax(t *testing.T) {
	for _, c := range []struct {
		in          []float64
		median, max float64
	}{
		{nil, 0, 0},
		{[]float64{5}, 5, 5},
		{[]float64{3, 1, 2}, 2, 3},
		{[]float64{4, 1, 3, 2}, 2.5, 4},
		{[]float64{120, 90, 30000, 100, 110}, 110, 30000},
	} {
		in := slices.Clone(c.in)
		if got := median(in); got != c.median {
			t.Errorf("median(%v) = %v, want %v", c.in, got, c.median)
		}
		if got := maxOf(in); got != c.max {
			t.Errorf("maxOf(%v) = %v, want %v", c.in, got, c.max)
		}
		if !slices.Equal(in, c.in) {
			t.Errorf("median reordered its input: %v, want %v", in, c.in)
		}
	}
}
