package dashboard

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// What a session spent, as its transcripts record it (L138, answers 2 and
// 3): each assistant message's usage, from the main transcript and its
// sub-agents' (`<session>/subagents/*.jsonl`, for both runtimes), summed by
// day and model. pi records `message.usage` as input, output, cacheRead,
// cacheWrite, totalTokens and `cost.total`; Claude Code as input_tokens,
// output_tokens, cache_read_input_tokens and cache_creation_input_tokens,
// with no cost, and one response streamed as several entries sharing its
// message id, counted once. Money is only what pi recorded: a row's cost is
// null when none of its messages recorded one; nothing is estimated.

// A UsageRow is one day's and one model's usage in a session.
type UsageRow struct {
	Day        string   `json:"day"`
	Model      string   `json:"model"`
	Input      float64  `json:"input"`
	Output     float64  `json:"output"`
	CacheRead  float64  `json:"cache_read"`
	CacheWrite float64  `json:"cache_write"`
	Total      float64  `json:"total"`
	Cost       *float64 `json:"cost"`
	Messages   int      `json:"messages"`
}

// Usage is a session's usage, by day then model.
type Usage struct {
	Rows []UsageRow `json:"rows"`
}

// usageValue is u as the dashboard's JSON values hold it.
func usageValue(u Usage) any {
	b, _ := json.Marshal(u)
	v, _ := js.ParseJSON(string(b))
	return v
}

// zone is where a day starts: the machine's, as the page's; a variable for the tests.
var zone = time.Local

type usageMsg struct {
	at, model                         string
	input, output, read, write, total float64
	cost                              *float64
}

// UsageOf is the usage the session at path recorded, its sub-agents' included.
func UsageOf(path string) Usage {
	files := []string{path}
	if path != "" {
		subs, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(path, ".jsonl"), "subagents", "*.jsonl"))
		files = append(files, subs...)
	}
	rows := map[[2]string]*UsageRow{}
	for _, f := range files {
		for _, m := range usageMessages(f) {
			t, err := time.Parse(time.RFC3339Nano, m.at)
			if err != nil {
				continue
			}
			key := [2]string{t.In(zone).Format("2006-01-02"), m.model}
			r := rows[key]
			if r == nil {
				r = &UsageRow{Day: key[0], Model: key[1]}
				rows[key] = r
			}
			r.Input += m.input
			r.Output += m.output
			r.CacheRead += m.read
			r.CacheWrite += m.write
			r.Total += m.total
			r.Messages++
			if m.cost != nil {
				c := roundCost(*m.cost + deref(r.Cost))
				r.Cost = &c
			}
		}
	}
	out := Usage{Rows: []UsageRow{}}
	for _, r := range rows {
		out.Rows = append(out.Rows, *r)
	}
	slices.SortFunc(out.Rows, func(a, b UsageRow) int {
		if c := strings.Compare(a.Day, b.Day); c != 0 {
			return c
		}
		return strings.Compare(a.Model, b.Model)
	})
	return out
}

// usageMessages is the assistant messages of one transcript that record a
// usage, a Claude Code response once (its last entry's usage).
func usageMessages(path string) []usageMsg {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []usageMsg
	byID := map[string]int{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"usage"`) {
			continue
		}
		var e struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Message   struct {
				ID    string                     `json:"id"`
				Role  string                     `json:"role"`
				Model string                     `json:"model"`
				Usage map[string]json.RawMessage `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &e) != nil || e.Message.Role != "assistant" || e.Message.Usage == nil {
			continue
		}
		u := e.Message.Usage
		model := e.Message.Model
		if model == "" {
			model = "unknown"
		}
		var m usageMsg
		switch e.Type {
		case "message": // pi
			m = usageMsg{at: e.Timestamp, model: model, input: tokens(u["input"]), output: tokens(u["output"]), read: tokens(u["cacheRead"]), write: tokens(u["cacheWrite"])}
			m.total = m.input + m.output + m.read + m.write
			if t, ok := number(u["totalTokens"]); ok && t >= 0 {
				m.total = t
			}
			var cost struct {
				Total *float64 `json:"total"`
			}
			if json.Unmarshal(u["cost"], &cost) == nil && cost.Total != nil && *cost.Total >= 0 && !math.IsInf(*cost.Total, 0) {
				m.cost = cost.Total
			}
		case "assistant": // Claude Code
			if model == "<synthetic>" {
				continue
			}
			m = usageMsg{at: e.Timestamp, model: model, input: tokens(u["input_tokens"]), output: tokens(u["output_tokens"]), read: tokens(u["cache_read_input_tokens"]), write: tokens(u["cache_creation_input_tokens"])}
			m.total = m.input + m.output + m.read + m.write
			if i, seen := byID[e.Message.ID]; seen && e.Message.ID != "" {
				m.at = out[i].at
				out[i] = m
				continue
			}
			byID[e.Message.ID] = len(out)
		default:
			continue
		}
		out = append(out, m)
	}
	return out
}

func number(raw json.RawMessage) (float64, bool) {
	var f float64
	if json.Unmarshal(raw, &f) != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// tokens is a token count: a finite number not below zero, else 0.
func tokens(raw json.RawMessage) float64 {
	if f, ok := number(raw); ok && f >= 0 {
		return f
	}
	return 0
}

func deref(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

// roundCost keeps a sum of dollars to nine decimals, as pi's own stats did.
func roundCost(f float64) float64 { return math.Round(f*1e9) / 1e9 }
