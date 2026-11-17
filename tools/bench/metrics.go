package main

import (
	"bytes"
	"encoding/json"
	"strings"
)

// StreamFacts are what a run's stdout says about it.
type StreamFacts struct {
	SessionID    string
	Turns        int
	TurnStarts   int
	OutputTokens float64
	CostUSD      float64
	// Utilization is the seven_day window's utilization (0-1), nil when the
	// stream carries none.
	Utilization *float64
}

// Metrics are one run's numbers, written to its metrics.json.
type Metrics struct {
	Task      string   `json:"task"`
	Variant   string   `json:"variant"`
	Condition string   `json:"condition"`
	Repeat    int      `json:"repeat"`
	SessionID string   `json:"session_id"`
	Model     string   `json:"model"`
	PiEnv     []string `json:"pi_env,omitempty"`
	// Profile is the bench HOME's active Jhon profile when it is not
	// default (T77b): the models of a Jhon condition's sub-agents.
	Profile string `json:"profile,omitempty"`
	// ProjectSettings are the KEY=VALUE settings written into the repo's
	// LDD config before the agent started (T86): part of the cell.
	ProjectSettings []string `json:"project_settings,omitempty"`
	// TaskSize is the task's size (T146 FB1), which picks its `bench value`
	// gates; SteadyState is whether the run's repo started already set up
	// for LDD (T146 FB2).
	TaskSize    string `json:"task_size,omitempty"`
	SteadyState bool   `json:"steady_state,omitempty"`
	// JhonCommit and JhonVersion are the bench HOME's `jhon version --json`
	// when the batch started (T60).
	JhonCommit  string `json:"jhon_commit,omitempty"`
	JhonVersion string `json:"jhon_version,omitempty"`
	Success     bool   `json:"success"`
	// CheckedTree is the tree the hidden check and the tree facts were read
	// on (T82, tree.go): "main", "worktree:<branch>" or "none".
	CheckedTree      string         `json:"checked_tree,omitempty"`
	Exit             int            `json:"exit"`
	TimedOut         bool           `json:"timed_out"`
	TurnCapped       bool           `json:"turn_capped"`
	Turns            int            `json:"turns"`
	InputTokens      float64        `json:"input_tokens"`
	OutputTokens     float64        `json:"output_tokens"`
	CacheReadTokens  float64        `json:"cache_read_tokens"`
	CacheWriteTokens float64        `json:"cache_write_tokens"`
	CostUSD          float64        `json:"cost_usd"`
	ActiveMs         float64        `json:"active_ms"`
	ElapsedMs        float64        `json:"elapsed_ms"`
	WallMs           float64        `json:"wall_ms"`
	CacheHit         *float64       `json:"cache_hit"`
	AgentsLaunched   int            `json:"agents_launched"`
	VerifyRuns       int            `json:"verify_runs"`
	Findings         map[string]int `json:"findings"`
	Utilization      *float64       `json:"utilization"`
	QuotaPoints      *float64       `json:"quota_points"`
	Transcript       string         `json:"transcript"`
	// The process-quality metrics (process.go); nil is unknown.
	TestChanged          *bool    `json:"test_changed"`
	TestsRunBeforeEnd    *bool    `json:"tests_run_before_end"`
	RedBeforeGreen       *bool    `json:"red_before_green"`
	RedBeforeGreenSource string   `json:"red_before_green_source,omitempty"`
	OutOfScopeFiles      *int     `json:"out_of_scope_files"`
	OutOfScope           []string `json:"out_of_scope,omitempty"`
	ChangedFiles         []string `json:"changed_files,omitempty"`
	// The quality metrics of T46 (quality.go, process.go): each message's
	// numbers, the per-message checks, regressions among them (nil when the
	// task names no check) and rework (nil when the transcript is missing).
	Messages         []MessageMetrics `json:"messages,omitempty"`
	ChecksPerMessage []MessageCheck   `json:"checks_per_message,omitempty"`
	Regressions      *int             `json:"regressions"`
	Rework           *int             `json:"rework"`
	// UIRequestsDeclined are the dialogs pi raised in RPC mode, each answered
	// as cancelled (T47).
	UIRequestsDeclined int `json:"ui_requests_declined"`
	// The certification metrics (ceremony.go, T60): nil when the run's
	// variant or task has none. Refusals are the top-level refusals of the
	// run's stream (streamRefusals, T105).
	Ceremony  *Ceremony  `json:"ceremony,omitempty"`
	LddLayout *LddLayout `json:"ldd_layout,omitempty"`
	Slices    *float64   `json:"slices"`
	Refusals  *int       `json:"refusals"`
	Problems  []string   `json:"problems,omitempty"`
}

