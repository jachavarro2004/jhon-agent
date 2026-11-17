// Package isodate reads and writes the ISO 8601 timestamps Jhon's records
// carry, as JavaScript's Date.parse and toISOString do for them.
package isodate

import (
	"regexp"
	"time"
)

var layouts = []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02T15:04", "2006-01-02T15:04Z07:00"}

// Parse reads an ISO 8601 date-time; without a zone it is local time, as Date.parse reads it.
func Parse(s string) (time.Time, bool) {
	for _, l := range layouts {
		loc := time.Local
		if t, err := time.ParseInLocation(l, s, loc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

var dated = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T`)

// IsDate is whether s is a date-time Parse reads, written date first.
func IsDate(s string) bool {
	if !dated.MatchString(s) {
		return false
	}
	_, ok := Parse(s)
	return ok
}

// Format writes t as toISOString does: UTC, milliseconds, Z.
func Format(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}
