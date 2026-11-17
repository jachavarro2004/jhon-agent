package audit

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode"

	"github.com/jachavarro2004/jhon-agent/engine/internal/js"
)

// The owner's dialogue with the main thread, in counts (T66a, L990): how
// often the owner answered, what each answer set off before the session
// waited for them again, and how the session asked: through the ask tool,
// or in plain text. A statistic like the timing: never a finding, and no
// message, question, or option text ever leaves it. Sub-agents are left
// out: they do not talk to the owner.

// askTool is the tool that puts questions to the owner, by the name the
// checks read: Claude Code's AskUserQuestion, which pi's ask_user maps onto
// (piTools, piInput).
const askTool = "AskUserQuestion"

// AnswerCost is what one answer of the owner's set off until the session
// next waited for the owner: the main thread's responses, its tool calls
// (the ask tool's own apart: a question is not work), and the time from the
// answer to the last of those responses, 0 with none. What the owner then
// took to answer is in none of them.
type AnswerCost struct {
	Responses, ToolCalls int
	Ms                   float64
}

// Dialogue is the dialogue of one main thread.
//
// Answers has one entry per answer of the owner's, in order: a message the
// owner wrote after their first (IsGenuineUserMessage), or a result of the
// ask tool that is not an error (one that failed, or that the owner or a
// hook refused, answered nothing). Each is measured until the next answer,
// or the end.
//
// AskCalls are the calls of the ask tool, answered or not, and
// QuestionsPerCall how many of them put one, two, three, and four or more
// questions; a call that names no question is in none.
//
// ProseQuestions are the turns that ended waiting for the owner on a
// question in plain text: the turn's last response calls no tool, its last
// text ends in a question mark once the whitespace, closing markdown, and
// quotes after it are trimmed, and the turn holds no call of the ask tool.
// It is a lower bound: a question followed by more text, one asked in the
// middle of a reply, and one with no question mark are not counted, and
// neither is one whose turn the owner cut short.
//
// Corrections are the owner's messages that correct the agent, by kind (corrections.go).
type Dialogue struct {
	Answers          []AnswerCost
	AskCalls         int
	QuestionsPerCall [4]int
	ProseQuestions   int
	Corrections      Corrections
}

// computeDialogue measures the main thread of s.
func computeDialogue(s *Session) *Dialogue {
	d := &Dialogue{Answers: []AnswerCost{}}
	es := s.Main.Entries
	asks := map[string]bool{}
	for _, e := range es {
		for _, u := range e.ToolUses {
			if u.Name != askTool {
				continue
			}
			asks[u.ID] = true
			d.AskCalls++
			if qs, _ := propList(u.Input, "questions"); len(qs) > 0 {
				d.QuestionsPerCall[min(len(qs), len(d.QuestionsPerCall))-1]++
			}
		}
	}
	var (
		opened   bool        // the owner's first message was read
		cur      *AnswerCost // the answer being measured; nil before the first
		from     float64     // when it was given
		last     = -1        // the turn's last assistant entry
		reply    = -1        // the main thread's last assistant entry, whatever the turn
		asked    bool        // the turn holds a call of the ask tool
		response = map[string]bool{}
	)
	// wait closes the turn: the session waits for the owner, or ends.
	wait := func() {
		if last >= 0 && !asked && endsInQuestion(es, last) {
			d.ProseQuestions++
		}
		if cur != nil {
			if last >= 0 {
				if ms := dateParse(es[last].Timestamp) - from; ms > 0 {
					cur.Ms = ms
				}
			}
			d.Answers = append(d.Answers, *cur)
		}
		cur, last, asked = nil, -1, false
	}
	for i, e := range es {
		answered := slices.ContainsFunc(e.ToolResults, func(r ToolResult) bool { return asks[r.ID] && !r.IsError })
		wrote := IsGenuineUserMessage(e)
		switch {
		case wrote || answered:
			if wrote {
				d.Corrections.count(e.note(), reply >= 0 && asksOwner(es, reply))
			}
			wait()
			if answered || opened {
				cur, from = &AnswerCost{}, dateParse(e.Timestamp)
			}
			opened = opened || wrote
		case e.Role == "assistant":
			last, reply = i, i
			// Every line of one response carries its id; pi's are each a response (threadTiming).
			id := ""
			if e.MessageID != nil {
				id = *e.MessageID
			}
			if cur != nil && (id == "" || !response[id]) {
				cur.Responses++
			}
			response[id] = true
			for _, u := range e.ToolUses {
				switch {
				case u.Name == askTool:
					asked = true
				case cur != nil:
					cur.ToolCalls++
				}
			}
		}
	}
	wait()
	return d
}

// closers are what may follow a question mark at the end of a text: closing markdown and quotes.
const closers = "*_`~\"'”’»›"