// records are the JSON objects of a JSONL stream; unreadable lines are skipped.
func records(b []byte) []map[string]any {
	var out []map[string]any
	for _, line := range bytes.Split(b, []byte("\n")) {
		var m map[string]any
		if json.Unmarshal(bytes.TrimSpace(line), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

// at is the value at a path of object keys, nil when any step is missing.
func at(v any, path ...string) any {
	for _, k := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

// sevenDay is the seven_day utilization in a rate_limit_event record.
func sevenDay(r map[string]any) *float64 {
	if str(r["type"]) != "rate_limit_event" {
		return nil
	}
	if u, ok := at(r, "rate_limit_info", "unifiedWindows", "seven_day", "utilization").(float64); ok {
		return &u
	}
	if str(at(r, "rate_limit_info", "rateLimitType")) == "seven_day" {
		if u, ok := at(r, "rate_limit_info", "utilization").(float64); ok {
			return &u
		}
	}
	return nil
}

// claudeStream reads Claude Code's stream-json: the result events' turns,
// output tokens and cost, summed over a multi-message run's calls, and the
// last rate_limit_event's weekly reading.
//
// One claude process can write several result records: a headless run
// whose orchestrator launched background agents ends its turn (result
// 0), wakes when they finish and writes another (result_index 1, ...)
// (T90 probe, L754). num_turns and usage are per segment and add up;
// total_cost_usd is the process's running total, so a record with
// result_index > 0 replaces its process's cost instead of adding to it.
// Records without result_index (older Claude Code) each add, as before.
func claudeStream(b []byte) StreamFacts {
	var f StreamFacts
	proc := 0.0 // the current process's running total, not yet in f.CostUSD
	for _, r := range records(b) {
		if u := sevenDay(r); u != nil {
			f.Utilization = u
		}
		if id := str(r["session_id"]); id != "" && f.SessionID == "" {
			f.SessionID = id
		}
		if str(r["type"]) == "result" {
			f.Turns += int(num(r["num_turns"]))
			f.OutputTokens += num(at(r, "usage", "output_tokens"))
			cost := num(r["total_cost_usd"])
			if num(r["result_index"]) > 0 {
				proc = max(proc, cost)
			} else {
				f.CostUSD += proc
				proc = cost
			}
		}
	}
	f.CostUSD += proc
	return f
}

// bridgeHeader finds the weekly utilization pi's claude-bridge re-emits as
// an Anthropic unified header, anywhere in a record (unverified: the pilot
// shows whether pi's JSON stream carries it).
func bridgeHeader(v any) *float64 {
	switch v := v.(type) {
	case map[string]any:
		for k, x := range v {
			if k == "anthropic-ratelimit-unified-7d-utilization" || k == "anthropic-ratelimit-unified-seven_day-utilization" {
				var u float64
				if err := json.Unmarshal([]byte(strings.TrimSpace(str(x))), &u); err == nil {
					return &u
				}
				if f, ok := x.(float64); ok {
					return &f
				}
			}
			if u := bridgeHeader(x); u != nil {
				return u
			}
		}
	case []any:
		for _, x := range v {
			if u := bridgeHeader(x); u != nil {
				return u
			}
		}
	}
	return nil
}

// piStream reads pi's JSON mode: the session header's id, turn_start
// events as turns, the assistant messages' output tokens, and a weekly
// reading when the bridge passes one through.
func piStream(b []byte) StreamFacts {
	var f StreamFacts
	for _, r := range records(b) {
		switch str(r["type"]) {
		case "session":
			f.SessionID = str(r["id"])
		case "turn_start":
			f.TurnStarts++
		case "message_end":
			if str(at(r, "message", "role")) == "assistant" {
				f.OutputTokens += num(at(r, "message", "usage", "output"))
				f.CostUSD += num(at(r, "message", "usage", "cost", "total"))
			}
		}
		if u := sevenDay(r); u != nil {
			f.Utilization = u
		} else if u := bridgeHeader(r); u != nil {
			f.Utilization = u
		}
	}
	f.Turns = f.TurnStarts
	return f
}

// fromAudit fills m from `jhon audit --json`: tokens summed over the
// session's threads, active and elapsed time, the main thread's cache hit,
// agents launched and verify runs over its changes, and findings by check.
func (m *Metrics) fromAudit(b []byte) error {
	var a map[string]any
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	turns := 0
	for _, x := range list(at(a, "context_use", "agents")) {
		m.InputTokens += num(at(x, "input_tokens"))
		m.CacheWriteTokens += num(at(x, "cache_creation_input_tokens"))
		m.CacheReadTokens += num(at(x, "cache_read_input_tokens"))
		if turns == 0 {
			turns = int(num(at(x, "turns")))
		}
	}
	if m.Turns == 0 {
		m.Turns = turns
	}
	m.ActiveMs = num(at(a, "outcomes", "session", "active_ms"))
	m.ElapsedMs = num(at(a, "outcomes", "session", "elapsed_ms"))
	if h, ok := at(a, "timing", "main", "cache_hit").(float64); ok {
		m.CacheHit = &h
	}
	for _, c := range list(at(a, "outcomes", "changes")) {
		m.AgentsLaunched += int(num(at(c, "agents")))
		m.VerifyRuns += int(num(at(c, "verify_runs")))
	}
	m.Findings = map[string]int{}
	for _, f := range list(a["findings"]) {
		if c := str(at(f, "check")); c != "" {
			m.Findings[c]++
		}
	}
	return nil
}
