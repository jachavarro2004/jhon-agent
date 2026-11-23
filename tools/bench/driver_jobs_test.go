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

// A message whose agent settled while a background sub-agent it launched
// with subagent or subagent_continue still runs stays open until the
// agent_settled after the sub-agent's end (its notice, matched by task id):
// another settle in between does not close it, a sub-agent task_stop ended
// holds it no longer, and a foreground subagent holds nothing (T209).
func TestABackgroundSubAgentKeepsTheMessageOpen(t *testing.T) {
	for _, c := range []struct {
		mode, notices string
		want          []string
	}{
		{"subagent", "agent-1", []string{"1 start", "1 wait agent-1", "1 end settled", "2 start", "2 end settled"}},
		{"continue", "agent-1", []string{"1 start", "1 wait agent-1", "1 end settled", "2 start", "2 end settled"}},
		{"two", "agent-2", []string{"1 start", "1 wait agent-1 agent-2", "1 wait agent-2", "1 end settled", "2 start", "2 end settled"}},
		{"stop", "", []string{"1 start", "1 end settled", "2 start", "2 end settled"}},
		{"foreground", "", []string{"1 start", "1 end settled", "2 start", "2 end settled"}},
	} {
		t.Run(c.mode, func(t *testing.T) {
			f := newFake(t)
			t.Setenv("FAKE_PI_AGENT", c.mode)
			o := multiOptions(t, f, "", "pi-jhon")
			o.Tasks[0].Timeout = 20 * time.Second
			if err := Execute(o); err != nil {
				t.Fatal(err)
			}
			stream := string(readStream(t, o, "pagination-bug-plain-pi-jhon-1"))
			if got := boundaries([]byte(stream)); !slices.Equal(got, c.want) {
				t.Errorf("boundaries %q, want %q", got, c.want)
			}
			if c.notices == "" {
				return
			}
			notice := strings.Index(stream, `Background sub-agent `+c.notices+` `)
			if end := strings.Index(stream, `"n":1,"phase":"end"`); notice < 0 || end < notice {
				t.Errorf("message 1 ended at %d, before the %s notice at %d:\n%s", end, c.notices, notice, stream)
			}
		})
	}
}