// endsInQuestion is whether the response whose last line is es[last] calls
// no tool and ends its text in a question mark.
func endsInQuestion(es []*Entry, last int) bool {
	id := es[last].MessageID
	for i := last; i >= 0; i-- {
		e := es[i]
		if e.Role != "assistant" || (i < last && (id == nil || e.MessageID == nil || *e.MessageID != *id)) {
			break
		}
		if len(e.ToolUses) > 0 {
			return false
		}
		for j := len(e.Texts) - 1; j >= 0; j-- {
			text := strings.TrimRightFunc(e.Texts[j], func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(closers, r) })
			if text != "" {
				return strings.HasSuffix(text, "?") || strings.HasSuffix(text, "？")
			}
		}
	}
	return false
}

// spread is the median and the maximum of xs, NaN for none. The median is
// the lower of the two middle values of an even count, as --all's other
// medians are: a value some answer had.
func spread(xs []float64) (median, max float64) {
	if len(xs) == 0 {
		return math.NaN(), math.NaN()
	}
	xs = slices.Sorted(slices.Values(xs))
	return xs[(len(xs)-1)/2], xs[len(xs)-1]
}

func (d *Dialogue) spread(of func(AnswerCost) float64) (median, max float64) {
	xs := make([]float64, len(d.Answers))
	for i, a := range d.Answers {
		xs[i] = of(a)
	}
	return spread(xs)
}

// Responses are the responses per answer: the median and the maximum, NaN with no answer.
func (d *Dialogue) Responses() (median, max float64) {
	return d.spread(func(a AnswerCost) float64 { return float64(a.Responses) })
}

// ToolCalls are the tool calls per answer: the median and the maximum, NaN with no answer.
func (d *Dialogue) ToolCalls() (median, max float64) {
	return d.spread(func(a AnswerCost) float64 { return float64(a.ToolCalls) })
}

// Ms is the time per answer in milliseconds: the median and the maximum, NaN with no answer.
func (d *Dialogue) Ms() (median, max float64) {
	return d.spread(func(a AnswerCost) float64 { return a.Ms })
}

// fill sets the dialogue's keys on o, as the report and --all state them.
func (d *Dialogue) fill(o *js.Object) *js.Object {
	pair := func(median, max float64) *js.Object { return js.Obj("median", measure(median), "max", measure(max)) }
	o.Set("owner_answers", float64(len(d.Answers)))
	o.Set("responses_per_answer", pair(d.Responses()))
	o.Set("tool_calls_per_answer", pair(d.ToolCalls()))
	o.Set("ms_per_answer", pair(d.Ms()))
	o.Set("ask_tool_calls", float64(d.AskCalls))
	q := d.QuestionsPerCall
	o.Set("questions_per_call", js.Obj("1", float64(q[0]), "2", float64(q[1]), "3", float64(q[2]), "4_or_more", float64(q[3])))
	o.Set("prose_questions", float64(d.ProseQuestions))
	o.Set("corrections", d.Corrections.value())
	return o
}

func (d Dialogue) value() *js.Object { return d.fill(js.NewObject()) }

// humanDialogue is the dialogue as a person reads it: the answers and what
// each set off, then how the session asked, then how often the owner corrected it.
func humanDialogue(d *Dialogue) []string {
	first := fmt.Sprintf("  dialogue  owner answers %d", len(d.Answers))
	if len(d.Answers) > 0 {
		responses, maxResponses := d.Responses()
		calls, maxCalls := d.ToolCalls()
		ms, maxMs := d.Ms()
		first += fmt.Sprintf("  responses per answer %s (max %s)  tool calls per answer %s (max %s)  time per answer %s (max %s)",
			num(responses), num(maxResponses), num(calls), num(maxCalls), Duration(ms), Duration(maxMs))
	}
	q, c := d.QuestionsPerCall, d.Corrections
	return []string{first, fmt.Sprintf("  dialogue  ask-tool calls %d  questions per call 1:%d 2:%d 3:%d 4+:%d  prose questions %d (lower bound)",
		d.AskCalls, q[0], q[1], q[2], q[3], d.ProseQuestions),
		fmt.Sprintf("  dialogue  corrections %d of %d owner messages  keep-going %d  repeat %d  unrequested %d  decision %d",
			c.Correcting, c.OwnerMessages, c.KeepGoing, c.Repeat, c.Unrequested, c.Decision)}
}

// FleetDialogue is the dialogue of many sessions (`jhon audit --all`): the
// counts summed, and every answer of every session kept, so the median and
// the maximum are over them all. Sessions are those summed: the ones whose
// audit read them.
type FleetDialogue struct {
	Sessions int
	Dialogue
}

// add sums one session's dialogue into the fleet's.
func (f *FleetDialogue) add(d *Dialogue) {
	f.Sessions++
	f.Answers = append(f.Answers, d.Answers...)
	f.AskCalls += d.AskCalls
	for i, n := range d.QuestionsPerCall {
		f.QuestionsPerCall[i] += n
	}
	f.ProseQuestions += d.ProseQuestions
	f.Corrections.add(d.Corrections)
}

// Value is the fleet's dialogue as `jhon audit --all --json` states it: the
// sessions summed, then one session's keys.
func (f FleetDialogue) Value() *js.Object {
	return f.fill(js.Obj("sessions", float64(f.Sessions)))
}
