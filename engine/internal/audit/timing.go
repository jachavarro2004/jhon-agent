package audit

import (
	"math"
)

// ThreadTiming is where one thread's time went, read from its lines' times:
// the gap before an assistant line is the model's (thinking, then writing,
// one response's lines after another), the gap before a tool result the
// tool's; the gap before a prompt or a notification is waiting, and counts
// for neither. Turns are its responses; CacheHit is the share of the context
// they processed that came from the cache (NaN with no usage recorded).
// FirstResponseMs is, for a sub-agent, from its prompt to its first response
// line: what starting it cost before it did anything (NaN for the main thread).
type ThreadTiming struct {
	Agent, AgentType, Change string
	Turns                    int
	ModelMs, ToolMs          float64
	FirstResponseMs          float64
	Processed, CacheRead     float64
	CacheHit                 float64
}

// Timing is how the session ran: the main thread and each sub-agent, and
// the main thread's compactions.
type Timing struct {
	Main        ThreadTiming
	Agents      []ThreadTiming
	Compactions []Compaction
}

// computeTiming measures every thread of s.
func computeTiming(s *Session) *Timing {
	t := &Timing{Main: threadTiming(s.Main, false), Compactions: s.Compactions}
	if t.Compactions == nil {
		t.Compactions = []Compaction{}
	}
	t.Agents = []ThreadTiming{}
	for _, a := range s.Subagents {
		x := threadTiming(&a.Transcript, true)
		x.Change = a.change
		t.Agents = append(t.Agents, x)
	}
	return t
}

func threadTiming(tr *Transcript, sub bool) ThreadTiming {
	x := ThreadTiming{Agent: tr.Agent, AgentType: tr.AgentType, FirstResponseMs: math.NaN(), CacheHit: math.NaN()}
	usage := map[string]*Usage{}
	var ids []string
	prev := math.NaN()
	for i, e := range tr.Entries {
		at := dateParse(e.Timestamp)
		gap := math.Max(0, at-prev)
		switch {
		case e.Role == "assistant":
			if !math.IsNaN(gap) {
				x.ModelMs += gap
			}
			if sub && math.IsNaN(x.FirstResponseMs) && i > 0 {
				x.FirstResponseMs = math.Max(0, at-dateParse(tr.Entries[0].Timestamp))
			}
			// Every line of one response carries its id; pi's carry none, and each is a response.
			id := ""
			if e.MessageID != nil {
				id = *e.MessageID
			}
			if _, seen := usage[id]; id == "" || !seen {
				x.Turns++
				ids = append(ids, id)
			}
			if e.Usage != nil {
				if id == "" {
					x.addUsage(e.Usage)
				} else {
					usage[id] = e.Usage
				}
			} else if _, seen := usage[id]; !seen && id != "" {
				usage[id] = nil
			}
		case len(e.ToolResults) > 0 && !math.IsNaN(gap):
			x.ToolMs += gap
		}
		if !math.IsNaN(at) {
			prev = at
		}
	}
	for _, id := range ids {
		if u := usage[id]; u != nil {
			x.addUsage(u)
		}
	}
	if x.Processed > 0 {
		x.CacheHit = x.CacheRead / x.Processed
	}
	return x
}

func (x *ThreadTiming) addUsage(u *Usage) {
	x.Processed += u.Input + u.CacheCreation + u.CacheRead
	x.CacheRead += u.CacheRead
}
