package main

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// A message whose agent settled while its background job still runs stays
// open until the next agent_settled, the job's notice turn (T47).
func TestABackgroundJobKeepsTheMessageOpen(t *testing.T) {
	for _, mode := range []string{"notice", "silent"} {
		t.Run(mode, func(t *testing.T) {
			f := newFake(t)
			t.Setenv("FAKE_PI_BG", mode)
			o := multiOptions(t, f, "", "pi-jhon")
			o.Tasks[0].Timeout = 20 * time.Second
			if err := Execute(o); err != nil {
				t.Fatal(err)
			}
			stream := string(readStream(t, o, "pagination-bug-plain-pi-jhon-1"))
			if got := boundaries([]byte(stream)); !slices.Equal(got, []string{"1 start", "1 wait job-1", "1 end settled", "2 start", "2 end settled"}) {
				t.Errorf("boundaries %q", got)
			}
			first := strings.Index(stream, `"agent_settled"`)
			second := first + 1 + strings.Index(stream[first+1:], `"agent_settled"`)
			if end := strings.Index(stream, `"n":1,"phase":"end"`); end < second {
				t.Errorf("message 1 ended at %d, before the second agent_settled at %d:\n%s", end, second, stream)
			}
		})
	}
}
