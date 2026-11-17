package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
)

// The quality metrics of a multi-message run (T46, L436): the final hidden
// check stays the first; per-message checks show whether a later message
// broke what an earlier one built (regressions); rework is in process.go.

// MessageMetrics are one owner message's numbers, read from the stream
// between its bench_message records: the tokens of Claude Code's result
// events or pi's assistant message_end events, its turns (result num_turns
// or turn_start events), its wall time from start to end, and why it ended.
type MessageMetrics struct {
	N                int     `json:"n"`
	InputTokens      float64 `json:"input_tokens"`
	OutputTokens     float64 `json:"output_tokens"`
	CacheReadTokens  float64 `json:"cache_read_tokens"`
	CacheWriteTokens float64 `json:"cache_write_tokens"`
	Turns            int     `json:"turns"`
	WallMs           float64 `json:"wall_ms"`
	End              string  `json:"end,omitempty"`
}

// add counts one stream record into m.
func (m *MessageMetrics) add(r map[string]any) {
	switch str(r["type"]) {
	case "result":
		// num_turns and usage are per segment: a claude process that wakes
		// for background agents writes one result per segment (L754), so
		// they add. Cost is not read here (claudeStream counts it once).
		m.Turns += int(num(r["num_turns"]))
		m.InputTokens += num(at(r, "usage", "input_tokens"))
		m.OutputTokens += num(at(r, "usage", "output_tokens"))
		m.CacheReadTokens += num(at(r, "usage", "cache_read_input_tokens"))
		m.CacheWriteTokens += num(at(r, "usage", "cache_creation_input_tokens"))
	case "turn_start":
		m.Turns++
	case "message_end":
		if str(at(r, "message", "role")) == "assistant" {
			u := at(r, "message", "usage")
			m.InputTokens += num(at(u, "input"))
			m.OutputTokens += num(at(u, "output"))
			m.CacheReadTokens += num(at(u, "cacheRead"))
			m.CacheWriteTokens += num(at(u, "cacheWrite"))
		}
	}
}

// messageMetrics are the messages of stream; a stream with no
// bench_message record (a single-message run) is one message of wallMs.
func messageMetrics(stream []byte, wallMs float64) []MessageMetrics {
	var out []MessageMetrics
	var cur *MessageMetrics
	start := 0.0
	for _, r := range records(stream) {
		if str(r["type"]) != boundaryType {
			if cur != nil {
				cur.add(r)
			}
			continue
		}
		switch str(r["phase"]) {
		case "start":
			out = append(out, MessageMetrics{N: int(num(r["n"]))})
			cur, start = &out[len(out)-1], num(r["at_ms"])
		case "end":
			if cur != nil {
				cur.WallMs, cur.End = num(r["at_ms"])-start, str(r["reason"])
			}
			cur = nil
		}
	}
	if out != nil {
		return out
	}
	m := MessageMetrics{N: 1, WallMs: wallMs}
	for _, r := range records(stream) {
		m.add(r)
	}
	return []MessageMetrics{m}
}

// MessageCheck is task.yaml's check_<Check>, run after message After settled.
type MessageCheck struct {
	After  int  `json:"after"`
	Check  int  `json:"check"`
	Passed bool `json:"passed"`
}

// regressions are the times a check failed after it had passed after an
// earlier message: a later message broke earlier behaviour.
func regressions(checks []MessageCheck) int {
	n, passed := 0, map[int]bool{}
	for _, c := range checks {
		switch {
		case c.Passed:
			passed[c.Check] = true
		case passed[c.Check]:
			n++
			passed[c.Check] = false
		}
	}
	return n
}

// finalChecks are the checks run after the last message that ran: how many passed.
func finalChecks(checks []MessageCheck) Rate {
	var r Rate
	last := 0
	for _, c := range checks {
		last = max(last, c.After)
	}
	for _, c := range checks {
		if c.After == last {
			r.add(&c.Passed)
		}
	}
	return r
}

// messageChecks runs, after message n of r settled, each of its task's
// check_<k> with k <= n on a copy of the tree delivered at that moment
// (deliveredTree; the main checkout when none is, T86b) with the hidden
// directory in it, so the agent never sees the hidden tests; each result
// goes to checks, its output to the run's checks.txt.
func messageChecks(r *Run, env []string, start string, n int, checks *[]MessageCheck) error {
	if len(r.Task.MessageChecks) == 0 {
		return nil
	}
	copyDir, err := os.MkdirTemp(r.Dir, "check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(copyDir)
	tree := r.Repo
	if t, err := deliveredTree(r.Repo, start, env, nil); err == nil && t.Dir != "" {
		tree = t.Dir
	}
	if err := copyTree(tree, copyDir); err != nil {
		return err
	}
	if err := copyTree(r.Task.HiddenDir(), copyDir); err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(r.Dir, "checks.txt"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer log.Close()
	for k, check := range r.Task.MessageChecks[:min(n, len(r.Task.MessageChecks))] {
		if check == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
		c := exec.CommandContext(ctx, "sh", "-c", check)
		c.Dir, c.Env = copyDir, append(slices.Clone(env), "PYTHONDONTWRITEBYTECODE=1")
		out, err := c.CombinedOutput()
		cancel()
		if _, exit := err.(*exec.ExitError); err != nil && !exit {
			return err
		}
		verdict := "PASS"
		if err != nil {
			verdict = "FAIL"
		}
		fmt.Fprintf(log, "== after message %d, check %d: %s\n$ %s\n%s\n", n, k+1, verdict, check, bytes.TrimSpace(out))
		*checks = append(*checks, MessageCheck{After: n, Check: k + 1, Passed: err == nil})
	}
	return nil
}
