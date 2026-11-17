package cli

import (
	"testing"
	"time"
)

// F6 (L1159): a call's default time bound is the shell's reviewTimeoutMs:
// 3 minutes plus 1.5 s per started KiB of prompt, at most 30 minutes.
func TestReviewClaudeTimeout(t *testing.T) {
	for bytes, want := range map[int]time.Duration{
		-1:         3 * time.Minute,
		0:          3 * time.Minute,
		1:          3*time.Minute + 1500*time.Millisecond,
		1024:       3*time.Minute + 1500*time.Millisecond,
		1025:       3*time.Minute + 3*time.Second,
		100 << 10:  3*time.Minute + 150*time.Second,
		1080 << 10: 30 * time.Minute,
		1079 << 10: 3*time.Minute + 1079*1500*time.Millisecond,
		10 << 20:   30 * time.Minute,
	} {
		if got := reviewClaudeTimeout(bytes); got != want {
			t.Errorf("reviewClaudeTimeout(%d) = %s, want %s", bytes, got, want)
		}
	}
}
