package telemetry

import "time"

// Last is the summary of the last days days, counted back from now's UTC
// midnight (1 is today); days is clamped to 1..Keep.
func Last(days int) Summary {
	days = max(1, min(Keep, days))
	t := now().UTC()
	since := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1-days)
	events, skipped := Read(since)
	return Summarize(events, days, skipped)
}
